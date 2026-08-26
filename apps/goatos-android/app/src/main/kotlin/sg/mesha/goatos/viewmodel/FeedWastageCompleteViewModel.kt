package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
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
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedWastageCompleteEvent
import sg.mesha.goatos.feature.feed.FeedWastageCompleteResultUi
import sg.mesha.goatos.feature.feed.FeedWastageCompleteStatus
import sg.mesha.goatos.feature.feed.FeedWastageCompleteUiState
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import javax.inject.Inject

/**
 * The feed-WASTAGE completion detail (`/feed/wastage/complete/...`) — the verifier-GATED wastage
 * flow (maintainer decision 2026-08-18). Opened by tapping a pen row on Feed Wastage.
 *
 * Same single-proof shape as [FeedPackingCompleteViewModel], one grain simpler: the completion is
 * a PEN-DAY (no session, no workflow — the server stamps `experiment`). Two offline-first writes:
 *  - a MANDATORY leftover-feed VIDEO ([ProofCaptureRepository.captureReplacingLatest] ->
 *    PROOF_UPLOAD, scope=shed);
 *  - **Submit** ([SyncRepository.enqueueFeedWastageComplete]) — carries the proof outbox item id
 *    so the dispatcher resolves the uploaded proof_id and sends it; the pen-day flips to
 *    `pending_verification` and NOTHING is completed until a verifier approves.
 *
 * Both enqueue on the SAME outbox group (the pen-day), so the video drains strictly before the
 * completion. A pen-day accepts exactly ONE video: a second, DIFFERENT one is a 409 the drain
 * surfaces as terminal — the operator sees the server's own sentence, never a silent retry loop.
 */
@HiltViewModel
class FeedWastageCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val drafts: CaptureDraftRepository,
    private val feedRepository: FeedRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    // "-" is the blank-park sentinel the route helper uses; it maps back to "" so the backend
    // resolves the default park.
    private val parkId: String = savedStateHandle.get<String>(ARG_PARK_ID)?.takeIf { it != "-" }.orEmpty()
    private val shedId: String = savedStateHandle.get<String>(ARG_SHED_ID).orEmpty()
    private val targetDate: String = savedStateHandle.get<String>(ARG_TARGET_DATE).orEmpty()
    private val shedLabel: String = savedStateHandle.get<String>(ARG_SHED_LABEL).orEmpty()
    private val parkLabel: String = savedStateHandle.get<String>(ARG_PARK_LABEL).orEmpty()
    private val partitionLabel: String = savedStateHandle.get<String>(ARG_PARTITION_LABEL).orEmpty()
    private val experimentArm: String = savedStateHandle.get<String>(ARG_EXPERIMENT_ARM).orEmpty()
    private val captureAllowed: Boolean =
        savedStateHandle.get<String>(ARG_CAPTURE_ALLOWED)?.toBooleanStrictOrNull() ?: false

    // First-paint hint only; the Room-backed live status below supersedes it the moment Room has
    // one (see FeedPackingCompleteViewModel's identical shape for the STG 2026-08-09 rationale).
    private val lifecycleStatusHint: String = savedStateHandle.get<String>(ARG_LIFECYCLE_STATUS).orEmpty()
    private val alreadySubmitted: Boolean =
        !captureAllowed || !feedSessionCanCapture(lifecycleStatusHint, isToday = true)

    // The day-shed-PEN identity for BOTH the proof AND the completion, so the proof drains
    // strictly before the gated completion that references it. Session 0 is a real value here —
    // the wastage grain HAS no session — and the constant workflow keeps the key stable.
    private val groupKey =
        feedCaptureGroupKey("feed-wastage", shedId, partitionLabel, 0, WASTAGE_WORKFLOW, targetDate)

    private var videoProofRowId: String? = null

    /** Durable per pen-day; see the shared store's kdoc for why SavedStateHandle lost the clip. */
    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(
        FeedWastageCompleteUiState(
            shedLabel = shedLabel,
            experimentArm = experimentArm,
            alreadySubmitted = alreadySubmitted,
        ),
    )
    val state: StateFlow<FeedWastageCompleteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var videoStatusJob: Job? = null
    private var syncStatusJob: Job? = null
    private var submitInFlight = false

    init {
        analytics.track(AnalyticsEvents.FEED_WASTAGE_COMPLETE_OPENED, wastageEventProps(ACTION_DETAIL_OPENED))
        viewModelScope.launch {
            draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
            _state.update { it.copy(videoCaptured = draft.hasProof(STEP_VIDEO)) }
            draft.proofs[STEP_VIDEO]?.let(::observeProofItem)
            recomputeCanComplete()
            draft.submitOutboxItemId?.let(::observeOutboxItem)
        }
        observeSyncStatus()
        observeDurableProof()
        observeLiveLifecycleStatus()
    }

    /** Supersede the nav-arg hint with the LIVE Room-backed status — the same table the worklist
     *  renders from — plus a bounded server poll for a teammate's submit on another phone. */
    private fun observeLiveLifecycleStatus() {
        viewModelScope.launch {
            feedRepository.observeWastageRowStatus(shedId, partitionLabel, WASTAGE_WORKFLOW)
                .collect { liveStatus -> applyLiveStatus(liveStatus, fromRoom = true) }
        }
        startServerStatusPolling()
    }

    /** `null` (no answer yet / poll failed) is ignored: never flip editable -> locked on a guess,
     *  and never flip locked -> editable at all. */
    private fun applyLiveStatus(liveStatus: String?, fromRoom: Boolean) {
        if (liveStatus == null) return
        _state.update { it.copy(alreadySubmitted = !captureAllowed || !feedSessionCanCapture(liveStatus, isToday = true)) }
        if (!fromRoom) {
            viewModelScope.launch {
                feedRepository.persistWastageRowStatus(shedId, partitionLabel, WASTAGE_WORKFLOW, liveStatus)
            }
        }
    }

    /** Bounded periodic server read — see [FeedPackingCompleteViewModel.startServerStatusPolling]
     *  for why this is bounded rather than a bare `while (isActive)`. */
    private fun startServerStatusPolling() {
        viewModelScope.launch {
            repeat(MAX_SERVER_STATUS_POLLS) {
                delay(SERVER_STATUS_POLL_INTERVAL_MS)
                pollServerStatusOnce()
            }
        }
    }

    private suspend fun pollServerStatusOnce() {
        if (shedId.isBlank() || targetDate.isBlank()) return
        val status = runCatching { // exception:exempt expected poll failure (offline/timeout/5xx); null-is-unknown is the contract, not an error to record
            feedRepository.fetchWastageRowStatus(parkId, shedId, partitionLabel, targetDate)
        }.getOrNull()
        applyLiveStatus(status, fromRoom = false)
    }

    fun onEvent(event: FeedWastageCompleteEvent) {
        when (event) {
            FeedWastageCompleteEvent.RecordWastageVideo -> captureWastageVideo()
            // Re-record runs the camera FIRST and drops the old take's queued upload only once new
            // media exists (captureReplacingLatest) — discarding up front deleted a good clip
            // whenever the operator cancelled or the camera failed.
            FeedWastageCompleteEvent.ReRecordWastageVideo -> captureWastageVideo(replacing = true)
            FeedWastageCompleteEvent.MarkDone -> markDone()
            FeedWastageCompleteEvent.SyncNow -> syncNow()
            FeedWastageCompleteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /** MANDATORY leftover-feed video — a LIVE in-app camera clip, enqueued as a PROOF_UPLOAD on
     *  the pen-day group so it drains before the completion. Camera-only; no gallery. */
    private fun captureWastageVideo(replacing: Boolean = false) {
        if (_state.value.isCapturingVideo || _state.value.alreadySubmitted || shedId.isBlank()) return
        if (!replacing && _state.value.videoCaptured) return
        analytics.track(
            AnalyticsEvents.FEED_WASTAGE_COMPLETE_OPENED,
            wastageEventProps(if (replacing) ACTION_RE_RECORD_VIDEO else ACTION_RECORD_VIDEO),
        )
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = feedWastageProofCaption(),
                        primaryTag = shedLabel.ifBlank { shedId },
                        workLabel = experimentArm,
                        prompt = ProofCapturePrompt.FEED_WASTAGE,
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed wastage video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            val slot = EvidenceSlot(
                identity = buildFeedEvidenceSlotIdentity("feed-wastage", shedId, partitionLabel, 0, WASTAGE_WORKFLOW, targetDate),
                fieldKey = FIELD_FEED_WASTAGE_VIDEO,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = feedWastageProofCaption(),
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
                    videoProofRowId = result.value.id
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        _state.update { it.copy(isCapturingVideo = false, videoCaptured = false, videoMessage = PROOF_FAILED) }
                        return@launch
                    }
                    drafts.putProof(CaptureFlow.FEED_WASTAGE, groupKey, STEP_VIDEO, proofOutboxId)
                    draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
                    observeProofItem(proofOutboxId)
                    analytics.track(
                        AnalyticsEvents.FEED_WASTAGE_VIDEO_CAPTURED,
                        wastageEventProps(ACTION_CAPTURED) +
                            (AnalyticsEvents.Params.PROOF_ID to result.value.id) +
                            (PARAM_OUTBOX_ITEM_ID to proofOutboxId),
                    )
                    _state.update { it.copy(isCapturingVideo = false, videoCaptured = true, videoMessage = VIDEO_QUEUED) }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed wastage video enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_WASTAGE_COMPLETE_FAILURE,
                        wastageEventProps(ACTION_CAPTURE_FAILED) +
                            (AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update { it.copy(isCapturingVideo = false, videoCaptured = false, videoMessage = PROOF_FAILED) }
                }
            }
        }
    }

    private fun observeProofItem(itemId: String) {
        videoStatusJob?.cancel()
        videoStatusJob = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect(::updateProofStatus)
        }
    }

    private fun observeDurableProof() {
        viewModelScope.launch {
            // No partitionLabel: [groupKey] ALREADY carries the pen (feedCaptureGroupKey embeds
            // partitionMatchToken), so this read is pen-scoped by the task id alone — keep read
            // and write symmetric (see FeedPackingCompleteViewModel.observeDurableProof's kdoc).
            proofCaptureRepository.observeProofs(groupKey)
                .collect { rows ->
                    val row = rows
                        .filter { it.fieldKey == FIELD_FEED_WASTAGE_VIDEO && it.syncStatus != CaptureSyncStatus.FAILED }
                        .maxByOrNull { it.capturedAtMs }
                        ?: return@collect
                    hydrateFromProof(row)
                    recomputeCanComplete()
                }
        }
    }

    private suspend fun hydrateFromProof(row: ProofCaptureRow) {
        videoProofRowId = row.id
        row.outboxItemId?.takeIf { it.isNotBlank() }?.let { outboxId ->
            if (draft.proofs[STEP_VIDEO] != outboxId) {
                drafts.putProof(CaptureFlow.FEED_WASTAGE, groupKey, STEP_VIDEO, outboxId)
                draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
            }
            observeProofItem(outboxId)
        }
        val status = row.toProofStatus()
        _state.update {
            it.copy(
                videoCaptured = true,
                videoPreviewPath = row.previewUri() ?: it.videoPreviewPath,
                videoStatus = status,
                videoMessage = row.toProofMessage(status, VIDEO_QUEUED, PROOF_UPLOADING, PROOF_SYNCED, PROOF_FAILED),
            )
        }
    }

    private fun updateProofStatus(item: SyncQueueItem) {
        val proofStatus = when (item.status) {
            SyncItemStatus.QUEUED -> FeedDistributionProofStatus.QUEUED
            SyncItemStatus.IN_FLIGHT -> FeedDistributionProofStatus.UPLOADING
            SyncItemStatus.SUCCEEDED -> FeedDistributionProofStatus.SYNCED
            SyncItemStatus.FAILED -> FeedDistributionProofStatus.FAILED
        }
        val message = when (proofStatus) {
            FeedDistributionProofStatus.QUEUED -> VIDEO_QUEUED
            FeedDistributionProofStatus.UPLOADING -> PROOF_UPLOADING
            FeedDistributionProofStatus.SYNCED -> PROOF_SYNCED
            FeedDistributionProofStatus.FAILED -> item.lastError ?: PROOF_FAILED
            FeedDistributionProofStatus.EMPTY -> null
        }
        _state.update {
            it.copy(
                videoCaptured = it.videoCaptured || item.localFilePath != null,
                videoPreviewPath = item.localFilePath ?: it.videoPreviewPath,
                videoStatus = proofStatus,
                videoMessage = message,
            )
        }
        recomputeCanComplete()
    }

    private fun markDone() {
        val current = _state.value
        val videoItem = draft.proofs[STEP_VIDEO]
        // Defense in depth alongside the UI gate, plus the submitInFlight latch so a second tap in
        // the async gap cannot enqueue a second write (same idiom as FeedPackingCompleteViewModel).
        if (submitInFlight || !current.submitEnabled || videoItem.isNullOrBlank()) {
            _state.update { it.copy(canComplete = false, videoMessage = NEED_VIDEO_MESSAGE) }
            return
        }
        submitInFlight = true
        viewModelScope.launch {
            // Stable for the selected proof, fresh when the operator re-records: a retry of the
            // same video must replay; a replacement video must not collide with the old payload —
            // the backend answers the DIFFERENT-video case with a terminal 409 it words itself.
            val completeIdempotencyKey = "feed-wastage-complete:$groupKey:$videoItem"
            if (draft.submitIdempotencyKey != completeIdempotencyKey) {
                drafts.putSubmit(CaptureFlow.FEED_WASTAGE, groupKey, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
            }
            when (
                val result = syncRepository.enqueueFeedWastageComplete(
                    groupKey = groupKey,
                    idempotencyKey = completeIdempotencyKey,
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    targetDate = targetDate,
                    wastageProofOutboxItemId = videoItem,
                )
            ) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.FEED_WASTAGE, groupKey, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
                    observeOutboxItem(result.value)
                    analytics.track(
                        AnalyticsEvents.FEED_WASTAGE_SUBMITTED,
                        wastageEventProps(ACTION_SUBMIT) +
                            (PARAM_PROOF_OUTBOX_ITEM_ID to videoItem) +
                            (PARAM_OUTBOX_ITEM_ID to result.value),
                    )
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    submitInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "feed wastage complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_WASTAGE_COMPLETE_FAILURE,
                        wastageEventProps(ACTION_SUBMIT_FAILED) +
                            (AnalyticsEvents.Params.REASON to result.message),
                    )
                    _state.update {
                        it.copy(result = FeedWastageCompleteResultUi(FeedWastageCompleteStatus.FAILED, result.message), canComplete = true)
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
                    // Reset the latch on terminal FAILED so the operator can retry — including the
                    // 409 different-video conflict, whose server sentence renders verbatim below.
                    if (item.status == SyncItemStatus.FAILED) {
                        submitInFlight = false
                    }
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(
                            result = FeedWastageCompleteResultUi(writeResult.status.toWastageStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && wastageProofReadyForSubmit(it),
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedWastageCompleteStatus.SYNCED || r.status == FeedWastageCompleteStatus.QUEUED } ?: false
            it.copy(canComplete = wastageProofReadyForSubmit(it) && !committed)
        }
    }

    private fun wastageProofReadyForSubmit(state: FeedWastageCompleteUiState): Boolean =
        state.videoCaptured && state.videoStatus.isQueuedForSubmit()

    private fun syncNow() {
        viewModelScope.launch {
            syncRepository.triggerDrain()
            // Same "did a teammate already submit this" answer the periodic poll provides.
            pollServerStatusOnce()
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

    private fun sg.mesha.goatos.feature.counts.CountsWriteStatus.toWastageStatus(): FeedWastageCompleteStatus = when (this) {
        sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED -> FeedWastageCompleteStatus.SYNCED
        sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED -> FeedWastageCompleteStatus.FAILED
        else -> FeedWastageCompleteStatus.QUEUED
    }

    private fun feedWastageProofCaption(): String =
        proofOverlayContextLine(
            feature = "Feed wastage",
            parkLabel = parkLabel.ifBlank { parkId },
            locationLabel = shedLabel.ifBlank { shedId },
            extraLabel = listOf(experimentArm, partitionLabel).filter { it.isNotBlank() }.joinToString(" . "),
        )

    private fun wastageEventProps(action: String): Map<String, String> =
        mapOf(
            AnalyticsEvents.Params.SOURCE to SCREEN_FEED_WASTAGE_DETAIL,
            AnalyticsEvents.Params.KIND to KIND_WASTAGE,
            AnalyticsEvents.Params.ACTION to action,
            AnalyticsEvents.Params.SHED_ID to shedId,
            AnalyticsEvents.Params.PARTITION_LABEL to partitionLabel,
            AnalyticsEvents.Params.FIELD to FIELD_FEED_WASTAGE_VIDEO,
            PARAM_GROUP_KEY to groupKey,
        )

    companion object {
        const val ARG_PARK_ID = "park_id"
        const val ARG_SHED_ID = "shed_id"
        const val ARG_TARGET_DATE = "target_date"
        const val ARG_SHED_LABEL = "shed_label"
        const val ARG_PARK_LABEL = "park_label"
        const val ARG_EXPERIMENT_ARM = "experiment_arm"
        const val ARG_CAPTURE_ALLOWED = "capture_allowed"

        /** The PEN worked. Part of the completion's identity: a partitioned shed has one wastage
         *  task PER PEN, so dropping it would let one pen's video close out its siblings. */
        const val ARG_PARTITION_LABEL = "partition_label"
        const val ARG_LIFECYCLE_STATUS = "lifecycle_status"

        /** Wastage exists only on experiment pens; the server stamps the workflow, and the local
         *  capture/group keys pin the same constant so the Room grain matches the worklist's. */
        private const val WASTAGE_WORKFLOW = "experiment"

        private const val SERVER_STATUS_POLL_INTERVAL_MS = 30_000L
        private const val MAX_SERVER_STATUS_POLLS = 2_880
        private const val KIND_WASTAGE = "wastage"
        private const val SCREEN_FEED_WASTAGE_DETAIL = "feed_wastage_complete"
        private const val ACTION_DETAIL_OPENED = "detail_opened"
        private const val ACTION_RECORD_VIDEO = "record_video"
        private const val ACTION_RE_RECORD_VIDEO = "re_record_video"
        private const val ACTION_CAPTURED = "captured"
        private const val ACTION_CAPTURE_FAILED = "capture_failed"
        private const val ACTION_SUBMIT = "submit"
        private const val ACTION_SUBMIT_FAILED = "submit_failed"
        private const val PARAM_GROUP_KEY = "group_key"
        private const val PARAM_OUTBOX_ITEM_ID = "outbox_item_id"
        private const val PARAM_PROOF_OUTBOX_ITEM_ID = "proof_outbox_item_id"

        /** Draft step name in the shared capture-draft store. */
        private const val STEP_VIDEO = "video"
        private const val FIELD_FEED_WASTAGE_VIDEO = "feed_wastage_video"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the leftover feed video."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this is counted."
        private const val VIDEO_QUEUED = "Leftover feed video saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Leftover feed video upload is in progress."
        private const val PROOF_SYNCED = "Leftover feed video is ready."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
        private const val NEED_VIDEO_MESSAGE = "Record the leftover feed video before submitting."
    }
}
