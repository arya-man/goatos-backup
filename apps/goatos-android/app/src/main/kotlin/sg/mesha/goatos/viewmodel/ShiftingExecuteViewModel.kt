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
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.ShiftingExecuteAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent
import sg.mesha.goatos.feature.counts.ShiftingFeedItemUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteUiState
import java.util.Locale
import java.util.UUID
import javax.inject.Inject

/**
 * The Shifting EXECUTE screen (`/counts/shifting/execute/{shifting_event_id}`) — an operator
 * recording operator completion for a raised or approved movement.
 *
 * Two independent, offline-first writes:
 *  - **Mandatory video** (`enqueueProofUpload`): captured LIVE with the in-app camera and registered
 *    against the destination shed with the movement id in metadata,
 *    uploaded to GCS via the same signed-URL path as vaccination proof. It is REQUIRED (maintainer
 *    decision, 2026-07-26): completion stays disabled until a video is recorded/uploaded, and a
 *    verifier reviews it independently after the task.
 *  - **Mark done** (`enqueueShiftingComplete`): stores completion and relocates only when Park Head
 *    approval already exists; otherwise approval applies it later. Idempotency is
 *    a STABLE `SavedStateHandle`-persisted key keyed to the movement, so a resend after process
 *    death collapses onto the original relocation instead of moving the herd twice.
 *
 * The movement detail is read from the Room-cached pending row (offline-first open): the operator
 * tapped a row already in Room, so no refetch is needed.
 */
@HiltViewModel
class ShiftingExecuteViewModel @Inject constructor(
    private val repo: ShiftingPendingRepository,
    private val drafts: CaptureDraftRepository,
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shiftingEventId: String = savedStateHandle[ARG_SHIFTING_EVENT_ID] ?: ""

    // The movement's own destination shed is the outbox group key + proof scope. Resolved from the
    // cached row on load, held here so completion/proof enqueues do not re-read Room.
    private var destinationShedId: String = ""

    private val proofKey = DraftIdempotencyKey(savedStateHandle, KEY_PROOF_IDEMPOTENCY, "counts-shifting-proof")
    private val packingProofKey = DraftIdempotencyKey(savedStateHandle, KEY_PACKING_PROOF_IDEMPOTENCY, "counts-shifting-packing-proof")
    private val feedingProofKey = DraftIdempotencyKey(savedStateHandle, KEY_FEEDING_PROOF_IDEMPOTENCY, "counts-shifting-feeding-proof")

    /**
     * The movement's DURABLE draft (Room, keyed by movement id) — the recorded proofs' outbox item
     * ids, the completion's idempotency key, and the completion's own outbox item id.
     *
     * These used to live in [SavedStateHandle], which dies with the nav backstack entry. Pressing
     * Back and re-opening the SAME movement therefore built a ViewModel that had forgotten the video
     * already recorded (the operator was asked to shoot it again while the first clip uploaded
     * anyway), and re-minted the completion idempotency key, so a resend could no longer collapse
     * onto the original herd-moving write. Room outlives the backstack; [SavedStateHandle] keeps only
     * the per-recording proof keys, which are meant to be fresh for each new clip.
     */
    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(ShiftingExecuteUiState(shiftingEventId = shiftingEventId))
    val state: StateFlow<ShiftingExecuteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_OPENED)
        loadMovement()
    }

    fun onEvent(event: ShiftingExecuteEvent) {
        when (event) {
            ShiftingExecuteEvent.RecordVideo -> captureVideo(STEP_SHIFTING, ProofCapturePrompt.SHIFTING)
            ShiftingExecuteEvent.RecordFeedPackingVideo -> captureVideo(STEP_PACKING, ProofCapturePrompt.FEED_PACKING)
            ShiftingExecuteEvent.RecordFeedGivenVideo -> captureVideo(STEP_FEEDING, ProofCapturePrompt.SHIFTING_FEED_GIVEN)
            // Re-record: the operator judged the clip unusable. The queued upload is dropped so a
            // discarded take never reaches the verifier, then the fresh capture takes its place.
            ShiftingExecuteEvent.ReRecordVideo -> reRecord(STEP_SHIFTING, ProofCapturePrompt.SHIFTING)
            ShiftingExecuteEvent.ReRecordFeedPackingVideo -> reRecord(STEP_PACKING, ProofCapturePrompt.FEED_PACKING)
            ShiftingExecuteEvent.ReRecordFeedGivenVideo -> reRecord(STEP_FEEDING, ProofCapturePrompt.SHIFTING_FEED_GIVEN)
            ShiftingExecuteEvent.MarkDone -> markDone()
            ShiftingExecuteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun loadMovement() {
        viewModelScope.launch {
            // Rehydrate the durable draft FIRST: everything below (rework reset, feed-config
            // fingerprint check, canComplete) reads the evidence this movement already has.
            draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            draft.submitOutboxItemId?.let(::observeOutboxItem)
            val cached = repo.findCached(shiftingEventId)
            if (cached == null) {
                _state.update { it.copy(loading = false, notFound = true, canComplete = false) }
                return@launch
            }
            when {
                cached.verificationState == "rejected" -> resetEvidenceForRework()
                cached.priority.equals("high", ignoreCase = true) &&
                    draft.fingerprint
                        ?.let { it != cached.feedRequirement?.fingerprint } == true ->
                    resetFeedEvidenceForChangedConfig()
            }
            destinationShedId = cached.destinationShedId
            _state.update { current -> cached.toUiState(current) }
        }
    }

    /**
     * MANDATORY video (maintainer decision, 2026-07-28). Records a LIVE in-app camera clip through
     * the shared [ProofCaptureSource], then enqueues a PROOF_UPLOAD under the MOVEMENT'S outbox group
     * (shiftingEventId) so it drains strictly BEFORE the completion on the same group. The proof
     * outbox item id is retained so the completion can resolve the uploaded proof_id and send it as
     * proof_ref. Until a video is captured, "Mark done" stays disabled.
     */
    private fun captureVideo(step: String, prompt: ProofCapturePrompt) {
        if (_state.value.isCapturingVideo || destinationShedId.isBlank()) return
        _state.update { it.copy(isCapturingVideo = true, capturingStep = step, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(prompt)
            } catch (error: Exception) {
                crashReporter.recordException(error, "shifting execute video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false, capturingStep = null) }
                return@launch
            }
            val result = proofCaptureRepository.capture(
                taskId = shiftingEventId,
                fieldKey = shiftingProofFieldKey(step),
                subject = ProofSubject.SHED,
                subjectId = destinationShedId,
                localUri = captured.localUri,
                mimeType = captured.mimeType,
                caption = "Shifting $step proof",
                scopeType = "shed",
                scopeId = destinationShedId,
                capturedStartMs = captured.startedAtMs,
                capturedEndMs = captured.endedAtMs,
                capturedByPrincipalId = null,
                proofPolicy = feedShedProofPolicy(captured.captureSource),
                awaitUploadEnqueue = true,
                uploadGroupKey = shiftingEventId,
            )
            when (result) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        _state.update { it.copy(isCapturingVideo = false, capturingStep = null, videoMessage = VIDEO_FAILED) }
                        return@launch
                    }
                    // Durable BEFORE the UI flips: if the process dies here, re-entry still finds
                    // the recorded clip instead of asking for it again.
                    drafts.putProof(
                        flowKey = CaptureFlow.SHIFTING,
                        entityId = shiftingEventId,
                        step = step,
                        outboxItemId = proofOutboxId,
                        fingerprint = _state.value.feedConfigFingerprint.takeIf { step != STEP_SHIFTING },
                    )
                    draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
                    analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_VIDEO_CAPTURED)
                    _state.update {
                        it.copy(
                            isCapturingVideo = false,
                            capturingStep = null,
                            videoCaptured = it.videoCaptured || step == "shifting",
                            feedPackingVideoCaptured = it.feedPackingVideoCaptured || step == "packing",
                            feedGivenVideoCaptured = it.feedGivenVideoCaptured || step == "feeding",
                            videoMessage = VIDEO_QUEUED,
                            canComplete = evidenceReady(
                                highPriority = it.highPriority,
                                feedReady = it.feedConfigStatus == "ready",
                                shifting = it.videoCaptured || step == "shifting",
                                packing = it.feedPackingVideoCaptured || step == "packing",
                                feeding = it.feedGivenVideoCaptured || step == "feeding",
                            ) && !it.result.isCommitted,
                        )
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "shifting execute proof enqueue failed") }
                    _state.update { it.copy(isCapturingVideo = false, capturingStep = null, videoMessage = VIDEO_FAILED) }
                }
            }
        }
    }

    /**
     * Replaces one step's clip. The previously queued PROOF_UPLOAD is deleted first (it has not been
     * reviewed and must not upload as a second piece of evidence), then the flow re-enters the normal
     * capture path so the new take is stored durably like any other.
     */
    private fun reRecord(step: String, prompt: ProofCapturePrompt) {
        if (_state.value.isCapturingVideo) return
        viewModelScope.launch {
            draft.proofs[step]?.let { syncRepository.deleteOutboxItem(it) }
            drafts.clearProof(CaptureFlow.SHIFTING, shiftingEventId, step)
            draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            _state.update {
                it.copy(
                    videoCaptured = if (step == STEP_SHIFTING) false else it.videoCaptured,
                    feedPackingVideoCaptured = if (step == STEP_PACKING) false else it.feedPackingVideoCaptured,
                    feedGivenVideoCaptured = if (step == STEP_FEEDING) false else it.feedGivenVideoCaptured,
                    canComplete = false,
                    videoMessage = null,
                )
            }
            captureVideo(step, prompt)
        }
    }

    private fun markDone() {
        val current = _state.value
        if (!current.canComplete) return
        // Mandatory-video guard (defense in depth alongside canComplete): a completion cannot be
        // submitted without the recorded video's proof upload to couple to.
        val proofItemId = draft.proofs[STEP_SHIFTING]
        if (!current.videoCaptured || proofItemId.isNullOrBlank()) {
            _state.update { it.copy(videoMessage = VIDEO_REQUIRED) }
            return
        }
        val packingItemId = draft.proofs[STEP_PACKING]
        val feedingItemId = draft.proofs[STEP_FEEDING]
        if (current.highPriority && (packingItemId.isNullOrBlank() || feedingItemId.isNullOrBlank())) {
            _state.update { it.copy(videoMessage = HIGH_PRIORITY_VIDEO_REQUIRED) }
            return
        }
        viewModelScope.launch {
            if (current.result.isCorrectable) {
                // A terminal rejection committed nothing server-side. The corrected request must
                // get a fresh key; transport retries continue to reuse their stored outbox key.
                statusJob?.cancel()
                statusJob = null
                // A corrected request after a terminal rejection is a NEW operation and must not
                // reuse the rejected key — hence the fresh suffix rather than a re-derivation of
                // the movement-stable key below.
                drafts.putSubmit(
                    flowKey = CaptureFlow.SHIFTING,
                    entityId = shiftingEventId,
                    idempotencyKey = "$COMPLETE_KEY_PREFIX:$shiftingEventId:${UUID.randomUUID()}",
                    outboxItemId = null,
                )
                draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            }
            // STABLE per movement and durable: a re-entered screen resends the SAME key, so a
            // completion the server already committed collapses onto it instead of moving twice.
            val completeIdempotencyKey = draft.submitIdempotencyKey
                ?: "$COMPLETE_KEY_PREFIX:$shiftingEventId"
            if (draft.submitIdempotencyKey == null) {
                drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
            }
            val result = syncRepository.enqueueShiftingComplete(
                // The movement id partitions ordering: two actions on the SAME movement drain
                // strictly oldest-first, so a complete and a cancel can never race, and the
                // mandatory video's proof upload (same group) drains before this completion.
                groupKey = shiftingEventId,
                idempotencyKey = completeIdempotencyKey,
                proofOutboxItemId = proofItemId,
                feedPackingProofOutboxItemId = packingItemId,
                feedGivenProofOutboxItemId = feedingItemId,
                feedConfigFingerprint = current.feedConfigFingerprint,
            )
            when (result) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_COMPLETED)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "shifting complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.COUNTS_WRITE_FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "shifting_complete",
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

    private fun shiftingProofFieldKey(step: String): String = "shifting_${step}_video"

    private fun resetFeedEvidenceForChangedConfig() {
        packingProofKey.invalidate()
        feedingProofKey.invalidate()
        statusJob?.cancel()
        statusJob = null
        viewModelScope.launch {
            drafts.clearProof(CaptureFlow.SHIFTING, shiftingEventId, STEP_PACKING)
            drafts.clearProof(CaptureFlow.SHIFTING, shiftingEventId, STEP_FEEDING)
            drafts.putSubmit(CaptureFlow.SHIFTING, shiftingEventId, null, null)
            draft = drafts.find(CaptureFlow.SHIFTING, shiftingEventId)
        }
        _state.update {
            it.copy(
                feedPackingVideoCaptured = false,
                feedGivenVideoCaptured = false,
                result = CountsWriteResultUi(),
                videoMessage = FEED_CONFIG_REFRESHED,
            )
        }
    }

    private fun resetEvidenceForRework() {
        proofKey.invalidate()
        viewModelScope.launch { drafts.clearProof(CaptureFlow.SHIFTING, shiftingEventId, STEP_SHIFTING) }
        resetFeedEvidenceForChangedConfig()
        _state.update { it.copy(videoCaptured = false, videoMessage = REWORK_REQUIRED) }
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
                        // Keep the cached task until the server accepts it. A feed-config conflict
                        // must remain reopenable so the operator can refresh the new ration.
                        repo.forgetExecuted(shiftingEventId)
                        // The movement is done: its draft has nothing left to protect, so the
                        // table keeps only work still in progress.
                        drafts.clear(CaptureFlow.SHIFTING, shiftingEventId)
                    }
                }
        }
    }

    private fun CountsShiftingPendingExecutionItemDto.toUiState(current: ShiftingExecuteUiState): ShiftingExecuteUiState =
        current.copy(
            loading = false,
            notFound = false,
            // Backend-composed label first, so the operator walking the animals sees the PEN
            // ("Castro - 1" -> "Castro - 2") and not the parent shed on both ends.
            sourceLabel = sourceOperationalLocationDisplay?.takeIf { it.isNotBlank() }
                ?: (sourceShedName ?: sourceParkName)?.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
            destinationLabel = destinationOperationalLocationDisplay.takeIf { it.isNotBlank() }
                ?: destinationShedName.takeIf { it.isNotBlank() }
                ?: destinationParkName.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
            priority = priority.titleCase(),
            category = category.titleCase(),
            animalCount = animalCount,
            animals = animals.map { ShiftingExecuteAnimalUi(it.goatId, it.displayId, it.tag) },
            animalsTruncated = animalsTruncated,
            highPriority = priority.equals("high", ignoreCase = true),
            feedConfigStatus = feedRequirement?.status ?: if (priority.equals("high", true)) "blocked" else "not_required",
            feedConfigBlockedReason = feedRequirement?.blockedReason,
            feedConfigFingerprint = feedRequirement?.fingerprint,
            feedTargetStage = feedRequirement?.targetManagementStage,
            feedItems = feedRequirement?.items?.map { ShiftingFeedItemUi(it.feedItemLabel, it.quantityGrams) }.orEmpty(),
            videoCaptured = current.videoCaptured || draft.hasProof(STEP_SHIFTING),
            feedPackingVideoCaptured = current.feedPackingVideoCaptured || draft.hasProof(STEP_PACKING),
            feedGivenVideoCaptured = current.feedGivenVideoCaptured || draft.hasProof(STEP_FEEDING),
            canComplete = evidenceReady(
                highPriority = priority.equals("high", ignoreCase = true),
                feedReady = feedRequirement?.status == "ready",
                shifting = current.videoCaptured || draft.hasProof(STEP_SHIFTING),
                packing = current.feedPackingVideoCaptured || draft.hasProof(STEP_PACKING),
                feeding = current.feedGivenVideoCaptured || draft.hasProof(STEP_FEEDING),
            ) && !current.result.isCommitted,
        )

    private fun evidenceReady(highPriority: Boolean, feedReady: Boolean, shifting: Boolean, packing: Boolean, feeding: Boolean): Boolean =
        shifting && (!highPriority || (feedReady && packing && feeding))

    private fun String.titleCase(): String =
        if (isEmpty()) this else replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() }

    private companion object {
        const val ARG_SHIFTING_EVENT_ID = "shifting_event_id"
        const val COMPLETE_KEY_PREFIX = "counts-shifting-complete"
        const val STEP_SHIFTING = "shifting"
        const val STEP_PACKING = "packing"
        const val STEP_FEEDING = "feeding"
        const val KEY_PROOF_IDEMPOTENCY = "shiftingExecute.proofKey"
        const val KEY_PACKING_PROOF_IDEMPOTENCY = "shiftingExecute.packingProofKey"
        const val KEY_FEEDING_PROOF_IDEMPOTENCY = "shiftingExecute.feedingProofKey"
        const val UNKNOWN_LOCATION = "—"
        const val QUEUED_MESSAGE = "Saved on this phone. The move will sync automatically."
        const val SYNCED_MESSAGE = "Completion recorded. The move applies when Park Head approval is also present."
        const val VIDEO_QUEUED = "Video saved on this phone. It will upload automatically."
        const val VIDEO_FAILED = "Couldn't save the video. A video is required — please record or upload it again."
        const val VIDEO_REQUIRED = "Record the video first — it is required evidence for this task."
        const val HIGH_PRIORITY_VIDEO_REQUIRED = "Record all three live videos before marking this high-priority shifting done."
        const val FEED_CONFIG_REFRESHED = "Feed configuration changed. Record fresh packing and feeding videos for the updated ration."
        const val REWORK_REQUIRED = "Verification requested rework. Record fresh evidence before submitting again."
    }
}
