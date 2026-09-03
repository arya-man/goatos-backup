package sg.mesha.goatos.core.analytics

/**
 * Canonical analytics event, parameter, and user-property names.
 *
 * Central so no call site hand-rolls a string — a typo in an inline event name becomes a silent
 * gap in a funnel that nobody notices until the dashboard is wrong. Values are `snake_case` to
 * match the analytics egress convention (see `context/analytics/final-analytics-infra.md`).
 */
object AnalyticsEvents {
    /** App process came to the foreground / cold-started. */
    const val APP_OPEN = "app_open"

    /** A new logical session began (emitted alongside [APP_OPEN] on launch). */
    // NOT "session_start": that name is RESERVED by Firebase, which rejects it outright --
    // "Invalid public event name. Event will not be logged (FE): session_start". The app looked
    // instrumented and the very first event of every journey was being dropped on the floor.
    const val SESSION_START = "app_session_start"

    /** Backend-driven bootstrap resolved; carries the [Params.CHROME] the shell will render. */
    const val BOOTSTRAP_LOADED = "bootstrap_loaded"

    /** User initiated a sign-in; [Params.METHOD] distinguishes email vs google vs dev. */
    const val LOGIN_ATTEMPT = "login_attempt"

    /** A sign-in produced a usable session. */
    const val LOGIN_SUCCESS = "login_success"

    /** Firebase/Auth provider signed in and returned an ID token; Goat OS bootstrap may still fail. */
    const val LOGIN_SESSION_READY = "login_session_ready"

    /** A sign-in failed; [Params.REASON] gives a coarse, non-PII cause. */
    const val LOGIN_FAILURE = "login_failure"

    /** Backend-driven bootstrap failed after a local session existed. */
    const val BOOTSTRAP_FAILED = "bootstrap_failed"

    /** User asked for a password-reset email. */
    const val PASSWORD_RESET_REQUESTED = "password_reset_requested"

    /** The password-reset email was dispatched. */
    const val PASSWORD_RESET_SENT = "password_reset_sent"

    /** User signed out; the session was cleared. */
    const val SIGN_OUT = "sign_out"

    /** RFID reader setup screen opened. */
    const val RFID_READER_SCREEN_OPENED = "rfid_reader_screen_opened"

    /** Operator triggered an RFID reader setup action. */
    const val RFID_READER_ACTION = "rfid_reader_action"

    /** A read-only vaccination shed record was opened. */
    const val VACCINATION_RECORD_OPENED = "vaccination_record_opened"

    /**
     * A tap on a shed card was blocked before it could open the scan/execute or record screen.
     * [Params.REASON] is one of `permission_denied`, `not_assigned`, `scheduled_later`,
     * `already_submitted`, `read_only_oversight`. Previously every one of these gates only ever
     * showed a Toast — the operator saw a dead end and nothing recorded which gate it was.
     */
    const val VACCINATION_OPEN_BLOCKED = "vaccination_open_blocked"

    /**
     * The sheds work list rendered with zero rows for an operator/viewer who is past the initial
     * loading state and not offline — a genuinely empty roster, not a load-in-progress or
     * connectivity gap. [Params.KIND] distinguishes the `/vaccination` route from the
     * `/calendar/drive`-hosted one. Previously this silently rendered an empty-state card with no
     * telemetry at all, indistinguishable from "operator never opened the screen".
     */
    const val SHEDS_EMPTY_ROSTER = "sheds_empty_roster"

    /**
     * A submit attempt was rejected by the client-side readiness gate before anything was
     * enqueued — the exact moment the operator sees a disabled Submit button and nothing else.
     * [Params.REASON] carries the coarse blocking cause (form field / proof requirement).
     */
    const val SUBMIT_BLOCKED = "submit_blocked"

    /**
     * The operator switched to a different module from the nav drawer; [Params.MODULE_KEY]
     * carries the backend module key (`vaccination`, `counts`, …). Measures which modules a
     * multi-module principal actually uses, and how often they move between them.
     */
    const val MODULE_SWITCHED = "module_switched"

    /** The Counts census read screen was opened. */
    const val COUNTS_VIEWED = "counts_viewed"

    /** An operator queued a birth. Fires when the write is DURABLE in the outbox, not when the
     *  network call succeeds — that is the moment the operator's work is actually safe. */
    const val COUNTS_BIRTH_SUBMITTED = "counts_birth_submitted"

    /** An operator queued a death (through the guardrailed critical-death exit). */
    const val COUNTS_DEATH_SUBMITTED = "counts_death_submitted"

    /** An operator queued a shifting/movement event. */
    const val COUNTS_SHIFTING_SUBMITTED = "counts_shifting_submitted"

    /** The operator opened the shifting confirmation sheet from a valid form. */
    const val COUNTS_SHIFTING_CONFIRM_OPENED = "counts_shifting_confirm_opened"

    /** The operator accepted the shifting confirmation and the app started durable enqueue. */
    const val COUNTS_SHIFTING_SUBMIT_ATTEMPTED = "counts_shifting_submit_attempted"

    /** The operator opened the Shifting "Pending" tab (the web-approved execution queue). */
    const val COUNTS_SHIFTING_PENDING_VIEWED = "counts_shifting_pending_viewed"

    /** The operator changed a farm/shed filter on the Pending tab. [Params.DIMENSION] =
     *  farm/shed/all; [Params.ACTION] = set/cleared. */
    const val COUNTS_SHIFTING_PENDING_FILTER_APPLIED = "counts_shifting_pending_filter_applied"

    /** The operator opened one approved movement to execute it (the execute screen). */
    const val COUNTS_SHIFTING_EXECUTE_OPENED = "counts_shifting_execute_opened"

    /** The operator attached (or re-recorded) the optional video on a movement being executed. */
    const val COUNTS_SHIFTING_EXECUTE_VIDEO_CAPTURED = "counts_shifting_execute_video_captured"

    /** The operator pressed "Mark done": the completion (relocation) write was queued. */
    const val COUNTS_SHIFTING_EXECUTE_COMPLETED = "counts_shifting_execute_completed"

    /** The operator opened the Herd Operations "Reconcile" tab (wrong-pen cards from weighing). */
    const val COUNTS_PEN_RECONCILIATION_VIEWED = "counts_pen_reconciliation_viewed"

    /** The operator opened one Reconcile card to return the animal (the execute screen). */
    const val COUNTS_PEN_RECONCILIATION_EXECUTE_OPENED = "counts_pen_reconciliation_execute_opened"

    /** The operator recorded (or re-recorded) the mandatory pen-return video on a Reconcile card. */
    const val COUNTS_PEN_RECONCILIATION_VIDEO_CAPTURED = "counts_pen_reconciliation_video_captured"

    /** The operator pressed "Mark done": the Reconcile completion write was queued. */
    const val COUNTS_PEN_RECONCILIATION_COMPLETED = "counts_pen_reconciliation_completed"

    /** The operator opened Birth's final "Tag the kid" permanent-RFID assignment. */
    const val COUNTS_RFID_PROMOTE_OPENED = "counts_rfid_promote_opened"

    /** The operator pressed "Promote to permanent RFID": the promote (retag) write was queued. */
    const val COUNTS_RFID_PROMOTE_SUBMITTED = "counts_rfid_promote_submitted"

    /**
     * The operator started the Bluetooth reader on a permanent-identifier field instead of typing
     * it. [Params.KIND] is the surface (`birth`/`rfid_promote`); [Params.FIELD] is which identifier
     * (`tag`/`tag2`/`primary`/`secondary`). Answers "is the field scanner actually being used, or
     * are operators still transcribing 15-digit tags by hand".
     */
    const val COUNTS_RFID_SCAN_STARTED = "counts_rfid_scan_started"

    /** A Bluetooth scan filled a permanent-identifier field with a completed tag read. */
    const val COUNTS_RFID_SCAN_CAPTURED = "counts_rfid_scan_captured"

    /** A Counts write could not be queued at all. [Params.KIND] distinguishes
     *  birth/death/shifting; [Params.REASON] carries a coarse, non-PII cause. */
    const val COUNTS_WRITE_FAILURE = "counts_write_failure"

    /** A Counts read (summary, breakdown page, or approval queue) failed to refresh. */
    const val COUNTS_READ_FAILURE = "counts_read_failure"

    /** A Health work list or case screen was opened. [Params.KIND] is the age band
     *  (`adult`/`kid`), which is the split the Health module actually routes on. */
    const val HEALTH_VIEWED = "health_viewed"

    /** An operator queued a health case. Fires when the write is DURABLE, matching the
     *  COUNTS_*_SUBMITTED convention: the operator's work being safe is the moment worth
     *  measuring, not the network call returning. */
    const val HEALTH_CASE_SUBMITTED = "health_case_submitted"

    /**
     * One completed observation form reached the durable outbox.
     *
     * Tracked on the ENQUEUE, not on a server round trip: the operator's work
     * being safe is the moment worth measuring, and the assessment they get back
     * is a separate thing that may arrive minutes later out of a shed.
     */
    const val HEALTH_OBSERVATION_SUBMITTED = "health_observation_submitted"

    /**
     * The Health Director's decision on one assessment reached the durable outbox.
     *
     * [Params.COUNT] is how many diagnoses were approved. ZERO is a real and
     * important value: it means the Director declined the whole assessment, which
     * is the signal that the register and the person disagree — exactly the number
     * worth watching as the rule table is tuned.
     */
    const val HEALTH_DIAGNOSIS_CONFIRMED = "health_diagnosis_confirmed"

    /** A Health write could not be queued at all. [Params.KIND] distinguishes the surface
     *  (`case`/`work_item`); [Params.REASON] carries a coarse, non-PII cause. */
    const val HEALTH_WRITE_FAILURE = "health_write_failure"

    /** A Health read (work list, case lookup, or goat search) failed to refresh. Cached Room
     *  data stays visible when present, so this is the only signal that a refresh is failing. */
    const val HEALTH_READ_FAILURE = "health_read_failure"

    /** The mandatory treatment video was captured and its upload queued (2026-08-29). */
    const val HEALTH_TREATMENT_VIDEO_CAPTURED = "health_treatment_video_captured"

    /** A treatment-session completion was queued durably, video reference attached. */
    const val HEALTH_TREATMENT_SUBMITTED = "health_treatment_submitted"

    /** A clinical case closure (recovered/referred/canceled) was queued durably. */
    const val HEALTH_CASE_CLOSED = "health_case_closed"

    /** Operator opened the Room-first weighing work list or a weighing capture scope. */
    const val WEIGHING_VIEWED = "weighing_viewed"

    /** A weighing read refresh failed; cached Room data remains visible when present. */
    const val WEIGHING_READ_FAILURE = "weighing_read_failure"

    /** Operator attempted to queue a weighing capture locally. */
    const val WEIGHING_CAPTURE_ATTEMPT = "weighing_capture_attempt"

    /** A weighing capture was durably stored locally with its proof state. */
    const val WEIGHING_CAPTURE_SUCCESS = "weighing_capture_success"

    /** A weighing capture could not be queued or proof storage failed. */
    const val WEIGHING_CAPTURE_FAILURE = "weighing_capture_failure"

    const val VACCINATION_SCAN = "vaccination_scan"
    const val VACCINATION_SCAN_REJECTED = "vaccination_scan_rejected"
    const val VACCINATION_PROOF_CAPTURE_ATTEMPT = "vaccination_proof_capture_attempt"
    const val VACCINATION_PROOF_CAPTURE_SUCCESS = "vaccination_proof_capture_success"
    const val VACCINATION_PROOF_CAPTURE_FAILURE = "vaccination_proof_capture_failure"
    const val VACCINATION_PROOF_CAPTURE_CANCELLED = "vaccination_proof_capture_cancelled"
    const val VACCINATION_SUBMIT_ATTEMPT = "vaccination_submit_attempt"
    const val VACCINATION_SUBMIT_SUCCESS = "vaccination_submit_success"
    const val VACCINATION_SUBMIT_FAILURE = "vaccination_submit_failure"

    const val WEIGHING_SCAN = "weighing_scan"
    const val WEIGHING_WEIGHT_CAPTURE_ATTEMPT = "weighing_weight_capture_attempt"
    const val WEIGHING_WEIGHT_CAPTURE_SUCCESS = "weighing_weight_capture_success"
    const val WEIGHING_WEIGHT_CAPTURE_FAILURE = "weighing_weight_capture_failure"
    const val WEIGHING_PROOF_CAPTURE_ATTEMPT = "weighing_proof_capture_attempt"
    const val WEIGHING_PROOF_CAPTURE_SUCCESS = "weighing_proof_capture_success"
    const val WEIGHING_PROOF_CAPTURE_FAILURE = "weighing_proof_capture_failure"
    const val WEIGHING_PROOF_CAPTURE_CANCELLED = "weighing_proof_capture_cancelled"
    const val WEIGHING_SUBMIT_ATTEMPT = "weighing_submit_attempt"
    const val WEIGHING_SUBMIT_SUCCESS = "weighing_submit_success"
    const val WEIGHING_SUBMIT_FAILURE = "weighing_submit_failure"

    /** A weight write was rejected by the server (409 conflict), indicating the weight was
     *  silently discarded and the operator must re-capture the animal. */
    const val WEIGHING_CAPTURE_CONFLICT = "weighing_capture_conflict"

    /** A weighing proof video's upload failed an attempt and will be re-tried. Emitted once per
     *  DISTINCT failure, not once per Room emission, so the funnel counts real attempts.
     *  [Params.SUBJECT_TYPE] separates the lump-sum shed video from the per-animal one,
     *  [Params.ATTEMPT] is the retry ordinal this session has seen, [Params.REASON] the coarse
     *  cause. Without this the retry loop was invisible: a shed proof re-uploaded for 15 minutes
     *  with nothing on screen but "uploading" and not one line in logcat (phone-QA 2026-08-03). */
    const val WEIGHING_PROOF_UPLOAD_RETRY = "weighing_proof_upload_retry"

    /** A weighing proof video's upload reached its terminal FAILED state, so the capture can
     *  never be submitted until the video is recorded again. */
    const val WEIGHING_PROOF_UPLOAD_FAILED = "weighing_proof_upload_failed"

    /** Leadership opened the weighing plan wizard to create or edit a weighing task. */
    const val WEIGHING_PLAN_VIEWED = "weighing_plan_viewed"

    /** Leadership attempted to save a weighing plan (before outbox enqueue). */
    const val WEIGHING_PLAN_SAVE_ATTEMPTED = "weighing_plan_save_attempted"

    /** A weighing plan was durably queued to save. */
    const val WEIGHING_PLAN_SAVE_SUCCEEDED = "weighing_plan_save_succeeded"

    /** A weighing plan save could not be queued or enqueue failed. */
    const val WEIGHING_PLAN_SAVE_FAILED = "weighing_plan_save_failed"

    /** Leadership opened the weighing alerts history screen. */
    /** Vaccination's own module-scoped alerts feed (GET /app/vaccination/alerts). Distinct from
     *  the control-tower gap summary the Alerts tab used to render. */
    const val VACCINATION_ALERTS_VIEWED = "vaccination_alerts_viewed"
    const val VACCINATION_ALERTS_REFRESH_ATTEMPTED = "vaccination_alerts_refresh_attempted"
    const val VACCINATION_ALERTS_REFRESH_SUCCEEDED = "vaccination_alerts_refresh_succeeded"
    const val VACCINATION_ALERTS_REFRESH_FAILED = "vaccination_alerts_refresh_failed"

    const val WEIGHING_ALERTS_VIEWED = "weighing_alerts_viewed"

    /** Leadership attempted to refresh the weighing alerts. */
    const val WEIGHING_ALERTS_REFRESH_ATTEMPTED = "weighing_alerts_refresh_attempted"

    /** Weighing alerts refresh succeeded. */
    const val WEIGHING_ALERTS_REFRESH_SUCCEEDED = "weighing_alerts_refresh_succeeded"

    /** Weighing alerts refresh failed. */
    const val WEIGHING_ALERTS_REFRESH_FAILED = "weighing_alerts_refresh_failed"

    /** Leadership opened a weighing shed detail screen. */
    const val WEIGHING_SHED_DETAIL_VIEWED = "weighing_shed_detail_viewed"

    /** A weighing shed detail load failed to refresh. */
    const val WEIGHING_SHED_DETAIL_LOAD_FAILED = "weighing_shed_detail_load_failed"

    /** Leadership attempted to reopen a weighing shed. */
    const val WEIGHING_SHED_DETAIL_REOPEN_ATTEMPTED = "weighing_shed_detail_reopen_attempted"

    /** A weighing shed reopen was durably queued. */
    const val WEIGHING_SHED_DETAIL_REOPEN_SUCCEEDED = "weighing_shed_detail_reopen_succeeded"

    /** A weighing shed reopen could not be queued. */
    const val WEIGHING_SHED_DETAIL_REOPEN_FAILED = "weighing_shed_detail_reopen_failed"

    /** Leadership opened the vaccination verification videos gallery. */
    const val VACCINATION_LEADERSHIP_VIDEO_VIEWED = "vaccination_leadership_video_viewed"

    /** Leadership started playback of one vaccination proof video. */
    const val VACCINATION_LEADERSHIP_VIDEO_PLAY_STARTED = "vaccination_leadership_video_play_started"

    /** Leadership ended a bounded playback session for one vaccination proof video. */
    const val VACCINATION_LEADERSHIP_VIDEO_WATCH_SUMMARY = "vaccination_leadership_video_watch_summary"

    /** Leadership vaccination video playback failed before a usable review could continue. */
    const val VACCINATION_LEADERSHIP_VIDEO_PLAYBACK_ERROR = "vaccination_leadership_video_playback_error"

    /** Verifier opened a proof item detail screen that can stream evidence media. */
    const val VERIFY_ITEM_OPENED = "verify_item_opened"

    /** Verifier tapped the play/pause control on a proof video — recorded at the TAP, before the
     *  player callback that proves playback actually started ([VERIFY_VIDEO_PLAY_STARTED]). A
     *  finished/idle ExoPlayer can silently no-op on play(), which is otherwise indistinguishable
     *  from "the operator never tapped it" — see [VERIFY_VIDEO_PLAY_DEAD] and
     *  `docs/observability/TELEMETRY_GUARDRAILS.md`. */
    const val VERIFY_VIDEO_PLAY_INTENT = "verify_video_play_intent"

    /** [VERIFY_VIDEO_PLAY_INTENT] fired but no [VERIFY_VIDEO_PLAY_STARTED] /
     *  onIsPlayingChanged(false-to-true after a pause tap) callback landed within the watchdog
     *  window — a dead play/pause control. */
    const val VERIFY_VIDEO_PLAY_DEAD = "verify_video_play_dead"

    /** Verifier started playing one proof video. */
    const val VERIFY_VIDEO_PLAY_STARTED = "verify_video_play_started"

    /** Verifier ended a bounded playback session for one proof video. */
    const val VERIFY_VIDEO_WATCH_SUMMARY = "verify_video_watch_summary"

    /** Verifier video playback failed before a usable review could continue. */
    const val VERIFY_VIDEO_PLAYBACK_ERROR = "verify_video_playback_error"

    /** Verifier tapped the fullscreen button on a proof video. */
    const val VERIFY_VIDEO_FULLSCREEN_OPENED = "verify_video_fullscreen_opened"

    /** Verifier dismissed the fullscreen proof-video dialog (X button, back gesture, or scrim
     *  dismiss) — pairs with [VERIFY_VIDEO_FULLSCREEN_OPENED] so a fullscreen session that never
     *  closes (crash, ANR) is distinguishable from one the verifier deliberately exited. */
    const val VERIFY_VIDEO_FULLSCREEN_EXITED = "verify_video_fullscreen_exited"

    /** The verifier changed a queue filter/scope. [Params.DIMENSION] is `park`/`shed`/`module`/
     *  `category`; [Params.ACTION] is `set`/`cleared`, mirroring [COUNTS_FILTER_APPLIED]. */
    const val VERIFY_QUEUE_FILTER_APPLIED = "verify_queue_filter_applied"

    /** The verifier scrolled near the end of the queue and the next keyset page was requested. */
    const val VERIFY_QUEUE_LOAD_MORE = "verify_queue_load_more" // mobile-guard:ignore: analytics event name for auto-triggered keyset paging, not a tappable UI control

    /**
     * ONE summary per queue-screen exit — never per scroll frame, which would drown the funnel
     * and cost battery. Carries the max row index reached ([Params.MAX_SCROLL_INDEX]) and how
     * many rows were on screen ([Params.ROW_COUNT]) so "did she actually scroll through the
     * list" is answerable without a per-frame event.
     */
    const val VERIFY_QUEUE_SCROLL_SUMMARY = "verify_queue_scroll_summary"

    /** A verifier/authority tapped "Close drive" on a ready batch — before the outbox enqueue. */
    const val VERIFY_DRIVE_CLOSE_ATTEMPTED = "verify_drive_close_attempted"

    /** A drive-close was durably queued and confirmed by the backend. */
    const val VERIFY_DRIVE_CLOSE_SUCCEEDED = "verify_drive_close_succeeded"

    /** A drive-close could not be queued or was rejected by the backend. */
    const val VERIFY_DRIVE_CLOSE_FAILED = "verify_drive_close_failed"

    /**
     * The verifier left the detail screen (back navigation / close button). [Params.REASON] is
     * `fully_decided` when every entry in the shed group reached a terminal verdict, or
     * `abandoned` when at least one entry was still PENDING — the signal that distinguishes a
     * shed the verifier finished from one she walked away from mid-review.
     */
    const val VERIFY_ITEM_CLOSED = "verify_item_closed"

    /** The mandatory-reason reject dialog was opened for one animal's verdict. */
    const val VERIFY_REJECT_DIALOG_OPENED = "verify_reject_dialog_opened"

    /** The reject dialog was dismissed without confirming (Cancel, scrim, back). */
    const val VERIFY_REJECT_DIALOG_CANCELLED = "verify_reject_dialog_cancelled"

    /** Reject was tapped with a blank reason — the client-side mandatory-reason gate refused
     *  before anything was enqueued (mirrors [SUBMIT_BLOCKED] for this screen's own gate). */
    const val VERIFY_REJECT_BLOCKED_EMPTY_REASON = "verify_reject_blocked_empty_reason"

    /** The irreversible-approve confirmation dialog was opened for one animal's verdict. */
    const val VERIFY_APPROVE_DIALOG_OPENED = "verify_approve_dialog_opened"

    /** The approve confirmation dialog was dismissed without confirming. */
    const val VERIFY_APPROVE_DIALOG_CANCELLED = "verify_approve_dialog_cancelled"

    /**
     * The operator changed a census filter on the Counts screen. [Params.DIMENSION] is which
     * filter (`park`/`shed`/`breed`/`all`) and [Params.ACTION] is `set` or `cleared`.
     *
     * Measures which slices of the herd operators actually look at, which is what tells us whether
     * the three dimensions offered are the right three — and `cleared` on `all` is the signal that
     * a filter combination produced nothing useful and had to be abandoned.
     */
    const val COUNTS_FILTER_APPLIED = "counts_filter_applied"

    /** The Feed Direction generated-sheet screen was opened. */
    const val FEED_DIRECTION_VIEWED = "feed_direction_viewed"

    /** The Feed Packing worklist screen was opened. */
    const val FEED_PACKING_VIEWED = "feed_packing_viewed"

    /** The Feed Transport verification-submission worklist screen was opened. */
    const val FEED_TRANSPORT_VIEWED = "feed_transport_viewed"

    /** A Feed read (direction, packing, or transport) failed. [Params.KIND] is the surface,
     *  [Params.REASON] a coarse cause. */
    const val FEED_READ_FAILURE = "feed_read_failure"

    /** A Feed filter changed. [Params.DIMENSION] is `farm`/`shed`/`workflow`/`all`, [Params.ACTION]
     *  is `set`/`cleared`. */
    const val FEED_FILTER_APPLIED = "feed_filter_applied"

    /** A Feed worklist row was tapped. [Params.KIND] is `direction`/`packing`/`transport`. */
    const val FEED_ROW_TAPPED = "feed_row_tapped"

    /** The feed-direction completion detail was opened (a shed-session row was tapped). */
    const val FEED_COMPLETE_OPENED = "feed_complete_opened"

    /** An optional video was captured on the feed completion detail. */
    const val FEED_COMPLETE_VIDEO_CAPTURED = "feed_complete_video_captured"

    /** A shed-session was marked fed (feed.direction.completed enqueued). */
    const val FEED_DIRECTION_COMPLETED = "feed_direction_completed"

    /** A feed completion could not be queued. [Params.REASON] carries a coarse, non-PII cause. */
    const val FEED_COMPLETE_FAILURE = "feed_complete_failure"

    /** The verifier-gated feed-distribution completion detail was opened (a Direction row tapped). */
    const val FEED_DISTRIBUTION_OPENED = "feed_distribution_opened"

    /** Operator left the feed-distribution proof detail. */
    const val FEED_DISTRIBUTION_BACK_TAPPED = "feed_distribution_back_tapped"

    /** Operator tapped one of the independent feed-distribution proof capture actions. */
    const val FEED_DISTRIBUTION_CAPTURE_TAPPED = "feed_distribution_capture_tapped"

    /** A feed-distribution proof was captured locally. [Params.KIND] names the proof slot. */
    const val FEED_DISTRIBUTION_PROOF_CAPTURED = "feed_distribution_proof_captured"

    /** A feed-distribution proof upload reached backend sync. [Params.KIND] names the proof slot. */
    const val FEED_DISTRIBUTION_PROOF_UPLOAD_SYNCED = "feed_distribution_proof_upload_synced"

    /** Operator tapped a completed proof card to reupload that proof. */
    const val FEED_DISTRIBUTION_PROOF_REUPLOAD_TAPPED = "feed_distribution_proof_reupload_tapped"

    /** Submit was tapped before all three proof uploads were synced. */
    const val FEED_DISTRIBUTION_SUBMIT_BLOCKED = "feed_distribution_submit_blocked"

    /** Operator manually asked the detail to sync proof/upload state. */
    const val FEED_DISTRIBUTION_SYNC_TAPPED = "feed_distribution_sync_tapped"

    /** A submitted/in-review feed-distribution row was tapped and blocked from reopening. */
    const val FEED_DISTRIBUTION_REOPEN_BLOCKED = "feed_distribution_reopen_blocked"

    /** The MANDATORY feed-WEIGHT photo (step 1) was captured on the distribution detail. Its own
     *  event rather than a [Params.KIND] on the video one: it is the step most likely to be skipped
     *  or retaken, and the drop-off between it and [FEED_DISTRIBUTION_SUBMITTED] is the funnel that
     *  says whether the 2026-08-11 third capture is costing operators completions. */
    const val FEED_DISTRIBUTION_WEIGHT_PHOTO_CAPTURED = "feed_distribution_weight_photo_captured"

    /** The MANDATORY feed-distribution video (step 2) was captured on the distribution detail. */
    const val FEED_DISTRIBUTION_VIDEO_CAPTURED = "feed_distribution_video_captured"

    /** The MANDATORY water-distribution video (step 3) was captured. [Params.KIND] is retained and is
     *  always `video` since the 2026-08-11 video-only rule; it stays on the event so history before
     *  and after the change remains comparable in the same funnel. */
    const val FEED_DISTRIBUTION_WATER_PROOF_CAPTURED = "feed_distribution_water_proof_captured"

    /** A feed-distribution completion was submitted for verification (all three proofs queued,
     *  completion enqueued -> the shed-session moves to pending_verification). */
    const val FEED_DISTRIBUTION_SUBMITTED = "feed_distribution_submitted"

    /** A feed-distribution completion or proof could not be queued. [Params.REASON] a coarse cause. */
    const val FEED_DISTRIBUTION_FAILURE = "feed_distribution_failure"

    /**
     * A read of ANOTHER operator's server-recorded proof slots for this pen-session completed
     * (success, empty, a failed attempt, or the retry ladder exhausted). [Params.RESULT],
     * [Params.SLOT_MASK], [Params.RETRY_COUNT], and [Params.SOURCE] carry the bounded outcome;
     * richer per-slot detail is backend-mirror-only on the same call site.
     */
    const val FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ = "feed_distribution_teammate_captures_read"

    /** A teammate's server proof ref was adopted for a slot this phone had not itself captured.
     *  [Params.KIND] names the slot; [Params.LOCAL_SLOT_STATE] is always `empty` here (a local
     *  capture always wins and is never overwritten -- see [adoptTeammateCapture]). */
    const val FEED_DISTRIBUTION_TEAMMATE_PROOF_ADOPTED = "feed_distribution_teammate_proof_adopted"

    /** Fired alongside [FEED_DISTRIBUTION_SUBMITTED]/[FEED_DISTRIBUTION_SUBMIT_BLOCKED] with the
     *  per-slot source breakdown ([Params.FEED_WEIGHT_SOURCE]/[Params.FEED_VIDEO_SOURCE]/
     *  [Params.WATER_VIDEO_SOURCE]) and [Params.RESULT]. */
    const val FEED_DISTRIBUTION_SUBMIT_SOURCES = "feed_distribution_submit_sources"

    /** The screen's editable/read-only status changed. [Params.SOURCE] is `room`/`server_poll`/
     *  `sync_tap`; [Params.PREVIOUS]/[Params.NEXT] are `editable`/`readonly`; [Params.STATUS] is
     *  the underlying lifecycle bucket. */
    const val FEED_DISTRIBUTION_LIVE_STATUS_CHANGED = "feed_distribution_live_status_changed"

    /** The verifier-gated feed-packing completion detail was opened (a Packing row tapped). */
    const val FEED_PACKING_COMPLETE_OPENED = "feed_packing_complete_opened"

    /** The MANDATORY packing video was captured on the packing completion detail. */
    const val FEED_PACKING_VIDEO_CAPTURED = "feed_packing_video_captured"
    const val FEED_PACKING_CAPTURE_TAPPED = "feed_packing_capture_tapped"
    const val FEED_PACKING_PROOF_UPLOAD_SYNCED = "feed_packing_proof_upload_synced"
    const val FEED_PACKING_REUPLOAD_TAPPED = "feed_packing_reupload_tapped"
    const val FEED_PACKING_SYNC_TAPPED = "feed_packing_sync_tapped"
    const val FEED_PACKING_SUBMIT_BLOCKED = "feed_packing_submit_blocked"

    /** A feed-packing completion was submitted for verification (proof queued, completion enqueued
     *  -> the shed-session moves to pending_verification). */
    const val FEED_PACKING_SUBMITTED = "feed_packing_submitted"

    /** A feed-packing completion or proof could not be queued. [Params.REASON] a coarse cause. */
    const val FEED_PACKING_COMPLETE_FAILURE = "feed_packing_complete_failure"

    /** The Feed Wastage worklist screen was opened (maintainer decision 2026-08-18). */
    const val FEED_WASTAGE_VIEWED = "feed_wastage_viewed"

    /** The verifier-gated feed-wastage capture detail was opened (a pen row was tapped). */
    const val FEED_WASTAGE_COMPLETE_OPENED = "feed_wastage_complete_opened"

    /** The MANDATORY leftover-feed video was captured on the wastage capture detail. */
    const val FEED_WASTAGE_VIDEO_CAPTURED = "feed_wastage_video_captured"

    /** A feed-wastage completion was submitted for verification (proof queued, completion enqueued
     *  -> the pen-day moves to pending_verification). */
    const val FEED_WASTAGE_SUBMITTED = "feed_wastage_submitted"

    /** A feed-wastage completion or proof could not be queued. [Params.REASON] a coarse cause. */
    const val FEED_WASTAGE_COMPLETE_FAILURE = "feed_wastage_complete_failure"

    /** The feed-transport proof/submit detail was opened from the Transport worklist. */
    const val FEED_TRANSPORT_OPENED = "feed_transport_opened"

    /** The MANDATORY transport video was captured on the feed-transport detail. */
    const val FEED_TRANSPORT_VIDEO_CAPTURED = "feed_transport_video_captured"
    const val FEED_TRANSPORT_CAPTURE_TAPPED = "feed_transport_capture_tapped"
    const val FEED_TRANSPORT_PROOF_UPLOAD_SYNCED = "feed_transport_proof_upload_synced"
    const val FEED_TRANSPORT_REUPLOAD_TAPPED = "feed_transport_reupload_tapped"
    const val FEED_TRANSPORT_SYNC_TAPPED = "feed_transport_sync_tapped"
    const val FEED_TRANSPORT_SUBMIT_BLOCKED = "feed_transport_submit_blocked"

    /** A feed-transport completion was submitted for verification. */
    const val FEED_TRANSPORT_SUBMITTED = "feed_transport_submitted"

    /** A feed-transport proof or completion could not be queued. */
    const val FEED_TRANSPORT_FAILURE = "feed_transport_failure"

    // --- PC Care (module pc_care, maintainer decision 2026-08-21): planner-assigned deworming /
    // ticks removal / hoof trimming / hair trimming tasks with per-animal slot videos. ---

    /** A PC Care category worklist tab was opened; [Params.KIND] carries the category key. */
    const val PC_CARE_WORKLIST_VIEWED = "pc_care_worklist_viewed"

    /** A PC Care task detail was opened from a worklist card. */
    const val PC_CARE_TASK_OPENED = "pc_care_task_opened"

    /** The vaccination Stock proof detail rendered; [Params.STATUS] is the task lifecycle. */
    const val PC_CARE_STOCK_PROOF_SCREEN_VISIBLE = "pc_care_stock_proof_screen_visible"

    /** A vaccination Stock proof row/button was tapped; [Params.KIND] = photo/video. */
    const val PC_CARE_STOCK_PROOF_ACTION_TAPPED = "pc_care_stock_proof_action_tapped"

    /** Camera returned for Stock proof capture before durable Room enqueue. */
    const val PC_CARE_STOCK_PROOF_CAPTURE_RESULT = "pc_care_stock_proof_capture_result"

    /** Captured Stock proof row was inserted into Room and upload enqueue was requested. */
    const val PC_CARE_STOCK_PROOF_ROOM_WRITTEN = "pc_care_stock_proof_room_written"

    /** Upload outbox id became visible in Room for the Stock proof. */
    const val PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED = "pc_care_stock_proof_upload_enqueued"

    /** Stock proof slot registration write was enqueued or failed. */
    const val PC_CARE_STOCK_PROOF_REGISTRATION = "pc_care_stock_proof_registration"

    /** Stock proof manual refresh/sync action started or completed. */
    const val PC_CARE_STOCK_PROOF_SYNC = "pc_care_stock_proof_sync"

    /** Stock detail proof preview URL hydration/playback. */
    const val PC_CARE_STOCK_PROOF_PREVIEW = "pc_care_stock_proof_preview"

    /** Stock proof submit button was tapped or blocked before confirmation. */
    const val PC_CARE_STOCK_PROOF_SUBMIT = "pc_care_stock_proof_submit"

    /** Stock proof submit write was enqueued or failed. */
    const val PC_CARE_STOCK_PROOF_SUBMIT_ENQUEUED = "pc_care_stock_proof_submit_enqueued"

    /** The PC Director's stock verdict (approve/reject) was sent, landed, or failed. */
    const val PC_CARE_STOCK_VERDICT = "pc_care_stock_verdict"

    /** A scanned tag was accepted into the task (queued durably for sync). */
    const val PC_CARE_SCAN_ACCEPTED = "pc_care_scan_accepted"

    /** A scanned tag was already in the task ("Already scanned" notice shown). */
    const val PC_CARE_SCAN_DUPLICATE = "pc_care_scan_duplicate"

    /** A slot's video recorder was opened; [Params.KIND] carries the slot field key. */
    const val PC_CARE_SLOT_CAPTURE_STARTED = "pc_care_slot_capture_started"

    /** A slot's video was captured and its upload queued; [Params.KIND] the slot field key. */
    const val PC_CARE_SLOT_CAPTURED = "pc_care_slot_captured"

    /** Submit was refused; [Params.REASON] carries the coarse blocked cause. */
    const val PC_CARE_SUBMIT_BLOCKED = "pc_care_submit_blocked"

    /** The operator confirmed the whole-task submit (write enqueued). */
    const val PC_CARE_SUBMIT_CONFIRMED = "pc_care_submit_confirmed"

    /** A task in rework was opened (the verifier's reason is on screen). */
    const val PC_CARE_REWORK_VIEWED = "pc_care_rework_viewed"

    /** The planner created a PC Care task; [Params.KIND] carries the category key. */
    const val PC_CARE_PLAN_TASK_CREATED = "pc_care_plan_task_created"

    /** A PC Care read, scan, capture, or submit could not be queued/served. [Params.REASON]. */
    const val PC_CARE_FAILURE = "pc_care_failure"

    /** The milk preparation screen (park's milk processing setup) was opened. */
    const val MILK_PREPARATION_OPENED = "milk_preparation_opened"

    /** Operator attempted to capture a proof video for a milk preparation step. */
    const val MILK_PREPARATION_PROOF_CAPTURE_ATTEMPT = "milk_preparation_proof_capture_attempt"

    /** A milk preparation proof video was captured successfully. */
    const val MILK_PREPARATION_PROOF_CAPTURE_SUCCESS = "milk_preparation_proof_capture_success"

    /** A milk preparation proof capture failed or was cancelled. */
    const val MILK_PREPARATION_PROOF_CAPTURE_FAILURE = "milk_preparation_proof_capture_failure"

    /** Operator submitted milk preparation answers and proofs for verification. */
    const val MILK_PREPARATION_SUBMITTED = "milk_preparation_submitted"

    /** A milk preparation submission could not be queued. [Params.REASON] carries a coarse cause. */
    const val MILK_PREPARATION_FAILURE = "milk_preparation_failure"

    /** The milk feeding screen (recording feeding observations) was opened. */
    const val MILK_FEEDING_OPENED = "milk_feeding_opened"

    /** Operator attempted to capture a proof video for a milk feeding task. */
    const val MILK_FEEDING_PROOF_CAPTURE_ATTEMPT = "milk_feeding_proof_capture_attempt"

    /** A milk feeding proof video was captured successfully. */
    const val MILK_FEEDING_PROOF_CAPTURE_SUCCESS = "milk_feeding_proof_capture_success"

    /** A milk feeding proof capture failed or was cancelled. */
    const val MILK_FEEDING_PROOF_CAPTURE_FAILURE = "milk_feeding_proof_capture_failure"

    /** Operator submitted milk feeding answers and proofs for verification. */
    const val MILK_FEEDING_SUBMITTED = "milk_feeding_submitted"

    /** A milk feeding submission could not be queued. [Params.REASON] carries a coarse cause. */
    const val MILK_FEEDING_FAILURE = "milk_feeding_failure"

    /**
     * A Birth/Death workflow work list was opened (docs/decisions/birth-death-workflows.md).
     * [Params.KIND] is the module (`birth`/`death`).
     */
    const val WORKFLOW_LIST_VIEWED = "workflow_list_viewed"

    /** A per-goat workflow card was opened (the drill-in action screen). [Params.KIND] = module. */
    const val WORKFLOW_CARD_OPENED = "workflow_card_opened"

    /** A question / question_select workflow action was answered (durably queued). */
    const val WORKFLOW_ACTION_ANSWERED = "workflow_action_answered"

    /** An `action`-type workflow step was completed (durably queued). */
    const val WORKFLOW_ACTION_COMPLETED = "workflow_action_completed"

    /** A mandatory video was recorded/picked for a requires_video workflow action. */
    const val WORKFLOW_VIDEO_CAPTURED = "workflow_video_captured"

    /**
     * The vaccination sheds/overview screen rendered its first non-loading state — the operator's
     * shed-first work queue AND the leadership (CEO/CXO/Director) read-only oversight card share
     * this one screen ([Params.KIND] = `leadership`/`operator`). Previously nothing at all was
     * emitted for this screen: a CEO could sit on the Vaccination Overview looking at park
     * filters, a protocol-adherence card, and shed cards with real counts, and the only signal in
     * analytics was `bootstrap_loaded` — indistinguishable from the CEO never opening the screen.
     */
    const val VACCINATION_SHEDS_VIEWED = "vaccination_sheds_viewed"

    /** The operator/leadership principal changed the selected day tab on the sheds/overview
     *  screen. [Params.DIMENSION] is always `day`; carried for symmetry with other filter events. */
    const val VACCINATION_DAY_SELECTED = "vaccination_day_selected"

    /** A park filter on the sheds/overview screen was set or cleared. [Params.DIMENSION] is
     *  `park`; [Params.ACTION] is `set`/`cleared`, mirroring [COUNTS_FILTER_APPLIED]. */
    const val VACCINATION_PARK_FILTER_APPLIED = "vaccination_park_filter_applied"

    /** A sheds/overview refresh (pull-to-refresh or park-filter-triggered) was attempted. */
    const val VACCINATION_REFRESH_ATTEMPTED = "vaccination_refresh_attempted"

    /** A sheds/overview refresh landed successfully. */
    const val VACCINATION_REFRESH_SUCCEEDED = "vaccination_refresh_succeeded"

    /** A sheds/overview refresh failed; cached Room data remains visible when present. */
    const val VACCINATION_REFRESH_FAILED = "vaccination_refresh_failed"

    /** The sheds/overview screen requested the next keyset page (scrolled near the end). */
    const val VACCINATION_LOAD_MORE_ATTEMPTED = "vaccination_load_more_attempted"

    /** A sheds/overview "load more" page append succeeded. */
    const val VACCINATION_LOAD_MORE_SUCCEEDED = "vaccination_load_more_succeeded"

    /** A sheds/overview "load more" page append failed. */
    const val VACCINATION_LOAD_MORE_FAILED = "vaccination_load_more_failed"

    /**
     * The Calendar screen rendered its first non-loading state for a role (leadership week/month
     * history, or the calendar-hosted drive list). [Params.KIND] carries the active segment
     * (`week`/`month`). Previously a Director could sit on the Calendar looking at a week strip
     * and drive cards with nothing emitted beyond `bootstrap_loaded`.
     */
    const val CALENDAR_VIEWED = "calendar_viewed"

    /** The Calendar week-strip day selection changed. [Params.DIMENSION] is `day`. */
    const val CALENDAR_DAY_SELECTED = "calendar_day_selected"

    /** The Calendar week/month segment tab changed (`week`/`month`). [Params.DIMENSION] is
     *  `segment`. */
    const val CALENDAR_SEGMENT_SELECTED = "calendar_segment_selected"

    /** A Calendar month filter (park/shed/vaccine/status) was applied or cleared. [Params.DIMENSION]
     *  names the filter; [Params.ACTION] is `set`/`cleared`. */
    const val CALENDAR_FILTER_APPLIED = "calendar_filter_applied"

    /** A Calendar refresh (initial load, day change, or filter change) was attempted. */
    const val CALENDAR_REFRESH_ATTEMPTED = "calendar_refresh_attempted"

    /** A Calendar refresh landed successfully (no request in the batch failed). */
    const val CALENDAR_REFRESH_SUCCEEDED = "calendar_refresh_succeeded"

    /** A Calendar refresh failed (at least one request in the batch failed). */
    const val CALENDAR_REFRESH_FAILED = "calendar_refresh_failed"

    /** The Calendar selected-day list requested the next keyset page. */
    const val CALENDAR_LOAD_MORE_ATTEMPTED = "calendar_load_more_attempted"

    /** A Calendar "load more" page append succeeded. */
    const val CALENDAR_LOAD_MORE_SUCCEEDED = "calendar_load_more_succeeded"

    /** A Calendar "load more" page append failed. */
    const val CALENDAR_LOAD_MORE_FAILED = "calendar_load_more_failed"

    /** The Counts approver's pending-decision queue was opened. */
    const val COUNTS_APPROVAL_QUEUE_VIEWED = "counts_approval_queue_viewed"

    /**
     * An approver queued a decision. [Params.DECISION] is `approved`/`rejected` and
     * [Params.KIND] is the request type (`birth`/`death`/`shifting`).
     *
     * Fires when the decision is DURABLE in the outbox, not when the network call succeeds — that
     * is the moment the approver's action is actually safe, and it is what makes an approval made
     * with no signal measurable rather than invisible.
     */
    const val COUNTS_APPROVAL_DECIDED = "counts_approval_decided"

    /** A decision could not be queued at all. [Params.REASON] carries a coarse, non-PII cause. */
    const val COUNTS_APPROVAL_FAILURE = "counts_approval_failure"

    /**
     * The visible background-upload foreground service could not be started, so the outbox
     * drain fell back to the WorkManager backstop. [Params.REASON] is a coarse cause
     * (`no_foreground_presence` — the app has no user-visible presence, the normal
     * post-reboot case; `start_rejected` — the platform refused `startForegroundService`;
     * `start_foreground_refused` — the platform refused `startForeground` from inside the
     * service).
     *
     * The operator's queued writes are NOT lost when this fires — they are durable in the Room
     * outbox and still sync — but the progress notification is skipped, so this is the metric
     * that keeps a silent degradation visible instead of surfacing later as "my recorded birth
     * never synced".
     */
    const val SYNC_FOREGROUND_START_BLOCKED = "sync_foreground_start_blocked"

    /**
     * A scan arrived for a DIFFERENT subject (goat/animal) while a proof-video capture was still
     * in flight for another one — the camera is a single physical device, so it cannot be
     * cancelled without destroying an unrecoverable in-progress recording (the confirmed shed
     * defect: an RFID scan mid-recording silently killed the clip). Instead of cancelling, the
     * new scan is QUEUED and its camera opens automatically once the in-flight capture's job
     * completes. [Params.KIND] is `vaccination`/`weighing`; [Params.REASON] is always
     * `recording_in_progress`.
     */
    const val PROOF_CAPTURE_SCAN_DEFERRED = "proof_capture_scan_deferred"

    /**
     * A previously-queued deferred scan (see [PROOF_CAPTURE_SCAN_DEFERRED]) was itself overtaken
     * by a NEWER scan before its camera ever opened — only the last queued scan survives, so the
     * earlier one's camera will never open and the operator must rescan that subject. This is a
     * genuine drop and must be visible, never silent. [Params.KIND] is `vaccination`/`weighing`.
     */
    const val PROOF_CAPTURE_SCAN_DROPPED = "proof_capture_scan_dropped"

    /** A proof-video job entered a new durable processing/upload state in Room. */
    const val PROOF_VIDEO_STATE_CHANGED = "proof_video_state_changed"

    /** A proof-video media processing step failed and the pipeline queued original upload. */
    const val PROOF_VIDEO_PROCESSING_FALLBACK = "proof_video_processing_fallback"

    /** A proof-video upload retry was scheduled from the shared queue. */
    const val PROOF_VIDEO_UPLOAD_RETRY = "proof_video_upload_retry"

    /** A proof-video job reached a terminal state and will need operator/manual action. */
    const val PROOF_VIDEO_DEAD_LETTER = "proof_video_dead_letter"

    /** Shared Room-first proof pipeline events, emitted for every feature using ProofCaptureRepository. */
    const val PROOF_CAMERA_REQUESTED = "proof_camera_requested"
    const val PROOF_CAMERA_VISIBLE = "proof_camera_visible"
    const val PROOF_CAMERA_BOUND = "proof_camera_bound"
    const val PROOF_CAMERA_STREAMING = "proof_camera_streaming"
    const val PROOF_CAMERA_RECORDING_STARTED = "proof_camera_recording_started"
    const val PROOF_CAMERA_STOP_TAPPED = "proof_camera_stop_tapped"
    const val PROOF_CAMERA_CANCELLED = "proof_camera_cancelled"
    const val PROOF_CAMERA_RETRY_TAPPED = "proof_camera_retry_tapped"
    const val PROOF_CAMERA_FINALIZED = "proof_camera_finalized"
    const val PROOF_CAMERA_FAILED = "proof_camera_failed"
    const val PROOF_CAMERA_TORCH_ON = "proof_camera_torch_on"
    const val PROOF_CAMERA_TORCH_OFF = "proof_camera_torch_off"
    const val PROOF_CAMERA_TORCH_FAILED = "proof_camera_torch_failed"
    const val PROOF_GALLERY_PICKER_OPENED = "proof_gallery_picker_opened"
    const val PROOF_GALLERY_PICKER_CANCELLED = "proof_gallery_picker_cancelled"
    const val PROOF_GALLERY_PICKER_IMPORTED = "proof_gallery_picker_imported"
    const val PROOF_GALLERY_PICKER_FAILED = "proof_gallery_picker_failed"
    const val PROOF_PROCESSING_STARTED = "proof_processing_started"
    const val PROOF_PROCESSING_COMPLETED = "proof_processing_completed"
    const val PROOF_PROCESSING_FAILED = "proof_processing_failed"
    const val PROOF_CAPTURE_COMPLETED = "proof_capture_completed"
    const val PROOF_CAPTURE_VALIDATION_FAILED = "proof_capture_validation_failed"
    const val PROOF_GALLERY_SAVE_STARTED = "proof_gallery_save_started"
    const val PROOF_GALLERY_SAVE_COMPLETED = "proof_gallery_save_completed"
    const val PROOF_GALLERY_SAVE_FAILED = "proof_gallery_save_failed"
    const val PROOF_UPLOAD_REGISTERED = "proof_upload_registered"
    const val PROOF_UPLOAD_STARTED = "proof_upload_started"
    const val PROOF_UPLOAD_COMPLETED = "proof_upload_completed"
    const val PROOF_UPLOAD_FAILED = "proof_upload_failed"
    const val PROOF_UPLOAD_ENQUEUE_FAILED = "proof_upload_enqueue_failed"
    const val PROOF_UPLOAD_DRIVER_MISSING = "proof_upload_driver_missing"

    /** Standard event parameter keys. */
    /**
     * The OS notification-permission prompt was shown. Until this existed, POST_NOTIFICATIONS was
     * never requested at all: FCM accepted every push, reported it delivered, and Android dropped
     * it silently. Whether operators actually see alerts is now measurable rather than assumed.
     */
    const val NOTIFICATION_PERMISSION_PROMPTED = "notification_permission_prompted"

    /** The prompt was answered; [Params.REASON] is "granted" or "denied". */
    const val NOTIFICATION_PERMISSION_RESULT = "notification_permission_result"

    /**
     * A backend API call was REFUSED or FAILED (HTTP >= 400, or the call threw before any
     * response arrived). Emitted once per call from the single OkHttp interceptor seam
     * ([FailureReportingNetworkTelemetryReporter]) — never from per-screen call sites, so no
     * surface can forget it and no surface needs boilerplate to have it.
     *
     * Carries [Params.METHOD], [Params.ROUTE] (bounded-cardinality template — never a raw
     * goat/shed id), [Params.STATUS_CODE] and [Params.DURATION_MS]. Never the Authorization
     * header, an FCM token, or any request/response body.
     */
    const val API_CALL_FAILURE = "api_call_failure"

    /**
     * A durably-queued write attempt did not go through. Emitted once per ATTEMPT from the single
     * outbox drain seam ([sg.mesha.goatos.core.common.OutboxTelemetryReporter]) — never from a
     * feature screen.
     *
     * Distinct from [API_CALL_FAILURE], which only ever sees calls that REACHED the network: a
     * write blocked behind a stalled queue head, or one whose dispatch threw before any request
     * was made, produces this and no `api_call_failure` at all. That is the gap that made a
     * minutes-long stuck upload invisible on-device.
     *
     * Carries [Params.OP_TYPE], [Params.ATTEMPT], [Params.MAX_ATTEMPTS] and [Params.REASON] (the
     * failure's exception CLASS name). Never a payload, a server error string, or a credential.
     */
    const val SYNC_WRITE_ATTEMPT_FAILED = "sync_write_attempt_failed"

    /**
     * A queued submit/completion write is waiting for one or more referenced proof-upload rows to
     * succeed. Emitted from the outbox drain seam without consuming the write's retry budget.
     */
    const val SYNC_WRITE_DEPENDENCY_WAIT = "sync_write_dependency_wait"

    /**
     * A queued write is DEAD — it will never be sent again without a manual retry. The loudest
     * event in the outbox lifecycle, and the one whose absence meant permanently-undelivered farm
     * data looked exactly like data still on its way.
     *
     * [Params.REASON] is `conflict` (a definitive server refusal) or `attempts_exhausted`.
     * Accompanied by a throttled Crashlytics non-fatal, the same treatment a failed HTTP call gets.
     */
    const val SYNC_WRITE_DEAD = "sync_write_dead"

    object Params {
        const val METHOD = "method"
        const val REASON = "reason"

        /** Stable park identifier, event-scoped (unlike [UserProps.PARK_ID], which is a durable
         *  user property) — e.g. which park a Milk Preparation/Feeding event happened in. */
        const val PARK_ID = "park_id"

        /**
         * Bounded-cardinality request route TEMPLATE (`/app/weighing/campaigns/{id}/sheds`),
         * produced by `TelemetryInterceptor.routeTemplate` — never a raw path.
         */
        const val ROUTE = "route"

        /** HTTP status of a failed call; `-1` when the call threw before any response arrived. */
        const val STATUS_CODE = "status_code"

        const val CHROME = "chrome"
        const val ACTION = "action"
        const val SHED_ID = "shed_id"
        const val RFID = "rfid"
        const val GOAT_ID = "goat_id"
        const val CAMPAIGN_ID = "campaign_id"
        const val CAMPAIGN_SHED_ID = "campaign_shed_id"
        const val PARTITION_ID = "partition_id"
        const val PARTITION_LABEL = "partition_label"
        const val OUTCOME = "outcome"
        const val WEIGHT_KG = "weight_kg"
        const val ANIMAL_COUNT = "animal_count"
        const val PROOF_CAPTURED = "proof_captured"
        const val PROOF_UPLOADED = "proof_uploaded"
        const val SESSION_NO = "session_no"

        /** Stable backend module key from the bootstrap `modules` array. */
        const val MODULE_KEY = "module_key"

        /** Which Counts write/read a shared event refers to (`birth`/`death`/`shifting`/…). */
        const val KIND = "kind"
        const val COUNT = "count"

        /**
         * Which form field an event refers to (`tag`/`tag2`/`primary`/`secondary`).
         *
         * The field NAME only — never the scanned value. An RFID is livestock operations data
         * rather than PII, but the analytics question here is "which identifier slot gets
         * scanned", which the name answers and the tag id only bloats.
         */
        const val FIELD = "field"

        /** Weighing scope category (`individual_animal` or `per_shed_partition`). */
        const val CATEGORY = "category"

        /** How an approval request was decided (`approved`/`rejected`). */
        const val DECISION = "decision"

        const val ITEM_ID = "item_id"
        const val PROOF_ID = "proof_id"
        const val REPLACED_PROOF_ID = "replaced_proof_id"
        const val PROOF_SURFACE = "proof_surface"
        const val PROOF_MODE = "proof_mode"
        const val PROOF_STATE = "proof_state"
        const val PROOF_STAGE = "proof_stage"
        const val UPLOAD_ORIGINAL = "upload_original"
        const val BYTES_IN = "bytes_in"
        const val BYTES_OUT = "bytes_out"
        const val LOCATION_STATUS = "location_status"
        const val GEOCODER_STATUS = "geocoder_status"
        /** What a proof is evidence OF (`shed` for a lump-sum group video, `other` for the
         *  per-animal one). Diagnosing a stuck upload starts with knowing which lane it is in. */
        const val SUBJECT_TYPE = "subject_type"
        /** 1-based ordinal of the upload attempt this session has observed for one proof. */
        const val ATTEMPT = "attempt"

        /**
         * Which queued write an outbox-lifecycle event refers to — the `OutboxOpType` NAME
         * (`WEIGHING_ANIMAL_OBSERVATION`, `PROOF_UPLOAD`, …). Bounded cardinality by
         * construction (it is an enum), and it carries no goat, shed, or operator identity.
         */
        const val OP_TYPE = "op_type"

        /** Local outbox row id for durable-sync lifecycle/debug events. */
        const val OUTBOX_ITEM_ID = "outbox_item_id"

        /** Local durable sync lane key, used to reconstruct ordering/dependency waits. */
        const val GROUP_KEY = "group_key"

        /** Deterministic write key used for server replay/idempotency. */
        const val IDEMPOTENCY_KEY = "idempotency_key"

        /** Referenced proof upload outbox row id for dependent register/submit writes. */
        const val PROOF_OUTBOX_ITEM_ID = "proof_outbox_item_id"

        /** How many attempts that queued write is allowed before it is declared dead. */
        const val MAX_ATTEMPTS = "max_attempts"
        const val MIME_TYPE = "mime_type"
        const val WATCH_TIME_MS = "watch_time_ms"
        const val DURATION_MS = "duration_ms"
        const val POSITION_MS = "position_ms"
        const val PERCENT_WATCHED = "percent_watched"
        const val SEEK_COUNT = "seek_count"
        const val REPLAY_COUNT = "replay_count"
        const val BUFFERING_TIME_MS = "buffering_time_ms"

        /** Deepest row index a queue LazyColumn scrolled to during one screen visit. */
        const val MAX_SCROLL_INDEX = "max_scroll_index"

        /** How many rows were loaded in the queue at the moment of a scroll/load-more summary. */
        const val ROW_COUNT = "row_count"

        /** A drive-close batch id (`VerifyDriveClosure.batchId`) — bounded-cardinality within one
         *  drive, never a goat/shed id. */
        const val BATCH_ID = "batch_id"

        /**
         * Which dimension a filter/grouping event refers to (`park`/`shed`/`breed`/`all`).
         *
         * Deliberately the dimension NAME, not the selected value: a park or shed id is livestock
         * operations data rather than PII, but the analytics question here is "which slices do
         * operators reach for", which the name answers and the id only bloats.
         */
        const val DIMENSION = "dimension"

        /**
         * This install's stable device id (`DeviceStore.appInstallId()`), stamped onto every event
         * by [FirebaseAnalyticsAdapter.track] from [AnalyticsContext.deviceId] so the same login on
         * two phones is distinguishable per-event (the user-scoped [UserProps.DEVICE_ID] is
         * last-write-wins and only reflects the current device).
         */
        const val DEVICE_ID = "device_id"
        const val EMAIL = "email"

        /**
         * Value is `auth_uid`, NOT `firebase_uid` — Firebase RESERVES the `firebase_` param
         * prefix and silently drops any param whose name starts with it (the same failure mode
         * as the [SESSION_START] reserved-event-name incident this project already had; see the
         * telemetry guard's `reserved_names` check). The Kotlin constant keeps its old name so
         * every call site below is untouched; only the wire value changed.
         */
        const val FIREBASE_UID = "auth_uid"

        /**
         * This work session's stable journey id (`DeviceStore.journeyId()`), stamped onto every
         * event by [FirebaseAnalyticsAdapter.track] from [AnalyticsContext.journeyId] — mirrors
         * [DEVICE_ID]'s stamping mechanism exactly. Unlike Firebase's built-in 30-minute
         * auto-session, this survives process death and spans a whole login-to-logout work
         * session (a vaccination or weighing drive can run for hours), so a single operator's
         * continuous work is reconstructible end-to-end instead of fragmenting into unrelated
         * auto-sessions.
         */
        const val JOURNEY_ID = "journey_id"

        /** Bounded outcome of a read/attempt (`success_slots`/`success_empty`/`failed`/
         *  `retry_exhausted`/`submitted`/`blocked`, depending on the event). */
        const val RESULT = "result"

        /** Bitmask-style label of which teammate-captured slots came back (`weight`/`feed`/
         *  `water`/`weight_feed`/`weight_water`/`feed_water`/`all`/`none`). */
        const val SLOT_MASK = "slot_mask"

        /** How many retries a bounded retry ladder has attempted so far. */
        const val RETRY_COUNT = "retry_count"

        /** What triggered a read or a status change (`open`/`sync_tap`/`retry`/`server_poll`/
         *  `room`, depending on the event). */
        const val SOURCE = "source"

        /** Whether the local slot was empty or already locally captured when a teammate ref
         *  arrived (`empty`/`local_present`). */
        const val LOCAL_SLOT_STATE = "local_slot_state"

        /** Where the feed-weight-photo slot's submitted value came from (`local_outbox`/
         *  `server_ref`/`missing`). */
        const val FEED_WEIGHT_SOURCE = "feed_weight_source"

        /** Where the feed-video slot's submitted value came from (`local_outbox`/`server_ref`/
         *  `missing`). */
        const val FEED_VIDEO_SOURCE = "feed_video_source"

        /** Where the water-video slot's submitted value came from (`local_outbox`/`server_ref`/
         *  `missing`). */
        const val WATER_VIDEO_SOURCE = "water_video_source"

        /** Editable/read-only state before a status change (`editable`/`readonly`). */
        const val PREVIOUS = "previous"

        /** Editable/read-only state after a status change (`editable`/`readonly`). */
        const val NEXT = "next"

        /** Coarse lifecycle bucket backing a status change (`open`/`pending_verification`/
         *  `submitted`/`unknown`). */
        const val STATUS = "status"
    }

    /** Durable user-property keys (set via [AnalyticsPort.setUserProperty]). */
    object UserProps {
        const val ROLE = "role"

        /** The signed-in user's email (Firebase Auth). Business-owner decision: this is the primary
         *  user identity dimension for segmentation. */
        const val EMAIL = "email"

        /** This install's stable device id (`DeviceStore.appInstallId()`). GA4 scopes a user
         *  property to the user, so this is LAST-WRITE-WINS — it reflects the device the principal
         *  most recently bootstrapped on. For per-event device attribution use [Params.DEVICE_ID]. */
        const val DEVICE_ID = "device_id"

        /** Display label (e.g. "Park A") — kept for backward compatibility with existing
         *  dashboards. Prefer [PARK_ID] (a stable id) for new analytics/segmentation. */
        const val PRIMARY_PARK = "primary_park"

        /** Stable park identifier (`BootstrapOperatorProfileDto.primaryLocationId`) — unlike
         *  [PRIMARY_PARK]'s display label, this never changes if the park is renamed. */
        const val PARK_ID = "park_id"
        const val FLAVOR = "flavor"

        /** The authenticated principal's tenant id (`BootstrapActorDto.tenantId`, via
         *  [sg.mesha.goatos.core.data.BootstrapRepository.actorTenantId]). */
        const val TENANT = "tenant"
    }
}
