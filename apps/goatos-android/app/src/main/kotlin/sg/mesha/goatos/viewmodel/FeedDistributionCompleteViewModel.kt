package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
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
 * FOUR offline-first writes, mirroring [ShiftingExecuteViewModel]'s mandatory-video coupling:
 *  - a MANDATORY feed-weight PHOTO ([PhotoCaptureSource], live camera — the backend additionally
 *    asserts `capture_source = in_app_camera`, because this is the capture that carries a number);
 *  - a MANDATORY feed-distribution VIDEO ([ProofCaptureSource], scope=shed);
 *  - a MANDATORY water-distribution VIDEO — video-only since 2026-08-11, the photo path is gone;
 *  - **Submit** ([SyncRepository.enqueueFeedDistributionComplete]) — carries ALL THREE proof outbox
 *    item ids so the dispatcher resolves each uploaded proof_id and sends the set; the shed-session
 *    flips to `pending_verification` and NOTHING is completed until a verifier approves.
 *
 * All four enqueue on the SAME outbox group (the shed-session), so the three proofs drain strictly
 * before the completion. STABLE `SavedStateHandle`-persisted idempotency keys collapse a resend after
 * process death onto the original writes; a failed enqueue invalidates its key so a retry mints fresh.
 * Client-side, Submit is gated on ALL THREE proofs being captured/uploaded; the backend also rejects
 * a blank or wrong-kind proof `422 proof_required`.
 *
 * The capture ORDER is enforced by the UI state (weight -> feed -> water) because the weight photo
 * can only be taken while the feed is still on the scale.
 */
@HiltViewModel
class FeedDistributionCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
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

    // The row's backend-owned lifecycle bucket. The ONLY signal this screen has that the session is
    // already with the verifier: the capture draft is local and a reinstall wipes it, which is how
    // an operator was shown an empty form for work already submitted (STG 2026-08-09).
    private val alreadySubmitted: Boolean =
        !feedSessionCanCapture(savedStateHandle.get<String>(ARG_LIFECYCLE_STATUS).orEmpty(), isToday = true)

    // The day-shed-PEN-session partitions ordering for BOTH proofs AND the completion, so the
    // proofs drain strictly before the gated completion that references them. The PEN and the DAY
    // are both part of this key: see feedCaptureGroupKey for what dropping either did to the field.
    private val groupKey =
        feedCaptureGroupKey("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate)

    private val weightKey = DraftIdempotencyKey(savedStateHandle, KEY_WEIGHT_IDEMPOTENCY, "feed-distribution-weight")
    private val videoKey = DraftIdempotencyKey(savedStateHandle, KEY_VIDEO_IDEMPOTENCY, "feed-distribution-video")
    private val waterKey = DraftIdempotencyKey(savedStateHandle, KEY_WATER_IDEMPOTENCY, "feed-distribution-water")

    /**
     * The shed-session's DURABLE draft: the recorded proofs' outbox item ids and the completion's
     * idempotency key, keyed by [groupKey] in the shared capture-draft store.
     *
     * These used to live in [SavedStateHandle], which dies with the nav backstack entry — so
     * recording the feed video, pressing Back and re-opening the session lost the clip and asked for
     * it again while the first one uploaded anyway (maintainer report 2026-07-30, the same defect
     * first seen on Shifting). The completion key was lost with it, defeating the idempotency that
     * stops a double submit.
     */
    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(
        FeedDistributionUiState(
            shedLabel = shedLabel,
            sessionLabel = sessionLabel,
            workflowLabel = workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
            alreadySubmitted = alreadySubmitted,
        ),
    )
    val state: StateFlow<FeedDistributionUiState> = _state.asStateFlow()

    private var statusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_OPENED)
        viewModelScope.launch {
            // Rehydrate BEFORE the first render decision: a re-entered session must show the proofs
            // it already has rather than an empty form.
            draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
            _state.update {
                it.copy(
                    weightCaptured = draft.hasProof(STEP_WEIGHT),
                    videoCaptured = draft.hasProof(STEP_VIDEO),
                    waterCaptured = draft.hasProof(STEP_WATER),
                )
            }
            recomputeCanComplete()
            draft.submitOutboxItemId?.let(::observeOutboxItem)
        }
    }

    fun onEvent(event: FeedDistributionEvent) {
        when (event) {
            FeedDistributionEvent.TakeFeedWeightPhoto -> captureFeedWeightPhoto()
            FeedDistributionEvent.RecordFeedVideo -> captureFeedVideo()
            FeedDistributionEvent.RecordWaterVideo -> captureWaterVideo()
            // Re-record/re-take: drop the queued upload of the take being discarded so the verifier
            // never receives two captures for one step, then capture afresh.
            FeedDistributionEvent.ReTakeFeedWeightPhoto -> reCapture(STEP_WEIGHT) { captureFeedWeightPhoto() }
            FeedDistributionEvent.ReRecordFeedVideo -> reCapture(STEP_VIDEO) { captureFeedVideo() }
            FeedDistributionEvent.ReRecordWaterVideo -> reCapture(STEP_WATER) { captureWaterVideo() }
            FeedDistributionEvent.MarkDone -> markDone()
            FeedDistributionEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /**
     * Discards one step's captured proof and re-runs its capture. The queued PROOF_UPLOAD is deleted
     * first: it has not been reviewed, and leaving it would submit a clip the operator rejected.
     */
    private fun reCapture(step: String, capture: () -> Unit) {
        if (_state.value.isCapturingWeight || _state.value.isCapturingVideo || _state.value.isCapturingWater) return
        viewModelScope.launch {
            draft.proofs[step]?.let { syncRepository.deleteOutboxItem(it) }
            drafts.clearProof(CaptureFlow.FEED_DISTRIBUTION, groupKey, step)
            draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
            when (step) {
                STEP_WEIGHT -> weightKey.invalidate()
                STEP_VIDEO -> videoKey.invalidate()
                STEP_WATER -> waterKey.invalidate()
            }
            _state.update {
                it.copy(
                    weightCaptured = if (step == STEP_WEIGHT) false else it.weightCaptured,
                    videoCaptured = if (step == STEP_VIDEO) false else it.videoCaptured,
                    waterCaptured = if (step == STEP_WATER) false else it.waterCaptured,
                    canComplete = false,
                    weightMessage = if (step == STEP_WEIGHT) null else it.weightMessage,
                    videoMessage = if (step == STEP_VIDEO) null else it.videoMessage,
                    waterMessage = if (step == STEP_WATER) null else it.waterMessage,
                )
            }
            capture()
        }
    }

    /**
     * STEP 1 — MANDATORY feed-weight PHOTO from the LIVE in-app camera.
     *
     * `capture_source` is sent and the backend ASSERTS it is `in_app_camera` for this step (the two
     * videos are not asserted that way). A gallery still of a scale is a reading from some other day,
     * and this is the only capture a verifier can check against the expected ration.
     */
    private fun captureFeedWeightPhoto() {
        if (!_state.value.weightCaptureEnabled || shedId.isBlank()) return
        _state.update { it.copy(isCapturingWeight = true, weightMessage = null) }
        viewModelScope.launch {
            val captured = try {
                photoCaptureSource.capturePhoto()
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed weight photo capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingWeight = false) }
                return@launch
            }
            val request = ProofUploadRequestDto(
                proofType = "photo",
                mimeType = captured.mimeType,
                scopeType = "shed",
                scopeId = shedId,
                subjectType = "shed",
                subjectId = shedId,
                // No capture window: a photo is an instant, and the backend requires start/end only
                // for videos. capture_source IS required here -- see the KDoc above.
                metadata = mapOf(
                    META_SESSION_NO to JsonPrimitive(sessionNo.toString()),
                    META_CAPTURE_SOURCE to JsonPrimitive(captured.captureSource),
                ),
            )
            when (
                val result = syncRepository.enqueueProofUpload(
                    groupKey = groupKey,
                    idempotencyKey = weightKey.current(),
                    request = request,
                    localFilePath = captured.localUri,
                    durationMs = null,
                )
            ) {
                is AppResult.Ok -> {
                    // Durable BEFORE the UI flips: a process death here must not lose the photo.
                    drafts.putProof(CaptureFlow.FEED_DISTRIBUTION, groupKey, STEP_WEIGHT, result.value)
                    draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
                    analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_WEIGHT_PHOTO_CAPTURED)
                    _state.update { it.copy(isCapturingWeight = false, weightCaptured = true, weightMessage = WEIGHT_QUEUED) }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    // The row was never created; drop the key so a retry mints a fresh one.
                    weightKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed weight photo enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingWeight = false, weightMessage = PROOF_FAILED) }
                }
            }
        }
    }

    /** MANDATORY feed-distribution video from the LIVE in-app camera. It enqueues a PROOF_UPLOAD on
     *  the shed-session group so it drains before the completion. */
    private fun captureFeedVideo() {
        if (!_state.value.videoCaptureEnabled || shedId.isBlank()) return
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
            when (
                val result = proofCaptureRepository.capture(
                    taskId = groupKey,
                    fieldKey = FIELD_FEED_DISTRIBUTION_VIDEO,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = "Feed distribution session $sessionNo",
                    scopeType = "shed",
                    scopeId = shedId,
                    capturedStartMs = captured.startedAtMs,
                    capturedEndMs = captured.endedAtMs,
                    capturedByPrincipalId = null,
                    proofPolicy = feedShedProofPolicy(captured.captureSource),
                    awaitUploadEnqueue = true,
                    uploadGroupKey = groupKey,
                )
            ) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        _state.update { it.copy(isCapturingVideo = false, videoMessage = PROOF_FAILED) }
                        return@launch
                    }
                    // Durable BEFORE the UI flips: a process death here must not lose the clip.
                    drafts.putProof(CaptureFlow.FEED_DISTRIBUTION, groupKey, STEP_VIDEO, proofOutboxId)
                    draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
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

    /**
     * STEP 3 — MANDATORY water-distribution VIDEO from the LIVE in-app camera.
     *
     * VIDEO-ONLY since 2026-08-11. The photo branch (and its `isVideo` parameter) is deliberately
     * gone rather than defaulted: a photo of a full trough proves a trough is full, not that this
     * operator filled it today. The backend rejects a photo here `422 proof_required`.
     */
    private fun captureWaterVideo() {
        if (!_state.value.waterCaptureEnabled || shedId.isBlank()) return
        _state.update { it.copy(isCapturingWater = true, waterMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(ProofCapturePrompt.WATER_DISTRIBUTION)
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed distribution water video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingWater = false) }
                return@launch
            }
            when (
                val result = proofCaptureRepository.capture(
                    taskId = groupKey,
                    fieldKey = FIELD_FEED_DISTRIBUTION_WATER_VIDEO,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = "Water distribution video session $sessionNo",
                    scopeType = "shed",
                    scopeId = shedId,
                    capturedStartMs = captured.startedAtMs,
                    capturedEndMs = captured.endedAtMs,
                    capturedByPrincipalId = null,
                    proofPolicy = feedShedProofPolicy(captured.captureSource),
                    awaitUploadEnqueue = true,
                    uploadGroupKey = groupKey,
                )
            ) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        _state.update { it.copy(isCapturingWater = false, waterMessage = PROOF_FAILED) }
                        return@launch
                    }
                    drafts.putProof(CaptureFlow.FEED_DISTRIBUTION, groupKey, STEP_WATER, proofOutboxId)
                    draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_WATER_PROOF_CAPTURED,
                        mapOf(AnalyticsEvents.Params.KIND to "video"),
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
        val weightItem = draft.proofs[STEP_WEIGHT]
        val videoItem = draft.proofs[STEP_VIDEO]
        val waterItem = draft.proofs[STEP_WATER]
        // Defense in depth alongside the UI gate: all three proofs must exist to submit.
        if (!current.weightCaptured || !current.videoCaptured || !current.waterCaptured ||
            weightItem.isNullOrBlank() || videoItem.isNullOrBlank() || waterItem.isNullOrBlank()
        ) {
            _state.update { it.copy(canComplete = false) }
            return
        }
        viewModelScope.launch {
            // STABLE per shed-session and durable, so a re-entered screen resends the SAME key and a
            // completion the server already accepted collapses onto it instead of submitting twice.
            val completeIdempotencyKey = draft.submitIdempotencyKey ?: "feed-distribution-complete:$groupKey"
            if (draft.submitIdempotencyKey == null) {
                drafts.putSubmit(CaptureFlow.FEED_DISTRIBUTION, groupKey, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
            }
            when (
                val result = syncRepository.enqueueFeedDistributionComplete(
                    groupKey = groupKey,
                    idempotencyKey = completeIdempotencyKey,
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                    feedWeightProofOutboxItemId = weightItem,
                    distributionProofOutboxItemId = videoItem,
                    waterProofOutboxItemId = waterItem,
                )
            ) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.FEED_DISTRIBUTION, groupKey, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.FEED_DISTRIBUTION, groupKey)
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
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(
                            result = FeedDistributionResultUi(writeResult.status.toDistributionStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted &&
                                it.weightCaptured && it.videoCaptured && it.waterCaptured,
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedDistributionStatus.SYNCED || r.status == FeedDistributionStatus.QUEUED } ?: false
            it.copy(canComplete = it.weightCaptured && it.videoCaptured && it.waterCaptured && !committed)
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

        /** The PEN worked. Part of the completion's identity: without it one pen's video closed
         *  out every pen of the shed (STG 2026-08-08). */
        const val ARG_PARTITION_LABEL = "partition_label"
        const val ARG_LIFECYCLE_STATUS = "lifecycle_status"

        /** Draft step names in the shared capture-draft store. */
        private const val STEP_WEIGHT = "weight"
        private const val STEP_VIDEO = "video"
        private const val STEP_WATER = "water"
        private const val KEY_COMPLETE_IDEMPOTENCY = "feedDistribution.completeKey"
        private const val KEY_WEIGHT_IDEMPOTENCY = "feedDistribution.weightKey"
        private const val KEY_VIDEO_IDEMPOTENCY = "feedDistribution.videoKey"
        private const val KEY_WATER_IDEMPOTENCY = "feedDistribution.waterKey"
        private const val META_SESSION_NO = "session_no"
        private const val META_CAPTURE_SOURCE = "capture_source"
        private const val FIELD_FEED_DISTRIBUTION_VIDEO = "feed_distribution_video"
        private const val FIELD_FEED_DISTRIBUTION_WATER_VIDEO = "feed_distribution_water_video"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the weight photo, feed video and water video."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this feeding is counted."
        private const val WEIGHT_QUEUED = "Feed weight photo saved on this phone. It will upload automatically."
        private const val VIDEO_QUEUED = "Feed video saved on this phone. It will upload automatically."
        private const val PROOF_QUEUED = "Water video saved on this phone. It will upload automatically."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }
}
