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
import kotlinx.coroutines.flow.first
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
import sg.mesha.goatos.core.data.normalizePcCareTag
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
import sg.mesha.goatos.feature.pccare.PcCareRosterRowUi
import sg.mesha.goatos.feature.pccare.PcCareSlotChipUi
import sg.mesha.goatos.feature.pccare.PcCareSlotState
import sg.mesha.goatos.feature.pccare.PcCareTaskEvent
import sg.mesha.goatos.feature.pccare.PcCareTaskUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
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

    // Set only on the per-animal capture drill (roster mode): the animal this screen is focused
    // on. The FIRST entry records the tag into the task — the tap IS the free-flow scan.
    private val focusTagKey: String = savedStateHandle.get<String>(ARG_TAG_KEY).orEmpty()
    private val focusTagVerbatim: String = savedStateHandle.get<String>(ARG_TAG_VERBATIM).orEmpty()

    /** Oversight drill (planner/monitor list tap): the screen is read-only regardless of status. */
    private val monitorView: Boolean = savedStateHandle.get<String>(ARG_MONITOR) == "1"

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
        val readerName: String = "",
        val readerStatusLabel: String = "",
        val readerConnected: Boolean = false,
    )

    private val local = MutableStateFlow(LocalBits())

    // Latest durable snapshots for act-time reads (scan lock check, submit recompute). The SAME
    // Room flows the rendered state combines — never a stashed UI list.
    private var latestDetail: PcCareTaskDto? = null
    private var latestAnimals: List<PcCareAnimalRowEntity> = emptyList()
    private var latestProofs: List<ProofCaptureRow> = emptyList()
    private var latestRoster: List<String> = emptyList()
    private var rosterRefreshRequested = false

    val state: StateFlow<PcCareTaskUiState> = combine(
        repository.observeTaskDetail(taskId),
        repository.observeAnimals(taskId),
        proofCaptureRepository.observeProofs(taskId),
        repository.observeRoster(taskId),
        local,
    ) { detail, animals, proofs, roster, bits ->
        buildState(detail, animals, proofs, roster, bits)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PcCareTaskUiState(title = categoryTitle))

    init {
        analytics.track(AnalyticsEvents.PC_CARE_TASK_OPENED, mapOf(AnalyticsEvents.Params.KIND to taskId))
        viewModelScope.launch { repository.refreshTaskDetail(taskId) }
        viewModelScope.launch { repository.pollTaskOnce(taskId) }
        // Track the durable snapshots the act paths read.
        viewModelScope.launch {
            repository.observeTaskDetail(taskId).collect { detail ->
                latestDetail = detail
                // The roster tap list exists only for roster_pick work — fetch it once the mode is
                // known (the mode rides the task contract, so it may arrive after screen entry).
                if (detail?.captureMode == PC_CARE_CAPTURE_MODE_ROSTER && !rosterRefreshRequested) {
                    rosterRefreshRequested = true
                    viewModelScope.launch { repository.refreshRoster(taskId) }
                }
            }
        }
        viewModelScope.launch { repository.observeRoster(taskId).collect { latestRoster = it } }
        // Self-healing registration reconcile on every screen open (and again on every manual
        // refresh): a clip whose UPLOAD row is durable but whose slot registration enqueue was
        // lost is re-enqueued under the SAME stable idempotency key, so a duplicate is a no-op
        // and a lost one is repaired.
        viewModelScope.launch { reconcileSlotRegistrations() }
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
        // Reader banner: the same status + paired-device mapping the weighing capture screen shows.
        viewModelScope.launch {
            combine(reader.status, reader.devices) { readerStatus, devices ->
                Triple(
                    devices.firstOrNull()?.name ?: "RFID reader",
                    when (readerStatus) {
                        RfidReaderStatus.READY -> "Reader connected"
                        RfidReaderStatus.PAIRED_NOT_READY -> "Reader disconnected"
                        RfidReaderStatus.NOT_PAIRED -> "Reader not paired"
                        RfidReaderStatus.PERMISSION_NEEDED -> "Bluetooth permission needed"
                        RfidReaderStatus.BLUETOOTH_OFF -> "Bluetooth off"
                    },
                    readerStatus == RfidReaderStatus.READY,
                )
            }.collect { (name, label, connected) ->
                local.update { it.copy(readerName = name, readerStatusLabel = label, readerConnected = connected) }
            }
        }
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
            // Handled by the host: navigates to the per-animal capture drill (Feed Direction's
            // completion-screen shape — the three videos as clearly labeled options).
            is PcCareTaskEvent.RosterTapped -> Unit
            PcCareTaskEvent.Refresh -> refresh()
            // Handled by the host (navigates to the reader pairing screen).
            PcCareTaskEvent.ReconnectReader -> Unit
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
                    // Scan-and-record (deworming / ticks removal): the recorder opens the moment a
                    // NEW tag lands — the scan IS the start of that animal's video.
                    autoRecordAfterScan(verbatim)
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

    /**
     * Scan-and-record: opens the recorder for the freshly scanned animal's video. Skips silently
     * when a recording is already in flight (the scan itself is durable; the operator records
     * that animal from its row) and does nothing in roster mode, where the tap owns recording.
     */
    private fun autoRecordAfterScan(tagVerbatim: String) {
        val detail = latestDetail ?: return
        if (detail.captureMode == PC_CARE_CAPTURE_MODE_ROSTER) return
        if (local.value.capturingSlotKey != null) return
        val slotFieldKey = detail.expectedSlots.firstOrNull()?.fieldKey ?: return
        onRecordSlot(normalizePcCareTag(tagVerbatim), slotFieldKey, tagVerbatimFallback = tagVerbatim)
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
    private fun onRecordSlot(tagKey: String, slotFieldKey: String, tagVerbatimFallback: String? = null) {
        val bits = local.value
        if (bits.capturingSlotKey != null) {
            local.update { it.copy(message = "Finish the current video first.") }
            return
        }
        val detail = latestDetail
        if (detail == null || isLifecycleLocked(detail)) return
        val slotDto = detail.expectedSlots.firstOrNull { it.fieldKey == slotFieldKey } ?: return
        // A just-scanned animal's Room row may not have re-emitted yet — the caller supplies the
        // verbatim tag (or it is this drill's focus animal) so a record straight off the roster
        // tap never loses the race.
        val animalTagVerbatim = latestAnimals.firstOrNull { it.normalizedTag == tagKey }?.tagVerbatim
            ?: tagVerbatimFallback
            ?: focusTagVerbatim.takeIf { tagKey == focusTagKey && it.isNotBlank() }
            ?: return
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
                            primaryTag = animalTagVerbatim,
                            workLabel = pcCareSlotHintLabel(slotDto.minDurationHintSeconds),
                            headerTitle = categoryTitle.ifBlank { null },
                        ),
                    )
                } catch (error: Exception) {
                    crashReporter.recordException(error, "pc care slot video capture failed")
                    null
                }
                if (captured == null) return@launch
                // Everything after a REAL recording is durable bookkeeping — the animal's scan,
                // the clip's processing/upload enqueue, and the slot registration. It runs
                // NonCancellable so backing out of the screen (which cancels this ViewModel's
                // scope) can never orphan a clip the operator actually shot: that exact
                // cancellation left uploads with no slot registration on 2026-08-21.
                kotlinx.coroutines.withContext(kotlinx.coroutines.NonCancellable) {
                // The FIRST record for a roster animal records its scan (the tap IS the free-flow
                // scan). A tag already in the task is a Duplicate no-op.
                if (latestAnimals.none { it.normalizedTag == tagKey }) {
                    repository.recordScan(taskId, animalTagVerbatim)
                }
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
                        // The proof platform requires a UUID subject/scope (validateCreate); the
                        // animal's tag identity rides rfidTag + the slot field key instead.
                        subjectId = taskId,
                        localUri = captured.localUri,
                        mimeType = captured.mimeType,
                        caption = "${slotDto.label} · $animalTagVerbatim",
                        rfidTag = animalTagVerbatim,
                        scopeType = "task",
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
                        // The upload-row id can land in Room a beat AFTER the capture result is
                        // composed, so a blank id here is usually a read race, not a lost clip
                        // (2026-08-21: a fully uploaded clip reported "didn't save" and its slot
                        // registration was skipped). Re-read the durable row before concluding
                        // anything failed; only a row still without an upload id after the wait
                        // is a real capture failure.
                        var proofOutboxId = result.value.outboxItemId
                        var waited = 0L
                        while (proofOutboxId.isNullOrBlank() && waited < PROOF_ROW_SETTLE_MAX_MS) {
                            delay(PROOF_ROW_SETTLE_STEP_MS)
                            waited += PROOF_ROW_SETTLE_STEP_MS
                            proofOutboxId = proofCaptureRepository.observeProofs(taskId).first()
                                .firstOrNull { it.id == result.value.id }
                                ?.outboxItemId
                        }
                        if (proofOutboxId.isNullOrBlank()) {
                            crashReporter.recordException(
                                IllegalStateException("pc care clip ${result.value.id} has no upload row after ${waited}ms"),
                                "pc care slot capture never enqueued its upload",
                            )
                            local.update { it.copy(message = "Video didn't save. Record again.") }
                            return@withContext
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
                } // NonCancellable
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
                reconcileSlotRegistrations()
                syncRepository.triggerDrain()
                repository.refreshTaskDetail(taskId)
                repository.pollTaskOnce(taskId)
            } finally {
                local.update { it.copy(isRefreshing = false) }
            }
        }
    }

    /**
     * Re-enqueues the slot registration for every durable clip of this task. Registration rides
     * a STABLE idempotency key per (task, tag, slot, upload row), so a registration that already
     * exists is a no-op and one that was lost (process death, back-press before the fix, a read
     * race) is repaired.
     */
    private suspend fun reconcileSlotRegistrations() {
        val proofs = proofCaptureRepository.observeProofs(taskId).first()
        proofs.forEach { row ->
            val outboxId = row.outboxItemId
            if (outboxId.isNullOrBlank() || row.syncStatus == CaptureSyncStatus.FAILED) return@forEach
            val tag = row.fieldKey.substringBefore(':')
            val slot = row.fieldKey.substringAfter(':')
            if (tag.isNotBlank() && slot.isNotBlank() && slot != row.fieldKey) {
                repository.registerSlotProof(taskId, tag, slot, outboxId)
            }
        }
    }

    // ---- State assembly ----------------------------------------------------------------------

    private fun buildState(
        detail: PcCareTaskDto?,
        animals: List<PcCareAnimalRowEntity>,
        proofs: List<ProofCaptureRow>,
        roster: List<String>,
        bits: LocalBits,
    ): PcCareTaskUiState {
        val locked = isLifecycleLocked(detail) || bits.submitQueued || monitorView
        val expectedSlots = detail?.expectedSlots.orEmpty()
        val rosterMode = detail?.captureMode == PC_CARE_CAPTURE_MODE_ROSTER
        val rosterRows = if (rosterMode) {
            pcCareBuildRosterRows(expectedSlots, roster, animals, proofs, bits.capturingSlotKey, json)
        } else {
            emptyList()
        }
        val evaluation = pcCareEvaluateSubmit(expectedSlots, animals, proofs, json)
        // Per-animal drill focus: the scanned row's live chips, or a fresh all-empty card set
        // while the entry scan is still landing in Room.
        val focusAnimal = if (focusTagKey.isNotBlank()) {
            pcCareBuildAnimalUis(
                expectedSlots,
                animals.filter { it.normalizedTag == focusTagKey },
                proofs,
                bits.capturingSlotKey,
                json,
            ).firstOrNull() ?: PcCareAnimalUi(
                key = focusTagKey,
                tagLabel = focusTagVerbatim.ifBlank { focusTagKey },
                scannedByLine = "",
                slots = expectedSlots.map { slot ->
                    PcCareSlotChipUi(
                        fieldKey = slot.fieldKey,
                        label = slot.label,
                        description = slot.description,
                        state = PcCareSlotState.EMPTY,
                        statusLabel = "",
                        hintLabel = pcCareSlotHintLabel(slot.minDurationHintSeconds),
                        canRecord = true,
                    )
                },
            )
        } else {
            null
        }
        return PcCareTaskUiState(
            focusAnimal = focusAnimal,
            title = categoryTitle.ifBlank { detail?.category.orEmpty() },
            locationDisplay = detail?.let { it.operationalLocationDisplay.ifBlank { it.shedLabel } }.orEmpty(),
            parkLabel = detail?.parkLabel.orEmpty(),
            dateLabel = detail?.plannedBusinessDate.orEmpty(),
            assigneeLine = detail?.assigneeNames.orEmpty().joinToString(", "),
            isLocked = locked,
            lockNotice = when {
                detail?.status == PC_CARE_STATUS_COMPLETED -> "Checked and approved"
                detail?.status == PC_CARE_STATUS_PENDING_VERIFICATION || bits.submitQueued -> "Sent for checking"
                monitorView -> "Viewing only"
                else -> ""
            },
            reworkReason = detail?.takeIf { it.status == PC_CARE_STATUS_REWORK }?.reworkReason.orEmpty(),
            scanInput = bits.scanInput,
            scanNotice = bits.scanNotice,
            animals = pcCareBuildAnimalUis(expectedSlots, animals, proofs, bits.capturingSlotKey, json),
            animalCountLabel = when {
                rosterMode && rosterRows.isNotEmpty() ->
                    "${rosterRows.count { it.done }} of ${rosterRows.size} recorded"
                rosterMode -> ""
                animals.isEmpty() -> ""
                animals.size == 1 -> "1 animal"
                else -> "${animals.size} animals"
            },
            rosterMode = rosterMode,
            rosterRows = rosterRows,
            rosterEmptyNotice = if (rosterMode && rosterRows.isEmpty() && !bits.isRefreshing) {
                "No tagged animals listed for this pen yet — scan tags instead."
            } else {
                ""
            },
            submitEnabled = evaluation.ready && !locked,
            submitBlockedReason = when {
                evaluation.ready || locked -> ""
                // Roster mode has no scan verb — the same gate reads as a record ask.
                rosterMode && animals.isEmpty() -> "Record at least one animal first" // mobile-contract:ignore: device-local pre-sync gate copy
                else -> evaluation.blockedReason
            },
            showSubmitConfirmation = bits.showSubmitConfirmation,
            submitInFlight = bits.submitInFlight,
            submitQueued = bits.submitQueued,
            message = bits.message,
            isRefreshing = bits.isRefreshing,
            readerName = bits.readerName,
            readerStatusLabel = bits.readerStatusLabel,
            readerConnected = bits.readerConnected,
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
        /** Backend capture_mode token for the roster-tap flow (trimming work). */
        internal const val PC_CARE_CAPTURE_MODE_ROSTER = "roster_pick"

        const val ARG_TASK_ID = "task_id"
        const val ARG_CATEGORY = "category"
        const val ARG_TITLE = "title"
        const val ARG_TAG_KEY = "tag_key"
        const val ARG_TAG_VERBATIM = "tag_verbatim"
        const val ARG_MONITOR = "monitor"

        private const val SCAN_NOTICE_DISMISS_MS = 4_000L
        private const val MAX_REASON_CHARS = 96

        /** Bounded wait for a fresh clip's upload-row id to land in Room (read-race headroom). */
        private const val PROOF_ROW_SETTLE_STEP_MS = 250L
        private const val PROOF_ROW_SETTLE_MAX_MS = 5_000L

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
        // One active clip per (animal, slot) is the REAL bound (the field key carries the animal
        // tag). The per-subject budget pools the WHOLE task, because the proof platform requires
        // a UUID subject and that subject is the TASK — so it must cover a large pen's full
        // triple (hundreds of clips) plus transient replacement rows, never a per-animal 5.
        maximumCountPerField = 1,
        maximumCountPerSubject = 1200,
    )

private fun decodeServerSlots(json: Json, serverSlotsJson: String): List<PcCareAnimalSlotDto> {
    if (serverSlotsJson.isBlank()) return emptyList()
    // A stale-shaped cached blob degrades to "no server slots yet" — the next poll rewrites the
    // row from the live contract, so the failure is self-repairing and carries no signal a
    // report would add (the blob is written only by our own repository from server DTOs).
    // exception:exempt stale cached JSON degrades to empty and the next poll rewrites the row
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
            description = slot.description,
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
                description = slot.description,
                state = PcCareSlotState.SYNCED,
                statusLabel = "Video sent",
                hintLabel = hint,
                canRecord = true,
            )
            ProofProcessingStatus.RECORD_AGAIN -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
                state = PcCareSlotState.FAILED,
                statusLabel = "Record again",
                hintLabel = hint,
                canRecord = true,
            )
            else -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
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
            description = slot.description,
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
        description = slot.description,
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

/** True when this chip's clip is durably in (this phone synced, or a peer's attribution). */
private fun pcCareChipDone(chip: PcCareSlotChipUi): Boolean =
    chip.state == PcCareSlotState.SYNCED || chip.state == PcCareSlotState.PEER

/**
 * Derives the roster-tap rows: one per pen-roster RFID, in roster order, each summarizing the
 * animal's WHOLE slot set (one video for scan-record work; before/while/after for the trimming
 * categories). Animals scanned into the task but absent from the roster append after, so no
 * recorded work is ever hidden.
 */
internal fun pcCareBuildRosterRows(
    expectedSlots: List<PcCareSlotDto>,
    roster: List<String>,
    animals: List<PcCareAnimalRowEntity>,
    proofs: List<ProofCaptureRow>,
    capturingSlotKey: String?,
    json: Json,
): List<PcCareRosterRowUi> {
    if (expectedSlots.isEmpty()) return emptyList()
    val proofsByAnimal = proofs.groupBy { it.fieldKey.substringBefore(':') }
    val animalsByTag = animals.associateBy { it.normalizedTag }

    fun rowFor(tagKey: String, tagVerbatim: String): PcCareRosterRowUi {
        val animal = animalsByTag[tagKey]
            ?: return PcCareRosterRowUi(key = tagKey, tagLabel = tagVerbatim)
        val serverSlots = decodeServerSlots(json, animal.serverSlotsJson)
        val animalProofs = proofsByAnimal[tagKey].orEmpty()
        val chips = expectedSlots.map { slot ->
            pcCareSlotChip(slot, tagKey, animalProofs, serverSlots, capturingSlotKey)
        }
        val doneCount = chips.count(::pcCareChipDone)
        val done = doneCount == chips.size
        // The camera is open for THIS animal (never merely uploading — a background upload must
        // not block recording the animal's next clip).
        val recordingNow = capturingSlotKey != null && capturingSlotKey.startsWith("$tagKey:")
        val statusLabel = when {
            recordingNow -> "Recording…"
            done && chips.size == 1 -> chips.first().statusLabel
            done -> "All ${chips.size} videos in"
            chips.any { it.state == PcCareSlotState.FAILED } -> "Record again"
            doneCount > 0 || chips.any { it.state == PcCareSlotState.WORKING } ->
                "$doneCount of ${chips.size} videos"
            else -> ""
        }
        return PcCareRosterRowUi(
            key = tagKey,
            tagLabel = animal.tagVerbatim,
            statusLabel = statusLabel,
            done = done,
            working = recordingNow,
        )
    }

    val rosterKeys = LinkedHashMap<String, String>() // mobile-guard:ignore: function-local projection of one task roster, capped by PC Care repository page limits and discarded on return
    roster.forEach { id -> rosterKeys.putIfAbsent(normalizePcCareTag(id), id) }
    val rows = rosterKeys.map { (key, verbatim) -> rowFor(key, verbatim) }
    val extras = animals
        .filter { it.normalizedTag !in rosterKeys }
        .map { rowFor(it.normalizedTag, it.tagVerbatim) }
    return rows + extras
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
        // Device-local offline submit gate: this state exists before any server round-trip
        // (zero scans yet), so no backend contract can carry this copy; same class as the
        // sync-status lines below ("Waiting for network", "still uploading").
        return PcCareSubmitEvaluation(ready = false, blockedReason = "Scan at least one animal first") // mobile-contract:ignore: device-local pre-sync gate copy
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
