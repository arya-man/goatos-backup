package sg.mesha.goatos.viewmodel

import android.content.Context
import android.os.Build
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenRoutines
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.ClockPunchFactsProvider
import sg.mesha.goatos.core.data.PenRoutinesRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofProcessingStatus
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.PEN_ROUTINE_PROOF_KIND_PHOTO
import sg.mesha.goatos.core.data.sync.PEN_ROUTINE_PROOF_KIND_VIDEO
import sg.mesha.goatos.core.data.sync.PenRoutineSubmitProof
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.penRoutineGrainKey
import sg.mesha.goatos.core.data.sync.parsePenRoutineQuestionProofFieldKey
import sg.mesha.goatos.core.data.sync.penRoutinePhotoFieldKey
import sg.mesha.goatos.core.data.sync.penRoutineQuestionProofFieldKey
import sg.mesha.goatos.core.data.sync.penRoutineTaskGroupKey
import sg.mesha.goatos.core.data.sync.penRoutineVideoFieldKey
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_SCOPE_PARK
import sg.mesha.goatos.core.network.dto.PenRoutineIntegrityDto
import sg.mesha.goatos.core.network.dto.PenRoutineLocationDto
import sg.mesha.goatos.core.network.dto.PenRoutineQuestionDto
import sg.mesha.goatos.core.network.dto.PenRoutineTaskDto
import sg.mesha.goatos.feature.penroutines.PenRoutineAnswerRowUi
import sg.mesha.goatos.feature.penroutines.PenRoutineDetailEvent
import sg.mesha.goatos.feature.penroutines.PenRoutineDetailUiState
import sg.mesha.goatos.feature.penroutines.PenRoutineOptionUi
import sg.mesha.goatos.feature.penroutines.PenRoutinePhase
import sg.mesha.goatos.feature.penroutines.PenRoutineQuestionKind
import sg.mesha.goatos.feature.penroutines.PenRoutineQuestionUi
import sg.mesha.goatos.feature.penroutines.PenRoutineQuestionProofKind
import sg.mesha.goatos.feature.penroutines.PenRoutineSlotKind
import sg.mesha.goatos.feature.penroutines.PenRoutineSlotUi
import sg.mesha.goatos.feature.penroutines.PenRoutineTone
import java.time.Instant
import javax.inject.Inject

/**
 * ONE routine task's detail state holder (maintainer instruction 2026-09-16,
 * docs/decisions/pen-routines.md).
 *
 * Room is the single source of truth: the screen renders from
 * [PenRoutinesRepository.observeTask] merged with the DURABLE proof rows of every capture slot
 * ([ProofCaptureRepository.observeProofs]), the queued presence/submit outbox rows, and the
 * phone's own draft answers (kept in [SavedStateHandle] so they survive process death). The
 * SERVER owns every visible sentence, `can_check_in` and `can_submit`; this class maps the
 * payload one-to-one and adds only the submit GATE — the local pre-check of exactly the rules the
 * server re-runs under its row lock (`domain.CheckSubmit`): every required question answered and
 * every answer fitting its kind, photo/video counts at their minimum, and presence satisfied when
 * the routine asks for it.
 *
 * Three writes, ONE FIFO lane per task ([penRoutineTaskGroupKey]): the `enter` punch, every
 * slot's PROOF_UPLOAD row, and the submit that references them. Everything after a REAL capture
 * runs inside `NonCancellable` so backing out of the screen can never orphan a clip the park head
 * actually shot (the PC Care defect of 2026-08-21).
 */
@HiltViewModel
class PenRoutineDetailViewModel @Inject constructor(
    private val repository: PenRoutinesRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val syncRepository: SyncRepository,
    private val factsProvider: ClockPunchFactsProvider,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val taskId: String = savedStateHandle.get<String>(ARG_TASK_ID).orEmpty()

    /** This phone's own transient state: what is mid-capture, and the rows it queued. */
    private data class Local(
        val isRefreshing: Boolean = false,
        val capturing: Boolean = false,
        /** True while the device location is being captured for the punch. */
        val locating: Boolean = false,
        /** The PEN_ROUTINE_PRESENCE outbox row id this screen queued; blank until it did. */
        val presenceOutboxItemId: String = "",
        /** The PEN_ROUTINE_SUBMIT outbox row id this screen queued; blank until it did. */
        val submitOutboxItemId: String = "",
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())

    /**
     * The draft answers, keyed by question id. A choice holds its option value(s); a number or
     * text question holds ONE element, the typed text. Kept in [SavedStateHandle] so a process
     * death mid-form brings the park head back to the answers he already gave.
     */
    private val draft: StateFlow<HashMap<String, ArrayList<String>>?> =
        savedStateHandle.getStateFlow<HashMap<String, ArrayList<String>>?>(KEY_DRAFT, null)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val presenceItem: Flow<SyncQueueItem?> = local
        .map { it.presenceOutboxItemId }
        .distinctUntilChanged()
        .flatMapLatest { id -> if (id.isBlank()) flowOf(latestDurableItem(OP_TYPE_PRESENCE)) else syncRepository.observeItem(id) }

    @OptIn(ExperimentalCoroutinesApi::class)
    private val submitItem: Flow<SyncQueueItem?> = local
        .map { it.submitOutboxItemId }
        .distinctUntilChanged()
        .flatMapLatest { id -> if (id.isBlank()) flowOf(latestDurableItem(OP_TYPE_SUBMIT)) else syncRepository.observeItem(id) }

    private data class Queue(val presence: SyncQueueItem?, val submit: SyncQueueItem?, val sending: Set<String>)

    private val queue: Flow<Queue> = combine(presenceItem, submitItem, syncRepository.observeSubmittedForReviewGrains()) { p, s, g -> Queue(p, s, g) }

    val state: StateFlow<PenRoutineDetailUiState> =
        combine(
            repository.observeTask(taskId),
            local,
            draft,
            proofCaptureRepository.observeProofs(taskId),
            queue,
        ) { detail, own, answers, proofs, q ->
            toUiState(detail, own, answers.orEmpty(), proofs, q)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PenRoutineDetailUiState())

    init {
        refresh()
        analytics.track(AnalyticsEventsPenRoutines.DETAIL_OPENED, mapOf(PARAM_TASK_ID to taskId, AnalyticsEvents.Params.SOURCE to "detail"))
    }

    fun onEvent(event: PenRoutineDetailEvent) {
        when (event) {
            PenRoutineDetailEvent.Refresh -> refresh()
            PenRoutineDetailEvent.Back -> Unit
            PenRoutineDetailEvent.CheckIn -> checkIn()
            is PenRoutineDetailEvent.SetChoice -> editDraft { it[event.questionId] = arrayListOf(event.value) }
            is PenRoutineDetailEvent.ToggleChoice -> editDraft { map ->
                val current = map[event.questionId].orEmpty()
                map[event.questionId] = ArrayList(if (event.value in current) current - event.value else current + event.value)
            }
            is PenRoutineDetailEvent.SetText -> editDraft { it[event.questionId] = arrayListOf(event.text) }
            is PenRoutineDetailEvent.CaptureSlot -> captureSlot(event.fieldKey)
            PenRoutineDetailEvent.Submit -> submit()
            is PenRoutineDetailEvent.ProofPreviewAction -> trackPreviewAction(event.fieldKey, event.action)
            PenRoutineDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
        }
    }

    private fun editDraft(edit: (HashMap<String, ArrayList<String>>) -> Unit) {
        val next = HashMap(draft.value.orEmpty())
        edit(next)
        savedStateHandle[KEY_DRAFT] = next
    }

    /** Non-blocking by contract: on failure the cached detail keeps rendering. */
    private fun refresh() {
        if (taskId.isBlank()) return
        viewModelScope.launch {
            local.update { it.copy(isRefreshing = true) }
            try {
                repository.refreshTask(taskId)
            } finally {
                local.update { it.copy(isRefreshing = false) }
            }
        }
    }

    /**
     * Captures the device location and queues the `enter` punch. Re-checks the SERVER's
     * `can_check_in` first: the screen may have been sitting open while the task was finished
     * elsewhere. The punch carries whatever the phone can honestly tell — a missing permission
     * or no fix is RECORDED (`permission_missing` / `unavailable`), never refused here; V1 leaves
     * the judgement to the verifier.
     */
    private fun checkIn() {
        if (taskId.isBlank() || local.value.locating) return
        viewModelScope.launch {
            val detail = repository.observeTask(taskId).first()
            if (detail == null || !detail.canCheckIn || detail.inPen) {
                refresh()
                return@launch
            }
            local.update { it.copy(locating = true, message = null) }
            try {
                val facts = try {
                    factsProvider.capture()
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    crashReporter.recordException(error, "pen routine presence location capture failed")
                    null
                }
                val location = facts?.location?.let {
                    PenRoutineLocationDto(
                        latitude = it.latitude,
                        longitude = it.longitude,
                        accuracyM = it.gpsAccuracyM,
                        status = it.status,
                        address = it.address,
                    )
                } ?: PenRoutineLocationDto(status = LOCATION_UNAVAILABLE)
                val integrity = PenRoutineIntegrityDto(
                    mockLocation = facts?.verdict?.mockFix,
                    appVersion = BuildConfig.VERSION_NAME,
                    deviceModel = deviceModel(),
                    offline = facts?.offline,
                )
                when (
                    val queued = syncRepository.enqueuePenRoutinePresence(
                        taskId = taskId,
                        rowVersion = detail.rowVersion,
                        eventType = PRESENCE_ENTER,
                        capturedAt = Instant.now().toString(),
                        location = location,
                        integrity = integrity,
                    )
                ) {
                    is AppResult.Ok -> {
                        local.update { it.copy(presenceOutboxItemId = queued.value) }
                        analytics.track(
                            AnalyticsEventsPenRoutines.CHECK_IN,
                            props(detail, outcome = "queued", extra = mapOf(AnalyticsEvents.Params.OUTBOX_ITEM_ID to queued.value, "location_status" to location.status)),
                        )
                    }
                    is AppResult.Err -> {
                        queued.cause?.let { crashReporter.recordException(it, "pen routine presence enqueue failed") }
                        analytics.track(AnalyticsEventsPenRoutines.FAILURE, props(detail, outcome = "failure", reason = queued.message, stage = "check_in"))
                        local.update { it.copy(message = appContext.getString(R.string.pen_routines_msg_not_sent)) }
                    }
                }
            } finally {
                local.update { it.copy(locating = false) }
            }
        }
    }

    /**
     * Opens the in-app camera for one slot and writes the capture down. Photo slots use the
     * live photo capture (the toxin strip / water-proof path); video slots the shared recorder.
     * Re-checks the SERVER's `can_submit` before opening the camera, so a task finished elsewhere
     * never costs the park head a capture he cannot use.
     */
    private fun captureSlot(fieldKey: String) {
        if (taskId.isBlank() || local.value.capturing) return
        val slotKind = slotKindOf(fieldKey) ?: return
        viewModelScope.launch {
            val detail = repository.observeTask(taskId).first()
            if (detail == null || !detail.canSubmit || detail.workState == PEN_ROUTINE_WORK_STATE_COMPLETED) {
                refresh()
                return@launch
            }
            local.update { it.copy(capturing = true, message = null) }
            try {
                val captured: Captured? = try {
                    when (slotKind) {
                        PenRoutineSlotKind.PHOTO -> photoCaptureSource.capturePhoto(
                            PhotoCaptureContext(title = detail.title, instruction = detail.instruction),
                        )?.let { Captured(it.localUri, it.mimeType, it.capturedAtMs, it.capturedAtMs, it.captureSource) }
                        PenRoutineSlotKind.VIDEO -> proofCaptureSource.captureVideo(
                            ProofCaptureContext(
                                // Backend-owned title, pen label and instruction lead the recorder
                                // chrome, so the words on the camera are the words on the card.
                                title = detail.title,
                                primaryTag = detail.operationalLocationDisplay,
                                workLabel = detail.instruction,
                                headerTitle = appContext.getString(R.string.proof_video_pen_routine_header),
                            ),
                        )?.let { Captured(it.localUri, it.mimeType, it.startedAtMs, it.endedAtMs, it.captureSource) }
                    }
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    crashReporter.recordException(error, "pen routine capture failed")
                    analytics.track(AnalyticsEventsPenRoutines.FAILURE, props(detail, outcome = "failure", reason = error.message ?: "unknown", stage = fieldKey))
                    null
                }
                if (captured == null) {
                    analytics.track(
                        AnalyticsEventsPenRoutines.CAPTURE_RESULT,
                        props(detail, outcome = CAPTURE_RESULT_CANCELLED, extra = mapOf(AnalyticsEvents.Params.FIELD to fieldKey)),
                    )
                    return@launch
                }
                analytics.track(
                    AnalyticsEventsPenRoutines.CAPTURE_RESULT,
                    props(detail, outcome = CAPTURE_RESULT_RECORDED, source = captured.captureSource, extra = mapOf(AnalyticsEvents.Params.FIELD to fieldKey)),
                )
                // Everything after a REAL capture is durable bookkeeping. NonCancellable so backing
                // out of the screen can never orphan a capture the park head actually took.
                withContext(NonCancellable) {
                    writeCapture(detail, fieldKey, slotKind, captured)
                }
            } finally {
                local.update { it.copy(capturing = false) }
            }
        }
    }

    private data class Captured(val localUri: String, val mimeType: String, val startMs: Long, val endMs: Long, val captureSource: String)

    /**
     * Writes the durable proof row for one slot and waits for its PROOF_UPLOAD row id. The
     * settle wait mirrors PC Care's and the pen visit's: the upload-row id can land in Room a beat
     * AFTER the capture result is composed, so a blank id here is usually a read race, not a lost
     * capture. Only a row still without an upload id after the wait is a genuine failure.
     */
    private suspend fun writeCapture(detail: PenRoutineTaskDto, fieldKey: String, kind: PenRoutineSlotKind, captured: Captured) {
        val slot = slotFor(fieldKey)
        val result = proofCaptureRepository.captureReplacingLatest(
            slot = slot,
            subject = ProofSubject.OTHER,
            // The proof platform requires a UUID subject/scope; the routine TASK is that subject.
            // There is no animal here — a routine is a pen check, not a scan.
            subjectId = taskId,
            localUri = captured.localUri,
            mimeType = captured.mimeType,
            caption = detail.title,
            scopeType = SCOPE_TYPE_TASK,
            scopeId = taskId,
            capturedStartMs = captured.startMs,
            capturedEndMs = captured.endMs,
            capturedByPrincipalId = null,
            proofPolicy = penRoutineProofPolicy(kind, captured.captureSource),
            awaitUploadEnqueue = true,
            // ONE FIFO lane per task: every upload drains BEFORE the submit that resolves it.
            uploadGroupKey = penRoutineTaskGroupKey(taskId),
        )
        when (result) {
            is AppResult.Err -> {
                result.cause?.let { crashReporter.recordException(it, "pen routine capture write failed") }
                analytics.track(AnalyticsEventsPenRoutines.FAILURE, props(detail, outcome = "failure", reason = result.message, stage = fieldKey))
                local.update { it.copy(message = appContext.getString(R.string.pen_routines_msg_capture_not_saved)) }
                return
            }
            is AppResult.Ok -> Unit
        }
        var proofOutboxId = result.value.outboxItemId
        var waited = 0L
        while (proofOutboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
            delay(PROOF_ROW_SETTLE_STEP_MS)
            waited += PROOF_ROW_SETTLE_STEP_MS
            proofOutboxId = proofCaptureRepository.observeProofs(taskId).first().firstOrNull { it.id == result.value.id }?.outboxItemId
        }
        if (proofOutboxId.isNullOrBlank()) {
            crashReporter.recordException(
                IllegalStateException("pen routine capture ${result.value.id} has no upload row after ${waited}ms"),
                "pen routine capture never enqueued its upload",
            )
            analytics.track(AnalyticsEventsPenRoutines.FAILURE, props(detail, outcome = "failure", reason = "upload_row_missing", stage = fieldKey))
            local.update { it.copy(message = appContext.getString(R.string.pen_routines_msg_capture_not_saved)) }
        }
    }

    /**
     * Queues the submit: the draft answers in the server's wire shape plus every captured slot
     * by reference to its upload row, fenced on the row version the screen rendered. The gate is
     * re-run here from Room truth rather than trusted from the last rendered state.
     */
    private fun submit() {
        if (taskId.isBlank() || local.value.capturing) return
        viewModelScope.launch {
            val detail = repository.observeTask(taskId).first()
            if (detail == null) return@launch
            val answers = draft.value.orEmpty()
            val proofs = proofCaptureRepository.observeProofs(taskId).first()
            val q = queue.first()
            val ui = toUiState(detail, local.value, answers, proofs, q)
            if (!ui.submitEnabled) {
                refresh()
                return@launch
            }
            val slotRows = slotRows(detail, proofs).filterKeys { key -> key in referencedSlotKeys(ui) }
            val references = slotRows.mapNotNull { (fieldKey, row) ->
                val outboxId = row.outboxItemId?.takeIf { it.isNotBlank() } ?: return@mapNotNull null
                PenRoutineSubmitProof(
                    proofOutboxItemId = outboxId,
                    kind = if (slotKindOf(fieldKey) == PenRoutineSlotKind.PHOTO) PEN_ROUTINE_PROOF_KIND_PHOTO else PEN_ROUTINE_PROOF_KIND_VIDEO,
                    questionId = parsePenRoutineQuestionProofFieldKey(fieldKey)?.questionId.orEmpty(),
                )
            }
            if (references.size != slotRows.size) {
                // A capture without its upload row cannot be referenced; the settle wait already
                // reported it, and re-taking that slot is the honest repair.
                local.update { it.copy(message = appContext.getString(R.string.pen_routines_msg_capture_not_saved)) }
                return@launch
            }
            local.update { it.copy(message = null) }
            // The leave stamp rides the submit: capture the same honest location block the
            // check-in carried, but only when the routine tracks presence at all.
            var location: PenRoutineLocationDto? = null
            var integrity: PenRoutineIntegrityDto? = null
            if (detail.presenceRequired) {
                val facts = try {
                    factsProvider.capture()
                } catch (error: Exception) {
                    if (error is CancellationException) throw error
                    crashReporter.recordException(error, "pen routine submit location capture failed")
                    null
                }
                location = facts?.location?.let {
                    PenRoutineLocationDto(latitude = it.latitude, longitude = it.longitude, accuracyM = it.gpsAccuracyM, status = it.status, address = it.address)
                } ?: PenRoutineLocationDto(status = LOCATION_UNAVAILABLE)
                integrity = PenRoutineIntegrityDto(mockLocation = facts?.verdict?.mockFix, appVersion = BuildConfig.VERSION_NAME, deviceModel = deviceModel(), offline = facts?.offline)
            }
            withContext(NonCancellable) {
                when (
                    val queued = syncRepository.enqueuePenRoutineSubmit(
                        taskId = taskId,
                        rowVersion = detail.rowVersion,
                        answers = answersJson(detail.form.questions, answers),
                        proofs = references,
                        capturedAt = Instant.now().toString(),
                        location = location,
                        integrity = integrity,
                    )
                ) {
                    is AppResult.Ok -> {
                        local.update { it.copy(submitOutboxItemId = queued.value) }
                        analytics.track(
                            AnalyticsEventsPenRoutines.SUBMIT,
                            props(detail, outcome = "queued", extra = mapOf(AnalyticsEvents.Params.OUTBOX_ITEM_ID to queued.value, AnalyticsEvents.Params.COUNT to references.size.toString())),
                        )
                    }
                    is AppResult.Err -> {
                        queued.cause?.let { crashReporter.recordException(it, "pen routine submit enqueue failed") }
                        analytics.track(AnalyticsEventsPenRoutines.SUBMIT_FAILED, props(detail, outcome = "failure", reason = queued.message))
                        local.update { it.copy(message = appContext.getString(R.string.pen_routines_msg_not_sent)) }
                    }
                }
            }
        }
    }

    /** A capture preview's play/pause/fullscreen/share/failure, traced like every other proof surface. */
    private fun trackPreviewAction(fieldKey: String, raw: String) {
        val trace = ProofPreviewActionTrace.from(raw)
        viewModelScope.launch {
            val detail = repository.observeTask(taskId).first()
            analytics.track(
                AnalyticsEventsPenRoutines.CAPTURE_RESULT,
                props(detail, outcome = trace.outcome, reason = trace.reason, source = "preview", extra = mapOf(AnalyticsEvents.Params.FIELD to fieldKey, AnalyticsEvents.Params.ACTION to trace.action)),
            )
        }
    }

    private fun props(
        detail: PenRoutineTaskDto?,
        outcome: String? = null,
        reason: String? = null,
        source: String? = null,
        stage: String? = null,
        extra: Map<String, String> = emptyMap(),
    ): Map<String, String> = buildMap {
        put(PARAM_TASK_ID, detail?.taskId ?: taskId)
        put(AnalyticsEvents.Params.GROUP_KEY, penRoutineTaskGroupKey(taskId))
        detail?.let {
            put("row_version", it.rowVersion.toString())
            put("work_state", it.workState)
        }
        source?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.SOURCE, it.take(MAX_REASON_CHARS)) }
        stage?.takeIf { it.isNotBlank() }?.let { put("stage", it.take(MAX_REASON_CHARS)) }
        outcome?.let {
            put(AnalyticsEvents.Params.OUTCOME, it.take(MAX_REASON_CHARS))
            put(AnalyticsEvents.Params.RESULT, it.take(MAX_REASON_CHARS))
        }
        reason?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        putAll(extra)
    }

    private fun deviceModel(): String = listOf(Build.MANUFACTURER, Build.MODEL)
        .map { it.orEmpty().trim() }
        .filter { it.isNotBlank() }
        .distinct()
        .joinToString(" ")

    /**
     * The slot keys the rendered form actually shows a capture in: the task-wide slots and every
     * question slot. A question slot that switched medium on a re-take leaves its older row
     * behind under the other key; that row is not on screen and must not ride the submit.
     */
    private fun referencedSlotKeys(ui: PenRoutineDetailUiState): Set<String> =
        buildSet {
            ui.photoSlots.forEach { add(it.fieldKey) }
            ui.videoSlots.forEach { add(it.fieldKey) }
            ui.questions.forEach { q -> q.proofSlots.forEach { add(it.fieldKey) } }
        }

    private fun slotFor(fieldKey: String) = EvidenceSlot(
        identity = ProofIdentity(flow = ProofFlow.PEN_ROUTINE, taskId = taskId, subjectKey = fieldKey),
        fieldKey = fieldKey,
    )

    private suspend fun latestDurableItem(opType: String): SyncQueueItem? =
        when (val result = syncRepository.findLatestOutboxItem(penRoutineTaskGroupKey(taskId), opType)) {
            is AppResult.Ok -> result.value
            is AppResult.Err -> {
                result.cause?.let { crashReporter.recordException(it, "pen routine durable lookup failed") }
                null
            }
        }

    // ---- state composition ------------------------------------------------------------------

    private fun toUiState(
        detail: PenRoutineTaskDto?,
        own: Local,
        answers: Map<String, List<String>>,
        proofs: List<ProofCaptureRow>,
        q: Queue,
    ): PenRoutineDetailUiState {
        if (detail == null) {
            return PenRoutineDetailUiState(loading = true, isRefreshing = own.isRefreshing, message = own.message)
        }
        // A row THIS screen just queued whose outbox flow has not emitted yet is on its way too:
        // the beat between enqueue and Room's first emission must not re-arm the button.
        val submitJustQueued = own.submitOutboxItemId.isNotBlank() && q.submit == null
        val presenceJustQueued = own.presenceOutboxItemId.isNotBlank() && q.presence == null
        val phase = penRoutinePhase(
            detail,
            q.submit,
            sendingAlive = submitJustQueued || penRoutineGrainKey(taskId, detail.rowVersion) in q.sending,
        )
        val rowsByKey = slotRows(detail, proofs)
        val questions = detail.form.questions.map { question ->
            val ui = question.toUi(answers[question.id].orEmpty())
            val proofKind = question.proof?.let { PenRoutineQuestionProofKind.from(it.kind) }
            if (proofKind == null) return@map ui
            val owed = ui.required || ui.isAnswered()
            val slots = questionProofSlots(question.id, proofKind, question.proof?.count == PEN_ROUTINE_PROOF_COUNT_MULTIPLE, owed, rowsByKey)
            val taken = slots.any { it.previewPath.isNotBlank() && it.failureReason.isBlank() }
            ui.copy(
                proofKind = proofKind,
                proofSlots = slots,
                proofMissing = owed && !taken,
            )
        }
        val photoSlots = (1..detail.form.photo.max).map { index ->
            val key = penRoutinePhotoFieldKey(index)
            slotUi(key, PenRoutineSlotKind.PHOTO, index, required = index <= detail.form.photo.min, row = rowsByKey[key])
        }
        val videoSlots = (1..detail.form.video.max).map { index ->
            val key = penRoutineVideoFieldKey(index)
            slotUi(key, PenRoutineSlotKind.VIDEO, index, required = index <= detail.form.video.min, row = rowsByKey[key])
        }
        val presenceQueued = presenceJustQueued || q.presence?.isActive == true
        val presenceRefused = q.presence?.isTerminalFailure == true
        val submitRefused = q.submit?.isTerminalFailure == true
        val formLive = phase == PenRoutinePhase.OPEN || phase == PenRoutinePhase.REWORK
        val gate = penRoutineSubmitGate(
            canSubmit = detail.canSubmit && formLive,
            questions = questions,
            photosTaken = photoSlots.count { it.previewPath.isNotBlank() && it.failureReason.isBlank() },
            photoMin = detail.form.photo.min,
            videosTaken = videoSlots.count { it.previewPath.isNotBlank() && it.failureReason.isBlank() },
            videoMin = detail.form.video.min,
            presenceRequired = detail.presenceRequired,
            inPen = detail.inPen,
        )
        return PenRoutineDetailUiState(
            loading = false,
            taskId = taskId,
            title = detail.title,
            penLabel = detail.operationalLocationDisplay,
            parkName = detail.parkName,
            parkTask = detail.scopeKind == PEN_ROUTINE_SCOPE_PARK,
            reasonLine = detail.reasonLine,
            evidenceLine = detail.evidenceLine,
            stateChip = detail.stateChip,
            tone = PenRoutineTone.from(detail.stateTone),
            instruction = detail.instruction,
            phase = phase,
            presenceRequired = detail.presenceRequired,
            presenceLine = detail.presenceLine,
            inPen = detail.inPen,
            canCheckIn = detail.canCheckIn && !detail.inPen && !own.locating && !presenceQueued,
            checkingIn = own.locating || (presenceQueued && !detail.inPen),
            canSubmit = detail.canSubmit,
            questions = questions,
            photoSlots = photoSlots,
            photoMin = detail.form.photo.min,
            videoSlots = videoSlots,
            videoMin = detail.form.video.min,
            submitEnabled = gate && !own.capturing && !own.locating && !presenceQueued,
            answerRows = detail.answerRows.map { PenRoutineAnswerRowUi(it.questionId, it.title, it.value) },
            proofCount = detail.proofs.size,
            doneLine = detail.doneLine,
            reworkReason = detail.reworkReason,
            failureReason = when {
                submitRefused -> q.submit?.lastError.orEmpty()
                presenceRefused && !detail.inPen -> q.presence?.lastError.orEmpty()
                else -> ""
            },
            isRefreshing = own.isRefreshing,
            capturing = own.capturing,
            message = own.message,
        )
    }

    /**
     * The latest ACTIVE proof row per slot field key. After a REWORK the old captures are
     * history (the server re-collects everything), so only rows captured after the last submit
     * count towards the re-opened form.
     */
    private fun slotRows(detail: PenRoutineTaskDto, proofs: List<ProofCaptureRow>): Map<String, ProofCaptureRow> {
        val since = if (detail.status == PEN_ROUTINE_STATUS_REWORK) detail.submittedAt?.let { parseInstantMs(it) } else null
        return proofs
            .filter { it.syncStatus != CaptureSyncStatus.FAILED && slotKindOf(it.fieldKey) != null }
            .filter { since == null || it.capturedAtMs > since }
            .groupBy { it.fieldKey }
            .mapValues { (_, rows) -> rows.maxBy { it.capturedAtMs } }
    }

    private fun slotUi(fieldKey: String, kind: PenRoutineSlotKind, index: Int, required: Boolean, row: ProofCaptureRow?): PenRoutineSlotUi {
        if (row == null) return PenRoutineSlotUi(fieldKey = fieldKey, kind = kind, index = index, required = required)
        val failed = row.processingStatus == ProofProcessingStatus.RECORD_AGAIN || row.syncStatus == CaptureSyncStatus.FAILED
        val delivered = !row.serverProofId.isNullOrBlank() || row.syncStatus == CaptureSyncStatus.SYNCED
        return PenRoutineSlotUi(
            fieldKey = fieldKey,
            kind = kind,
            index = index,
            required = required,
            previewPath = row.processedUri ?: row.localUri,
            working = !failed && !delivered,
            progressLabel = if (!failed && !delivered) row.processingStatus.operatorLabel else "",
            failureReason = if (failed) row.lastError.orEmpty() else "",
        )
    }

    /**
     * One question's own capture slots. A photo or video question opens slots of that kind; a
     * photo-or-video question's slots hold whichever kind was captured there and, while empty,
     * offer both cameras through [PenRoutineSlotUi.videoFieldKey]. `multiple` allows up to the
     * cap, but the screen shows only the captures TAKEN plus ONE empty slot -- five stacked
     * "Record video" buttons read as five owed videos, and the rule is "up to", not "exactly".
     * The first slot carries the required mark only while the question OWES a capture
     * (required, or optional and answered) -- an optional question left blank owes nothing
     * and must not show a star it will never enforce.
     */
    private fun questionProofSlots(
        questionId: String,
        kind: PenRoutineQuestionProofKind,
        multiple: Boolean,
        owed: Boolean,
        rowsByKey: Map<String, ProofCaptureRow>,
    ): List<PenRoutineSlotUi> {
        val cap = if (multiple) PEN_ROUTINE_QUESTION_PROOF_MAX else 1
        val all = (1..cap).map { index ->
            val photoKey = penRoutineQuestionProofFieldKey(questionId, PEN_ROUTINE_PROOF_KIND_PHOTO, index)
            val videoKey = penRoutineQuestionProofFieldKey(questionId, PEN_ROUTINE_PROOF_KIND_VIDEO, index)
            when (kind) {
                PenRoutineQuestionProofKind.PHOTO -> slotUi(photoKey, PenRoutineSlotKind.PHOTO, index, required = owed && index == 1, row = rowsByKey[photoKey])
                PenRoutineQuestionProofKind.VIDEO -> slotUi(videoKey, PenRoutineSlotKind.VIDEO, index, required = owed && index == 1, row = rowsByKey[videoKey])
                PenRoutineQuestionProofKind.EITHER -> {
                    val photoRow = rowsByKey[photoKey]
                    val videoRow = rowsByKey[videoKey]
                    when {
                        // Both taken on one slot (a re-take that switched medium): the newer wins.
                        photoRow != null && (videoRow == null || photoRow.capturedAtMs >= videoRow.capturedAtMs) ->
                            slotUi(photoKey, PenRoutineSlotKind.PHOTO, index, required = owed && index == 1, row = photoRow)
                        videoRow != null -> slotUi(videoKey, PenRoutineSlotKind.VIDEO, index, required = owed && index == 1, row = videoRow)
                        else -> PenRoutineSlotUi(fieldKey = photoKey, kind = PenRoutineSlotKind.PHOTO, index = index, required = owed && index == 1, videoFieldKey = videoKey)
                    }
                }
            }
        }
        // Taken slots, then the first empty one; nothing past it until it is filled.
        val firstEmpty = all.indexOfFirst { it.previewPath.isBlank() }
        return if (firstEmpty < 0) all else all.take(firstEmpty + 1)
    }

    private companion object {
        const val ARG_TASK_ID = "task_id"
        const val KEY_DRAFT = "pen_routine_draft_answers"
        const val OP_TYPE_PRESENCE = "PEN_ROUTINE_PRESENCE"
        const val OP_TYPE_SUBMIT = "PEN_ROUTINE_SUBMIT"
        const val PRESENCE_ENTER = "enter"
        const val LOCATION_UNAVAILABLE = "unavailable"
        const val SCOPE_TYPE_TASK = "task"
        const val CAPTURE_RESULT_RECORDED = "recorded"
        const val CAPTURE_RESULT_CANCELLED = "cancelled"
        const val PROOF_ROW_SETTLE_MAX_MS = 3_000L
        const val PROOF_ROW_SETTLE_STEP_MS = 100L
        const val MAX_REASON_CHARS = 120
        const val PARAM_TASK_ID = "task_id"
    }
}

/** The wire `work_state` / `status` values this screen reads (backend/internal/penroutines/domain). */
internal const val PEN_ROUTINE_WORK_STATE_COMPLETED = "completed"
internal const val PEN_ROUTINE_STATUS_OPEN = "open"
internal const val PEN_ROUTINE_STATUS_PENDING_VERIFICATION = "pending_verification"
internal const val PEN_ROUTINE_STATUS_REWORK = "rework"
internal const val PEN_ROUTINE_STATUS_COMPLETED = "completed"

/** Which slot kind a capture field key names, or null for a row that is not a routine slot. */
internal fun slotKindOf(fieldKey: String): PenRoutineSlotKind? = when {
    fieldKey.startsWith("routine-photo-") -> PenRoutineSlotKind.PHOTO
    fieldKey.startsWith("routine-video-") -> PenRoutineSlotKind.VIDEO
    else -> when (parsePenRoutineQuestionProofFieldKey(fieldKey)?.kind) {
        PEN_ROUTINE_PROOF_KIND_PHOTO -> PenRoutineSlotKind.PHOTO
        PEN_ROUTINE_PROOF_KIND_VIDEO -> PenRoutineSlotKind.VIDEO
        else -> null
    }
}

/** The wire `proof.count` value that opens several slots on one question. */
internal const val PEN_ROUTINE_PROOF_COUNT_MULTIPLE = "multiple"

/** The most captures one question may carry when its count is `multiple` (server MaxProofPerKind). */
internal const val PEN_ROUTINE_QUESTION_PROOF_MAX = 5

/**
 * Derives the task's phase from Room truth ONLY — the server's task, the queued submit's outbox
 * row (when this screen queued it) and the outbox's active grain set (which survives a process
 * death). Precedence, and why:
 *  1. The SERVER says done -> DONE. Nothing local outranks the record.
 *  2. A submit still on the wire -> SENDING (queued, retrying, or landed and not yet reconciled).
 *  3. The server's gate: with the verifier -> IN_REVIEW; sent back -> REWORK.
 *  4. Not the caller's to record -> LOCKED; otherwise OPEN.
 */
internal fun penRoutinePhase(detail: PenRoutineTaskDto, submit: SyncQueueItem?, sendingAlive: Boolean): PenRoutinePhase {
    if (detail.status == PEN_ROUTINE_STATUS_COMPLETED || detail.workState == PEN_ROUTINE_WORK_STATE_COMPLETED) return PenRoutinePhase.DONE
    if (submit != null && submit.isActive) return PenRoutinePhase.SENDING
    if (sendingAlive) return PenRoutinePhase.SENDING
    if (detail.status == PEN_ROUTINE_STATUS_PENDING_VERIFICATION) return PenRoutinePhase.IN_REVIEW
    // The submit landed but the server's row has not been re-read yet: still on its way.
    if (submit != null && submit.status == SyncItemStatus.SUCCEEDED && submit.idempotencyKey.endsWith(":${detail.rowVersion}")) return PenRoutinePhase.SENDING
    if (!detail.canSubmit) return PenRoutinePhase.LOCKED
    if (detail.status == PEN_ROUTINE_STATUS_REWORK) return PenRoutinePhase.REWORK
    return PenRoutinePhase.OPEN
}

/**
 * The local pre-check of the server's submit rules (`domain.CheckSubmit`): every required
 * question answered, every number parsing and inside its range, photo/video counts at their
 * minimum, presence satisfied when required. The server re-runs all of it under its row lock;
 * this only keeps a submit that cannot succeed off the wire.
 */
internal fun penRoutineSubmitGate(
    canSubmit: Boolean,
    questions: List<PenRoutineQuestionUi>,
    photosTaken: Int,
    photoMin: Int,
    videosTaken: Int,
    videoMin: Int,
    presenceRequired: Boolean,
    inPen: Boolean,
): Boolean {
    if (!canSubmit) return false
    if (questions.any { it.invalid }) return false
    if (questions.any { it.required && !it.isAnswered() }) return false
    if (questions.any { it.proofMissing }) return false
    if (photosTaken < photoMin || videosTaken < videoMin) return false
    if (presenceRequired && !inPen) return false
    return true
}

internal fun PenRoutineQuestionUi.isAnswered(): Boolean = when (kind) {
    PenRoutineQuestionKind.YES_NO, PenRoutineQuestionKind.CHOICE, PenRoutineQuestionKind.MULTI_CHOICE -> selected.isNotEmpty()
    PenRoutineQuestionKind.NUMBER, PenRoutineQuestionKind.TEXT -> text.isNotBlank()
}

/** Maps one authored question plus the draft answer into its widget model. */
internal fun PenRoutineQuestionDto.toUi(answer: List<String>): PenRoutineQuestionUi {
    val kind = PenRoutineQuestionKind.from(this.kind)
    val text = if (kind == PenRoutineQuestionKind.NUMBER || kind == PenRoutineQuestionKind.TEXT) answer.firstOrNull().orEmpty() else ""
    val invalid = kind == PenRoutineQuestionKind.NUMBER && text.isNotBlank() && !numberFits(text, min, max)
    return PenRoutineQuestionUi(
        id = id,
        kind = kind,
        title = title,
        hint = hint,
        required = required,
        options = options.map { PenRoutineOptionUi(value = it.value, label = it.label) },
        min = min,
        max = max,
        unit = unit,
        selected = if (text.isEmpty()) answer.filter { it.isNotBlank() } else emptyList(),
        text = text,
        invalid = invalid,
    )
}

private fun numberFits(text: String, min: Double?, max: Double?): Boolean {
    val value = text.trim().toDoubleOrNull() ?: return false
    if (min != null && value < min) return false
    if (max != null && value > max) return false
    return true
}

/**
 * The draft answers in the server's wire shape: yes_no / choice -> the option value; multi_choice
 * -> the values; number -> a number (an integral value travels without a decimal point); text ->
 * the text. Unanswered questions are omitted, never sent blank.
 */
internal fun answersJson(questions: List<PenRoutineQuestionDto>, answers: Map<String, List<String>>): JsonObject {
    val out = LinkedHashMap<String, kotlinx.serialization.json.JsonElement>() // mobile-guard:ignore: per-call local, bounded by the authored question list, returned as one JsonObject
    questions.forEach { question ->
        val given = answers[question.id].orEmpty().filter { it.isNotBlank() }
        if (given.isEmpty()) return@forEach
        when (PenRoutineQuestionKind.from(question.kind)) {
            PenRoutineQuestionKind.YES_NO, PenRoutineQuestionKind.CHOICE -> out[question.id] = JsonPrimitive(given.first())
            PenRoutineQuestionKind.MULTI_CHOICE -> out[question.id] = JsonArray(given.map { JsonPrimitive(it) })
            PenRoutineQuestionKind.NUMBER -> {
                val value = given.first().trim().toDoubleOrNull() ?: return@forEach
                out[question.id] = if (value == Math.floor(value) && !value.isInfinite()) JsonPrimitive(value.toLong()) else JsonPrimitive(value)
            }
            PenRoutineQuestionKind.TEXT -> out[question.id] = JsonPrimitive(given.first())
        }
    }
    return JsonObject(out)
}

private fun parseInstantMs(raw: String): Long? =
    // exception:exempt an unparseable server timestamp only disables the rework filter; the
    // captures still render and the server still validates the submit.
    runCatching { Instant.parse(raw).toEpochMilli() }.getOrNull()

/**
 * A routine capture's proof policy: ONE capture per slot, replaced on a re-take.
 * `maximumCountPerField = 1` is the real local bound; the backend owns the min/max per kind.
 */
internal fun penRoutineProofPolicy(kind: PenRoutineSlotKind, captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        types = listOf(if (kind == PenRoutineSlotKind.PHOTO) "photo" else "video"),
        proofMode = if (kind == PenRoutineSlotKind.PHOTO) "pen_routine_photo" else "pen_routine_video",
        featureSurface = "pen_routines",
        featureCategory = "pen_routine",
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
    )
