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
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedPackingCompleteEvent
import sg.mesha.goatos.feature.feed.FeedPackingCompleteResultUi
import sg.mesha.goatos.feature.feed.FeedPackingCompleteStatus
import sg.mesha.goatos.feature.feed.FeedPackingCompleteUiState
import java.util.Locale
import javax.inject.Inject

/**
 * The feed-PACKING completion detail (`/feed/packing/complete/...`) — the verifier-GATED Packing
 * flow. Opened by tapping a Feed PACKING row.
 *
 * Simpler than [FeedDistributionCompleteViewModel]: only ONE mandatory proof, so only TWO
 * offline-first writes instead of three:
 *  - a MANDATORY packing VIDEO ([SyncRepository.enqueueProofUpload], scope=shed);
 *  - **Submit** ([SyncRepository.enqueueFeedPackingComplete]) — carries the proof outbox item id
 *    so the dispatcher resolves the uploaded proof_id and sends it; the shed-session flips to
 *    `pending_verification` and NOTHING is completed until a verifier approves.
 *
 * Both enqueue on the SAME outbox group (the shed-session), so the video drains strictly before
 * the completion. STABLE `SavedStateHandle`-persisted idempotency keys collapse a resend after
 * process death onto the original writes; a failed enqueue invalidates its key so a retry mints
 * fresh. Client-side, Submit is gated on the video being captured/uploaded; the backend also
 * rejects a blank proof `422 proof_required`.
 */
@HiltViewModel
class FeedPackingCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val drafts: CaptureDraftRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    // "-" is the blank-park sentinel the route helper uses; it maps back to "" so the backend
    // resolves the default park.
    private val parkId: String = savedStateHandle.get<String>(ARG_PARK_ID)?.takeIf { it != "-" }.orEmpty()
    private val shedId: String = savedStateHandle.get<String>(ARG_SHED_ID).orEmpty()
    private val sessionNo: Int = savedStateHandle.get<String>(ARG_SESSION_NO)?.toIntOrNull() ?: 0
    private val workflow: String = savedStateHandle.get<String>(ARG_WORKFLOW).orEmpty()
    private val targetDate: String = savedStateHandle.get<String>(ARG_TARGET_DATE).orEmpty()
    private val shedLabel: String = savedStateHandle.get<String>(ARG_SHED_LABEL).orEmpty()
    private val sessionLabel: String = savedStateHandle.get<String>(ARG_SESSION_LABEL).orEmpty()
    private val partitionLabel: String = savedStateHandle.get<String>(ARG_PARTITION_LABEL).orEmpty()

    // The shed-PEN-session partitions ordering for BOTH the proof AND the completion, so the proof
    // drains strictly before the gated completion that references it. The PEN is part of this key:
    // see feedCaptureGroupKey for what sharing it across a shed's pens did to the field.
    private val groupKey = feedCaptureGroupKey("feed-pack", shedId, partitionLabel, sessionNo, workflow)

    private val videoKey = DraftIdempotencyKey(savedStateHandle, KEY_VIDEO_IDEMPOTENCY, "feed-packing-video")

    /** Durable per shed-session; see the shared store's kdoc for why SavedStateHandle lost the clip. */
    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(
        FeedPackingCompleteUiState(
            shedLabel = shedLabel,
            sessionLabel = sessionLabel,
            workflowLabel = workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
        ),
    )
    val state: StateFlow<FeedPackingCompleteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.FEED_PACKING_COMPLETE_OPENED)
        viewModelScope.launch {
            draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
            _state.update { it.copy(videoCaptured = draft.hasProof(STEP_VIDEO)) }
            recomputeCanComplete()
            draft.submitOutboxItemId?.let(::observeOutboxItem)
        }
    }

    fun onEvent(event: FeedPackingCompleteEvent) {
        when (event) {
            FeedPackingCompleteEvent.RecordPackingVideo -> capturePackingVideo()
            // Re-record: drop the discarded take's queued upload, then capture afresh.
            FeedPackingCompleteEvent.ReRecordPackingVideo -> {
                viewModelScope.launch {
                    if (_state.value.isCapturingVideo) return@launch
                    draft.proofs[STEP_VIDEO]?.let { syncRepository.deleteOutboxItem(it) }
                    drafts.clearProof(CaptureFlow.FEED_PACKING, groupKey, STEP_VIDEO)
                    draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
                    videoKey.invalidate()
                    _state.update { it.copy(videoCaptured = false, canComplete = false, videoMessage = null) }
                    capturePackingVideo()
                }
            }
            FeedPackingCompleteEvent.MarkDone -> markDone()
            FeedPackingCompleteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /** MANDATORY packing video — a LIVE in-app camera clip. It enqueues a PROOF_UPLOAD on the shed-session group so it
     *  drains before the completion. */
    private fun capturePackingVideo() {
        if (_state.value.isCapturingVideo || _state.value.videoCaptured || shedId.isBlank()) return
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(ProofCapturePrompt.FEED_PACKING)
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed packing video capture failed")
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
                scopeId = shedId,
                subjectType = "shed",
                subjectId = shedId,
                // The backend REQUIRES these three for a video proof (proof/app.validateCreate):
                // capture_source + the capture window. This flow permits only the live in-app
                // camera; omitting them is rejected 400 invalid_proof.
                metadata = mapOf(
                    META_SESSION_NO to JsonPrimitive(sessionNo.toString()),
                    META_CAPTURE_SOURCE to JsonPrimitive(captured.captureSource),
                    META_CAPTURED_START_MS to JsonPrimitive(captured.startedAtMs),
                    META_CAPTURED_END_MS to JsonPrimitive(captured.endedAtMs),
                ),
            )
            when (
                val result = syncRepository.enqueueProofUpload(
                    groupKey = groupKey,
                    idempotencyKey = videoKey.current(),
                    request = request,
                    localFilePath = captured.localUri,
                    durationMs = (captured.endedAtMs - captured.startedAtMs).takeIf { it > 0 },
                )
            ) {
                is AppResult.Ok -> {
                    drafts.putProof(CaptureFlow.FEED_PACKING, groupKey, STEP_VIDEO, result.value)
                    draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
                    analytics.track(AnalyticsEvents.FEED_PACKING_VIDEO_CAPTURED)
                    _state.update { it.copy(isCapturingVideo = false, videoCaptured = true, videoMessage = VIDEO_QUEUED) }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    // The row was never created; drop the key so a retry mints a fresh one.
                    videoKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed packing video enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingVideo = false, videoMessage = PROOF_FAILED) }
                }
            }
        }
    }

    private fun markDone() {
        val current = _state.value
        val videoItem = draft.proofs[STEP_VIDEO]
        // Defense in depth alongside the UI gate: the video must exist to submit.
        if (!current.videoCaptured || videoItem.isNullOrBlank()) {
            _state.update { it.copy(canComplete = false) }
            return
        }
        viewModelScope.launch {
            // STABLE per shed-session and durable: a re-entered screen resends the SAME key so a
            // completion the server already accepted cannot be submitted twice.
            val completeIdempotencyKey = draft.submitIdempotencyKey ?: "feed-packing-complete:$groupKey"
            if (draft.submitIdempotencyKey == null) {
                drafts.putSubmit(CaptureFlow.FEED_PACKING, groupKey, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
            }
            when (
                val result = syncRepository.enqueueFeedPackingComplete(
                    groupKey = groupKey,
                    idempotencyKey = completeIdempotencyKey,
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                    packingProofOutboxItemId = videoItem,
                )
            ) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.FEED_PACKING, groupKey, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.FEED_PACKING_SUBMITTED)
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed packing complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update {
                        it.copy(result = FeedPackingCompleteResultUi(FeedPackingCompleteStatus.FAILED, result.message), canComplete = true)
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
                        it.copy(
                            result = FeedPackingCompleteResultUi(writeResult.status.toPackingStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && it.videoCaptured,
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedPackingCompleteStatus.SYNCED || r.status == FeedPackingCompleteStatus.QUEUED } ?: false
            it.copy(canComplete = it.videoCaptured && !committed)
        }
    }

    private fun sg.mesha.goatos.feature.counts.CountsWriteStatus.toPackingStatus(): FeedPackingCompleteStatus = when (this) {
        sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED -> FeedPackingCompleteStatus.SYNCED
        sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED -> FeedPackingCompleteStatus.FAILED
        else -> FeedPackingCompleteStatus.QUEUED
    }

    companion object {
        const val ARG_PARK_ID = "park_id"
        const val ARG_SHED_ID = "shed_id"
        const val ARG_SESSION_NO = "session_no"
        const val ARG_WORKFLOW = "workflow"
        const val ARG_TARGET_DATE = "target_date"
        const val ARG_SHED_LABEL = "shed_label"
        const val ARG_SESSION_LABEL = "session_label"

        /** The PEN worked. Part of the completion's identity: without it one pen's video closed
         *  out every pen of the shed (STG 2026-08-08). */
        const val ARG_PARTITION_LABEL = "partition_label"

        /** Draft step name in the shared capture-draft store. */
        private const val STEP_VIDEO = "video"
        private const val KEY_COMPLETE_IDEMPOTENCY = "feedPacking.completeKey"
        private const val KEY_VIDEO_IDEMPOTENCY = "feedPacking.videoKey"
        private const val META_SESSION_NO = "session_no"
        private const val META_CAPTURE_SOURCE = "capture_source"
        private const val META_CAPTURED_START_MS = "captured_start_ms"
        private const val META_CAPTURED_END_MS = "captured_end_ms"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the packing video."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this packing is counted."
        private const val VIDEO_QUEUED = "Packing video saved on this phone. It will upload automatically."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }
}
