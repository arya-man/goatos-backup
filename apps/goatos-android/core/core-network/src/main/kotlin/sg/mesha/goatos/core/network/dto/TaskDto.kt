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
