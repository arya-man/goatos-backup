package sg.mesha.goatos.core.analytics

/**
 * Event/param constants added for the Verification + Vaccination/Calendar + Alerts
 * full-journey instrumentation pass.
 *
 * Deliberately a SEPARATE file from [AnalyticsEvents]/[AnalyticsFunnels] — those two files are
 * owned by a parallel session working the same module concurrently; adding new constants here
 * avoids a merge collision while keeping the same `snake_case` / typed-constant idiom (see
 * [AnalyticsEvents] for the rationale). Call sites should prefer these constants exactly like the
 * ones in [AnalyticsEvents] — never an inline string.
 */
object AnalyticsEventsVerification {

    /**
     * Approve was rendered DISABLED because this animal's evidence could not be confirmed
     * watchable — [AnalyticsFunnels.Params.REASON] carries the
     * `VerifyDecisionUnavailableReason` name (`EVIDENCE_UNAVAILABLE`). This is the moment the
     * verifier is stuck looking at a dead Approve button with no path forward on this item; it is
     * genuinely reachable state, not a bug, but until now nothing recorded that it happened.
     * Fired ONCE per item id transitioning into this state (see
     * `VerifyDetailViewModel.trackDecisionUnavailable`) — never once per recomposition.
     */
    const val VERIFY_DECISION_UNAVAILABLE = "verify_decision_unavailable"

    /**
     * The Alerts screen (vaccination control-tower summary, `AlertsViewModel`) rendered its
     * first non-loading state. Previously nothing was emitted for this screen at all —
     * indistinguishable from "the operator never opened Alerts".
     */
    const val ALERTS_VIEWED = "alerts_viewed"

    /** An alert row was tapped (marks it read locally — there is no per-alert detail screen). */
    const val ALERT_TAPPED = "alert_tapped"

    /** "Mark all read" was tapped — the only bulk-dismiss affordance this screen has. */
    const val ALERT_MARK_ALL_READ = "alert_mark_all_read"

    /** An Alerts refresh (initial load or pull-to-refresh) was attempted. */
    const val ALERTS_REFRESH_ATTEMPTED = "alerts_refresh_attempted"

    /** An Alerts refresh landed successfully. */
    const val ALERTS_REFRESH_SUCCEEDED = "alerts_refresh_succeeded"

    /** An Alerts refresh failed; cached Room data remains visible when present. */
    const val ALERTS_REFRESH_FAILED = "alerts_refresh_failed"

    object Params {
        /** Which alert row an event refers to (`ControlTowerAlert.rowId`) — bounded-cardinality
         *  operational id, not PII. */
        const val ALERT_ID = "alert_id"

        /** How many rows were unread at the moment "mark all read" was tapped. */
        const val UNREAD_COUNT = "unread_count"

        /** How many rows were on screen when Alerts was viewed / refreshed. */
        const val ROW_COUNT = "row_count"

        const val REASON = "reason"
    }
}
