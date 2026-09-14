package sg.mesha.goatos.core.analytics

/**
 * Market survey analytics event constants (maintainer decision 2026-09-14).
 *
 * Kept in a SEPARATE object like [AnalyticsEventsToxin] and [AnalyticsEventsAnimalPurchase], so the
 * slice can grow its own instrumentation without touching the shared file. Every name is
 * `snake_case` and `market_`-prefixed, and every param reuses a key from [AnalyticsEvents.Params].
 *
 * Why these exist: the reporter is nudged at 08:00 and has a handful of cards to fill; a morning
 * where the list was opened but no card was saved, or a save that never left the outbox, is
 * invisible from any single screen -- only this funnel says where the calls stopped.
 */
object AnalyticsEventsMarket {
    /** The day's city cards (the tab's L0 route) were opened or resumed. */
    const val DAY_VIEWED = "market_day_viewed"

    /** A city card was tapped and its entry form opened. */
    const val CITY_OPENED = "market_city_opened"

    /** A city's prices were durably queued on the outbox -- recorded even offline. */
    const val CITY_QUEUED = "market_city_queued"

    /** A queued save reached a terminal outcome (saved, still queued, rejected). */
    const val WRITE_OUTCOME = "market_write_outcome"

    /** A read refresh or an enqueue failed on the phone (the reason rides as a param). */
    const val FAILURE = "market_failure"
}
