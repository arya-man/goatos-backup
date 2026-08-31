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
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import sg.mesha.goatos.feature.feed.FeedPackingCompleteEvent
import sg.mesha.goatos.feature.feed.FeedPackingCompleteResultUi
import sg.mesha.goatos.feature.feed.FeedPackingCompleteStatus
import sg.mesha.goatos.feature.feed.FeedPackingCompleteUiState
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
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
    private val proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val drafts: CaptureDraftRepository,
    private val feedRepository: FeedRepository,
    private val feedCompletionStore: FeedCompletionLocalStore,
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

    // The row's backend-owned lifecycle bucket AT THE MOMENT the row was tapped. This is only the
    // FIRST-PAINT hint: it is a nav-arg snapshot, so a status change while this screen stays open
    // (verifier approves/rejects elsewhere, or a reinstall lost the local draft for work already
    // submitted) would leave it stale. [observeLiveLifecycleStatus] below supersedes it with the
    // Room-backed live value the moment Room has one (STG 2026-08-09).
    private val lifecycleStatusHint: String = savedStateHandle.get<String>(ARG_LIFECYCLE_STATUS).orEmpty()
    private val alreadySubmitted: Boolean = !feedSessionCanCapture(lifecycleStatusHint, isToday = true)

    // The day-shed-PEN-session partitions ordering for BOTH the proof AND the completion, so the
    // proof drains strictly before the gated completion that references it. The PEN, the SESSION and
    // the DAY are all part of this key: see feedCaptureGroupKey for what dropping any of them did to
    // the field.
    private val groupKey =
        feedCaptureGroupKey("feed-pack", shedId, partitionLabel, sessionNo, workflow, targetDate)

    private val videoKey = DraftIdempotencyKey(savedStateHandle, KEY_VIDEO_IDEMPOTENCY, "feed-packing-video")
    private var videoProofRowId: String? = null

    /** Durable per shed-session; see the shared store's kdoc for why SavedStateHandle lost the clip. */
    private var draft = CaptureDraft()

    private val _state = MutableStateFlow(
        FeedPackingCompleteUiState(
            shedLabel = shedLabel,
            sessionLabel = sessionLabel,
            workflowLabel = workflow.replaceFirstChar { if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString() },
            alreadySubmitted = alreadySubmitted,
        ),
    )
    val state: StateFlow<FeedPackingCompleteUiState> = _state.asStateFlow()

    private var statusJob: Job? = null
    private var videoStatusJob: Job? = null
    private var syncStatusJob: Job? = null
    private var submitInFlight = false

    init {
        analytics.track(AnalyticsEvents.FEED_PACKING_COMPLETE_OPENED, packingEventProps(ACTION_DETAIL_OPENED))
        viewModelScope.launch {
            draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
            _state.update { it.copy(videoCaptured = draft.hasProof(STEP_VIDEO)) }
            draft.proofs[STEP_VIDEO]?.let(::observeProofItem)
            recomputeCanComplete()
            draft.submitOutboxItemId?.let(::observeOutboxItem)
        }
        observeSyncStatus()
        observeDurableProof()
        observeLiveLifecycleStatus()
    }

    /**
     * Supersede the nav-arg [lifecycleStatusHint] with the LIVE Room-backed status the moment Room
     * has one for this pen-session — the same table the worklist renders from, so a status change
     * elsewhere (verifier approves/rejects, or another device submits) flips this screen to
     * read-only WHILE IT IS OPEN, not only on next entry. A `null` emission means Room has no cached
     * row for this session yet (e.g. offline-first cold start before any worklist page ever cached
     * it) and is deliberately IGNORED so the screen keeps the nav-arg hint rather than forcing itself
     * editable.
     */
    private fun observeLiveLifecycleStatus() {
        viewModelScope.launch {
            feedRepository.observePackingRowStatus(shedId, partitionLabel, workflow, sessionNo)
                .collect { liveStatus -> applyLiveStatus(liveStatus, fromRoom = true) }
        }
        startServerStatusPolling()
    }

    /** Shared by the Room-backed observer above and the SERVER poll below. `null` (no answer yet /
     *  poll failed) is ignored: this must never flip editable -> locked on a guess, and never flips
     *  locked -> editable at all. */
    private fun applyLiveStatus(liveStatus: String?, fromRoom: Boolean) {
        if (liveStatus == null) return
        _state.update { it.copy(alreadySubmitted = !feedSessionCanCapture(liveStatus, isToday = true)) }
        // Persist SERVER-sourced status into Room so the screen survives process death offline.
        // Room-sourced emissions are already in Room — writing them back fires a spurious
        // table-wide invalidation on every emission.
        if (!fromRoom) {
            viewModelScope.launch {
                feedRepository.persistPackingRowStatus(shedId, partitionLabel, workflow, sessionNo, liveStatus)
            }
        }
    }

    /**
     * Periodic SERVER read of this shed-session's lifecycle status while the screen stays open, so a
     * TEAMMATE'S submit on another phone flips this screen read-only without back/reopen —
     * [observePackingRowStatus] above only changes when THIS phone's own worklist sync writes a fresh
     * Room row. Reuses the existing `GET /feed-packing/worklist` read via
     * [FeedRepository.fetchPackingRowStatus] (no new backend endpoint). See
     * [FeedDistributionCompleteViewModel.startServerStatusPolling]'s kdoc for why this is bounded at
     * [MAX_SERVER_STATUS_POLLS] rather than a bare `while (isActive)`.
     */
    private fun startServerStatusPolling() {
        viewModelScope.launch {
            repeat(MAX_SERVER_STATUS_POLLS) {
                delay(SERVER_STATUS_POLL_INTERVAL_MS)
                pollServerStatusOnce()
            }
        }
    }

    /** A poll failure (offline/timeout/5xx) is swallowed and leaves state exactly as it was — see
     *  [applyLiveStatus]'s null-is-unknown contract. */
    private suspend fun pollServerStatusOnce() {
        if (shedId.isBlank() || workflow.isBlank() || targetDate.isBlank() || sessionNo < 1) return
        val status = runCatching { // exception:exempt expected poll failure (offline/timeout/5xx); see pollServerStatusOnce kdoc — null-is-unknown is the contract, not an error to record
            feedRepository.fetchPackingRowStatus(parkId, shedId, partitionLabel, workflow, sessionNo, targetDate)
        }.getOrNull()
        applyLiveStatus(status, fromRoom = false)
    }

    fun onEvent(event: FeedPackingCompleteEvent) {
        when (event) {
            FeedPackingCompleteEvent.RecordPackingVideo -> capturePackingVideo()
            // Re-record: drop the discarded take's queued upload, then capture afresh.
            // Re-record runs the camera FIRST and drops the old take's queued upload only once new
            // media exists (see capturePackingVideo). Discarding up front deleted a good clip
            // whenever the operator cancelled or the camera failed, leaving the slot empty.
            FeedPackingCompleteEvent.ReRecordPackingVideo -> capturePackingVideo(replacing = true)
            FeedPackingCompleteEvent.MarkDone -> markDone()
            FeedPackingCompleteEvent.SyncNow -> syncNow()
            FeedPackingCompleteEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    /** MANDATORY packing video — a LIVE in-app camera clip. It enqueues a PROOF_UPLOAD on the shed-session group so it
     *  drains before the completion. */
    private fun capturePackingVideo(replacing: Boolean = false) {
        if (_state.value.isCapturingVideo || _state.value.alreadySubmitted || shedId.isBlank()) return
        // A re-record starts from a FILLED slot, so videoCaptured only blocks a fresh record.
        if (!replacing && _state.value.videoCaptured) return
        analytics.track(
            AnalyticsEvents.FEED_PACKING_COMPLETE_OPENED,
            packingEventProps(if (replacing) ACTION_RE_RECORD_VIDEO else ACTION_RECORD_VIDEO),
        )
        _state.update { it.copy(isCapturingVideo = true, videoMessage = null) }
        viewModelScope.launch {
            val captured = try {
                proofCaptureSource.captureVideo(
                    ProofCaptureContext(
                        title = feedPackingProofCaption(),
                        primaryTag = shedLabel.ifBlank { shedId },
                        workLabel = sessionLabel.ifBlank { "Session $sessionNo" },
                        prompt = ProofCapturePrompt.FEED_PACKING,
                    ),
                )
            } catch (error: Exception) {
                crashReporter.recordException(error, "feed packing video capture failed")
                null
            }
            if (captured == null) {
                _state.update { it.copy(isCapturingVideo = false) }
                return@launch
            }
            // Build the evidence slot for re-capture. captureReplacingLatest ensures
            // the old row is only removed after the new capture succeeds (Manohar ordering).
            val slot = EvidenceSlot(
                identity = buildFeedEvidenceSlotIdentity("feed-pack", shedId, partitionLabel, sessionNo, workflow, targetDate),
                fieldKey = FIELD_FEED_PACKING_VIDEO,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = slot,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = feedPackingProofCaption(),
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
                    drafts.putProof(CaptureFlow.FEED_PACKING, groupKey, STEP_VIDEO, proofOutboxId)
                    draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
                    observeProofItem(proofOutboxId)
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_VIDEO_CAPTURED,
                        packingEventProps(ACTION_CAPTURED) +
                            (AnalyticsEvents.Params.PROOF_ID to result.value.id) +
                            (PARAM_OUTBOX_ITEM_ID to proofOutboxId),
                    )
                    _state.update { it.copy(isCapturingVideo = false, videoCaptured = true, videoMessage = VIDEO_QUEUED) }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    // The row was never created; drop the key so a retry mints a fresh one.
                    videoKey.invalidate()
                    result.cause?.let { crashReporter.recordException(it, "feed packing video enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
                        packingEventProps(ACTION_CAPTURE_FAILED) +
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
            // partitionMatchToken), so this read is pen-scoped by the task id alone. Passing the
            // label as well filtered on proof_capture.partitionKey, which capture() writes as
            // "whole" because the feed capture calls do not pass a label — so on a partitioned
            // shed the read asked for "3" while the row said "whole" and rehydration silently
            // returned nothing. Re-entering the screen showed an empty form for a video that was
            // sitting in Room and already uploading. Keep read and write symmetric (feed transport
            // and weighing omit it on both sides too); do not "restore" the label on one side only.
            proofCaptureRepository.observeProofs(groupKey)
                .collect { rows ->
                    val row = rows
                        .filter { it.fieldKey == FIELD_FEED_PACKING_VIDEO && it.syncStatus != CaptureSyncStatus.FAILED }
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
                drafts.putProof(CaptureFlow.FEED_PACKING, groupKey, STEP_VIDEO, outboxId)
                draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
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
        // Defense in depth alongside the UI gate: the video must exist to submit. Also check the
        // submitInFlight latch (SubmitViewModel idiom): a plain latch checked-and-set BEFORE the
        // enqueue coroutine launches, so a second tap landing in the async gap between the tap and
        // the state update reflecting it (`result`/`canComplete`) cannot slip past submitEnabled
        // and enqueue a second write. Reset on any terminal outcome (success or error) so a real
        // failure stays retryable.
        if (submitInFlight || !current.submitEnabled || videoItem.isNullOrBlank()) {
            _state.update { it.copy(canComplete = false, videoMessage = "Record the packing video before submitting.") }
            return
        }
        submitInFlight = true
        viewModelScope.launch {
            // Stable for the selected proof, fresh when the operator re-records. A retry of the same
            // video must replay; a replacement video must not collide with the old submit payload.
            val completeIdempotencyKey = "feed-packing-complete:$groupKey:$videoItem"
            if (draft.submitIdempotencyKey != completeIdempotencyKey) {
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
                    // Mark this pen-session as submitted for review immediately so the ViewModel overlay
                    observeOutboxItem(result.value)
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_SUBMITTED,
                        packingEventProps(ACTION_SUBMIT) +
                            (PARAM_PROOF_OUTBOX_ITEM_ID to videoItem) +
                            (PARAM_OUTBOX_ITEM_ID to result.value),
                    )
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    submitInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "feed packing complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
                        packingEventProps(ACTION_SUBMIT_FAILED) +
                            (AnalyticsEvents.Params.REASON to result.message),
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
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    // Reset submitInFlight latch on terminal FAILED so the user can retry.
                    // The latch was set true when the enqueue launched, but only reset on
                    // synchronous enqueue Err — not when the outbox row later reaches FAILED.
                    // Without this reset, button stays dead forever after terminal failure.
                    if (item.status == SyncItemStatus.FAILED) {
                        submitInFlight = false
                    }
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(
                            result = FeedPackingCompleteResultUi(writeResult.status.toPackingStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && packingProofReadyForSubmit(it),
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedPackingCompleteStatus.SYNCED || r.status == FeedPackingCompleteStatus.QUEUED } ?: false
            it.copy(canComplete = packingProofReadyForSubmit(it) && !committed)
        }
    }

    private fun packingProofReadyForSubmit(state: FeedPackingCompleteUiState): Boolean =
        state.videoCaptured && state.videoStatus.isQueuedForSubmit()

    private fun syncNow() {
        viewModelScope.launch {
            val videoProof = videoProofRowId
            if (!videoProof.isNullOrBlank()) {
                proofCaptureRepository.retryUpload(groupKey, videoProof)
            }
            draft.submitOutboxItemId?.takeIf { it.isNotBlank() }?.let { submitItem ->
                syncRepository.retry(submitItem)
            }
            syncRepository.triggerDrain()
            // Re-check the session's lifecycle status directly from the server, so a manual Sync
            // tap gets the same "did a teammate already submit this" answer the periodic poll
            // provides, without waiting for the next tick.
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

    private fun sg.mesha.goatos.feature.counts.CountsWriteStatus.toPackingStatus(): FeedPackingCompleteStatus = when (this) {
        sg.mesha.goatos.feature.counts.CountsWriteStatus.SYNCED -> FeedPackingCompleteStatus.SYNCED
        sg.mesha.goatos.feature.counts.CountsWriteStatus.FAILED -> FeedPackingCompleteStatus.FAILED
        else -> FeedPackingCompleteStatus.QUEUED
    }

    private fun feedPackingProofCaption(): String =
        proofOverlayContextLine(
            feature = "Feed packing",
            parkLabel = parkLabel.ifBlank { parkId },
            locationLabel = shedLabel.ifBlank { shedId },
            extraLabel = listOf(
                sessionLabel.ifBlank { "Session $sessionNo" },
                partitionLabel,
            ).filter { it.isNotBlank() }.joinToString(" . "),
        )

    private fun packingEventProps(action: String): Map<String, String> =
        mapOf(
            AnalyticsEvents.Params.SOURCE to SCREEN_FEED_PACKING_DETAIL,
            AnalyticsEvents.Params.KIND to KIND_PACKING,
            AnalyticsEvents.Params.ACTION to action,
            AnalyticsEvents.Params.SHED_ID to shedId,
            AnalyticsEvents.Params.PARTITION_LABEL to partitionLabel,
            AnalyticsEvents.Params.SESSION_NO to sessionNo.toString(),
            AnalyticsEvents.Params.FIELD to FIELD_FEED_PACKING_VIDEO,
            PARAM_GROUP_KEY to groupKey,
        )

    companion object {
        const val ARG_PARK_ID = "park_id"
        const val ARG_SHED_ID = "shed_id"
        const val ARG_SESSION_NO = "session_no"
        const val ARG_WORKFLOW = "workflow"
        const val ARG_TARGET_DATE = "target_date"
        const val ARG_SHED_LABEL = "shed_label"
        const val ARG_SESSION_LABEL = "session_label"
        const val ARG_PARK_LABEL = "park_label"

        /** The PEN worked. Part of the completion's identity: without it one pen's video closed
         *  out every pen of the shed (STG 2026-08-08). */
        const val ARG_PARTITION_LABEL = "partition_label"
        const val ARG_LIFECYCLE_STATUS = "lifecycle_status"

        /** How often [startServerStatusPolling] re-checks this session's lifecycle status directly
         *  from the server while the screen stays open. */
        private const val SERVER_STATUS_POLL_INTERVAL_MS = 30_000L

        /** Bound for [startServerStatusPolling] — see its kdoc for why this cannot be unbounded. */
        private const val MAX_SERVER_STATUS_POLLS = 2_880
        private const val KIND_PACKING = "packing"
        private const val SCREEN_FEED_PACKING_DETAIL = "feed_packing_complete"
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
        private const val KEY_VIDEO_IDEMPOTENCY = "feedPacking.videoKey"
        private const val FIELD_FEED_PACKING_VIDEO = "feed_packing_video"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the packing video."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this packing is counted."
        private const val VIDEO_QUEUED = "Packing video saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Packing video upload is in progress."
        private const val PROOF_SYNCED = "Packing video is ready."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }
}
