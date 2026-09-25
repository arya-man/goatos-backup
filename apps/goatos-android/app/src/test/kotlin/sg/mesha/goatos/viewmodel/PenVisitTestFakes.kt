package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.PenVisitPageMeta
import sg.mesha.goatos.core.data.PenVisitsRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.PenVisitDetailDto
import sg.mesha.goatos.core.network.dto.PenVisitDto
import sg.mesha.goatos.core.network.dto.PenVisitFilterDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto

/** Test doubles for the pen-visit ViewModels (maintainer decision 2026-09-07). */

/** In-memory [PenVisitsRepository]: Room's role is played by a [MutableStateFlow] of the detail. */
class FakePenVisitsRepository(
    initialDetail: PenVisitDto? = null,
    private val pages: List<PenVisitDto> = emptyList(),
) : PenVisitsRepository {
    private val detail = MutableStateFlow(initialDetail)
    private val _pageMeta = MutableStateFlow(PenVisitPageMeta())
    override val pageMeta: StateFlow<PenVisitPageMeta> = _pageMeta

    val requestedFilters = mutableListOf<String>()
    val invalidatedFilters = mutableListOf<String>()
    var refreshDetailCalls: Int = 0
        private set
    val persistedDetails = mutableListOf<PenVisitDetailDto>()

    /** Simulates the server's next composition landing in Room (a refresh, or a write reconcile). */
    fun emitDetail(next: PenVisitDto?) {
        detail.value = next
    }

    fun emitPageMeta(next: PenVisitPageMeta) {
        _pageMeta.value = next
    }

    override fun visits(filter: String): Flow<PagingData<PenVisitDto>> {
        requestedFilters += filter
        return flowOf(PagingData.from(pages))
    }

    override suspend fun invalidateVisits(filter: String) {
        invalidatedFilters += filter
    }

    override fun observeVisit(taskId: String): Flow<PenVisitDto?> =
        detail.map { current -> current?.takeIf { it.taskId == taskId } }

    /** A server that holds no such visit is unreachable here: a refresh "lands" only with a detail. */
    override suspend fun refreshVisit(taskId: String): Boolean {
        refreshDetailCalls++
        return detail.value != null
    }

    override suspend fun persistServerDetail(detail: PenVisitDetailDto) {
        persistedDetails += detail
        this.detail.value = detail.task
    }
}

/** Records every pen-visit outbox enqueue so a test can assert the durable write actually happened. */
class RecordingPenVisitSyncRepository : SyncRepository {
    data class Submit(val taskId: String, val rowVersion: Int, val proofOutboxItemId: String)

    val submits = mutableListOf<Submit>()
    var failNextSubmit: Boolean = false

    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    private val aliveGrains = MutableStateFlow<Set<String>>(emptySet())

    /** Drives the outbox row a queued submit resolves to (QUEUED -> SUCCEEDED / FAILED). */
    fun emitItem(itemId: String, item: SyncQueueItem?) {
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = item
    }

    /** Drives the ACTIVE grain set a process-death-surviving screen reads "Sending" from. */
    fun emitAliveGrains(next: Set<String>) {
        aliveGrains.value = next
    }

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

    override fun observeSubmittedForReviewGrains(): Flow<Set<String>> = aliveGrains

    override suspend fun enqueuePenVisitSubmit(
        taskId: String,
        rowVersion: Int,
        proofOutboxItemId: String,
    ): AppResult<String> {
        if (failNextSubmit) {
            failNextSubmit = false
            return AppResult.Err("Simulated submit enqueue failure.")
        }
        submits += Submit(taskId, rowVersion, proofOutboxItemId)
        return AppResult.Ok("pen-visit-submit-${submits.size}")
    }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-upload-1")

    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
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

// ---------------------------------------------------------------------------
// Payload builders — one place that knows the SERVER's wire vocabulary, so a test never
// hand-spells `scheduled` / `completed` / `danger` in five places.
// ---------------------------------------------------------------------------

fun penVisit(
    taskId: String = PEN_VISIT_TEST_TASK_ID,
    workState: String = "scheduled",
    status: String = "open",
    canSubmit: Boolean = true,
    rowVersion: Int = 3,
    stateChip: String = "Visit pen today",
    stateTone: String = "info",
    doneLine: String = "",
    reworkReason: String = "",
    verified: Boolean = false,
): PenVisitDto = PenVisitDto(
    taskId = taskId,
    title = "Visit Castro 2 · Coimbatore",
    parkId = "park-cbe",
    parkName = "Coimbatore",
    shedId = "shed-castro",
    shedName = "Castro",
    partitionLabel = "2",
    operationalLocationDisplay = "Castro 2",
    reasons = listOf("vaccination"),
    reasonLabels = listOf("Vaccination"),
    reasonLine = "Vaccination yesterday",
    sourceBusinessDate = "2026-09-06",
    plannedBusinessDate = "2026-09-07",
    dueBusinessDate = "2026-09-07",
    workState = workState,
    status = status,
    stateChip = stateChip,
    stateTone = stateTone,
    reworkReason = reworkReason,
    verified = verified,
    instruction = "Walk the pen and record one video of the animals.",
    doneLine = doneLine,
    canSubmit = canSubmit,
    rowVersion = rowVersion,
)

fun penVisitFilters(selected: String = "todo"): List<PenVisitFilterDto> = listOf(
    PenVisitFilterDto(key = "todo", label = "To do", count = 2, selected = selected == "todo", emptyMessage = "No pens to visit today."),
    PenVisitFilterDto(key = "done", label = "Done", count = 5, selected = selected == "done", emptyMessage = "Nothing visited yet."),
)

/** A UUID, because the proof platform validates the capture's subject/scope as one. */
const val PEN_VISIT_TEST_TASK_ID = "66666666-7777-8888-9999-aaaaaaaaaaaa"
