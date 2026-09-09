package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
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
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenVisits
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.PenVisitsRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofProcessingStatus
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.PEN_VISIT_VIDEO_FIELD_KEY
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.penVisitGrainKey
import sg.mesha.goatos.core.data.sync.penVisitTaskGroupKey
import sg.mesha.goatos.core.network.dto.PenVisitDto
import sg.mesha.goatos.feature.penvisits.PenVisitDetailEvent
import sg.mesha.goatos.feature.penvisits.PenVisitDetailUiState
import sg.mesha.goatos.feature.penvisits.PenVisitTone
import sg.mesha.goatos.feature.penvisits.PenVisitVideoState
import javax.inject.Inject

/**
 * ONE pen visit's detail state holder (maintainer decision 2026-09-07).
 *
 * Room is the single source of truth: the screen renders from
 * [PenVisitsRepository.observeVisit] merged with the DURABLE proof slot
 * ([ProofCaptureRepository.observeLatest]) and the queued submit's outbox row. The SERVER owns
 * every visible sentence and `can_submit`; this class maps the payload one-to-one and adds
 * nothing.
 *
 * SUBMIT IS IMPLICIT: a finished recording is written down and its submit queued in ONE act,
 * everything after the real recording inside `NonCancellable` so backing out of the screen can
 * never orphan a clip the park head actually shot (the PC Care defect of 2026-08-21). The video
 * rides the SAME pipeline PC Care and Toxin use — in-app camera -> durable Room proof row ->
 * PROOF_UPLOAD outbox row -> the submit that references it, all on one FIFO lane per visit
 * ([penVisitTaskGroupKey]) so the upload always drains before the submit.
 */
@HiltViewModel
class PenVisitDetailViewModel @Inject constructor(
    private val repository: PenVisitsRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val taskId: String = savedStateHandle.get<String>(ARG_TASK_ID).orEmpty()

    /** This phone's own transient state: what is mid-capture, and the submit row it queued. */
    private data class Local(
        val isRefreshing: Boolean = false,
        val capturing: Boolean = false,
        /** The PEN_VISIT_SUBMIT outbox row id this screen queued; blank until it did. */
        val submitOutboxItemId: String = "",
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())

    /**
     * The DURABLE slot the visit video occupies. Observing the slot — rather than remembering a
     * capture in memory — is what lets a clip survive leaving the drill or a process death
     * mid-upload, instead of reading as never taken and getting shot again.
     */
    private val videoSlot = EvidenceSlot(
        identity = ProofIdentity(flow = ProofFlow.PEN_VISIT, taskId = taskId, subjectKey = PEN_VISIT_VIDEO_FIELD_KEY),
        fieldKey = PEN_VISIT_VIDEO_FIELD_KEY,
    )

    @OptIn(ExperimentalCoroutinesApi::class)
    private val submitItem = local
        .map { it.submitOutboxItemId }
        .distinctUntilChanged()
        .flatMapLatest { id ->
            if (id.isBlank()) {
                flowOf(latestDurableSubmitItem())
            } else {
                syncRepository.observeItem(id)
            }
        }

    val state: StateFlow<PenVisitDetailUiState> =
        combine(
            repository.observeVisit(taskId),
            local,
            proofCaptureRepository.observeLatest(videoSlot),
            submitItem,
            syncRepository.observeSubmittedForReviewGrains(),
        ) { detail, own, proof, item, sending ->
            toUiState(detail, own, proof, item, sendingAlive = penVisitGrainKey(taskId) in sending)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PenVisitDetailUiState())

    init {
        refresh()
    }

    fun onEvent(event: PenVisitDetailEvent) {
        when (event) {
            PenVisitDetailEvent.Refresh -> refresh()
            PenVisitDetailEvent.Back -> Unit
            PenVisitDetailEvent.RecordVideo -> recordVideo()
            is PenVisitDetailEvent.ProofPreviewAction -> trackPreviewAction(event.action)
            PenVisitDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
        }
    }

    /** Non-blocking by contract: on failure the cached detail keeps rendering. */
    private fun refresh() {
        if (taskId.isBlank()) return
        viewModelScope.launch {
            local.update { it.copy(isRefreshing = true) }
            try {
                repository.refreshVisit(taskId)
                reconcileSubmitFromDurableProof(source = "refresh")
            } finally {
                local.update { it.copy(isRefreshing = false) }
            }
        }
    }

    /**
     * Repairs the one process-death gap between "the video is durable" and "the submit row is
     * durable". PC Care does the same repair for slot registrations: if the app dies after the
     * proof upload row exists but before the downstream write is enqueued, re-opening the screen
     * replays that downstream write from Room truth instead of asking the park head to record again.
     */
    private suspend fun reconcileSubmitFromDurableProof(source: String) {
        if (local.value.submitOutboxItemId.isNotBlank()) return
        val detail = repository.observeVisit(taskId).first() ?: return
        if (!detail.canSubmit || detail.workState == PEN_VISIT_WORK_STATE_COMPLETED) return
        if (penVisitGrainKey(taskId) in syncRepository.observeSubmittedForReviewGrains().first()) return
        if (latestDurableSubmitItem()?.isTerminalFailure == true) return
        val proof = proofCaptureRepository.observeLatest(videoSlot).first() ?: return
        val proofOutboxId = proof.outboxItemId?.takeIf { it.isNotBlank() } ?: return
        if (proof.syncStatus == CaptureSyncStatus.FAILED || proof.processingStatus == ProofProcessingStatus.RECORD_AGAIN) return

        when (
            val queued = syncRepository.enqueuePenVisitSubmit(
                taskId = taskId,
                rowVersion = detail.rowVersion,
                proofOutboxItemId = proofOutboxId,
            )
        ) {
            is AppResult.Ok -> {
                local.update { it.copy(submitOutboxItemId = queued.value, message = null) }
                analytics.track(
                    AnalyticsEventsPenVisits.SUBMIT_RECOVERED,
                    penVisitAnalyticsProps(
                        detail = detail,
                        proof = proof,
                        proofOutboxItemId = proofOutboxId,
                        submitOutboxItemId = queued.value,
                        source = source,
                        outcome = "queued",
                    ),
                )
            }
            is AppResult.Err -> {
                queued.cause?.let { crashReporter.recordException(it, "pen visit submit recovery failed") }
                analytics.track(
                    AnalyticsEventsPenVisits.FAILURE,
                    penVisitAnalyticsProps(
                        detail = detail,
                        proof = proof,
                        proofOutboxItemId = proofOutboxId,
                        source = source,
                        outcome = "failure",
                        reason = queued.message,
                    ),
                )
            }
        }
    }

    /**
     * Records the visit's ONE video and queues its submit.
     *
     * Re-checks the SERVER's `can_submit` immediately before opening the camera: the screen may
     * have been sitting open while the visit was completed elsewhere or cancelled. Recording first
     * and discovering that afterwards wastes the one thing that cannot be redone cheaply — the
     * park head's walk to the pen.
     */
    private fun recordVideo() {
        if (taskId.isBlank() || local.value.capturing) return
        viewModelScope.launch {
            val detail = repository.observeVisit(taskId).first()
            if (detail == null || !detail.canSubmit || detail.workState == PEN_VISIT_WORK_STATE_COMPLETED) {
                // The server has moved on. Pull its truth rather than guessing what changed.
                refresh()
                return@launch
            }
            local.update { it.copy(capturing = true, message = null) }
            try {
                analytics.track(AnalyticsEventsPenVisits.CAPTURE_STARTED, penVisitAnalyticsProps(detail = detail, source = "record_tap"))
                val captured = try {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            // Backend-owned title, pen label and instruction lead the recorder
                            // chrome, so the words on the camera are the words on the card.
                            title = detail.title,
                            primaryTag = detail.operationalLocationDisplay,
                            workLabel = detail.instruction,
                            headerTitle = appContext.getString(R.string.proof_video_pen_visit_header),
                        ),
                    )
                } catch (error: Exception) {
                    reportFailure(error, "pen visit video capture failed")
                    null
                }
                if (captured == null) {
                    analytics.track(
                        AnalyticsEventsPenVisits.CAPTURE_RESULT,
                        penVisitAnalyticsProps(detail = detail, source = "camera", outcome = CAPTURE_RESULT_CANCELLED),
                    )
                    return@launch
                }
                analytics.track(
                    AnalyticsEventsPenVisits.CAPTURE_RESULT,
                    penVisitAnalyticsProps(detail = detail, source = captured.captureSource, outcome = CAPTURE_RESULT_RECORDED),
                )

                // Everything after a REAL recording is durable bookkeeping. NonCancellable so
                // backing out of the screen (which cancels this scope) can never orphan a clip the
                // park head actually shot.
                withContext(NonCancellable) {
                    val proofOutboxId = captureProof(
                        detail = detail,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = detail.title,
                        captureSource = captured.captureSource,
                        startMs = captured.startedAtMs,
                        endMs = captured.endedAtMs,
                    ) ?: return@withContext
                    analytics.track(
                        AnalyticsEventsPenVisits.UPLOAD_ENQUEUED,
                        penVisitAnalyticsProps(detail = detail, proofOutboxItemId = proofOutboxId, source = captured.captureSource),
                    )
                    when (
                        val queued = syncRepository.enqueuePenVisitSubmit(
                            taskId = taskId,
                            rowVersion = detail.rowVersion,
                            proofOutboxItemId = proofOutboxId,
                        )
                    ) {
                        is AppResult.Ok -> {
                            local.update { it.copy(submitOutboxItemId = queued.value) }
                            analytics.track(
                                AnalyticsEventsPenVisits.SUBMITTED,
                                penVisitAnalyticsProps(
                                    detail = detail,
                                    proofOutboxItemId = proofOutboxId,
                                    submitOutboxItemId = queued.value,
                                    source = captured.captureSource,
                                    outcome = "queued",
                                ),
                            )
                        }
                        is AppResult.Err -> {
                            queued.cause?.let { crashReporter.recordException(it, "pen visit submit enqueue failed") }
                            analytics.track(
                                AnalyticsEventsPenVisits.FAILURE,
                                penVisitAnalyticsProps(
                                    detail = detail,
                                    proofOutboxItemId = proofOutboxId,
                                    source = captured.captureSource,
                                    outcome = "failure",
                                    reason = queued.message,
                                ),
                            )
                            local.update { it.copy(message = appContext.getString(R.string.pen_visits_msg_not_sent)) }
                        }
                    }
                }
            } finally {
                local.update { it.copy(capturing = false) }
            }
        }
    }

    /**
     * Writes the durable proof row and returns the id of the PROOF_UPLOAD outbox row the submit
     * must reference, or null when the clip never reached the outbox.
     *
     * The settle wait mirrors PC Care's and Toxin's: the upload-row id can land in Room a beat
     * AFTER the capture result is composed, so a blank id here is usually a read race, not a
     * lost clip. Only a row still without an upload id after the wait is a genuine failure.
     */
    private suspend fun captureProof(
        detail: PenVisitDto,
        localUri: String,
        mimeType: String,
        caption: String,
        captureSource: String,
        startMs: Long,
        endMs: Long,
    ): String? {
        val result = proofCaptureRepository.captureReplacingLatest(
            slot = videoSlot,
            subject = ProofSubject.OTHER,
            // The proof platform requires a UUID subject/scope; the visit TASK is that subject.
            // There is no animal here at all — a pen visit is a walk, not a scan.
            subjectId = taskId,
            localUri = localUri,
            mimeType = mimeType,
            caption = caption,
            scopeType = SCOPE_TYPE_TASK,
            scopeId = taskId,
            capturedStartMs = startMs,
            capturedEndMs = endMs,
            capturedByPrincipalId = null,
            proofPolicy = penVisitProofPolicy(captureSource),
            awaitUploadEnqueue = true,
            // ONE FIFO lane per visit: the upload drains BEFORE the submit that resolves it
            // (PenVisitPayloads.kt).
            uploadGroupKey = penVisitTaskGroupKey(taskId),
        )
        when (result) {
            is AppResult.Err -> {
                result.cause?.let { crashReporter.recordException(it, "pen visit capture write failed") }
                analytics.track(
                    AnalyticsEventsPenVisits.FAILURE,
                    penVisitAnalyticsProps(
                        detail = detail,
                        source = captureSource,
                        outcome = "failure",
                        reason = result.message,
                    ),
                )
                local.update { it.copy(message = appContext.getString(R.string.pen_visits_msg_capture_not_saved)) }
                return null
            }
            is AppResult.Ok -> analytics.track(
                AnalyticsEventsPenVisits.ROOM_WRITTEN,
                penVisitAnalyticsProps(
                    detail = detail,
                    proof = result.value,
                    proofOutboxItemId = result.value.outboxItemId.orEmpty(),
                    source = captureSource,
                ),
            )
        }
        var proofOutboxId = result.value.outboxItemId
        var waited = 0L
        while (proofOutboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
            delay(PROOF_ROW_SETTLE_STEP_MS)
            waited += PROOF_ROW_SETTLE_STEP_MS
            proofOutboxId = proofCaptureRepository.observeProofs(taskId).first()
                .firstOrNull { it.id == result.value.id }
                ?.outboxItemId
        }
        if (proofOutboxId.isNullOrBlank()) {
            crashReporter.recordException(
                IllegalStateException("pen visit clip ${result.value.id} has no upload row after ${waited}ms"),
                "pen visit video never enqueued its upload",
            )
            analytics.track(
                AnalyticsEventsPenVisits.FAILURE,
                penVisitAnalyticsProps(
                    detail = detail,
                    proof = result.value,
                    source = captureSource,
                    outcome = "failure",
                    reason = "upload_row_missing",
                ),
            )
            local.update { it.copy(message = appContext.getString(R.string.pen_visits_msg_capture_not_saved)) }
            return null
        }
        return proofOutboxId
    }

    /** The clip preview's play/pause/fullscreen/share/failure, traced like every other proof surface. */
    private fun trackPreviewAction(raw: String) {
        val trace = ProofPreviewActionTrace.from(raw)
        viewModelScope.launch {
            val detail = repository.observeVisit(taskId).first()
            val proof = proofCaptureRepository.observeLatest(videoSlot).first()
            analytics.track(
                AnalyticsEventsPenVisits.PROOF_PREVIEW_ACTION,
                penVisitAnalyticsProps(
                    detail = detail,
                    proof = proof,
                    source = "preview",
                    outcome = trace.outcome,
                    reason = trace.reason,
                ) + mapOf(AnalyticsEvents.Params.ACTION to trace.action),
            )
        }
    }

    private fun reportFailure(error: Throwable, context: String) {
        if (error is CancellationException) throw error
        crashReporter.recordException(error, context)
        analytics.track(
            AnalyticsEventsPenVisits.FAILURE,
            penVisitAnalyticsProps(outcome = "failure", reason = error.message ?: "unknown"),
        )
    }

    private fun penVisitAnalyticsProps(
        detail: PenVisitDto? = null,
        proof: ProofCaptureRow? = null,
        proofOutboxItemId: String = proof?.outboxItemId.orEmpty(),
        submitOutboxItemId: String = "",
        source: String? = null,
        outcome: String? = null,
        reason: String? = null,
        stage: String? = null,
    ): Map<String, String> = buildMap {
        put(PARAM_TASK_ID, detail?.taskId ?: taskId)
        put(AnalyticsEvents.Params.FIELD, PEN_VISIT_VIDEO_FIELD_KEY)
        put(AnalyticsEvents.Params.GROUP_KEY, penVisitTaskGroupKey(taskId))
        detail?.let {
            put(AnalyticsEvents.Params.IDEMPOTENCY_KEY, sg.mesha.goatos.core.data.sync.penVisitSubmitIdempotencyKey(it.taskId, it.rowVersion))
            put("row_version", it.rowVersion.toString())
            put("work_state", it.workState)
        }
        proof?.let {
            put(AnalyticsEvents.Params.PROOF_ID, it.id)
            put(AnalyticsEvents.Params.PROOF_STATE, it.processingStatus.wireValue)
            it.serverProofId?.takeIf { id -> id.isNotBlank() }?.let { id -> put("server_proof_id", id) }
        }
        if (proofOutboxItemId.isNotBlank()) put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, proofOutboxItemId)
        if (submitOutboxItemId.isNotBlank()) put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, submitOutboxItemId)
        source?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.SOURCE, it.take(MAX_REASON_CHARS)) }
        stage?.takeIf { it.isNotBlank() }?.let { put("stage", it.take(MAX_REASON_CHARS)) }
        outcome?.let {
            put(AnalyticsEvents.Params.OUTCOME, it.take(MAX_REASON_CHARS))
            put(AnalyticsEvents.Params.RESULT, it.take(MAX_REASON_CHARS))
        }
        reason?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
    }

    private fun toUiState(
        detail: PenVisitDto?,
        own: Local,
        proof: ProofCaptureRow?,
        item: SyncQueueItem?,
        sendingAlive: Boolean,
    ): PenVisitDetailUiState {
        if (detail == null) {
            return PenVisitDetailUiState(loading = true, isRefreshing = own.isRefreshing, message = own.message)
        }
        val video = penVisitVideoState(detail, proof, item, sendingAlive)
        return PenVisitDetailUiState(
            loading = false,
            taskId = taskId,
            title = detail.title,
            penLabel = detail.operationalLocationDisplay,
            parkName = detail.parkName,
            reasonLine = detail.reasonLine,
            stateChip = detail.stateChip,
            tone = PenVisitTone.from(detail.stateTone),
            instruction = detail.instruction,
            doneLine = detail.doneLine,
            canSubmit = detail.canSubmit,
            videoState = video.state,
            // The clip itself, shown back — the DURABLE row, not a remembered path.
            previewPath = proof?.let { it.processedUri ?: it.localUri }.orEmpty(),
            progressLabel = video.progressLabel,
            failureReason = video.failureReason,
            isRefreshing = own.isRefreshing,
            capturing = own.capturing,
            message = own.message,
        )
    }

    private companion object {
        const val ARG_TASK_ID = "task_id"
        const val OP_TYPE_PEN_VISIT_SUBMIT = "PEN_VISIT_SUBMIT"
        const val SCOPE_TYPE_TASK = "task"
        const val CAPTURE_RESULT_RECORDED = "recorded"
        const val CAPTURE_RESULT_CANCELLED = "cancelled"
        const val PROOF_ROW_SETTLE_MAX_MS = 3_000L
        const val PROOF_ROW_SETTLE_STEP_MS = 100L
        const val MAX_REASON_CHARS = 120
        const val PARAM_TASK_ID = "task_id"
    }

    private suspend fun latestDurableSubmitItem(): SyncQueueItem? =
        when (val result = syncRepository.findLatestOutboxItem(penVisitTaskGroupKey(taskId), OP_TYPE_PEN_VISIT_SUBMIT)) {
            is AppResult.Ok -> result.value
            is AppResult.Err -> {
                result.cause?.let { crashReporter.recordException(it, "pen visit submit durable lookup failed") }
                null
            }
        }
}

/** The slot's derived state plus the one line the WORKING/FAILED renders carry. */
internal data class PenVisitVideoUi(
    val state: PenVisitVideoState,
    val progressLabel: String = "",
    val failureReason: String = "",
)

/**
 * Derives the visit's video state from Room truth ONLY — the server's task, the durable proof
 * row, the queued submit's outbox row (when this screen queued it) and the outbox's active grain
 * set (which survives a process death). Precedence, and why:
 *
 *  1. The SERVER says done -> DONE. Nothing local outranks the record.
 *  2. The queued submit this screen knows -> its row decides: succeeded is DONE (the reconcile
 *     is a beat behind), active is WORKING, a terminal failure is FAILED with the queue's own
 *     sentence (the server's farm copy when it gave one).
 *  3. A submit still alive in the outbox from a previous process -> WORKING.
 *  4. Otherwise the proof row: none is EMPTY; a dead-lettered or terminally failed upload is
 *     FAILED; an uploaded clip with NO live submit and NO completion stays WORKING while the
 *     ViewModel re-enqueues the missing submit; anything else is WORKING with the pipeline's own
 *     progress line.
 */
internal fun penVisitVideoState(
    detail: PenVisitDto,
    proof: ProofCaptureRow?,
    item: SyncQueueItem?,
    sendingAlive: Boolean,
): PenVisitVideoUi {
    if (detail.workState == PEN_VISIT_WORK_STATE_COMPLETED) return PenVisitVideoUi(PenVisitVideoState.DONE)
    if (item != null) {
        return when {
            item.status == sg.mesha.goatos.core.data.sync.SyncItemStatus.SUCCEEDED -> PenVisitVideoUi(PenVisitVideoState.DONE)
            item.isActive -> PenVisitVideoUi(PenVisitVideoState.WORKING)
            else -> PenVisitVideoUi(PenVisitVideoState.FAILED, failureReason = item.lastError.orEmpty())
        }
    }
    if (sendingAlive) return PenVisitVideoUi(PenVisitVideoState.WORKING)
    if (proof == null) return PenVisitVideoUi(PenVisitVideoState.EMPTY)
    return when (proof.processingStatus) {
        ProofProcessingStatus.RECORD_AGAIN ->
            PenVisitVideoUi(PenVisitVideoState.FAILED, failureReason = proof.lastError.orEmpty())
        ProofProcessingStatus.UPLOADED ->
            PenVisitVideoUi(PenVisitVideoState.WORKING, progressLabel = proof.processingStatus.operatorLabel)
        else -> if (proof.syncStatus == CaptureSyncStatus.FAILED) {
            PenVisitVideoUi(PenVisitVideoState.FAILED, failureReason = proof.lastError.orEmpty())
        } else {
            PenVisitVideoUi(PenVisitVideoState.WORKING, progressLabel = proof.processingStatus.operatorLabel)
        }
    }
}

/**
 * A pen-visit capture's proof policy: ONE video per visit, replaced on a re-record.
 * `maximumCountPerField = 1` is the real local bound; the backend owns any feature-specific total.
 */
internal fun penVisitProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "pen_visit_video",
        featureSurface = "pen_visits",
        featureCategory = "pen_visit",
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
    )
