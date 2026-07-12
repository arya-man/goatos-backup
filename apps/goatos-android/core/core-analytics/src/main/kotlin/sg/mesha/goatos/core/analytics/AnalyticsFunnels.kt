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
    }

    /** Call from wherever a vaccination drive is opened (a calendar/drive-list row tap) — see
     *  `docs/TELEMETRY.md` for the exact feature-calendar call site. */
    fun trackDriveOpened(analytics: AnalyticsPort, driveId: String, parkId: String? = null) {
        analytics.track(
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
        analytics.track(Events.SCAN_STARTED, mapOf(Params.SHED_ID to shedId))
    }

    /** Call when a shed's RFID scan step finishes (success or abandon). */
    fun trackScanCompleted(analytics: AnalyticsPort, shedId: String, scannedCount: Int) {
        analytics.track(
            Events.SCAN_COMPLETED,
            mapOf(Params.SHED_ID to shedId, Params.SCANNED_COUNT to scannedCount.toString()),
        )
    }

    /** Call when vaccination-capture (record) starts for a task — see `docs/TELEMETRY.md` for the
     *  feature-record call site (`RecordViewModel`, owned by a parallel session; not wired here). */
    fun trackVaccinationCaptureStarted(analytics: AnalyticsPort, taskId: String) {
        analytics.track(Events.VACCINATION_CAPTURE_STARTED, mapOf(Params.TASK_ID to taskId))
    }

    /** Call when vaccination-capture completes; [outcome] e.g. `done`/`skipped`. */
    fun trackVaccinationCaptureCompleted(analytics: AnalyticsPort, taskId: String, outcome: String) {
        analytics.track(
            Events.VACCINATION_CAPTURE_COMPLETED,
            mapOf(Params.TASK_ID to taskId, Params.OUTCOME to outcome),
        )
    }

    /** Call when the final submission is attempted — see `docs/TELEMETRY.md` for the
     *  feature-submit call site. */
    fun trackSubmitAttempted(analytics: AnalyticsPort, taskId: String) {
        analytics.track(Events.SUBMIT_ATTEMPTED, mapOf(Params.TASK_ID to taskId))
    }

    fun trackSubmitSucceeded(analytics: AnalyticsPort, taskId: String) {
        analytics.track(Events.SUBMIT_SUCCEEDED, mapOf(Params.TASK_ID to taskId))
    }

    fun trackSubmitFailed(analytics: AnalyticsPort, taskId: String, reason: String) {
        analytics.track(Events.SUBMIT_FAILED, mapOf(Params.TASK_ID to taskId, Params.REASON to reason))
    }
}
