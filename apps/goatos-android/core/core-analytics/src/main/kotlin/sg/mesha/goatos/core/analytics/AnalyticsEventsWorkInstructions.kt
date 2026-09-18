package sg.mesha.goatos.core.analytics

/**
 * Work-instructions analytics event constants (SOP studio phase 2, maintainer decision
 * 2026-09-18, docs/decisions/sop-studio.md): the phone module that starts a GENERAL SOP run.
 *
 * Separate object like [AnalyticsEventsPenRoutines]. `snake_case`, `work_instruction_`-prefixed;
 * params reuse [AnalyticsEvents.Params] keys. The funnel list_opened -> start -> (the run's own
 * workflow events) says whether a general SOP is ever started at all.
 */
object AnalyticsEventsWorkInstructions {
    /** The Work instructions list (the module's L0 route) was opened or resumed. */
    const val LIST_OPENED = "work_instruction_list_opened"

    /** A run was started; [AnalyticsEvents.Params.RESULT] is `started` or the failure kind. */
    const val START = "work_instruction_start"

    /** Any read/write path failed; [AnalyticsEvents.Params.REASON] carries the real message, truncated. */
    const val FAILURE = "work_instruction_failure"
}
