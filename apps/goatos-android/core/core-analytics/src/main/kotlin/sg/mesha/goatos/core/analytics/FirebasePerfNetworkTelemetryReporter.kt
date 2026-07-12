package sg.mesha.goatos.core.analytics

import com.google.firebase.perf.FirebasePerformance
import sg.mesha.goatos.core.network.NetworkTelemetryEvent
import sg.mesha.goatos.core.network.NetworkTelemetryReporter

/**
 * Reports `TelemetryInterceptor`'s per-call telemetry (`core-network`) as a Firebase Performance
 * custom trace, IN ADDITION to Firebase Perf's own automatic OkHttp network instrumentation
 * (enabled by the `com.google.firebase.firebase-perf` Gradle plugin with no code — see
 * `docs/TELEMETRY.md`). The automatic instrumentation reports the raw URL; this custom trace adds
 * the bounded-cardinality ROUTE TEMPLATE and status class as attributes, which the automatic
 * instrumentation cannot express.
 *
 * TODO(otel-otlp): replace/augment with a real OTLP span exporter — see `TelemetryInterceptor`'s
 * TODO in `core-network`.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
class FirebasePerfNetworkTelemetryReporter : NetworkTelemetryReporter {
    override fun onNetworkCall(event: NetworkTelemetryEvent) {
        val trace = FirebasePerformance.getInstance().newTrace(TRACE_NAME)
        trace.start()
        trace.putAttribute("method", event.method)
        trace.putAttribute("route", event.route)
        trace.putAttribute("status_class", statusClass(event.statusCode))
        trace.putMetric("duration_ms", event.durationMs)
        trace.stop()
    }

    private fun statusClass(code: Int): String = when {
        code < 0 -> "error"
        code < 300 -> "2xx"
        code < 400 -> "3xx"
        code < 500 -> "4xx"
        else -> "5xx"
    }

    companion object {
        private const val TRACE_NAME = "network_call"
    }
}
