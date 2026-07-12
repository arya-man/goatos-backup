package sg.mesha.goatos.core.analytics

import com.google.firebase.crashlytics.FirebaseCrashlytics

/**
 * Real [CrashReporter] backed by Firebase Crashlytics. Bound in place of [NoopCrashReporter] for
 * flavors with `BuildConfig.TELEMETRY_ENABLED = true` (see `di/TelemetryModule.kt` in `:app`).
 */
class FirebaseCrashReporter : CrashReporter {
    private val crashlytics: FirebaseCrashlytics
        get() = FirebaseCrashlytics.getInstance()

    override fun recordException(throwable: Throwable, message: String?) {
        message?.let { crashlytics.log(it) }
        crashlytics.recordException(throwable)
    }

    override fun log(message: String) {
        crashlytics.log(message)
    }

    override fun setCustomKey(key: String, value: String) {
        crashlytics.setCustomKey(key, value)
    }
}
