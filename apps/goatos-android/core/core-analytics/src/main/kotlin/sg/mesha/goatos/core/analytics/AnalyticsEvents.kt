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
    const val SESSION_START = "session_start"

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

    /** A weighing proof video's upload failed an attempt and will be re-tried. Emitted once per
     *  DISTINCT failure, not once per Room emission, so the funnel counts real attempts.
     *  [Params.SUBJECT_TYPE] separates the lump-sum shed video from the per-animal one,
     *  [Params.ATTEMPT] is the retry ordinal this session has seen, [Params.REASON] the coarse
     *  cause. Without this the retry loop was invisible: a shed proof re-uploaded for 15 minutes
     *  with nothing on screen but "uploading" and not one line in logcat (phone-QA 2026-08-03). */
    const val WEIGHING_PROOF_UPLOAD_RETRY = "weighing_proof_upload_retry"

    /** A weighing proof video's upload reached its terminal FAILED state, so the capture can
     *  never be submitted until the video is recorded again. Always paired with a Crashlytics
     *  non-fatal — this is the state that silently disables Submit. */
    const val WEIGHING_PROOF_UPLOAD_FAILED = "weighing_proof_upload_failed"

    /** Verifier opened a proof item detail screen that can stream evidence media. */
    const val VERIFY_ITEM_OPENED = "verify_item_opened"

    /** Verifier started playing one proof video. */
    const val VERIFY_VIDEO_PLAY_STARTED = "verify_video_play_started"

    /** Verifier ended a bounded playback session for one proof video. */
    const val VERIFY_VIDEO_WATCH_SUMMARY = "verify_video_watch_summary"

    /** Verifier video playback failed before a usable review could continue. */
    const val VERIFY_VIDEO_PLAYBACK_ERROR = "verify_video_playback_error"

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

    /** A Feed read (direction or packing) failed. [Params.KIND] is the surface, [Params.REASON] a
     *  coarse cause. */
    const val FEED_READ_FAILURE = "feed_read_failure"

    /** A Feed filter changed. [Params.DIMENSION] is `farm`/`shed`/`workflow`/`all`, [Params.ACTION]
     *  is `set`/`cleared`. */
    const val FEED_FILTER_APPLIED = "feed_filter_applied"

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

    /** The MANDATORY feed-distribution video was captured on the distribution detail. */
    const val FEED_DISTRIBUTION_VIDEO_CAPTURED = "feed_distribution_video_captured"

    /** The MANDATORY water-distribution proof was captured. [Params.KIND] is `photo`/`video`. */
    const val FEED_DISTRIBUTION_WATER_PROOF_CAPTURED = "feed_distribution_water_proof_captured"

    /** A feed-distribution completion was submitted for verification (both proofs queued, completion
     *  enqueued -> the shed-session moves to pending_verification). */
    const val FEED_DISTRIBUTION_SUBMITTED = "feed_distribution_submitted"

    /** A feed-distribution completion or proof could not be queued. [Params.REASON] a coarse cause. */
    const val FEED_DISTRIBUTION_FAILURE = "feed_distribution_failure"

    /** The verifier-gated feed-packing completion detail was opened (a Packing row tapped). */
    const val FEED_PACKING_COMPLETE_OPENED = "feed_packing_complete_opened"

    /** The MANDATORY packing video was captured on the packing completion detail. */
    const val FEED_PACKING_VIDEO_CAPTURED = "feed_packing_video_captured"

    /** A feed-packing completion was submitted for verification (proof queued, completion enqueued
     *  -> the shed-session moves to pending_verification). */
    const val FEED_PACKING_SUBMITTED = "feed_packing_submitted"

    /** A feed-packing completion or proof could not be queued. [Params.REASON] a coarse cause. */
    const val FEED_PACKING_COMPLETE_FAILURE = "feed_packing_complete_failure"

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

    /** Standard event parameter keys. */
    /**
     * The OS notification-permission prompt was shown. Until this existed, POST_NOTIFICATIONS was
     * never requested at all: FCM accepted every push, reported it delivered, and Android dropped
     * it silently. Whether operators actually see alerts is now measurable rather than assumed.
     */
    const val NOTIFICATION_PERMISSION_PROMPTED = "notification_permission_prompted"

    /** The prompt was answered; [Params.REASON] is "granted" or "denied". */
    const val NOTIFICATION_PERMISSION_RESULT = "notification_permission_result"

    object Params {
        const val METHOD = "method"
        const val REASON = "reason"
        const val CHROME = "chrome"
        const val ACTION = "action"
        const val SHED_ID = "shed_id"

        /** Stable backend module key from the bootstrap `modules` array. */
        const val MODULE_KEY = "module_key"

        /** Which Counts write/read a shared event refers to (`birth`/`death`/`shifting`/…). */
        const val KIND = "kind"

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
        /** What a proof is evidence OF (`shed` for a lump-sum group video, `other` for the
         *  per-animal one). Diagnosing a stuck upload starts with knowing which lane it is in. */
        const val SUBJECT_TYPE = "subject_type"
        /** 1-based ordinal of the upload attempt this session has observed for one proof. */
        const val ATTEMPT = "attempt"
        const val MIME_TYPE = "mime_type"
        const val WATCH_TIME_MS = "watch_time_ms"
        const val DURATION_MS = "duration_ms"
        const val POSITION_MS = "position_ms"
        const val PERCENT_WATCHED = "percent_watched"
        const val SEEK_COUNT = "seek_count"
        const val REPLAY_COUNT = "replay_count"
        const val BUFFERING_TIME_MS = "buffering_time_ms"

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
        const val FIREBASE_UID = "firebase_uid"
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
