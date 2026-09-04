package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.UUID
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.R
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsLeadershipTasks
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.LeadershipTasksRepository
import sg.mesha.goatos.core.network.dto.LeadershipTaskAttachmentRefDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskEditRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskRaiseRequestDto
import sg.mesha.goatos.feature.leadershiptasks.LEADERSHIP_TASK_ATTACHMENT_CAP
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAssigneeUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipAttachmentKind
import sg.mesha.goatos.feature.leadershiptasks.LeadershipDraftAttachmentUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipDraftUploadState
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskComposeEvent
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskComposeUiState
import sg.mesha.goatos.feature.leadershiptasks.formatBytes
import sg.mesha.goatos.feature.leadershiptasks.formatClock
import sg.mesha.goatos.leadershiptasks.AttachmentImporter
import sg.mesha.goatos.leadershiptasks.ImportResult
import sg.mesha.goatos.leadershiptasks.VoiceNoteRecorder
import javax.inject.Inject

/** One attachment on the draft, stored or still local. */
internal data class DraftAttachment(
    /** The attachment's own idempotency key, minted ONCE when it joined the draft. */
    val key: String,
    val kind: LeadershipAttachmentKind,
    val fileName: String,
    val mimeType: String,
    val sizeBytes: Long,
    val durationMs: Long?,
    /** Local bytes to upload; blank for an attachment the server already holds. */
    val localPath: String,
    /** How the bytes were produced, for the proof metadata. */
    val captureSource: String,
    /** Server proof id once uploaded (or when it was already stored). */
    val proofId: String? = null,
    val uploadState: LeadershipDraftUploadState = LeadershipDraftUploadState.PENDING,
)

/**
 * Raise or edit a task (maintainer request 2026-09-04) — the one screen with in-progress input.
 *
 * Idempotency is minted ONCE PER DRAFT, up front, and never re-minted: the task key lives in the
 * SavedStateHandle so it survives process death, and each attachment carries its own key from
 * the moment it joins the draft. A "Send" that fails half-way keeps every uploaded proof id and
 * every key, so the retry re-sends the SAME request and the server cannot double-create.
 *
 * Writes are ONLINE (v1 decision): the task needs the server-issued proof ids of its
 * attachments, which the outbox never hands back, so upload -> POST runs here in one go with a
 * visible "Sending…" state. A failure keeps the draft intact and says so in farm language.
 */
@HiltViewModel
class LeadershipTaskComposeViewModel @Inject constructor(
    private val repository: LeadershipTasksRepository,
    private val recorder: VoiceNoteRecorder,
    private val importer: AttachmentImporter,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val editingTaskId: String = savedStateHandle.get<String>(ARG_TASK_ID).orEmpty()
    val isEdit: Boolean = editingTaskId.isNotBlank()

    /** Minted once per draft; survives process death through the SavedStateHandle. */
    internal val taskIdempotencyKey: String =
        savedStateHandle.get<String>(KEY_TASK_IDEMPOTENCY) ?: UUID.randomUUID().toString().also { savedStateHandle[KEY_TASK_IDEMPOTENCY] = it }

    internal data class Draft(
        val assignees: List<LeadershipAssigneeUi> = emptyList(),
        val assigneesLoading: Boolean = false,
        val assigneeId: String = "",
        val title: String = "",
        val body: String = "",
        val attachments: List<DraftAttachment> = emptyList(),
        val recordingElapsedMs: Long? = null,
        val sending: Boolean = false,
        val sentTaskId: String? = null,
        val message: String? = null,
        /** The edited task's row version, so the save is fenced on what was on screen. */
        val rowVersion: Int = 0,
        /** True once an edit's existing task landed in the draft (always true for a new task). */
        val seeded: Boolean,
    )

    private val draft = MutableStateFlow(Draft(seeded = !isEdit))
    private var recordingTicker: Job? = null
    private var voiceNoteCount = 0

    /** The draft as the ViewModel holds it — exposed for the unit test only. */
    internal val currentDraft: Draft get() = draft.value

    val state: StateFlow<LeadershipTaskComposeUiState> = draft
        .map { it.toUiState() }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Draft(seeded = !isEdit).toUiState())

    init {
        if (isEdit) seedFromExisting() else loadAssignees()
    }

    fun onEvent(event: LeadershipTaskComposeEvent) {
        when (event) {
            LeadershipTaskComposeEvent.Back -> Unit
            is LeadershipTaskComposeEvent.TitleChanged -> draft.update { it.copy(title = event.value.take(MAX_TITLE_CHARS)) }
            is LeadershipTaskComposeEvent.BodyChanged -> draft.update { it.copy(body = event.value.take(MAX_BODY_CHARS)) }
            is LeadershipTaskComposeEvent.SelectAssignee -> if (!isEdit) draft.update { it.copy(assigneeId = event.userId) }
            LeadershipTaskComposeEvent.StartRecording -> startRecording()
            LeadershipTaskComposeEvent.StopRecording -> stopRecording()
            LeadershipTaskComposeEvent.RecordingPermissionDenied ->
                draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_mic_needed)) }
            is LeadershipTaskComposeEvent.MediaPicked -> importPicked(event.uris, PICKER_GALLERY)
            is LeadershipTaskComposeEvent.FilesPicked -> importPicked(event.uris, PICKER_FILE)
            is LeadershipTaskComposeEvent.RemoveAttachment -> removeAttachment(event.listKey)
            LeadershipTaskComposeEvent.Send -> send()
            LeadershipTaskComposeEvent.DismissMessage -> draft.update { it.copy(message = null) }
        }
    }

    override fun onCleared() {
        recordingTicker?.cancel()
        recorder.discard()
        super.onCleared()
    }

    private fun loadAssignees() {
        viewModelScope.launch {
            draft.update { it.copy(assigneesLoading = true) }
            when (val result = repository.assignees()) {
                is AppResult.Ok -> draft.update { current ->
                    val list = result.value.map { LeadershipAssigneeUi(userId = it.userId, name = it.name) }
                    current.copy(
                        assignees = list,
                        assigneesLoading = false,
                        // One CXO to choose from is not a choice: preselect.
                        assigneeId = current.assigneeId.ifBlank { list.singleOrNull()?.userId.orEmpty() },
                    )
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "leadership task assignees load failed") }
                    analytics.track(
                        AnalyticsEventsLeadershipTasks.FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to (result.cause?.message ?: result.message).take(MAX_REASON_CHARS)),
                    )
                    draft.update {
                        it.copy(assigneesLoading = false, message = appContext.getString(R.string.leadership_tasks_msg_assignees_unavailable))
                    }
                }
            }
        }
    }

    /** Edit: the existing task (from Room, refreshed behind it) seeds the draft exactly once. */
    private fun seedFromExisting() {
        viewModelScope.launch {
            repository.refreshTaskDetail(editingTaskId)
            val task = repository.observeTaskDetail(editingTaskId).first { it != null } ?: return@launch
            draft.update { current ->
                if (current.seeded) current else current.copy(seeded = true).seededWith(task)
            }
        }
    }

    private fun Draft.seededWith(task: LeadershipTaskDto): Draft = copy(
        assignees = listOf(LeadershipAssigneeUi(userId = task.assigneeUserId, name = task.assigneeName)),
        assigneeId = task.assigneeUserId,
        title = task.title,
        body = task.body,
        rowVersion = task.rowVersion,
        attachments = task.attachments.sortedBy { it.position }.map { stored ->
            DraftAttachment(
                key = stored.attachmentId.ifBlank { stored.proofId },
                kind = LeadershipAttachmentKind.from(stored.kind),
                fileName = stored.fileName,
                mimeType = stored.mimeType,
                sizeBytes = stored.sizeBytes,
                durationMs = stored.durationMs,
                localPath = "",
                captureSource = "",
                proofId = stored.proofId,
                uploadState = LeadershipDraftUploadState.UPLOADED,
            )
        },
    )

    private fun startRecording() {
        val current = draft.value
        if (current.recordingElapsedMs != null || current.sending) return
        if (current.attachments.size >= LEADERSHIP_TASK_ATTACHMENT_CAP) {
            draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_too_many)) }
            return
        }
        try {
            recorder.start()
        } catch (error: Exception) {
            crashReporter.recordException(error, "leadership task voice note start failed")
            analytics.track(
                AnalyticsEventsLeadershipTasks.FAILURE,
                mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS)),
            )
            draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_recording_failed)) }
            return
        }
        draft.update { it.copy(recordingElapsedMs = 0L, message = null) }
        recordingTicker?.cancel()
        recordingTicker = viewModelScope.launch {
            val startedAt = System.currentTimeMillis()
            while (true) {
                delay(RECORDING_TICK_MS)
                val elapsed = System.currentTimeMillis() - startedAt
                draft.update { it.copy(recordingElapsedMs = elapsed) }
                if (elapsed >= MAX_RECORDING_MS) {
                    stopRecording()
                    break
                }
            }
        }
    }

    private fun stopRecording() {
        recordingTicker?.cancel()
        recordingTicker = null
        if (draft.value.recordingElapsedMs == null) return
        val note = recorder.stop()
        draft.update { it.copy(recordingElapsedMs = null) }
        if (note == null) {
            draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_recording_failed)) }
            return
        }
        voiceNoteCount += 1
        val name = appContext.getString(R.string.leadership_tasks_voice_note_file_name, voiceNoteCount)
        addAttachment(
            DraftAttachment(
                key = UUID.randomUUID().toString(),
                kind = LeadershipAttachmentKind.AUDIO,
                fileName = name,
                mimeType = MIME_AUDIO_M4A,
                sizeBytes = note.sizeBytes,
                durationMs = note.durationMs,
                localPath = note.localPath,
                captureSource = SOURCE_RECORDER,
            ),
        )
        analytics.track(AnalyticsEventsLeadershipTasks.AUDIO_RECORDED)
    }

    private fun importPicked(uris: List<String>, source: String) {
        if (uris.isEmpty() || draft.value.sending) return
        viewModelScope.launch {
            for (uri in uris) {
                if (draft.value.attachments.size >= LEADERSHIP_TASK_ATTACHMENT_CAP) {
                    draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_too_many)) }
                    break
                }
                when (val result = importer.import(uri, MAX_ATTACHMENT_BYTES)) {
                    is ImportResult.Imported -> {
                        val imported = result.attachment
                        addAttachment(
                            DraftAttachment(
                                key = UUID.randomUUID().toString(),
                                kind = kindFor(imported.mimeType),
                                fileName = imported.fileName,
                                mimeType = imported.mimeType,
                                sizeBytes = imported.sizeBytes,
                                durationMs = imported.durationMs,
                                localPath = imported.localPath,
                                captureSource = source,
                            ),
                        )
                    }
                    is ImportResult.TooLarge ->
                        draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_too_large)) }
                    is ImportResult.Failed -> {
                        crashReporter.recordException(result.cause, "leadership task attachment import failed")
                        analytics.track(
                            AnalyticsEventsLeadershipTasks.FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to (result.cause.message ?: "unknown").take(MAX_REASON_CHARS)),
                        )
                        draft.update { it.copy(message = appContext.getString(R.string.leadership_tasks_msg_attach_failed)) }
                    }
                }
            }
        }
    }

    /** Adds one attachment under the cap; the cap is the last word even if a picker returned more. */
    internal fun addAttachment(attachment: DraftAttachment) {
        var added = false
        draft.update { current ->
            if (current.attachments.size >= LEADERSHIP_TASK_ATTACHMENT_CAP) {
                current.copy(message = appContext.getString(R.string.leadership_tasks_msg_too_many))
            } else {
                added = true
                current.copy(attachments = current.attachments + attachment, message = null)
            }
        }
        if (added) {
            analytics.track(
                AnalyticsEventsLeadershipTasks.ATTACHMENT_ADDED,
                mapOf(AnalyticsEvents.Params.KIND to attachment.kind.wireValue),
            )
        }
    }

    private fun removeAttachment(key: String) {
        if (draft.value.sending) return
        draft.update { current -> current.copy(attachments = current.attachments.filterNot { it.key == key }) }
    }

    /**
     * Upload every local attachment under its OWN key, then send the task under the DRAFT's key.
     * Nothing is re-minted on a retry: an attachment that already has a proof id is skipped, and
     * the task request goes out under the same key it went out with the first time.
     */
    private fun send() {
        val current = draft.value
        if (current.sending || current.recordingElapsedMs != null) return
        if (current.title.isBlank() || current.assigneeId.isBlank()) return
        viewModelScope.launch {
            draft.update { it.copy(sending = true, message = null) }
            try {
                for (attachment in draft.value.attachments) {
                    if (attachment.proofId != null) continue
                    setUploadState(attachment.key, LeadershipDraftUploadState.UPLOADING)
                    val uploaded = repository.uploadAttachment(
                        idempotencyKey = attachment.key,
                        kind = attachment.kind.wireValue,
                        fileName = attachment.fileName,
                        mimeType = attachment.mimeType,
                        localFilePath = attachment.localPath,
                        durationMs = attachment.durationMs,
                        captureSource = attachment.captureSource,
                    )
                    when (uploaded) {
                        is AppResult.Ok -> draft.update { d ->
                            d.copy(
                                attachments = d.attachments.map {
                                    if (it.key == attachment.key) it.copy(proofId = uploaded.value, uploadState = LeadershipDraftUploadState.UPLOADED) else it
                                },
                            )
                        }
                        is AppResult.Err -> {
                            setUploadState(attachment.key, LeadershipDraftUploadState.FAILED)
                            uploaded.cause?.let { crashReporter.recordException(it, "leadership task attachment upload failed") }
                            analytics.track(
                                AnalyticsEventsLeadershipTasks.FAILURE,
                                mapOf(AnalyticsEvents.Params.REASON to (uploaded.cause?.message ?: uploaded.message).take(MAX_REASON_CHARS)),
                            )
                            draft.update {
                                it.copy(message = uploaded.message.ifBlank { appContext.getString(R.string.leadership_tasks_msg_not_sent) })
                            }
                            return@launch
                        }
                    }
                }
                val ready = draft.value
                val refs = ready.attachments.map {
                    LeadershipTaskAttachmentRefDto(proofId = it.proofId.orEmpty(), kind = it.kind.wireValue, fileName = it.fileName)
                }
                val result = if (isEdit) {
                    repository.editTask(
                        taskId = editingTaskId,
                        idempotencyKey = taskIdempotencyKey,
                        request = LeadershipTaskEditRequestDto(
                            title = ready.title.trim(),
                            body = ready.body.trim(),
                            attachments = refs,
                            rowVersion = ready.rowVersion,
                        ),
                    )
                } else {
                    repository.raiseTask(
                        idempotencyKey = taskIdempotencyKey,
                        request = LeadershipTaskRaiseRequestDto(
                            title = ready.title.trim(),
                            body = ready.body.trim(),
                            assigneeUserId = ready.assigneeId,
                            attachments = refs,
                        ),
                    )
                }
                when (result) {
                    is AppResult.Ok -> {
                        analytics.track(
                            if (isEdit) AnalyticsEventsLeadershipTasks.TASK_EDITED else AnalyticsEventsLeadershipTasks.TASK_RAISED,
                            mapOf(AnalyticsEvents.Params.COUNT to refs.size.toString()),
                        )
                        draft.update { it.copy(sentTaskId = result.value.taskId) }
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "leadership task send failed") }
                        analytics.track(
                            AnalyticsEventsLeadershipTasks.FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to (result.cause?.message ?: result.message).take(MAX_REASON_CHARS)),
                        )
                        draft.update {
                            it.copy(message = result.message.ifBlank { appContext.getString(R.string.leadership_tasks_msg_not_sent) })
                        }
                    }
                }
            } finally {
                draft.update { it.copy(sending = false) }
            }
        }
    }

    private fun setUploadState(key: String, state: LeadershipDraftUploadState) {
        draft.update { d -> d.copy(attachments = d.attachments.map { if (it.key == key) it.copy(uploadState = state) else it }) }
    }

    private fun Draft.toUiState(): LeadershipTaskComposeUiState = LeadershipTaskComposeUiState(
        isEdit = isEdit,
        assignees = assignees,
        assigneesLoading = assigneesLoading || (isEdit && !seeded),
        selectedAssigneeId = assigneeId,
        title = title,
        body = body,
        attachments = attachments.map {
            LeadershipDraftAttachmentUi(
                listKey = it.key,
                kind = it.kind,
                fileName = it.fileName,
                sizeLabel = formatBytes(it.sizeBytes),
                durationLabel = it.durationMs?.takeIf { d -> d > 0L }?.let(::formatClock).orEmpty(),
                localPath = it.localPath,
                uploadState = it.uploadState,
            )
        },
        recordingElapsedMs = recordingElapsedMs,
        sending = sending,
        sentTaskId = sentTaskId,
        canSend = title.isNotBlank() && assigneeId.isNotBlank() && !sending && recordingElapsedMs == null && seeded,
        message = message,
    )

    private fun kindFor(mimeType: String): LeadershipAttachmentKind = when {
        mimeType.startsWith("image/") -> LeadershipAttachmentKind.PHOTO
        mimeType.startsWith("video/") -> LeadershipAttachmentKind.VIDEO
        mimeType.startsWith("audio/") -> LeadershipAttachmentKind.AUDIO
        else -> LeadershipAttachmentKind.FILE
    }

    internal companion object {
        const val ARG_TASK_ID = "task_id"
        const val KEY_TASK_IDEMPOTENCY = "leadership_task_idempotency_key"
        const val MAX_TITLE_CHARS = 160
        const val MAX_BODY_CHARS = 4000
        /** 50 MB per attachment. */
        const val MAX_ATTACHMENT_BYTES = 50L * 1024L * 1024L
        const val MAX_RECORDING_MS = 10L * 60L * 1000L
        const val RECORDING_TICK_MS = 250L
        const val MAX_REASON_CHARS = 120
        const val MIME_AUDIO_M4A = "audio/mp4"
        const val SOURCE_RECORDER = "in_app_recorder"
        const val PICKER_GALLERY = "gallery_picker"
        const val PICKER_FILE = "file_picker"
    }
}
