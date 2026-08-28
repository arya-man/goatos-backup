package sg.mesha.goatos.core.analytics

/**
 * Weighing-module analytics event/param constants that do not already live on [AnalyticsEvents].
 *
 * Kept in a SEPARATE object, not folded into [AnalyticsEvents], so the weighing feature slice can
 * grow its own instrumentation without touching the shared file every other module's telemetry
 * also lives in — the same reasoning [AnalyticsFunnels] already applies to derived funnel events.
 * Every name is `snake_case`, `weighing_`-prefixed (never a bare Firebase-reserved name — see
 * [AnalyticsEvents.SESSION_START]'s own doc for why that matters), and every param reuses an
 * existing key from [AnalyticsEvents.Params] wherever one already fits; only genuinely new params
 * are declared here.
 */
object AnalyticsEventsWeighing {
    /**
     * The operator attempted the scope-level Submit action on an individual-animal weighing shed
     * (every scanned RFID has a saved weight and a synced video, and the confirm sheet was
     * accepted) — before the outbox call resolves. Distinct from [AnalyticsEvents.WEIGHING_CAPTURE_ATTEMPT],
     * which fires per-ROW as each animal's weight is saved; this is the single shed-level write
     * that closes the scope out.
     */
    const val WEIGHING_SUBMIT_ATTEMPTED = "weighing_submit_attempted"

    /** The operator opened the final weighing submit confirmation. */
    const val WEIGHING_SUBMIT_CONFIRMATION_OPENED = "weighing_submit_confirmation_opened"

    /** The operator dismissed the final weighing submit confirmation without submitting. */
    const val WEIGHING_SUBMIT_CONFIRMATION_CANCELLED = "weighing_submit_confirmation_cancelled"

    /** The operator confirmed the final weighing submit confirmation. */
    const val WEIGHING_SUBMIT_CONFIRMATION_CONFIRMED = "weighing_submit_confirmation_confirmed"

    /** The scope-level Submit call succeeded. */
    const val WEIGHING_SUBMIT_SUCCEEDED = "weighing_submit_succeeded"

    /** The scope-level Submit call failed. [AnalyticsEvents.Params.REASON] carries the real
     *  message the repository/backend returned, truncated like every other reason field in this
     *  ViewModel — never a fabricated code the domain result does not actually expose. */
    const val WEIGHING_SUBMIT_FAILED = "weighing_submit_failed"

    /**
     * The planner's authoring wizard rendered a NEW step — fires once per step transition
     * (forward or back), never once per recomposition. [Params.WIZARD_STEP] names the step;
     * [AnalyticsEvents.Params.CATEGORY] carries `create`/`edit` so a locked edit's 3-step flow is
     * distinguishable from a create's full 5-step one (see [WeighingWizardStep] and the
     * edit-mode step remap in `WeighingPlanWizardViewModel.toUiState`).
     */
    const val WEIGHING_PLAN_WIZARD_STEP_REACHED = "weighing_plan_wizard_step_reached"

    /**
     * The wizard's ViewModel was cleared (the planner left the screen — back, app switch, process
     * death) with a step reached beyond the first AND no [AnalyticsEvents.WEIGHING_PLAN_SAVE_SUCCEEDED]
     * ever fired for this instance — real authoring progress that was never saved. Never fired for
     * a wizard that never left its first step (nothing was started) or one that already saved.
     * [Params.WIZARD_STEP] carries the LAST step reached, so a funnel can tell where a planner
     * actually gives up.
     */
    const val WEIGHING_PLAN_WIZARD_ABANDONED = "weighing_plan_wizard_abandoned"

    /**
     * The operator asked to retry, remove, or (re)capture a shed-level group video proof —
     * fired before the repository call resolves. [AnalyticsEvents.Params.CATEGORY] carries which
     * action (`retry`/`remove`/`capture`), so the three share one funnel while staying
     * distinguishable. Added because these three actions previously set [WeighingViewModel]'s
     * `message` on failure and emitted NOTHING to telemetry — an operator whose group-video retry
     * kept failing was invisible in the dashboard, the exact blindness class
     * [AnalyticsEvents.WEIGHING_PROOF_UPLOAD_FAILED]'s own doc already called out for the outbox
     * side of the same proof.
     */
    const val WEIGHING_SHED_VIDEO_ACTION_ATTEMPTED = "weighing_shed_video_action_attempted"

    /** The retry/remove/capture call above succeeded. */
    const val WEIGHING_SHED_VIDEO_ACTION_SUCCEEDED = "weighing_shed_video_action_succeeded"

    /** The retry/remove/capture call above failed. [AnalyticsEvents.Params.REASON] carries the
     *  real message the repository/domain result returned, truncated like every other reason
     *  field in [WeighingViewModel] — never a bare "failed". */
    const val WEIGHING_SHED_VIDEO_ACTION_FAILED = "weighing_shed_video_action_failed"

    /** A synced per-animal weighing proof had no draft proof id but was recovered by its RFID tag. */
    const val WEIGHING_ORPHAN_SYNCED_PROOF_RECOVERED = "weighing_orphan_synced_proof_recovered"

    /** A synced per-animal proof could not be attached because the local observation row was missing. */
    const val WEIGHING_PROOF_ATTACH_NO_OBSERVATION = "weighing_proof_attach_no_observation"

    /** A per-animal proof attached locally, but the durable observation outbox enqueue failed. */
    const val WEIGHING_OBSERVATION_ENQUEUE_FAILED = "weighing_observation_enqueue_failed"

    object Params {
        /** Which wizard step an event refers to (`date`/`park`/`buckets`/`configure`/`review`),
         *  lowercase of the [WeighingWizardStep] enum name. */
        const val WIZARD_STEP = "wizard_step"

        /** The scanned livestock RFID/tag associated with a per-animal weighing proof. */
        const val RFID = "rfid"

        /** Server proof_artifacts.proof_id, used to join app telemetry to backend proof rows. */
        const val SERVER_PROOF_ID = "server_proof_id"

        /** Count of rows currently visible in the operator's weighing scan screen. */
        const val VISIBLE_ROW_COUNT = "visible_row_count"

        /** Count of local scan-capture rows observed on this phone for the weighing scope. */
        const val SCANNED_ROW_COUNT = "scanned_row_count"

        /** Count of visible rows that already have saved weight and synced proof. */
        const val READY_VISIBLE_ROW_COUNT = "ready_visible_row_count"

        /** Count of ready server/cache drafts paired to identifiers considered for submit. */
        const val PAIRED_DRAFT_COUNT = "paired_draft_count"

        /** Count of identifiers the submit gate considers fully ready. */
        const val SUBMIT_READY_IDENTIFIER_COUNT = "submit_ready_identifier_count"
    }

    /**
     * The weight field could not take focus immediately after the camera returned, so the operator
     * sat in front of a focused-looking field with no keyboard.
     *
     * Measured on a physical device 2026-08-08: WindowManager withheld INPUT focus from the app for
     * ~10s after the in-process CameraX surface tore down, so every showSoftInput() before that was
     * accepted by the IME service and dropped ("getSurroundingText on inactive InputConnection").
     * [Params.DURATION_MS] carries how long the retry took to win focus, so this is measurable in
     * the field across every operator and phone instead of one person noticing it in a shed.
     */
    const val WEIGHING_WEIGHT_FIELD_FOCUS_DELAYED = "weighing_weight_field_focus_delayed"

    /**
     * The retry budget expired without the field ever gaining focus — the operator is stuck mid-weighing,
     * unable to enter the weight and unable to proceed. This is a field-blocking failure: the operator
     * scanned an animal and captured video proof, but cannot type the weight because the keyboard focus
     * system is unrecoverable after the camera surface tore down, leaving WindowManager's INPUT focus
     * held by another surface. A spike in this event signals operators in sheds unable to progress through
     * their weighing capture flow; every affected animal must be re-scanned.
     *
     * Root cause (same as [WEIGHING_WEIGHT_FIELD_FOCUS_DELAYED]): the LazyColumn row key was derived
     * from a mutable server ID, so when the upload synced, the row rebuilt and tore the OutlinedTextField
     * from under the IME. The fix was to key on the stable animal ID, not the upload state. This event
     * should be rare/zero after that fix lands; a spike indicates the fix was reverted or a similar key
     * instability was reintroduced.
     */
    const val WEIGHING_WEIGHT_FIELD_FOCUS_FAILED = "weighing_weight_field_focus_failed"
}
