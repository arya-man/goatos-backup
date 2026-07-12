package sg.mesha.goatos.core.analytics

// Analytics/telemetry port. Real impl (event egress) lands in a later, Firebase-gated pass; the
// no-op keeps callers decoupled from any vendor SDK.

/**
 * The single seam between product code and the analytics vendor.
 *
 * Two distinct concerns:
 *  - [track] records an ACTION (an event with per-occurrence params).
 *  - [setUserProperty] sets a durable PRINCIPAL attribute (role, park, flavor…) that the egress
 *    impl stamps onto every subsequent event. Passing `null` clears the property.
 *
 * Call sites use the [AnalyticsEvents] constants for names — never inline strings — so a typo can
 * never silently split a funnel.
 */
interface AnalyticsPort {
    fun track(event: String, props: Map<String, String> = emptyMap())

    fun setUserProperty(name: String, value: String?)
}

/** Wired today (real egress is a later pass). Deliberately does nothing so callers stay decoupled
 *  from any vendor SDK and analytics is never on a critical path. */
class NoopAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
}
