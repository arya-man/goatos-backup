package sg.mesha.goatos.core.analytics

import android.util.Log
import sg.mesha.goatos.core.network.NetworkTelemetryEvent
import sg.mesha.goatos.core.network.NetworkTelemetryReporter

/**
 * Turns a FAILED backend call into signal, at the ONE place every call already passes through:
 * `TelemetryInterceptor`'s reporter seam.
 *
 * The night a CEO tapped Publish and saw a bare `HTTP 409 Conflict`, the only evidence anywhere
 * was a server access-log line. Nothing reached Crashlytics, nothing reached Analytics, and
 * nothing reached logcat — because the only existing reporter
 * ([FirebasePerfNetworkTelemetryReporter]) records a *timing* trace, and a 409 that returns in
 * 80 ms is a perfectly healthy-looking timing trace. Failure was never its job.
 *
 * This decorator adds the failure half, and deliberately does it here rather than at ~200 call
 * sites: a per-ViewModel `recordException` can be forgotten by the next screen someone writes,
 * an interceptor cannot.
 *
 * Four outputs per failure, each with a different reader:
 *  - **logcat** (always, every flavor, even with telemetry disabled) — so a developer watching
 *    `adb logcat` during a retry storm SEES the storm. This is the half that was missing when
 *    ~20 HTTP 500s produced silence on-device.
 *  - **Crashlytics breadcrumb** (every failure) — so whatever crash or non-fatal lands next
 *    carries the preceding request failures as context.
 *  - **Crashlytics non-fatal** (throttled, see below) — an issue that shows up in the console
 *    without needing a crash.
 *  - **Analytics `api_call_failure` event** (every failure) — the aggregate view: which routes
 *    and which codes users are actually hitting.
 *
 * ### Throttling
 * A retry storm is 20 identical failures in seconds. All 20 are worth a breadcrumb, a logcat
 * line, and an event (the COUNT is the signal). They are not worth 20 Crashlytics non-fatals —
 * that buries the console and burns quota. Non-fatals are therefore throttled to one per
 * `(method, route, status)` per [NON_FATAL_THROTTLE_MS]; the storm still shows up as 20 events
 * and 20 breadcrumbs attached to that single issue.
 *
 * ### What never crosses this seam
 * Method, route TEMPLATE (ids already replaced with `{id}` upstream), status and duration only.
 * Never the Authorization header, never an FCM token, never a request or response body. Goat
 * RFIDs/tags would be permissible (livestock data, not PII) but simply are not present here —
 * the route template has already removed them.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
class FailureReportingNetworkTelemetryReporter(
    private val delegate: NetworkTelemetryReporter,
    private val crashReporter: CrashReporter,
    private val analytics: AnalyticsPort,
    private val nowMs: () -> Long = { System.currentTimeMillis() },
    private val logLine: (String) -> Unit = { Log.w(TAG, it) },
) : NetworkTelemetryReporter {

    private val lastNonFatalMs = mutableMapOf<String, Long>()

    override fun onNetworkCall(event: NetworkTelemetryEvent) {
        // The delegate (Firebase Perf) runs for EVERY call, success or failure, and must not be
        // skipped by a throw from the failure path below.
        runCatching { delegate.onNetworkCall(event) }
        if (!isFailure(event.statusCode)) return
        runCatching { report(event) }
    }

    private fun report(event: NetworkTelemetryEvent) {
        val summary = summarize(event)
        logLine(summary)
        crashReporter.log(summary)
        analytics.track(
            AnalyticsEvents.API_CALL_FAILURE,
            mapOf(
                AnalyticsEvents.Params.METHOD to event.method,
                AnalyticsEvents.Params.ROUTE to event.route,
                AnalyticsEvents.Params.STATUS_CODE to event.statusCode.toString(),
                AnalyticsEvents.Params.DURATION_MS to event.durationMs.toString(),
            ),
        )
        if (shouldRecordNonFatal(event)) {
            crashReporter.recordException(ApiCallFailure(summary), summary)
        }
    }

    private fun shouldRecordNonFatal(event: NetworkTelemetryEvent): Boolean {
        val key = "${event.method} ${event.route} ${event.statusCode}"
        val now = nowMs()
        val previous = lastNonFatalMs[key]
        if (previous != null && now - previous < NON_FATAL_THROTTLE_MS) return false
        lastNonFatalMs[key] = now
        return true
    }

    /** `-1` means the call threw before any response; anything >= 400 is a refusal or fault. */
    private fun isFailure(statusCode: Int): Boolean = statusCode < 0 || statusCode >= 400

    private fun summarize(event: NetworkTelemetryEvent): String {
        val status = if (event.statusCode < 0) "no_response" else event.statusCode.toString()
        return "api_call_failure ${event.method} ${event.route} status=$status " +
            "duration_ms=${event.durationMs} traceparent=${event.traceparent}"
    }

    /**
     * Distinct exception type so these group as their own Crashlytics issue rather than merging
     * into whatever generic `IOException` a call site happened to throw.
     */
    class ApiCallFailure(message: String) : RuntimeException(message)

    companion object {
        private const val TAG = "GoatOsNetwork"

        /** One non-fatal per `(method, route, status)` per minute — see "Throttling" above. */
        const val NON_FATAL_THROTTLE_MS: Long = 60_000
    }
}
