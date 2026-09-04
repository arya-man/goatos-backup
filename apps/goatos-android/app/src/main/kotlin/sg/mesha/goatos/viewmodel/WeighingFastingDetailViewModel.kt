package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.weighing.WeighingFastingRepository
import sg.mesha.goatos.feature.weighing.WeighingFastingDetailEvent
import sg.mesha.goatos.feature.weighing.WeighingFastingDetailUiState
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotKind
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotStatus
import sg.mesha.goatos.feature.weighing.WeighingFastingSlotUi
import sg.mesha.goatos.feature.weighing.R as WeighingR
import sg.mesha.goatos.ui.Routes
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * The feed & water removal recording screen for ONE SHED (maintainer correction #2, 2026-09-03:
 * the list serves one card per shed and submit is PER SHED). The screen holds exactly two capture
 * slots — this shed's live-camera feed-removal and water-removal videos — and one submit that
 * sends THIS shed's pair. The verifier reviews one item per shed; a rejection hands back this
 * shed alone.
 *
 * The sync discipline copies [FeedDistributionCompleteViewModel], the canonical gated-completion
 * template: each clip goes through the ONE shared proof pipeline
 * ([ProofCaptureRepository.captureReplacingLatest], per-clip idempotency key, per-field cap of 1)
 * on its OWN per-slot upload group, and the submit rides the outbox
 * ([SyncRepository.enqueueWeighingFastingSubmit]) on a group scoped to the (fasting task,
 * campaign shed) pair, carrying the two clips' proof OUTBOX ITEM IDS — never proof ids — which
 * the sync engine resolves at drain time. A card in `rework` resets to Record video and requires
 * FRESH clips. No direct API call anywhere: the card is worked at night in a shed, where the
 * network is worst.
 *
 * The card's status itself is Room truth ([WeighingFastingRepository.observeCard]): a roll-forward
 * at midnight or a verifier's verdict lands as a re-read, never as client-derived clock logic.
 */
@HiltViewModel
class WeighingFastingDetailViewModel @Inject constructor(
    private val fastingRepository: WeighingFastingRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val fastingTaskId: String = savedStateHandle.get<String>(Routes.WEIGHING_FASTING_TASK_ARG).orEmpty()
    private val campaignShedId: String = savedStateHandle.get<String>(Routes.WEIGHING_FASTING_SHED_ARG).orEmpty()

    /** First-paint title from the tapped card; superseded by the Room card the moment it emits. */
    private val titleHint: String = savedStateHandle.get<String>(Routes.WEIGHING_FASTING_TITLE_ARG).orEmpty()

    private val submitOutboxItemId = DraftOutboxItemId(savedStateHandle, KEY_SUBMIT_OUTBOX_ITEM_ID)

    private var submitEnqueueInFlight = false
    private val proofJobs = mutableMapOf<WeighingFastingSlotKind, Job>()
    private var submitJob: Job? = null
    private var refreshInFlight = false

    private val _state = MutableStateFlow(
        WeighingFastingDetailUiState(
            title = titleHint,
            feedSlot = emptySlot(WeighingFastingSlotKind.FEED),
            waterSlot = emptySlot(WeighingFastingSlotKind.WATER),
            submitQueued = submitOutboxItemId.value != null,
        ),
    )
    val state: StateFlow<WeighingFastingDetailUiState> = _state.asStateFlow()

    init {
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_REMOVAL_CARD_OPENED,
            mapOf(AnalyticsEvents.Params.SOURCE to "detail"),
        )
        observeCard()
        submitOutboxItemId.value?.let(::observeSubmitItem)
        WeighingFastingSlotKind.entries.forEach { kind ->
            slotItemId(kind)?.let { observeProofItem(kind, it) }
        }
        recomputeSubmit()
        refresh()
    }

    fun onEvent(event: WeighingFastingDetailEvent) {
        when (event) {
            is WeighingFastingDetailEvent.RecordSlot -> recordSlot(event.kind)
            WeighingFastingDetailEvent.Submit -> submit()
            WeighingFastingDetailEvent.Refresh -> refresh()
            WeighingFastingDetailEvent.DismissMessage -> _state.update { it.copy(message = null) }
        }
    }

    /** Room is the SSOT for this shed card; the network refresh only rewrites Room. */
    private fun observeCard() {
        viewModelScope.launch {
            fastingRepository.observeCard(fastingTaskId, campaignShedId).filterNotNull().collect { card ->
                val dto = card.dto
                val status = dto.status.trim().lowercase()
                val readOnly = status == STATUS_PENDING_VERIFICATION || status == STATUS_COMPLETED
                val todayIst = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
                // The card came BACK from the verifier while a queued submit marker is still
                // held: that submit's clips were the judged act. Hand the card back — drop the
                // queued marker and reset both slots so THIS shed requires FRESH clips.
                if (status == STATUS_REWORK && submitOutboxItemId.value != null) {
                    submitOutboxItemId.value = null
                    submitJob?.cancel()
                    clearSlots()
                }
                if (readOnly) {
                    // A submitted/approved card renders its clips from the server (a reinstall
                    // holds no local file). Enrichment only — fetched async, best effort, retried
                    // on the next Room emit; the feed-distribution screen's exact behaviour.
                    fetchRemotePreview(WeighingFastingSlotKind.FEED, dto.feedProofRef.orEmpty())
                    fetchRemotePreview(WeighingFastingSlotKind.WATER, dto.waterProofRef.orEmpty())
                }
                _state.update { current ->
                    current.copy(
                        title = dto.subjectLabel.ifBlank { current.title },
                        dateLabel = if (dto.removalBusinessDate == todayIst) {
                            appContext.getString(WeighingR.string.weighing_removal_date_tonight)
                        } else {
                            farmRemovalDateLabel(dto.removalBusinessDate)
                        },
                        status = status,
                        isReadOnly = readOnly,
                        lockNotice = when (status) {
                            STATUS_PENDING_VERIFICATION ->
                                appContext.getString(WeighingR.string.weighing_removal_status_in_review)
                            STATUS_COMPLETED ->
                                appContext.getString(WeighingR.string.weighing_removal_status_done)
                            else -> ""
                        },
                        // The verifier's own sentence about THIS shed, verbatim, only while sent back.
                        reworkReason = if (status == STATUS_REWORK) dto.reworkReason.orEmpty() else "",
                        submitQueued = submitOutboxItemId.value != null && current.submitQueued,
                        feedSlot = mergedSlot(WeighingFastingSlotKind.FEED, current),
                        waterSlot = mergedSlot(WeighingFastingSlotKind.WATER, current),
                    )
                }
                recomputeSubmit()
            }
        }
    }

    private fun fetchRemotePreview(kind: WeighingFastingSlotKind, proofRef: String) {
        if (proofRef.isBlank()) return
        if (_state.value.slotOf(kind).let { it.remoteUrl != null || it.previewPath != null }) return
        viewModelScope.launch {
            val url = fastingRepository.fetchProofDownloadUrl(proofRef) ?: return@launch
            updateSlot(kind) {
                it.copy(
                    captured = true,
                    status = WeighingFastingSlotStatus.SYNCED,
                    statusLabel = PROOF_SYNCED_LABEL,
                    remoteUrl = url,
                )
            }
        }
    }

    /** One slot, merging any live local capture state over a clean baseline. */
    private fun mergedSlot(
        kind: WeighingFastingSlotKind,
        current: WeighingFastingDetailUiState,
    ): WeighingFastingSlotUi {
        // Reuse the live slot only while something real backs it (a capture in flight, a
        // failure to show, or a persisted outbox item). A card whose slots were just reset
        // for rework therefore falls through to a clean "Record video" slot.
        val live = current.slotOf(kind)
            .takeIf { it.fieldKey == fieldKey(kind) }
            ?.takeIf {
                it.busy ||
                    it.status == WeighingFastingSlotStatus.FAILED ||
                    slotItemId(kind) != null
            }
        return live ?: emptySlot(kind)
    }

    private fun emptySlot(kind: WeighingFastingSlotKind): WeighingFastingSlotUi = WeighingFastingSlotUi(
        fieldKey = fieldKey(kind),
        kind = kind,
        title = appContext.getString(
            if (kind == WeighingFastingSlotKind.FEED) {
                WeighingR.string.weighing_removal_feed_slot
            } else {
                WeighingR.string.weighing_removal_water_slot
            },
        ),
        hint = appContext.getString(
            if (kind == WeighingFastingSlotKind.FEED) {
                WeighingR.string.weighing_removal_feed_hint
            } else {
                WeighingR.string.weighing_removal_water_hint
            },
        ),
        captured = slotItemId(kind) != null,
        // This device's own recording survives process death alongside the outbox item id, so
        // the preview comes back with the draft (the feed-distribution screen's behaviour).
        previewPath = slotPreviewPath(kind),
    )

    private fun refresh() {
        if (refreshInFlight) return
        refreshInFlight = true
        _state.update { it.copy(isSyncing = true) }
        viewModelScope.launch {
            try {
                when (val result = fastingRepository.refresh()) {
                    is AppResult.Ok -> Unit
                    // The cached card stays on screen; a refresh miss at night in a shed is
                    // expected, not an error banner.
                    is AppResult.Err -> crashReporter.recordException(
                        result.cause ?: IllegalStateException(result.message),
                        "weighing removal detail refresh failed",
                    )
                }
                syncRepository.triggerDrain()
            } finally {
                refreshInFlight = false
                _state.update { it.copy(isSyncing = false) }
            }
        }
    }

    private fun recordSlot(kind: WeighingFastingSlotKind) {
        val current = _state.value
        if (current.isReadOnly || current.submitQueued) return
        val slotUi = current.slotOf(kind)
        if (slotUi.busy) return
        updateSlot(kind) { it.copy(busy = true) }
        viewModelScope.launch {
            try {
                val captured = try {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            title = slotUi.title,
                            // The SHED the clip must show — the backend-owned card title, which
                            // names the shed — front and centre.
                            primaryTag = current.title.ifBlank { slotUi.title },
                            workLabel = slotUi.title,
                            headerTitle = appContext.getString(WeighingR.string.weighing_removal_screen_title),
                        ),
                    )
                } catch (error: Exception) {
                    crashReporter.recordException(error, "weighing removal video capture failed")
                    trackFailure(slotUi.fieldKey, "camera_exception")
                    null
                }
                if (captured == null) {
                    updateSlot(kind) { it.copy(busy = false) }
                    return@launch
                }
                val fieldKey = fieldKey(kind)
                val slot = EvidenceSlot(
                    identity = ProofIdentity(
                        flow = ProofFlow.WEIGHING_FASTING,
                        taskId = fastingTaskId,
                        subjectKey = fieldKey,
                    ),
                    fieldKey = fieldKey,
                )
                when (
                    val result = proofCaptureRepository.captureReplacingLatest(
                        slot = slot,
                        subject = ProofSubject.TASK,
                        subjectId = fastingTaskId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = "${slotUi.title} · ${current.title}".trim(' ', '·'),
                        scopeType = "task",
                        scopeId = fastingTaskId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = null,
                        proofPolicy = weighingFastingProofPolicy(captured.captureSource),
                        awaitUploadEnqueue = true,
                        // PER-SLOT upload group: each clip is independent field work, so one
                        // backed-off upload must never strand the other as "waiting". The submit
                        // waits for both by resolving their outbox ids at drain time instead.
                        uploadGroupKey = proofUploadGroupKey(fieldKey),
                    )
                ) {
                    is AppResult.Ok -> {
                        val proofOutboxId = result.value.outboxItemId
                        if (proofOutboxId.isNullOrBlank()) {
                            trackFailure(fieldKey, "missing_upload_outbox")
                            updateSlot(kind) {
                                it.copy(
                                    busy = false,
                                    status = WeighingFastingSlotStatus.FAILED,
                                    statusLabel = appContext.getString(WeighingR.string.weighing_removal_record_again),
                                )
                            }
                            return@launch
                        }
                        setSlotItemId(kind, proofOutboxId)
                        setSlotPreviewPath(kind, captured.localUri)
                        observeProofItem(kind, proofOutboxId)
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_REMOVAL_SLOT_CAPTURED,
                            mapOf(AnalyticsEvents.Params.FIELD to fieldKey),
                        )
                        updateSlot(kind) {
                            it.copy(
                                busy = false,
                                captured = true,
                                status = WeighingFastingSlotStatus.QUEUED,
                                statusLabel = PROOF_QUEUED_LABEL,
                                previewPath = captured.localUri,
                            )
                        }
                        recomputeSubmit()
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "weighing removal proof enqueue failed") }
                        trackFailure(fieldKey, result.message)
                        updateSlot(kind) {
                            it.copy(
                                busy = false,
                                status = WeighingFastingSlotStatus.FAILED,
                                statusLabel = result.message,
                            )
                        }
                    }
                }
            } finally {
                updateSlot(kind) { it.copy(busy = false) }
            }
        }
    }

    private fun submit() {
        if (submitEnqueueInFlight) return
        val current = _state.value
        // BOTH of this shed's clips, always: a submit missing one would be refused server-side
        // anyway (proof-shaped 422, backend farm copy), so the block is stated here before any
        // queueing.
        val feedItem = slotItemId(WeighingFastingSlotKind.FEED)
        val waterItem = slotItemId(WeighingFastingSlotKind.WATER)
        if (current.isReadOnly || current.submitQueued || feedItem == null || waterItem == null) {
            recomputeSubmit()
            return
        }
        submitEnqueueInFlight = true
        viewModelScope.launch {
            when (
                val result = syncRepository.enqueueWeighingFastingSubmit(
                    // (task, shed)-grain group: two submits of the same shed card drain strictly
                    // in order. The proofs are NOT on this group — each rides its own per-slot
                    // upload lane and the dispatcher resolves them by outbox id.
                    groupKey = submitGroupKey(),
                    // STABLE per (task, shed, clip pair): a retry replays for free; a post-rework
                    // re-shoot names new proofs and is a genuinely new act under a new key.
                    idempotencyKey = submitIdempotencyKey(feedItem, waterItem),
                    fastingTaskId = fastingTaskId,
                    campaignShedId = campaignShedId,
                    feedProofOutboxItemId = feedItem,
                    waterProofOutboxItemId = waterItem,
                )
            ) {
                is AppResult.Ok -> {
                    submitOutboxItemId.value = result.value
                    observeSubmitItem(result.value)
                    analytics.track(
                        AnalyticsEventsWeighing.WEIGHING_REMOVAL_SUBMITTED,
                        mapOf(AnalyticsEvents.Params.STATUS to current.status),
                    )
                    submitEnqueueInFlight = false
                    _state.update {
                        it.copy(
                            submitQueued = true,
                            submitEnabled = false,
                            message = SUBMIT_QUEUED_MESSAGE,
                        )
                    }
                }
                is AppResult.Err -> {
                    submitEnqueueInFlight = false
                    result.cause?.let { crashReporter.recordException(it, "weighing removal submit enqueue failed") }
                    trackFailure("submit", result.message)
                    _state.update { it.copy(message = result.message) }
                }
            }
        }
    }

    private fun observeProofItem(kind: WeighingFastingSlotKind, itemId: String) {
        proofJobs.remove(kind)?.cancel()
        proofJobs[kind] = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item -> applyProofItem(kind, item) }
        }
    }

    private fun applyProofItem(kind: WeighingFastingSlotKind, item: SyncQueueItem) {
        val status = when {
            item.status == SyncItemStatus.SUCCEEDED -> WeighingFastingSlotStatus.SYNCED
            item.isTerminalFailure -> WeighingFastingSlotStatus.FAILED
            item.status == SyncItemStatus.IN_FLIGHT -> WeighingFastingSlotStatus.UPLOADING
            else -> WeighingFastingSlotStatus.QUEUED
        }
        updateSlot(kind) {
            it.copy(
                captured = true,
                status = status,
                statusLabel = when (status) {
                    WeighingFastingSlotStatus.SYNCED -> PROOF_SYNCED_LABEL
                    WeighingFastingSlotStatus.UPLOADING -> PROOF_UPLOADING_LABEL
                    // The server's (or transport's) own reason, verbatim where one exists.
                    WeighingFastingSlotStatus.FAILED -> item.lastError ?: PROOF_FAILED_LABEL
                    else -> PROOF_QUEUED_LABEL
                },
            )
        }
        recomputeSubmit()
    }

    private fun observeSubmitItem(itemId: String) {
        submitJob?.cancel()
        submitJob = viewModelScope.launch {
            syncRepository.observeItem(itemId)
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    val writeResult = item.toWriteResult(SUBMIT_QUEUED_MESSAGE, SUBMIT_SYNCED_MESSAGE)
                    _state.update {
                        it.copy(
                            submitQueued = writeResult.isCommitted,
                            message = writeResult.message,
                            submitEnabled = false,
                        )
                    }
                    if (writeResult.isCorrectable) {
                        // A terminal refusal hands the card back: drop the queued marker so the
                        // operator can re-record and submit again under a new proof set.
                        submitOutboxItemId.value = null
                        recomputeSubmit()
                    }
                }
        }
    }

    private fun recomputeSubmit() {
        _state.update { current ->
            // BOTH of this shed's clips recorded (their outbox items exist) — nothing else gates
            // the button; the drain waits for the uploads by resolving the outbox ids.
            val bothReady = slotItemId(WeighingFastingSlotKind.FEED) != null &&
                slotItemId(WeighingFastingSlotKind.WATER) != null
            val enabled = bothReady && !current.isReadOnly && !current.submitQueued
            current.copy(
                submitEnabled = enabled,
                submitBlockedReason = when {
                    enabled || current.submitQueued || current.isReadOnly -> ""
                    else -> SUBMIT_BLOCKED_BOTH_VIDEOS
                },
            )
        }
    }

    private fun updateSlot(
        kind: WeighingFastingSlotKind,
        transform: (WeighingFastingSlotUi) -> WeighingFastingSlotUi,
    ) {
        _state.update { current ->
            if (kind == WeighingFastingSlotKind.FEED) {
                current.copy(feedSlot = transform(current.feedSlot))
            } else {
                current.copy(waterSlot = transform(current.waterSlot))
            }
        }
    }

    // ------------------------------------------------------------------ per-slot draft identity

    /** Field keys are part of the durable capture identity — never rename casually. The shed id
     *  stays in the key exactly as the per-shed sections carried it. */
    internal fun fieldKey(kind: WeighingFastingSlotKind): String =
        if (kind == WeighingFastingSlotKind.FEED) {
            "${FIELD_FEED_VIDEO_PREFIX}$campaignShedId"
        } else {
            "${FIELD_WATER_VIDEO_PREFIX}$campaignShedId"
        }

    private fun slotStateKey(kind: WeighingFastingSlotKind): String =
        "$KEY_SLOT_PROOF_ITEM_ID_PREFIX:$campaignShedId:${kind.name.lowercase()}"

    private fun slotItemId(kind: WeighingFastingSlotKind): String? =
        savedStateHandle.get<String>(slotStateKey(kind))?.takeIf { it.isNotBlank() }

    private fun setSlotItemId(kind: WeighingFastingSlotKind, itemId: String?) {
        val key = slotStateKey(kind)
        if (itemId == null) savedStateHandle.remove<String>(key) else savedStateHandle[key] = itemId
    }

    private fun slotPreviewKey(kind: WeighingFastingSlotKind): String =
        "$KEY_SLOT_PREVIEW_PATH_PREFIX:$campaignShedId:${kind.name.lowercase()}"

    private fun slotPreviewPath(kind: WeighingFastingSlotKind): String? =
        savedStateHandle.get<String>(slotPreviewKey(kind))?.takeIf { it.isNotBlank() }

    private fun setSlotPreviewPath(kind: WeighingFastingSlotKind, path: String?) {
        val key = slotPreviewKey(kind)
        if (path == null) savedStateHandle.remove<String>(key) else savedStateHandle[key] = path
    }

    private fun clearSlots() {
        WeighingFastingSlotKind.entries.forEach { kind ->
            setSlotItemId(kind, null)
            setSlotPreviewPath(kind, null)
            proofJobs.remove(kind)?.cancel()
        }
        _state.update {
            it.copy(
                feedSlot = emptySlot(WeighingFastingSlotKind.FEED),
                waterSlot = emptySlot(WeighingFastingSlotKind.WATER),
            )
        }
    }

    /** Each clip drains on its OWN lane (commit e8a540e4f's parallel-slot contract); the submit
     *  never shares them — it waits by resolving the outbox ids at dispatch instead. */
    internal fun proofUploadGroupKey(fieldKey: String): String = "${submitGroupKey()}:$fieldKey"

    /** Scoped to the (fasting task, campaign shed) pair — the per-shed submit's own lane. */
    internal fun submitGroupKey(): String = "weighing-fasting:$fastingTaskId:$campaignShedId"

    /** STABLE across retries, NEW after any re-shoot: the shed plus its two clip outbox ids. */
    internal fun submitIdempotencyKey(feedItemId: String, waterItemId: String): String =
        "weighing-fasting-submit:$fastingTaskId:$campaignShedId:$feedItemId|$waterItemId"

    private fun trackFailure(fieldKey: String, reason: String) {
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_REMOVAL_FAILURE,
            mapOf(
                AnalyticsEvents.Params.FIELD to fieldKey,
                AnalyticsEvents.Params.REASON to reason.take(MAX_REASON_CHARS),
            ),
        )
    }

    companion object {
        /** Per-shed field keys: `weighing_fasting_feed_video_<campaignShedId>` etc. — part of the
         *  durable capture identity, never renamed casually. */
        const val FIELD_FEED_VIDEO_PREFIX = "weighing_fasting_feed_video_"
        const val FIELD_WATER_VIDEO_PREFIX = "weighing_fasting_water_video_"

        private const val KEY_SLOT_PROOF_ITEM_ID_PREFIX = "weighing_fasting_proof_item_id"
        private const val KEY_SLOT_PREVIEW_PATH_PREFIX = "weighing_fasting_proof_preview_path"
        private const val KEY_SUBMIT_OUTBOX_ITEM_ID = "weighing_fasting_submit_outbox_item_id"

        private const val STATUS_PENDING_VERIFICATION = "pending_verification"
        private const val STATUS_COMPLETED = "completed"
        private const val STATUS_REWORK = "rework"

        private const val MAX_REASON_CHARS = 96

        // Device-local pre-sync status copy (mobile-contract:ignore: device-local outbox state
        // has no backend contract to carry it; server copy rides lastError verbatim above).
        private const val PROOF_QUEUED_LABEL = "Video saved. It will upload on its own."
        private const val PROOF_UPLOADING_LABEL = "Video on its way…"
        private const val PROOF_SYNCED_LABEL = "Video sent"
        private const val PROOF_FAILED_LABEL = "Video didn't go through. Record again."
        private const val SUBMIT_BLOCKED_BOTH_VIDEOS = "Record both videos to finish."
        private const val SUBMIT_QUEUED_MESSAGE = "Saved. It will be sent when the network allows."
        private const val SUBMIT_SYNCED_MESSAGE = "Sent. The videos will be checked later."
    }
}

private fun WeighingFastingDetailUiState.slotOf(kind: WeighingFastingSlotKind): WeighingFastingSlotUi =
    if (kind == WeighingFastingSlotKind.FEED) feedSlot else waterSlot

/** One proof per slot: the shed's feed clip and water clip are distinct required steps, never
 *  repeat takes of one thing — the same per-field cap of 1 feed distribution uses. */
internal fun weighingFastingProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "task_video",
        subjectScope = ProofSubject.TASK.wireValue,
        expectedSubjects = listOf(ProofSubject.TASK.wireValue),
        captureSource = captureSource,
        maximumCountPerField = 1,
    )
