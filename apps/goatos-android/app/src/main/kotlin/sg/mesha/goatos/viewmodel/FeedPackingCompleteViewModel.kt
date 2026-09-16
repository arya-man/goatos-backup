package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
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
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedSopCardDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.feature.feed.FeedPackingCompleteEvent
import sg.mesha.goatos.feature.feed.FeedPackingCompleteResultUi
import sg.mesha.goatos.feature.feed.FeedPackingCompleteStatus
import sg.mesha.goatos.feature.feed.FeedPackingCompleteUiState
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import java.util.Locale
import javax.inject.Inject

/**
 * The feed-PACKING completion detail (`/feed/packing/complete/...`) — the verifier-GATED packing
 * flow (docs/decisions/feed-distribution-verification.md → "Feed packing is proved ONCE PER BAG").
 * Grain is the shed-SESSION: a pen's morning and evening bags are two cards, two proofs.
 *
 * FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md): the captures this screen
 * asks for are the packing CARD's, authored on /feed/sops and pinned on the sheet. Today's card is
 * one packing video; the maintainer may add a photo of the scale, make a slot optional or ask a
 * question, and this screen follows without a new build. The card reaches the phone two ways: the
 * SEEDED fallback compiled in, and the sheet's card the Feed Packing worklist cached in Room
 * (offline; refreshed every time the list is opened online).
 *
 * The slot machinery is [FeedSopSlotController]; this class owns the pen-session identity, the
 * read-only gate and the submit ([SyncRepository.enqueueFeedPackingComplete]), which names every
 * captured slot by its outbox row and is keyed on that proof set plus the answers -- a retry
 * replays, a replacement take does not collide with the old submit.
 */
@HiltViewModel
class FeedPackingCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    proofCaptureSource: ProofCaptureSource,
    photoCaptureSource: PhotoCaptureSource,
    proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val drafts: CaptureDraftRepository,
    private val feedRepository: FeedRepository,
    @Suppress("unused") private val feedCompletionStore: FeedCompletionLocalStore,
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

    // The row's backend-owned lifecycle bucket AT THE MOMENT the row was tapped — a FIRST-PAINT hint.
    // [observeLiveLifecycleStatus] supersedes it with the Room-backed live value (STG 2026-08-09).
    private val lifecycleStatusHint: String = savedStateHandle.get<String>(ARG_LIFECYCLE_STATUS).orEmpty()
    private val alreadySubmitted: Boolean = !feedSessionCanCapture(lifecycleStatusHint, isToday = true)

    // The day-shed-PEN-session partitions ordering for the proofs AND the completion, so every
    // proof drains strictly before the gated completion that references it.
    private val groupKey = feedCaptureGroupKey("feed-pack", shedId, partitionLabel, sessionNo, workflow, targetDate)

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

    private val slots = FeedSopSlotController(
        scope = viewModelScope,
        savedStateHandle = savedStateHandle,
        syncRepository = syncRepository,
        proofCaptureSource = proofCaptureSource,
        photoCaptureSource = photoCaptureSource,
        proofCaptureRepository = proofCaptureRepository,
        analytics = analytics,
        crashReporter = crashReporter,
        stageKey = "feedPacking",
        groupKey = groupKey,
        shedId = shedId,
        evidenceIdentity = buildFeedEvidenceSlotIdentity("feed-pack", shedId, partitionLabel, sessionNo, workflow, targetDate),
        proofPolicy = ::feedShedProofPolicy,
        caption = { feedPackingProofCaption() },
        videoContext = { title ->
            ProofCaptureContext(
                title = title,
                primaryTag = shedLabel.ifBlank { shedId },
                workLabel = sessionLabel.ifBlank { "Session $sessionNo" },
                prompt = ProofCapturePrompt.FEED_PACKING,
            )
        },
        photoContext = { slot -> PhotoCaptureContext(title = slot.title, instruction = slot.hint.ifBlank { slot.title }) },
        events = FeedSopSlotController.Events(
            captureTapped = AnalyticsEvents.FEED_PACKING_CAPTURE_TAPPED,
            captured = AnalyticsEvents.FEED_PACKING_VIDEO_CAPTURED,
            uploadSynced = AnalyticsEvents.FEED_PACKING_PROOF_UPLOAD_SYNCED,
            failure = AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
            reuploadTapped = AnalyticsEvents.FEED_PACKING_REUPLOAD_TAPPED,
            cardApplied = AnalyticsEvents.FEED_PACKING_CARD_APPLIED,
        ),
        baseProps = { slotKey, action -> packingEventProps(action, slotKey) },
        locked = { _state.value.isFinalSubmitted },
        onChanged = ::recomputeCanComplete,
        durableDrafts = drafts,
        durableFlowKey = CaptureFlow.FEED_PACKING,
        legacySteps = mapOf(STEP_VIDEO to FIELD_FEED_PACKING_VIDEO),
    )

    private var statusJob: Job? = null
    private var syncStatusJob: Job? = null
    private var submitInFlight = false

    init {
        analytics.track(AnalyticsEvents.FEED_PACKING_COMPLETE_OPENED, packingEventProps(ACTION_DETAIL_OPENED))
        slots.applyCard(seededCard(), FeedSopSlotController.CARD_RANK_SEEDED, source = "seeded")
        viewModelScope.launch { slots.state.collect { card -> _state.update { it.copy(card = card) }; recomputeCanComplete() } }
        slots.start()
        observeCachedCard()
        viewModelScope.launch {
            draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
            draft.submitOutboxItemId?.let(::observeOutboxItem)
        }
        observeSyncStatus()
        observeLiveLifecycleStatus()
    }

    /** The seeded packing card (feeddirection/domain/sopseed/feed_packing.json), compiled in as the
     *  fallback; its slot key is the field_key this app has always stamped on the packing clip. */
    private fun seededCard(): FeedSopCardDto = FeedSopCardDto(
        version = 0,
        stage = "packing",
        instruction = appContext.getString(R.string.feed_seed_packing_instruction),
        proofs = listOf(
            WeighingRemovalProofSlotDto(
                key = FIELD_FEED_PACKING_VIDEO,
                title = appContext.getString(R.string.feed_seed_slot_packing_video),
                hint = appContext.getString(R.string.feed_seed_slot_packing_video_hint),
                kind = FeedSlotCaptureKind.VIDEO,
                required = true,
            ),
        ),
    )

    /** The sheet's packing card as the Feed Packing worklist last cached it (Room). */
    private fun observeCachedCard() {
        if (targetDate.isBlank() || workflow.isBlank()) return
        viewModelScope.launch {
            feedRepository.observePackingCard(parkId, targetDate, workflow)
                .filterNotNull()
                .collect { card -> slots.applyCard(card, FeedSopSlotController.CARD_RANK_CACHED, source = "room") }
        }
    }

    /**
     * Supersede the nav-arg [lifecycleStatusHint] with the LIVE Room-backed status the moment Room
     * has one for this pen-session — the same table the worklist renders from. A `null` emission
     * (no cached row yet) is deliberately IGNORED so the screen keeps the hint.
     */
    private fun observeLiveLifecycleStatus() {
        viewModelScope.launch {
            feedRepository.observePackingRowStatus(shedId, partitionLabel, workflow, sessionNo)
                .collect { liveStatus -> applyLiveStatus(liveStatus, fromRoom = true) }
        }
        startServerStatusPolling()
    }

    /** `null` (no answer yet / poll failed) is ignored: never flips editable -> locked on a guess. */
    private fun applyLiveStatus(liveStatus: String?, fromRoom: Boolean) {
        if (liveStatus == null) return
        _state.update { it.copy(alreadySubmitted = !feedSessionCanCapture(liveStatus, isToday = true)) }
        if (!fromRoom) {
            viewModelScope.launch {
                feedRepository.persistPackingRowStatus(shedId, partitionLabel, workflow, sessionNo, liveStatus)
            }
        }
    }

    /** Periodic SERVER read of the lifecycle status while the screen stays open; bounded, see
     *  [FeedDistributionCompleteViewModel.startServerStatusPolling]. */
    private fun startServerStatusPolling() {
        viewModelScope.launch {
            repeat(MAX_SERVER_STATUS_POLLS) {
                delay(SERVER_STATUS_POLL_INTERVAL_MS)
                pollServerStatusOnce()
            }
        }
    }

    private suspend fun pollServerStatusOnce() {
        if (shedId.isBlank() || workflow.isBlank() || targetDate.isBlank() || sessionNo < 1) return
        val status = runCatching { // exception:exempt expected poll failure (offline/timeout/5xx); null-is-unknown is the contract, not an error to record
            feedRepository.fetchPackingRowStatus(parkId, shedId, partitionLabel, workflow, sessionNo, targetDate)
        }.getOrNull()
        applyLiveStatus(status, fromRoom = false)
    }

    fun onEvent(event: FeedPackingCompleteEvent) {
        when (event) {
            is FeedPackingCompleteEvent.CaptureSlot -> slots.captureSlot(event.slotKey, event.kind)
            is FeedPackingCompleteEvent.Answer -> slots.answer(event.questionId, event.value)
            FeedPackingCompleteEvent.MarkDone -> markDone()
            FeedPackingCompleteEvent.SyncNow -> syncNow()
            FeedPackingCompleteEvent.Back -> Unit // navigation — handled by the nav host.
            is FeedPackingCompleteEvent.PreviewAction -> trackPreviewAction(event.slotKey, event.action)
        }
    }

    private fun trackPreviewAction(slotKey: String, action: String) {
        val previewAction = ProofPreviewActionTrace.from(action)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_PREVIEW_ACTION,
            packingEventProps(previewAction.action, slotKey) + buildMap {
                put(AnalyticsEvents.Params.OUTCOME, previewAction.outcome)
                previewAction.reason?.let { put(AnalyticsEvents.Params.REASON, it) }
                slots.proofRowIdFor(slotKey)?.takeIf { it.isNotBlank() }?.let { put("local_proof_row_id", it) }
                slots.outboxItemIdFor(slotKey)?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it) }
            },
        )
    }

    private fun markDone() {
        val current = _state.value
        val slotRefs = slots.submitRefs()
        // Defense in depth alongside the UI gate, plus the submitInFlight latch (SubmitViewModel
        // idiom): checked-and-set BEFORE the enqueue coroutine launches, so a second tap in the async
        // gap cannot enqueue a second write. Reset on any terminal outcome so a failure stays retryable.
        if (submitInFlight || !current.submitEnabled || slotRefs == null) {
            analytics.track(AnalyticsEvents.FEED_PACKING_SUBMIT_BLOCKED, packingEventProps(ACTION_SUBMIT_BLOCKED))
            _state.update { it.copy(canComplete = false) }
            return
        }
        val answers = slots.answersJson()
        submitInFlight = true
        viewModelScope.launch {
            // Stable for the selected proof SET (+ answers), fresh when the operator re-records. A
            // retry of the same take must replay; a replacement must not collide with the old submit.
            val completeIdempotencyKey = "feed-packing-complete:$groupKey:" + slots.submitDigest(slotRefs, answers)
            if (draft.submitIdempotencyKey != completeIdempotencyKey) {
                drafts.putSubmit(CaptureFlow.FEED_PACKING, groupKey, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
            }
            val legacyVideo = slotRefs[FIELD_FEED_PACKING_VIDEO]?.outboxItemId.orEmpty()
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
                    packingProofOutboxItemId = legacyVideo,
                    slotProofs = slotRefs,
                    answers = answers,
                )
            ) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.FEED_PACKING, groupKey, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.FEED_PACKING, groupKey)
                    observeOutboxItem(result.value)
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_SUBMITTED,
                        packingEventProps(ACTION_SUBMIT) +
                            (PARAM_PROOF_OUTBOX_ITEM_ID to legacyVideo) +
                            (PARAM_OUTBOX_ITEM_ID to result.value) +
                            ("slot_keys" to slotRefs.keys.joinToString(",")),
                    )
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    submitInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "feed packing complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_PACKING_COMPLETE_FAILURE,
                        packingEventProps(ACTION_SUBMIT_FAILED) + (AnalyticsEvents.Params.REASON to result.message),
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
                    // Reset the latch on terminal FAILED so the operator can retry.
                    if (item.status == SyncItemStatus.FAILED) submitInFlight = false
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(
                            result = FeedPackingCompleteResultUi(writeResult.status.toPackingStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && it.card.readyToSubmit,
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedPackingCompleteStatus.SYNCED || r.status == FeedPackingCompleteStatus.QUEUED } ?: false
            it.copy(canComplete = it.card.readyToSubmit && !committed)
        }
    }

    private fun syncNow() {
        analytics.track(AnalyticsEvents.FEED_PACKING_SYNC_TAPPED, packingEventProps(ACTION_REFRESH))
        viewModelScope.launch {
            slots.retryUploads()
            draft.submitOutboxItemId?.takeIf { it.isNotBlank() }?.let { submitItem -> syncRepository.retry(submitItem) }
            syncRepository.triggerDrain()
            pollServerStatusOnce()
        }
    }

    private fun observeSyncStatus() {
        syncStatusJob?.cancel()
        syncStatusJob = viewModelScope.launch {
            syncRepository.observeStatus()
                .map { status -> status.inFlightCount > 0 }
                .distinctUntilChanged()
                .collect { syncing -> _state.update { it.copy(isSyncing = syncing) } }
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

    private fun packingEventProps(action: String, slotKey: String? = null): Map<String, String> =
        mapOf(
            AnalyticsEvents.Params.SOURCE to SCREEN_FEED_PACKING_DETAIL,
            AnalyticsEvents.Params.KIND to KIND_PACKING,
            AnalyticsEvents.Params.ACTION to action,
            AnalyticsEvents.Params.SHED_ID to shedId,
            AnalyticsEvents.Params.PARTITION_LABEL to partitionLabel,
            AnalyticsEvents.Params.SESSION_NO to sessionNo.toString(),
            AnalyticsEvents.Params.FIELD to (slotKey ?: FIELD_FEED_PACKING_VIDEO),
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

        private const val SERVER_STATUS_POLL_INTERVAL_MS = 30_000L
        private const val MAX_SERVER_STATUS_POLLS = 2_880
        private const val KIND_PACKING = "packing"
        private const val SCREEN_FEED_PACKING_DETAIL = "feed_packing_complete"
        private const val ACTION_DETAIL_OPENED = "detail_opened"
        private const val ACTION_REFRESH = "refresh"
        private const val ACTION_SUBMIT = "submit"
        private const val ACTION_SUBMIT_BLOCKED = "submit_blocked"
        private const val ACTION_SUBMIT_FAILED = "submit_failed"
        private const val PARAM_GROUP_KEY = "group_key"
        private const val PARAM_OUTBOX_ITEM_ID = "outbox_item_id"
        private const val PARAM_PROOF_OUTBOX_ITEM_ID = "proof_outbox_item_id"

        /** Draft step name an older build wrote for the one packing clip; maps to the seeded slot. */
        private const val STEP_VIDEO = "video"
        internal const val FIELD_FEED_PACKING_VIDEO = "feed_packing_video"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the packing proofs."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this packing is counted."
    }
}
