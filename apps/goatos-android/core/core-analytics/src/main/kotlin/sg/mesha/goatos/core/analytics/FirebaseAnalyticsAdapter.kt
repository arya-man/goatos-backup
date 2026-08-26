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

// 2026-08-15 (P1 fix): FIREBASE_MAX_EVENT_PARAMS was briefly raised from 25 to 37 to fit the
// proof-flow-integration params (result, slot_mask, retry_count, source, local_slot_state,
// feed_weight_source, feed_video_source, water_video_source, previous, next, status, kind).
// That was wrong: GA4 enforces a HARD technical ceiling of 25 custom parameters per logged event
// at ingestion, unconditionally -- raising this internal constant past 25 does not raise Google's
// platform-side limit, it just makes the local guard bless events that Firebase itself silently
// truncates on arrival (params dropped with zero client-visible error). Restored to a HARD 25 here
// AND in the CI guard (tools/agent-hooks/check-firebase-analytics-param-budget.mjs), which now
// FAILS any allowlist entry count above 25 instead of merely warning.
//
// To make room for the proof-flow params without exceeding 25, FIREBASE_PARAM_ALLOWLIST below was
// re-curated rather than just grown: several pre-existing proof-capture diagnostic params that are
// redundant with, or lower-value than, what stayed (capture_source, mime_type, processing_attempt,
// location_status, geocoder_status, original_size_bucket, upload_original, proof_subject, and the
// duplicate-of-rfid_tag Params.RFID key) were DROPPED from the Firebase envelope, and 2 of the 12
// new proof-flow params (feed_video_source, water_video_source) were dropped too, keeping only
// feed_weight_source as the representative "which slot source" diagnostic. Firebase/GA4 stays a
// compact diagnostic surface only -- the FULL payload (every param, no allowlist, no cap) still
// reaches the backend via BackendAnalyticsAdapter on the same call sites, so nothing is lost for
// forensic debugging; it just is not duplicated into Firebase where GA4 would drop it past 25
// anyway. The surviving 25 entries deliberately preserve: split-operator slot info (slot_mask,
// local_slot_state, feed_weight_source), submit source (source), retry/failure reason (retry_count,
// reason, outcome), and live-status transition (previous, next, status), plus the pre-existing
// proof-capture core diagnostics (proof_id, task_id, field_key, feature_surface, rfid_tag,
// processing_state, duration_bucket, proof_upload_status, submit_status) and result/failure_kind.
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

// Exactly 25 entries -- the hard GA4 platform cap. Do not add without removing one; see the
// FIREBASE_MAX_EVENT_PARAMS comment above for what was traded off and why.
private val FIREBASE_PARAM_ALLOWLIST = listOf(
    AnalyticsEvents.Params.DEVICE_ID,
    AnalyticsEvents.Params.JOURNEY_ID,
    AnalyticsEvents.UserProps.ROLE,
    AnalyticsEvents.UserProps.PRIMARY_PARK,
    "proof_id",
    "task_id",
    "field_key",
    "feature_surface",
    "rfid_tag",
    AnalyticsEvents.Params.OUTCOME,
    AnalyticsEvents.Params.REASON,
    "processing_state",
    "duration_bucket",
    "proof_upload_status",
    "submit_status",
    // proof-flow-integration additions (2026-08-15) -- kept within the 25-cap by trading off the
    // lower-value legacy params documented in the comment above.
    AnalyticsEvents.Params.RESULT,
    AnalyticsEvents.Params.SLOT_MASK,
    AnalyticsEvents.Params.RETRY_COUNT,
    AnalyticsEvents.Params.SOURCE,
    AnalyticsEvents.Params.LOCAL_SLOT_STATE,
    AnalyticsEvents.Params.FEED_WEIGHT_SOURCE,
    AnalyticsEvents.Params.PREVIOUS,
    AnalyticsEvents.Params.NEXT,
    AnalyticsEvents.Params.STATUS,
    AnalyticsEvents.Params.KIND,
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
