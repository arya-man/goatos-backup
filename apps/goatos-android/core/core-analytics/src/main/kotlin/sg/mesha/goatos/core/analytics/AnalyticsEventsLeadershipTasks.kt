package sg.mesha.goatos.core.analytics

/**
 * Leadership Tasks analytics event constants (maintainer request 2026-09-04).
 *
 * Kept in a SEPARATE object like [AnalyticsEventsToxin] so this module's instrumentation can
 * grow without touching the shared file. Every name is `snake_case` and `leadership_task_`-
 * prefixed; params reuse [AnalyticsEvents.Params] keys wherever one fits.
 *
 * Why these exist: a task a director raised and a CXO never saw is invisible from any single
 * screen. The funnel raised -> opened -> status_changed says whether the loop closes, and the
 * attachment events say which kinds of brief (voice, photo, file) leadership actually uses.
 */
object AnalyticsEventsLeadershipTasks {
    /** The task list (the module's L0 route) was opened or resumed. */
    const val LIST_VIEWED = "leadership_task_list_viewed"

    /** A task card was tapped and its detail opened. */
    const val TASK_OPENED = "leadership_task_task_opened"

    /** A new task reached the server. [AnalyticsEvents.Params.COUNT] carries its attachment count. */
    const val TASK_RAISED = "leadership_task_task_raised"

    /** An existing task's title/body/attachments were saved. */
    const val TASK_EDITED = "leadership_task_task_edited"

    /** The task moved; [AnalyticsEvents.Params.STATUS] carries the backend status key. */
    const val STATUS_CHANGED = "leadership_task_status_changed"

    /** An attachment was added to a draft; [AnalyticsEvents.Params.KIND] carries its kind. */
    const val ATTACHMENT_ADDED = "leadership_task_attachment_added"

    /** A voice note was recorded in the app and kept on the draft. */
    const val AUDIO_RECORDED = "leadership_task_audio_recorded"

    /**
     * Any read/write path failed. [AnalyticsEvents.Params.REASON] carries the real message the
     * layer returned, truncated — never a fabricated code. Paired with a
     * `CrashReporter.recordException` on the same path.
     */
    const val FAILURE = "leadership_task_failure"
}
