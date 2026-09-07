package sg.mesha.goatos.core.analytics

/**
 * Pen-visit analytics event constants (maintainer decision 2026-09-07).
 *
 * Kept in a SEPARATE object like [AnalyticsEventsLeadershipTasks] so this tab's instrumentation
 * can grow without touching the shared file. Every name is `snake_case` and `pen_visit_`-
 * prefixed; params reuse [AnalyticsEvents.Params] keys wherever one fits.
 *
 * Why these exist: a visit the kernel raised and the park head never opened is invisible from
 * any single screen. The funnel list_viewed -> task_opened -> capture_started -> capture_result
 * -> room_written -> upload_enqueued -> submitted says where the loop breaks, and every failure
 * pairs with a `CrashReporter.recordException` on the same path.
 */
object AnalyticsEventsPenVisits {
    /** The "For me" list (the tab's L0 route) was opened or resumed. */
    const val LIST_VIEWED = "pen_visit_list_viewed"

    /** A visit card was tapped and its detail opened. */
    const val TASK_OPENED = "pen_visit_task_opened"

    /** The in-app camera was opened for a visit video. */
    const val CAPTURE_STARTED = "pen_visit_capture_started"

    /** The recorder returned; [AnalyticsEvents.Params.RESULT] is `recorded` or `cancelled`. */
    const val CAPTURE_RESULT = "pen_visit_capture_result"

    /** The durable proof row reached Room. */
    const val ROOM_WRITTEN = "pen_visit_room_written"

    /** The proof upload was queued on the visit's outbox lane. */
    const val UPLOAD_ENQUEUED = "pen_visit_upload_enqueued"

    /** The submit was queued behind the upload — the visit is on its way. */
    const val SUBMITTED = "pen_visit_submitted"

    /** A durable video was found after process death and its missing submit row was re-enqueued. */
    const val SUBMIT_RECOVERED = "pen_visit_submit_recovered"

    /** The park head played, paused, expanded or shared the recorded clip, or its playback failed. */
    const val PROOF_PREVIEW_ACTION = "pen_visit_proof_preview_action"

    /**
     * Any read/capture/write path failed. [AnalyticsEvents.Params.REASON] carries the real
     * message the layer returned, truncated — never a fabricated code. Paired with a
     * `CrashReporter.recordException` on the same path.
     */
    const val FAILURE = "pen_visit_failure"
}
