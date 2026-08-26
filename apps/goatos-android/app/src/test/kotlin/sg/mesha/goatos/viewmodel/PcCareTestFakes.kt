package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.PcCareRepository
import sg.mesha.goatos.core.data.PcCareScanOutcome
import sg.mesha.goatos.core.data.PcCareWorklistQuery
import sg.mesha.goatos.core.data.cache.PcCareAnimalRowEntity
import sg.mesha.goatos.core.data.cache.PcCareScanStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.PcCareCreateTaskRequestDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerCatalogDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerShedsDto
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.core.network.dto.PcCareTaskDto
import sg.mesha.goatos.core.network.dto.PcCareTaskProofDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

/** Fixture builder: one PC Care task detail with the given category slot contract. */
internal fun pcCareTaskDtoFixture(
    taskId: String = "task-1",
    category: String = "hoof_trimming",
    status: String = "open",
    workState: String = "scheduled",
    reworkReason: String = "",
    rowVersion: Int = 1,
    expectedSlots: List<PcCareSlotDto> = listOf(
        PcCareSlotDto(fieldKey = "before_video", label = "Before trimming"),
        PcCareSlotDto(fieldKey = "during_video", label = "While trimming", minDurationHintSeconds = 10),
        PcCareSlotDto(fieldKey = "after_video", label = "After trimming"),
    ),
    taskProofs: List<PcCareTaskProofDto> = emptyList(),
): PcCareTaskDto = PcCareTaskDto(
    taskId = taskId,
    category = category,
    parkId = "park-1",
    parkLabel = "CPT",
    shedId = "shed-1",
    shedLabel = "Castro",
    partitionLabel = "2",
    operationalLocationDisplay = "Castro 2",
    plannedBusinessDate = "2026-08-21",
    dueBusinessDate = "2026-08-21",
    workState = workState,
    status = status,
    reworkReason = reworkReason,
    rowVersion = rowVersion,
    assigneeNames = listOf("Amit Kumar"),
    expectedSlots = expectedSlots,
    taskProofs = taskProofs,
)

internal fun pcCareAnimalEntity(
    taskId: String = "task-1",
    tag: String = "tag-1",
    scanSyncStatus: String = PcCareScanStatus.SYNCED,
    scannedByName: String = "Amit Kumar",
    serverSlotsJson: String = "",
): PcCareAnimalRowEntity = PcCareAnimalRowEntity(
    taskId = taskId,
    normalizedTag = tag.trim().lowercase(),
    tagVerbatim = tag,
    animalRowId = if (scanSyncStatus == PcCareScanStatus.SYNCED) "row-$tag" else "",
    scannedByName = scannedByName,
    scanSyncStatus = scanSyncStatus,
    serverSlotsJson = serverSlotsJson,
    updatedAt = 0L,
)

/**
 * In-memory [PcCareRepository] with the REAL duplicate semantics of the production repository:
 * a tag already present (and not terminally failed) is a Duplicate and enqueues NOTHING.
 */
internal class FakePcCareRepository : PcCareRepository {
    val detailFlow = MutableStateFlow<PcCareTaskDto?>(null)
    val animalsFlow = MutableStateFlow<List<PcCareAnimalRowEntity>>(emptyList())
    val worklistQueries = mutableListOf<PcCareWorklistQuery>()
    var worklistTasks: List<PcCareTaskDto> = emptyList()

    var scanEnqueues = 0
        private set
    var submitCalls = mutableListOf<Pair<String, Int>>()
    var slotRegistrations = mutableListOf<List<String>>()
    var taskProofRegistrations = mutableListOf<List<String>>()
    val proofDownloadUrls = mutableMapOf<String, String>()
    var pollCount = 0
        private set
    var failNextSubmit = false

    override fun worklistRows(query: PcCareWorklistQuery): Flow<androidx.paging.PagingData<PcCareTaskDto>> {
        worklistQueries += query
        return flowOf(androidx.paging.PagingData.from(worklistTasks))
    }
    val invalidatedWorklistQueries = mutableListOf<PcCareWorklistQuery>()
    override suspend fun invalidateWorklist(query: PcCareWorklistQuery) {
        invalidatedWorklistQueries += query
    }

    override fun observeTaskDetail(taskId: String): Flow<PcCareTaskDto?> = detailFlow

    override fun observeTaskRowStatus(taskId: String): Flow<String?> = detailFlow.map { it?.status }

    override suspend fun refreshTaskDetail(taskId: String) = Unit

    override fun observeAnimals(taskId: String): Flow<List<PcCareAnimalRowEntity>> = animalsFlow

    val rosterFlow = MutableStateFlow<List<String>>(emptyList())
    var rosterRefreshCount = 0
        private set

    override fun observeRoster(taskId: String): Flow<List<String>> = rosterFlow

    override suspend fun refreshRoster(taskId: String) {
        rosterRefreshCount++
    }

    override suspend fun pollTaskOnce(taskId: String) {
        pollCount++
    }

    override suspend fun recordScan(taskId: String, tagVerbatim: String): PcCareScanOutcome {
        val normalized = tagVerbatim.trim().lowercase()
        if (normalized.isEmpty()) return PcCareScanOutcome.Failed("blank tag")
        val existing = animalsFlow.value.firstOrNull { it.normalizedTag == normalized }
        if (existing != null && existing.scanSyncStatus != PcCareScanStatus.FAILED) {
            return PcCareScanOutcome.Duplicate(existing.scannedByName)
        }
        scanEnqueues++
        animalsFlow.value = animalsFlow.value + pcCareAnimalEntity(
            taskId = taskId,
            tag = tagVerbatim.trim(),
            scanSyncStatus = PcCareScanStatus.PENDING,
            scannedByName = "",
        )
        return PcCareScanOutcome.Queued
    }

    override suspend fun registerSlotProof(
        taskId: String,
        normalizedTag: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> {
        slotRegistrations += listOf(taskId, normalizedTag, slotFieldKey, proofOutboxItemId)
        return AppResult.Ok("slot-outbox-${slotRegistrations.size}")
    }

    override suspend fun registerTaskProof(
        taskId: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> {
        taskProofRegistrations += listOf(taskId, slotFieldKey, proofOutboxItemId)
        return AppResult.Ok("task-proof-outbox-${taskProofRegistrations.size}")
    }

    override suspend fun proofDownloadUrl(proofId: String): AppResult<String> =
        AppResult.Ok(proofDownloadUrls[proofId] ?: "https://proof.local/$proofId")

    override suspend fun submitTask(taskId: String, rowVersion: Int): AppResult<String> {
        if (failNextSubmit) {
            failNextSubmit = false
            return AppResult.Err("test submit failure")
        }
        submitCalls += taskId to rowVersion
        return AppResult.Ok("submit-outbox-${submitCalls.size}")
    }

    override suspend fun persistTaskSubmitResult(taskId: String, status: String, rowVersion: Int, animalCount: Int) = Unit

    override suspend fun plannerCatalog(): PcCarePlannerCatalogDto = PcCarePlannerCatalogDto()

    override suspend fun plannerParkSheds(
        parkId: String,
        category: String,
        date: String,
        cursor: String?,
    ): PcCarePlannerShedsDto = PcCarePlannerShedsDto()

    override suspend fun createTask(idempotencyKey: String, request: PcCareCreateTaskRequestDto): PcCareTaskDto =
        pcCareTaskDtoFixture()

    override suspend fun cancelTask(taskId: String) = Unit
}

internal class PcCareFakeReaderPort : RfidReaderPort {
    override val status: StateFlow<RfidReaderStatus> = MutableStateFlow(RfidReaderStatus.READY)
    override val reads = MutableSharedFlow<RfidRead>(extraBufferCapacity = 16)
    override val readerName: StateFlow<String?> = MutableStateFlow(null)
    override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())
    override fun refreshStatus() = Unit
    override fun openSystemPairing() = Unit
    override fun setCaptureEnabled(enabled: Boolean) = Unit
    override fun setCompletionKeySwallowEnabled(enabled: Boolean) = Unit
    override fun onKeyEvent(event: android.view.KeyEvent): Boolean = false

    suspend fun emitRead(tag: String) {
        reads.emit(RfidRead(tag = tag, capturedAtDeviceMs = 0L))
    }
}

/** Minimal [SyncRepository]: only the abstract members; every enqueue keeps its default. */
internal class MinimalPcCareSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-outbox-1")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
