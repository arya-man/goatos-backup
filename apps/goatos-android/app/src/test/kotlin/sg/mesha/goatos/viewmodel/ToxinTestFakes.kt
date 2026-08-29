package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.ToxinRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ToxinOutcomeOptionDto
import sg.mesha.goatos.core.network.dto.ToxinStepDto
import sg.mesha.goatos.core.network.dto.ToxinTaskDetailDto
import sg.mesha.goatos.core.network.dto.ToxinTaskDto
import sg.mesha.goatos.core.network.dto.ToxinTaskFilterDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto

/** Test doubles for the Toxin ViewModels (module toxin, maintainer decision 2026-08-25). */

/** In-memory [ToxinRepository]: Room's role is played by a [MutableStateFlow] of the detail. */
class FakeToxinRepository(
    initialDetail: ToxinTaskDetailDto? = null,
    private val pages: List<ToxinTaskDto> = emptyList(),
) : ToxinRepository {
    private val detail = MutableStateFlow(initialDetail)
    private val _statusCounts = MutableStateFlow<Map<String, Int>>(emptyMap())
    private val _filters = MutableStateFlow<List<ToxinTaskFilterDto>>(emptyList())

    var refreshDetailCalls: Int = 0
        private set
    val invalidatedStatuses = mutableListOf<String>()

    /** Every filter key the ViewModel asked the pager for, in order. */
    val requestedFilters = mutableListOf<String>()
    val persistedDetails = mutableListOf<ToxinTaskDetailDto>()

    /** Simulates the server's next composition landing in Room (a refresh, or a write reconcile). */
    fun emitDetail(next: ToxinTaskDetailDto?) {
        detail.value = next
    }

    override fun tasks(filter: String): Flow<PagingData<ToxinTaskDto>> {
        requestedFilters += filter
        return flowOf(PagingData.from(pages))
    }

    override val statusCounts: StateFlow<Map<String, Int>> = _statusCounts

    override val filters: StateFlow<List<ToxinTaskFilterDto>> = _filters

    /** Simulates the backend's composed chips landing from a list refresh. */
    fun emitFilters(next: List<ToxinTaskFilterDto>) {
        _filters.value = next
    }

    override suspend fun invalidateTasks(filter: String) {
        invalidatedStatuses += filter
    }

    override fun observeTaskDetail(taskId: String): Flow<ToxinTaskDetailDto?> =
        detail.map { current -> current?.takeIf { it.taskId == taskId } }

    override suspend fun refreshTaskDetail(taskId: String) {
        refreshDetailCalls++
    }

    override suspend fun persistServerDetail(detail: ToxinTaskDetailDto) {
        persistedDetails += detail
        this.detail.value = detail
    }
}

/** Records every toxin outbox enqueue so a test can assert the durable write actually happened. */
class RecordingToxinSyncRepository : SyncRepository {
    data class StepComplete(val taskId: String, val stepNo: Int, val proofOutboxItemId: String)
    data class Submit(val taskId: String, val outcome: String, val stripPhotoOutboxItemId: String)

    val stepCompletes = mutableListOf<StepComplete>()
    val submits = mutableListOf<Submit>()
    var failNextStepComplete: Boolean = false
    var failNextSubmit: Boolean = false

    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

    override suspend fun enqueueToxinStepComplete(
        taskId: String,
        stepNo: Int,
        proofOutboxItemId: String,
    ): AppResult<String> {
        if (failNextStepComplete) {
            failNextStepComplete = false
            return AppResult.Err("Simulated step enqueue failure.")
        }
        stepCompletes += StepComplete(taskId, stepNo, proofOutboxItemId)
        return AppResult.Ok("toxin-step-${stepCompletes.size}")
    }

    override suspend fun enqueueToxinSubmit(
        taskId: String,
        outcome: String,
        stripPhotoOutboxItemId: String,
    ): AppResult<String> {
        if (failNextSubmit) {
            failNextSubmit = false
            return AppResult.Err("Simulated submit enqueue failure.")
        }
        submits += Submit(taskId, outcome, stripPhotoOutboxItemId)
        return AppResult.Ok("toxin-submit-${submits.size}")
    }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-upload-1")

    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDirectionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, sessionNo: Int, targetDate: String, workflow: String): AppResult<String> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

/** A photo capture source that always returns one in-app-camera photo. */
class AlwaysCapturingPhotoSource : sg.mesha.goatos.capture.PhotoCaptureSource {
    var captureCount: Int = 0
        private set

    override suspend fun capturePhoto(
        context: sg.mesha.goatos.capture.PhotoCaptureContext,
    ): sg.mesha.goatos.capture.CapturedPhoto {
        captureCount++
        return sg.mesha.goatos.capture.CapturedPhoto(
            localUri = "file:///strip-$captureCount.jpg",
            capturedAtMs = 1_000L + captureCount,
        )
    }
}

// ---------------------------------------------------------------------------
// Payload builders — one place that knows the SERVER's wire vocabulary, so a test
// never hand-spells `available` / `photo_reading` in five places.
// ---------------------------------------------------------------------------

fun toxinStep(
    stepNo: Int,
    kind: String = "video",
    state: String = "locked",
    title: String = "Step $stepNo",
    instruction: String = "Instruction $stepNo",
    availableAt: String = "",
    completedBy: String = "",
    completedAt: String = "",
): ToxinStepDto = ToxinStepDto(
    stepNo = stepNo,
    kind = kind,
    title = title,
    instruction = instruction,
    state = state,
    availableAt = availableAt,
    completedBy = completedBy,
    completedAt = completedAt,
)

fun toxinDetail(
    taskId: String = TOXIN_TEST_TASK_ID,
    steps: List<ToxinStepDto>,
    statusChip: String = "In progress",
    contextLine: String = "Maize · Kumar Traders · Load 4",
    stepsDone: Int = steps.count { it.state == "done" },
    stepsTotal: Int = steps.size,
    readingGuide: List<String> = listOf("Two lines means negative"),
    outcomeOptions: List<ToxinOutcomeOptionDto> = listOf(
        ToxinOutcomeOptionDto(value = "negative", label = "Negative"),
        ToxinOutcomeOptionDto(value = "positive", label = "Positive"),
        ToxinOutcomeOptionDto(value = "invalid", label = "Invalid strip"),
    ),
): ToxinTaskDetailDto = ToxinTaskDetailDto(
    taskId = taskId,
    status = "in_progress",
    statusChip = statusChip,
    contextLine = contextLine,
    stepsDone = stepsDone,
    stepsTotal = stepsTotal,
    steps = steps,
    readingGuide = readingGuide,
    outcomeOptions = outcomeOptions,
)

/** A UUID, because the proof platform validates the capture's subject/scope as one. */
const val TOXIN_TEST_TASK_ID = "11111111-2222-3333-4444-555555555555"
