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

    /** A sign-in failed; [Params.REASON] gives a coarse, non-PII cause. */
    const val LOGIN_FAILURE = "login_failure"

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

    /** The operator opened the "Awaiting RFID" list (goats carrying a temporary tag). */
    const val COUNTS_AWAITING_RFID_VIEWED = "counts_awaiting_rfid_viewed"

    /** The operator opened one temporary-tagged goat to promote it (the promote screen). */
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

        /** How an approval request was decided (`approved`/`rejected`). */
        const val DECISION = "decision"

        /**
         * Which dimension a filter/grouping event refers to (`park`/`shed`/`breed`/`all`).
         *
         * Deliberately the dimension NAME, not the selected value: a park or shed id is livestock
         * operations data rather than PII, but the analytics question here is "which slices do
         * operators reach for", which the name answers and the id only bloats.
         */
        const val DIMENSION = "dimension"
    }

    /** Durable user-property keys (set via [AnalyticsPort.setUserProperty]). */
    object UserProps {
        const val ROLE = "role"

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
