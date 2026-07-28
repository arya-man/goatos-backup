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
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.counts.CountsWriteResultUi
import sg.mesha.goatos.feature.counts.CountsWriteStatus
import sg.mesha.goatos.feature.counts.ShiftingExecuteAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent
import sg.mesha.goatos.feature.counts.ShiftingExecuteUiState
import java.util.Locale
import javax.inject.Inject

/**
 * The Shifting EXECUTE screen (`/counts/shifting/execute/{shifting_event_id}`) — an operator
 * confirming an approved movement physically happened.
 *
 * Two independent, offline-first writes:
 *  - **Mandatory video** (`enqueueProofUpload`): captured LIVE with the in-app camera OR picked from
 *    the device gallery, registered against the destination shed with the movement id in metadata,
 *    uploaded to GCS via the same signed-URL path as vaccination proof. It is REQUIRED (maintainer
 *    decision, 2026-07-26): completion stays disabled until a video is recorded/uploaded, and a
 *    verifier must approve it before the move is applied.
 *  - **Mark done** (`enqueueShiftingComplete`): the write that RELOCATES the animals. Idempotency is
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

    private val shiftingEventId: String = savedStateHandle[ARG_SHIFTING_EVENT_ID] ?: ""

    // The movement's own destination shed is the outbox group key + proof scope. Resolved from the
    // cached row on load, held here so completion/proof enqueues do not re-read Room.
    private var destinationShedId: String = ""

    private val completeKey = DraftIdempotencyKey(savedStateHandle, KEY_COMPLETE_IDEMPOTENCY, "counts-shifting-complete")
    private val proofKey = DraftIdempotencyKey(savedStateHandle, KEY_PROOF_IDEMPOTENCY, "counts-shifting-proof")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    // The PROOF_UPLOAD outbox item id from recordVideo, persisted so a process-death mid-flow still
    // couples the mandatory video to the completion. The complete carries this id so the sync engine
    // can resolve the uploaded proof_id and send it as proof_ref (maintainer decision, 2026-07-26:
    // a shed move is applied only after a verifier approves the operator's video).
    private val proofOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_PROOF_OUTBOX_ITEM_ID)

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
            ShiftingExecuteEvent.RecordVideo -> captureVideo()
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
    private fun captureVideo() {
        if (_state.value.isCapturingVideo || destinationShedId.isBlank()) return
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(ProofCapturePrompt.SHIFTING)
            } catch (error: Exception) {
                crashReporter.recordException(error, "shifting execute video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
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
                ),
            )
            val result = syncRepository.enqueueProofUpload(
                // Group by the MOVEMENT (not the shed) so this proof drains strictly before the
                // completion enqueued on the same group; the completion resolves this proof's id.
                groupKey = shiftingEventId,
                idempotencyKey = proofKey.current(),
                request = request,
                localFilePath = captured.localUri,
                durationMs = (captured.endedAtMs - captured.startedAtMs).takeIf { it > 0 },
            )
            when (result) {
                is AppResult.Ok -> {
                    proofOutboxItemId.value = result.value
                    analytics.track(AnalyticsEvents.COUNTS_SHIFTING_EXECUTE_VIDEO_CAPTURED)
                    _state.update {
                        it.copy(
                            isCapturingVideo = false,
                            videoCaptured = true,
                            videoMessage = VIDEO_QUEUED,
                            // The mandatory video is now recorded, so completion is allowed.
                            canComplete = !it.result.isCommitted,
                        )
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "shifting execute proof enqueue failed") }
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
        val proofItemId = proofOutboxItemId.value
        if (!current.videoCaptured || proofItemId.isNullOrBlank()) {
            _state.update { it.copy(videoMessage = VIDEO_REQUIRED) }
            return
        }
        viewModelScope.launch {
            val result = syncRepository.enqueueShiftingComplete(
                // The movement id partitions ordering: two actions on the SAME movement drain
                // strictly oldest-first, so a complete and a cancel can never race, and the
                // mandatory video's proof upload (same group) drains before this completion.
                groupKey = shiftingEventId,
                idempotencyKey = completeKey.current(),
                proofOutboxItemId = proofItemId,
            )
            when (result) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    // Leave the pending queue the moment the completion is durable, so the same
                    // movement cannot be completed a second time while its first drains.
                    repo.forgetExecuted(shiftingEventId)
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
            // The mandatory video gates completion: enabled only once a video is captured and the
            // write is not already committed (maintainer decision, 2026-07-26).
            canComplete = current.videoCaptured && !current.result.isCommitted,
        )

    private fun String.titleCase(): String =
        if (isEmpty()) this else replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() }

    private companion object {
        const val ARG_SHIFTING_EVENT_ID = "shifting_event_id"
        const val KEY_COMPLETE_IDEMPOTENCY = "shiftingExecute.completeKey"
        const val KEY_PROOF_IDEMPOTENCY = "shiftingExecute.proofKey"
        const val KEY_OUTBOX_ITEM_ID = "shiftingExecute.outboxItemId"
        const val KEY_PROOF_OUTBOX_ITEM_ID = "shiftingExecute.proofOutboxItemId"
        const val META_SHIFTING_EVENT_ID = "shifting_event_id"
        const val META_CAPTURE_SOURCE = "capture_source"
        const val META_CAPTURED_START_MS = "captured_start_ms"
        const val META_CAPTURED_END_MS = "captured_end_ms"
        const val UNKNOWN_LOCATION = "—"
        const val QUEUED_MESSAGE = "Saved on this phone. The move will sync automatically."
        const val SYNCED_MESSAGE = "Movement completed. The animals are now at the destination shed."
        const val VIDEO_QUEUED = "Video saved on this phone. It will upload automatically."
        const val VIDEO_FAILED = "Couldn't save the video. A video is required — please record or upload it again."
        const val VIDEO_REQUIRED = "Record the video first — a verifier reviews it before the move is applied."
    }
}
