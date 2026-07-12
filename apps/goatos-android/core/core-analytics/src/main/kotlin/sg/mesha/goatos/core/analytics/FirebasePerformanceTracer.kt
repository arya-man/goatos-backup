package sg.mesha.goatos.core.analytics

import com.google.firebase.perf.FirebasePerformance
import com.google.firebase.perf.metrics.Trace

/**
 * Real [PerformanceTracer] backed by Firebase Performance custom traces. Bound in place of
 * [NoopPerformanceTracer] for flavors with `BuildConfig.TELEMETRY_ENABLED = true` (see
 * `di/TelemetryModule.kt` in `:app`).
 */
class FirebasePerformanceTracer : PerformanceTracer {
    override fun startTrace(name: String): TraceHandle {
        val trace = FirebasePerformance.getInstance().newTrace(name)
        trace.start()
        return FirebaseTraceHandle(trace)
    }
}

private class FirebaseTraceHandle(private val trace: Trace) : TraceHandle {
    override fun putMetric(name: String, value: Long) {
        trace.putMetric(name, value)
    }

    override fun putAttribute(name: String, value: String) {
        trace.putAttribute(name, value)
    }

    override fun stop() {
        trace.stop()
    }
}
