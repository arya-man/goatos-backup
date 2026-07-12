package sg.mesha.goatos.core.analytics

import android.content.Context
import android.os.Bundle
import com.google.firebase.analytics.FirebaseAnalytics

/**
 * Real [AnalyticsPort] backed by Firebase Analytics (GA4). Bound in place of [NoopAnalytics] for
 * flavors with `BuildConfig.TELEMETRY_ENABLED = true` (see `di/TelemetryModule.kt` in `:app`).
 *
 * [crashReporter] is composed here (not called directly by feature code) so every durable
 * identity property this class ever sets is ALSO visible on the next crash/non-fatal report —
 * [AnalyticsEvents.UserProps] are role/primary_park/flavor/tenant only, so this never risks
 * leaking PII into Crashlytics. [setUserProperty] enforces an allowlist to prevent accidental
 * forwarding of arbitrary names (e.g. email, name) to Crashlytics custom keys.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
class FirebaseAnalyticsAdapter(
    context: Context,
    private val crashReporter: CrashReporter,
) : AnalyticsPort {
    private val firebaseAnalytics: FirebaseAnalytics = FirebaseAnalytics.getInstance(context)

    override fun track(event: String, props: Map<String, String>) {
        val bundle = Bundle(props.size)
        for ((key, value) in props) bundle.putString(key, value)
        firebaseAnalytics.logEvent(event, bundle)
    }

    override fun setUserProperty(name: String, value: String?) {
        // Enforce allowlist: only forward whitelisted user property keys to Crashlytics.
        // This prevents accidental PII leaks (email, name, phone, etc.) into crash reports.
        if (name in ALLOWED_USER_PROPERTY_KEYS) {
            firebaseAnalytics.setUserProperty(name, value)
            crashReporter.setCustomKey(name, value.orEmpty())
        } else {
            // Log-skip arbitrary keys (do not forward to Crashlytics, but still set in GA4
            // if the caller explicitly called this method; GA4's own filtering will reject
            // keys outside its public schema).
            firebaseAnalytics.setUserProperty(name, value)
        }
    }

    /**
     * Sets the durable PRINCIPAL identifier (the stable, non-PII workforce member id) on GA4.
     * Also forwarded to Crashlytics so a crash report is attributable to the same operator —
     * never a name/email/phone, matching the [setUserProperty] allowlist discipline above.
     * `null` clears the identity on both (logout clean-slate; see `PushLogoutCleanup`).
     */
    override fun setUserId(id: String?) {
        firebaseAnalytics.setUserId(id)
        crashReporter.setCustomKey(USER_ID_CRASH_KEY, id.orEmpty())
    }

    companion object {
        /**
         * Allowed user-property keys that are safe to forward to Crashlytics.
         * These are the durable identity dimensions used by Goat OS analytics.
         * Any other key is dropped from Crashlytics to prevent PII leaks.
         */
        private val ALLOWED_USER_PROPERTY_KEYS = setOf(
            AnalyticsEvents.UserProps.ROLE,
            AnalyticsEvents.UserProps.PRIMARY_PARK,
            AnalyticsEvents.UserProps.FLAVOR,
            AnalyticsEvents.UserProps.TENANT,
        )

        /** Crashlytics custom key the workforce member id is stamped under (see [setUserId]). */
        private const val USER_ID_CRASH_KEY = "member_id"
    }
}
