package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.UUID
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.R
import sg.mesha.goatos.boot.NavStateRefreshSignal
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsLeadershipTasks
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.LeadershipTasksRepository
import sg.mesha.goatos.core.network.dto.LeadershipTaskDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskStatusRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskCommentRequestDto
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentKind
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipStatusOptionUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskDetailEvent
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskDetailUiState
import sg.mesha.goatos.feature.leadershiptasks.formatBytes
import sg.mesha.goatos.feature.leadershiptasks.formatClock
import javax.inject.Inject

/** "Open this file outside the app": the host resolves a viewer for [mimeType]. */
data class LeadershipOpenFileRequest(val localPath: String, val mimeType: String)

/**
 * ONE task's detail state holder (maintainer request 2026-09-04).
 *
 * Room is the single source of truth: the screen renders from
 * [LeadershipTasksRepository.observeTaskDetail] merged with this phone's own transient state.
 * The SERVER owns every visible sentence and every capability; this class maps the payload
 * one-to-one and adds nothing.
 *
 * Two write rules, both provable in the unit test:
 *  - `seen` fires ONCE per open, and only when the payload says the caller has not seen it.
 *  - a status change mints its idempotency key ONCE per (task, target) and reuses it verbatim on
 *    a retry, so a tap repeated after a timeout can never move the task twice.
 */
@HiltViewModel
class LeadershipTaskDetailViewModel @Inject constructor(
    private val repository: LeadershipTasksRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val navRefresh: NavStateRefreshSignal,
    @ApplicationContext private val appContext: Context,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val taskId: String = savedStateHandle.get<String>(ARG_TASK_ID).orEmpty()

    private data class Local(
        val isRefreshing: Boolean = false,
        val actionInFlight: Boolean = false,
        val showCancelConfirm: Boolean = false,
        val message: String? = null,
        /** Attachment listKey -> local path once fetched. */
        val fetched: Map<String, String> = emptyMap(),
        /** Attachment listKeys whose bytes are being fetched. */
        val fetching: Set<String> = emptySet(),
        /** Idempotency keys minted for status writes, keyed by target status; kept until success. */
        val statusKeys: Map<String, String> = emptyMap(),
        /** The comment being typed; null means "show the server's". */
        val commentDraft: String? = null,
        val commentSaving: Boolean = false,
        val commentKey: String? = null,
    )

    private val local = MutableStateFlow(Local())

    /** Tracks the one `seen` per open — a second detail emission must not fire it again. */
    private var seenFired = false

    private val _openFile = MutableSharedFlow<LeadershipOpenFileRequest>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)

    /** One-shot: the host opens the file with a system viewer. */
    val openFile: SharedFlow<LeadershipOpenFileRequest> = _openFile

    /** The freshest task the screen has, for the edit route and the status writes. */
    private var latest: LeadershipTaskDto? = null

    val state: StateFlow<LeadershipTaskDetailUiState> =
        combine(repository.observeTaskDetail(taskId), local) { detail, own ->
            latest = detail
            if (detail != null) markSeenIfNeeded(detail)
            toUiState(detail, own)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), LeadershipTaskDetailUiState())

    init {
        refresh()
    }

    fun onEvent(event: LeadershipTaskDetailEvent) {
        when (event) {
            LeadershipTaskDetailEvent.Refresh -> refresh()
            LeadershipTaskDetailEvent.Back -> Unit
            LeadershipTaskDetailEvent.Edit -> Unit
            is LeadershipTaskDetailEvent.ChangeStatus -> changeStatus(event.statusKey)
            LeadershipTaskDetailEvent.RequestCancel -> local.update { it.copy(showCancelConfirm = true) }
            LeadershipTaskDetailEvent.DismissCancel -> local.update { it.copy(showCancelConfirm = false) }
            LeadershipTaskDetailEvent.ConfirmCancel -> {
                local.update { it.copy(showCancelConfirm = false) }
                changeStatus(STATUS_CANCELLED)
            }
            is LeadershipTaskDetailEvent.OpenAttachment -> openAttachment(event.listKey)
            is LeadershipTaskDetailEvent.CommentChanged -> local.update { it.copy(commentDraft = event.value.take(MAX_COMMENT_CHARS), commentKey = null) }
            LeadershipTaskDetailEvent.SaveComment -> saveComment()
            LeadershipTaskDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
        }
    }

    /** The row version the edit route should carry, from the freshest task known here. */
    fun currentTask(): LeadershipTaskDto? = latest

    private fun refresh() {
        if (taskId.isBlank()) return
        viewModelScope.launch {
            local.update { it.copy(isRefreshing = true) }
            try {
                repository.refreshTaskDetail(taskId)
            } finally {
                local.update { it.copy(isRefreshing = false) }
            }
        }
    }

    /**
     * The assignee opened it. Fired once per open, only while the payload says unseen, and only
     * for the person who can act on it — a raiser opening their own task is not the CXO reading it.
     */
    private fun markSeenIfNeeded(detail: LeadershipTaskDto) {
        if (seenFired || detail.isSeen || !detail.isAssignee) return
        seenFired = true
        viewModelScope.launch {
            when (val result = repository.markSeen(taskId)) {
                is AppResult.Ok -> navRefresh.request()
                is AppResult.Err -> {
                    // Not the caller's problem: the next open tries again. Reported, not shown.
                    result.cause?.let { crashReporter.recordException(it, "leadership task seen failed") }
                    analytics.track(
                        AnalyticsEventsLeadershipTasks.FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to (result.cause?.message ?: result.message).take(MAX_REASON_CHARS)),
                    )
                    seenFired = false
                }
            }
        }
    }

    private fun changeStatus(statusKey: String) {
        val task = latest ?: return
        if (local.value.actionInFlight) return
        // ONE key per (task, target status) for the life of this screen: a retry after a timeout
        // sends the same key and the server answers with the original result instead of a second move.
        val key = local.value.statusKeys[statusKey] ?: UUID.randomUUID().toString().also { minted ->
            local.update { it.copy(statusKeys = it.statusKeys + (statusKey to minted)) }
        }
        viewModelScope.launch {
            local.update { it.copy(actionInFlight = true, message = null) }
            try {
                when (
                    val result = repository.changeStatus(
                        taskId = taskId,
                        idempotencyKey = key,
                        request = LeadershipTaskStatusRequestDto(status = statusKey, rowVersion = task.rowVersion),
                    )
                ) {
                    is AppResult.Ok -> {
                        local.update { it.copy(statusKeys = it.statusKeys - statusKey) }
                        analytics.track(
                            AnalyticsEventsLeadershipTasks.STATUS_CHANGED,
                            mapOf(AnalyticsEvents.Params.STATUS to statusKey),
                        )
                        navRefresh.request()
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "leadership task status change failed") }
                        analytics.track(
                            AnalyticsEventsLeadershipTasks.FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to (result.cause?.message ?: result.message).take(MAX_REASON_CHARS)),
                        )
                        // The server's own sentence when it gave one; the connection line otherwise.
                        local.update {
                            it.copy(message = result.message.ifBlank { appContext.getString(R.string.leadership_tasks_msg_not_sent) })
                        }
                        // A stale row version means the task moved elsewhere: pull the truth.
                        refresh()
                    }
                }
            } finally {
                local.update { it.copy(actionInFlight = false) }
            }
        }
    }

    private fun saveComment() {
        val task = latest ?: return
        val own = local.value
        val draft = own.commentDraft ?: return
        if (own.commentSaving || draft.trim() == task.comment.trim()) return
        // One key per draft text: a retry of the same text replays, a changed text is a new write.
        val key = own.commentKey ?: UUID.randomUUID().toString().also { minted -> local.update { it.copy(commentKey = minted) } }
        viewModelScope.launch {
            local.update { it.copy(commentSaving = true, message = null) }
            try {
                when (val result = repository.setComment(taskId, key, LeadershipTaskCommentRequestDto(comment = draft.trim()))) {
                    is AppResult.Ok -> {
                        local.update { it.copy(commentDraft = null, commentKey = null) }
                        analytics.track(AnalyticsEventsLeadershipTasks.STATUS_CHANGED, mapOf(AnalyticsEvents.Params.STATUS to "comment"))
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "leadership task comment failed") }
                        analytics.track(
                            AnalyticsEventsLeadershipTasks.FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to (result.cause?.message ?: result.message).take(MAX_REASON_CHARS)),
                        )
                        local.update {
                            it.copy(message = result.message.ifBlank { appContext.getString(R.string.leadership_tasks_msg_not_sent) })
                        }
                    }
                }
            } finally {
                local.update { it.copy(commentSaving = false) }
            }
        }
    }

    private fun openAttachment(listKey: String) {
        val attachment = latest?.attachments?.firstOrNull { it.attachmentId.ifBlank { it.proofId } == listKey } ?: return
        val own = local.value
        val known = own.fetched[listKey]
        if (known != null) {
            if (LeadershipAttachmentKind.from(attachment.kind) == LeadershipAttachmentKind.FILE) {
                _openFile.tryEmit(LeadershipOpenFileRequest(localPath = known, mimeType = attachment.mimeType))
            }
            return
        }
        if (listKey in own.fetching) return
        viewModelScope.launch {
            local.update { it.copy(fetching = it.fetching + listKey) }
            try {
                when (val result = repository.attachmentFile(taskId, attachment.proofId, attachment.fileName)) {
                    is AppResult.Ok -> {
                        local.update { it.copy(fetched = it.fetched + (listKey to result.value)) }
                        if (LeadershipAttachmentKind.from(attachment.kind) == LeadershipAttachmentKind.FILE) {
                            _openFile.tryEmit(LeadershipOpenFileRequest(localPath = result.value, mimeType = attachment.mimeType))
                        }
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "leadership task attachment fetch failed") }
                        analytics.track(
                            AnalyticsEventsLeadershipTasks.FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to (result.cause?.message ?: result.message).take(MAX_REASON_CHARS)),
                        )
                        local.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_attachment_unavailable)) }
                    }
                }
            } finally {
                local.update { it.copy(fetching = it.fetching - listKey) }
            }
        }
    }

    private fun toUiState(detail: LeadershipTaskDto?, own: Local): LeadershipTaskDetailUiState {
        if (detail == null) {
            return LeadershipTaskDetailUiState(loading = true, isRefreshing = own.isRefreshing, message = own.message)
        }
        return LeadershipTaskDetailUiState(
            loading = false,
            numberLabel = detail.numberLabel,
            statusChip = detail.statusChip,
            status = detail.status,
            title = detail.title,
            body = detail.body,
            metaLine = detail.metaLine,
            attachments = detail.attachments
                .sortedBy { it.position }
                .map { attachment ->
                    val key = attachment.attachmentId.ifBlank { attachment.proofId }
                    LeadershipAttachmentUi(
                        listKey = key,
                        proofId = attachment.proofId,
                        kind = LeadershipAttachmentKind.from(attachment.kind),
                        fileName = attachment.fileName,
                        sizeLabel = formatBytes(attachment.sizeBytes),
                        durationLabel = attachment.durationMs?.takeIf { it > 0L }?.let(::formatClock).orEmpty(),
                        localPath = own.fetched[key].orEmpty(),
                        loading = key in own.fetching,
                    )
                },
            statusOptions = detail.statusOptions
                .filter { it.key != STATUS_CANCELLED }
                .map { LeadershipStatusOptionUi(key = it.key, label = it.label) },
            canEdit = detail.canEdit,
            // Cancel is its own ghost action with a confirm, whether the backend lists it as an
            // option or flags it — both mean the same thing for this caller.
            canCancel = detail.canCancel || detail.statusOptions.any { it.key == STATUS_CANCELLED },
            comment = detail.comment,
            commentDraft = own.commentDraft ?: detail.comment,
            canComment = detail.canComment,
            commentSaving = own.commentSaving,
            isRefreshing = own.isRefreshing,
            actionInFlight = own.actionInFlight,
            showCancelConfirm = own.showCancelConfirm,
            message = own.message,
        )
    }

    private companion object {
        const val ARG_TASK_ID = "task_id"
        const val STATUS_CANCELLED = "cancelled"
        const val MAX_REASON_CHARS = 120
        const val MAX_COMMENT_CHARS = 2000
    }
}
