package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonObject
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.clock.MockLocationVerdict
import sg.mesha.goatos.core.data.ClockPunchFacts
import sg.mesha.goatos.core.data.ClockPunchFactsProvider
import sg.mesha.goatos.core.data.PenRoutinePageMeta
import sg.mesha.goatos.core.data.PenRoutinesRepository
import sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload
import sg.mesha.goatos.core.data.sync.PenRoutineSubmitProof
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ClockLocationDto
import sg.mesha.goatos.core.network.dto.PenRoutineDetailDto
import sg.mesha.goatos.core.network.dto.PenRoutineFilterDto
import sg.mesha.goatos.core.network.dto.PenRoutineFormDto
import sg.mesha.goatos.core.network.dto.PenRoutineIntegrityDto
import sg.mesha.goatos.core.network.dto.PenRoutineLocationDto
import sg.mesha.goatos.core.network.dto.PenRoutineOptionDto
import sg.mesha.goatos.core.network.dto.PenRoutineProofRuleDto
import sg.mesha.goatos.core.network.dto.PenRoutineQuestionDto
import sg.mesha.goatos.core.network.dto.PenRoutineTaskDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto

/** Test doubles for the pen-routine ViewModels (maintainer instruction 2026-09-16). */

/** In-memory [PenRoutinesRepository]: Room's role is played by a [MutableStateFlow] of the detail. */
class FakePenRoutinesRepository(
    initialDetail: PenRoutineTaskDto? = null,
    private val pages: List<PenRoutineTaskDto> = emptyList(),
) : PenRoutinesRepository {
    private val detail = MutableStateFlow(initialDetail)
    private val _pageMeta = MutableStateFlow(PenRoutinePageMeta())
    override val pageMeta: StateFlow<PenRoutinePageMeta> = _pageMeta

    val requestedFilters = mutableListOf<String>()
    val invalidatedFilters = mutableListOf<String>()
    var refreshDetailCalls: Int = 0
        private set
    val persistedDetails = mutableListOf<PenRoutineDetailDto>()

    /** Simulates the server's next composition landing in Room (a refresh, or a write reconcile). */
    fun emitDetail(next: PenRoutineTaskDto?) {
        detail.value = next
    }

    fun emitPageMeta(next: PenRoutinePageMeta) {
        _pageMeta.value = next
    }

    override fun tasks(filter: String): Flow<PagingData<PenRoutineTaskDto>> {
        requestedFilters += filter
        return flowOf(PagingData.from(pages))
    }

    override suspend fun invalidateTasks(filter: String) {
        invalidatedFilters += filter
    }

    override fun observeTask(taskId: String): Flow<PenRoutineTaskDto?> =
        detail.map { current -> current?.takeIf { it.taskId == taskId } }

    override suspend fun refreshTask(taskId: String) {
        refreshDetailCalls++
    }

    override suspend fun persistServerDetail(detail: PenRoutineDetailDto) {
        persistedDetails += detail
        this.detail.value = detail.task
    }
}

/** Records every pen-routine outbox enqueue so a test can assert the durable write actually happened. */
class RecordingPenRoutineSyncRepository : SyncRepository {
    data class Presence(
        val taskId: String,
        val rowVersion: Int,
        val eventType: String,
        val location: PenRoutineLocationDto,
        val integrity: PenRoutineIntegrityDto,
    )

    data class Submit(
        val taskId: String,
        val rowVersion: Int,
        val answers: JsonObject,
        val proofs: List<PenRoutineSubmitProof>,
        val location: PenRoutineLocationDto?,
    )

    val presences = mutableListOf<Presence>()
    val submits = mutableListOf<Submit>()
    var failNextSubmit: Boolean = false

    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    private val aliveGrains = MutableStateFlow<Set<String>>(emptySet())

    /** Drives the outbox row a queued write resolves to (QUEUED -> SUCCEEDED / FAILED). */
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

    override suspend fun enqueuePenRoutinePresence(
        taskId: String,
        rowVersion: Int,
        eventType: String,
        capturedAt: String,
        location: PenRoutineLocationDto,
        integrity: PenRoutineIntegrityDto,
    ): AppResult<String> {
        presences += Presence(taskId, rowVersion, eventType, location, integrity)
        return AppResult.Ok("pen-routine-presence-${presences.size}")
    }

    override suspend fun enqueuePenRoutineSubmit(
        taskId: String,
        rowVersion: Int,
        answers: JsonObject,
        proofs: List<PenRoutineSubmitProof>,
        capturedAt: String?,
        location: PenRoutineLocationDto?,
        integrity: PenRoutineIntegrityDto?,
    ): AppResult<String> {
        if (failNextSubmit) {
            failNextSubmit = false
            return AppResult.Err("Simulated submit enqueue failure.")
        }
        submits += Submit(taskId, rowVersion, answers, proofs, location)
        return AppResult.Ok("pen-routine-submit-${submits.size}")
    }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = AppResult.Ok("proof-upload-1")

    override suspend fun enqueueFeedTransportSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        proofOutboxItemId: String,
        slotProofs: Map<String, FeedSlotProofSourcePayload>,
        answers: JsonObject,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueFeedPackingComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        packingProofOutboxItemId: String,
        slotProofs: Map<String, FeedSlotProofSourcePayload>,
        answers: JsonObject,
    ): AppResult<String> = error("unused")
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

/** A scripted [ClockPunchFactsProvider]: what the device "sees" when the punch is tapped. */
class FakeClockPunchFactsProvider(
    var facts: ClockPunchFacts = ClockPunchFacts(
        location = ClockLocationDto(status = "captured", latitude = 11.0168, longitude = 76.9558, gpsAccuracyM = 8.0, address = "Coimbatore"),
        verdict = MockLocationVerdict(mockFix = false, mockApps = emptyList(), developerOptions = false),
        batteryPct = 80,
        networkKind = "wifi",
        offline = false,
    ),
) : ClockPunchFactsProvider {
    var captureCount: Int = 0
        private set

    override suspend fun capture(): ClockPunchFacts {
        captureCount++
        return facts
    }
}

// ---------------------------------------------------------------------------
// Payload builders — one place that knows the SERVER's wire vocabulary, so a test never
// hand-spells `scheduled` / `completed` / `danger` in five places.
// ---------------------------------------------------------------------------

fun penRoutineForm(
    questions: List<PenRoutineQuestionDto> = penRoutineQuestions(),
    photoMin: Int = 1,
    photoMax: Int = 2,
    videoMin: Int = 0,
    videoMax: Int = 1,
    presence: String = "required",
): PenRoutineFormDto = PenRoutineFormDto(
    questions = questions,
    photo = PenRoutineProofRuleDto(min = photoMin, max = photoMax),
    video = PenRoutineProofRuleDto(min = videoMin, max = videoMax),
    presence = presence,
)

fun penRoutineQuestions(): List<PenRoutineQuestionDto> = listOf(
    PenRoutineQuestionDto(
        id = "cleaned",
        kind = "yes_no",
        title = "Was the pen cleaned?",
        required = true,
        options = listOf(PenRoutineOptionDto("yes", "Yes"), PenRoutineOptionDto("no", "No")),
    ),
    PenRoutineQuestionDto(
        id = "water",
        kind = "choice",
        title = "Water trough",
        required = true,
        options = listOf(PenRoutineOptionDto("clean", "Clean"), PenRoutineOptionDto("dirty", "Dirty")),
    ),
    PenRoutineQuestionDto(
        id = "issues",
        kind = "multi_choice",
        title = "Anything seen",
        options = listOf(PenRoutineOptionDto("limping", "Limping"), PenRoutineOptionDto("coughing", "Coughing")),
    ),
    PenRoutineQuestionDto(id = "count", kind = "number", title = "Sick animals seen", min = 0.0, max = 500.0, unit = "animals"),
    PenRoutineQuestionDto(id = "note", kind = "text", title = "Anything else", hint = "Optional"),
)

fun penRoutineTask(
    taskId: String = PEN_ROUTINE_TEST_TASK_ID,
    workState: String = "scheduled",
    status: String = "open",
    canSubmit: Boolean = true,
    canCheckIn: Boolean = true,
    inPen: Boolean = false,
    presenceRequired: Boolean = true,
    rowVersion: Int = 3,
    stateChip: String = "Due today",
    stateTone: String = "info",
    doneLine: String = "",
    reworkReason: String = "",
    verified: Boolean = false,
    form: PenRoutineFormDto = penRoutineForm(presence = if (presenceRequired) "required" else "off"),
    submittedAt: String? = null,
): PenRoutineTaskDto = PenRoutineTaskDto(
    taskId = taskId,
    routineId = "routine-clean",
    routineVersion = 2,
    routineName = "Pen cleaning",
    title = "Pen cleaning · Castro 2 · Coimbatore",
    parkId = "park-cbe",
    parkName = "Coimbatore",
    shedId = "shed-castro",
    shedName = "Castro",
    partitionLabel = "2",
    operationalLocationDisplay = "Castro 2",
    reasonLine = "Every day",
    sourceBusinessDate = "2026-09-16",
    plannedBusinessDate = "2026-09-16",
    dueBusinessDate = "2026-09-16",
    workState = workState,
    status = status,
    stateChip = stateChip,
    stateTone = stateTone,
    instruction = "Check the pen and answer the questions.",
    evidenceLine = "2 questions · 1 photo · check in",
    reviewKind = "verifier",
    form = form,
    presenceRequired = presenceRequired,
    presenceLine = if (inPen) "In pen since 07:12" else "Check in to the pen before you start",
    inPen = inPen,
    canCheckIn = canCheckIn,
    canSubmit = canSubmit,
    verified = verified,
    doneLine = doneLine,
    reworkReason = reworkReason,
    submittedAt = submittedAt,
    rowVersion = rowVersion,
)

/**
 * A GENERAL PARK task (2026-09-17 revision): `scope_kind = park`, and the pen fields arrive as ""
 * exactly as the backend sends them; title, reason and presence line stay backend copy.
 */
fun penRoutineParkTask(inPen: Boolean = false): PenRoutineTaskDto = penRoutineTask(inPen = inPen).copy(
    scopeKind = "park",
    routineName = "Medicine store check",
    title = "Medicine store check · Coimbatore",
    shedId = "",
    shedName = "",
    partitionLabel = "",
    operationalLocationDisplay = "",
    presenceLine = if (inPen) "Checked in since 07:12" else "Check in before you start",
    instruction = "Check the medicine store.",
)

fun penRoutineFilters(selected: String = "todo"): List<PenRoutineFilterDto> = listOf(
    PenRoutineFilterDto(key = "todo", label = "To do", count = 4, selected = selected == "todo", emptyMessage = "No routines due today."),
    PenRoutineFilterDto(key = "done", label = "Done", count = 9, selected = selected == "done", emptyMessage = "Nothing done yet."),
)

/** A UUID, because the proof platform validates the capture's subject/scope as one. */
const val PEN_ROUTINE_TEST_TASK_ID = "77777777-8888-9999-aaaa-bbbbbbbbbbbb"
