package sg.mesha.goatos.core.analytics

/**
 * Toxin-module analytics event/param constants (module toxin, maintainer decision 2026-08-25 —
 * `docs/decisions/toxin-testing-module.md`).
 *
 * Kept in a SEPARATE object for the same reason [AnalyticsEventsWeighing] is: the toxin slice can
 * grow its own instrumentation without touching the shared file every other module's telemetry
 * lives in. Every name is `snake_case` and `toxin_`-prefixed (never a bare Firebase-reserved
 * name), and every param reuses an existing key from [AnalyticsEvents.Params] wherever one fits;
 * only genuinely new params are declared here.
 *
 * Why each of these exists: the aflatoxin test is a SEVEN-step chain with two server-clock waits
 * inside it, worked by whoever is free rather than one person start-to-finish. A round that
 * stalls is therefore invisible from any single screen — only the funnel across these events says
 * whether a round died at "sample taken" or at the strip reading.
 */
object AnalyticsEventsToxin {
    /** The toxin test-task list (the module's L0 route) was opened or resumed. */
    const val TOXIN_LIST_VIEWED = "toxin_list_viewed"

    /** A task row was tapped and the guided 7-step drill opened. */
    const val TOXIN_TASK_OPENED = "toxin_task_opened"

    /**
     * One guided STEP's video was recorded and durably captured — before the step-completion
     * write drains. [Params.STEP_NO] carries which step, so the funnel shows exactly where rounds
     * stop.
     */
    const val TOXIN_STEP_VIDEO_CAPTURED = "toxin_step_video_captured"

    /** Step 7's strip PHOTO was taken and durably captured (the reading is not sent yet). */
    const val TOXIN_STRIP_PHOTO_CAPTURED = "toxin_strip_photo_captured"

    /**
     * A step completion was durably queued on the outbox — the operator's work for that step is
     * recorded even offline. [Params.STEP_NO] carries the step.
     */
    const val TOXIN_STEP_SUBMITTED = "toxin_step_submitted"

    /**
     * Step 7's reading was durably queued. [AnalyticsEvents.Params.OUTCOME] carries the
     * BACKEND-OWNED outcome value the operator picked (never a client-invented token), because an
     * invalid strip cancels the round and mints a retest — the one outcome that costs the farm a
     * whole second test.
     */
    const val TOXIN_READING_SUBMITTED = "toxin_reading_submitted"

    /**
     * Any toxin capture/queue path failed. [AnalyticsEvents.Params.REASON] carries the real
     * message the repository/capture layer returned, truncated like every other reason field —
     * never a fabricated code. Paired with a `CrashReporter.recordException` on the same path.
     */
    const val TOXIN_FAILURE = "toxin_failure"

    object Params {
        /** Which of the seven guided steps an event refers to, as its `step_no`. */
        const val STEP_NO = "step_no"
    }
}
