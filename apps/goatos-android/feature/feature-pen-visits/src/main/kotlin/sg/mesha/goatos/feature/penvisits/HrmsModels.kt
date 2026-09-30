package sg.mesha.goatos.feature.penvisits

// telemetry:exempt pure UI model declarations; the HRMS @HiltViewModels in :app own the
// hrms_* AnalyticsEventsHrms + CrashReporter wiring for every read, open and write.

import androidx.compose.runtime.Immutable

/**
 * HRMS on the Tasks "For me" tab (maintainer decisions 2026-09-30). An approved death opens an
 * ENQUIRY for the park head of that animal's park; they answer what the published HRMS SOP asks,
 * name anyone responsible with a violation and a fine -- or nobody -- and submit. A park head may
 * also record a violation directly against someone in the parks they head.
 *
 * Every business word (enquiry title, subject, park, dates, status, questions, violation types,
 * fines, people) is BACKEND copy carried verbatim. The client owns only its chrome.
 */

@Immutable
data class EnquiryCardUi(
    val listKey: String,
    val enquiryId: String,
    val title: String,
    val subjectLabel: String,
    val parkLabel: String,
    val dueLabel: String,
    val statusLabel: String,
    val overdue: Boolean,
)

/**
 * The HRMS part of the For me tab. [showEnquiries] / [showViolations] are the SERVER's answer
 * (a 403 hides the part), never a role check here.
 */
@Immutable
data class ForMeHrmsUi(
    val showEnquiries: Boolean = false,
    val enquiries: List<EnquiryCardUi> = emptyList(),
    /** The park's violations are this person's to see and record (the server's answer). */
    val showViolations: Boolean = false,
    /** The pen-visit list is not this person's (403): render the HRMS part alone, no pen error. */
    val penVisitsDenied: Boolean = false,
)

sealed interface ForMeHrmsEvent {
    data class OpenEnquiry(val enquiryId: String) : ForMeHrmsEvent
    data object OpenViolations : ForMeHrmsEvent
}

/** One option in a picker: a person or a violation type. [detail] is a quiet second line. */
@Immutable
data class ChoiceUi(
    val key: String,
    val label: String,
    val detail: String = "",
)

@Immutable
data class PenaltyDraftUi(
    val personId: String = "",
    val typeKey: String = "",
    /** Digits only; blank means no fine. A violation type never fills this in. */
    val fine: String = "",
    val note: String = "",
)

@Immutable
data class EnquiryQuestionUi(
    val id: String,
    /** `text` | `yes_no`. */
    val kind: String,
    val title: String,
    val required: Boolean,
    val text: String = "",
    val yes: Boolean? = null,
)

@Immutable
data class RecordedViolationUi(
    val listKey: String,
    val personName: String,
    val typeLabel: String,
    val fineLabel: String,
    val note: String,
)

@Immutable
data class EnquiryReportUiState(
    val loading: Boolean = true,
    val unavailable: Boolean = false,
    val title: String = "",
    val subjectLabel: String = "",
    val parkLabel: String = "",
    val happenedLabel: String = "",
    val openedLabel: String = "",
    val dueLabel: String = "",
    val statusLabel: String = "",
    val overdue: Boolean = false,
    /** True once the report is in: read only, what was answered and who was penalised. */
    val submitted: Boolean = false,
    val submittedLine: String = "",
    val questions: List<EnquiryQuestionUi> = emptyList(),
    val recorded: List<RecordedViolationUi> = emptyList(),
    val people: List<ChoiceUi> = emptyList(),
    val types: List<ChoiceUi> = emptyList(),
    val penalties: List<PenaltyDraftUi> = emptyList(),
    val submitting: Boolean = false,
    /** The server's refusal (or the local enqueue failure), verbatim. */
    val message: String = "",
    /** Queued while offline: it sends by itself when the phone is back online. */
    val queuedOffline: Boolean = false,
)

sealed interface EnquiryReportEvent {
    data object Back : EnquiryReportEvent
    data object Retry : EnquiryReportEvent
    data class AnswerText(val questionId: String, val value: String) : EnquiryReportEvent
    data class AnswerYesNo(val questionId: String, val value: Boolean) : EnquiryReportEvent
    data object AddPerson : EnquiryReportEvent
    data class RemovePerson(val index: Int) : EnquiryReportEvent
    data class SetPerson(val index: Int, val personId: String) : EnquiryReportEvent
    data class SetType(val index: Int, val typeKey: String) : EnquiryReportEvent
    data class SetFine(val index: Int, val value: String) : EnquiryReportEvent
    data class SetNote(val index: Int, val value: String) : EnquiryReportEvent
    data object Submit : EnquiryReportEvent
}

@Immutable
data class RecordViolationUiState(
    val loading: Boolean = true,
    val unavailable: Boolean = false,
    val people: List<ChoiceUi> = emptyList(),
    val types: List<ChoiceUi> = emptyList(),
    /** The days offered, newest first: key = wire date, label = DD/MM/YYYY. */
    val days: List<ChoiceUi> = emptyList(),
    val personId: String = "",
    val typeKey: String = "",
    val fine: String = "",
    val day: String = "",
    val note: String = "",
    val submitting: Boolean = false,
    val message: String = "",
    val done: Boolean = false,
    val queuedOffline: Boolean = false,
) {
    val canSubmit: Boolean get() = !submitting && personId.isNotBlank() && typeKey.isNotBlank() && day.isNotBlank()
}

sealed interface RecordViolationEvent {
    data object Back : RecordViolationEvent
    data object Retry : RecordViolationEvent
    data class SetPerson(val personId: String) : RecordViolationEvent
    data class SetType(val typeKey: String) : RecordViolationEvent
    data class SetFine(val value: String) : RecordViolationEvent
    data class SetDay(val day: String) : RecordViolationEvent
    data class SetNote(val value: String) : RecordViolationEvent
    data object Submit : RecordViolationEvent
    data object RecordAnother : RecordViolationEvent
}

/** One violation in the park head's list. Every word is backend copy, verbatim. */
@Immutable
data class ViolationRowUi(
    val listKey: String,
    val personName: String,
    val designation: String,
    val typeLabel: String,
    val fineLabel: String,
    val dateLabel: String,
    val sourceLabel: String,
    val recordedByName: String,
    val note: String,
    val statusLabel: String,
    val withdrawn: Boolean,
    val withdrawReason: String,
)

/** The park head's violations: one month at a time, newest first, ~20 rows a page. */
@Immutable
data class ViolationsListUiState(
    val loading: Boolean = true,
    val unavailable: Boolean = false,
    val isRefreshing: Boolean = false,
    val refreshFailed: Boolean = false,
    val lastSyncedAt: Long? = null,
    val months: List<ChoiceUi> = emptyList(),
    val month: String = "",
    /** Recorded violations in the month (withdrawn ones are listed but never counted). */
    val count: Int = 0,
    val fineLabel: String = "",
    val people: Int = 0,
    val rows: List<ViolationRowUi> = emptyList(),
    val loadingMore: Boolean = false,
    val canRecord: Boolean = false,
)

sealed interface ViolationsListEvent {
    data object Back : ViolationsListEvent
    data object Refresh : ViolationsListEvent
    data object LoadMore : ViolationsListEvent
    data object Record : ViolationsListEvent
    data class SelectMonth(val key: String) : ViolationsListEvent
}
