package sg.mesha.goatos.core.analytics

/**
 * Pen-routine analytics event constants (maintainer instruction 2026-09-16,
 * docs/decisions/pen-routines.md).
 *
 * Kept in a SEPARATE object like [AnalyticsEventsPenVisits] so this module's instrumentation can
 * grow without touching the shared file. Every name is `snake_case` and `pen_routine_`-prefixed;
 * params reuse [AnalyticsEvents.Params] keys wherever one fits.
 *
 * Why these exist: a routine task the kernel raised and the park head never opened is invisible
 * from any single screen. The funnel list_opened -> detail_opened -> check_in -> capture_result
 * -> submit says where the loop breaks, and every failure pairs with a
 * `CrashReporter.recordException` on the same path.
 */
object AnalyticsEventsPenRoutines {
    /** The Routines list (the module's L0 route) was opened or resumed. */
    const val LIST_OPENED = "pen_routine_list_opened"

    /** A routine card was tapped and its detail opened. */
    const val DETAIL_OPENED = "pen_routine_detail_opened"

    /** The park head tapped "Check in to pen"; [AnalyticsEvents.Params.RESULT] says what happened. */
    const val CHECK_IN = "pen_routine_check_in"

    /** The in-app camera returned for a slot; [AnalyticsEvents.Params.RESULT] is `recorded` or `cancelled`. */
    const val CAPTURE_RESULT = "pen_routine_capture_result"

    /** The submit was queued behind the uploads — the task is on its way. */
    const val SUBMIT = "pen_routine_submit"

    /** A submit could not be queued, or the server definitively refused it. */
    const val SUBMIT_FAILED = "pen_routine_submit_failed"

    /**
     * Any read/capture/write path failed. [AnalyticsEvents.Params.REASON] carries the real
     * message the layer returned, truncated — never a fabricated code. Paired with a
     * `CrashReporter.recordException` on the same path.
     */
    const val FAILURE = "pen_routine_failure"

    /**
     * A filter changed on a routine list (maintainer instruction 2026-10-01): [AnalyticsEvents.Params.KIND]
     * is `status` | `date` | `pen`, and [Params.TAB_KEY] names the web-authored tab when there is
     * one. Never the chosen value — which pen or which dates is not needed to see whether the
     * filters are used.
     */
    const val FILTER_CHANGED = "pen_routine_filter_changed"

    object Params {
        /** The web-authored phone tab's key (`^[a-z][a-z0-9_]{1,39}$`, a bounded vocabulary). */
        const val TAB_KEY = "tab_key"
    }
}
