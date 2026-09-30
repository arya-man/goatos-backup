package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

/**
 * HRMS enquiries and violations (maintainer decisions 2026-09-30). An approved death opens an
 * ENQUIRY for the park head of that animal's park: they answer the questions its pinned HRMS SOP
 * version asks, name each person responsible with a violation and fine (or nobody) and submit --
 * the violations are recorded with the report. A park head may also RECORD a violation directly
 * against someone whose home park they head. Every list (violation types, fines, questions,
 * deadline) is authored on the web's HRMS SOP; the phone renders what the backend sends.
 *
 * ALL visible copy (`title`, `subject_label`, `status_label`, `*_label`, question `title`) is
 * BACKEND-OWNED and rendered verbatim. Wire contract of record: backend/internal/workforce/domain
 * discipline_types.go (contracts/openapi/app-api.yaml: Enquiry*, Violation*).
 */
@Serializable
data class EnquiryDto(
    @SerialName("enquiry_id") val enquiryId: String = "",
    @SerialName("trigger_key") val triggerKey: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("subject_label") val subjectLabel: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("occurred_at_label") val occurredAtLabel: String = "",
    @SerialName("opened_at_label") val openedAtLabel: String = "",
    @SerialName("due_at") val dueAt: String = "",
    @SerialName("due_at_label") val dueAtLabel: String = "",
    /** `open` | `submitted`. */
    @SerialName("status") val status: String = "",
    @SerialName("status_label") val statusLabel: String = "",
    @SerialName("overdue") val overdue: Boolean = false,
    @SerialName("submitted_by_name") val submittedByName: String = "",
    @SerialName("submitted_at_label") val submittedAtLabel: String = "",
    @SerialName("penalty_count") val penaltyCount: Int = 0,
    @SerialName("penalty_label") val penaltyLabel: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
)

@Serializable
data class EnquirySummaryDto(
    @SerialName("open") val open: Int = 0,
    @SerialName("overdue") val overdue: Int = 0,
    @SerialName("submitted") val submitted: Int = 0,
)

@Serializable
data class ViolationParkDto(
    @SerialName("park_id") val parkId: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class EnquiryPageDto(
    @SerialName("parks") val parks: List<ViolationParkDto> = emptyList(),
    @SerialName("park_id") val parkId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("summary") val summary: EnquirySummaryDto = EnquirySummaryDto(),
    @SerialName("items") val items: List<EnquiryDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
)

@Serializable
data class EnquiryQuestionDto(
    @SerialName("id") val id: String = "",
    /** `text` | `yes_no`. */
    @SerialName("kind") val kind: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("required") val required: Boolean = false,
)

/** An authored violation type. It carries no fine: a mistake and its money are separate. */
@Serializable
data class ViolationTypeOptionDto(
    @SerialName("key") val key: String = "",
    @SerialName("title") val title: String = "",
)

/** One month the violations list can show ("Sep 2026": a month heading, not a date). */
@Serializable
data class MonthOptionDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class ViolationPersonOptionDto(
    @SerialName("person_id") val personId: String = "",
    @SerialName("name") val name: String = "",
    @SerialName("designation") val designation: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_label") val parkLabel: String = "",
)

@Serializable
data class ViolationDto(
    @SerialName("violation_id") val violationId: String = "",
    @SerialName("person_id") val personId: String = "",
    @SerialName("person_name") val personName: String = "",
    @SerialName("designation") val designation: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("type_key") val typeKey: String = "",
    @SerialName("type_label") val typeLabel: String = "",
    @SerialName("fine_rupees") val fineRupees: Int = 0,
    @SerialName("fine_label") val fineLabel: String = "",
    @SerialName("occurred_on") val occurredOn: String = "",
    @SerialName("occurred_on_label") val occurredOnLabel: String = "",
    @SerialName("note") val note: String = "",
    @SerialName("source_label") val sourceLabel: String = "",
    @SerialName("recorded_by_name") val recordedByName: String = "",
    @SerialName("recorded_at_label") val recordedAtLabel: String = "",
    @SerialName("withdraw_reason") val withdrawReason: String = "",
    /** "late" / "absent" on an automatic clock-in violation (2026-09-30), "" otherwise. */
    @SerialName("attendance_kind") val attendanceKind: String = "",
    @SerialName("detail") val detail: String = "",
    @SerialName("decision_note") val decisionNote: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("status_label") val statusLabel: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
)

@Serializable
data class EnquiryDetailDto(
    @SerialName("enquiry") val enquiry: EnquiryDto = EnquiryDto(),
    @SerialName("questions") val questions: List<EnquiryQuestionDto> = emptyList(),
    @SerialName("answers") val answers: JsonObject = JsonObject(emptyMap()),
    @SerialName("violations") val violations: List<ViolationDto> = emptyList(),
    @SerialName("types") val types: List<ViolationTypeOptionDto> = emptyList(),
    @SerialName("people") val people: List<ViolationPersonOptionDto> = emptyList(),
    @SerialName("can_submit") val canSubmit: Boolean = false,
    @SerialName("sop_version") val sopVersion: Int = 0,
)

@Serializable
data class EnquiryPenaltyDto(
    @SerialName("person_id") val personId: String,
    @SerialName("type_key") val typeKey: String,
    @SerialName("fine_rupees") val fineRupees: Int? = null,
    @SerialName("note") val note: String = "",
)

@Serializable
data class SubmitEnquiryRequestDto(
    /** Question id -> answer (a string for text, a boolean for yes/no). */
    @SerialName("answers") val answers: JsonObject,
    @SerialName("penalties") val penalties: List<EnquiryPenaltyDto>,
    @SerialName("row_version") val rowVersion: Int,
)

@Serializable
data class ViolationSummaryDto(
    @SerialName("count") val count: Int = 0,
    @SerialName("fine_rupees") val fineRupees: Int = 0,
    @SerialName("fine_label") val fineLabel: String = "",
    @SerialName("people") val people: Int = 0,
    /** Automatic clock-in violations still waiting for HR in the period. */
    @SerialName("pending") val pending: Int = 0,
)

@Serializable
data class ViolationsPageDto(
    @SerialName("parks") val parks: List<ViolationParkDto> = emptyList(),
    @SerialName("months") val months: List<MonthOptionDto> = emptyList(),
    @SerialName("month") val month: String = "",
    @SerialName("summary") val summary: ViolationSummaryDto = ViolationSummaryDto(),
    @SerialName("items") val items: List<ViolationDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    @SerialName("types") val types: List<ViolationTypeOptionDto> = emptyList(),
    @SerialName("people") val people: List<ViolationPersonOptionDto> = emptyList(),
    @SerialName("sop_version") val sopVersion: Int = 0,
)

@Serializable
data class RecordViolationRequestDto(
    @SerialName("person_id") val personId: String,
    @SerialName("type_key") val typeKey: String,
    @SerialName("fine_rupees") val fineRupees: Int? = null,
    /** YYYY-MM-DD (a wire value; never displayed). */
    @SerialName("occurred_on") val occurredOn: String,
    @SerialName("note") val note: String = "",
    @SerialName("idempotency_key") val idempotencyKey: String = "",
)

@Serializable
data class ViolationResponseDto(
    @SerialName("violation") val violation: ViolationDto = ViolationDto(),
)
