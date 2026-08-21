package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.PcCareRepository
import sg.mesha.goatos.core.data.PcCareScanOutcome
import sg.mesha.goatos.core.data.cache.PcCareAnimalRowEntity
import sg.mesha.goatos.core.data.cache.PcCareScanStatus
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.EvidenceSlot
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofFlow
import sg.mesha.goatos.core.data.capture.ProofIdentity
import sg.mesha.goatos.core.data.capture.ProofProcessingStatus
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.pcCareTaskGroupKey
import sg.mesha.goatos.core.network.dto.PcCareAnimalSlotDto
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.core.network.dto.PcCareTaskDto
import sg.mesha.goatos.feature.pccare.PcCareAnimalUi
import sg.mesha.goatos.feature.pccare.PcCareSlotChipUi
import sg.mesha.goatos.feature.pccare.PcCareSlotState
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent
import sg.mesha.goatos.feature.pccare.PcCareTaskUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import javax.inject.Inject

/**
 * ONE PC Care task's capture screen state holder (module pc_care, maintainer decision 2026-08-21).
 *
 * Room is the single source of truth: the screen renders from three observed flows — the cached
 * task detail, the scanned-animal rows, and this phone's own proof capture rows — merged per slot.
 * MERGE RULE: a LOCAL proof row for this phone's own capture wins (live processing status);
 * otherwise the server-attributed peer slot from the captures poll renders as "Captured by X".
 *
 * Slots are PARALLEL: a slot chip's state derives ONLY from its own capture rows + the task
 * lifecycle lock, never from a sibling slot. Peer visibility and the submit lock arrive through
 * [PcCareRepository.pollTaskOnce], which writes THROUGH Room.
 */
@HiltViewModel
class PcCareTaskViewModel @Inject constructor(
    private val repository: PcCareRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val captureDrafts: CaptureDraftRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val reader: RfidReaderPort,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val taskId: String = savedStateHandle.get<String>(ARG_TASK_ID).orEmpty()
    private val categoryTitle: String = savedStateHandle.get<String>(ARG_TITLE).orEmpty()

    private val json = Json { ignoreUnknownKeys = true }

    private data class LocalBits(
        val scanInput: String = "",
        val scanNotice: String = "",
        val scanNoticeNonce: Int = 0,
        val message: String? = null,
        val showSubmitConfirmation: Boolean = false,
        val submitInFlight: Boolean = false,
        val submitQueued: Boolean = false,
        val isRefreshing: Boolean = false,
        /** The (tag:slot) key whose camera is open right now — [onRecordSlot]'s single-flight guard. */
        val capturingSlotKey: String? = null,
    )

    private val local = MutableStateFlow(LocalBits())

    // Latest durable snapshots for act-time reads (scan lock check, submit recompute). The SAME
    // Room flows the rendered state combines — never a stashed UI list.
    private var latestDetail: PcCareTaskDto? = null
    private var latestAnimals: List<PcCareAnimalRowEntity> = emptyList()
    private var latestProofs: List<ProofCaptureRow> = emptyList()

    val state: StateFlow<PcCareTaskUiState> = combine(
        repository.observeTaskDetail(taskId),
        repository.observeAnimals(taskId),
        proofCaptureRepository.observeProofs(taskId),
        local,
    ) { detail, animals, proofs, bits ->
        buildState(detail, animals, proofs, bits)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PcCareTaskUiState(title = categoryTitle))

    init {
        analytics.track(AnalyticsEvents.PC_CARE_TASK_OPENED, mapOf(AnalyticsEvents.Params.KIND to taskId))
        viewModelScope.launch { repository.refreshTaskDetail(taskId) }
        viewModelScope.launch { repository.pollTaskOnce(taskId) }
        // Track the durable snapshots the act paths read.
        viewModelScope.launch {
            repository.observeTaskDetail(taskId).collect { detail ->
                latestDetail = detail
            }
        }
        viewModelScope.launch {
            repository.observeTaskDetail(taskId)
                .map { it?.status.orEmpty() }
                .distinctUntilChanged()
                .collect { status ->
                    if (status == PC_CARE_STATUS_REWORK) {
                        analytics.track(AnalyticsEvents.PC_CARE_REWORK_VIEWED)
                        // A verifier sent the task back: earlier slots are being replaced under a
                        // bumped row version, so the queued-submit overlay must not keep the screen
                        // reading as sent.
                        local.update { it.copy(submitQueued = false) }
                    }
                }
        }
        viewModelScope.launch { repository.observeAnimals(taskId).collect { latestAnimals = it } }
        viewModelScope.launch { proofCaptureRepository.observeProofs(taskId).collect { latestProofs = it } }
        // Reader free-flow: every hardware read lands here while this screen's VM is alive. A scan
        // during an in-flight recording still records the ANIMAL — it must never retarget the
        // pending capture (that identity is fixed at Record-tap time, see [onRecordSlot]).
        viewModelScope.launch { reader.reads.collect { read -> handleScan(read.tag) } }
        // Peer-visibility poll, BOUNDED (a bare while(isActive) delay-loop hangs any
        // advanceUntilIdle in tests — see FeedDistributionCompleteViewModel's same-shaped loop).
        viewModelScope.launch {
            repeat(MAX_STATUS_POLLS) {
                delay(STATUS_POLL_INTERVAL_MS)
                repository.pollTaskOnce(taskId)
            }
        }
    }

    /** Route gating for the hardware reader — mirrored from the weighing capture screen. */
    fun setCaptureActive(active: Boolean) {
        reader.setCaptureEnabled(active)
        if (active) reader.refreshStatus()
    }

    fun onEvent(event: PcCareTaskEvent) {
        when (event) {
            is PcCareTaskEvent.ScanInputChanged -> local.update { it.copy(scanInput = event.value) }
            PcCareTaskEvent.SubmitTypedScan -> handleScan(local.value.scanInput, fromTypedEntry = true)
            is PcCareTaskEvent.RecordSlot -> onRecordSlot(event.tagKey, event.slotFieldKey)
            PcCareTaskEvent.Submit -> armSubmit()
            PcCareTaskEvent.ConfirmSubmit -> confirmSubmit()
            PcCareTaskEvent.DismissSubmitConfirmation ->
                local.update { it.copy(showSubmitConfirmation = false) }
            PcCareTaskEvent.Refresh -> refresh()
            PcCareTaskEvent.Back -> Unit
        }
    }

    // ---- Scanning ----------------------------------------------------------------------------

    private fun handleScan(raw: String, fromTypedEntry: Boolean = false) {
        val verbatim = raw.trim()
        if (verbatim.isEmpty()) return
        if (isLifecycleLocked(latestDetail)) {
            showScanNotice("This task is locked — no more scans.")
            return
        }
        viewModelScope.launch {
            when (val outcome = repository.recordScan(taskId, verbatim)) {
                is PcCareScanOutcome.Queued -> {
                    analytics.track(AnalyticsEvents.PC_CARE_SCAN_ACCEPTED)
                    if (fromTypedEntry) local.update { it.copy(scanInput = "") }
                }
                is PcCareScanOutcome.Duplicate -> {
                    analytics.track(AnalyticsEvents.PC_CARE_SCAN_DUPLICATE)
                    showScanNotice("Already scanned · $verbatim")
                    if (fromTypedEntry) local.update { it.copy(scanInput = "") }
                }
                is PcCareScanOutcome.Failed -> {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to outcome.reason.take(MAX_REASON_CHARS)),
                    )
                    local.update { it.copy(message = "Couldn't add this tag. Try again.") }
                }
            }
        }
    }

    private fun showScanNotice(text: String) {
        val nonce = local.value.scanNoticeNonce + 1
        local.update { it.copy(scanNotice = text, scanNoticeNonce = nonce) }
        viewModelScope.launch {
            delay(SCAN_NOTICE_DISMISS_MS)
            // Only the newest notice may clear itself — a fresh notice keeps its full display time.
            local.update { if (it.scanNoticeNonce == nonce) it.copy(scanNotice = "") else it }
        }
    }

    // ---- Slot capture ------------------------------------------------------------------------

    /**
     * Opens the camera for ONE slot of ONE scanned animal. The capture's target identity
     * (tag + slot) is fixed HERE, as immutable locals — a reader scan arriving while the video is
     * being recorded records that animal into the task but can NEVER retarget this capture (the
     * weighing mid-recording defect class).
     */
    private fun onRecordSlot(tagKey: String, slotFieldKey: String) {
        val bits = local.value
        if (bits.capturingSlotKey != null) {
            local.update { it.copy(message = "Finish the current video first.") }
            return
        }
        val detail = latestDetail
        if (detail == null || isLifecycleLocked(detail)) return
        val slotDto = detail.expectedSlots.firstOrNull { it.fieldKey == slotFieldKey } ?: return
        val animal = latestAnimals.firstOrNull { it.normalizedTag == tagKey } ?: return
        val slotKey = pcCareSlotProofFieldKey(tagKey, slotFieldKey)
        local.update { it.copy(capturingSlotKey = slotKey, message = null) }
        analytics.track(
            AnalyticsEvents.PC_CARE_SLOT_CAPTURE_STARTED,
            mapOf(AnalyticsEvents.Params.KIND to slotFieldKey),
        )
        viewModelScope.launch {
            try {
                val captured = try {
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            // Backend-owned slot label leads the recorder chrome; the duration
                            // hint is GUIDANCE only — never a client-enforced cap.
                            title = slotDto.label,
                            primaryTag = animal.tagVerbatim,
                            workLabel = pcCareSlotHintLabel(slotDto.minDurationHintSeconds),
                            headerTitle = categoryTitle.ifBlank { null },
                        ),
                    )
                } catch (error: Exception) {
                    crashReporter.recordException(error, "pc care slot video capture failed")
                    null
                }
                if (captured == null) return@launch
                val slot = EvidenceSlot(
                    identity = ProofIdentity(
                        flow = ProofFlow.PC_CARE,
                        taskId = taskId,
                        subjectKey = slotKey,
                    ),
                    fieldKey = slotKey,
                )
                when (
                    val result = proofCaptureRepository.captureReplacingLatest(
                        slot = slot,
                        subject = ProofSubject.OTHER,
                        subjectId = tagKey,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = "${slotDto.label} · ${animal.tagVerbatim}",
                        rfidTag = animal.tagVerbatim,
                        scopeType = "pc_care_task",
                        scopeId = taskId,
                        capturedStartMs = captured.startedAtMs,
                        capturedEndMs = captured.endedAtMs,
                        capturedByPrincipalId = null,
                        proofPolicy = pcCareProofPolicy(captured.captureSource),
                        awaitUploadEnqueue = true,
                        // One FIFO lane per task: the upload drains BEFORE the slot registration
                        // that resolves it and before the final submit (PcCarePayloads.kt).
                        uploadGroupKey = pcCareTaskGroupKey(taskId),
                    )
                ) {
                    is AppResult.Ok -> {
                        val proofOutboxId = result.value.outboxItemId
                        if (proofOutboxId.isNullOrBlank()) {
                            local.update { it.copy(message = "Video didn't save. Record again.") }
                            return@launch
                        }
                        // Durable draft: re-entering the screen after process death still knows
                        // which slot this queued clip belongs to.
                        captureDrafts.putProof(CaptureFlow.PC_CARE, taskId, slotKey, proofOutboxId)
                        when (
                            val registered = repository.registerSlotProof(
                                taskId = taskId,
                                normalizedTag = tagKey,
                                slotFieldKey = slotFieldKey,
                                proofOutboxItemId = proofOutboxId,
                            )
                        ) {
                            is AppResult.Ok -> {
                                analytics.track(
                                    AnalyticsEvents.PC_CARE_SLOT_CAPTURED,
                                    mapOf(AnalyticsEvents.Params.KIND to slotFieldKey),
                                )
                            }
                            is AppResult.Err -> {
                                registered.cause?.let { crashReporter.recordException(it, "pc care slot registration enqueue failed") }
                                analytics.track(
                                    AnalyticsEvents.PC_CARE_FAILURE,
                                    mapOf(AnalyticsEvents.Params.REASON to registered.message.take(MAX_REASON_CHARS)),
                                )
                                local.update { it.copy(message = "Video saved, but couldn't be attached. Tap refresh to retry.") }
                            }
                        }
                    }
                    is AppResult.Err -> {
                        result.cause?.let { crashReporter.recordException(it, "pc care slot capture enqueue failed") }
                        analytics.track(
                            AnalyticsEvents.PC_CARE_FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)),
                        )
                        local.update { it.copy(message = result.message) }
                    }
                }
            } finally {
                local.update { it.copy(capturingSlotKey = null) }
            }
        }
    }

    // ---- Submit ------------------------------------------------------------------------------

    private fun armSubmit() {
        val bits = local.value
        if (bits.submitInFlight || bits.submitQueued) return
        val detail = latestDetail ?: return
        if (isLifecycleLocked(detail)) return
        val evaluation = pcCareEvaluateSubmit(detail.expectedSlots, latestAnimals, latestProofs, json)
        if (!evaluation.ready) {
            analytics.track(
                AnalyticsEvents.PC_CARE_SUBMIT_BLOCKED,
                mapOf(AnalyticsEvents.Params.REASON to evaluation.blockedReason.take(MAX_REASON_CHARS)),
            )
            local.update { it.copy(message = evaluation.blockedReason) }
            return
        }
        local.update { it.copy(showSubmitConfirmation = true, message = null) }
    }

    private fun confirmSubmit() {
        val bits = local.value
        // Gate-first, then RECOMPUTE from durable observed state — never trust a stashed set
        // (WeighingViewModel.confirmSubmitIndividualScope's lesson).
        if (!bits.showSubmitConfirmation || bits.submitInFlight) return
        val detail = latestDetail
        if (detail == null || isLifecycleLocked(detail)) {
            local.update { it.copy(showSubmitConfirmation = false) }
            return
        }
        val evaluation = pcCareEvaluateSubmit(detail.expectedSlots, latestAnimals, latestProofs, json)
        if (!evaluation.ready) {
            // The task stopped being submittable between arm and confirm — close and say so,
            // rather than submitting stale work or silently doing nothing.
            local.update { it.copy(showSubmitConfirmation = false, message = evaluation.blockedReason) }
            return
        }
        local.update { it.copy(showSubmitConfirmation = false, submitInFlight = true) }
        viewModelScope.launch {
            when (val result = repository.submitTask(taskId, detail.rowVersion)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEvents.PC_CARE_SUBMIT_CONFIRMED)
                    local.update { it.copy(submitInFlight = false, submitQueued = true) }
                }
                is AppResult.Err -> {
                    // Single-flight latch RESETS on terminal failure, or one failed enqueue
                    // wedges the button forever (feed 2693a21f7 lesson).
                    result.cause?.let { crashReporter.recordException(it, "pc care submit enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.PC_CARE_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)),
                    )
                    local.update { it.copy(submitInFlight = false, message = "Couldn't send the task. Try again.") }
                }
            }
        }
    }

    private fun refresh() {
        if (local.value.isRefreshing) return
        local.update { it.copy(isRefreshing = true) }
        viewModelScope.launch {
            try {
                syncRepository.triggerDrain()
                repository.refreshTaskDetail(taskId)
                repository.pollTaskOnce(taskId)
            } finally {
                local.update { it.copy(isRefreshing = false) }
            }
        }
    }

    // ---- State assembly ----------------------------------------------------------------------

    private fun buildState(
        detail: PcCareTaskDto?,
        animals: List<PcCareAnimalRowEntity>,
        proofs: List<ProofCaptureRow>,
        bits: LocalBits,
    ): PcCareTaskUiState {
        val locked = isLifecycleLocked(detail) || bits.submitQueued
        val expectedSlots = detail?.expectedSlots.orEmpty()
        val evaluation = pcCareEvaluateSubmit(expectedSlots, animals, proofs, json)
        return PcCareTaskUiState(
            title = categoryTitle.ifBlank { detail?.category.orEmpty() },
            locationDisplay = detail?.let { it.operationalLocationDisplay.ifBlank { it.shedLabel } }.orEmpty(),
            parkLabel = detail?.parkLabel.orEmpty(),
            dateLabel = detail?.plannedBusinessDate.orEmpty(),
            assigneeLine = detail?.assigneeNames.orEmpty().joinToString(", "),
            isLocked = locked,
            lockNotice = when {
                detail?.status == PC_CARE_STATUS_COMPLETED -> "Checked and approved"
                detail?.status == PC_CARE_STATUS_PENDING_VERIFICATION || bits.submitQueued -> "Sent for checking"
                else -> ""
            },
            reworkReason = detail?.takeIf { it.status == PC_CARE_STATUS_REWORK }?.reworkReason.orEmpty(),
            scanInput = bits.scanInput,
            scanNotice = bits.scanNotice,
            animals = pcCareBuildAnimalUis(expectedSlots, animals, proofs, bits.capturingSlotKey, json),
            animalCountLabel = when {
                animals.isEmpty() -> ""
                animals.size == 1 -> "1 animal"
                else -> "${animals.size} animals"
            },
            submitEnabled = evaluation.ready && !locked,
            submitBlockedReason = if (!evaluation.ready && !locked) evaluation.blockedReason else "",
            showSubmitConfirmation = bits.showSubmitConfirmation,
            submitInFlight = bits.submitInFlight,
            submitQueued = bits.submitQueued,
            message = bits.message,
            isRefreshing = bits.isRefreshing,
        )
    }

    private fun isLifecycleLocked(detail: PcCareTaskDto?): Boolean {
        if (detail == null) return false
        if (detail.status == PC_CARE_STATUS_PENDING_VERIFICATION || detail.status == PC_CARE_STATUS_COMPLETED) return true
        return when (detail.workState) {
            "closed", "canceled" -> true
            else -> false
        }
    }

    companion object {
        const val ARG_TASK_ID = "task_id"
        const val ARG_CATEGORY = "category"
        const val ARG_TITLE = "title"

        private const val SCAN_NOTICE_DISMISS_MS = 4_000L
        private const val MAX_REASON_CHARS = 96

        /** 30 s cadence, bounded at ~24 h of screen-open time; a manual refresh re-reads anyway. */
        private const val STATUS_POLL_INTERVAL_MS = 30_000L
        private const val MAX_STATUS_POLLS = 2_880
    }
}

// ---------------------------------------------------------------------------
// Pure derivation helpers (unit-tested directly — no ViewModel needed).
// ---------------------------------------------------------------------------

/** The ONE builder of a slot's local proof field key: `<normalizedTag>:<slotFieldKey>`. */
internal fun pcCareSlotProofFieldKey(normalizedTag: String, slotFieldKey: String): String =
    "$normalizedTag:$slotFieldKey"

internal fun pcCareSlotHintLabel(minDurationHintSeconds: Int): String =
    if (minDurationHintSeconds > 0) "Record at least $minDurationHintSeconds seconds" else ""

internal fun pcCareProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "per_animal_slot_video",
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        // One active clip per (animal, slot); an animal's slots pool under the per-subject cap,
        // which comfortably covers the largest slot set (3) plus a transient replacement row.
        maximumCountPerField = 1,
        maximumCountPerSubject = 5,
    )

private fun decodeServerSlots(json: Json, serverSlotsJson: String): List<PcCareAnimalSlotDto> {
    if (serverSlotsJson.isBlank()) return emptyList()
    // A stale-shaped cached blob degrades to "no server slots yet" — the next poll repairs it.
    return runCatching { json.decodeFromString<List<PcCareAnimalSlotDto>>(serverSlotsJson) }
        .getOrDefault(emptyList())
}

/**
 * Derives ONE slot chip. Depends ONLY on this slot's own local proof rows and its own
 * server-attributed state — NEVER on a sibling slot (slots are parallel by contract).
 */
internal fun pcCareSlotChip(
    slot: PcCareSlotDto,
    normalizedTag: String,
    animalProofs: List<ProofCaptureRow>,
    serverSlots: List<PcCareAnimalSlotDto>,
    capturingSlotKey: String?,
): PcCareSlotChipUi {
    val slotKey = pcCareSlotProofFieldKey(normalizedTag, slot.fieldKey)
    val hint = pcCareSlotHintLabel(slot.minDurationHintSeconds)
    if (capturingSlotKey == slotKey) {
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            state = PcCareSlotState.WORKING,
            statusLabel = "Recording…",
            hintLabel = hint,
            canRecord = false,
        )
    }
    // MERGE RULE: this phone's own capture wins — its live processing status is what the person
    // holding the phone needs. Only when no local row exists does the peer attribution render.
    val localRow = animalProofs
        .filter { it.fieldKey == slotKey }
        .maxByOrNull { it.capturedAtMs }
    if (localRow != null) {
        return when (localRow.processingStatus) {
            ProofProcessingStatus.UPLOADED -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                state = PcCareSlotState.SYNCED,
                statusLabel = "Video sent",
                hintLabel = hint,
                canRecord = true,
            )
            ProofProcessingStatus.RECORD_AGAIN -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                state = PcCareSlotState.FAILED,
                statusLabel = "Record again",
                hintLabel = hint,
                canRecord = true,
            )
            else -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                state = if (localRow.syncStatus == CaptureSyncStatus.FAILED) PcCareSlotState.FAILED else PcCareSlotState.WORKING,
                statusLabel = localRow.processingStatus.operatorLabel,
                hintLabel = hint,
                canRecord = localRow.syncStatus == CaptureSyncStatus.FAILED,
            )
        }
    }
    val serverSlot = serverSlots.firstOrNull { it.fieldKey == slot.fieldKey && it.proofRef.isNotBlank() }
    if (serverSlot != null) {
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            state = PcCareSlotState.PEER,
            statusLabel = if (serverSlot.capturedByName.isNotBlank()) {
                "Captured by ${serverSlot.capturedByName}"
            } else {
                "Captured by a teammate"
            },
            hintLabel = hint,
            canRecord = false,
        )
    }
    return PcCareSlotChipUi(
        fieldKey = slot.fieldKey,
        label = slot.label,
        state = PcCareSlotState.EMPTY,
        statusLabel = "",
        hintLabel = hint,
        canRecord = true,
    )
}

internal fun pcCareBuildAnimalUis(
    expectedSlots: List<PcCareSlotDto>,
    animals: List<PcCareAnimalRowEntity>,
    proofs: List<ProofCaptureRow>,
    capturingSlotKey: String?,
    json: Json,
): List<PcCareAnimalUi> {
    // Parse/group once, never re-filter inside the per-slot loop for the WHOLE proof list.
    val proofsByAnimal = proofs.groupBy { it.fieldKey.substringBefore(':') }
    return animals.map { animal ->
        val serverSlots = decodeServerSlots(json, animal.serverSlotsJson)
        val animalProofs = proofsByAnimal[animal.normalizedTag].orEmpty()
        PcCareAnimalUi(
            key = animal.normalizedTag,
            tagLabel = animal.tagVerbatim,
            scannedByLine = when (animal.scanSyncStatus) {
                PcCareScanStatus.PENDING -> "Scan waiting for network"
                PcCareScanStatus.FAILED -> "Scan didn't go through — scan again"
                else -> animal.scannedByName.takeIf { it.isNotBlank() }?.let { "Scanned by $it" }.orEmpty()
            },
            slots = expectedSlots.map { slot ->
                pcCareSlotChip(slot, animal.normalizedTag, animalProofs, serverSlots, capturingSlotKey)
            },
        )
    }
}

internal data class PcCareSubmitEvaluation(
    val ready: Boolean,
    val blockedReason: String = "",
    val submittableTags: List<String> = emptyList(),
)

/**
 * The whole-task submit gate, recomputed FRESH from the durable Room-observed inputs on every
 * arm AND on every confirm: every animal's scan reached the server, and every expected slot on
 * every animal holds either this phone's SYNCED upload (serverProofId set) or a server-attributed
 * peer proof ref.
 */
internal fun pcCareEvaluateSubmit(
    expectedSlots: List<PcCareSlotDto>,
    animals: List<PcCareAnimalRowEntity>,
    proofs: List<ProofCaptureRow>,
    json: Json,
): PcCareSubmitEvaluation {
    if (animals.isEmpty()) {
        return PcCareSubmitEvaluation(ready = false, blockedReason = "Scan at least one animal first")
    }
    val pendingScans = animals.count { it.scanSyncStatus != PcCareScanStatus.SYNCED }
    val proofsByAnimal = proofs.groupBy { it.fieldKey.substringBefore(':') }
    var animalsMissingVideos = 0
    var uploadsInFlight = 0
    animals.forEach { animal ->
        val serverSlots = decodeServerSlots(json, animal.serverSlotsJson)
        val animalProofs = proofsByAnimal[animal.normalizedTag].orEmpty()
        var missing = false
        expectedSlots.forEach { slot ->
            val slotKey = pcCareSlotProofFieldKey(animal.normalizedTag, slot.fieldKey)
            val localRow = animalProofs
                .filter { it.fieldKey == slotKey && it.syncStatus != CaptureSyncStatus.FAILED }
                .maxByOrNull { it.capturedAtMs }
            val localSynced = localRow?.syncStatus == CaptureSyncStatus.SYNCED && !localRow.serverProofId.isNullOrBlank()
            val peerCaptured = serverSlots.any { it.fieldKey == slot.fieldKey && it.proofRef.isNotBlank() }
            when {
                localSynced || peerCaptured -> Unit
                localRow != null -> uploadsInFlight++
                else -> missing = true
            }
        }
        if (missing) animalsMissingVideos++
    }
    val reason = when {
        pendingScans == 1 -> "Waiting for network — 1 scan still sending"
        pendingScans > 1 -> "Waiting for network — $pendingScans scans still sending"
        animalsMissingVideos == 1 -> "1 animal still needs videos"
        animalsMissingVideos > 1 -> "$animalsMissingVideos animals still need videos"
        uploadsInFlight == 1 -> "1 video still uploading"
        uploadsInFlight > 1 -> "$uploadsInFlight videos still uploading"
        else -> ""
    }
    return if (reason.isEmpty()) {
        PcCareSubmitEvaluation(ready = true, submittableTags = animals.map { it.normalizedTag })
    } else {
        PcCareSubmitEvaluation(ready = false, blockedReason = reason)
    }
}
