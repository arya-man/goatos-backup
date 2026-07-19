package sg.mesha.goatos.leak

import android.content.Context
import android.util.Log
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import leakcanary.EventListener
import sg.mesha.goatos.core.analytics.CrashReporter

/**
 * Debug-only bridge that forwards LeakCanary heap-analysis results to Firebase Crashlytics as
 * non-fatal reports, so memory leaks found on a running dev build show up in the same console as
 * crashes instead of living only in LeakCanary's on-device notification/UI.
 *
 * Scope by construction:
 *  - This file is in the `debug` source set and LeakCanary is a `debugImplementation` dependency,
 *    so the listener (and the whole `leakcanary.*` import) exists ONLY in debug builds. The stg
 *    App Distribution APK is a release build → no LeakCanary, no leak reports there. (dev flavor +
 *    debug build type = the only combination that captures leaks.)
 *  - Upload still depends on [CrashReporter] being the real Firebase one, which is bound only when
 *    `BuildConfig.TELEMETRY_ENABLED = true` (dev is now true — see `app/build.gradle.kts`). When
 *    telemetry is off the injected reporter is a no-op and this bridge silently does nothing.
 *
 * Only APPLICATION leaks are reported. LibraryLeaks are known third-party/SDK leaks and would be
 * console noise. Each leak is recorded with a synthetic throwable whose message + top frame carry
 * the LeakCanary signature, so Crashlytics groups repeats of the same leak into one issue, tagged
 * `issue_type=memory_leak` for easy filtering apart from real crashes.
 *
 * [CrashReporter] is resolved lazily via a Hilt [EntryPoint] the first time a leak fires — never at
 * registration time, because the registering ContentProvider ([LeakCanaryBridgeInstaller]) runs
 * before Hilt is initialised in Application.onCreate.
 */
internal class CrashlyticsLeakEventListener(
    private val appContext: Context,
) : EventListener {

    @EntryPoint
    @InstallIn(SingletonComponent::class)
    internal interface LeakBridgeEntryPoint {
        fun crashReporter(): CrashReporter
    }

    private val crashReporter: CrashReporter? by lazy {
        runCatching {
            EntryPointAccessors
                .fromApplication(appContext, LeakBridgeEntryPoint::class.java)
                .crashReporter()
        }.onFailure { Log.w(TAG, "CrashReporter unavailable; leaks not uploaded", it) }
            .getOrNull()
    }

    override fun onEvent(event: EventListener.Event) {
        if (event !is EventListener.Event.HeapAnalysisDone.HeapAnalysisSucceeded) return
        val reporter = crashReporter ?: return
        val leaks = event.heapAnalysis.applicationLeaks
        if (leaks.isEmpty()) return

        reporter.setCustomKey("issue_type", "memory_leak")
        for (leak in leaks) {
            val retainedBytes = leak.totalRetainedHeapByteSize
            val summary = buildString {
                append(leak.shortDescription)
                append(" [sig=").append(leak.signature).append(']')
                if (retainedBytes != null) append(" retained=").append(retainedBytes).append('B')
            }
            val throwable = LeakCanaryReportedLeak(summary).apply {
                // Single stable synthetic frame keyed on the signature → Crashlytics groups every
                // occurrence of the same leak trace into one issue.
                stackTrace = arrayOf(
                    StackTraceElement("LeakCanary", leak.signature, "LeakTrace", 0),
                )
            }
            reporter.log("leakcanary application_leak sig=${leak.signature}")
            reporter.recordException(throwable, "LeakCanary: ${leak.shortDescription}")
            Log.i(TAG, "Reported leak to Crashlytics: ${leak.signature}")
        }
    }

    /** Named so the Crashlytics issue title reads clearly as a leak, not a generic exception. */
    private class LeakCanaryReportedLeak(message: String) : RuntimeException(message)

    private companion object {
        const val TAG = "LeakCrashBridge"
    }
}
