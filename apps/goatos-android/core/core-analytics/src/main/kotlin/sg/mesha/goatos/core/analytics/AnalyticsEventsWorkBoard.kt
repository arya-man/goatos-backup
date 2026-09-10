package sg.mesha.goatos.core.analytics

/**
 * Work Board (My Work) analytics event constants (maintainer decision 2026-09-10).
 *
 * Kept in a SEPARATE object like [AnalyticsEventsPenVisits] so this screen's instrumentation can
 * grow without touching the shared file. Every name is `snake_case` and `work_board_`-prefixed;
 * params reuse [AnalyticsEvents.Params] keys wherever one fits.
 *
 * Why these exist: the board is the one place a person sees every module's work at once, so a
 * board nobody opens, a filter nobody uses, or a row nobody drills into says the cross-module
 * view is not earning its place. Every failure pairs with a `CrashReporter.recordException`.
 */
object AnalyticsEventsWorkBoard {
    /** The My Work list (`/work`) was opened or resumed and a refresh fired. */
    const val VIEWED = "work_board_viewed"

    /** A lane / module / day filter changed; [AnalyticsEvents.Params.DIMENSION] names which. */
    const val FILTER_APPLIED = "work_board_filter_applied"

    /** A board row was tapped and its detail opened. */
    const val ROW_OPENED = "work_board_row_opened"

    /** The detail's "Open" button sent the reader to the module's own screen. */
    const val ROW_FOLLOWED = "work_board_row_followed"

    /**
     * A read failed (summary refresh or a page load). [AnalyticsEvents.Params.REASON] carries the
     * real message the layer returned, truncated — never a fabricated code. Paired with a
     * `CrashReporter.recordException` on the same path.
     */
    const val FAILURE = "work_board_failure"
}
