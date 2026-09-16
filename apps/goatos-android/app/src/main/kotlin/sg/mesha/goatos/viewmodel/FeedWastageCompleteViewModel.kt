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
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedSopCardDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind
import sg.mesha.goatos.feature.feed.FeedWastageCompleteEvent
import sg.mesha.goatos.feature.feed.FeedWastageCompleteResultUi
import sg.mesha.goatos.feature.feed.FeedWastageCompleteStatus
import sg.mesha.goatos.feature.feed.FeedWastageCompleteUiState
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import javax.inject.Inject

/**
 * The feed-WASTAGE completion detail (`/feed/wastage/complete/...`) — the verifier-GATED wastage
 * flow (maintainer decision 2026-08-18). Opened by tapping a pen row on Feed Wastage. The
 * completion is a PEN-DAY (no session, no workflow — the server stamps `experiment`); the
 * verifier reads the leftover weight off the clip and her approve carries the number.
 *
 * FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md): the captures are the
 * direction SOP's WASTAGE CARD, authored on /feed/sops and pinned on the sheet. Today's card is one
 * leftover-feed video; a photo, an optional slot or a question added there reaches this screen
 * without a new build. Card sources: the SEEDED fallback compiled in, then the sheet's card the
 * Feed Wastage worklist cached in Room. The slot machinery is [FeedSopSlotController]; the submit
 * ([SyncRepository.enqueueFeedWastageComplete]) names every captured slot by its outbox row.
 */
@HiltViewModel
class FeedWastageCompleteViewModel @Inject constructor(
    private val syncRepository: SyncRepository,
    proofCaptureSource: ProofCaptureSource,
    photoCaptureSource: PhotoCaptureSource,
    proofCaptureRepository: ProofCaptureRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val drafts: CaptureDraftRepository,
    private val feedRepository: FeedRepository,
    @ApplicationContext private val appContext: Context,
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

    // The day-shed-PEN identity for the proofs AND the completion, so every proof drains strictly
    // before the gated completion that references it. Session 0 is a real value here — the
    // wastage grain HAS no session — and the constant workflow keeps the key stable.
    private val groupKey =
        feedCaptureGroupKey("feed-wastage", shedId, partitionLabel, 0, WASTAGE_WORKFLOW, targetDate)

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

    private val slots = FeedSopSlotController(
        scope = viewModelScope,
        savedStateHandle = savedStateHandle,
        syncRepository = syncRepository,
        proofCaptureSource = proofCaptureSource,
        photoCaptureSource = photoCaptureSource,
        proofCaptureRepository = proofCaptureRepository,
        analytics = analytics,
        crashReporter = crashReporter,
        stageKey = "feedWastage",
        groupKey = groupKey,
        shedId = shedId,
        evidenceIdentity = buildFeedEvidenceSlotIdentity("feed-wastage", shedId, partitionLabel, 0, WASTAGE_WORKFLOW, targetDate),
        proofPolicy = ::feedShedProofPolicy,
        caption = { feedWastageProofCaption() },
        videoContext = { title ->
            ProofCaptureContext(
                title = title,
                primaryTag = shedLabel.ifBlank { shedId },
                workLabel = experimentArm,
                prompt = ProofCapturePrompt.FEED_WASTAGE,
            )
        },
        photoContext = { slot -> PhotoCaptureContext(title = slot.title, instruction = slot.hint.ifBlank { slot.title }) },
        events = FeedSopSlotController.Events(
            captureTapped = AnalyticsEvents.FEED_WASTAGE_CAPTURE_TAPPED,
            captured = AnalyticsEvents.FEED_WASTAGE_VIDEO_CAPTURED,
            uploadSynced = AnalyticsEvents.FEED_WASTAGE_PROOF_UPLOAD_SYNCED,
            failure = AnalyticsEvents.FEED_WASTAGE_COMPLETE_FAILURE,
            reuploadTapped = AnalyticsEvents.FEED_WASTAGE_REUPLOAD_TAPPED,
            cardApplied = AnalyticsEvents.FEED_WASTAGE_CARD_APPLIED,
        ),
        baseProps = { slotKey, action -> wastageEventProps(action, slotKey) },
        locked = { _state.value.isFinalSubmitted },
        onChanged = ::recomputeCanComplete,
        durableDrafts = drafts,
        durableFlowKey = CaptureFlow.FEED_WASTAGE,
        legacySteps = mapOf(STEP_VIDEO to FIELD_FEED_WASTAGE_VIDEO),
    )

    private var statusJob: Job? = null
    private var syncStatusJob: Job? = null
    private var submitInFlight = false

    init {
        analytics.track(AnalyticsEvents.FEED_WASTAGE_COMPLETE_OPENED, wastageEventProps(ACTION_DETAIL_OPENED))
        slots.applyCard(seededCard(), FeedSopSlotController.CARD_RANK_SEEDED, source = "seeded")
        viewModelScope.launch { slots.state.collect { card -> _state.update { it.copy(card = card) }; recomputeCanComplete() } }
        slots.start()
        observeCachedCard()
        viewModelScope.launch {
            draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
            draft.submitOutboxItemId?.let(::observeOutboxItem)
        }
        observeSyncStatus()
        observeLiveLifecycleStatus()
    }

    /** The seeded wastage card (feeddirection/domain/sopseed/feed_direction.json → wastage). */
    private fun seededCard(): FeedSopCardDto = FeedSopCardDto(
        version = 0,
        stage = "wastage",
        instruction = appContext.getString(R.string.feed_seed_wastage_instruction),
        proofs = listOf(
            WeighingRemovalProofSlotDto(
                key = FIELD_FEED_WASTAGE_VIDEO,
                title = appContext.getString(R.string.feed_seed_slot_wastage_video),
                hint = appContext.getString(R.string.feed_seed_slot_wastage_video_hint),
                kind = FeedSlotCaptureKind.VIDEO,
                required = true,
            ),
        ),
    )

    private fun observeCachedCard() {
        if (targetDate.isBlank()) return
        viewModelScope.launch {
            feedRepository.observeWastageCard(parkId, targetDate)
                .filterNotNull()
                .collect { card -> slots.applyCard(card, FeedSopSlotController.CARD_RANK_CACHED, source = "room") }
        }
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
            is FeedWastageCompleteEvent.CaptureSlot -> slots.captureSlot(event.slotKey, event.kind)
            is FeedWastageCompleteEvent.Answer -> slots.answer(event.questionId, event.value)
            FeedWastageCompleteEvent.MarkDone -> markDone()
            FeedWastageCompleteEvent.SyncNow -> syncNow()
            FeedWastageCompleteEvent.Back -> Unit // navigation — handled by the nav host.
            is FeedWastageCompleteEvent.PreviewAction -> trackPreviewAction(event.slotKey, event.action)
        }
    }

    private fun trackPreviewAction(slotKey: String, action: String) {
        val previewAction = ProofPreviewActionTrace.from(action)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_PREVIEW_ACTION,
            wastageEventProps(previewAction.action, slotKey) + buildMap {
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
        // Defense in depth alongside the UI gate, plus the submitInFlight latch so a second tap in
        // the async gap cannot enqueue a second write (same idiom as FeedPackingCompleteViewModel).
        if (submitInFlight || !current.submitEnabled || slotRefs == null) {
            analytics.track(AnalyticsEvents.FEED_WASTAGE_SUBMIT_BLOCKED, wastageEventProps(ACTION_SUBMIT_BLOCKED))
            _state.update { it.copy(canComplete = false) }
            return
        }
        val answers = slots.answersJson()
        submitInFlight = true
        viewModelScope.launch {
            // Stable for the selected proof SET (+ answers), fresh when the operator re-records: a
            // retry of the same take must replay; a replacement must not collide with the old
            // payload — the backend answers the DIFFERENT-video case with a terminal 409.
            val completeIdempotencyKey = "feed-wastage-complete:$groupKey:" + slots.submitDigest(slotRefs, answers)
            if (draft.submitIdempotencyKey != completeIdempotencyKey) {
                drafts.putSubmit(CaptureFlow.FEED_WASTAGE, groupKey, completeIdempotencyKey, null)
                draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
            }
            val legacyVideo = slotRefs[FIELD_FEED_WASTAGE_VIDEO]?.outboxItemId.orEmpty()
            when (
                val result = syncRepository.enqueueFeedWastageComplete(
                    groupKey = groupKey,
                    idempotencyKey = completeIdempotencyKey,
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    targetDate = targetDate,
                    wastageProofOutboxItemId = legacyVideo,
                    slotProofs = slotRefs,
                    answers = answers,
                )
            ) {
                is AppResult.Ok -> {
                    drafts.putSubmit(CaptureFlow.FEED_WASTAGE, groupKey, completeIdempotencyKey, result.value)
                    draft = drafts.find(CaptureFlow.FEED_WASTAGE, groupKey)
                    observeOutboxItem(result.value)
                    analytics.track(
                        AnalyticsEvents.FEED_WASTAGE_SUBMITTED,
                        wastageEventProps(ACTION_SUBMIT) +
                            (PARAM_PROOF_OUTBOX_ITEM_ID to legacyVideo) +
                            (PARAM_OUTBOX_ITEM_ID to result.value) +
                            ("slot_keys" to slotRefs.keys.joinToString(",")),
                    )
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    submitInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "feed wastage complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_WASTAGE_COMPLETE_FAILURE,
                        wastageEventProps(ACTION_SUBMIT_FAILED) + (AnalyticsEvents.Params.REASON to result.message),
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
                    if (item.status == SyncItemStatus.FAILED) submitInFlight = false
                    _state.update {
                        val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                        it.copy(
                            result = FeedWastageCompleteResultUi(writeResult.status.toWastageStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && it.card.readyToSubmit,
                        )
                    }
                }
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedWastageCompleteStatus.SYNCED || r.status == FeedWastageCompleteStatus.QUEUED } ?: false
            it.copy(canComplete = it.card.readyToSubmit && !committed)
        }
    }

    private fun syncNow() {
        analytics.track(AnalyticsEvents.FEED_WASTAGE_SYNC_TAPPED, wastageEventProps(ACTION_REFRESH))
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

    private fun wastageEventProps(action: String, slotKey: String? = null): Map<String, String> =
        mapOf(
            AnalyticsEvents.Params.SOURCE to SCREEN_FEED_WASTAGE_DETAIL,
            AnalyticsEvents.Params.KIND to KIND_WASTAGE,
            AnalyticsEvents.Params.ACTION to action,
            AnalyticsEvents.Params.SHED_ID to shedId,
            AnalyticsEvents.Params.PARTITION_LABEL to partitionLabel,
            AnalyticsEvents.Params.FIELD to (slotKey ?: FIELD_FEED_WASTAGE_VIDEO),
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
        private const val ACTION_REFRESH = "refresh"
        private const val ACTION_SUBMIT = "submit"
        private const val ACTION_SUBMIT_BLOCKED = "submit_blocked"
        private const val ACTION_SUBMIT_FAILED = "submit_failed"
        private const val PARAM_GROUP_KEY = "group_key"
        private const val PARAM_OUTBOX_ITEM_ID = "outbox_item_id"
        private const val PARAM_PROOF_OUTBOX_ITEM_ID = "proof_outbox_item_id"

        /** Draft step name an older build wrote for the one wastage clip; maps to the seeded slot. */
        private const val STEP_VIDEO = "video"
        internal const val FIELD_FEED_WASTAGE_VIDEO = "feed_wastage_video"
        private const val QUEUED_MESSAGE = "Submitted for verification. A verifier will review the leftover feed proofs."
        private const val SYNCED_MESSAGE = "Submitted. Waiting for verifier approval before this is counted."
    }
}
