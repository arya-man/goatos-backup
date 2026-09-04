package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Leadership Tasks module (maintainer request 2026-09-04): a DIRECTOR raises a task for ONE CXO
 * with a title, a brief, and optional attachments (an in-app voice note, gallery photos/videos,
 * arbitrary files). The CXO marks it seen and moves it open -> in_progress -> done; the raiser may
 * edit and cancel while it is not done.
 *
 * ALL business copy (`number_label`, `status_chip`, `meta_line`, `raised_on_label`, the filter
 * labels and empty messages, the status option labels, the page title) is BACKEND-OWNED and
 * rendered verbatim. Every control on the phone is driven by the capability booleans the
 * backend composed for THIS caller (`can_raise`, `can_edit`, `can_change_status`, `can_cancel`,
 * `status_options`) — never by a role string.
 *
 * Wire contract of record: the Leadership Tasks contract (v1, 2026-09-04).
 */
@Serializable
data class LeadershipAssigneeDto(
    @SerialName("user_id") val userId: String = "",
    @SerialName("name") val name: String = "",
)

@Serializable
data class LeadershipAssigneeListDto(
    @SerialName("assignees") val assignees: List<LeadershipAssigneeDto> = emptyList(),
)

/** One stored attachment on a task. [kind] is `audio` | `video` | `photo` | `file`. */
@Serializable
data class LeadershipTaskAttachmentDto(
    @SerialName("attachment_id") val attachmentId: String = "",
    @SerialName("proof_id") val proofId: String = "",
    @SerialName("kind") val kind: String = "",
    @SerialName("mime_type") val mimeType: String = "",
    @SerialName("file_name") val fileName: String = "",
    @SerialName("size_bytes") val sizeBytes: Long = 0L,
    @SerialName("duration_ms") val durationMs: Long? = null,
    @SerialName("position") val position: Int = 0,
)

/** Request-side attachment reference: the proof the attachment's bytes were uploaded under. */
@Serializable
data class LeadershipTaskAttachmentRefDto(
    @SerialName("proof_id") val proofId: String,
    @SerialName("kind") val kind: String,
    @SerialName("file_name") val fileName: String,
)

/** A status THIS caller may move the task to; [label] is backend copy, rendered verbatim. */
@Serializable
data class LeadershipTaskStatusOptionDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class LeadershipTaskDto(
    @SerialName("task_id") val taskId: String = "",
    @SerialName("task_no") val taskNo: Int = 0,
    /** Backend-composed ("#12"), rendered VERBATIM. */
    @SerialName("number_label") val numberLabel: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("body") val body: String = "",
    /** `open` | `in_progress` | `done` | `cancelled`. */
    @SerialName("status") val status: String = "",
    /** Backend-composed chip copy, rendered VERBATIM. */
    @SerialName("status_chip") val statusChip: String = "",
    @SerialName("raised_by_user_id") val raisedByUserId: String = "",
    @SerialName("raised_by_name") val raisedByName: String = "",
    @SerialName("assignee_user_id") val assigneeUserId: String = "",
    @SerialName("assignee_name") val assigneeName: String = "",
    @SerialName("raised_at") val raisedAt: String = "",
    /** Backend-composed farm date ("4 Sep 2026"), rendered VERBATIM. */
    @SerialName("raised_on_label") val raisedOnLabel: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    @SerialName("done_at") val doneAt: String? = null,
    @SerialName("seen_at") val seenAt: String? = null,
    @SerialName("is_seen") val isSeen: Boolean = false,
    /** The caller is the person this task is for / raised it -- backend-resolved party. */
    @SerialName("is_assignee") val isAssignee: Boolean = false,
    @SerialName("is_raiser") val isRaiser: Boolean = false,
    /** Backend-composed meta sentence ("Raised by Hemant · 4 Sep 2026"), rendered VERBATIM. */
    @SerialName("meta_line") val metaLine: String = "",
    @SerialName("row_version") val rowVersion: Int = 0,
    @SerialName("can_edit") val canEdit: Boolean = false,
    @SerialName("can_change_status") val canChangeStatus: Boolean = false,
    /** The CXO's note back on the task; [canComment] says whether the caller may write it. */
    @SerialName("comment") val comment: String = "",
    @SerialName("can_comment") val canComment: Boolean = false,
    @SerialName("can_cancel") val canCancel: Boolean = false,
    /** The statuses THIS caller may move the task to; empty when none. */
    @SerialName("status_options") val statusOptions: List<LeadershipTaskStatusOptionDto> = emptyList(),
    @SerialName("attachment_count") val attachmentCount: Int = 0,
    @SerialName("attachments") val attachments: List<LeadershipTaskAttachmentDto> = emptyList(),
)

@Serializable
data class LeadershipTaskDetailDto(
    @SerialName("task") val task: LeadershipTaskDto = LeadershipTaskDto(),
    @SerialName("trace_id") val traceId: String = "",
)

/** One filter chip on the list. Labels, counts and the empty line are backend-composed. */
@Serializable
data class LeadershipTaskFilterDto(
    @SerialName("key") val key: String = "",
    @SerialName("label") val label: String = "",
    @SerialName("count") val count: Int = 0,
    @SerialName("selected") val selected: Boolean = false,
    @SerialName("empty_message") val emptyMessage: String = "",
)

@Serializable
data class LeadershipTaskPageDto(
    /** The backend's own page title ("Tasks"), rendered VERBATIM. */
    @SerialName("title") val title: String = "",
    @SerialName("rows") val rows: List<LeadershipTaskDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
    @SerialName("filters") val filters: List<LeadershipTaskFilterDto> = emptyList(),
    /** Tasks assigned to the caller not yet seen; 0 for a raiser. */
    @SerialName("unseen_count") val unseenCount: Int = 0,
    /** The caller may raise a task — the ONLY thing that shows the "+" action. */
    @SerialName("can_raise") val canRaise: Boolean = false,
    @SerialName("trace_id") val traceId: String = "",
)

@Serializable
data class LeadershipTaskRaiseRequestDto(
    @SerialName("title") val title: String,
    @SerialName("body") val body: String,
    @SerialName("assignee_user_id") val assigneeUserId: String,
    @SerialName("attachments") val attachments: List<LeadershipTaskAttachmentRefDto> = emptyList(),
)

/** [attachments] is the FULL new list; the server diffs it. */
@Serializable
data class LeadershipTaskEditRequestDto(
    @SerialName("title") val title: String,
    @SerialName("body") val body: String,
    @SerialName("attachments") val attachments: List<LeadershipTaskAttachmentRefDto> = emptyList(),
    @SerialName("row_version") val rowVersion: Int,
)

@Serializable
data class LeadershipTaskCommentRequestDto(
    @SerialName("comment") val comment: String,
)

@Serializable
data class LeadershipTaskStatusRequestDto(
    @SerialName("status") val status: String,
    @SerialName("row_version") val rowVersion: Int,
)
