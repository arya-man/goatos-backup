package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.LeadershipTaskPageMeta
import sg.mesha.goatos.core.data.LeadershipTasksRepository
import sg.mesha.goatos.core.network.dto.LeadershipAssigneeDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskEditRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskRaiseRequestDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskStatusRequestDto
import sg.mesha.goatos.leadershiptasks.AttachmentImporter
import sg.mesha.goatos.leadershiptasks.ImportResult
import sg.mesha.goatos.leadershiptasks.ImportedAttachment
import sg.mesha.goatos.leadershiptasks.RecordedVoiceNote
import sg.mesha.goatos.leadershiptasks.VoiceNoteRecorder

/** Test doubles for the Leadership Tasks ViewModels (maintainer request 2026-09-04). */

/** In-memory [LeadershipTasksRepository]: Room's role is played by a [MutableStateFlow] of the detail. */
class FakeLeadershipTasksRepository(
    initialDetail: LeadershipTaskDto? = null,
    private val pages: List<LeadershipTaskDto> = emptyList(),
    var assigneeList: List<LeadershipAssigneeDto> = emptyList(),
) : LeadershipTasksRepository {
    data class UploadCall(val idempotencyKey: String, val kind: String, val fileName: String, val localFilePath: String)
    data class RaiseCall(val idempotencyKey: String, val request: LeadershipTaskRaiseRequestDto)
    data class EditCall(val taskId: String, val idempotencyKey: String, val request: LeadershipTaskEditRequestDto)
    data class StatusCall(val taskId: String, val idempotencyKey: String, val request: LeadershipTaskStatusRequestDto)

    private val detail = MutableStateFlow(initialDetail)
    private val _pageMeta = MutableStateFlow(LeadershipTaskPageMeta())
    override val pageMeta: StateFlow<LeadershipTaskPageMeta> = _pageMeta

    val requestedFilters = mutableListOf<String>()
    val invalidatedFilters = mutableListOf<String>()
    var refreshDetailCalls: Int = 0
        private set
    val seenCalls = mutableListOf<String>()
    val uploads = mutableListOf<UploadCall>()
    val raises = mutableListOf<RaiseCall>()
    val edits = mutableListOf<EditCall>()
    val statusCalls = mutableListOf<StatusCall>()
    val fetchedProofs = mutableListOf<String>()

    /** Scripted failures: each consumed once. */
    var failNextUpload: Boolean = false
    var failNextRaise: Boolean = false
    var failNextStatus: Boolean = false
    var failSeen: Boolean = false
    var uploadCounter: Int = 0

    fun emitDetail(next: LeadershipTaskDto?) {
        detail.value = next
    }

    fun emitPageMeta(next: LeadershipTaskPageMeta) {
        _pageMeta.value = next
    }

    override fun tasks(filter: String): Flow<PagingData<LeadershipTaskDto>> {
        requestedFilters += filter
        return flowOf(PagingData.from(pages))
    }

    override suspend fun invalidateTasks(filter: String) {
        invalidatedFilters += filter
    }

    override fun observeTaskDetail(taskId: String): Flow<LeadershipTaskDto?> =
        detail.map { current -> current?.takeIf { it.taskId == taskId } }

    override suspend fun refreshTaskDetail(taskId: String) {
        refreshDetailCalls++
    }

    override suspend fun assignees(): AppResult<List<LeadershipAssigneeDto>> = AppResult.Ok(assigneeList)

    override suspend fun uploadAttachment(
        idempotencyKey: String,
        kind: String,
        fileName: String,
        mimeType: String,
        localFilePath: String,
        durationMs: Long?,
        captureSource: String,
    ): AppResult<String> {
        uploads += UploadCall(idempotencyKey, kind, fileName, localFilePath)
        if (failNextUpload) {
            failNextUpload = false
            return AppResult.Err("")
        }
        uploadCounter += 1
        return AppResult.Ok("proof-$uploadCounter")
    }

    override suspend fun raiseTask(idempotencyKey: String, request: LeadershipTaskRaiseRequestDto): AppResult<LeadershipTaskDto> {
        raises += RaiseCall(idempotencyKey, request)
        if (failNextRaise) {
            failNextRaise = false
            return AppResult.Err("")
        }
        val created = LeadershipTaskDto(taskId = "task-new", title = request.title, body = request.body)
        detail.value = created
        return AppResult.Ok(created)
    }

    override suspend fun editTask(taskId: String, idempotencyKey: String, request: LeadershipTaskEditRequestDto): AppResult<LeadershipTaskDto> {
        edits += EditCall(taskId, idempotencyKey, request)
        val updated = (detail.value ?: LeadershipTaskDto(taskId = taskId)).copy(title = request.title, body = request.body, rowVersion = request.rowVersion + 1)
        detail.value = updated
        return AppResult.Ok(updated)
    }

    override suspend fun changeStatus(taskId: String, idempotencyKey: String, request: LeadershipTaskStatusRequestDto): AppResult<LeadershipTaskDto> {
        statusCalls += StatusCall(taskId, idempotencyKey, request)
        if (failNextStatus) {
            failNextStatus = false
            return AppResult.Err("")
        }
        val moved = (detail.value ?: LeadershipTaskDto(taskId = taskId)).copy(status = request.status, rowVersion = request.rowVersion + 1)
        detail.value = moved
        return AppResult.Ok(moved)
    }

    override suspend fun setComment(taskId: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.LeadershipTaskCommentRequestDto): AppResult<LeadershipTaskDto> {
        val updated = (detail.value ?: LeadershipTaskDto(taskId = taskId)).copy(comment = request.comment)
        detail.value = updated
        return AppResult.Ok(updated)
    }

    override suspend fun markSeen(taskId: String): AppResult<LeadershipTaskDto> {
        seenCalls += taskId
        if (failSeen) return AppResult.Err("")
        val seen = (detail.value ?: LeadershipTaskDto(taskId = taskId)).copy(isSeen = true)
        detail.value = seen
        return AppResult.Ok(seen)
    }

    override suspend fun attachmentFile(proofId: String, fileName: String): AppResult<String> {
        fetchedProofs += proofId
        return AppResult.Ok("/cache/$proofId")
    }
}

/** A recorder that always yields one short note. */
class FakeVoiceNoteRecorder : VoiceNoteRecorder {
    var started = 0
        private set
    var recording = false
        private set

    override fun start() {
        started += 1
        recording = true
    }

    override fun stop(): RecordedVoiceNote? {
        if (!recording) return null
        recording = false
        return RecordedVoiceNote(localPath = "/drafts/voice-$started.m4a", durationMs = 4_200L, sizeBytes = 50_000L)
    }

    override fun discard() {
        recording = false
    }
}

/** An importer that copies nothing and answers with a scripted result per URI. */
class FakeAttachmentImporter(
    private val tooLarge: Set<String> = emptySet(),
) : AttachmentImporter {
    override suspend fun import(uri: String, maxBytes: Long): ImportResult {
        if (uri in tooLarge) return ImportResult.TooLarge(maxBytes + 1)
        val name = uri.substringAfterLast('/')
        val mime = when {
            name.endsWith(".jpg") -> "image/jpeg"
            name.endsWith(".mp4") -> "video/mp4"
            else -> "application/pdf"
        }
        return ImportResult.Imported(
            ImportedAttachment(localPath = "/drafts/$name", fileName = name, mimeType = mime, sizeBytes = 1_024L, durationMs = null),
        )
    }
}

/** A UUID, because the proof platform validates scope ids as one. */
const val LEADERSHIP_TEST_TASK_ID = "22222222-3333-4444-5555-666666666666"

fun leadershipTask(
    taskId: String = LEADERSHIP_TEST_TASK_ID,
    isSeen: Boolean = false,
    canChangeStatus: Boolean = true,
    isAssignee: Boolean = canChangeStatus,
    canEdit: Boolean = false,
    canCancel: Boolean = false,
    rowVersion: Int = 3,
    statusOptions: List<sg.mesha.goatos.core.network.dto.LeadershipTaskStatusOptionDto> = listOf(
        sg.mesha.goatos.core.network.dto.LeadershipTaskStatusOptionDto(key = "in_progress", label = "Start"),
    ),
    attachments: List<sg.mesha.goatos.core.network.dto.LeadershipTaskAttachmentDto> = emptyList(),
): LeadershipTaskDto = LeadershipTaskDto(
    taskId = taskId,
    taskNo = 12,
    numberLabel = "#12",
    title = "Fix the CPT water line",
    body = "Before the weekend.",
    status = "open",
    statusChip = "Open",
    assigneeUserId = "cxo-1",
    assigneeName = "Ravi",
    metaLine = "Raised by Hemant · 4 Sep 2026",
    rowVersion = rowVersion,
    isSeen = isSeen,
    isAssignee = isAssignee,
    isRaiser = !isAssignee,
    canEdit = canEdit,
    canChangeStatus = canChangeStatus,
    canCancel = canCancel,
    statusOptions = statusOptions,
    attachmentCount = attachments.size,
    attachments = attachments,
)
