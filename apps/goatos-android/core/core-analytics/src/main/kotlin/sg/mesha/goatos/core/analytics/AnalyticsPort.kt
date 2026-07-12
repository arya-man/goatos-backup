package sg.mesha.goatos.core.analytics

// Analytics/telemetry port. Real impl (event egress) lands in a later, Firebase-gated pass; the
// no-op keeps callers decoupled from any vendor SDK.

/**
 * The single seam between product code and the analytics vendor.
 *
 * Three distinct concerns:
 *  - [track] records an ACTION (an event with per-occurrence params).
 *  - [setUserProperty] sets a durable PRINCIPAL attribute (role, park, flavor…) that the egress
 *    impl stamps onto every subsequent event. Passing `null` clears the property.
 *  - [setUserId] sets the durable PRINCIPAL identifier itself (the stable, non-PII workforce
 *    member id — never a name/email/phone). Passing `null` clears it (logout clean-slate).
 *
 * Call sites use the [AnalyticsEvents] constants for names — never inline strings — so a typo can
 * never silently split a funnel.
 */
interface AnalyticsPort {
    fun track(event: String, props: Map<String, String> = emptyMap())

    fun setUserProperty(name: String, value: String?)

    fun setUserId(id: String?)
}

/** Test/preview fallback. Deliberately does nothing so callers stay decoupled from any vendor
 *  SDK — the real egress binding is [FirebaseAnalyticsAdapter] (bound only for flavors with a
 *  confirmed Firebase project; see `di/AnalyticsModule.kt` in `:app`). */
class NoopAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}
