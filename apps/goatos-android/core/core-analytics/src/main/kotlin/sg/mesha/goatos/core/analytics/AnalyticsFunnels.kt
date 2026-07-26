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

        // Standalone Verifier section funnel (context/architecture/verifier-app-and-flow.md):
        // login → bootstrap → verify-queue-opened → verify-item-opened → verify-verdict-submitted.
        const val VERIFY_QUEUE_OPENED: String = "funnel_verify_queue_opened"
        const val VERIFY_ITEM_OPENED: String = "funnel_verify_item_opened"
        const val VERIFY_VERDICT_ATTEMPTED: String = "funnel_verify_verdict_attempted"
        const val VERIFY_VERDICT_SUCCEEDED: String = "funnel_verify_verdict_succeeded"
        const val VERIFY_VERDICT_FAILED: String = "funnel_verify_verdict_failed"
        const val VERIFY_VIDEO_PLAY_STARTED: String = AnalyticsEvents.VERIFY_VIDEO_PLAY_STARTED
        const val VERIFY_VIDEO_WATCH_SUMMARY: String = AnalyticsEvents.VERIFY_VIDEO_WATCH_SUMMARY
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
}
