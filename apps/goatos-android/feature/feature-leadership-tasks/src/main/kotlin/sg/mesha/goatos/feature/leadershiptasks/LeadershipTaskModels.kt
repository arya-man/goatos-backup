package sg.mesha.goatos.feature.leadershiptasks

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the
// leadership_task_* AnalyticsEventsLeadershipTasks + CrashReporter wiring for every read
// refresh, upload, and write.

import androidx.compose.runtime.Immutable

/**
 * UI models for the Leadership Tasks module (maintainer request 2026-09-04): a director raises a
 * task for one CXO; the CXO marks it seen and moves it open -> in progress -> done.
 *
 * EVERY business sentence here is BACKEND-OWNED and carried through verbatim: the number label,
 * the status chip, the meta line, the filter chips and their empty copy, the status option
 * labels, the page title. The renderer composes none of them. Every control is driven by the
 * capability booleans the backend composed for THIS caller — never by who they are.
 */

/** What an attachment IS, mirrored one-to-one from the wire (`audio|video|photo|file`). */
enum class LeadershipAttachmentKind {
    AUDIO,
    VIDEO,
    PHOTO,
    FILE,
    ;

    /** The wire value the request side carries. */
    val wireValue: String
        get() = when (this) {
            AUDIO -> "audio"
            VIDEO -> "video"
            PHOTO -> "photo"
            FILE -> "file"
        }

    companion object {
        fun from(raw: String): LeadershipAttachmentKind = when (raw) {
            "audio" -> AUDIO
            "video" -> VIDEO
            "photo" -> PHOTO
            else -> FILE
        }
    }
}

/** One stored attachment on a task, as the detail renders it. */
@Immutable
data class LeadershipAttachmentUi(
    /** Stable list key: the attachment id (the proof id before the server assigned one). */
    val listKey: String,
    val proofId: String,
    val kind: LeadershipAttachmentKind,
    val fileName: String,
    /** "1.2 MB" — a number, not a sentence; blank when the size is unknown. */
    val sizeLabel: String = "",
    /** "0:42" for audio/video; blank otherwise. */
    val durationLabel: String = "",
    /** Local path of the bytes once fetched into app-private cache; blank until then. */
    val localPath: String = "",
    /** True while the bytes are being fetched. */
    val loading: Boolean = false,
)

/** One task as the list renders it. */
@Immutable
data class LeadershipTaskCardUi(
    /** Stable list key — the task id IS this list's grain. */
    val listKey: String,
    val taskId: String,
    /** Backend-composed ("#12"), VERBATIM. */
    val numberLabel: String,
    /** Backend-composed chip copy, VERBATIM. */
    val statusChip: String,
    /** The status KEY the chip colour follows (open / in_progress / done / cancelled). */
    val status: String = "",
    val title: String,
    /** Backend-composed meta sentence, VERBATIM. */
    val metaLine: String,
    val attachmentCount: Int = 0,
    /** True for an assigned task the caller has not opened yet — drawn with a brand rail. */
    val unseen: Boolean = false,
)

/** One filter chip. Label, count and empty copy are BACKEND-COMPOSED; the screen sends back [key]. */
@Immutable
data class LeadershipTaskFilterUi(
    val key: String,
    val label: String,
    val count: Int,
    val selected: Boolean,
    val emptyMessage: String = "",
)

@Immutable
data class LeadershipTaskListUiState(
    /** The backend's page title, VERBATIM. */
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val filters: List<LeadershipTaskFilterUi> = emptyList(),
    /** The backend's `can_raise` — the ONLY thing that shows the "+" action. */
    val canRaise: Boolean = false,
)

sealed interface LeadershipTaskListEvent {
    data object Refresh : LeadershipTaskListEvent
    data class SelectFilter(val key: String) : LeadershipTaskListEvent
    data class OpenTask(val taskId: String) : LeadershipTaskListEvent

    /** The "+" action: raise a new task. Only offered when `can_raise`. */
    data object RaiseTask : LeadershipTaskListEvent
}

/** One status THIS caller may move the task to; [label] is backend copy, VERBATIM. */
@Immutable
data class LeadershipStatusOptionUi(
    val key: String,
    val label: String,
)

@Immutable
data class LeadershipTaskDetailUiState(
    /** True until the first cached/fetched detail lands. */
    val loading: Boolean = true,
    val numberLabel: String = "",
    val statusChip: String = "",
    val status: String = "",
    val title: String = "",
    val body: String = "",
    val metaLine: String = "",
    val attachments: List<LeadershipAttachmentUi> = emptyList(),
    /** The backend's status options for this caller; empty renders no action row at all. */
    val statusOptions: List<LeadershipStatusOptionUi> = emptyList(),
    val canEdit: Boolean = false,
    val canCancel: Boolean = false,
    /** The CXO's note on the task (server truth), the draft being typed, and whether this caller may write it. */
    val comment: String = "",
    val commentDraft: String = "",
    val canComment: Boolean = false,
    val commentSaving: Boolean = false,
    val isRefreshing: Boolean = false,
    /** True while a status change / cancel is on the wire — every action is dead meanwhile. */
    val actionInFlight: Boolean = false,
    val showCancelConfirm: Boolean = false,
    /** Transient notice; the ViewModel owns the copy and clears it. */
    val message: String? = null,
)

sealed interface LeadershipTaskDetailEvent {
    data object Refresh : LeadershipTaskDetailEvent
    data object Back : LeadershipTaskDetailEvent
    data object Edit : LeadershipTaskDetailEvent
    data class ChangeStatus(val statusKey: String) : LeadershipTaskDetailEvent
    data object RequestCancel : LeadershipTaskDetailEvent
    data object ConfirmCancel : LeadershipTaskDetailEvent
    data object DismissCancel : LeadershipTaskDetailEvent

    /** Fetch (if needed) and open one attachment: audio plays in place, a file opens outside. */
    data class OpenAttachment(val listKey: String) : LeadershipTaskDetailEvent
    data class CommentChanged(val value: String) : LeadershipTaskDetailEvent
    data object SaveComment : LeadershipTaskDetailEvent
    data object DismissMessage : LeadershipTaskDetailEvent
}

/** Where a draft attachment stands on the wire. */
enum class LeadershipDraftUploadState { PENDING, UPLOADING, UPLOADED, FAILED }

/** One attachment on a draft (raise or edit): either already stored, or local and to be sent. */
@Immutable
data class LeadershipDraftAttachmentUi(
    /** Stable list key — the attachment's own idempotency key, minted once per draft. */
    val listKey: String,
    val kind: LeadershipAttachmentKind,
    val fileName: String,
    val sizeLabel: String = "",
    val durationLabel: String = "",
    /** Local path for a preview (blank for an already-stored attachment that was not fetched). */
    val localPath: String = "",
    val uploadState: LeadershipDraftUploadState = LeadershipDraftUploadState.PENDING,
)

@Immutable
data class LeadershipAssigneeUi(
    val userId: String,
    val name: String,
)

@Immutable
data class LeadershipTaskComposeUiState(
    /** True when editing an existing task (assignee locked, header says so). */
    val isEdit: Boolean = false,
    /** Backend-owned page title for the list, reused as the eyebrow so the screen says where it is. */
    val assignees: List<LeadershipAssigneeUi> = emptyList(),
    val assigneesLoading: Boolean = false,
    val selectedAssigneeId: String = "",
    val title: String = "",
    val body: String = "",
    val attachments: List<LeadershipDraftAttachmentUi> = emptyList(),
    /** Elapsed millis of the voice note being recorded; null when not recording. */
    val recordingElapsedMs: Long? = null,
    /** True while attachments upload and the task is sent. */
    val sending: Boolean = false,
    /** Set once the task reached the server; the host pops back. */
    val sentTaskId: String? = null,
    /** Title and assignee present, nothing in flight. */
    val canSend: Boolean = false,
    /** Transient notice; the ViewModel owns the copy and clears it. */
    val message: String? = null,
)

sealed interface LeadershipTaskComposeEvent {
    data object Back : LeadershipTaskComposeEvent
    data class TitleChanged(val value: String) : LeadershipTaskComposeEvent
    data class BodyChanged(val value: String) : LeadershipTaskComposeEvent
    data class SelectAssignee(val userId: String) : LeadershipTaskComposeEvent
    data object StartRecording : LeadershipTaskComposeEvent
    data object StopRecording : LeadershipTaskComposeEvent

    /** The microphone permission was refused; the host owns the copy. */
    data object RecordingPermissionDenied : LeadershipTaskComposeEvent

    /** Gallery photos/videos picked, as content URIs. */
    data class MediaPicked(val uris: List<String>) : LeadershipTaskComposeEvent

    /** Documents picked, as content URIs. */
    data class FilesPicked(val uris: List<String>) : LeadershipTaskComposeEvent
    data class RemoveAttachment(val listKey: String) : LeadershipTaskComposeEvent
    data object Send : LeadershipTaskComposeEvent
    data object DismissMessage : LeadershipTaskComposeEvent
}
