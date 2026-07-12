package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * Operator Tasks:
 *   GET  /app/tasks                       -> TaskListResponse (items[] + trace_id)
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
)

@Serializable
data class TaskListResponseDto(
    @SerialName("items") val items: List<TaskSummaryDto> = emptyList(),
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
    @SerialName("errors") val errors: List<ValidationIssueDto> = emptyList(),
    @SerialName("warnings") val warnings: List<ValidationIssueDto> = emptyList(),
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
)

/** Request body for POST /app/tasks/{task_id}/submissions. */
@Serializable
data class SubmitTaskRequestDto(
    @SerialName("sop_version_id") val sopVersionId: String,
    @SerialName("idempotency_key") val idempotencyKey: String,
    @SerialName("answers") val answers: Map<String, JsonElement> = emptyMap(),
    @SerialName("proof_refs") val proofRefs: List<ProofReferenceDto> = emptyList(),
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
