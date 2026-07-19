package sg.mesha.goatos

import android.app.Application
import android.util.Log
import androidx.hilt.work.HiltWorkerFactory
import androidx.work.Configuration
import com.google.firebase.FirebaseApp
import dagger.hilt.android.HiltAndroidApp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.PerformanceTracer
import sg.mesha.goatos.core.analytics.PerformanceTraceNames
import sg.mesha.goatos.core.analytics.TraceHandle
import sg.mesha.goatos.core.data.sync.ConnectivitySyncTrigger
import sg.mesha.goatos.push.PushNotifications
import sg.mesha.goatos.sync.SyncWorkScheduler
import javax.inject.Inject

/** Application entry point + Hilt DI root. Kept thin (TRD §3).
 *
 * The sync triggers converge on the same durable Room outbox +
 * [sg.mesha.goatos.core.data.sync.SyncEngine.drainOnce]:
 *  - [connectivitySyncTrigger] drains promptly WHILE the process is alive (reconnect → drain).
 *  - WorkManager ([SyncWorkScheduler]) is the OS-scheduled backstop that survives PROCESS DEATH:
 *    a killed app with queued writes still drains under a CONNECTED constraint. Its worker is
 *    Hilt-injected via [workerFactory] ([Configuration.Provider]) — the default WorkManager
 *    initializer is removed in the manifest so this on-demand config is the one that wins.
 *
 * The visible `UploadForegroundService` is deliberately not launched here. WorkManager can create
 * the application process from `BOOT_COMPLETED`, where Android 15+ forbids `dataSync` foreground
 * promotion. The service is started only by a user-originated relevant enqueue; WorkManager owns
 * boot/process-death recovery. */
@HiltAndroidApp
class GoatOsApplication : Application(), Configuration.Provider {
    @Inject lateinit var connectivitySyncTrigger: ConnectivitySyncTrigger
    @Inject lateinit var syncWorkScheduler: SyncWorkScheduler
    @Inject lateinit var workerFactory: HiltWorkerFactory
    @Inject lateinit var appScope: CoroutineScope
    @Inject lateinit var analytics: AnalyticsPort
    @Inject lateinit var crashReporter: CrashReporter
    @Inject lateinit var performanceTracer: PerformanceTracer
    @Inject lateinit var pushNotifications: PushNotifications

    /** Started here, stopped on the first post-auth `MainActivity.onResume` (see
     *  `docs/TELEMETRY.md`). Public var (not Hilt-scoped) so `MainActivity` can stop the SAME
     *  handle without a second DI graph lookup; `null` after the first stop so it reports once. */
    var coldStartTrace: TraceHandle? = null

    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder().setWorkerFactory(workerFactory).build()

    override fun onCreate() {
        super.onCreate()
        // Cold-start custom trace (docs/TELEMETRY.md item 3) — started as early as possible;
        // stopped in MainActivity.onResume once the first frame after auth is showing.
        coldStartTrace = performanceTracer.startTrace(PerformanceTraceNames.APP_COLD_START)
        // Launch/session markers. Hilt has already field-injected [analytics] by the time
        // super.onCreate() returns, so it is safe to call here.
        analytics.track(AnalyticsEvents.APP_OPEN)
        analytics.track(AnalyticsEvents.SESSION_START)
        // Crash reporting: log a breadcrumb so every session boundary shows up alongside any
        // crash/non-fatal that follows it. Never logs PII — flavor is a build constant.
        crashReporter.log("app_open flavor=${BuildConfig.FLAVOR}")
        // Notification channels must exist BEFORE the first notification is posted (system
        // silently drops a notify() on a channel that was never created) — cheap, synchronous,
        // safe to call every cold start (NotificationManager.createNotificationChannels is
        // idempotent for an unchanged channel).
        pushNotifications.ensureChannels()
        // Firebase-availability check (docs: FCM push slice). A build flavor with no committed
        // `firebase.xml` (`prod` today) never gets a default FirebaseApp — FirebaseInitProvider
        // silently skips init when the required resources are missing, it does not throw. Every
        // push call site (DefaultNotificationsPort/AndroidPushTokenSync/GoatOsMessagingService/
        // PushLogoutCleanup) already degrades to a safe no-op via its own `runCatching`, so this
        // check changes no behavior — it only logs ONCE at cold start so a "push isn't working"
        // report on that flavor is immediately explained in logcat instead of requiring a repro
        // across every one of those runCatching call sites. (Analytics identity is a separate,
        // flavor-gated seam — see `di/AnalyticsModule.kt` — so it is unaffected by this check.)
        if (runCatching { FirebaseApp.getInstance() }.getOrNull() == null) {
            Log.w(TAG, "Firebase not configured for this flavor; push disabled")
        }
        // Off the main thread: enqueueUniquePeriodicWork does disk I/O on the calling thread, and
        // starting the connectivity trigger touches ConnectivityManager — neither is on the
        // critical path to first frame, so defer both to the app scope to keep cold start snappy.
        appScope.launch {
            connectivitySyncTrigger.start()
            syncWorkScheduler.schedule()
        }
    }

    private companion object {
        const val TAG = "GoatOsApplication"
    }
}
