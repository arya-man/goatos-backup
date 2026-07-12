package sg.mesha.goatos.core.analytics

/**
 * Custom-trace seam on top of Firebase Performance's automatic screen + network traces (the
 * automatic traces need no code — they come from the `com.google.firebase.firebase-perf` Gradle
 * plugin's bytecode instrumentation once applied). Use [PerformanceTracer] only for a span that
 * automatic instrumentation can't express, e.g. app cold-start (`GoatOsApplication.onCreate` →
 * `MainActivity.onResume`, see [PerformanceTraceNames.APP_COLD_START]).
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
interface PerformanceTracer {
    /** Starts (and returns a handle for) a named custom trace. Call [TraceHandle.stop] exactly
     *  once — an un-stopped trace never reports. */
    fun startTrace(name: String): TraceHandle
}

/** A single in-flight custom trace. */
interface TraceHandle {
    fun putMetric(name: String, value: Long)
    fun putAttribute(name: String, value: String)
    fun stop()
}

/** Fallback for tests/local/dev — never touches a vendor SDK, never throws. */
class NoopPerformanceTracer : PerformanceTracer {
    override fun startTrace(name: String): TraceHandle = NoopTraceHandle
}

private object NoopTraceHandle : TraceHandle {
    override fun putMetric(name: String, value: Long) {}
    override fun putAttribute(name: String, value: String) {}
    override fun stop() {}
}

/** Canonical custom-trace names — central so no call site hand-rolls a string. */
object PerformanceTraceNames {
    /** Started in `GoatOsApplication.onCreate`, stopped on the first `MainActivity.onResume`
     *  after a session is authed — approximates cold start to first usable frame. */
    const val APP_COLD_START = "app_cold_start"
}
