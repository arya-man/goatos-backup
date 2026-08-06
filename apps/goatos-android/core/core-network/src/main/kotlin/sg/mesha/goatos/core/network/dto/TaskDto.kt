package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

@Serializable
data class TaskPresentationItemDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("value") val value: String = "",
)

@Serializable
data class TaskPresentationDto(
    @SerialName("eyebrow") val eyebrow: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("summary_items") val summaryItems: List<TaskPresentationItemDto> = emptyList(),
)

/**
 * Operator Tasks:
 *   GET  /app/tasks                       -> TaskListResponse (items[] + paging metadata)
 *   POST /app/tasks/{task_id}/submissions -> SubmitTaskRequest / SubmissionResponse
 *
 * snake_case wire format. Free-form JSON blobs (context, answers, metadata, result)
 * are modeled as Map<String, JsonElement> so any payload parses; the enums (state)
 * stay String for forward-compat.
 */
@Serializable
data class TaskSummaryDto(
    @SerialName("task_id") val taskId: String = "",
    @SerialName("tenant_id") val tenantId: String = "",
    @SerialName("sop_id") val sopId: String = "",
    @SerialName("sop_version_id") val sopVersionId: String = "",
    @SerialName("sop_code") val sopCode: String = "",
    @SerialName("task_type") val taskType: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("description") val description: String = "",
    @SerialName("state") val state: String = "",
    @SerialName("assigned_to") val assignedTo: String? = null,
    @SerialName("scope_type") val scopeType: String = "",
    @SerialName("scope_id") val scopeId: String = "",
    @SerialName("priority") val priority: String = "",
    @SerialName("due_at") val dueAt: String? = null,
    @SerialName("context") val context: Map<String, JsonElement> = emptyMap(),
    @SerialName("row_version") val rowVersion: Int = 1,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    /** Locale-aware display contract for app surfaces. Raw ids/title remain transport facts. */
    @SerialName("presentation") val presentation: TaskPresentationDto? = null,
)

@Serializable
data class TaskListResponseDto(
    @SerialName("items") val items: List<TaskSummaryDto> = emptyList(),
    @SerialName("total") val total: Long = 0,
    @SerialName("has_more") val hasMore: Boolean = false,
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class ProofReferenceDto(
    @SerialName("proof_id") val proofId: String = "",
    @SerialName("proof_type") val proofType: String = "",
    @SerialName("subject_type") val subjectType: String = "",
    @SerialName("subject_id") val subjectId: String? = null,
    @SerialName("upload_state") val uploadState: String = "",
    @SerialName("metadata") val metadata: Map<String, JsonElement> = emptyMap(),
)

@Serializable
data class ValidationIssueDto(
    @SerialName("field") val field: String = "",
    @SerialName("code") val code: String = "",
    @SerialName("message") val message: String = "",
)

@Serializable
data class ValidationReportDto(
    @SerialName("valid") val valid: Boolean = true,
    @SerialName("errors") val errors: List<ValidationIssueDto>? = emptyList(),
    @SerialName("warnings") val warnings: List<ValidationIssueDto>? = emptyList(),
)

@Serializable
data class SubmissionItemDto(
    @SerialName("item_id") val itemId: String = "",
    @SerialName("goat_id") val goatId: String? = null,
    @SerialName("item_key") val itemKey: String = "",
    @SerialName("state") val state: String = "",
    @SerialName("result") val result: Map<String, JsonElement> = emptyMap(),
)

@Serializable
data class VaccineBreakdownItemDto(
    @SerialName("vaccine") val vaccine: String = "",
    @SerialName("count") val count: Int = 0,
)

@Serializable
data class ShedCompletionSummaryDto(
    @SerialName("task_id") val taskId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("drive_name") val driveName: String = "",
    @SerialName("expected_count") val expectedCount: Int = 0,
    @SerialName("handled_count") val handledCount: Int = 0,
    @SerialName("proof_ready_count") val proofReadyCount: Int = 0,
    @SerialName("proof_mode") val proofMode: String = "",
    @SerialName("vaccine_breakdown") val vaccineBreakdown: List<VaccineBreakdownItemDto> = emptyList(),
    @SerialName("submit_enabled") val submitEnabled: Boolean = false,
    @SerialName("blocking_reason") val blockingReason: String? = null,
    @SerialName("submit_state") val submitState: String = "",
    // True only when a live/accepted submission trail exists for THIS shed's CURRENT round of
    // eligible obligations. See backend domain.ShedCompletionSummary.RoundSubmitted.
    @SerialName("round_submitted") val roundSubmitted: Boolean = false,
    // Deterministic fingerprint of this shed's current obligation-round state; changes value on
    // submit or verifier-rejection reopen. Opaque -- carried for future round-identity
    // comparisons, not currently read by any client gate. See
    // backend domain.ShedCompletionSummary.RoundID.
    @SerialName("round_id") val roundId: String = "",
)

@Serializable
data class SubmissionSummaryDto(
    @SerialName("submission_id") val submissionId: String = "",
    @SerialName("task_id") val taskId: String = "",
    @SerialName("sop_version_id") val sopVersionId: String = "",
    @SerialName("submitted_by") val submittedBy: String = "",
    @SerialName("idempotency_key") val idempotencyKey: String = "",
    @SerialName("answers") val answers: Map<String, JsonElement> = emptyMap(),
    @SerialName("proof_refs") val proofRefs: List<ProofReferenceDto> = emptyList(),
    @SerialName("state") val state: String = "",
    @SerialName("validation_report") val validationReport: ValidationReportDto = ValidationReportDto(),
    @SerialName("items") val items: List<SubmissionItemDto> = emptyList(),
    @SerialName("submitted_at") val submittedAt: String = "",
    @SerialName("accepted_at") val acceptedAt: String? = null,
    @SerialName("row_version") val rowVersion: Int = 1,
)

/**
 * SOP version slice returned inside [TaskDetailResponseDto] — carries the [formDsl] the operator
 * form runner renders (schema_version + fields[] + rules[]) and the [proofPolicy]. Both are
 * free-form JSON blobs (the backend owns the schema); the client walks them structurally.
 */
@Serializable
data class SopVersionDto(
    @SerialName("sop_version_id") val sopVersionId: String = "",
    @SerialName("sop_id") val sopId: String = "",
    @SerialName("version") val version: Int = 0,
    @SerialName("status") val status: String = "",
    @SerialName("form_dsl") val formDsl: Map<String, JsonElement> = emptyMap(),
    @SerialName("proof_policy") val proofPolicy: Map<String, JsonElement> = emptyMap(),
)

/**
 * GET /app/tasks/{task_id} — a task PLUS its SOP version (the form to render) and prior
 * submissions. The mobile form runner needs [sopVersion].`form_dsl` to draw the drive form;
 * the list endpoint (`GET /app/tasks`) omits it, so a task is opened via this detail fetch.
 */
@Serializable
data class TaskDetailResponseDto(
    @SerialName("task") val task: TaskSummaryDto = TaskSummaryDto(),
    @SerialName("sop_version") val sopVersion: SopVersionDto? = null,
    @SerialName("submissions") val submissions: List<SubmissionSummaryDto> = emptyList(),
    @SerialName("trace_id") val traceId: String = "",
    /** Client-only cache envelope field populated from the task option-values endpoint. */
    @SerialName("option_values") val optionValues: TaskOptionValuesResponseDto? = null,
)

@Serializable
data class TaskOptionValueDto(
    @SerialName("value") val value: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("disabled") val disabled: Boolean = false,
    @SerialName("disabled_reason") val disabledReason: String? = null,
    @SerialName("available_quantity") val availableQuantity: String? = null,
    @SerialName("quantity_unit") val quantityUnit: String? = null,
    @SerialName("expiry_date") val expiryDate: String? = null,
    @SerialName("fefo_rank") val fefoRank: Int? = null,
)

@Serializable
data class TaskOptionSourceDto(
    @SerialName("source") val source: String = "",
    @SerialName("disabled_reason") val disabledReason: String? = null,
    @SerialName("options") val options: List<TaskOptionValueDto> = emptyList(),
)

@Serializable
data class TaskOptionValuesResponseDto(
    @SerialName("task_id") val taskId: String = "",
    @SerialName("batch_id") val batchId: String = "",
    @SerialName("sop_version_id") val sopVersionId: String = "",
    @SerialName("task_row_version") val taskRowVersion: Int = 0,
    @SerialName("sources") val sources: List<TaskOptionSourceDto> = emptyList(),
)

/** Request body for POST /app/tasks/{task_id}/submissions. */
@Serializable
data class SubmitTaskRequestDto(
    @SerialName("sop_version_id") val sopVersionId: String,
    @SerialName("idempotency_key") val idempotencyKey: String,
    @SerialName("answers") val answers: Map<String, JsonElement> = emptyMap(),
    @SerialName("proof_refs") val proofRefs: List<ProofReferenceDto> = emptyList(),
)

/** Request body for POST /app/tasks/{task_id}/scan-captures.
 *  A scan capture is a server-side draft, not a final vaccination completion. It protects
 *  field work before Submit, and Submit later validates/finalizes the task. */
@Serializable
data class ScanCaptureRequestDto(
    @SerialName("field_key") val fieldKey: String,
    @SerialName("tag") val tag: String,
    @SerialName("goat_id") val goatId: String? = null,
    @SerialName("obligation_id") val obligationId: String? = null,
    @SerialName("captured_at_ms") val capturedAtMs: Long? = null,
)

@Serializable
data class ScanCaptureDto(
    @SerialName("capture_id") val captureId: String = "",
    @SerialName("task_id") val taskId: String = "",
    @SerialName("field_key") val fieldKey: String = "",
    @SerialName("tag") val tag: String = "",
    @SerialName("goat_id") val goatId: String? = null,
    @SerialName("obligation_id") val obligationId: String? = null,
    @SerialName("captured_at") val capturedAt: String = "",
)

@Serializable
data class ScanCaptureResponseDto(
    @SerialName("capture") val capture: ScanCaptureDto = ScanCaptureDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** Request body for POST /app/tasks/{task_id}/scan-attempts.
 *  An attempt is append-only RFID audit evidence. It records accepted, duplicate, not-due,
 *  and unknown physical reads without changing Submit counters. */
@Serializable
data class ScanAttemptRequestDto(
    @SerialName("field_key") val fieldKey: String,
    @SerialName("tag") val tag: String,
    @SerialName("normalized_tag") val normalizedTag: String? = null,
    @SerialName("goat_id") val goatId: String? = null,
    @SerialName("obligation_id") val obligationId: String? = null,
    @SerialName("outcome") val outcome: String,
    @SerialName("tag_role") val tagRole: String = "unknown",
    @SerialName("reason") val reason: String? = null,
    @SerialName("captured_at_ms") val capturedAtMs: Long? = null,
)

@Serializable
data class ScanAttemptDto(
    @SerialName("attempt_id") val attemptId: String = "",
    @SerialName("task_id") val taskId: String = "",
    @SerialName("field_key") val fieldKey: String = "",
    @SerialName("tag") val tag: String = "",
    @SerialName("goat_id") val goatId: String? = null,
    @SerialName("obligation_id") val obligationId: String? = null,
    @SerialName("outcome") val outcome: String = "",
    @SerialName("tag_role") val tagRole: String = "unknown",
    @SerialName("reason") val reason: String? = null,
    @SerialName("captured_at") val capturedAt: String = "",
)

@Serializable
data class ScanAttemptResponseDto(
    @SerialName("attempt") val attempt: ScanAttemptDto = ScanAttemptDto(),
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class SubmissionResponseDto(
    @SerialName("submission") val submission: SubmissionSummaryDto = SubmissionSummaryDto(),
    @SerialName("task") val task: TaskSummaryDto = TaskSummaryDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** Request body for POST /admin/tasks/{task_id}/verify and POST /admin/tasks/{task_id}/rework. */
@Serializable
data class ReviewTaskRequestDto(
    @SerialName("reason") val reason: String,
    @SerialName("row_version") val rowVersion: Int,
)

/** Response for task review (verify/rework) operations. */
@Serializable
data class ReviewTaskResponseDto(
    @SerialName("task") val task: TaskSummaryDto = TaskSummaryDto(),
    @SerialName("trace_id") val traceId: String = "",
)
