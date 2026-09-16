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
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.analytics.ProofPreviewActionTrace
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedDistributionCapturedSlotDto
import sg.mesha.goatos.core.network.dto.FeedSopCardDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedDistributionQuestionUi
import sg.mesha.goatos.feature.feed.FeedDistributionResultUi
import sg.mesha.goatos.feature.feed.FeedDistributionSlotUi
import sg.mesha.goatos.feature.feed.FeedDistributionStatus
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind
import sg.mesha.goatos.feature.feed.feedSessionCanCapture
import sg.mesha.goatos.feature.feed.isQueuedForSubmit
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import java.util.UUID
import javax.inject.Inject

/**
 * The feed-DISTRIBUTION completion detail (`/feed/distribution/complete/...`) — the verifier-GATED
 * Direction flow (docs/decisions/feed-distribution-verification.md). Opened by tapping a Feed
 * DIRECTION row.
 *
 * FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md): the captures this screen
 * asks for are the CARD's, authored on /feed/sops and pinned on the sheet the pen-session belongs
 * to. Today's card is the seeded trio (feed weight PHOTO, feed VIDEO, water VIDEO); tomorrow's may
 * add a slot, drop one, make one optional or accept either medium, and the screen follows without a
 * new build. The card reaches the phone three ways, weakest first: the SEEDED fallback compiled in
 * (so a phone that has never been online still shows the crew what today's SOP asks), the sheet's
 * card cached by the Feed Direction list in Room (offline), and the live captures read (online,
 * authoritative, arrives with the teammate slots and the session status).
 *
 * The COLLABORATION contract is unchanged (docs/product/feed-proof-collaboration.md): every slot is
 * an independent offline-first proof write on the pen-session's shared group; slots are parallel
 * and never gate each other; a slot shot on a teammate's phone is adopted by its server proof id;
 * the submit names every slot's reference, this phone's outbox row or the teammate's id, and is
 * keyed on that proof SET so two phones submitting the same pen are one write.
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
    private val savedStateHandle: SavedStateHandle,
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

    // The shed-session partitions ordering for the proof uploads and completion, so every proof
    // item drains before the gated completion references them.
    private val groupKey = feedCaptureGroupKey("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate)

    private val outboxItemId = DraftOutboxItemId(savedStateHandle, KEY_OUTBOX_ITEM_ID)

    /**
     * This phone's durable view of ONE slot, keyed by the card's slot key = the proof register
     * field_key. Persisted in the SavedStateHandle per key so a card with four slots survives
     * process death as well as the seeded three did. [remoteRef] is the SERVER proof id of a proof
     * a teammate recorded (maintainer decision 2026-08-14); a slot shot elsewhere has no local
     * outbox row here, so its server id is what this phone submits with.
     */
    private inner class SlotDraft(val key: String) {
        val proofItemId = DraftOutboxItemId(savedStateHandle, "$KEY_SLOT_PREFIX$key.proofItemId")
        val proofRowId = DraftOutboxItemId(savedStateHandle, "$KEY_SLOT_PREFIX$key.proofRowId")
        val remoteRef = DraftOutboxItemId(savedStateHandle, "$KEY_SLOT_PREFIX$key.remoteRef")
        var statusJob: Job? = null
        var syncedTracked = false
        val locallyCaptured: Boolean get() = proofItemId.value != null
        /** The reference the submit names for this slot: own outbox row first, else the teammate's. */
        val submitRef: String? get() = proofItemId.value ?: remoteRef.value
    }

    private val drafts = mutableMapOf<String, SlotDraft>() // mobile-guard:ignore: bounded by the card's slot count (<= 12 per SOP) plus rows already in Room for this pen-session
    private fun draft(key: String): SlotDraft = drafts.getOrPut(key) { SlotDraft(key) }

    /** Which card the screen currently renders: 0 = seeded fallback, 1 = Room-cached sheet card,
     *  2 = the live captures read. A weaker source never overwrites a stronger one. */
    private var cardRank = CARD_RANK_SEEDED
    private var cardVersion = -1
    private var lastProofRows: List<ProofCaptureRow> = emptyList()
    private var lastTeammateSlots: List<FeedDistributionCapturedSlotDto> = emptyList()
    private var completeEnqueueInFlight = false
    /** Single-flight: the internal retry ladder can run ~17s, and the 30s poll re-invokes this. */
    private var teammateRefreshInFlight = false

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
    private var syncStatusJob: Job? = null

    init {
        analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_OPENED, distributionEventProps(action = ACTION_DETAIL_OPENED))
        applyCard(seededCard(), CARD_RANK_SEEDED, source = "seeded")
        observeCachedCard()
        recomputeCanComplete()
        observeSyncStatus()
        observeDurableProofs()
        refreshTeammateCaptures(source = "open")
        outboxItemId.value?.let(::observeOutboxItem)
        observeLiveLifecycleStatus()
    }

    // ------------------------------------------------------------------ the card

    /**
     * The seeded distribution card (feeddirection/domain/sopseed/feed_direction.json), compiled in as
     * the fallback for a phone that has never seen the sheet online. Its slot keys are the proof
     * register field_keys this app has always stamped, so a draft recorded under the fallback is
     * the same draft once the pinned card arrives.
     */
    private fun seededCard(): FeedSopCardDto = FeedSopCardDto(
        version = 0,
        stage = "distribution",
        instruction = appContext.getString(R.string.feed_seed_distribution_instruction),
        proofs = listOf(
            WeighingRemovalProofSlotDto(
                key = FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO,
                title = appContext.getString(R.string.feed_seed_slot_weight_photo),
                hint = appContext.getString(R.string.feed_seed_slot_weight_photo_hint),
                kind = FeedSlotCaptureKind.PHOTO,
                required = true,
            ),
            WeighingRemovalProofSlotDto(
                key = FIELD_FEED_DISTRIBUTION_VIDEO,
                title = appContext.getString(R.string.feed_seed_slot_feed_video),
                hint = appContext.getString(R.string.feed_seed_slot_feed_video_hint),
                kind = FeedSlotCaptureKind.VIDEO,
                required = true,
            ),
            WeighingRemovalProofSlotDto(
                key = FIELD_FEED_DISTRIBUTION_WATER_VIDEO,
                title = appContext.getString(R.string.feed_seed_slot_water_video),
                hint = appContext.getString(R.string.feed_seed_slot_water_video_hint),
                kind = FeedSlotCaptureKind.VIDEO,
                required = true,
            ),
        ),
    )

    /** The sheet's card as the Feed Direction list last cached it (Room), for the offline open. */
    private fun observeCachedCard() {
        if (targetDate.isBlank() || workflow.isBlank()) return
        viewModelScope.launch {
            feedRepository.observeDirectionCard(parkId, targetDate, workflow)
                .filterNotNull()
                .collect { card -> applyCard(card, CARD_RANK_CACHED, source = "room") }
        }
    }

    /**
     * Re-shapes the slot list to [card], keeping each slot's capture state by key. A slot the card
     * dropped leaves the screen (its upload, if any, stays in the register and is simply not named
     * on the submit); a slot the card added appears empty, or already filled when this pen-session
     * has a proof under that key in Room or on the server.
     */
    private fun applyCard(card: FeedSopCardDto, rank: Int, source: String) {
        if (rank < cardRank) return
        if (card.proofs.isEmpty()) return
        val changed = rank != cardRank || card.version != cardVersion || card.proofs.map { it.key } != _state.value.slots.map { it.slotKey }
        cardRank = rank
        cardVersion = card.version
        _state.update { st ->
            val byKey = st.slots.associateBy { it.slotKey }
            st.copy(
                instruction = card.instruction,
                slots = card.proofs.map { p ->
                    val prev = byKey[p.key]
                    val kind = p.kind.ifBlank { FeedSlotCaptureKind.VIDEO }
                    (prev ?: FeedDistributionSlotUi(slotKey = p.key, title = p.title, capturedKind = if (kind == FeedSlotCaptureKind.PHOTO) kind else FeedSlotCaptureKind.VIDEO))
                        .copy(
                            title = p.title.ifBlank { prev?.title ?: p.key },
                            hint = p.hint,
                            captureKind = kind,
                            required = p.required,
                        )
                },
                questions = card.questions.map { q ->
                    FeedDistributionQuestionUi(
                        id = q.id,
                        kind = q.kind,
                        title = q.title,
                        hint = q.hint,
                        required = q.required,
                        options = q.options.map { it.value to it.label },
                        allowOther = q.allowOther,
                        unit = q.unit,
                        onlyIfQuestion = q.onlyIf?.questionId.orEmpty(),
                        onlyIfValue = q.onlyIf?.value.orEmpty(),
                    )
                },
            )
        }
        // Slots the card just added may already hold work: a draft restored from the saved state
        // (process death mid-capture), this phone's own rows in Room, an outbox row still uploading,
        // or a teammate's server proof. Replay each source onto the new list.
        _state.value.slots.forEach { slot ->
            val d = draft(slot.slotKey)
            if (d.proofItemId.value != null && !slot.captured) {
                updateSlot(slot.slotKey) { it.copy(captured = true, status = FeedDistributionProofStatus.QUEUED, previewIdentity = previewIdentity(slot.slotKey)) }
            } else if (d.remoteRef.value != null && !slot.captured) {
                val ref = d.remoteRef.value.orEmpty()
                updateSlot(slot.slotKey) {
                    it.copy(captured = true, status = FeedDistributionProofStatus.SYNCED, message = PROOF_RECORDED_BY_TEAMMATE, previewIdentity = previewIdentity(slot.slotKey), remoteUrl = feedBackendProofUrl(ref))
                }
            }
        }
        hydrateSlotsFromRows(lastProofRows)
        _state.value.slots.forEach { slot ->
            val d = draft(slot.slotKey)
            d.proofItemId.value?.let { if (d.statusJob == null) observeProofItem(slot.slotKey, it) }
        }
        adoptTeammateSlots(lastTeammateSlots)
        recomputeCanComplete()
        if (changed) {
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_CARD_APPLIED,
                distributionEventProps(
                    action = ACTION_CARD_APPLIED,
                    extra = mapOf(
                        AnalyticsEvents.Params.SOURCE to source,
                        PARAM_SOP_VERSION to card.version.toString(),
                        PARAM_SLOT_COUNT to card.proofs.size.toString(),
                        PARAM_QUESTION_COUNT to card.questions.size.toString(),
                    ),
                ),
            )
        }
    }

    // ------------------------------------------------------------------ lifecycle status

    /** See [FeedPackingCompleteViewModel.observeLiveLifecycleStatus] — the same shaped gate for the
     *  feed-direction shed-session table. A `null` emission (no cached row yet) is ignored so the
     *  screen keeps [lifecycleStatusHint] rather than forcing itself editable. */
    private fun observeLiveLifecycleStatus() {
        viewModelScope.launch {
            feedRepository.observeDirectionSessionStatus(shedId, partitionLabel, workflow, sessionNo)
                .collect { liveStatus -> applyLiveStatus(liveStatus, source = "room") }
        }
        startServerStatusPolling()
    }

    /** Shared by the Room-backed observer above and the SERVER poll below — both feed the same
     *  read-only gate. `null` (no answer yet / poll failed) is ignored: this must never flip
     *  editable -> locked on a guess, and never flips locked -> editable at all. */
    private fun applyLiveStatus(liveStatus: String?, source: String) {
        if (liveStatus == null) return
        val previouslyReadOnly = _state.value.alreadySubmitted
        val nowReadOnly = !feedSessionCanCapture(liveStatus, isToday = true)
        _state.update { it.copy(alreadySubmitted = nowReadOnly) }
        if (previouslyReadOnly != nowReadOnly) {
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_LIVE_STATUS_CHANGED,
                mapOf(
                    AnalyticsEvents.Params.SOURCE to source,
                    AnalyticsEvents.Params.PREVIOUS to if (previouslyReadOnly) "readonly" else "editable",
                    AnalyticsEvents.Params.NEXT to if (nowReadOnly) "readonly" else "editable",
                    AnalyticsEvents.Params.STATUS to liveStatus,
                ),
            )
        }
        // Persist SERVER-sourced status into Room so the live observer emits and the screen
        // survives process death offline. Room-sourced emissions are already in Room — writing
        // them back would fire a spurious table-wide invalidation on every emission.
        if (source != "room") {
            viewModelScope.launch {
                feedRepository.persistDirectionSessionStatus(shedId, partitionLabel, workflow, sessionNo, liveStatus)
            }
        }
    }

    /**
     * Periodic SERVER read of this shed-session's lifecycle status while the screen stays open, so a
     * TEAMMATE'S submit on another phone flips this screen read-only without back/reopen. Bounded at
     * [MAX_SERVER_STATUS_POLLS] (~24h) rather than a bare `while (isActive)`, which would make any
     * `advanceUntilIdle()` in a test hang forever.
     */
    private fun startServerStatusPolling() {
        viewModelScope.launch {
            repeat(MAX_SERVER_STATUS_POLLS) {
                delay(SERVER_STATUS_POLL_INTERVAL_MS)
                pollServerStatusOnce(source = "server_poll")
                // A teammate can upload a slot WHILE this screen sits open; without this the
                // screen only learns on reopen/Sync (field bug 2026-08-15).
                refreshTeammateCaptures(source = "server_poll")
            }
        }
    }

    /** A poll failure (offline/timeout/5xx) is swallowed and leaves state exactly as it was — see
     *  [applyLiveStatus]'s null-is-unknown contract. */
    private suspend fun pollServerStatusOnce(source: String) {
        if (shedId.isBlank() || workflow.isBlank() || targetDate.isBlank() || sessionNo < 1) return
        val status = runCatching { // exception:exempt expected poll failure (offline/timeout/5xx); see pollServerStatusOnce kdoc — null-is-unknown is the contract, not an error to record
            feedRepository.fetchDirectionSessionStatus(parkId, shedId, partitionLabel, workflow, sessionNo, targetDate)
        }.getOrNull()
        applyLiveStatus(status, source)
    }

    // ------------------------------------------------------------------ events

    fun onEvent(event: FeedDistributionEvent) {
        when (event) {
            is FeedDistributionEvent.CaptureSlot -> captureSlot(event.slotKey, event.kind)
            is FeedDistributionEvent.Answer -> answer(event.questionId, event.value)
            FeedDistributionEvent.MarkDone -> markDone()
            FeedDistributionEvent.SyncNow -> syncNow()
            FeedDistributionEvent.Back -> analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_BACK_TAPPED)
            is FeedDistributionEvent.SlotPlaybackFailed -> refreshTeammatePreviewUrl(event.slotKey)
            is FeedDistributionEvent.ProofPreviewAction -> trackPreviewAction(event.fieldKey, event.action)
        }
    }

    private fun answer(questionId: String, value: String) {
        if (_state.value.isFinalSubmitted) return
        _state.update { it.copy(answers = it.answers + (questionId to value)) }
        recomputeCanComplete()
    }

    private fun trackPreviewAction(fieldKey: String, action: String) {
        val d = drafts[fieldKey] ?: return
        val previewAction = ProofPreviewActionTrace.from(action)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_PREVIEW_ACTION,
            distributionEventProps(
                slotKey = fieldKey,
                action = previewAction.action,
                extra = buildMap {
                    put(AnalyticsEvents.Params.FIELD, fieldKey)
                    put(AnalyticsEvents.Params.OUTCOME, previewAction.outcome)
                    previewAction.reason?.let { put(AnalyticsEvents.Params.REASON, it) }
                    d.proofRowId.value?.takeIf { it.isNotBlank() }?.let {
                        put(PARAM_PROOF_ID, it)
                        put("local_proof_row_id", it)
                    }
                    d.proofItemId.value?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it) }
                    d.remoteRef.value?.takeIf { it.isNotBlank() }?.let { put("server_proof_id", it) }
                },
            ),
        )
    }

    // ------------------------------------------------------------------ capture

    /** What medium a tap on [slot] records: the tap's pick for an `either` slot, else the slot's kind. */
    private fun mediumFor(slot: FeedDistributionSlotUi, requested: String?): String = when (slot.captureKind) {
        FeedSlotCaptureKind.PHOTO -> FeedSlotCaptureKind.PHOTO
        FeedSlotCaptureKind.VIDEO -> FeedSlotCaptureKind.VIDEO
        else -> if (requested == FeedSlotCaptureKind.PHOTO) FeedSlotCaptureKind.PHOTO else FeedSlotCaptureKind.VIDEO
    }

    /**
     * ONE slot of the card, from the LIVE in-app camera, as an offline-first proof write on the
     * pen-session's shared group. Every slot goes through this same path; the only branch is the
     * medium. The gate is the slot's OWN state plus the session lock — never a sibling slot.
     */
    private fun captureSlot(slotKey: String, requestedKind: String?) {
        val slot = _state.value.slot(slotKey) ?: return
        if (!slot.captureEnabled || _state.value.isFinalSubmitted || shedId.isBlank()) return
        val medium = mediumFor(slot, requestedKind)
        val replacing = slot.captured
        val d = draft(slotKey)
        if (replacing) trackReuploadTapped(slotKey)
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_CAPTURE_TAPPED,
            distributionEventProps(slotKey, ACTION_RECORD_PROOF, mapOf(PARAM_MEDIUM to medium)),
        )
        updateSlot(slotKey) { it.copy(isCapturing = true, message = if (replacing) it.message else null, status = if (replacing) it.status else FeedDistributionProofStatus.QUEUED) }
        viewModelScope.launch {
            var captureThrew = false
            val captured: CapturedMedia? = try {
                if (medium == FeedSlotCaptureKind.PHOTO) {
                    photoCaptureSource.capturePhoto(
                        PhotoCaptureContext(title = slot.title, instruction = slot.hint.ifBlank { slot.title }),
                    )?.let { CapturedMedia(it.localUri, it.mimeType, it.capturedAtMs, it.capturedAtMs, it.captureSource) }
                } else {
                    proofCaptureSource.captureVideo(proofContext(slot.title))
                        ?.let { CapturedMedia(it.localUri, it.mimeType, it.startedAtMs, it.endedAtMs, it.captureSource) }
                }
            } catch (error: Exception) {
                captureThrew = true
                crashReporter.recordException(error, "feed distribution $slotKey capture failed")
                trackCaptureFailure(slotKey, "camera_exception")
                null
            }
            if (captured == null) {
                if (!captureThrew) trackCaptureFailure(slotKey, "camera_returned_null")
                updateSlot(slotKey) {
                    it.copy(
                        isCapturing = false,
                        status = if (replacing) it.status else FeedDistributionProofStatus.FAILED,
                        message = when {
                            captureThrew -> PROOF_FAILED
                            replacing -> it.message
                            else -> PROOF_FAILED
                        },
                    )
                }
                return@launch
            }
            // captureReplacingLatest removes the old row only after the new capture succeeds.
            val evidenceSlot = EvidenceSlot(
                identity = buildFeedEvidenceSlotIdentity("feed-dist", shedId, partitionLabel, sessionNo, workflow, targetDate),
                fieldKey = slotKey,
            )
            when (
                val result = proofCaptureRepository.captureReplacingLatest(
                    slot = evidenceSlot,
                    subject = ProofSubject.SHED,
                    subjectId = shedId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = feedProofCaption(slot.title),
                    scopeType = "shed",
                    scopeId = shedId,
                    capturedStartMs = captured.startMs,
                    capturedEndMs = captured.endMs,
                    capturedByPrincipalId = null,
                    proofPolicy = feedShedProofPolicy(captured.captureSource),
                    awaitUploadEnqueue = true,
                    uploadGroupKey = groupKey,
                )
            ) {
                is AppResult.Ok -> {
                    val proofOutboxId = result.value.outboxItemId
                    if (proofOutboxId.isNullOrBlank()) {
                        trackCaptureFailure(slotKey, "missing_upload_outbox")
                        updateSlot(slotKey) { it.copy(isCapturing = false, status = FeedDistributionProofStatus.FAILED, message = PROOF_FAILED) }
                        return@launch
                    }
                    d.proofItemId.value = proofOutboxId
                    d.proofRowId.value = result.value.id
                    d.syncedTracked = false
                    observeProofItem(slotKey, proofOutboxId)
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_PROOF_CAPTURED,
                        distributionEventProps(
                            slotKey,
                            ACTION_CAPTURED,
                            mapOf(
                                PARAM_PROOF_ID to result.value.id,
                                "local_proof_row_id" to result.value.id,
                                PARAM_OUTBOX_ITEM_ID to proofOutboxId,
                                AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to proofOutboxId,
                                AnalyticsEvents.Params.OUTCOME to "success",
                                PARAM_MEDIUM to medium,
                            ),
                        ),
                    )
                    updateSlot(slotKey) {
                        it.copy(
                            isCapturing = false,
                            captured = true,
                            capturedKind = medium,
                            previewPath = captured.localUri,
                            previewIdentity = previewIdentity(slotKey),
                            remoteUrl = null,
                            status = FeedDistributionProofStatus.QUEUED,
                            message = PROOF_QUEUED,
                        )
                    }
                    recomputeCanComplete()
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed distribution $slotKey enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        distributionEventProps(slotKey, ACTION_CAPTURE_FAILED, mapOf(AnalyticsEvents.Params.REASON to result.message)),
                    )
                    updateSlot(slotKey) { it.copy(isCapturing = false, status = FeedDistributionProofStatus.FAILED, message = result.message) }
                }
            }
        }
    }

    private class CapturedMedia(val localUri: String, val mimeType: String, val startMs: Long, val endMs: Long, val captureSource: String)

    // ------------------------------------------------------------------ submit

    private fun markDone() {
        if (completeEnqueueInFlight) return
        val current = _state.value
        // A slot is satisfied by EITHER this phone's own upload or a teammate's server proof id. A
        // pen-session split across operators leaves each phone holding a part, so demanding every
        // slot locally is what made a split pen unsubmittable at all. Optional slots ride along
        // only when captured.
        val slotRefs = linkedMapOf<String, FeedSlotProofSourcePayload>() // mobile-guard:ignore: bounded by the card's slot count
        var missing = false
        current.slots.forEach { slot ->
            val d = draft(slot.slotKey)
            val own = d.proofItemId.value
            val remote = d.remoteRef.value
            when {
                !own.isNullOrBlank() -> slotRefs[slot.slotKey] = FeedSlotProofSourcePayload(outboxItemId = own)
                !remote.isNullOrBlank() -> slotRefs[slot.slotKey] = FeedSlotProofSourcePayload(proofRef = remote)
                slot.required -> missing = true
            }
        }
        if (!current.submitEnabled || missing || slotRefs.isEmpty()) {
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_BLOCKED,
                distributionEventProps(action = ACTION_SUBMIT_BLOCKED, extra = slotStatusProps(current)),
            )
            trackSubmitSources(result = "blocked", current)
            _state.update { it.copy(canComplete = false) }
            return
        }
        val answers = answersJson(current)
        completeEnqueueInFlight = true
        viewModelScope.launch {
            when (
                val result = syncRepository.enqueueFeedDistributionComplete(
                    groupKey = groupKey,
                    // Keyed on the proof SET (plus the answers). Two operators submitting the same pen
                    // mint different keys, but they name the SAME proofs, so the backend's shed-session
                    // natural key makes the second an idempotent no-op rather than a double write (a
                    // DIFFERENT proof set is what raises ErrDistributionAlreadyRecorded). Deliberately
                    // NOT re-keyed on pen identity alone: that would collide with the earlier successful
                    // submit and swallow a legitimate rework re-submit.
                    idempotencyKey = feedDistributionCompleteKey(groupKey, slotRefs, answers),
                    parkId = parkId,
                    shedId = shedId,
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                    // The seeded slots' mirror, for the dispatcher's legacy fields.
                    feedWeightProofOutboxItemId = slotRefs[FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO]?.outboxItemId,
                    distributionProofOutboxItemId = slotRefs[FIELD_FEED_DISTRIBUTION_VIDEO]?.outboxItemId,
                    waterProofOutboxItemId = slotRefs[FIELD_FEED_DISTRIBUTION_WATER_VIDEO]?.outboxItemId,
                    feedWeightProofRef = slotRefs[FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO]?.proofRef,
                    distributionProofRef = slotRefs[FIELD_FEED_DISTRIBUTION_VIDEO]?.proofRef,
                    waterProofRef = slotRefs[FIELD_FEED_DISTRIBUTION_WATER_VIDEO]?.proofRef,
                    slotProofs = slotRefs,
                    answers = answers,
                )
            ) {
                is AppResult.Ok -> {
                    outboxItemId.value = result.value
                    observeOutboxItem(result.value)
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_SUBMITTED,
                        distributionEventProps(
                            action = ACTION_SUBMIT,
                            extra = mapOf(PARAM_OUTBOX_ITEM_ID to result.value) + slotRefProps(),
                        ),
                    )
                    trackSubmitSources(result = "submitted", current)
                    completeEnqueueInFlight = false
                    _state.update { it.copy(canComplete = false) }
                }
                is AppResult.Err -> {
                    completeEnqueueInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "feed distribution complete enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                        distributionEventProps(action = ACTION_SUBMIT_FAILED, extra = mapOf(AnalyticsEvents.Params.REASON to result.message)),
                    )
                    _state.update {
                        it.copy(result = FeedDistributionResultUi(FeedDistributionStatus.FAILED, result.message), canComplete = true)
                    }
                }
            }
        }
    }

    /** The answers as the backend's wire shape: only applicable questions, typed by kind. The
     *  screen keeps a multi pick comma-joined and an "other" text under `<id>_other`. */
    private fun answersJson(st: FeedDistributionUiState): JsonObject = buildJsonObject {
        st.questions.forEach { q ->
            if (!st.appliesTo(q)) return@forEach
            val a = st.answers[q.id].orEmpty().trim()
            when (q.kind) {
                "multi" -> a.split(",").map { it.trim() }.filter { it.isNotBlank() }
                    .takeIf { it.isNotEmpty() }?.let { put(q.id, JsonArray(it.map { v -> JsonPrimitive(v) })) }
                "number" -> a.toDoubleOrNull()?.let { put(q.id, JsonPrimitive(it)) }
                "choice" -> if (a.isNotBlank()) {
                    put(q.id, JsonPrimitive(a))
                    val other = st.answers[q.id + "_other"].orEmpty().trim()
                    if (a == "other" && q.allowOther && other.isNotBlank()) put("${q.id}_other", JsonPrimitive(other))
                }
                else -> if (a.isNotBlank()) put(q.id, JsonPrimitive(a))
            }
        }
    }

    private fun slotSourceLabel(d: SlotDraft): String = when {
        !d.proofItemId.value.isNullOrBlank() -> "local_outbox"
        !d.remoteRef.value.isNullOrBlank() -> "server_ref"
        else -> "missing"
    }

    private fun slotStatusProps(st: FeedDistributionUiState): Map<String, String> = buildMap {
        st.slots.forEach { put("${it.slotKey}_status", it.status.name.lowercase(Locale.ROOT)) }
    }

    private fun slotRefProps(): Map<String, String> = buildMap {
        _state.value.slots.forEach { slot ->
            val d = draft(slot.slotKey)
            put("${slot.slotKey}_outbox_item_id", d.proofItemId.value.orEmpty())
            put("${slot.slotKey}_proof_ref", d.remoteRef.value.orEmpty())
        }
    }

    private fun trackSubmitSources(result: String, st: FeedDistributionUiState) {
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_SUBMIT_SOURCES,
            buildMap {
                put(AnalyticsEvents.Params.SOURCE, SCREEN_FEED_DISTRIBUTION_DETAIL)
                put(AnalyticsEvents.Params.KIND, KIND_DISTRIBUTION)
                put(PARAM_ACTION, "submit_sources")
                put(AnalyticsEvents.Params.SHED_ID, shedId)
                put(PARAM_SHED_LABEL, shedLabel)
                put(PARAM_PARK_LABEL, parkLabel)
                put(PARAM_SESSION_NO, sessionNo.toString())
                put(PARAM_SESSION_LABEL, sessionLabel)
                put(PARAM_WORKFLOW, workflow)
                put(PARAM_PARTITION_LABEL, partitionLabel)
                put(PARAM_GROUP_KEY, groupKey)
                st.slots.forEach { put("${it.slotKey}_source", slotSourceLabel(draft(it.slotKey))) }
                // The seeded trio keeps its historical parameter names for the existing dashboards.
                drafts[FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO]?.let { put(AnalyticsEvents.Params.FEED_WEIGHT_SOURCE, slotSourceLabel(it)) }
                drafts[FIELD_FEED_DISTRIBUTION_VIDEO]?.let { put(AnalyticsEvents.Params.FEED_VIDEO_SOURCE, slotSourceLabel(it)) }
                drafts[FIELD_FEED_DISTRIBUTION_WATER_VIDEO]?.let { put(AnalyticsEvents.Params.WATER_VIDEO_SOURCE, slotSourceLabel(it)) }
                putAll(slotRefProps())
                put(AnalyticsEvents.Params.RESULT, result)
            },
        )
    }

    private fun syncNow() {
        analytics.track(AnalyticsEvents.FEED_DISTRIBUTION_SYNC_TAPPED, distributionEventProps(action = ACTION_REFRESH))
        viewModelScope.launch {
            syncRepository.triggerDrain()
            // Manual sync must also re-fetch teammate/server proof slots (and the card): another
            // operator may have uploaded the missing captures while this screen is open.
            refreshTeammateCaptures(source = "sync_tap")
            pollServerStatusOnce(source = "sync_tap")
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

    private fun observeOutboxItem(itemId: String) {
        statusJob?.cancel()
        statusJob = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    val writeResult = item.toWriteResult(QUEUED_MESSAGE, SYNCED_MESSAGE)
                    if (item.status == SyncItemStatus.SUCCEEDED) {
                        analytics.track(
                            AnalyticsEvents.FEED_DISTRIBUTION_SUBMITTED,
                            distributionEventProps(action = "sync_success", extra = submitTerminalProps(item, "success")),
                        )
                    } else if (item.conflict || item.isDeadLetter) {
                        analytics.track(
                            AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                            distributionEventProps(action = "sync_failure", extra = submitTerminalProps(item, "failure")),
                        )
                    }
                    _state.update {
                        it.copy(
                            result = FeedDistributionResultUi(writeResult.status.toDistributionStatus(), writeResult.message.orEmpty()),
                            canComplete = !writeResult.isCommitted && it.compulsorySlotsFilled,
                        )
                    }
                }
        }
    }

    private fun observeProofItem(slotKey: String, itemId: String) {
        val d = draft(slotKey)
        d.statusJob?.cancel()
        d.statusJob = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item -> updateProofStatus(slotKey, item) }
        }
    }

    // ------------------------------------------------------------------ teammates

    /**
     * Learns which slots ANOTHER operator already recorded for this pen-session, and the card the
     * server judges it against. Best effort by design: a failure leaves every remote ref null and
     * the screen behaves exactly as it did before this read existed. A slot this phone recorded
     * ITSELF always wins -- local capture state is never overwritten by the shared read.
     */
    private fun refreshTeammateCaptures(source: String) {
        if (shedId.isBlank() || workflow.isBlank() || targetDate.isBlank() || sessionNo < 1) return
        if (teammateRefreshInFlight) return
        teammateRefreshInFlight = true
        viewModelScope.launch {
            try {
                // A failed read (null) and an empty read are different answers. Empty is final: the
                // server confirmed no teammate proof exists. Null means offline/timeout/5xx — retry.
                val query = FeedPenSessionCaptureQuery(
                    parkId = parkId,
                    shedId = shedId,
                    // The PEN. Without it the server answers for the shed and would claim a sibling
                    // pen's work as this one's.
                    partitionLabel = partitionLabel,
                    sessionNo = sessionNo,
                    targetDate = targetDate,
                    workflow = workflow,
                )
                var captures = feedRepository.penSessionCaptures(query)
                var retryCount = 0
                for (delayMs in TEAMMATE_CAPTURE_RETRY_DELAYS_MS) {
                    if (captures != null) break
                    delay(delayMs)
                    retryCount += 1
                    captures = feedRepository.penSessionCaptures(query)
                }
                // The read-only gate, the card and the slots arrive in ONE answer — apply the
                // authoritative session status FIRST so a submitted session locks at open, then the
                // card (so an added slot exists before its teammate capture is adopted).
                captures?.sessionStatus?.let { applyLiveStatus(it, source) }
                captures?.card?.let { applyCard(it, CARD_RANK_LIVE, source = source) }
                val slots = captures?.slots
                if (slots == null) {
                    trackTeammateCapturesRead(
                        result = if (retryCount >= TEAMMATE_CAPTURE_RETRY_DELAYS_MS.size) "retry_exhausted" else "failed",
                        slotMask = "none",
                        retryCount = retryCount,
                        source = source,
                    )
                    return@launch
                }
                lastTeammateSlots = slots
                if (slots.isEmpty()) {
                    trackTeammateCapturesRead(result = "success_empty", slotMask = "none", retryCount = retryCount, source = source)
                    return@launch
                }
                adoptTeammateSlots(slots)
                trackTeammateCapturesRead(
                    result = "success_slots",
                    slotMask = slots.map { it.fieldKey }.sorted().joinToString(","),
                    retryCount = retryCount,
                    source = source,
                )
                recomputeCanComplete()
            } finally {
                teammateRefreshInFlight = false
            }
        }
    }

    private fun adoptTeammateSlots(slots: List<FeedDistributionCapturedSlotDto>) {
        slots.forEach { slot ->
            adoptTeammateCapture(slot.fieldKey, slot.proofRef, slot.capturedByName, slot.capturedAt, slot.mimeType)
        }
    }

    private fun trackTeammateCapturesRead(result: String, slotMask: String, retryCount: Int, source: String) {
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_CAPTURES_READ,
            mapOf(
                AnalyticsEvents.Params.RESULT to result,
                AnalyticsEvents.Params.SLOT_MASK to slotMask,
                AnalyticsEvents.Params.RETRY_COUNT to retryCount.coerceAtMost(TEAMMATE_CAPTURE_RETRY_DELAYS_MS.size).toString(),
                AnalyticsEvents.Params.SOURCE to source,
            ),
        )
    }

    /**
     * Marks a slot satisfied by a proof recorded on another phone. Skipped entirely when this phone
     * holds its OWN proof for the slot. The status is SYNCED because the proof is already durable
     * server-side -- that is exactly what makes it submittable. A teammate's re-capture arrives as
     * a newer proofRef on the next read and overwrites the previous adoption.
     */
    private fun adoptTeammateCapture(slotKey: String, proofRef: String, capturedByName: String, capturedAtIso: String, mimeType: String?) {
        if (proofRef.isBlank()) return
        val d = draft(slotKey)
        if (d.locallyCaptured) return
        val slot = _state.value.slot(slotKey)
        val wasAlreadyAdopted = d.remoteRef.value != null
        val isDifferent = d.remoteRef.value != proofRef
        val previewUrlMissing = slot?.remoteUrl == null
        // Same-ref repeats are only a no-op when nothing is left to do; if the preview URL is
        // missing, a repeat read is exactly the retry opportunity.
        if (wasAlreadyAdopted && !isDifferent && !previewUrlMissing) return
        // Accept the proof ref SYNCHRONOUSLY — adoption must never depend on anything else.
        d.remoteRef.value = proofRef
        if (slot == null) return // the card does not (yet) show this slot; the ref waits in the draft
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_TEAMMATE_PROOF_ADOPTED,
            mapOf(
                AnalyticsEvents.Params.KIND to slotKey,
                AnalyticsEvents.Params.SOURCE to "server_teammate_ref",
                AnalyticsEvents.Params.LOCAL_SLOT_STATE to "empty",
            ),
        )
        val message = buildTeammateProofMessage(capturedByName, capturedAtIso)
        updateSlot(slotKey) {
            it.copy(
                captured = true,
                capturedKind = if (mimeType?.startsWith("image/") == true) FeedSlotCaptureKind.PHOTO else if (mimeType?.startsWith("video/") == true) FeedSlotCaptureKind.VIDEO else it.capturedKind,
                status = FeedDistributionProofStatus.SYNCED,
                message = message,
                previewPath = null,
                previewIdentity = previewIdentity(slotKey),
                remoteUrl = feedBackendProofUrl(proofRef),
            )
        }
    }

    private fun buildTeammateProofMessage(capturedByName: String, capturedAtIso: String): String {
        if (capturedByName.isBlank()) return PROOF_RECORDED_BY_TEAMMATE
        val formattedTime = formatCaptureTime(capturedAtIso)
        return if (formattedTime.isNotBlank()) "Captured by $capturedByName · $formattedTime" else "Captured by $capturedByName"
    }

    /** ISO instant -> device-local "h:mm a" today, else "dd/MM/yyyy · h:mm a"; blank on failure. */
    private fun formatCaptureTime(capturedAtIso: String): String {
        return try {
            if (capturedAtIso.isBlank()) return ""
            val instant = Instant.parse(capturedAtIso)
            val deviceZone = ZoneId.systemDefault()
            val localDateTime = instant.atZone(deviceZone).toLocalDateTime()
            val localDate = localDateTime.toLocalDate()
            if (localDate == LocalDate.now(deviceZone)) {
                DateTimeFormatter.ofPattern("h:mm a", Locale.getDefault()).format(localDateTime)
            } else {
                val dateFormat = DateTimeFormatter.ofPattern("dd/MM/yyyy", Locale.getDefault())
                val timeFormat = DateTimeFormatter.ofPattern("h:mm a", Locale.getDefault())
                "${dateFormat.format(localDate)} · ${timeFormat.format(localDateTime)}"
            }
        } catch (e: Exception) {
            "" // Non-fatal: return empty string on parse failure
        }
    }

    private fun refreshTeammatePreviewUrl(slotKey: String) {
        val proofRef = drafts[slotKey]?.remoteRef?.value ?: return
        updateSlot(slotKey) { it.copy(remoteUrl = null, previewIdentity = previewIdentity(slotKey)) }
        updateSlot(slotKey) { it.copy(remoteUrl = feedBackendProofUrl(proofRef), previewIdentity = previewIdentity(slotKey)) }
    }

    private fun previewIdentity(slotKey: String): String {
        val d = draft(slotKey)
        return d.proofItemId.value ?: d.proofRowId.value ?: d.remoteRef.value ?: "feed-distribution:$slotKey"
    }

    // ------------------------------------------------------------------ durable proofs (Room)

    private fun observeDurableProofs() {
        viewModelScope.launch {
            // No partitionLabel: [groupKey] ALREADY carries the pen (feedCaptureGroupKey embeds
            // partitionMatchToken), so this read is pen-scoped by the task id alone. Passing the
            // label as well filtered on proof_capture.partitionKey, which capture() writes as
            // "whole" — on a partitioned shed the read asked for "3" while the row said "whole" and
            // rehydration silently returned nothing. Keep read and write symmetric.
            proofCaptureRepository.observeProofs(groupKey)
                .collect { rows ->
                    lastProofRows = rows
                    hydrateSlotsFromRows(rows)
                    recomputeCanComplete()
                }
        }
    }

    private fun hydrateSlotsFromRows(rows: List<ProofCaptureRow>) {
        if (rows.isEmpty()) return
        _state.value.slots.forEach { slot -> hydrateSlotFromProof(slot.slotKey, rows.latestFor(slot.slotKey)) }
    }

    private fun hydrateSlotFromProof(slotKey: String, row: ProofCaptureRow?) {
        if (row == null) return
        val d = draft(slotKey)
        row.outboxItemId?.takeIf { it.isNotBlank() }?.let { outboxId ->
            if (d.proofItemId.value != outboxId || d.statusJob == null) {
                d.proofItemId.value = outboxId
                observeProofItem(slotKey, outboxId)
            }
        }
        d.proofRowId.value = row.id
        val status = row.toProofStatus()
        val preview = row.previewUri()
        updateSlot(slotKey) {
            it.copy(
                captured = true,
                capturedKind = if (row.mimeType.startsWith("image/")) FeedSlotCaptureKind.PHOTO else FeedSlotCaptureKind.VIDEO,
                previewPath = preview ?: it.previewPath,
                previewIdentity = previewIdentity(slotKey),
                status = status,
                message = row.toProofMessage(status, PROOF_QUEUED, PROOF_UPLOADING, PROOF_SYNCED, PROOF_FAILED),
            )
        }
    }

    private fun updateProofStatus(slotKey: String, item: SyncQueueItem) {
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
        updateSlot(slotKey) {
            it.copy(
                captured = it.captured || item.localFilePath != null,
                previewPath = item.localFilePath ?: it.previewPath,
                previewIdentity = previewIdentity(slotKey),
                status = proofStatus,
                message = message,
            )
        }
        val d = draft(slotKey)
        if (proofStatus == FeedDistributionProofStatus.SYNCED && !d.syncedTracked) {
            d.syncedTracked = true
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_PROOF_UPLOAD_SYNCED,
                distributionEventProps(slotKey, ACTION_UPLOAD_SYNCED, proofUploadTerminalProps(slotKey, item, outcome = "sync_success")),
            )
        } else if (proofStatus == FeedDistributionProofStatus.FAILED) {
            analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
                distributionEventProps(slotKey, ACTION_UPLOAD_FAILED, proofUploadTerminalProps(slotKey, item, outcome = "sync_terminal_failure")),
            )
        }
        recomputeCanComplete()
    }

    private fun proofUploadTerminalProps(slotKey: String, item: SyncQueueItem, outcome: String): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.OUTCOME, outcome)
        put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, item.id)
        put(PARAM_OUTBOX_ITEM_ID, item.id)
        item.lastError?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(96)) }
        val d = draft(slotKey)
        d.proofRowId.value?.takeIf { it.isNotBlank() }?.let {
            put(PARAM_PROOF_ID, it)
            put("local_proof_row_id", it)
        }
        d.remoteRef.value?.takeIf { it.isNotBlank() }?.let { put("server_proof_id", it) }
    }

    private fun submitTerminalProps(item: SyncQueueItem, outcome: String): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.OUTCOME, outcome)
        put(PARAM_OUTBOX_ITEM_ID, item.id)
        put("submit_outbox_id", item.id)
        put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, item.id)
        item.lastError?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(96)) }
        putAll(slotRefProps())
        val ds = _state.value.slots.map { draft(it.slotKey) }
        val proofOutboxIds = ds.mapNotNull { it.proofItemId.value?.takeIf { v -> v.isNotBlank() } }
        val localProofRowIds = ds.mapNotNull { it.proofRowId.value?.takeIf { v -> v.isNotBlank() } }
        val serverProofIds = ds.mapNotNull { it.remoteRef.value?.takeIf { v -> v.isNotBlank() } }
        put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, proofOutboxIds.firstOrNull().orEmpty())
        put("local_proof_row_id", localProofRowIds.firstOrNull().orEmpty())
        put("server_proof_id", serverProofIds.firstOrNull().orEmpty())
        put("proof_outbox_item_ids", proofOutboxIds.joinToString(","))
        put("local_proof_row_ids", localProofRowIds.joinToString(","))
        put("server_proof_ids", serverProofIds.joinToString(","))
    }

    private fun trackCaptureFailure(slotKey: String, reason: String) {
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_FAILURE,
            distributionEventProps(slotKey, ACTION_CAPTURE_FAILED, mapOf(AnalyticsEvents.Params.REASON to reason)),
        )
    }

    private fun List<ProofCaptureRow>.latestFor(fieldKey: String): ProofCaptureRow? =
        filter { it.fieldKey == fieldKey && it.syncStatus != CaptureSyncStatus.FAILED }
            .maxByOrNull { it.capturedAtMs }

    private fun trackReuploadTapped(slotKey: String) {
        analytics.track(
            AnalyticsEvents.FEED_DISTRIBUTION_PROOF_REUPLOAD_TAPPED,
            distributionEventProps(slotKey, ACTION_RE_RECORD_PROOF),
        )
    }

    /** Replaces one slot's UI row by key; a key the card does not show is a no-op. */
    private inline fun updateSlot(slotKey: String, crossinline transform: (FeedDistributionSlotUi) -> FeedDistributionSlotUi) {
        _state.update { st ->
            if (st.slots.none { it.slotKey == slotKey }) st
            else st.copy(slots = st.slots.map { if (it.slotKey == slotKey) transform(it) else it })
        }
    }

    private fun recomputeCanComplete() {
        _state.update {
            val committed = it.result?.let { r -> r.status == FeedDistributionStatus.SYNCED || r.status == FeedDistributionStatus.QUEUED } ?: false
            it.copy(
                canComplete = it.compulsorySlotsFilled &&
                    it.slots.filter { s -> s.captured }.all { s -> s.status.isQueuedForSubmit() } &&
                    !committed,
            )
        }
    }

    /**
     * The submit key, derived from the pen-session plus the identity of each proof in the set (in
     * card order) and the answers. Each slot contributes whichever reference this phone holds: its
     * own outbox id, or the SERVER proof id of a proof a teammate recorded.
     */
    private fun feedDistributionCompleteKey(groupKey: String, slotRefs: Map<String, FeedSlotProofSourcePayload>, answers: JsonObject): String {
        val canonical = buildList {
            add(groupKey)
            slotRefs.forEach { (key, ref) -> add("$key=${ref.outboxItemId ?: ref.proofRef.orEmpty()}") }
            if (answers.isNotEmpty()) add("answers=" + answers.toString())
        }.joinToString("|")
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

    private fun distributionEventProps(
        slotKey: String? = null,
        action: String,
        extra: Map<String, String> = emptyMap(),
    ): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.SOURCE, SCREEN_FEED_DISTRIBUTION_DETAIL)
        put(AnalyticsEvents.Params.KIND, slotKey?.let(::analyticsKind) ?: KIND_DISTRIBUTION)
        put(PARAM_ACTION, action)
        put(AnalyticsEvents.Params.SHED_ID, shedId)
        put(PARAM_SHED_LABEL, shedLabel)
        put(PARAM_PARK_LABEL, parkLabel)
        put(PARAM_SESSION_NO, sessionNo.toString())
        put(PARAM_SESSION_LABEL, sessionLabel)
        put(PARAM_WORKFLOW, workflow)
        put(PARAM_PARTITION_LABEL, partitionLabel)
        put(PARAM_GROUP_KEY, groupKey)
        slotKey?.let { put(AnalyticsEvents.Params.FIELD, it) }
        putAll(extra)
    }

    /** The seeded slots keep their historical analytics kinds; a new slot reports its key. */
    private fun analyticsKind(slotKey: String): String = when (slotKey) {
        FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO -> "feed_weight_photo"
        FIELD_FEED_DISTRIBUTION_VIDEO -> "feed_video"
        FIELD_FEED_DISTRIBUTION_WATER_VIDEO -> "water_video"
        else -> slotKey
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
        const val ARG_PARK_LABEL = "park_label"
        const val ARG_PARTITION_LABEL = "partition_label"
        const val ARG_LIFECYCLE_STATUS = "lifecycle_status"

        /** Retry cadence for the teammate-capture read; a farm-WiFi blip usually outlives one attempt. */
        private val TEAMMATE_CAPTURE_RETRY_DELAYS_MS = longArrayOf(2_000L, 5_000L, 10_000L)

        /** How often [startServerStatusPolling] re-checks this session's lifecycle status directly
         *  from the server while the screen stays open. */
        private const val SERVER_STATUS_POLL_INTERVAL_MS = 30_000L

        /** Bound for [startServerStatusPolling] — see its kdoc for why this cannot be unbounded. */
        private const val MAX_SERVER_STATUS_POLLS = 2_880

        private const val CARD_RANK_SEEDED = 0
        private const val CARD_RANK_CACHED = 1
        private const val CARD_RANK_LIVE = 2

        private const val KEY_OUTBOX_ITEM_ID = "feedDistribution.outboxItemId"
        private const val KEY_SLOT_PREFIX = "feedDistribution.slot."

        /** The seeded card's slot keys = the proof register field_keys this app has always stamped. */
        internal const val FIELD_FEED_DISTRIBUTION_FEED_WEIGHT_PHOTO = "feed_distribution_feed_weight_photo"
        internal const val FIELD_FEED_DISTRIBUTION_VIDEO = "feed_distribution_video"
        internal const val FIELD_FEED_DISTRIBUTION_WATER_VIDEO = "feed_distribution_water_video"

        private const val KIND_DISTRIBUTION = "distribution"
        private const val SCREEN_FEED_DISTRIBUTION_DETAIL = "feed_distribution_complete"
        private const val ACTION_DETAIL_OPENED = "detail_opened"
        private const val ACTION_CARD_APPLIED = "card_applied"
        private const val ACTION_RECORD_PROOF = "record_proof"
        private const val ACTION_RE_RECORD_PROOF = "re_record_proof"
        private const val ACTION_CAPTURED = "captured"
        private const val ACTION_CAPTURE_FAILED = "capture_failed"
        private const val ACTION_UPLOAD_SYNCED = "upload_synced"
        private const val ACTION_UPLOAD_FAILED = "upload_failed"
        private const val ACTION_REFRESH = "refresh"
        private const val ACTION_SUBMIT = "submit"
        private const val ACTION_SUBMIT_BLOCKED = "submit_blocked"
        private const val ACTION_SUBMIT_FAILED = "submit_failed"
        private const val PARAM_ACTION = "action"
        private const val PARAM_SHED_LABEL = "shed_label"
        private const val PARAM_PARK_LABEL = "park_label"
        private const val PARAM_SESSION_NO = "session_no"
        private const val PARAM_SESSION_LABEL = "session_label"
        private const val PARAM_WORKFLOW = "workflow"
        private const val PARAM_PARTITION_LABEL = "partition_label"
        private const val PARAM_GROUP_KEY = "group_key"
        private const val PARAM_PROOF_ID = "proof_id"
        private const val PARAM_OUTBOX_ITEM_ID = "outbox_item_id"
        private const val PARAM_MEDIUM = "medium"
        private const val PARAM_SOP_VERSION = "sop_version"
        private const val PARAM_SLOT_COUNT = "slot_count"
        private const val PARAM_QUESTION_COUNT = "question_count"
        private const val QUEUED_MESSAGE = "Sent for verification. A verifier will review the proofs."
        private const val SYNCED_MESSAGE = "Sent. Waiting for verifier approval before this feeding is counted."
        private const val PROOF_QUEUED = "Proof saved on this phone. It will upload automatically."
        private const val PROOF_UPLOADING = "Proof upload is in progress."
        private const val PROOF_SYNCED = "Proof is ready."
        private const val PROOF_RECORDED_BY_TEAMMATE = "Already recorded by another operator."
        private const val PROOF_FAILED = "Couldn't save that proof. Please capture it again."
    }
}

private fun feedBackendProofUrl(proofRef: String): String =
    BuildConfig.API_BASE_URL.trimEnd('/') + "/app/proofs/${proofRef.trim()}/download"
