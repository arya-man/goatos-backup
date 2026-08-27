package sg.mesha.goatos.core.analytics

/**
 * Journey/funnel instrumentation for the mobile funnel in
 * `docs/observability/OBSERVABILITY_DESIGN.md` §2.5:
 * **login → bootstrap → drive-open → scan → vaccination-capture → submit**.
 *
 * `login` and `bootstrap` are ALREADY wired via the existing [AnalyticsEvents.LOGIN_ATTEMPT] /
 * [AnalyticsEvents.LOGIN_SUCCESS] / [AnalyticsEvents.LOGIN_FAILURE] (`boot/SessionViewModel.kt`)
 * and [AnalyticsEvents.BOOTSTRAP_LOADED] (`boot/BootstrapViewModel.kt`) calls — this file adds
 * the three later stages (`drive-open`, `scan`, `vaccination-capture`, `submit`) as typed helpers
 * so the owning feature call sites can wire them with one line each. See `docs/TELEMETRY.md` for
 * the exact remaining call sites (feature-calendar, feature-scan, feature-record,
 * feature-submit) — none are added here to stay out of the parallel session's feature-viewmodel
 * changes.
 *
 * Stable public API grepped by the telemetry CI guardrail (see `docs/TELEMETRY.md`) — do not
 * rename without updating that doc and the guard.
 */
object AnalyticsFunnels {

    /** Funnel event names. `LOGIN_*`/`BOOTSTRAP_LOADED` alias the existing [AnalyticsEvents]
     *  constants so a dashboard funnel can treat this object as the single source for all six
     *  stages, even though the first two are emitted by pre-existing call sites. */
    object Events {
        const val LOGIN_ATTEMPT: String = AnalyticsEvents.LOGIN_ATTEMPT
        const val LOGIN_SUCCESS: String = AnalyticsEvents.LOGIN_SUCCESS
        const val LOGIN_FAILURE: String = AnalyticsEvents.LOGIN_FAILURE
        const val BOOTSTRAP_LOADED: String = AnalyticsEvents.BOOTSTRAP_LOADED

        const val DRIVE_OPEN: String = "funnel_drive_open"
        const val SCAN_STARTED: String = "funnel_scan_started"
        const val SCAN_COMPLETED: String = "funnel_scan_completed"
        const val VACCINATION_CAPTURE_STARTED: String = "funnel_vaccination_capture_started"
        const val VACCINATION_CAPTURE_COMPLETED: String = "funnel_vaccination_capture_completed"
        const val SUBMIT_ATTEMPTED: String = "funnel_submit_attempted"
        const val SUBMIT_SUCCEEDED: String = "funnel_submit_succeeded"
        const val SUBMIT_FAILED: String = "funnel_submit_failed"
        const val SUBMIT_STATUS: String = "submit_status"

        /** The operator tapped a disabled/blocked Submit — the client-side readiness gate refused
         *  BEFORE anything reached the outbox, so [SUBMIT_ATTEMPTED] never fires. Previously this
         *  was a dead button with nothing recorded. */
        const val SUBMIT_BLOCKED: String = AnalyticsEvents.SUBMIT_BLOCKED

        // Standalone Verifier section funnel (context/architecture/verifier-app-and-flow.md):
        // login → bootstrap → verify-queue-opened → verify-item-opened → verify-verdict-submitted.
        const val VERIFY_QUEUE_OPENED: String = "funnel_verify_queue_opened"
        const val VERIFY_ITEM_OPENED: String = "funnel_verify_item_opened"
        const val VERIFY_VERDICT_ATTEMPTED: String = "funnel_verify_verdict_attempted"
        const val VERIFY_VERDICT_SUCCEEDED: String = "funnel_verify_verdict_succeeded"
        const val VERIFY_VERDICT_FAILED: String = "funnel_verify_verdict_failed"
        const val VERIFY_VIDEO_PLAY_STARTED: String = AnalyticsEvents.VERIFY_VIDEO_PLAY_STARTED
        const val VERIFY_VIDEO_WATCH_SUMMARY: String = AnalyticsEvents.VERIFY_VIDEO_WATCH_SUMMARY
        const val VERIFY_VIDEO_PLAY_INTENT: String = AnalyticsEvents.VERIFY_VIDEO_PLAY_INTENT
        const val VERIFY_VIDEO_PLAY_DEAD: String = AnalyticsEvents.VERIFY_VIDEO_PLAY_DEAD
    }

    /** Event parameter keys used by the helpers below. */
    object Params {
        const val DRIVE_ID: String = "drive_id"
        const val PARK_ID: String = "park_id"
        const val SHED_ID: String = "shed_id"
        const val TASK_ID: String = "task_id"
        const val OUTCOME: String = "outcome"
        const val REASON: String = "reason"
        const val SCANNED_COUNT: String = "scanned_count"
        const val CATEGORY: String = "category"
        const val ITEM_ID: String = "item_id"
        const val DECISION: String = "decision"
        const val PROOF_ID: String = "proof_id"
        const val MIME_TYPE: String = "mime_type"
        const val WATCH_TIME_MS: String = "watch_time_ms"
        const val TOTAL_WATCH_TIME_MS: String = "total_watch_time_ms"
        const val DURATION_MS: String = "duration_ms"
        const val POSITION_MS: String = "position_ms"
        const val PERCENT_WATCHED: String = "percent_watched"
        const val SEEK_COUNT: String = "seek_count"
        const val REPLAY_COUNT: String = "replay_count"
        const val BUFFERING_TIME_MS: String = "buffering_time_ms"
        const val DIMENSION: String = AnalyticsEvents.Params.DIMENSION
        const val ACTION: String = AnalyticsEvents.Params.ACTION
        const val MAX_SCROLL_INDEX: String = AnalyticsEvents.Params.MAX_SCROLL_INDEX
        const val ROW_COUNT: String = AnalyticsEvents.Params.ROW_COUNT
        const val BATCH_ID: String = AnalyticsEvents.Params.BATCH_ID
        const val PLAYER_STATE: String = "player_state"
        const val ARMED: String = "armed"
        const val TARGET_ACTION: String = "target_action"
        const val SUBMIT_STATUS: String = "submit_status"
        const val ATTEMPT_COUNT: String = "attempt_count"
        const val MAX_ATTEMPTS: String = "max_attempts"
    }

    private fun safeTrack(analytics: AnalyticsPort, event: String, props: Map<String, String> = emptyMap()) {
        runCatching { analytics.track(event, props) }
    }

    /** Call from wherever a vaccination drive is opened (a calendar/drive-list row tap) — see
     *  `docs/TELEMETRY.md` for the exact feature-calendar call site. */
    fun trackDriveOpened(analytics: AnalyticsPort, driveId: String, parkId: String? = null) {
        safeTrack(
            analytics,
            Events.DRIVE_OPEN,
            buildMap {
                put(Params.DRIVE_ID, driveId)
                parkId?.let { put(Params.PARK_ID, it) }
            },
        )
    }

    /** Call when a shed's RFID scan step begins — see `docs/TELEMETRY.md` for the feature-scan
     *  call site (`ScanViewModel`, owned by a parallel session; not wired here). */
    fun trackScanStarted(analytics: AnalyticsPort, shedId: String) {
        safeTrack(analytics, Events.SCAN_STARTED, mapOf(Params.SHED_ID to shedId))
    }

    /** Call when a shed's RFID scan step finishes (success or abandon). */
    fun trackScanCompleted(analytics: AnalyticsPort, shedId: String, scannedCount: Int) {
        safeTrack(
            analytics,
            Events.SCAN_COMPLETED,
            mapOf(Params.SHED_ID to shedId, Params.SCANNED_COUNT to scannedCount.toString()),
        )
    }

    /** Call when vaccination-capture (record) starts for a task — see `docs/TELEMETRY.md` for the
     *  feature-record call site (`RecordViewModel`, owned by a parallel session; not wired here). */
    fun trackVaccinationCaptureStarted(analytics: AnalyticsPort, taskId: String) {
        safeTrack(analytics, Events.VACCINATION_CAPTURE_STARTED, mapOf(Params.TASK_ID to taskId))
    }

    /** Call when vaccination-capture completes; [outcome] e.g. `done`/`skipped`. */
    fun trackVaccinationCaptureCompleted(analytics: AnalyticsPort, taskId: String, outcome: String) {
        safeTrack(
            analytics,
            Events.VACCINATION_CAPTURE_COMPLETED,
            mapOf(Params.TASK_ID to taskId, Params.OUTCOME to outcome),
        )
    }

    /** Call when the final submission is attempted — see `docs/TELEMETRY.md` for the
     *  feature-submit call site. */
    fun trackSubmitAttempted(analytics: AnalyticsPort, taskId: String) {
        safeTrack(analytics, Events.SUBMIT_ATTEMPTED, mapOf(Params.TASK_ID to taskId))
    }

    fun trackSubmitSucceeded(analytics: AnalyticsPort, taskId: String) {
        safeTrack(analytics, Events.SUBMIT_SUCCEEDED, mapOf(Params.TASK_ID to taskId))
    }

    fun trackSubmitFailed(analytics: AnalyticsPort, taskId: String, reason: String) {
        safeTrack(analytics, Events.SUBMIT_FAILED, mapOf(Params.TASK_ID to taskId, Params.REASON to reason))
    }

    fun trackSubmitStatus(
        analytics: AnalyticsPort,
        taskId: String,
        status: String,
        reason: String? = null,
        attemptCount: Int = 0,
        maxAttempts: Int = 0,
    ) {
        safeTrack(
            analytics,
            Events.SUBMIT_STATUS,
            buildMap {
                put(Params.TASK_ID, taskId)
                put(Params.SUBMIT_STATUS, status)
                if (!reason.isNullOrBlank()) put(Params.REASON, reason.take(80))
                if (attemptCount > 0) put(Params.ATTEMPT_COUNT, attemptCount.toString())
                if (maxAttempts > 0) put(Params.MAX_ATTEMPTS, maxAttempts.toString())
            },
        )
    }

    /** Call from [submit]'s early-return gates when the client-side readiness check refuses a
     *  tap — [reason] is the coarse blocking cause (e.g. a `blockedReason` field name or
     *  `shed_not_ready`), never a full form-field label. */
    fun trackSubmitBlocked(analytics: AnalyticsPort, taskId: String, reason: String) {
        safeTrack(analytics, Events.SUBMIT_BLOCKED, mapOf(Params.TASK_ID to taskId, Params.REASON to reason))
    }

    // --- Standalone Verifier section (VerifyQueueViewModel / VerifyDetailViewModel, :app) ----

    /** Call when the Verifier queue screen first loads/refreshes for a category scope. */
    fun trackVerifyQueueOpened(analytics: AnalyticsPort, category: String?) {
        safeTrack(analytics, Events.VERIFY_QUEUE_OPENED, buildMap { category?.let { put(Params.CATEGORY, it) } })
    }

    /** Call when a queue row is tapped and the detail screen opens. */
    fun trackVerifyItemOpened(analytics: AnalyticsPort, itemId: String, category: String) {
        val props = mapOf(Params.ITEM_ID to itemId, Params.CATEGORY to category)
        safeTrack(analytics, AnalyticsEvents.VERIFY_ITEM_OPENED, props)
        safeTrack(analytics, Events.VERIFY_ITEM_OPENED, props)
    }

    /** Call when Approve/Reject is tapped, before the outbox enqueue. [decision] is
     *  `"approved"`/`"rejected"` ([sg.mesha.goatos.core.network.dto.VerificationDecision]). */
    fun trackVerifyVerdictAttempted(analytics: AnalyticsPort, itemId: String, decision: String, watchTimeMs: Long = 0) {
        safeTrack(
            analytics,
            Events.VERIFY_VERDICT_ATTEMPTED,
            buildMap {
                put(Params.ITEM_ID, itemId)
                put(Params.DECISION, decision)
                put(Params.TOTAL_WATCH_TIME_MS, watchTimeMs.coerceAtLeast(0).toString())
            },
        )
    }

    /** Call once the verdict is durably queued to the outbox (optimistic — see SyncRepository). */
    fun trackVerifyVerdictSucceeded(analytics: AnalyticsPort, itemId: String, decision: String, watchTimeMs: Long = 0) {
        safeTrack(
            analytics,
            Events.VERIFY_VERDICT_SUCCEEDED,
            buildMap {
                put(Params.ITEM_ID, itemId)
                put(Params.DECISION, decision)
                put(Params.TOTAL_WATCH_TIME_MS, watchTimeMs.coerceAtLeast(0).toString())
            },
        )
    }

    /** Call when the verdict could not even be queued (e.g. a local storage error). */
    fun trackVerifyVerdictFailed(analytics: AnalyticsPort, itemId: String, decision: String, reason: String) {
        safeTrack(
            analytics,
            Events.VERIFY_VERDICT_FAILED,
            mapOf(Params.ITEM_ID to itemId, Params.DECISION to decision, Params.REASON to reason),
        )
    }

    /**
     * Intent/outcome watchdog for the verify proof-video play/pause control (inline AND
     * fullscreen). 1500ms: media3 with an already-buffered short proof clip (these are seconds
     * long, not minutes — see `VideoTimeReadout`) starts within a couple hundred ms on a
     * reasonable connection; 1500ms comfortably absorbs a cold decoder spin-up
     * (`player.prepare()` on first tap) or a brief network stall without false-positiving, while
     * still being short enough that the report is useful — a verifier will already have
     * re-tapped or complained well before a longer window would fire. Call [DeadControlWatchdog.armIntent]
     * from the click handler and [DeadControlWatchdog.disarm] from `onIsPlayingChanged`.
     */
    const val VERIFY_VIDEO_PLAY_WATCHDOG_TIMEOUT_MS: Long = 1_500L

    fun newVerifyVideoPlayWatchdog(
        analytics: AnalyticsPort,
        crashReporter: CrashReporter,
        scope: kotlinx.coroutines.CoroutineScope,
    ): DeadControlWatchdog = DeadControlWatchdog(
        analytics = analytics,
        crashReporter = crashReporter,
        scope = scope,
        intentEvent = Events.VERIFY_VIDEO_PLAY_INTENT,
        deadControlEvent = Events.VERIFY_VIDEO_PLAY_DEAD,
    )

    fun trackVerifyVideoPlayStarted(
        analytics: AnalyticsPort,
        itemId: String,
        proofId: String,
        mimeType: String,
        durationMs: Long,
    ) {
        safeTrack(
            analytics,
            Events.VERIFY_VIDEO_PLAY_STARTED,
            mapOf(
                Params.ITEM_ID to itemId,
                Params.PROOF_ID to proofId,
                Params.MIME_TYPE to mimeType,
                Params.DURATION_MS to durationMs.toString(),
            ),
        )
    }

    fun trackVerifyVideoWatchSummary(
        analytics: AnalyticsPort,
        itemId: String,
        proofId: String,
        mimeType: String,
        watchTimeMs: Long,
        durationMs: Long,
        positionMs: Long,
        percentWatched: Int,
        seekCount: Int,
        replayCount: Int,
        bufferingTimeMs: Long,
    ) {
        safeTrack(
            analytics,
            Events.VERIFY_VIDEO_WATCH_SUMMARY,
            mapOf(
                Params.ITEM_ID to itemId,
                Params.PROOF_ID to proofId,
                Params.MIME_TYPE to mimeType,
                Params.WATCH_TIME_MS to watchTimeMs.toString(),
                Params.DURATION_MS to durationMs.toString(),
                Params.POSITION_MS to positionMs.toString(),
                Params.PERCENT_WATCHED to percentWatched.toString(),
                Params.SEEK_COUNT to seekCount.toString(),
                Params.REPLAY_COUNT to replayCount.toString(),
                Params.BUFFERING_TIME_MS to bufferingTimeMs.toString(),
            ),
        )
    }

    fun trackVerifyVideoPlaybackError(analytics: AnalyticsPort, itemId: String, proofId: String, reason: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_VIDEO_PLAYBACK_ERROR,
            mapOf(Params.ITEM_ID to itemId, Params.PROOF_ID to proofId, Params.REASON to reason),
        )
    }

    /** Call when the fullscreen button is tapped on a proof video. */
    fun trackVerifyVideoFullscreenOpened(analytics: AnalyticsPort, itemId: String, proofId: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_VIDEO_FULLSCREEN_OPENED,
            mapOf(Params.ITEM_ID to itemId, Params.PROOF_ID to proofId),
        )
    }

    /** Call when the fullscreen proof-video dialog is dismissed (X, back, or scrim). */
    fun trackVerifyVideoFullscreenExited(analytics: AnalyticsPort, itemId: String, proofId: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_VIDEO_FULLSCREEN_EXITED,
            mapOf(Params.ITEM_ID to itemId, Params.PROOF_ID to proofId),
        )
    }

    fun trackVaccinationLeadershipVideoPlayStarted(
        analytics: AnalyticsPort,
        proofId: String,
        mimeType: String,
        durationMs: Long,
    ) {
        safeTrack(
            analytics,
            AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_PLAY_STARTED,
            mapOf(
                Params.PROOF_ID to proofId,
                Params.MIME_TYPE to mimeType,
                Params.DURATION_MS to durationMs.toString(),
            ),
        )
    }

    fun trackVaccinationLeadershipVideoWatchSummary(
        analytics: AnalyticsPort,
        proofId: String,
        mimeType: String,
        watchTimeMs: Long,
        durationMs: Long,
        positionMs: Long,
        percentWatched: Float,
        seekCount: Int,
        replayCount: Int,
        bufferingTimeMs: Long,
    ) {
        safeTrack(
            analytics,
            AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_WATCH_SUMMARY,
            mapOf(
                Params.PROOF_ID to proofId,
                Params.MIME_TYPE to mimeType,
                Params.WATCH_TIME_MS to watchTimeMs.toString(),
                Params.DURATION_MS to durationMs.toString(),
                Params.POSITION_MS to positionMs.toString(),
                Params.PERCENT_WATCHED to percentWatched.toInt().toString(),
                Params.SEEK_COUNT to seekCount.toString(),
                Params.REPLAY_COUNT to replayCount.toString(),
                Params.BUFFERING_TIME_MS to bufferingTimeMs.toString(),
            ),
        )
    }

    fun trackVaccinationLeadershipVideoPlaybackError(analytics: AnalyticsPort, proofId: String, reason: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VACCINATION_LEADERSHIP_VIDEO_PLAYBACK_ERROR,
            mapOf(Params.PROOF_ID to proofId, Params.REASON to reason),
        )
    }

    /** Call whenever a queue-scoping filter changes. [dimension] is `park`/`shed`/`module`/
     *  `category`; [action] is `set`/`cleared`, mirroring [AnalyticsEvents.COUNTS_FILTER_APPLIED]. */
    fun trackVerifyQueueFilterApplied(analytics: AnalyticsPort, dimension: String, action: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_QUEUE_FILTER_APPLIED,
            mapOf(Params.DIMENSION to dimension, Params.ACTION to action),
        )
    }

    /** Call once a keyset "load more" page has been appended into the Room-backed scope. */
    fun trackVerifyQueueLoadMore(analytics: AnalyticsPort, category: String, rowCount: Int) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_QUEUE_LOAD_MORE, // mobile-guard:ignore: analytics event name for auto-triggered keyset paging, not a tappable UI control
            mapOf(Params.CATEGORY to category, Params.ROW_COUNT to rowCount.toString()),
        )
    }

    /** Call ONCE per queue-screen exit with the deepest row index reached and how many rows were
     *  loaded — never per scroll frame (see [AnalyticsEvents.VERIFY_QUEUE_SCROLL_SUMMARY]). */
    fun trackVerifyQueueScrollSummary(analytics: AnalyticsPort, maxScrollIndex: Int, rowCount: Int) {
        if (rowCount <= 0) return
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_QUEUE_SCROLL_SUMMARY,
            mapOf(Params.MAX_SCROLL_INDEX to maxScrollIndex.toString(), Params.ROW_COUNT to rowCount.toString()),
        )
    }

    fun trackVerifyDriveCloseAttempted(analytics: AnalyticsPort, batchId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_DRIVE_CLOSE_ATTEMPTED, mapOf(Params.BATCH_ID to batchId))
    }

    fun trackVerifyDriveCloseSucceeded(analytics: AnalyticsPort, batchId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_DRIVE_CLOSE_SUCCEEDED, mapOf(Params.BATCH_ID to batchId))
    }

    fun trackVerifyDriveCloseFailed(analytics: AnalyticsPort, batchId: String, reason: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_DRIVE_CLOSE_FAILED,
            mapOf(Params.BATCH_ID to batchId, Params.REASON to reason),
        )
    }

    /** Call when the detail screen closes. [reason] is `fully_decided` or `abandoned` — see
     *  [AnalyticsEvents.VERIFY_ITEM_CLOSED]. */
    fun trackVerifyItemClosed(analytics: AnalyticsPort, itemId: String, reason: String) {
        safeTrack(
            analytics,
            AnalyticsEvents.VERIFY_ITEM_CLOSED,
            mapOf(Params.ITEM_ID to itemId, Params.REASON to reason),
        )
    }

    fun trackVerifyRejectDialogOpened(analytics: AnalyticsPort, itemId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_REJECT_DIALOG_OPENED, mapOf(Params.ITEM_ID to itemId))
    }

    fun trackVerifyRejectDialogCancelled(analytics: AnalyticsPort, itemId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_REJECT_DIALOG_CANCELLED, mapOf(Params.ITEM_ID to itemId))
    }

    fun trackVerifyRejectBlockedEmptyReason(analytics: AnalyticsPort, itemId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_REJECT_BLOCKED_EMPTY_REASON, mapOf(Params.ITEM_ID to itemId))
    }

    fun trackVerifyApproveDialogOpened(analytics: AnalyticsPort, itemId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_APPROVE_DIALOG_OPENED, mapOf(Params.ITEM_ID to itemId))
    }

    fun trackVerifyApproveDialogCancelled(analytics: AnalyticsPort, itemId: String) {
        safeTrack(analytics, AnalyticsEvents.VERIFY_APPROVE_DIALOG_CANCELLED, mapOf(Params.ITEM_ID to itemId))
    }
}
