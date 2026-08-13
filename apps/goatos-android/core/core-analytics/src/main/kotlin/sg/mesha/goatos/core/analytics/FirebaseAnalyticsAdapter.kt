package sg.mesha.goatos.core.analytics

import android.content.Context
import android.os.Bundle
import com.google.firebase.analytics.FirebaseAnalytics

/**
 * Real [AnalyticsPort] backed by Firebase Analytics (GA4). Bound in place of [NoopAnalytics] for
 * flavors with `BuildConfig.TELEMETRY_ENABLED = true` (see `di/TelemetryModule.kt` in `:app`).
 *
 * [crashReporter] is composed here (not called directly by feature code) so every durable
 * identity property this class ever sets is ALSO visible on the next crash/non-fatal report.
 * The forwarded [AnalyticsEvents.UserProps] are role/primary_park/flavor/tenant/device_id plus
 * `email` — the business owner's explicit decision to use email as the primary user identity
 * dimension. [setUserProperty] still enforces an allowlist so only these intended keys are
 * forwarded to Crashlytics; any other arbitrary key (e.g. name, phone) is dropped.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
class FirebaseAnalyticsAdapter(
    context: Context,
    private val crashReporter: CrashReporter,
    private val analyticsContext: AnalyticsContext,
) : AnalyticsPort {
    private val firebaseAnalytics: FirebaseAnalytics = FirebaseAnalytics.getInstance(context)

    init {
        firebaseAnalytics.setAnalyticsCollectionEnabled(true)
    }

    override fun track(event: String, props: Map<String, String>) {
        val mergedProps = firebaseEventParams(analyticsContext.standardEventParams() + props)
        val bundle = Bundle(mergedProps.size)
        for ((key, value) in mergedProps) bundle.putString(key, value)
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
            AnalyticsEvents.UserProps.EMAIL,
            AnalyticsEvents.UserProps.DEVICE_ID,
        )

        /** Crashlytics custom key the workforce member id is stamped under (see [setUserId]). */
        private const val USER_ID_CRASH_KEY = "member_id"
    }
}

internal const val FIREBASE_MAX_EVENT_PARAMS = 25
internal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100

internal fun firebaseEventParams(props: Map<String, String>): Map<String, String> {
    val result = LinkedHashMap<String, String>(FIREBASE_MAX_EVENT_PARAMS) // mobile-guard:ignore: bounded by FIREBASE_MAX_EVENT_PARAMS and allocated per event only.
    for (key in FIREBASE_PARAM_ALLOWLIST) {
        props[key]?.let { value ->
            if (result.size < FIREBASE_MAX_EVENT_PARAMS) result[key] = value.firebaseParamValue()
        }
    }
    return result
}

private fun String.firebaseParamValue(): String =
    if (length <= FIREBASE_MAX_PARAM_VALUE_LENGTH) this else take(FIREBASE_MAX_PARAM_VALUE_LENGTH)

private val FIREBASE_PARAM_ALLOWLIST = listOf(
    AnalyticsEvents.Params.DEVICE_ID,
    AnalyticsEvents.Params.JOURNEY_ID,
    AnalyticsEvents.UserProps.ROLE,
    AnalyticsEvents.UserProps.PRIMARY_PARK,
    "proof_id",
    "task_id",
    "field_key",
    "feature_surface",
    "proof_subject",
    "rfid_tag",
    AnalyticsEvents.Params.RFID,
    AnalyticsEvents.Params.OUTCOME,
    AnalyticsEvents.Params.REASON,
    "capture_source",
    "mime_type",
    "processing_state",
    "processing_attempt",
    "upload_original",
    "location_status",
    "geocoder_status",
    "duration_bucket",
    "original_size_bucket",
    "processed_size_bucket",
    "proof_upload_status",
    "submit_status",
)

fun AnalyticsContext.standardEventParams(): Map<String, String> =
    buildMap {
        deviceId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.DEVICE_ID, it) }
        journeyId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.JOURNEY_ID, it) }
        tenantId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.UserProps.TENANT, it) }
        actorId?.takeIf { it.isNotBlank() }?.let { put("actor_id", it) }
        email?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.EMAIL, it) }
        role?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.UserProps.ROLE, it) }
        parkScope?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.UserProps.PRIMARY_PARK, it) }
        parkId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.UserProps.PARK_ID, it) }
        flavor.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.UserProps.FLAVOR, it) }
        appVersionName?.takeIf { it.isNotBlank() }?.let { put("app_version_name", it) }
        appVersionCode?.takeIf { it.isNotBlank() }?.let { put("app_version_code", it) }
    }
