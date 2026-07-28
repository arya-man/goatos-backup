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
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.ShiftingExecuteAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent
import sg.mesha.goatos.feature.counts.ShiftingFeedItemUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteUiState
import java.util.Locale
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
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val savedState = savedStateHandle

    private val shiftingEventId: String = savedStateHandle[ARG_SHIFTING_EVENT_ID] ?: ""

    // The movement's own destination shed is the outbox group key + proof scope. Resolved from the
    // cached row on load, held here so completion/proof enqueues do not re-read Room.
    private var destinationShedId: String = ""

    private val completeKey = DraftIdempotencyKey(savedStateHandle, KEY_COMPLETE_IDEMPOTENCY, "counts-shifting-complete")
    private val proofKey = DraftIdempotencyKey(savedStateHandle, KEY_PROOF_IDEMPOTENCY, "counts-shifting-proof")
    private val packingProofKey = DraftIdempotencyKey(savedStateHandle, KEY_PACKING_PROOF_IDEMPOTENCY, "counts-shifting-packing-proof")
    private val feedingProofKey = DraftIdempotencyKey(savedStateHandle, KEY_FEEDING_PROOF_IDEMPOTENCY, "counts-shifting-feeding-proof")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    // The PROOF_UPLOAD outbox item id from recordVideo, persisted so a process-death mid-flow still
    // couples the mandatory video to the completion. The complete carries this id so the sync engine
    // can resolve the uploaded proof_id and send it as proof_ref (maintainer decision, 2026-07-26:
    // verification reviews evidence independently of the approval + completion apply gate).
    private val proofOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_PROOF_OUTBOX_ITEM_ID)
    private val packingProofOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_PACKING_PROOF_OUTBOX_ITEM_ID)
    private val feedingProofOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_FEEDING_PROOF_OUTBOX_ITEM_ID)

    private val _state = MutableStateFlow(ShiftingExecuteUiState(shiftingEventId = shiftingEventId))
    val state: StateFlow<ShiftingExecuteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_OPENED)
        loadMovement()
        outboxItemId.value?.let(::observeOutboxItem)
    }

    fun onEvent(event: ShiftingExecuteEvent) {
        when (event) {
            ShiftingExecuteEvent.RecordVideo -> captureVideo("shifting", ProofCapturePrompt.SHIFTING)
            ShiftingExecuteEvent.RecordFeedPackingVideo -> captureVideo("packing", ProofCapturePrompt.FEED_PACKING)
            ShiftingExecuteEvent.RecordFeedGivenVideo -> captureVideo("feeding", ProofCapturePrompt.SHIFTING_FEED_GIVEN)
            ShiftingExecuteEvent.MarkDone -> markDone()
            ShiftingExecuteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun loadMovement() {
        viewModelScope.launch {
            val cached = repo.findCached(shiftingEventId)
            if (cached == null) {
                _state.update { it.copy(loading = false, notFound = true, canComplete = false) }
                return@launch
            }
            when {
                cached.verificationState == "rejected" -> resetEvidenceForRework()
                cached.priority.equals("high", ignoreCase = true) &&
                    savedState.get<String>(KEY_FEED_EVIDENCE_FINGERPRINT)
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
            val request = ProofUploadRequestDto(
                proofType = "video",
                mimeType = captured.mimeType,
                scopeType = "shed",
                scopeId = destinationShedId,
                subjectType = "shed",
                subjectId = destinationShedId,
                // The backend REQUIRES these three for a video proof (proof/app.validateCreate):
                // capture_source plus the capture window. This flow permits only the live in-app
                // camera; omitting the metadata is rejected 400 invalid_proof.
                metadata = mapOf(
                    META_SHIFTING_EVENT_ID to JsonPrimitive(shiftingEventId),
                    META_CAPTURE_SOURCE to JsonPrimitive(captured.captureSource),
                    META_CAPTURED_START_MS to JsonPrimitive(captured.startedAtMs),
                    META_CAPTURED_END_MS to JsonPrimitive(captured.endedAtMs),
                    META_EVIDENCE_STEP to JsonPrimitive(step),
                ),
            )
            val result = syncRepository.enqueueProofUpload(
                // Group by the MOVEMENT (not the shed) so this proof drains strictly before the
                // completion enqueued on the same group; the completion resolves this proof's id.
                groupKey = shiftingEventId,
                idempotencyKey = freshProofKey(step),
                request = request,
                localFilePath = captured.localUri,
                durationMs = (captured.endedAtMs - captured.startedAtMs).takeIf { it > 0 },
            )
            when (result) {
                is AppResult.Ok -> {
                    when (step) {
                        "packing" -> {
                            packingProofOutboxItemId.value = result.value
                            savedState[KEY_FEED_EVIDENCE_FINGERPRINT] = _state.value.feedConfigFingerprint
                        }
                        "feeding" -> {
                            feedingProofOutboxItemId.value = result.value
                            savedState[KEY_FEED_EVIDENCE_FINGERPRINT] = _state.value.feedConfigFingerprint
                        }
                        else -> proofOutboxItemId.value = result.value
                    }
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

    private fun markDone() {
        val current = _state.value
        if (!current.canComplete) return
        // Mandatory-video guard (defense in depth alongside canComplete): a completion cannot be
        // submitted without the recorded video's proof upload to couple to.
        val proofItemId = proofOutboxItemId.value
        if (!current.videoCaptured || proofItemId.isNullOrBlank()) {
            _state.update { it.copy(videoMessage = VIDEO_REQUIRED) }
            return
        }
        val packingItemId = packingProofOutboxItemId.value
        val feedingItemId = feedingProofOutboxItemId.value
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
                completeKey.invalidate()
                outboxItemId.value = null
            }
            val result = syncRepository.enqueueShiftingComplete(
                // The movement id partitions ordering: two actions on the SAME movement drain
                // strictly oldest-first, so a complete and a cancel can never race, and the
                // mandatory video's proof upload (same group) drains before this completion.
                groupKey = shiftingEventId,
                idempotencyKey = completeKey.current(),
                proofOutboxItemId = proofItemId,
                feedPackingProofOutboxItemId = packingItemId,
                feedGivenProofOutboxItemId = feedingItemId,
                feedConfigFingerprint = current.feedConfigFingerprint,
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
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

    private fun freshProofKey(step: String): String {
        val key = when (step) {
            "packing" -> packingProofKey
            "feeding" -> feedingProofKey
            else -> proofKey
        }
        // Each explicit camera recording is new evidence. Network retries reuse the outbox row's
        // stored key, but a re-record must never collide with the prior clip's payload.
        key.invalidate()
        return key.current()
    }

    private fun resetFeedEvidenceForChangedConfig() {
        packingProofKey.invalidate()
        feedingProofKey.invalidate()
        packingProofOutboxItemId.value = null
        feedingProofOutboxItemId.value = null
        savedState.remove<String>(KEY_FEED_EVIDENCE_FINGERPRINT)
        completeKey.invalidate()
        outboxItemId.value = null
        statusJob?.cancel()
        statusJob = null
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
        proofOutboxItemId.value = null
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
                    }
                }
        }
    }

    private fun CountsShiftingPendingExecutionItemDto.toUiState(current: ShiftingExecuteUiState): ShiftingExecuteUiState =
        current.copy(
            loading = false,
            notFound = false,
            sourceLabel = (sourceShedName ?: sourceParkName)?.takeIf { it.isNotBlank() } ?: UNKNOWN_LOCATION,
            destinationLabel = destinationShedName.takeIf { it.isNotBlank() }
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
            videoCaptured = current.videoCaptured || !proofOutboxItemId.value.isNullOrBlank(),
            feedPackingVideoCaptured = current.feedPackingVideoCaptured || !packingProofOutboxItemId.value.isNullOrBlank(),
            feedGivenVideoCaptured = current.feedGivenVideoCaptured || !feedingProofOutboxItemId.value.isNullOrBlank(),
            canComplete = evidenceReady(
                highPriority = priority.equals("high", ignoreCase = true),
                feedReady = feedRequirement?.status == "ready",
                shifting = current.videoCaptured || !proofOutboxItemId.value.isNullOrBlank(),
                packing = current.feedPackingVideoCaptured || !packingProofOutboxItemId.value.isNullOrBlank(),
                feeding = current.feedGivenVideoCaptured || !feedingProofOutboxItemId.value.isNullOrBlank(),
            ) && !current.result.isCommitted,
        )

    private fun evidenceReady(highPriority: Boolean, feedReady: Boolean, shifting: Boolean, packing: Boolean, feeding: Boolean): Boolean =
        shifting && (!highPriority || (feedReady && packing && feeding))

    private fun String.titleCase(): String =
        if (isEmpty()) this else replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() }

    private companion object {
        const val ARG_SHIFTING_EVENT_ID = "shifting_event_id"
        const val KEY_COMPLETE_IDEMPOTENCY = "shiftingExecute.completeKey"
        const val KEY_PROOF_IDEMPOTENCY = "shiftingExecute.proofKey"
        const val KEY_PACKING_PROOF_IDEMPOTENCY = "shiftingExecute.packingProofKey"
        const val KEY_FEEDING_PROOF_IDEMPOTENCY = "shiftingExecute.feedingProofKey"
        const val KEY_OUTBOX_ITEM_ID = "shiftingExecute.outboxItemId"
        const val KEY_PROOF_OUTBOX_ITEM_ID = "shiftingExecute.proofOutboxItemId"
        const val KEY_PACKING_PROOF_OUTBOX_ITEM_ID = "shiftingExecute.packingProofOutboxItemId"
        const val KEY_FEEDING_PROOF_OUTBOX_ITEM_ID = "shiftingExecute.feedingProofOutboxItemId"
        const val KEY_FEED_EVIDENCE_FINGERPRINT = "shiftingExecute.feedEvidenceFingerprint"
        const val META_SHIFTING_EVENT_ID = "shifting_event_id"
        const val META_CAPTURE_SOURCE = "capture_source"
        const val META_CAPTURED_START_MS = "captured_start_ms"
        const val META_CAPTURED_END_MS = "captured_end_ms"
        const val META_EVIDENCE_STEP = "shifting_evidence_step"
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
