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
import kotlinx.coroutines.flow.first
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
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedDistributionResultUi
import sg.mesha.goatos.feature.feed.FeedDistributionStatus
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import java.util.Locale
import java.util.EnumSet
import java.util.UUID
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
    private val feedRepository: FeedRepository,
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
    private val parkLabel: String = savedStateHandle.get<String>(ARG_PARK_LABEL).orEmpty()
    private val partitionLabel: String = savedStateHandle.get<String>(ARG_PARTITION_LABEL).orEmpty()

    // The row's backend-owned lifecycle bucket AT THE MOMENT the row was tapped — a FIRST-PAINT hint
    // only. [observeLiveLifecycleStatus] supersedes it with the Room-backed live value the moment
    // Room has one, so a status change while this screen stays open (verifier decides elsewhere, or
    // a reinstall lost the local draft for work already submitted) flips this screen to read-only
    // live rather than on next entry only. Mirrors FeedPackingCompleteViewModel's same-shaped gate
    // (STG 2026-08-09).
    private val lifecycleStatusHint: String = savedStateHandle.get<String>(ARG_LIFECYCLE_STATUS).orEmpty()
    private val alreadySubmitted: Boolean = !feedSessionCanCapture(lifecycleStatusHint, isToday = true)

    // The shed-session partitions ordering for the proof uploads and completion, so all three
    // proof items drain before the gated completion references them.
    private val groupKey = feedCaptureGroupKey("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate)

    private val feedWeightPhotoKey = DraftIdempotencyKey(savedStateHandle, KEY_FEED_WEIGHT_PHOTO_IDEMPOTENCY, "feed-distribution-feed-weight-photo")
    private val videoKey = DraftIdempotencyKey(savedStateHandle, KEY_VIDEO_IDEMPOTENCY, "feed-distribution-video")
    private val waterVideoKey = DraftIdempotencyKey(savedStateHandle, KEY_WATER_VIDEO_IDEMPOTENCY, "feed-distribution-water-video")
    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)
    private val feedWeightPhotoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_FEED_WEIGHT_PHOTO_PROOF_ITEM_ID)
    private val videoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_VIDEO_PROOF_ITEM_ID)
    private val waterVideoProofItemId = DraftOutboxItemId(savedStateHandle, KEY_WATER_VIDEO_PROOF_ITEM_ID)
    private val feedWeightPhotoProofRowId = DraftOutboxItemId(savedStateHandle, KEY_FEED_WEIGHT_PHOTO_PROOF_ROW_ID)
    private val videoProofRowId = DraftOutboxItemId(savedStateHandle, KEY_VIDEO_PROOF_ROW_ID)
    private val waterVideoProofRowId = DraftOutboxItemId(savedStateHandle, KEY_WATER_VIDEO_PROOF_ROW_ID)

    // SERVER proof ids for slots ANOTHER operator recorded. A pen-session's three proofs may be
    // split across three phones (maintainer decision 2026-08-14); a slot shot elsewhere has no local
    // outbox row here, so its server id is what this phone submits with.
    private val feedWeightRemoteRef = DraftOutboxItemId(savedStateHandle, KEY_FEED_WEIGHT_REMOTE_REF)
    private val videoRemoteRef = DraftOutboxItemId(savedStateHandle, KEY_VIDEO_REMOTE_REF)
    private val waterVideoRemoteRef = DraftOutboxItemId(savedStateHandle, KEY_WATER_VIDEO_REMOTE_REF)
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
            alreadySubmitted = alreadySubmitted,
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
        observeDurableProofs()
        refreshTeammateCaptures()
        outboxItemId.value?.let(::observeOutboxItem)
        feedWeightPhotoProofItemId.value?.let { observeProofItem(ProofSlot.FEED_WEIGHT_PHOTO, it) }
        videoProofItemId.value?.let { observeProofItem(ProofSlot.FEED_VIDEO, it) }
        waterVideoProofItemId.value?.let { observeProofItem(ProofSlot.WATER_VIDEO, it) }
        observeLiveLifecycleStatus()
    }

    /** See [FeedPackingCompleteViewModel.observeLiveLifecycleStatus] — the same shaped gate for the
     *  feed-direction shed-session table. A `null` emission (no cached row yet) is ignored so the
     *  screen keeps [lifecycleStatusHint] rather than forcing itself editable. */
    private fun observeLiveLifecycleStatus() {
        viewModelScope.launch {
            feedRepository.observeDirectionSessionStatus(shedId, partitionLabel, workflow, sessionNo)
                .collect { liveStatus ->
                    if (liveStatus == null) return@collect
                    _state.update { it.copy(alreadySubmitted = !feedSessionCanCapture(liveStatus, isToday = true)) }
                }
        }
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
        _state.update {
            it.copy(
                isCapturingFeedWeightPhoto = true,
                feedWeightPhotoMessage = null,
                feedWeightPhotoStatus = FeedDistributionProofStatus.QUEUED,
                feedWeightPhotoPreviewPath = if (replacing) null else it.feedWeightPhotoPreviewPath,
            )
        }
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
            // Build the evidence slot for re-capture. captureReplacingLatest ensures
            // the old row is only removed after the new capture succeeds (Manohar ordering).
            val slot = EvidenceSlot(
                identity = buildFeedEvidenceSlotIdentity("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate),
                fieldKey = FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = feedProofCaption("Feed direction weight photo"),
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
                    feedWeightPhotoProofRowId.value = result.value.id
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
                    _state.update {
                        it.copy(
                            isCapturingFeedWeightPhoto = false,
                            feedWeightPhotoStatus = FeedDistributionProofStatus.FAILED,
                            feedWeightPhotoMessage = result.message,
                        )
                    }
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
        _state.update {
            it.copy(
                isCapturingVideo = true,
                videoMessage = null,
                videoStatus = FeedDistributionProofStatus.QUEUED,
                videoPreviewPath = if (replacing) null else it.videoPreviewPath,
            )
        }
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
            // Build the evidence slot for re-capture. captureReplacingLatest ensures
            // the old row is only removed after the new capture succeeds (Manohar ordering).
            val slot = EvidenceSlot(
                identity = buildFeedEvidenceSlotIdentity("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate),
                fieldKey = FIELD_FEED_DISTRIBUTION_VIDEO,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = feedProofCaption("Feed direction video"),
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
                    videoProofRowId.value = result.value.id
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
                    _state.update {
                        it.copy(
                            isCapturingVideo = false,
                            videoStatus = FeedDistributionProofStatus.FAILED,
                            videoMessage = result.message,
                        )
                    }
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
        _state.update {
            it.copy(
                isCapturingWaterVideo = true,
                waterVideoMessage = null,
                waterVideoStatus = FeedDistributionProofStatus.QUEUED,
                waterVideoPreviewPath = if (replacing) null else it.waterVideoPreviewPath,
            )
        }
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
            // Build the evidence slot for re-capture. captureReplacingLatest ensures
            // the old row is only removed after the new capture succeeds (Manohar ordering).
            val slot = EvidenceSlot(
                identity = buildFeedEvidenceSlotIdentity("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate),
                fieldKey = FIELD_FEED_DISTRIBUTION_WATER_VIDEO,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = feedProofCaption("Feed direction water video"),
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
                    waterVideoProofRowId.value = result.value.id
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
                    _state.update {
                        it.copy(
                            isCapturingWaterVideo = false,
                            waterVideoStatus = FeedDistributionProofStatus.FAILED,
                            waterVideoMessage = result.message,
                        )
                    }
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
        // A slot is satisfied by EITHER this phone's own upload or a teammate's server proof id. A
        // pen-session split across three operators leaves each phone holding one of three, so
        // demanding all three locally is what made a split pen unsubmittable at all.
        val feedWeightRemote = feedWeightRemoteRef.value
        val videoRemote = videoRemoteRef.value
        val waterVideoRemote = waterVideoRemoteRef.value
        if (
            !current.submitEnabled ||
            (feedWeightPhotoItem.isNullOrBlank() && feedWeightRemote.isNullOrBlank()) ||
            (videoItem.isNullOrBlank() && videoRemote.isNullOrBlank()) ||
            (waterVideoItem.isNullOrBlank() && waterVideoRemote.isNullOrBlank())
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
                    // Keyed on the proof SET, unchanged. Two operators submitting the same pen mint
                    // different keys, but they name the SAME three proofs, so the backend's
                    // shed-session natural key makes the second an idempotent no-op rather than a
                    // double write (a DIFFERENT proof set is what raises ErrDistributionAlreadyRecorded).
                    // Deliberately NOT re-keyed on pen identity alone: that would collide with the
                    // earlier successful submit and swallow a legitimate rework re-submit.
                    idempotencyKey = feedDistributionCompleteKey(
                        groupKey,
                        feedWeightPhotoItem ?: feedWeightRemote,
                        videoItem ?: videoRemote,
                        waterVideoItem ?: waterVideoRemote,
                    ),
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                    feedWeightProofOutboxItemId = feedWeightPhotoItem,
                    distributionProofOutboxItemId = videoItem,
                    waterProofOutboxItemId = waterVideoItem,
                    feedWeightProofRef = feedWeightRemote,
                    distributionProofRef = videoRemote,
                    waterProofRef = waterVideoRemote,
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
            // Manual sync must also re-fetch teammate/server proof slots: another operator may
            // have uploaded the missing captures while this screen is open, and draining the
            // local outbox alone leaves the slot display stale until back/reopen.
            refreshTeammateCaptures()
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
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
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

    private suspend fun discardExistingProof(slot: ProofSlot): Boolean {
        val rowIdState = when (slot) {
            ProofSlot.FEED_WEIGHT_PHOTO -> feedWeightPhotoProofRowId
            ProofSlot.FEED_VIDEO -> videoProofRowId
            ProofSlot.WATER_VIDEO -> waterVideoProofRowId
        }
        val outboxState = when (slot) {
            ProofSlot.FEED_WEIGHT_PHOTO -> feedWeightPhotoProofItemId
            ProofSlot.FEED_VIDEO -> videoProofItemId
            ProofSlot.WATER_VIDEO -> waterVideoProofItemId
        }
        val rowId = rowIdState.value ?: proofCaptureRepository
            .observeProofs(groupKey)
            .first()
            .firstOrNull { it.outboxItemId == outboxState.value }
            ?.id
        if (rowId.isNullOrBlank()) {
            outboxState.value = null
            clearProofRowId(slot)
            return true
        }
        return when (val removed = proofCaptureRepository.remove(groupKey, rowId)) {
            is AppResult.Ok -> {
                outboxState.value = null
                clearProofRowId(slot)
                true
            }
            is AppResult.Err -> {
                removed.cause?.let { crashReporter.recordException(it, "feed distribution proof discard failed") }
                analytics.track(
                    AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                    mapOf(AnalyticsEvents.Params.REASON to removed.message),
                )
                false
            }
        }
    }

    /**
     * Learns which slots ANOTHER operator already recorded for this pen-session.
     *
     * Three operators may split a pen-session's three proofs -- one shoots the weight photo, one the
     * feed video, one the water video (maintainer decision 2026-08-14). Without this read a proof was
     * visible only on the phone that shot it, so the others saw an empty form AND no phone held all
     * three references, which made the pen unsubmittable.
     *
     * Best effort by design: a failure leaves every remote ref null and the screen behaves exactly as
     * it did before this read existed. A slot this phone recorded ITSELF always wins -- local capture
     * state is never overwritten by the shared read.
     */
    private fun refreshTeammateCaptures() {
        if (shedId.isBlank() || workflow.isBlank() || targetDate.isBlank() || sessionNo < 1) return
        viewModelScope.launch {
            val slots = feedRepository.penSessionCaptures(
                FeedPenSessionCaptureQuery(
                    parkId = parkId,
                    shedId = shedId,
                    // The PEN. Without it the server answers for the shed and would claim a sibling
                    // pen's work as this one's.
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                ),
            )
            if (slots.isEmpty()) return@launch
            slots.forEach { slot ->
                when (slot.fieldKey) {
                    FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO ->
                        adoptTeammateCapture(ProofSlot.FEED_WEIGHT_PHOTO, feedWeightRemoteRef, slot.proofRef)
                    FIELD_FEED_DISTRIBUTION_VIDEO ->
                        adoptTeammateCapture(ProofSlot.FEED_VIDEO, videoRemoteRef, slot.proofRef)
                    FIELD_FEED_DISTRIBUTION_WATER_VIDEO ->
                        adoptTeammateCapture(ProofSlot.WATER_VIDEO, waterVideoRemoteRef, slot.proofRef)
                }
            }
            recomputeCanComplete()
        }
    }

    /**
     * Marks a slot satisfied by a proof recorded on another phone.
     *
     * Skipped entirely when this phone holds its OWN proof for the slot: the operator's own capture
     * is the one they can re-record, and replacing it with a teammate's reference would silently
     * discard their work. The status is SYNCED because the proof is already durable server-side --
     * that is exactly what makes it submittable -- and there is no preview, because this screen
     * deliberately never shows another operator's media.
     */
    private fun adoptTeammateCapture(slot: ProofSlot, remoteRef: DraftOutboxItemId, proofRef: String) {
        if (proofRef.isBlank()) return
        val locallyCaptured = when (slot) {
            ProofSlot.FEED_WEIGHT_PHOTO -> feedWeightPhotoProofItemId.value != null
            ProofSlot.FEED_VIDEO -> videoProofItemId.value != null
            ProofSlot.WATER_VIDEO -> waterVideoProofItemId.value != null
        }
        if (locallyCaptured) return
        remoteRef.value = proofRef
        _state.update {
            when (slot) {
                ProofSlot.FEED_WEIGHT_PHOTO -> it.copy(
                    feedWeightPhotoCaptured = true,
                    feedWeightPhotoStatus = FeedDistributionProofStatus.SYNCED,
                    feedWeightPhotoMessage = PROOF_RECORDED_BY_TEAMMATE,
                )
                ProofSlot.FEED_VIDEO -> it.copy(
                    videoCaptured = true,
                    videoStatus = FeedDistributionProofStatus.SYNCED,
                    videoMessage = PROOF_RECORDED_BY_TEAMMATE,
                )
                ProofSlot.WATER_VIDEO -> it.copy(
                    waterVideoCaptured = true,
                    waterVideoStatus = FeedDistributionProofStatus.SYNCED,
                    waterVideoMessage = PROOF_RECORDED_BY_TEAMMATE,
                )
            }
        }
    }

    private fun clearProofRowId(slot: ProofSlot) {
        when (slot) {
            ProofSlot.FEED_WEIGHT_PHOTO -> feedWeightPhotoProofRowId.value = null
            ProofSlot.FEED_VIDEO -> videoProofRowId.value = null
            ProofSlot.WATER_VIDEO -> waterVideoProofRowId.value = null
        }
    }

    private fun observeDurableProofs() {
        viewModelScope.launch {
            // No partitionLabel: [groupKey] ALREADY carries the pen (feedCaptureGroupKey embeds
            // partitionMatchToken), so this read is pen-scoped by the task id alone. Passing the
            // label as well filtered on proof_capture.partitionKey, which capture() writes as
            // "whole" because the feed capture calls do not pass a label — so on a partitioned
            // shed the read asked for "3" while the row said "whole" and rehydration silently
            // returned nothing. Re-entering the screen showed an empty form for a video that was
            // sitting in Room and already uploading. Keep read and write symmetric (feed transport
            // and weighing omit it on both sides too); do not "restore" the label on one side only.
            proofCaptureRepository.observeProofs(groupKey)
                .collect { rows ->
                    hydrateSlotFromProof(ProofSlot.FEED_WEIGHT_PHOTO, rows.latestFor(FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO))
                    hydrateSlotFromProof(ProofSlot.FEED_VIDEO, rows.latestFor(FIELD_FEED_DISTRIBUTION_VIDEO))
                    hydrateSlotFromProof(ProofSlot.WATER_VIDEO, rows.latestFor(FIELD_FEED_DISTRIBUTION_WATER_VIDEO))
                    recomputeCanComplete()
                }
        }
    }

    private fun hydrateSlotFromProof(slot: ProofSlot, row: ProofCaptureRow?) {
        if (row == null) return
        row.outboxItemId?.takeIf { it.isNotBlank() }?.let { outboxId ->
            when (slot) {
                ProofSlot.FEED_WEIGHT_PHOTO -> if (feedWeightPhotoProofItemId.value != outboxId) {
                    feedWeightPhotoProofItemId.value = outboxId
                    observeProofItem(slot, outboxId)
                }
                ProofSlot.FEED_VIDEO -> if (videoProofItemId.value != outboxId) {
                    videoProofItemId.value = outboxId
                    observeProofItem(slot, outboxId)
                }
                ProofSlot.WATER_VIDEO -> if (waterVideoProofItemId.value != outboxId) {
                    waterVideoProofItemId.value = outboxId
                    observeProofItem(slot, outboxId)
                }
            }
        }
        when (slot) {
            ProofSlot.FEED_WEIGHT_PHOTO -> feedWeightPhotoProofRowId.value = row.id
            ProofSlot.FEED_VIDEO -> videoProofRowId.value = row.id
            ProofSlot.WATER_VIDEO -> waterVideoProofRowId.value = row.id
        }
        val status = row.toProofStatus()
        val preview = row.previewUri()
        _state.update {
            when (slot) {
                ProofSlot.FEED_WEIGHT_PHOTO -> it.copy(
                    feedWeightPhotoCaptured = true,
                    feedWeightPhotoPreviewPath = preview ?: it.feedWeightPhotoPreviewPath,
                    feedWeightPhotoStatus = status,
                    feedWeightPhotoMessage = row.toProofMessage(status, PROOF_QUEUED, PROOF_UPLOADING, PROOF_SYNCED, PROOF_FAILED),
                )
                ProofSlot.FEED_VIDEO -> it.copy(
                    videoCaptured = true,
                    videoPreviewPath = preview ?: it.videoPreviewPath,
                    videoStatus = status,
                    videoMessage = row.toProofMessage(status, PROOF_QUEUED, PROOF_UPLOADING, PROOF_SYNCED, PROOF_FAILED),
                )
                ProofSlot.WATER_VIDEO -> it.copy(
                    waterVideoCaptured = true,
                    waterVideoPreviewPath = preview ?: it.waterVideoPreviewPath,
                    waterVideoStatus = status,
                    waterVideoMessage = row.toProofMessage(status, PROOF_QUEUED, PROOF_UPLOADING, PROOF_SYNCED, PROOF_FAILED),
                )
            }
        }
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
                    feedWeightPhotoPreviewPath = item.localFilePath ?: it.feedWeightPhotoPreviewPath,
                    feedWeightPhotoStatus = proofStatus,
                    feedWeightPhotoMessage = message,
                )
                ProofSlot.FEED_VIDEO -> it.copy(
                    videoCaptured = it.videoCaptured || item.localFilePath != null,
                    videoPreviewPath = item.localFilePath ?: it.videoPreviewPath,
                    videoStatus = proofStatus,
                    videoMessage = message,
                )
                ProofSlot.WATER_VIDEO -> it.copy(
                    waterVideoCaptured = it.waterVideoCaptured || item.localFilePath != null,
                    waterVideoPreviewPath = item.localFilePath ?: it.waterVideoPreviewPath,
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

    private fun List<ProofCaptureRow>.latestFor(fieldKey: String): ProofCaptureRow? =
        filter { it.fieldKey == fieldKey && it.syncStatus != CaptureSyncStatus.FAILED }
            .maxByOrNull { it.capturedAtMs }

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
                canComplete = it.feedWeightPhotoCaptured &&
                    it.videoCaptured &&
                    it.waterVideoCaptured &&
                    it.feedWeightPhotoStatus.isQueuedForSubmit() &&
                    it.videoStatus.isQueuedForSubmit() &&
                    it.waterVideoStatus.isQueuedForSubmit() &&
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

    /**
     * The submit key, derived from the pen-session plus the identity of each proof in the set.
     *
     * Each slot contributes whichever reference this phone holds: its own outbox id, or the SERVER
     * proof id of a proof a teammate recorded. Callers have already established every slot has one,
     * so the nullable parameters are a type accommodation, not an optional set.
     */
    private fun feedDistributionCompleteKey(
        groupKey: String,
        feedWeightPhotoItem: String?,
        videoItem: String?,
        waterVideoItem: String?,
    ): String {
        val canonical = listOf(groupKey, feedWeightPhotoItem.orEmpty(), videoItem.orEmpty(), waterVideoItem.orEmpty())
            .joinToString("|")
        return "feed-distribution-complete:" + UUID.nameUUIDFromBytes(canonical.toByteArray()).toString()
    }

    private fun proofContext(title: String): ProofCaptureContext = ProofCaptureContext(
        title = title,
        primaryTag = shedLabel.ifBlank { shedId },
        workLabel = listOf(
            parkLabel.ifBlank { parkId },
            sessionLabel,
            workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
        )
            .filter { it.isNotBlank() }
            .joinToString(" · "),
        headerTitle = title,
    )

    private fun feedProofCaption(action: String): String =
        proofOverlayContextLine(
            feature = action,
            parkLabel = parkLabel.ifBlank { parkId },
            locationLabel = shedLabel.ifBlank { shedId },
            extraLabel = listOf(
                sessionLabel.ifBlank { "Session $sessionNo" },
                partitionLabel,
            ).filter { it.isNotBlank() }.joinToString(" . "),
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
        const val ARG_PARK_LABEL = "park_label"
        const val ARG_PARTITION_LABEL = "partition_label"
        const val ARG_LIFECYCLE_STATUS = "lifecycle_status"

        private const val KEY_FEED_WEIGHT_PHOTO_IDEMPOTENCY = "feedDistribution.feedWeightPhotoKey"
        private const val KEY_VIDEO_IDEMPOTENCY = "feedDistribution.videoKey"
        private const val KEY_WATER_VIDEO_IDEMPOTENCY = "feedDistribution.waterVideoKey"
        private const val KEY_OUTBOX_ITEM_ID = "feedDistribution.outboxItemId"
        private const val KEY_FEED_WEIGHT_PHOTO_PROOF_ITEM_ID = "feedDistribution.feedWeightPhotoProofItemId"
        private const val KEY_VIDEO_PROOF_ITEM_ID = "feedDistribution.videoProofItemId"
        private const val KEY_WATER_VIDEO_PROOF_ITEM_ID = "feedDistribution.waterVideoProofItemId"
        private const val KEY_FEED_WEIGHT_PHOTO_PROOF_ROW_ID = "feedDistribution.feedWeightPhotoProofRowId"
        private const val KEY_VIDEO_PROOF_ROW_ID = "feedDistribution.videoProofRowId"
        private const val KEY_WATER_VIDEO_PROOF_ROW_ID = "feedDistribution.waterVideoProofRowId"
        private const val KEY_FEED_WEIGHT_REMOTE_REF = "feedDistribution.feedWeightRemoteRef"
        private const val KEY_VIDEO_REMOTE_REF = "feedDistribution.videoRemoteRef"
        private const val KEY_WATER_VIDEO_REMOTE_REF = "feedDistribution.waterVideoRemoteRef"
        private const val FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO = "feed_distribution_feed_weight_photo"
        private const val FIELD_FEED_DISTRIBUTION_VIDEO = "feed_distribution_video"
        private const val FIELD_FEED_DISTRIBUTION_WATER_VIDEO = "feed_distribution_water_video"
        private const val QUEUED_MESSAGE = "Sent for verification. A verifier will review the three proofs."
        private const val SYNCED_MESSAGE = "Sent. Waiting for verifier approval before this feeding is counted."
        private const val VIDEO_QUEUED = "Feed video saved on this phone. It will upload automatically."
        private const val PROOF_QUEUED = "Proof saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Proof upload is in progress."
        private const val PROOF_SYNCED = "Proof is ready."
        private const val PROOF_RECORDED_BY_TEAMMATE = "Already recorded by another operator."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }

    private enum class ProofSlot { FEED_WEIGHT_PHOTO, FEED_VIDEO, WATER_VIDEO }

private fun ProofSlot.analyticsKind(): String = when (this) {
    ProofSlot.FEED_WEIGHT_PHOTO -> "feed_weight_photo"
    ProofSlot.FEED_VIDEO -> "feed_video"
    ProofSlot.WATER_VIDEO -> "water_video"
}

}
