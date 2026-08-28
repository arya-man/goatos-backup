package sg.mesha.goatos.core.analytics

/**
 * Clock In / Clock Out analytics event constants (module clock, maintainer decision 2026-08-27 —
 * docs/features/clock-in-out/plan.md §4.5). Kept in a SEPARATE object for the same reason
 * [AnalyticsEventsToxin] is: the clock slice can grow its own instrumentation without touching
 * the shared file every other module's telemetry lives in. Every name is `snake_case` and
 * `clock_`-prefixed; params reuse [AnalyticsEvents.Params] keys wherever one fits.
 *
 * Why these exist: attendance only works if everyone actually punches, so the funnel that
 * matters is banner_shown → banner_tapped → in_attempted → in_succeeded — a person who sees the
 * reminder daily but never lands a clock-in is either blocked (refused_mock) or ignoring it, and
 * only these events tell those two stories apart.
 */
object AnalyticsEventsClock {
    /** A punch tap started: the device-fact capture + mock scan is running. */
    const val CLOCK_IN_ATTEMPTED = "clock_in_attempted"

    /** The clock-in write became DURABLE on the outbox (the moment the person's day is safe). */
    const val CLOCK_IN_SUCCEEDED = "clock_in_succeeded"

    /** The client-side mock-location gate refused the punch (mock fix or installed fake-GPS app). */
    const val CLOCK_IN_REFUSED_MOCK = "clock_in_refused_mock"

    /** A clock-out tap started. */
    const val CLOCK_OUT_ATTEMPTED = "clock_out_attempted"

    /** The clock-out write became DURABLE on the outbox. */
    const val CLOCK_OUT_SUCCEEDED = "clock_out_succeeded"

    /** The shell-global not-clocked-in reminder banner became visible. */
    const val CLOCK_BANNER_SHOWN = "clock_banner_shown"

    /** The reminder banner was tapped (navigates to the clock module). */
    const val CLOCK_BANNER_TAPPED = "clock_banner_tapped"

    /** The leadership presence board (Team page) was opened. */
    const val CLOCK_TEAM_OPENED = "clock_team_opened"
}
