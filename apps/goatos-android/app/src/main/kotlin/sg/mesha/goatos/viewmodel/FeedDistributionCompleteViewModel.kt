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
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionResultUi
import sg.mesha.goatos.feature.feed.FeedDistributionStatus
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import java.util.Locale
import javax.inject.Inject

/**
 * The feed-DISTRIBUTION completion detail (`/feed/distribution/complete/...`) — the verifier-GATED
 * Direction flow (docs/decisions/feed-distribution-verification.md). Opened by tapping a Feed
 * DIRECTION row (Packing rows still open the untouched [FeedCompleteViewModel]).
 *
 * THREE offline-first writes, mirroring [ShiftingExecuteViewModel]'s mandatory-video coupling:
 *  - a MANDATORY feed-distribution VIDEO ([SyncRepository.enqueueProofUpload], scope=shed);
 *  - a MANDATORY water-distribution proof — PHOTO ([PhotoCaptureSource]) OR VIDEO
 *    ([ProofCaptureSource]) — also an [SyncRepository.enqueueProofUpload];
 *  - **Submit** ([SyncRepository.enqueueFeedDistributionComplete]) — carries BOTH proof outbox item
 *    ids so the dispatcher resolves each uploaded proof_id and sends the pair; the shed-session flips
 *    to `pending_verification` and NOTHING is completed until a verifier approves.
 *
 * All three enqueue on the SAME outbox group (the shed-session), so the two proofs drain strictly
 * before the completion. STABLE `SavedStateHandle`-persisted idempotency keys collapse a resend after
 * process death onto the original writes; a failed enqueue invalidates its key so a retry mints fresh.
 * Client-side, Submit is gated on BOTH proofs being captured/uploaded; the backend also rejects a
 * blank either proof `422 proof_required`.
 */
@HiltViewModel
class FeedDistributionCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
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

    // The shed-session partitions ordering for BOTH proofs AND the completion, so the proofs drain
    // strictly before the gated completion that references them.
    private val groupKey = "feed-dist:$shedId:$sessionNo:$workflow"

    private val completeKey = DraftIdempotencyKey(savedStateHandle, KEY_COMPLETE_IDEMPOTENCY, "feed-distribution-complete")
    private val videoKey = DraftIdempotencyKey(savedStateHandle, KEY_VIDEO_IDEMPOTENCY, "feed-distribution-video")
    private val waterKey = DraftIdempotencyKey(savedStateHandle, KEY_WATER_IDEMPOTENCY, "feed-distribution-water")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)
    private val videoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_VIDEO_PROOF_ITEM_ID)
    private val waterProofItemId = DraftOutboxItemId(savedStateHandle, KEY_WATER_PROOF_ITEM_ID)

    private val _state = MutableStateFlow(
        FeedDistributionUiState(
            shedLabel = shedLabel,
            sessionLabel = sessionLabel,
            workflowLabel = workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
            videoCaptured = videoProofItemId.value != null,
            waterCaptured = waterProofItemId.value != null,
        ),
    )
    val state: StateFlow<FeedDistributionUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_OPENED)
        recomputeCanComplete()
        outboxItemId.value?.let(::observeOutboxItem)
    }

    fun onEvent(event: FeedDistributionEvent) {
        when (event) {
            FeedDistributionEvent.RecordFeedVideo -> captureFeedVideo()
            FeedDistributionEvent.TakeWaterPhoto -> captureWater(isVideo = false)
            FeedDistributionEvent.RecordWaterVideo -> captureWater(isVideo = true)
            FeedDistributionEvent.MarkDone -> markDone()
            FeedDistributionEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /** MANDATORY feed-distribution video from the LIVE in-app camera. It enqueues a PROOF_UPLOAD on
     *  the shed-session group so it drains before the completion. */
    private fun captureFeedVideo() {
        if (_state.value.isCapturingVideo || _state.value.videoCaptured || shedId.isBlank()) return
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(ProofCapturePrompt.FEED_DISTRIBUTION)
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed distribution video capture failed")
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
                    videoProofItemId.value = result.value
                    analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_VIDEO_CAPTURED)
                    _state.update { it.copy(isCapturingVideo = false, videoCaptured = true, videoMessage = VIDEO_QUEUED) }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    // The row was never created; drop the key so a retry mints a fresh one.
                    videoKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed distribution video enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingVideo = false, videoMessage = PROOF_FAILED) }
                }
            }
        }
    }

    /** MANDATORY water-distribution proof — live-camera PHOTO or VIDEO. It is the second step and
     *  cannot start until the feed video has been captured. */
    private fun captureWater(isVideo: Boolean) {
        if (!_state.value.waterCaptureEnabled || shedId.isBlank()) return
        _state.update { it.copy(isCapturingWater = true, waterMessage = null) }
        viewModelScope.launch {
            val proofType: String
            val mimeType: String
            val localUri: String?
            val durationMs: Long?
            // Video proofs (this water slot MAY be a video) must carry capture_source + the capture
            // window, or the backend rejects them 400 invalid_proof. A photo needs none of the three.
            val captureSource: String?
            val capturedStartMs: Long?
            val capturedEndMs: Long?
            if (isVideo) {
                val captured = try {
                    proofCaptureSource.captureVideo(ProofCapturePrompt.WATER_DISTRIBUTION)
                } catch (error: Exception) {
                    crashReporter.recordException(error, "feed distribution water video capture failed")
                    null
                }
                proofType = "video"
                mimeType = captured?.mimeType ?: "video/mp4"
                localUri = captured?.localUri
                durationMs = captured?.let { (it.endedAtMs - it.startedAtMs).takeIf { d -> d > 0 } }
                captureSource = captured?.captureSource
                capturedStartMs = captured?.startedAtMs
                capturedEndMs = captured?.endedAtMs
            } else {
                val captured = try {
                    photoCaptureSource.capturePhoto()
                } catch (error: Exception) {
                    crashReporter.recordException(error, "feed distribution water photo capture failed")
                    null
                }
                proofType = "photo"
                mimeType = captured?.mimeType ?: "image/jpeg"
                localUri = captured?.localUri
                durationMs = null
                captureSource = null
                capturedStartMs = null
                capturedEndMs = null
            }
            if (localUri == null) {
                _state.update { it.copy(isCapturingWater = false) }
                return@launch
            }
            val metadata = if (proofType == "video" && captureSource != null && capturedStartMs != null && capturedEndMs != null) {
                mapOf(
                    META_SESSION_NO to JsonPrimitive(sessionNo.toString()),
                    META_CAPTURE_SOURCE to JsonPrimitive(captureSource),
                    META_CAPTURED_START_MS to JsonPrimitive(capturedStartMs),
                    META_CAPTURED_END_MS to JsonPrimitive(capturedEndMs),
                )
            } else {
                mapOf(META_SESSION_NO to JsonPrimitive(sessionNo.toString()))
            }
            val request = ProofUploadRequestDto(
                proofType = proofType,
                mimeType = mimeType,
                scopeType = "shed",
                scopeId = shedId,
                subjectType = "shed",
                subjectId = shedId,
                metadata = metadata,
            )
            when (
                val result = syncRepository.enqueueProofUpload(
                    groupKey = groupKey,
                    idempotencyKey = waterKey.current(),
                    request = request,
                    localFilePath = localUri,
                    durationMs = durationMs,
                )
            ) {
                is AppResult.Ok -> {
                    waterProofItemId.value = result.value
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_WATER_PROOF_CAPTURED,
                        mapOf(AnalyticsEvents.Params.KIND to proofType),
                    )
                    _state.update { it.copy(isCapturingWater = false, waterCaptured = true, waterMessage = PROOF_QUEUED) }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    waterKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed distribution water enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingWater = false, waterMessage = PROOF_FAILED) }
                }
            }
        }
    }

    private fun markDone() {
        val current = _state.value
        val videoItem = videoProofItemId.value
        val waterItem = waterProofItemId.value
        // Defense in depth alongside the UI gate: both proofs must exist to submit.
        if (!current.videoCaptured || !current.waterCaptured || videoItem.isNullOrBlank() || waterItem.isNullOrBlank()) {
            _state.update { it.copy(canComplete = false) }
            return
        }
        viewModelScope.launch {
            when (
                val result = syncRepository.enqueueFeedDistributionComplete(
                    groupKey = groupKey,
                    idempotencyKey = completeKey.current(),
                    parkId = parkId,
                    shedId = shedId,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                    distributionProofOutboxItemId = videoItem,
                    waterProofOutboxItemId = waterItem,
                )
            ) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_SUBMITTED)
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed distribution complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update {
                        it.copy(result = FeedDistributionResultUi(FeedDistributionStatus.FAILED, result.message), canComplete = true)
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
                            result = FeedDistributionResultUi(writeResult.status.toDistributionStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && it.videoCaptured && it.waterCaptured,
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedDistributionStatus.SYNCED || r.status == FeedDistributionStatus.QUEUED } ?: false
            it.copy(canComplete = it.videoCaptured && it.waterCaptured && !committed)
        }
    }

    private fun sg.mesha.goatos.feature.counts.CountsWriteStatus.toDistributionStatus(): FeedDistributionStatus = when (this) {
        sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED -> FeedDistributionStatus.SYNCED
        sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED -> FeedDistributionStatus.FAILED
        else -> FeedDistributionStatus.QUEUED
    }

    companion object {
        const val ARG_PARK_ID = "park_id"
        const val ARG_SHED_ID = "shed_id"
        const val ARG_SESSION_NO = "session_no"
        const val ARG_WORKFLOW = "workflow"
        const val ARG_TARGET_DATE = "target_date"
        const val ARG_SHED_LABEL = "shed_label"
        const val ARG_SESSION_LABEL = "session_label"

        private const val KEY_COMPLETE_IDEMPOTENCY = "feedDistribution.completeKey"
        private const val KEY_VIDEO_IDEMPOTENCY = "feedDistribution.videoKey"
        private const val KEY_WATER_IDEMPOTENCY = "feedDistribution.waterKey"
        private const val KEY_OUTBOX_ITEM_ID = "feedDistribution.outboxItemId"
        private const val KEY_VIDEO_PROOF_ITEM_ID = "feedDistribution.videoProofItemId"
        private const val KEY_WATER_PROOF_ITEM_ID = "feedDistribution.waterProofItemId"
        private const val META_SESSION_NO = "session_no"
        private const val META_CAPTURE_SOURCE = "capture_source"
        private const val META_CAPTURED_START_MS = "captured_start_ms"
        private const val META_CAPTURED_END_MS = "captured_end_ms"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the video and water proof."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this feeding is counted."
        private const val VIDEO_QUEUED = "Feed video saved on this phone. It will upload automatically."
        private const val PROOF_QUEUED = "Water proof saved on this phone. It will upload automatically."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }
}
