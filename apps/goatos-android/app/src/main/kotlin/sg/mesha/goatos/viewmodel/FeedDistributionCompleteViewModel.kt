package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.qualifiers.ApplicationContext
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
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedDistributionResultUi
import sg.mesha.goatos.feature.feed.FeedDistributionStatus
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import java.util.Locale
import java.util.EnumSet
import javax.inject.Inject

/**
 * The feed-DISTRIBUTION completion detail (`/feed/distribution/complete/...`) — the verifier-GATED
 * Direction flow (docs/decisions/feed-distribution-verification.md). Opened by tapping a Feed
 * DIRECTION row (Packing rows still open the untouched [FeedCompleteViewModel]).
 *
 * THREE offline-first proof writes:
 *  - a MANDATORY feed weight PHOTO ([PhotoCaptureSource]), scope=shed;
 *  - a MANDATORY feed-distribution VIDEO ([SyncRepository.enqueueProofUpload], scope=shed);
 *  - a MANDATORY water-distribution VIDEO ([ProofCaptureSource]), scope=shed.
 *
 * All three proofs can be captured in any order and replaced before final submit. The final
 * verifier-gated completion unlocks only after all three proof uploads have synced. The completion
 * still references the backend's existing feed-video and water-video proof refs.
 */
@HiltViewModel
class FeedDistributionCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val photoCaptureSource: PhotoCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
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

    // The shed-session partitions ordering for the proof uploads and completion, so all three
    // proof items drain before the gated completion references them.
    private val groupKey = feedCaptureGroupKey("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate)

    private val completeKey = DraftIdempotencyKey(savedStateHandle, KEY_COMPLETE_IDEMPOTENCY, "feed-distribution-complete")
    private val feedWeightPhotoKey = DraftIdempotencyKey(savedStateHandle, KEY_FEED_WEIGHT_PHOTO_IDEMPOTENCY, "feed-distribution-feed-weight-photo")
    private val videoKey = DraftIdempotencyKey(savedStateHandle, KEY_VIDEO_IDEMPOTENCY, "feed-distribution-video")
    private val waterVideoKey = DraftIdempotencyKey(savedStateHandle, KEY_WATER_VIDEO_IDEMPOTENCY, "feed-distribution-water-video")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)
    private val feedWeightPhotoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_FEED_WEIGHT_PHOTO_PROOF_ITEM_ID)
    private val videoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_VIDEO_PROOF_ITEM_ID)
    private val waterVideoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_WATER_VIDEO_PROOF_ITEM_ID)
    private var completeEnqueueInFlight = false
    private val syncedProofAnalytics: MutableSet<ProofSlot> =
        EnumSet.noneOf(ProofSlot::class.java) // mobile-guard:ignore bounded by ProofSlot enum.

    private val _state = MutableStateFlow(
        FeedDistributionUiState(
            shedLabel = shedLabel,
            sessionLabel = sessionLabel,
            workflowLabel = workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
            feedWeightPhotoCaptured = feedWeightPhotoProofItemId.value != null,
            feedWeightPhotoStatus = feedWeightPhotoProofItemId.value?.let { FeedDistributionProofStatus.QUEUED } ?: FeedDistributionProofStatus.EMPTY,
            videoCaptured = videoProofItemId.value != null,
            videoStatus = videoProofItemId.value?.let { FeedDistributionProofStatus.QUEUED } ?: FeedDistributionProofStatus.EMPTY,
            waterVideoCaptured = waterVideoProofItemId.value != null,
            waterVideoStatus = waterVideoProofItemId.value?.let { FeedDistributionProofStatus.QUEUED } ?: FeedDistributionProofStatus.EMPTY,
        ),
    )
    val state: StateFlow<FeedDistributionUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var syncStatusJob: Job? = null
    private var feedWeightPhotoStatusJob: Job? = null
    private var feedVideoStatusJob: Job? = null
    private var waterVideoStatusJob: Job? = null
    init {
        analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_OPENED)
        recomputeCanComplete()
        observeSyncStatus()
        outboxItemId.value?.let(::observeOutboxItem)
        feedWeightPhotoProofItemId.value?.let { observeProofItem(ProofSlot.FEED_WEIGHT_PHOTO, it) }
        videoProofItemId.value?.let { observeProofItem(ProofSlot.FEED_VIDEO, it) }
        waterVideoProofItemId.value?.let { observeProofItem(ProofSlot.WATER_VIDEO, it) }
    }

    fun onEvent(event: FeedDistributionEvent) {
        when (event) {
            FeedDistributionEvent.RecordFeedVideo -> captureFeedVideo()
            FeedDistributionEvent.TakeFeedWeightPhoto -> captureFeedWeightPhoto()
            FeedDistributionEvent.RecordWaterVideo -> captureWaterVideo()
            FeedDistributionEvent.MarkDone -> markDone()
            FeedDistributionEvent.SyncNow -> syncNow()
            FeedDistributionEvent.Back -> analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_BACK_TAPPED)
        }
    }

    private fun captureFeedWeightPhoto() {
        if (_state.value.isCapturingFeedWeightPhoto || _state.value.isFinalSubmitted || shedId.isBlank()) return
        val replacing = _state.value.feedWeightPhotoCaptured
        if (replacing) trackReuploadTapped(ProofSlot.FEED_WEIGHT_PHOTO)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_CAPTURE_TAPPED,
            mapOf(AnalyticsEvents.Params.KIND to "feed_weight_photo"),
        )
        _state.update { it.copy(isCapturingFeedWeightPhoto = true, feedWeightPhotoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                photoCaptureSource.capturePhoto(
                    PhotoCaptureContext(
                        title = appContext.getString(R.string.proof_photo_feed_weight_title),
                        instruction = appContext.getString(R.string.proof_photo_feed_weight_instruction),
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed distribution feed weight photo capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingFeedWeightPhoto = false) }
                return@launch
            }
            when (
                val result = proofCaptureRepository.capture(
                    taskId = groupKey,
                    fieldKey = FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = "Feed weight photo session $sessionNo",
                    scopeType = "shed",
                    scopeId = shedId,
                    capturedStartMs = captured.capturedAtMs,
                    capturedEndMs = captured.capturedAtMs,
                    capturedByPrincipalId = null,
                    proofPolicy = feedShedProofPolicy(captured.captureSource),
                    awaitUploadEnqueue = true,
                    uploadGroupKey = groupKey,
                )
            ) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        _state.update { it.copy(isCapturingFeedWeightPhoto = false, feedWeightPhotoMessage = PROOF_FAILED) }
                        return@launch
                    }
                    feedWeightPhotoProofItemId.value = proofOutboxId
                    observeProofItem(ProofSlot.FEED_WEIGHT_PHOTO, proofOutboxId)
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_PROOF_CAPTURED,
                        mapOf(AnalyticsEvents.Params.KIND to "feed_weight_photo"),
                    )
                    _state.update {
                        it.copy(
                            isCapturingFeedWeightPhoto = false,
                            feedWeightPhotoCaptured = true,
                            feedWeightPhotoPreviewPath = captured.localUri,
                            feedWeightPhotoStatus = FeedDistributionProofStatus.QUEUED,
                            feedWeightPhotoMessage = PROOF_QUEUED,
                        )
                    }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    feedWeightPhotoKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed distribution feed weight photo enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingFeedWeightPhoto = false, feedWeightPhotoMessage = PROOF_FAILED) }
                }
            }
        }
    }

    /** MANDATORY feed-distribution video from the LIVE in-app camera. It enqueues a PROOF_UPLOAD on
     *  the shed-session group so it drains before the completion. */
    private fun captureFeedVideo() {
        if (_state.value.isCapturingVideo || _state.value.isFinalSubmitted || shedId.isBlank()) return
        val replacing = _state.value.videoCaptured
        if (replacing) trackReuploadTapped(ProofSlot.FEED_VIDEO)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_CAPTURE_TAPPED,
            mapOf(AnalyticsEvents.Params.KIND to "feed_video"),
        )
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(feedVideoContext())
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
                    videoProofItemId.value = proofOutboxId
                    observeProofItem(ProofSlot.FEED_VIDEO, proofOutboxId)
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_PROOF_CAPTURED,
                        mapOf(AnalyticsEvents.Params.KIND to "feed_video"),
                    )
                    _state.update {
                        it.copy(
                            isCapturingVideo = false,
                            videoCaptured = true,
                            videoPreviewPath = captured.localUri,
                            videoStatus = FeedDistributionProofStatus.QUEUED,
                            videoMessage = VIDEO_QUEUED,
                        )
                    }
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

    private fun captureWaterVideo() {
        if (_state.value.isCapturingWaterVideo || !_state.value.waterVideoCaptureEnabled || shedId.isBlank()) return
        val replacing = _state.value.waterVideoCaptured
        if (replacing) trackReuploadTapped(ProofSlot.WATER_VIDEO)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_CAPTURE_TAPPED,
            mapOf(AnalyticsEvents.Params.KIND to "water_video"),
        )
        _state.update { it.copy(isCapturingWaterVideo = true, waterVideoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(waterVideoContext())
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed distribution water video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingWaterVideo = false) }
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
                        _state.update { it.copy(isCapturingWaterVideo = false, waterVideoMessage = PROOF_FAILED) }
                        return@launch
                    }
                    waterVideoProofItemId.value = proofOutboxId
                    observeProofItem(ProofSlot.WATER_VIDEO, proofOutboxId)
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_PROOF_CAPTURED,
                        mapOf(AnalyticsEvents.Params.KIND to "water_video"),
                    )
                    _state.update {
                        it.copy(
                            isCapturingWaterVideo = false,
                            waterVideoCaptured = true,
                            waterVideoPreviewPath = captured.localUri,
                            waterVideoStatus = FeedDistributionProofStatus.QUEUED,
                            waterVideoMessage = PROOF_QUEUED,
                        )
                    }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    waterVideoKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed distribution water video enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingWaterVideo = false, waterVideoMessage = PROOF_FAILED) }
                }
            }
        }
    }

    private fun markDone() {
        if (completeEnqueueInFlight) return
        val current = _state.value
        val feedWeightPhotoItem = feedWeightPhotoProofItemId.value
        val videoItem = videoProofItemId.value
        val waterVideoItem = waterVideoProofItemId.value
        if (
            !current.submitEnabled ||
            feedWeightPhotoItem.isNullOrBlank() ||
            videoItem.isNullOrBlank() ||
            waterVideoItem.isNullOrBlank()
        ) {
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_BLOCKED,
                mapOf(
                    "feed_weight_photo_status" to current.feedWeightPhotoStatus.name.lowercase(Locale.ROOT),
                    "feed_video_status" to current.videoStatus.name.lowercase(Locale.ROOT),
                    "water_video_status" to current.waterVideoStatus.name.lowercase(Locale.ROOT),
                ),
            )
            _state.update { it.copy(canComplete = false) }
            return
        }
        completeEnqueueInFlight = true
        viewModelScope.launch {
            when (
                val result = syncRepository.enqueueFeedDistributionComplete(
                    groupKey = groupKey,
                    idempotencyKey = completeKey.current(),
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                    feedWeightProofOutboxItemId = feedWeightPhotoItem,
                    distributionProofOutboxItemId = videoItem,
                    waterProofOutboxItemId = waterVideoItem,
                )
            ) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_SUBMITTED)
                    completeEnqueueInFlight = false
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    completeEnqueueInFlight = false
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

    private fun syncNow() {
        analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_SYNC_TAPPED)
        viewModelScope.launch {
            syncRepository.triggerDrain()
        }
    }

    private fun observeSyncStatus() {
        syncStatusJob?.cancel()
        syncStatusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.inFlightCount > 0 }
                .distinctUntilChanged()
                .collect { syncing ->
                    _state.update { it.copy(isSyncing = syncing) }
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
                            canComplete = !writeResult.isCommitted && it.feedWeightPhotoCaptured && it.videoCaptured && it.waterVideoCaptured,
                        )
                    }
                }
        }
    }

    private fun observeProofItem(slot: ProofSlot, itemId: String) {
        val jobRef = when (slot) {
            ProofSlot.FEED_WEIGHT_PHOTO -> ::feedWeightPhotoStatusJob
            ProofSlot.FEED_VIDEO -> ::feedVideoStatusJob
            ProofSlot.WATER_VIDEO -> ::waterVideoStatusJob
        }
        jobRef.get()?.cancel()
        jobRef.set(
            viewModelScope.launch {
                syncRepository.observeItem(itemId)
                    .filterNotNull()
                    .distinctUntilChanged()
                    .collect { item ->
                        updateProofStatus(slot, item)
                    }
            },
        )
    }

    private fun updateProofStatus(slot: ProofSlot, item: SyncQueueItem) {
        val proofStatus = when (item.status) {
            SyncItemStatus.QUEUED -> FeedDistributionProofStatus.QUEUED
            SyncItemStatus.IN_FLIGHT -> FeedDistributionProofStatus.UPLOADING
            SyncItemStatus.SUCCEEDED -> FeedDistributionProofStatus.SYNCED
            SyncItemStatus.FAILED -> FeedDistributionProofStatus.FAILED
        }
        val message = when (proofStatus) {
            FeedDistributionProofStatus.QUEUED -> PROOF_QUEUED
            FeedDistributionProofStatus.UPLOADING -> PROOF_UPLOADING
            FeedDistributionProofStatus.SYNCED -> PROOF_SYNCED
            FeedDistributionProofStatus.FAILED -> item.lastError ?: PROOF_FAILED
            FeedDistributionProofStatus.EMPTY -> null
        }
        _state.update {
            when (slot) {
                ProofSlot.FEED_WEIGHT_PHOTO -> it.copy(
                    feedWeightPhotoCaptured = it.feedWeightPhotoCaptured || item.localFilePath != null,
                    feedWeightPhotoPreviewPath = it.feedWeightPhotoPreviewPath ?: item.localFilePath,
                    feedWeightPhotoStatus = proofStatus,
                    feedWeightPhotoMessage = message,
                )
                ProofSlot.FEED_VIDEO -> it.copy(
                    videoCaptured = it.videoCaptured || item.localFilePath != null,
                    videoPreviewPath = it.videoPreviewPath ?: item.localFilePath,
                    videoStatus = proofStatus,
                    videoMessage = message,
                )
                ProofSlot.WATER_VIDEO -> it.copy(
                    waterVideoCaptured = it.waterVideoCaptured || item.localFilePath != null,
                    waterVideoPreviewPath = it.waterVideoPreviewPath ?: item.localFilePath,
                    waterVideoStatus = proofStatus,
                    waterVideoMessage = message,
                )
            }
        }
        if (proofStatus == FeedDistributionProofStatus.SYNCED && syncedProofAnalytics.add(slot)) {
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_PROOF_UPLOAD_SYNCED,
                mapOf(AnalyticsEvents.Params.KIND to slot.analyticsKind()),
            )
        }
        recomputeCanComplete()
    }

    private fun trackReuploadTapped(slot: ProofSlot) {
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_REUPLOAD_TAPPED,
            mapOf(AnalyticsEvents.Params.KIND to slot.analyticsKind()),
        )
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedDistributionStatus.SYNCED || r.status == FeedDistributionStatus.QUEUED } ?: false
            it.copy(
                canComplete = it.feedWeightPhotoStatus == FeedDistributionProofStatus.SYNCED &&
                    it.videoStatus == FeedDistributionProofStatus.SYNCED &&
                    it.waterVideoStatus == FeedDistributionProofStatus.SYNCED &&
                    !committed,
            )
        }
    }

    private fun feedVideoContext(): ProofCaptureContext = proofContext(
        title = "Record feed video",
    )

    private fun waterVideoContext(): ProofCaptureContext = proofContext(
        title = "Record water video",
    )

    private fun proofContext(title: String): ProofCaptureContext = ProofCaptureContext(
        title = title,
        primaryTag = shedLabel.ifBlank { shedId },
        workLabel = listOf(sessionLabel, workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() })
            .filter { it.isNotBlank() }
            .joinToString(" · "),
        headerTitle = title,
    )

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
        const val ARG_PARTITION_LABEL = "partition_label"
        const val ARG_LIFECYCLE_STATUS = "lifecycle_status"

        private const val KEY_COMPLETE_IDEMPOTENCY = "feedDistribution.completeKey"
        private const val KEY_FEED_WEIGHT_PHOTO_IDEMPOTENCY = "feedDistribution.feedWeightPhotoKey"
        private const val KEY_VIDEO_IDEMPOTENCY = "feedDistribution.videoKey"
        private const val KEY_WATER_VIDEO_IDEMPOTENCY = "feedDistribution.waterVideoKey"
        private const val KEY_OUTBOX_ITEM_ID = "feedDistribution.outboxItemId"
        private const val KEY_FEED_WEIGHT_PHOTO_PROOF_ITEM_ID = "feedDistribution.feedWeightPhotoProofItemId"
        private const val KEY_VIDEO_PROOF_ITEM_ID = "feedDistribution.videoProofItemId"
        private const val KEY_WATER_VIDEO_PROOF_ITEM_ID = "feedDistribution.waterVideoProofItemId"
        private const val FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO = "feed_distribution_feed_weight_photo"
        private const val FIELD_FEED_DISTRIBUTION_VIDEO = "feed_distribution_video"
        private const val FIELD_FEED_DISTRIBUTION_WATER_VIDEO = "feed_distribution_water_video"
        private const val QUEUED_MESSAGE = "Sent for verification. A verifier will review the three proofs."
        private const val SYNCED_MESSAGE = "Sent. Waiting for verifier approval before this feeding is counted."
        private const val VIDEO_QUEUED = "Feed video saved on this phone. It will upload automatically."
        private const val PROOF_QUEUED = "Proof saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Proof upload is in progress."
        private const val PROOF_SYNCED = "Proof is ready."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }

    private enum class ProofSlot { FEED_WEIGHT_PHOTO, FEED_VIDEO, WATER_VIDEO }

    private fun ProofSlot.analyticsKind(): String = when (this) {
        ProofSlot.FEED_WEIGHT_PHOTO -> "feed_weight_photo"
        ProofSlot.FEED_VIDEO -> "feed_video"
        ProofSlot.WATER_VIDEO -> "water_video"
    }
}
