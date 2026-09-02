package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.PenReconciliationRepository
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.PenReconciliationExecuteEvent
import sg.mesha.goatos.feature.counts.PenReconciliationExecuteUiState
import java.util.UUID
import javax.inject.Inject

/**
 * The Reconcile EXECUTE screen (`/counts/reconcile/execute/{card_id}`) — an operator returning a
 * strayed animal to its registered pen (docs/decisions/pen-reconciliation.md).
 *
 * Two coupled offline-first writes, mirroring the shifting execute flow:
 *  - **Mandatory video** (`captureReplacingLatest` -> PROOF_UPLOAD): captured LIVE with the in-app
 *    camera against the REGISTERED shed, on the card's outbox group so it drains first.
 *  - **Mark done** (`enqueuePenReconciliationComplete`): flips the card to pending_verification
 *    for the tenant verifier. There is NO approver step, and the write never rewrites the herd
 *    register. Idempotency is a STABLE per-card key persisted in the durable draft, so a resend
 *    after process death collapses onto the original submission.
 *
 * The card detail is read from the Room-cached row (offline-first open): the operator tapped a row
 * already in Room, so no refetch is needed.
 */
@HiltViewModel
class PenReconciliationExecuteViewModel @Inject constructor(
    private val repo: PenReconciliationRepository,
    private val drafts: CaptureDraftRepository,
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val cardId: String = savedStateHandle[ARG_CARD_ID] ?: ""

    // The card's registered shed is the proof subject/scope. Resolved from the cached row on load.
    private var registeredShedId: String = ""
    private var parkLabel: String = ""
    private var belongsLabel: String = ""

    private val proofKey = DraftIdempotencyKey(savedStateHandle, KEY_PROOF_IDEMPOTENCY, "counts-pen-reconciliation-proof")

    /**
     * The card's DURABLE draft (Room, keyed by card id): the recorded video's outbox item id, the
     * completion's idempotency key, and the completion's own outbox item id. Room outlives the nav
     * backstack, so Back + re-entry keeps the recorded clip and resends under the SAME key.
     */
    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(PenReconciliationExecuteUiState(cardId = cardId))
    val state: StateFlow<PenReconciliationExecuteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var proofStatusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.COUNTS_PEN_RECONCILIATION_EXECUTE_OPENED)
        loadCard()
    }

    fun onEvent(event: PenReconciliationExecuteEvent) {
        when (event) {
            PenReconciliationExecuteEvent.RecordVideo -> captureVideo()
            // Re-record: the operator judged the clip unusable. captureReplacingLatest discards the
            // queued old take only AFTER the new one is durable, so a cancel keeps the old clip.
            PenReconciliationExecuteEvent.ReRecordVideo -> captureVideo(replacing = true)
            PenReconciliationExecuteEvent.MarkDone -> markDone()
            PenReconciliationExecuteEvent.Back -> Unit // navigation — handled by the nav host.
            PenReconciliationExecuteEvent.NavigationHandled -> _state.update { it.copy(returnToList = false) }
        }
    }

    private fun loadCard() {
        viewModelScope.launch {
            // Rehydrate the durable draft FIRST: everything below reads the evidence this card
            // already has.
            draft = drafts.find(CaptureFlow.PEN_RECONCILIATION, cardId)
            draft.submitOutboxItemId?.let(::observeOutboxItem)
            observeProofOutbox()
            val cached = repo.findCached(cardId)
            if (cached == null) {
                _state.update { it.copy(loading = false, notFound = true, canComplete = false) }
                return@launch
            }
            // A rework card came back because the verifier rejected the LAST video: any evidence
            // and submit key this phone still holds belong to the rejected round.
            if (cached.status == "rework" && (draft.hasProof(STEP_RETURN) || draft.submitIdempotencyKey != null)) {
                resetEvidenceForRework()
            }
            registeredShedId = cached.registeredShedId
            parkLabel = cached.parkName.orEmpty()
            belongsLabel = cached.registeredOperationalLocationDisplay.ifBlank { cached.registeredShedName }
            _state.update { current -> cached.toUiState(current) }
        }
    }

    /**
     * MANDATORY pen return video: records a LIVE in-app camera clip through the shared
     * [ProofCaptureSource], then enqueues a PROOF_UPLOAD under the CARD's outbox group so it
     * drains strictly BEFORE the completion on the same group. Until a video is captured,
     * "Mark done" stays disabled.
     */
    private fun captureVideo(replacing: Boolean = false) {
        if (_state.value.isCapturingVideo || registeredShedId.isBlank()) return
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = penReturnProofCaption(),
                        primaryTag = belongsLabel.ifBlank { registeredShedId },
                        workLabel = "Pen return",
                        prompt = ProofCapturePrompt.PEN_RECONCILIATION,
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "pen reconciliation video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            // Manohar ordering: the new proof is captured durably FIRST; only then does the
            // repository discard the previous take's row and outbox item (see
            // [ProofCaptureRepository.captureReplacingLatest]).
            val slot = EvidenceSlot(
                identity = ProofIdentity(flow = ProofFlow.PEN_RECONCILIATION, taskId = cardId, subjectKey = registeredShedId),
                fieldKey = FIELD_RETURN_VIDEO,
            )
            val result = proofCaptureRepository.captureReplacingLatest(
                slot = slot,
                subject = ProofSubject.SHED,
                subjectId = registeredShedId,
                localUri = captured.localUri,
                mimeType = captured.mimeType,
                caption = penReturnProofCaption(),
                scopeType = "shed",
                scopeId = registeredShedId,
                capturedStartMs = captured.startedAtMs,
                capturedEndMs = captured.endedAtMs,
                capturedByPrincipalId = null,
                proofPolicy = feedShedProofPolicy(captured.captureSource),
                awaitUploadEnqueue = true,
                uploadGroupKey = cardId,
            )
            when (result) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        _state.update { it.copy(isCapturingVideo = false, videoMessage = VIDEO_FAILED) }
                        return@launch
                    }
                    // Durable BEFORE the UI flips: if the process dies here, re-entry still finds
                    // the recorded clip instead of asking for it again.
                    if (replacing) {
                        drafts.clearProof(CaptureFlow.PEN_RECONCILIATION, cardId, STEP_RETURN)
                    }
                    drafts.putProof(
                        flowKey = CaptureFlow.PEN_RECONCILIATION,
                        entityId = cardId,
                        step = STEP_RETURN,
                        outboxItemId = proofOutboxId,
                    )
                    draft = drafts.find(CaptureFlow.PEN_RECONCILIATION, cardId)
                    observeProofOutbox()
                    analytics.track(
                        AnalyticsEvents.COUNTS_PEN_RECONCILIATION_VIDEO_CAPTURED,
                        mapOf(
                            AnalyticsEvents.Params.SOURCE to SCREEN_EXECUTE,
                            AnalyticsEvents.Params.KIND to KIND_COMPLETE,
                            AnalyticsEvents.Params.ACTION to ACTION_CAPTURED,
                            AnalyticsEvents.Params.FIELD to FIELD_RETURN_VIDEO,
                            AnalyticsEvents.Params.SHED_ID to registeredShedId,
                            AnalyticsEvents.Params.PROOF_ID to result.value.id,
                            PARAM_GROUP_KEY to cardId,
                            PARAM_OUTBOX_ITEM_ID to proofOutboxId,
                        ),
                    )
                    _state.update {
                        it.copy(
                            isCapturingVideo = false,
                            videoCaptured = true,
                            // The clip the operator just shot, playable in place before submitting
                            // (same review affordance as the feed proof screens).
                            videoPreviewPath = captured.localUri,
                            videoMessage = VIDEO_QUEUED,
                            canComplete = !it.result.isCommitted,
                        )
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "pen reconciliation proof enqueue failed") }
                    _state.update { it.copy(isCapturingVideo = false, videoMessage = VIDEO_FAILED) }
                }
            }
        }
    }

    private fun markDone() {
        val current = _state.value
        if (!current.canComplete) return
        // Mandatory-video guard (defense in depth alongside canComplete): a completion cannot be
        // submitted without the recorded video's proof upload to couple to.
        val proofItemId = draft.proofs[STEP_RETURN]
        if (!current.videoCaptured || proofItemId.isNullOrBlank()) {
            _state.update { it.copy(videoMessage = VIDEO_REQUIRED) }
            return
        }
        viewModelScope.launch {
            if (current.result.isCorrectable) {
                // A terminal rejection committed nothing server-side. The corrected request is a
                // NEW operation and must not reuse the rejected key.
                statusJob?.cancel()
                statusJob = null
                drafts.putSubmit(
                    flowKey = CaptureFlow.PEN_RECONCILIATION,
                    entityId = cardId,
                    idempotencyKey = "$COMPLETE_KEY_PREFIX:$cardId:${UUID.randomUUID()}",
                    outboxItemId = null,
                )
                draft = drafts.find(CaptureFlow.PEN_RECONCILIATION, cardId)
            }
            // STABLE per card and durable: a re-entered screen resends the SAME key, so a
            // completion the server already committed collapses onto it instead of queueing a
            // second verification.
            val completeIdempotencyKey = draft.submitIdempotencyKey
                ?: "$COMPLETE_KEY_PREFIX:$cardId"
            if (draft.submitIdempotencyKey == null) {
                drafts.putSubmit(CaptureFlow.PEN_RECONCILIATION, cardId, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.PEN_RECONCILIATION, cardId)
            }
            val result = syncRepository.enqueuePenReconciliationComplete(
                // The card id partitions ordering: the mandatory video's proof upload (same group)
                // drains before this completion, and two actions on one card never race.
                groupKey = cardId,
                idempotencyKey = completeIdempotencyKey,
                proofOutboxItemId = proofItemId,
            )
            when (result) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.PEN_RECONCILIATION, cardId, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.PEN_RECONCILIATION, cardId)
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.COUNTS_PEN_RECONCILIATION_COMPLETED)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "pen reconciliation complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.COUNTS_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to KIND_COMPLETE,
                            AnalyticsEvents.Params.REASON to result.message,
                        ),
                    )
                    _state.update {
                        it.copy(result = CountsWriteResultUi(CountsWriteStatus.FAILED, result.message), canComplete = true)
                    }
                }
            }
        }
    }

    private fun resetEvidenceForRework() {
        proofKey.invalidate()
        statusJob?.cancel()
        statusJob = null
        viewModelScope.launch {
            drafts.clearProof(CaptureFlow.PEN_RECONCILIATION, cardId, STEP_RETURN)
            drafts.putSubmit(CaptureFlow.PEN_RECONCILIATION, cardId, null, null)
            draft = drafts.find(CaptureFlow.PEN_RECONCILIATION, cardId)
            observeProofOutbox()
        }
        _state.update {
            it.copy(
                videoCaptured = false,
                videoPreviewPath = null,
                result = CountsWriteResultUi(),
                videoMessage = REWORK_REQUIRED,
            )
        }
    }

    private fun penReturnProofCaption(): String =
        proofOverlayContextLine(
            feature = "Pen return",
            parkLabel = parkLabel,
            locationLabel = belongsLabel.ifBlank { registeredShedId },
        )

    private fun observeProofOutbox() {
        val proofItemId = draft.proofs[STEP_RETURN]?.takeIf { it.isNotBlank() }
        proofStatusJob?.cancel()
        if (proofItemId == null) return
        proofStatusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.items.firstOrNull { it.id == proofItemId } }
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    val proofMessage = when (item.status) {
                        SyncItemStatus.FAILED -> VIDEO_UPLOAD_FAILED
                        SyncItemStatus.SUCCEEDED -> VIDEO_SYNCED
                        else -> VIDEO_QUEUED
                    }
                    _state.update { current ->
                        if (current.result.isCommitted) {
                            current
                        } else {
                            current.copy(
                                videoMessage = proofMessage,
                                // Re-entry restore: the queued upload's own local file is the clip
                                // the operator recorded, so the preview survives Back + reopen
                                // (mirrors FeedDistributionCompleteViewModel).
                                videoPreviewPath = item.localFilePath ?: current.videoPreviewPath,
                            )
                        }
                    }
                }
        }
    }

    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.items.firstOrNull { it.id == itemId } }
                .filterNotNull()
                .distinctUntilChanged()
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
                .collect { item ->
                    item ?: return@collect
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(result = writeResult, canComplete = !writeResult.isCommitted)
                    }
                    if (item.status == SyncItemStatus.SUCCEEDED) {
                        // The card is submitted: drop it from the cached list and clear its draft,
                        // then return the operator to the Reconcile list with the confirmation.
                        repo.forgetCompleted(cardId)
                        drafts.clear(CaptureFlow.PEN_RECONCILIATION, cardId)
                        _state.update { it.copy(returnToList = true, submissionNotice = SYNCED_MESSAGE) }
                    }
                }
        }
    }

    private fun CountsPenReconciliationCardDto.toUiState(
        current: PenReconciliationExecuteUiState,
    ): PenReconciliationExecuteUiState =
        current.copy(
            loading = false,
            notFound = false,
            scannedIdentifier = scannedIdentifier,
            goatDisplayId = goatDisplayId,
            // Backend-composed pen labels, rendered verbatim.
            foundLabel = foundOperationalLocationDisplay.ifBlank { UNKNOWN_LOCATION },
            belongsLabel = registeredOperationalLocationDisplay.ifBlank { registeredShedName.ifBlank { UNKNOWN_LOCATION } },
            reworkReason = reworkReason?.takeIf { it.isNotBlank() },
            videoCaptured = current.videoCaptured || draft.hasProof(STEP_RETURN),
            canComplete = (current.videoCaptured || draft.hasProof(STEP_RETURN)) && !current.result.isCommitted,
        )

    private companion object {
        const val ARG_CARD_ID = "card_id"
        const val COMPLETE_KEY_PREFIX = "counts-pen-reconciliation-complete"
        const val STEP_RETURN = "return"
        const val FIELD_RETURN_VIDEO = "pen_reconciliation_return_video"
        const val SCREEN_EXECUTE = "pen_reconciliation_execute"
        const val KIND_COMPLETE = "pen_reconciliation_complete"
        const val ACTION_CAPTURED = "captured"
        const val PARAM_GROUP_KEY = "group_key"
        const val PARAM_OUTBOX_ITEM_ID = "outbox_item_id"
        const val KEY_PROOF_IDEMPOTENCY = "penReconciliationExecute.proofKey"
        const val UNKNOWN_LOCATION = "—"
        const val QUEUED_MESSAGE = "Saved on this phone. It will sync automatically."
        const val SYNCED_MESSAGE = "Return recorded. The video is with the reviewer."
        const val VIDEO_QUEUED = "Video saved on this phone. It will upload automatically."
        const val VIDEO_SYNCED = "Video synced."
        const val VIDEO_UPLOAD_FAILED = "Video upload failed. Re-record the video."
        const val VIDEO_FAILED = "Couldn't save the video. A video is required — please record it again."
        const val VIDEO_REQUIRED = "Record the pen return video first — it is required evidence."
        const val REWORK_REQUIRED = "The reviewer asked for a new video. Record fresh evidence before submitting again."
    }
}
