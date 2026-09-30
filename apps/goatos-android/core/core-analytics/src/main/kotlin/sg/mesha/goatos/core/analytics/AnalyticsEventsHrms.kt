package sg.mesha.goatos.core.analytics

/**
 * HRMS enquiries + violations on the phone (maintainer decisions 2026-09-30): a park head's
 * enquiry cards on the Tasks "For me" tab, the enquiry report, and recording a violation.
 * Params stay bounded: ids and reason codes only, never names or free text.
 */
object AnalyticsEventsHrms {
    /** The For me tab's enquiries section loaded; [AnalyticsEvents.Params.COUNT] = open enquiries. */
    const val ENQUIRIES_VIEWED = "hrms_enquiries_viewed"

    /** An enquiry card was tapped and its report opened. */
    const val ENQUIRY_OPENED = "hrms_enquiry_opened"

    /** The report was queued on the outbox; [AnalyticsEvents.Params.COUNT] = people penalised. */
    const val ENQUIRY_SUBMITTED = "hrms_enquiry_submitted"

    /** The park head's violations list was opened. */
    const val VIOLATIONS_VIEWED = "hrms_violations_viewed"

    /** The Record violation form was opened. */
    const val VIOLATION_FORM_OPENED = "hrms_violation_form_opened"

    /** A violation was queued on the outbox. */
    const val VIOLATION_RECORDED = "hrms_violation_recorded"

    /** Any read or write failed; [AnalyticsEvents.Params.REASON] carries the real message, truncated. */
    const val FAILURE = "hrms_failure"
}
