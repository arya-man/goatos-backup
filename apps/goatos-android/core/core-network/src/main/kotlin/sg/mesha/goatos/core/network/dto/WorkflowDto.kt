package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Wire DTOs for the Birth/Death follow-up workflow module
 * (docs/decisions/birth-death-workflows.md):
 *
 *  - `GET /app/workflows?module=birth|death&date=…&filter=…` — the per-goat work-list cards plus
 *    the day's chip counts (both computed backend-side from `workflow_instances` alone);
 *  - `GET /app/workflows/{workflow_id}` — the drill-in detail: card + facts + the full ≤14-row
 *    action list;
 *  - `POST /app/workflows/{workflow_id}/actions/{action_id}/answer` and `…/complete` — the two
 *    idempotent action writes, drained through the offline outbox.
 *
 * Every field carries a default so a later contract addition never breaks decode of an
 * already-cached Room row (the same leniency contract as every other DTO here). All copy —
 * titles, details, labels, role labels — is backend-owned and rendered verbatim.
 */

/** The goat a workflow is about. [roleLabel] is backend display copy ("Mother", "Kid 1"). */
@Serializable
data class WorkflowSubjectDto(
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("display_id") val displayId: String = "",
    @SerialName("tag") val tag: String = "",
    @SerialName("role_label") val roleLabel: String = "",
    @SerialName("sex") val sex: String = "",
    @SerialName("breed") val breed: String = "",
)

/** The single next pending action summarized onto the card. */
@Serializable
data class WorkflowNextActionDto(
    @SerialName("key") val key: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("due_at") val dueAt: String? = null,
    @SerialName("overdue") val overdue: Boolean = false,
)

/**
 * One per-goat work-list card. Card fields (`actions_done`/`actions_total`/`next_action`/
 * `awaiting_verification`) are write-maintained on `workflow_instances` backend-side — the app
 * renders them and never recomputes progress from the action list.
 */
@Serializable
data class WorkflowCardDto(
    @SerialName("workflow_id") val workflowId: String = "",
    @SerialName("module") val module: String = "",
    @SerialName("template_key") val templateKey: String = "",
    @SerialName("subject") val subject: WorkflowSubjectDto = WorkflowSubjectDto(),
    @SerialName("event_at") val eventAt: String = "",
    @SerialName("event_date") val eventDate: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("actions_done") val actionsDone: Int = 0,
    @SerialName("actions_total") val actionsTotal: Int = 0,
    @SerialName("next_action") val nextAction: WorkflowNextActionDto? = null,
    @SerialName("awaiting_verification") val awaitingVerification: Boolean = false,
    @SerialName("state") val state: String = "",
)

/** The requested day's chip counts — backend-computed over the SAME scope the list reads. */
@Serializable
data class WorkflowChipsDto(
    @SerialName("all") val all: Int = 0,
    @SerialName("overdue") val overdue: Int = 0,
    @SerialName("due") val due: Int = 0,
    @SerialName("completed") val completed: Int = 0,
    @SerialName("awaiting_video") val awaitingVideo: Int = 0,
)

@Serializable
data class WorkflowListResponseDto(
    @SerialName("items") val items: List<WorkflowCardDto> = emptyList(),
    @SerialName("chips") val chips: WorkflowChipsDto = WorkflowChipsDto(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

/** One backend-labelled fact on the drill-in context card ("Born" → "12 Mar 07:20"). */
@Serializable
data class WorkflowFactDto(
    @SerialName("label") val label: String = "",
    @SerialName("value") val value: String = "",
)

/**
 * One per-goat SOP step. [actionType] is `question | question_select | action | approval`;
 * [status] is `pending | in_review | completed | rework | canceled`. [blocked] marks a later
 * operator action that cannot run until its previous sibling completes. Internal approval actions
 * are not returned to the operator. All display strings are backend copy rendered verbatim.
 */
@Serializable
data class WorkflowActionDto(
    @SerialName("action_id") val actionId: String = "",
    @SerialName("action_key") val actionKey: String = "",
    @SerialName("seq") val seq: Int = 0,
    @SerialName("section") val section: String = "main",
    @SerialName("action_type") val actionType: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("detail") val detail: String = "",
    @SerialName("requires_video") val requiresVideo: Boolean = false,
    @SerialName("options") val options: List<String> = emptyList(),
    @SerialName("due_at") val dueAt: String? = null,
    @SerialName("status") val status: String = "",
    @SerialName("blocked") val blocked: Boolean = false,
    @SerialName("answer_value") val answerValue: String? = null,
    @SerialName("proof_ref") val proofRef: String? = null,
    @SerialName("completed_by_label") val completedByLabel: String? = null,
    @SerialName("completed_at") val completedAt: String? = null,
    @SerialName("verification_status") val verificationStatus: String? = null,
)

/** The drill-in detail: the card, its facts grid, and the bounded operator action list (≤13 rows). */
@Serializable
data class WorkflowDetailResponseDto(
    @SerialName("workflow_id") val workflowId: String = "",
    @SerialName("module") val module: String = "",
    @SerialName("template_key") val templateKey: String = "",
    @SerialName("subject") val subject: WorkflowSubjectDto = WorkflowSubjectDto(),
    @SerialName("event_at") val eventAt: String = "",
    @SerialName("event_date") val eventDate: String = "",
    @SerialName("park_label") val parkLabel: String = "",
    @SerialName("shed_label") val shedLabel: String = "",
    @SerialName("actions_done") val actionsDone: Int = 0,
    @SerialName("actions_total") val actionsTotal: Int = 0,
    @SerialName("next_action") val nextAction: WorkflowNextActionDto? = null,
    @SerialName("awaiting_verification") val awaitingVerification: Boolean = false,
    @SerialName("state") val state: String = "",
    @SerialName("facts") val facts: List<WorkflowFactDto> = emptyList(),
    @SerialName("actions") val actions: List<WorkflowActionDto> = emptyList(),
)

/** Body of `POST …/actions/{action_id}/answer` (question / question_select). */
@Serializable
data class WorkflowActionAnswerRequestDto(
    @SerialName("answer_value") val answerValue: String,
)

/** Body of `POST …/actions/{action_id}/complete`. [proofRef] is REQUIRED when the action
 *  `requires_video` (a missing one is rejected 422 `proof_required`). */
@Serializable
data class WorkflowActionCompleteRequestDto(
    @SerialName("proof_ref") val proofRef: String? = null,
)

/** Shared response of both action writes. */
@Serializable
data class WorkflowActionWriteResponseDto(
    @SerialName("workflow_id") val workflowId: String = "",
    @SerialName("action_id") val actionId: String = "",
    @SerialName("status") val status: String = "",
    @SerialName("workflow_state") val workflowState: String = "",
    @SerialName("actions_done") val actionsDone: Int = 0,
    @SerialName("actions_total") val actionsTotal: Int = 0,
    @SerialName("awaiting_verification") val awaitingVerification: Boolean = false,
    @SerialName("completed_at") val completedAt: String? = null,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)
