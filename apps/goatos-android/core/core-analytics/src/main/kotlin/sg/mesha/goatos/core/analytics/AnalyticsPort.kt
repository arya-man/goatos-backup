package sg.mesha.goatos.core.analytics

// Analytics/telemetry port. Real impl (event egress) lands in a later pass; the
// no-op keeps callers decoupled from any vendor SDK.

interface AnalyticsPort {
    fun track(event: String, props: Map<String, String> = emptyMap())
}

class NoopAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
}
