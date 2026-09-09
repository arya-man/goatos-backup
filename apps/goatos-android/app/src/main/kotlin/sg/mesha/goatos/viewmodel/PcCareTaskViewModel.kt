package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
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
import sg.mesha.goatos.BuildConfig
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
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.pcCareTaskGroupKey
import sg.mesha.goatos.core.network.dto.PcCareAnimalSlotDto
import sg.mesha.goatos.core.network.dto.PcCareRemovalPenDto
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.core.network.dto.PcCareTaskDto
import sg.mesha.goatos.core.network.dto.PcCareTaskProofDto
import sg.mesha.goatos.feature.pccare.PcCareAnimalUi
import sg.mesha.goatos.feature.pccare.PcCareInventoryRequirementUi
import sg.mesha.goatos.feature.pccare.PcCareProofPreviewKind
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
 * MERGE RULE: a LOCAL proof row for this phone's own capture wins while it is recording or
 * uploading. Final green is different: it only comes from the PC Care business row carrying the
 * proof ref, never from the blob upload alone.
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
    private val photoCaptureSource: PhotoCaptureSource,
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

    /**
     * PC Director's stock-approval drill (maintainer decision 2026-09-02): the screen stays
     * read-only like any monitor view, and ADDITIONALLY offers the approve/send-back verdict
     * bar once the task is in review. Set from the backend's pc_care_stock_approve capability
     * flag by the host — the server independently gates the verdict route.
     */
    private val approveView: Boolean = savedStateHandle.get<String>(ARG_APPROVE) == "1"

    /**
     * The category the launching tab carried on the route (e.g. inventory_vaccine). Known
     * BEFORE the task detail loads, so the screen can pick the task-proof (fridge stock) face
     * immediately instead of flashing the scan-and-record row while the detail is in flight.
     */
    private val routeCategory: String = savedStateHandle.get<String>(ARG_CATEGORY).orEmpty()

    private val json = Json { ignoreUnknownKeys = true }

    private data class LocalBits(
        val scanInput: String = "",
        val scanNotice: String = "",
        val scanNoticeNonce: Int = 0,
        val message: String? = null,
        val showSubmitConfirmation: Boolean = false,
        val submitInFlight: Boolean = false,
        val submitQueued: Boolean = false,
        val submitOutboxItemId: String? = null,
        val isRefreshing: Boolean = false,
        /** The (tag:slot) key whose camera is open right now — [onRecordSlot]'s single-flight guard. */
        val capturingSlotKey: String? = null,
        val readerName: String = "",
        val readerStatusLabel: String = "",
        val readerConnected: Boolean = false,
        val taskProofPreviewUrls: Map<String, TaskProofPreviewUrl> = emptyMap(),
        val animalProofPreviewUrls: Map<String, TaskProofPreviewUrl> = emptyMap(),
        // PC Director's stock verdict (approve view only).
        val verdictInFlight: Boolean = false,
        val showRejectDialog: Boolean = false,
        val rejectReasonInput: String = "",
        /**
         * A ROUND-grain feed & water removal's pens (maintainer decision 2026-09-05). The card
         * is one evening's job; the evidence is one feed video and one water video PER PEN,
         * because a single clip stretched over four pens proves nothing and the verifier cannot
         * tell which pen was actually emptied. Empty on every other card.
         */
        val removalPens: List<PcCareRemovalPenDto> = emptyList(),
    )

    private val local = MutableStateFlow(LocalBits())

    // Latest durable snapshots for act-time reads (scan lock check, submit recompute). The SAME
    // Room flows the rendered state combines — never a stashed UI list.
    private var latestDetail: PcCareTaskDto? = null
    private var latestAnimals: List<PcCareAnimalRowEntity> = emptyList()
    private var latestProofs: List<ProofCaptureRow> = emptyList()
    private var latestRoster: List<String> = emptyList()
    private var rosterRefreshRequested = false
    private var removalPensRefreshRequested = false
    private var submitObserveJob: Job? = null
    private val proofUploadObserveJobs = mutableMapOf<String, Job>() // mobile-guard:ignore ViewModel-lifetime jobs bounded by proof rows on one PC Care task screen
    private val animalSlotProofTerminalEventsTracked = mutableSetOf<String>() // mobile-guard:ignore ViewModel-lifetime set bounded by proof rows on one PC Care task screen
    private val taskProofTerminalEventsTracked = mutableSetOf<String>() // mobile-guard:ignore ViewModel-lifetime set bounded by proof rows on one PC Care task screen
    private val animalSlotBusinessAckEventsTracked = mutableSetOf<String>() // mobile-guard:ignore ViewModel-lifetime set bounded by backend-visible slot refs on one PC Care task screen
    private val taskProofBusinessAckEventsTracked = mutableSetOf<String>() // mobile-guard:ignore ViewModel-lifetime set bounded by backend-visible task proof refs on one PC Care task screen
    private val pcCareTaskProofSlotKeys = setOf(
        PC_CARE_SLOT_STOCK_FRIDGE_PHOTO,
        PC_CARE_SLOT_STOCK_FRIDGE_VIDEO,
        PC_CARE_SLOT_FEED_VIDEO,
        PC_CARE_SLOT_WATER_VIDEO,
    )

    val state: StateFlow<PcCareTaskUiState> = combine(
        repository.observeTaskDetail(taskId),
        repository.observeAnimals(taskId),
        proofCaptureRepository.observeProofs(taskId),
        repository.observeRoster(taskId),
        local,
    ) { detail, animals, proofs, roster, bits ->
        buildState(detail, animals, proofs, roster, bits)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PcCareTaskUiState(title = pcCareTaskTitle(null)))

    init {
        analytics.track(AnalyticsEvents.PC_CARE_TASK_OPENED, mapOf(AnalyticsEvents.Params.KIND to taskId))
        viewModelScope.launch { repository.refreshTaskDetail(taskId) }
        viewModelScope.launch { repository.pollTaskOnce(taskId) }
        // Room is the source of truth for the removal card's pens: the screen renders what is
        // cached and the refresh above fills it in behind.
        viewModelScope.launch {
            repository.observeRemovalPens(taskId).collect { pens ->
                local.update { it.copy(removalPens = pens) }
                if (pcCareIsTaskProofMode(latestDetail)) {
                    hydrateTaskProofPreviews(latestDetail)
                    observeTaskProofUploads(latestProofs)
                }
            }
        }
        // Track the durable snapshots the act paths read.
        viewModelScope.launch {
            repository.observeTaskDetail(taskId).collect { detail ->
                latestDetail = detail
                if (pcCareIsTaskProofMode(detail)) {
                    hydrateTaskProofPreviews(detail)
                    observeTaskProofUploads(latestProofs)
                }
                hydrateAnimalProofPreviews(detail, latestAnimals)
                // The pen list rides the card's own contract, so it may arrive after screen
                // entry; refresh it once the category is known. Do this even when Room already
                // has a partial cached list: one completed pen must not hide the still-missing
                // pens on reopen.
                if (pcCareIsFeedWaterRemoval(detail) && !removalPensRefreshRequested) {
                    removalPensRefreshRequested = true
                    refreshRemovalPens()
                }
                // The roster tap list exists only for roster_pick work — fetch it once the mode is
                // known (the mode rides the task contract, so it may arrive after screen entry).
                if (detail?.captureMode == PC_CARE_CAPTURE_MODE_ROSTER && !rosterRefreshRequested) {
                    rosterRefreshRequested = true
                    viewModelScope.launch { repository.refreshRoster(taskId) }
                }
            }
        }
        viewModelScope.launch {
            repository.observeTaskDetail(taskId)
                .map { detail ->
                    if (pcCareIsTaskProofMode(detail)) {
                        detail?.status?.ifBlank { detail.workState }
                    } else {
                        null
                    }
                }
                .distinctUntilChanged()
                .collect { status ->
                    if (status != null) {
                        analytics.track(
                            AnalyticsEvents.PC_CARE_STOCK_PROOF_SCREEN_VISIBLE,
                            pcCareStockProofAnalyticsProps(status = status),
                        )
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
                        local.update { it.copy(submitQueued = false, submitOutboxItemId = null) }
                    }
                }
        }
        viewModelScope.launch {
            repository.observeAnimals(taskId).collect { animals ->
                latestAnimals = animals
                hydrateAnimalProofPreviews(latestDetail, animals)
            }
        }
        viewModelScope.launch {
            proofCaptureRepository.observeProofs(taskId).collect { proofs ->
                latestProofs = proofs
                observeAnimalSlotProofUploads(proofs)
                observeTaskProofUploads(proofs)
            }
        }
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
            is PcCareTaskEvent.RecordTaskProof -> onRecordTaskProof(event.slotFieldKey, event.mediaKind)
            is PcCareTaskEvent.ProofPreviewAction -> trackProofPreviewAction(
                slotFieldKey = event.slotFieldKey,
                mediaKind = event.mediaKind,
                action = event.action,
                tagKey = event.tagKey,
            ).also {
                if (event.action.startsWith("playback_failed")) {
                    recoverProofPreviewUrl(event.slotFieldKey, event.tagKey)
                }
            }
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
            // PC Director's stock verdict (maintainer decision 2026-09-02).
            PcCareTaskEvent.ApproveStock -> sendStockVerdict(STOCK_VERDICT_APPROVE, "")
            PcCareTaskEvent.OpenRejectStock ->
                local.update { it.copy(showRejectDialog = true) }
            PcCareTaskEvent.DismissRejectStock ->
                local.update { it.copy(showRejectDialog = false) }
            is PcCareTaskEvent.RejectStockReasonChanged ->
                local.update { it.copy(rejectReasonInput = event.value) }
            PcCareTaskEvent.ConfirmRejectStock -> {
                val reason = local.value.rejectReasonInput.trim()
                if (reason.isNotEmpty()) sendStockVerdict(STOCK_VERDICT_REJECT, reason)
            }
        }
    }

    /**
     * Sends the director's approve/send-back on the submitted fridge proof. A live online call
     * (the director is looking at the videos); success re-reads the task so the new status
     * renders immediately, failure keeps the screen with backend-worded copy.
     */
    private fun sendStockVerdict(verdict: String, reason: String) {
        if (local.value.verdictInFlight) return
        if (!approveView) return
        local.update { it.copy(verdictInFlight = true, message = null) }
        analytics.track(
            AnalyticsEvents.PC_CARE_STOCK_VERDICT,
            mapOf(AnalyticsEvents.Params.KIND to verdict, "task_id" to taskId),
        )
        viewModelScope.launch {
            when (val result = repository.recordStockVerdict(taskId, verdict, reason)) {
                is AppResult.Ok -> {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_VERDICT,
                        mapOf(
                            AnalyticsEvents.Params.KIND to verdict,
                            AnalyticsEvents.Params.OUTCOME to "success",
                            AnalyticsEvents.Params.STATUS to result.value.status,
                        ),
                    )
                    local.update {
                        it.copy(
                            verdictInFlight = false,
                            showRejectDialog = false,
                            rejectReasonInput = "",
                            message = if (verdict == STOCK_VERDICT_APPROVE) {
                                "Approved — stock check complete"
                            } else {
                                "Sent back for another recording"
                            },
                        )
                    }
                    refresh()
                }
                is AppResult.Err -> {
                    crashReporter.recordException(
                        result.cause ?: IllegalStateException(result.message),
                        "pc care stock verdict failed",
                    )
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_VERDICT,
                        mapOf(
                            AnalyticsEvents.Params.KIND to verdict,
                            AnalyticsEvents.Params.OUTCOME to "failure",
                            AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS),
                        ),
                    )
                    local.update { it.copy(verdictInFlight = false, message = result.message) }
                }
            }
        }
    }

    // ---- Scanning ----------------------------------------------------------------------------

    private fun handleScan(raw: String, fromTypedEntry: Boolean = false) {
        val verbatim = raw.trim()
        if (verbatim.isEmpty()) return
        if (isSuspiciousShortNumericRfid(verbatim)) {
            analytics.track(
                AnalyticsEvents.PC_CARE_FAILURE,
                mapOf(AnalyticsEvents.Params.REASON to "short_numeric_rfid"),
            )
            local.update { it.copy(message = "RFID was incomplete. Scan the full tag again.") }
            if (fromTypedEntry) local.update { it.copy(scanInput = "") }
            return
        }
        if (isLifecycleLocked(latestDetail)) {
            showScanNotice("This task is locked — no more scans.")
            return
        }
        viewModelScope.launch {
            when (val outcome = repository.recordScan(taskId, verbatim)) {
                is PcCareScanOutcome.Queued -> {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_SCAN_ACCEPTED,
                        pcCareScanAnalyticsProps(
                            tagVerbatim = verbatim,
                            outcome = "queued",
                            source = if (fromTypedEntry) "typed_entry" else "rfid_reader",
                            outboxItemId = outcome.outboxItemId,
                            groupKey = outcome.groupKey,
                            idempotencyKey = outcome.idempotencyKey,
                        ),
                    )
                    if (fromTypedEntry) local.update { it.copy(scanInput = "") }
                    // Scan-and-record (deworming / ticks removal): the recorder opens the moment a
                    // NEW tag lands — the scan IS the start of that animal's video.
                    autoRecordAfterScan(verbatim)
                }
                is PcCareScanOutcome.Duplicate -> {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_SCAN_DUPLICATE,
                        pcCareScanAnalyticsProps(
                            tagVerbatim = verbatim,
                            outcome = "duplicate",
                            source = if (fromTypedEntry) "typed_entry" else "rfid_reader",
                        ),
                    )
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
        if (pcCareIsTaskProofMode(detail)) return
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
        val failedRow = latestProofs
            .filter { it.fieldKey == slotKey }
            .maxByOrNull { it.capturedAtMs }
            ?.takeIf { it.processingStatus == ProofProcessingStatus.RECORD_AGAIN || it.syncStatus == CaptureSyncStatus.FAILED }
        if (failedRow != null) {
            retryPcCareSlotUpload(
                row = failedRow,
                tagKey = tagKey,
                tagVerbatim = animalTagVerbatim,
                slotFieldKey = slotFieldKey,
                slotKey = slotKey,
            )
            return
        }
        local.update { it.copy(capturingSlotKey = slotKey, message = null) }
        analytics.track(
            AnalyticsEvents.PC_CARE_SLOT_CAPTURE_STARTED,
            pcCareAnimalSlotAnalyticsProps(
                tagKey = tagKey,
                tagVerbatim = animalTagVerbatim,
                slotFieldKey = slotFieldKey,
                slotKey = slotKey,
                outcome = "started",
                legacyKind = slotFieldKey,
            ),
        )
        viewModelScope.launch {
            try {
                val captured =
                    proofCaptureSource.captureVideo(
                        ProofCaptureContext(
                            // Backend-owned slot label leads the recorder chrome; the duration
                            // hint is GUIDANCE only — never a client-enforced cap.
                            title = slotDto.label,
                            primaryTag = animalTagVerbatim,
                            workLabel = pcCareSlotHintLabel(slotDto.minDurationHintSeconds),
                            headerTitle = pcCareTaskTitle(detail).ifBlank { null },
                        ),
                    )
                if (captured == null) {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_SLOT_CAPTURE_RESULT,
                        pcCareAnimalSlotAnalyticsProps(
                            tagKey = tagKey,
                            tagVerbatim = animalTagVerbatim,
                            slotFieldKey = slotFieldKey,
                            slotKey = slotKey,
                            outcome = "cancelled",
                        ),
                    )
                    return@launch
                }
                analytics.track(
                    AnalyticsEvents.PC_CARE_SLOT_CAPTURE_RESULT,
                    pcCareAnimalSlotAnalyticsProps(
                        tagKey = tagKey,
                        tagVerbatim = animalTagVerbatim,
                        slotFieldKey = slotFieldKey,
                        slotKey = slotKey,
                        outcome = "success",
                        mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                    ),
                )
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
                            proofPolicy = pcCareProofPolicy(captured.captureSource, detail.category),
                            awaitUploadEnqueue = true,
                            // One FIFO lane per task: the upload drains BEFORE the slot registration
                            // that resolves it and before the final submit (PcCarePayloads.kt).
                            uploadGroupKey = pcCareTaskGroupKey(taskId),
                        )
                    ) {
                        is AppResult.Ok -> {
                            analytics.track(
                                AnalyticsEvents.PC_CARE_SLOT_ROOM_WRITTEN,
                                pcCareAnimalSlotAnalyticsProps(
                                    tagKey = tagKey,
                                    tagVerbatim = animalTagVerbatim,
                                    slotFieldKey = slotFieldKey,
                                    slotKey = slotKey,
                                    outcome = "success",
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                    proofRowId = result.value.id,
                                ),
                            )
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
                                analytics.track(
                                    AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED,
                                    pcCareAnimalSlotAnalyticsProps(
                                        tagKey = tagKey,
                                        tagVerbatim = animalTagVerbatim,
                                        slotFieldKey = slotFieldKey,
                                        slotKey = slotKey,
                                        outcome = "failure",
                                        reason = "missing_outbox_item_after_capture",
                                        mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                        proofRowId = result.value.id,
                                    ),
                                )
                                local.update { it.copy(message = "Video didn't queue. Retry upload.") }
                                return@withContext
                            }
                            analytics.track(
                                AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED,
                                pcCareAnimalSlotAnalyticsProps(
                                    tagKey = tagKey,
                                    tagVerbatim = animalTagVerbatim,
                                    slotFieldKey = slotFieldKey,
                                    slotKey = slotKey,
                                    outcome = "success",
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                    proofRowId = result.value.id,
                                    proofOutboxItemId = proofOutboxId,
                                ),
                            )
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
                                        AnalyticsEvents.PC_CARE_SLOT_REGISTRATION,
                                        pcCareAnimalSlotAnalyticsProps(
                                            tagKey = tagKey,
                                            tagVerbatim = animalTagVerbatim,
                                            slotFieldKey = slotFieldKey,
                                            slotKey = slotKey,
                                            outcome = "enqueued",
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                            outboxItemId = registered.value,
                                        ),
                                    )
                                    analytics.track(
                                        AnalyticsEvents.PC_CARE_SLOT_CAPTURED,
                                        pcCareAnimalSlotAnalyticsProps(
                                            tagKey = tagKey,
                                            tagVerbatim = animalTagVerbatim,
                                            slotFieldKey = slotFieldKey,
                                            slotKey = slotKey,
                                            outcome = "success",
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            legacyKind = slotFieldKey,
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                            outboxItemId = registered.value,
                                        ),
                                    )
                                }
                                is AppResult.Err -> {
                                    registered.cause?.let { crashReporter.recordException(it, "pc care slot registration enqueue failed") }
                                    analytics.track(
                                        AnalyticsEvents.PC_CARE_SLOT_REGISTRATION,
                                        pcCareAnimalSlotAnalyticsProps(
                                            tagKey = tagKey,
                                            tagVerbatim = animalTagVerbatim,
                                            slotFieldKey = slotFieldKey,
                                            slotKey = slotKey,
                                            outcome = "failure",
                                            reason = registered.message.take(MAX_REASON_CHARS),
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                        ),
                                    )
                                    analytics.track(
                                        AnalyticsEvents.PC_CARE_FAILURE,
                                        pcCareAnimalSlotAnalyticsProps(
                                            tagKey = tagKey,
                                            tagVerbatim = animalTagVerbatim,
                                            slotFieldKey = slotFieldKey,
                                            slotKey = slotKey,
                                            outcome = "failure",
                                            reason = registered.message.take(MAX_REASON_CHARS),
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                        ),
                                    )
                                    local.update { it.copy(message = "Video saved, but couldn't be attached. Tap refresh to retry.") }
                                }
                            }
                        }
                        is AppResult.Err -> {
                            result.cause?.let { crashReporter.recordException(it, "pc care slot capture enqueue failed") }
                            analytics.track(
                                AnalyticsEvents.PC_CARE_SLOT_ROOM_WRITTEN,
                                pcCareAnimalSlotAnalyticsProps(
                                    tagKey = tagKey,
                                    tagVerbatim = animalTagVerbatim,
                                    slotFieldKey = slotFieldKey,
                                    slotKey = slotKey,
                                    outcome = "failure",
                                    reason = result.message.take(MAX_REASON_CHARS),
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                ),
                            )
                            analytics.track(
                                AnalyticsEvents.PC_CARE_FAILURE,
                                pcCareAnimalSlotAnalyticsProps(
                                    tagKey = tagKey,
                                    tagVerbatim = animalTagVerbatim,
                                    slotFieldKey = slotFieldKey,
                                    slotKey = slotKey,
                                    outcome = "failure",
                                    reason = result.message.take(MAX_REASON_CHARS),
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                ),
                            )
                            local.update { it.copy(message = result.message) }
                        }
                    }
                } // NonCancellable
            } catch (error: kotlinx.coroutines.CancellationException) {
                throw error
            } catch (error: Exception) {
                crashReporter.recordException(error, "pc care slot video capture failed")
                analytics.track(
                    AnalyticsEvents.PC_CARE_SLOT_CAPTURE_RESULT,
                    pcCareAnimalSlotAnalyticsProps(
                        tagKey = tagKey,
                        tagVerbatim = animalTagVerbatim,
                        slotFieldKey = slotFieldKey,
                        slotKey = slotKey,
                        outcome = "failure",
                        reason = error.javaClass.simpleName.take(MAX_REASON_CHARS),
                    ),
                )
            } finally {
                local.update { it.copy(capturingSlotKey = null) }
            }
        }
    }

    private fun retryPcCareSlotUpload(
        row: ProofCaptureRow,
        tagKey: String,
        tagVerbatim: String,
        slotFieldKey: String,
        slotKey: String,
    ) {
        analytics.track(
            AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED,
            pcCareAnimalSlotAnalyticsProps(
                tagKey = tagKey,
                tagVerbatim = tagVerbatim,
                slotFieldKey = slotFieldKey,
                slotKey = slotKey,
                outcome = "retry_requested",
                mediaKind = pcCareMediaKindFromMime(row.mimeType),
                proofRowId = row.id,
                proofOutboxItemId = row.outboxItemId,
            ),
        )
        local.update { it.copy(message = "Retrying saved video upload…") }
        viewModelScope.launch {
            when (val retry = proofCaptureRepository.retryUpload(taskId, row.id)) {
                is AppResult.Ok -> {
                    reconcileSlotRegistrations()
                    analytics.track(
                        AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED,
                        pcCareAnimalSlotAnalyticsProps(
                            tagKey = tagKey,
                            tagVerbatim = tagVerbatim,
                            slotFieldKey = slotFieldKey,
                            slotKey = slotKey,
                            outcome = "retry_enqueued",
                            mediaKind = pcCareMediaKindFromMime(row.mimeType),
                            proofRowId = row.id,
                            proofOutboxItemId = row.outboxItemId,
                        ),
                    )
                    local.update { it.copy(message = "Saved video upload retry queued.") }
                }
                is AppResult.Err -> {
                    retry.cause?.let { crashReporter.recordException(it, "pc care slot upload retry failed") }
                    analytics.track(
                        AnalyticsEvents.PC_CARE_SLOT_UPLOAD_ENQUEUED,
                        pcCareAnimalSlotAnalyticsProps(
                            tagKey = tagKey,
                            tagVerbatim = tagVerbatim,
                            slotFieldKey = slotFieldKey,
                            slotKey = slotKey,
                            outcome = "retry_failed",
                            reason = retry.message.take(MAX_REASON_CHARS),
                            mediaKind = pcCareMediaKindFromMime(row.mimeType),
                            proofRowId = row.id,
                            proofOutboxItemId = row.outboxItemId,
                        ),
                    )
                    local.update { it.copy(message = retry.message) }
                }
            }
        }
    }

    // ---- Submit ------------------------------------------------------------------------------

    private fun onRecordTaskProof(slotFieldKey: String, mediaKind: String) {
        val bits = local.value
        if (bits.capturingSlotKey != null) {
            local.update { it.copy(message = "Finish the current proof first.") }
            return
        }
        val detail = latestDetail
        if (detail == null || !pcCareIsTaskProofMode(detail) || isLifecycleLocked(detail)) return
        val expectedSlots = pcCareTaskProofExpectedSlots(detail)
        val slotDto = expectedSlots.firstOrNull { it.fieldKey == slotFieldKey } ?: return
        val failedRow = latestProofs
            .filter { it.fieldKey == slotFieldKey }
            .maxByOrNull { it.capturedAtMs }
            ?.takeIf { it.processingStatus == ProofProcessingStatus.RECORD_AGAIN || it.syncStatus == CaptureSyncStatus.FAILED }
        analytics.track(
            AnalyticsEvents.PC_CARE_STOCK_PROOF_ACTION_TAPPED,
            pcCareStockProofAnalyticsProps(fieldKey = slotFieldKey, mediaKind = mediaKind, status = detail.status, source = "proof_row"),
        )
        if (failedRow != null) {
            retryPcCareTaskProofUpload(
                detail = detail,
                slotFieldKey = slotFieldKey,
                mediaKind = mediaKind,
                row = failedRow,
            )
            return
        }
        local.update { it.copy(capturingSlotKey = slotFieldKey, message = null) }
        viewModelScope.launch {
            try {
                val captured = try {
                    if (mediaKind == "photo") {
                        photoCaptureSource.capturePhoto(
                            PhotoCaptureContext(
                                title = slotDto.label,
                                instruction = slotDto.description.ifBlank { slotDto.label },
                                prompt = if (pcCareIsFeedWaterRemoval(detail)) null else ProofCapturePrompt.INVENTORY_VACCINE_STOCK,
                            ),
                        )?.let {
                            PcCareCapturedTaskProof(
                                localUri = it.localUri,
                                mimeType = it.mimeType,
                                startedAtMs = it.capturedAtMs,
                                endedAtMs = it.capturedAtMs,
                                captureSource = it.captureSource,
                            )
                        }
                    } else {
                        proofCaptureSource.captureVideo(
                            ProofCaptureContext(
                                title = slotDto.label,
                                primaryTag = detail.taskLabel.ifBlank { detail.operationalLocationDisplay.ifBlank { detail.shedLabel } },
                                workLabel = slotDto.description.ifBlank { slotDto.label },
                                prompt = if (pcCareIsFeedWaterRemoval(detail)) null else ProofCapturePrompt.INVENTORY_VACCINE_STOCK,
                                headerTitle = pcCareTaskTitle(detail).ifBlank { null },
                            ),
                        )?.let {
                            PcCareCapturedTaskProof(
                                localUri = it.localUri,
                                mimeType = it.mimeType,
                                startedAtMs = it.startedAtMs,
                                endedAtMs = it.endedAtMs,
                                captureSource = it.captureSource,
                            )
                        }
                    }
                } catch (error: Exception) {
                    crashReporter.recordException(error, "pc care task proof capture failed")
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_CAPTURE_RESULT,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = slotFieldKey,
                            mediaKind = mediaKind,
                            status = detail.status,
                            outcome = "failure",
                            reason = error.javaClass.simpleName.take(MAX_REASON_CHARS),
                        ),
                    )
                    null
                }
                if (captured == null) {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_CAPTURE_RESULT,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = slotFieldKey,
                            mediaKind = mediaKind,
                            status = detail.status,
                            outcome = "cancelled",
                        ),
                    )
                    return@launch
                }
                analytics.track(
                    AnalyticsEvents.PC_CARE_STOCK_PROOF_CAPTURE_RESULT,
                    pcCareStockProofAnalyticsProps(
                        fieldKey = slotFieldKey,
                        mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                        status = detail.status,
                        outcome = "success",
                    ),
                )
                val (_, mimeSlotKey) = pcCareSplitRemovalSlotKey(slotFieldKey)
                if (!pcCareStockSlotMatchesMime(mimeSlotKey, captured.mimeType)) {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_ROOM_WRITTEN,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = slotFieldKey,
                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                            status = detail.status,
                            outcome = "failure",
                            reason = "slot_media_mismatch",
                        ),
                    )
                    local.update { it.copy(message = "Wrong proof type returned. Record the ${slotDto.label.lowercase()} again.") }
                    return@launch
                }
                kotlinx.coroutines.withContext(kotlinx.coroutines.NonCancellable) {
                    val slot = EvidenceSlot(
                        identity = ProofIdentity(
                            flow = ProofFlow.PC_CARE,
                            taskId = taskId,
                            subjectKey = slotFieldKey,
                        ),
                        fieldKey = slotFieldKey,
                    )
                    val proofNoun = if (captured.mimeType.startsWith("image/", ignoreCase = true)) "Photo" else "Video"
                    when (
                        val result = proofCaptureRepository.captureReplacingLatest(
                            slot = slot,
                            subject = ProofSubject.OTHER,
                            subjectId = taskId,
                            localUri = captured.localUri,
                            mimeType = captured.mimeType,
                            caption = "${slotDto.label} · ${detail.taskLabel.ifBlank { detail.operationalLocationDisplay.ifBlank { detail.shedLabel } }}",
                            scopeType = "task",
                            scopeId = taskId,
                            capturedStartMs = captured.startedAtMs,
                            capturedEndMs = captured.endedAtMs,
                            capturedByPrincipalId = null,
                            proofPolicy = pcCareProofPolicy(captured.captureSource, detail.category),
                            awaitUploadEnqueue = true,
                            uploadGroupKey = pcCareTaskGroupKey(taskId),
                        )
                    ) {
                        is AppResult.Ok -> {
                            analytics.track(
                                AnalyticsEvents.PC_CARE_STOCK_PROOF_ROOM_WRITTEN,
                                pcCareStockProofAnalyticsProps(
                                    fieldKey = slotFieldKey,
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                    status = detail.status,
                                    outcome = "success",
                                    source = "room",
                                    proofRowId = result.value.id,
                                    proofOutboxItemId = result.value.outboxItemId,
                                    serverProofId = result.value.serverProofId,
                                ),
                            )
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
                                analytics.track(
                                    AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED,
                                    pcCareStockProofAnalyticsProps(
                                        fieldKey = slotFieldKey,
                                        mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                        status = detail.status,
                                        outcome = "failure",
                                        reason = "missing_upload_outbox",
                                        proofRowId = result.value.id,
                                        serverProofId = result.value.serverProofId,
                                    ),
                                )
                                local.update { it.copy(message = "$proofNoun didn't queue. Retry upload.") }
                                return@withContext
                            }
                            analytics.track(
                                AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED,
                                pcCareStockProofAnalyticsProps(
                                    fieldKey = slotFieldKey,
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                    status = detail.status,
                                    outcome = "success",
                                    source = "room",
                                    proofRowId = result.value.id,
                                    proofOutboxItemId = proofOutboxId,
                                    serverProofId = result.value.serverProofId,
                                ),
                            )
                            captureDrafts.putProof(CaptureFlow.PC_CARE, taskId, slotFieldKey, proofOutboxId)
                            // A round-grain removal's key carries its pen; the register splits it back out so the
                            // write lands on THAT pen's evidence row rather than on the card.
                            val (gatedTaskId, wireSlot) = pcCareSplitRemovalSlotKey(slotFieldKey)
                            when (
                                val registered = repository.registerTaskProof(
                                    taskId,
                                    wireSlot,
                                    proofOutboxId,
                                    gatedTaskId,
                                )
                            ) {
                                is AppResult.Ok -> {
                                    analytics.track(
                                        AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION,
                                        pcCareStockProofAnalyticsProps(
                                            fieldKey = slotFieldKey,
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            status = detail.status,
                                            outcome = "success",
                                            source = "outbox",
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                            serverProofId = result.value.serverProofId,
                                            outboxItemId = registered.value,
                                        ),
                                    )
                                    analytics.track(
                                        AnalyticsEvents.PC_CARE_SLOT_CAPTURED,
                                        pcCareStockProofAnalyticsProps(
                                            fieldKey = slotFieldKey,
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            status = detail.status,
                                            outcome = "success",
                                            source = "capture_repository",
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                            serverProofId = result.value.serverProofId,
                                        ),
                                    )
                                }
                                is AppResult.Err -> {
                                    registered.cause?.let { crashReporter.recordException(it, "pc care task proof registration enqueue failed") }
                                    analytics.track(
                                        AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION,
                                        pcCareStockProofAnalyticsProps(
                                            fieldKey = slotFieldKey,
                                            mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                            status = detail.status,
                                            outcome = "failure",
                                            reason = registered.message.take(MAX_REASON_CHARS),
                                            proofRowId = result.value.id,
                                            proofOutboxItemId = proofOutboxId,
                                            serverProofId = result.value.serverProofId,
                                        ),
                                    )
                                    local.update { it.copy(message = "$proofNoun saved, but couldn't be attached. Tap refresh to retry.") }
                                }
                            }
                        }
                        is AppResult.Err -> {
                            result.cause?.let { crashReporter.recordException(it, "pc care task proof capture enqueue failed") }
                            analytics.track(
                                AnalyticsEvents.PC_CARE_STOCK_PROOF_ROOM_WRITTEN,
                                pcCareStockProofAnalyticsProps(
                                    fieldKey = slotFieldKey,
                                    mediaKind = pcCareMediaKindFromMime(captured.mimeType),
                                    status = detail.status,
                                    outcome = "failure",
                                    reason = result.message.take(MAX_REASON_CHARS),
                                ),
                            )
                            local.update { it.copy(message = result.message) }
                        }
                    }
                }
            } finally {
                local.update { it.copy(capturingSlotKey = null) }
            }
        }
    }

    private fun retryPcCareTaskProofUpload(
        detail: PcCareTaskDto,
        slotFieldKey: String,
        mediaKind: String,
        row: ProofCaptureRow,
    ) {
        analytics.track(
            AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED,
            pcCareStockProofAnalyticsProps(
                fieldKey = slotFieldKey,
                mediaKind = mediaKind,
                status = detail.status,
                outcome = "retry_requested",
                source = "proof_row",
            ) + mapOf(
                AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to row.outboxItemId.orEmpty(),
                "local_proof_row_id" to row.id,
            ),
        )
        local.update { it.copy(message = "Retrying saved proof upload…") }
        viewModelScope.launch {
            when (val retry = proofCaptureRepository.retryUpload(taskId, row.id)) {
                is AppResult.Ok -> {
                    reconcileSlotRegistrations()
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = slotFieldKey,
                            mediaKind = pcCareMediaKindFromMime(row.mimeType),
                            status = detail.status,
                            outcome = "retry_enqueued",
                            source = "proof_row",
                        ) + mapOf(
                            AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to row.outboxItemId.orEmpty(),
                            "local_proof_row_id" to row.id,
                        ),
                    )
                    local.update { it.copy(message = "Saved proof upload retry queued.") }
                }
                is AppResult.Err -> {
                    retry.cause?.let { crashReporter.recordException(it, "pc care task proof upload retry failed") }
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_UPLOAD_ENQUEUED,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = slotFieldKey,
                            mediaKind = pcCareMediaKindFromMime(row.mimeType),
                            status = detail.status,
                            outcome = "retry_failed",
                            reason = retry.message.take(MAX_REASON_CHARS),
                            source = "proof_row",
                        ) + mapOf(
                            AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID to row.outboxItemId.orEmpty(),
                            "local_proof_row_id" to row.id,
                        ),
                    )
                    local.update { it.copy(message = retry.message) }
                }
            }
        }
    }

    private fun observeAnimalSlotProofUploads(proofs: List<ProofCaptureRow>) {
        proofs
            .filter { row ->
                row.outboxItemId?.isNotBlank() == true &&
                    row.fieldKey.contains(':') &&
                    !row.fieldKey.contains("::") &&
                    row.syncStatus != CaptureSyncStatus.FAILED
            }
            .forEach { row ->
                val itemId = row.outboxItemId.orEmpty()
                if (proofUploadObserveJobs.containsKey(itemId)) return@forEach
                proofUploadObserveJobs[itemId] = viewModelScope.launch {
                    syncRepository.observeItem(itemId).collect { item ->
                        if (item != null) trackAnimalSlotProofUploadTerminal(row, item)
                    }
                }
            }
    }

    private fun trackAnimalSlotProofUploadTerminal(row: ProofCaptureRow, item: SyncQueueItem) {
        val outcome = when {
            item.status == SyncItemStatus.SUCCEEDED -> "success"
            item.isTerminalFailure -> "failure"
            else -> return
        }
        if (!animalSlotProofTerminalEventsTracked.add(item.id)) return
        val latestRow = latestProofs.firstOrNull { it.id == row.id } ?: row
        val tag = latestRow.fieldKey.substringBefore(':')
        val slot = latestRow.fieldKey.substringAfter(':')
        val animal = latestAnimals.firstOrNull { it.normalizedTag == tag }
        analytics.track(
            AnalyticsEvents.PC_CARE_SLOT_UPLOAD_SYNCED,
            pcCareAnimalSlotAnalyticsProps(
                tagKey = tag,
                tagVerbatim = animal?.tagVerbatim ?: latestRow.rfidTag ?: tag,
                slotFieldKey = slot,
                slotKey = latestRow.fieldKey,
                outcome = outcome,
                action = "proof_upload_sync",
                reason = if (item.isTerminalFailure) {
                    item.lastError?.takeIf { it.isNotBlank() } ?: if (item.conflict) "conflict" else "attempts_exhausted"
                } else {
                    null
                },
                source = "proof_upload_outbox",
                mediaKind = pcCareMediaKindFromMime(latestRow.mimeType),
                proofRowId = latestRow.id,
                proofOutboxItemId = item.id,
                serverProofId = latestRow.serverProofId,
            ),
        )
    }

    private fun observeTaskProofUploads(proofs: List<ProofCaptureRow>) {
        val detail = latestDetail?.takeIf { pcCareIsTaskProofMode(it) } ?: return
        val expectedSlotKeys = pcCareTaskProofExpectedSlots(detail).mapTo(mutableSetOf()) { it.fieldKey }
        proofs
            .filter { row ->
                row.outboxItemId?.isNotBlank() == true &&
                    row.fieldKey in expectedSlotKeys &&
                    row.syncStatus != CaptureSyncStatus.FAILED
            }
            .forEach { row ->
                val itemId = row.outboxItemId.orEmpty()
                if (proofUploadObserveJobs.containsKey(itemId)) return@forEach
                proofUploadObserveJobs[itemId] = viewModelScope.launch {
                    syncRepository.observeItem(itemId).collect { item ->
                        if (item != null) trackTaskProofUploadTerminal(row, item)
                    }
                }
            }
    }

    private fun trackTaskProofUploadTerminal(row: ProofCaptureRow, item: SyncQueueItem) {
        val outcome = when {
            item.status == SyncItemStatus.SUCCEEDED -> "success"
            item.isTerminalFailure -> "failure"
            else -> return
        }
        if (!taskProofTerminalEventsTracked.add(item.id)) return
        val latestRow = latestProofs.firstOrNull { it.id == row.id } ?: row
        analytics.track(
            AnalyticsEvents.PC_CARE_TASK_PROOF_UPLOAD_SYNCED,
            pcCareStockProofAnalyticsProps(
                fieldKey = latestRow.fieldKey,
                mediaKind = pcCareMediaKindFromMime(latestRow.mimeType),
                status = latestDetail?.status.orEmpty(),
                outcome = outcome,
                action = "proof_upload_sync",
                reason = if (item.isTerminalFailure) {
                    item.lastError?.takeIf { it.isNotBlank() } ?: if (item.conflict) "conflict" else "attempts_exhausted"
                } else {
                    null
                },
                source = "proof_upload_outbox",
                proofRowId = latestRow.id,
                proofOutboxItemId = item.id,
                serverProofId = latestRow.serverProofId,
            ),
        )
    }

    private fun trackBusinessProofAcks(
        detail: PcCareTaskDto?,
        animals: List<PcCareAnimalRowEntity>,
        taskProofs: List<PcCareTaskProofDto>,
    ) {
        if (detail == null) return
        if (pcCareIsTaskProofMode(detail)) {
            taskProofs
                .filter { it.slotKey.isNotBlank() && it.proofRef.isNotBlank() }
                .forEach { proof ->
                    val key = proof.slotKey + ":" + proof.proofRef
                    if (!taskProofBusinessAckEventsTracked.add(key)) return@forEach
                    analytics.track(
                        AnalyticsEvents.PC_CARE_TASK_PROOF_BUSINESS_ACK,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = proof.slotKey,
                            mediaKind = pcCareTaskProofMediaKind(proof.slotKey),
                            status = detail.status,
                            category = detail.category,
                            captureMode = detail.captureMode,
                            outcome = "business_ack_visible",
                            source = if (proof.slotKey.contains("::")) "backend_removal_pen" else "backend_task_detail",
                            serverProofId = proof.proofRef,
                        ),
                    )
                }
            return
        }
        animals.forEach { animal ->
            val tagKey = animal.normalizedTag
            decodeServerSlots(json, animal.serverSlotsJson)
                .filter { it.fieldKey.isNotBlank() && it.proofRef.isNotBlank() }
                .forEach { slot ->
                    val slotKey = pcCareSlotProofFieldKey(tagKey, slot.fieldKey)
                    val key = slotKey + ":" + slot.proofRef
                    if (!animalSlotBusinessAckEventsTracked.add(key)) return@forEach
                    analytics.track(
                        AnalyticsEvents.PC_CARE_SLOT_BUSINESS_ACK,
                        pcCareAnimalSlotAnalyticsProps(
                            tagKey = tagKey,
                            tagVerbatim = animal.tagVerbatim,
                            slotFieldKey = slot.fieldKey,
                            slotKey = slotKey,
                            outcome = "business_ack_visible",
                            action = "backend_business_link_visible",
                            source = "backend_task_detail",
                            serverProofId = slot.proofRef,
                        ),
                    )
                }
        }
    }

    private fun armSubmit() {
        val bits = local.value
        if (bits.submitInFlight || bits.submitQueued) return
        val detail = latestDetail ?: return
        if (isLifecycleLocked(detail)) return
        val taskProofMode = pcCareIsTaskProofMode(detail)
        if (taskProofMode) {
            analytics.track(
                AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
                pcCareStockProofAnalyticsProps(status = detail.status, outcome = "tapped", source = "submit_bar"),
            )
        }
        val evaluation = if (taskProofMode) {
            pcCareEvaluateTaskProofSubmit(
                pcCareTaskProofExpectedSlots(detail),
                latestProofs,
                pcCareEffectiveTaskProofs(detail, local.value.removalPens),
                local.value.capturingSlotKey,
                missingCopy = if (pcCareIsFeedWaterRemoval(detail)) {
                    "Record the feed removal and water removal videos first" // mobile-contract:ignore: device-local pre-sync gate copy
                } else {
                    "Record the fridge stock photo and video first" // mobile-contract:ignore: device-local pre-sync gate copy
                },
            )
        } else {
            pcCareEvaluateSubmit(detail.expectedSlots, latestAnimals, latestProofs, json)
        }
        if (!evaluation.ready) {
            analytics.track(
                AnalyticsEvents.PC_CARE_SUBMIT_BLOCKED,
                mapOf(AnalyticsEvents.Params.REASON to evaluation.blockedReason.take(MAX_REASON_CHARS)),
            )
            if (taskProofMode) {
                analytics.track(
                    AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
                    pcCareStockProofAnalyticsProps(
                        status = detail.status,
                        outcome = "blocked",
                        reason = evaluation.blockedReason,
                        source = "submit_bar",
                    ),
                )
            }
            local.update { it.copy(message = evaluation.blockedReason) }
            return
        }
        if (taskProofMode) {
            analytics.track(
                AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
                pcCareStockProofAnalyticsProps(status = detail.status, outcome = "confirmation_shown", source = "submit_bar"),
            )
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
        val evaluation = if (pcCareIsTaskProofMode(detail)) {
            pcCareEvaluateTaskProofSubmit(
                pcCareTaskProofExpectedSlots(detail),
                latestProofs,
                pcCareEffectiveTaskProofs(detail, local.value.removalPens),
                local.value.capturingSlotKey,
                missingCopy = if (pcCareIsFeedWaterRemoval(detail)) {
                    "Record the feed removal and water removal videos first" // mobile-contract:ignore: device-local pre-sync gate copy
                } else {
                    "Record the fridge stock photo and video first" // mobile-contract:ignore: device-local pre-sync gate copy
                },
            )
        } else {
            pcCareEvaluateSubmit(detail.expectedSlots, latestAnimals, latestProofs, json)
        }
        if (!evaluation.ready) {
            // The task stopped being submittable between arm and confirm — close and say so,
            // rather than submitting stale work or silently doing nothing.
            if (pcCareIsTaskProofMode(detail)) {
                analytics.track(
                    AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
                    pcCareStockProofAnalyticsProps(
                        status = detail.status,
                        outcome = "blocked_on_confirm",
                        reason = evaluation.blockedReason,
                        source = "submit_dialog",
                    ),
                )
            }
            local.update { it.copy(showSubmitConfirmation = false, message = evaluation.blockedReason) }
            return
        }
        local.update { it.copy(showSubmitConfirmation = false, submitInFlight = true) }
        viewModelScope.launch {
            reconcileSlotRegistrations()
            syncRepository.triggerDrain()
            when (val result = repository.submitTask(taskId, detail.rowVersion)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEvents.PC_CARE_SUBMIT_CONFIRMED)
                    if (pcCareIsTaskProofMode(detail)) {
                        analytics.track(
                            AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT_ENQUEUED,
                            pcCareStockProofAnalyticsProps(status = detail.status, outcome = "success", source = "submit_dialog") +
                                pcCareTaskProofTraceProps() +
                                mapOf(AnalyticsEvents.Params.OUTBOX_ITEM_ID to result.value),
                        )
                    } else {
                        analytics.track(
                            AnalyticsEvents.PC_CARE_SLOT_SUBMIT,
                            pcCareAnimalSlotSubmitTraceProps(
                                outcome = "enqueued",
                                source = "submit_dialog",
                                outboxItemId = result.value,
                            ),
                        )
                    }
                    local.update { it.copy(submitInFlight = false, submitQueued = true, submitOutboxItemId = result.value) }
                    observeQueuedSubmit(result.value)
                }
                is AppResult.Err -> {
                    // Single-flight latch RESETS on terminal failure, or one failed enqueue
                    // wedges the button forever (feed 2693a21f7 lesson).
                    result.cause?.let { crashReporter.recordException(it, "pc care submit enqueue failed") }
                    analytics.track(
                        AnalyticsEvents.PC_CARE_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)),
                    )
                    if (pcCareIsTaskProofMode(detail)) {
                        analytics.track(
                            AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT_ENQUEUED,
                            pcCareStockProofAnalyticsProps(
                                status = detail.status,
                                outcome = "failure",
                                reason = result.message,
                                source = "submit_dialog",
                            ) + pcCareTaskProofTraceProps(),
                        )
                    } else {
                        analytics.track(
                            AnalyticsEvents.PC_CARE_SLOT_SUBMIT,
                            pcCareAnimalSlotSubmitTraceProps(
                                outcome = "failure",
                                reason = result.message,
                                source = "submit_dialog",
                            ),
                        )
                    }
                    local.update { it.copy(submitInFlight = false, submitOutboxItemId = null, message = "Couldn't send the task. Try again.") }
                }
            }
        }
    }

    private fun observeQueuedSubmit(outboxItemId: String) {
        submitObserveJob?.cancel()
        submitObserveJob = viewModelScope.launch {
            syncRepository.observeItem(outboxItemId).collect { item ->
                when {
                    item == null -> Unit
                    item.status == SyncItemStatus.SUCCEEDED -> {
                        if (pcCareIsTaskProofMode(latestDetail)) {
                            analytics.track(
                                AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
                                pcCareStockProofAnalyticsProps(
                                    status = latestDetail?.status.orEmpty(),
                                    outcome = "sync_success",
                                    source = "outbox_observer",
                                ) + pcCareTaskProofTraceProps() + mapOf(AnalyticsEvents.Params.OUTBOX_ITEM_ID to outboxItemId),
                            )
                        } else {
                            analytics.track(
                                AnalyticsEvents.PC_CARE_SLOT_SUBMIT,
                                pcCareAnimalSlotSubmitTraceProps(
                                    outcome = "sync_success",
                                    source = "outbox_observer",
                                    outboxItemId = outboxItemId,
                                ),
                            )
                        }
                        local.update { current ->
                            if (current.submitOutboxItemId == outboxItemId) {
                                current.copy(submitQueued = false, submitOutboxItemId = null)
                            } else {
                                current
                            }
                        }
                        refresh()
                    }
                    item.isTerminalFailure -> {
                        if (pcCareIsTaskProofMode(latestDetail)) {
                            analytics.track(
                                AnalyticsEvents.PC_CARE_STOCK_PROOF_SUBMIT,
                                pcCareStockProofAnalyticsProps(
                                    status = latestDetail?.status.orEmpty(),
                                    outcome = "sync_terminal_failure",
                                    reason = item.lastError.orEmpty(),
                                    source = "outbox_observer",
                                ) + pcCareTaskProofTraceProps() + mapOf(AnalyticsEvents.Params.OUTBOX_ITEM_ID to outboxItemId),
                            )
                        } else {
                            analytics.track(
                                AnalyticsEvents.PC_CARE_SLOT_SUBMIT,
                                pcCareAnimalSlotSubmitTraceProps(
                                    outcome = "sync_terminal_failure",
                                    reason = item.lastError.orEmpty(),
                                    source = "outbox_observer",
                                    outboxItemId = outboxItemId,
                                ),
                            )
                        }
                        local.update { current ->
                            if (current.submitOutboxItemId == outboxItemId) {
                                current.copy(
                                    submitQueued = false,
                                    submitOutboxItemId = null,
                                    message = "Task submit failed. Check the proofs and try again.",
                                )
                            } else {
                                current
                            }
                        }
                    }
                }
            }
        }
    }

    /**
     * Pulls a round-grain removal card's PENS. Failure is swallowed on purpose: the card still
     * renders from what is cached, and a removal with no pens falls back to the flat two-slot
     * face rather than showing the operator an empty screen.
     */
    private suspend fun refreshRemovalPens() {
        if (!pcCareIsFeedWaterRemoval(latestDetail)) return
        repository.refreshRemovalPens(taskId)
    }

    private fun refresh() {
        if (local.value.isRefreshing) return
        val detail = latestDetail
        if (pcCareIsTaskProofMode(detail)) {
            analytics.track(
                AnalyticsEvents.PC_CARE_STOCK_PROOF_SYNC,
                pcCareStockProofAnalyticsProps(status = detail?.status.orEmpty(), outcome = "started", source = "refresh_button"),
            )
        }
        local.update { it.copy(isRefreshing = true) }
        viewModelScope.launch {
            try {
                reconcileSlotRegistrations()
                syncRepository.triggerDrain()
                repository.refreshTaskDetail(taskId)
                repository.pollTaskOnce(taskId)
                refreshRemovalPens()
                if (pcCareIsTaskProofMode(detail)) {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_SYNC,
                        pcCareStockProofAnalyticsProps(status = latestDetail?.status ?: detail?.status.orEmpty(), outcome = "completed", source = "refresh_button"),
                    )
                }
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
            // A ROUND-grain removal key is "<gated task id>::<slot>" and it is matched FIRST.
            // It also contains a colon, so the animal branch below would otherwise claim it and
            // file a pen's removal video as some ANIMAL's scan proof, with a task id for a tag
            // and ":feed_video" for a slot — a wrong write, not merely a missed retry.
            val (removalGatedTaskId, removalSlot) = pcCareSplitRemovalSlotKey(row.fieldKey)
            if (removalGatedTaskId.isNotBlank() && pcCareTaskProofSlotKeys.contains(removalSlot)) {
                if (!pcCareStockSlotMatchesMime(removalSlot, row.mimeType)) return@forEach
                val result = repository.registerTaskProof(taskId, removalSlot, outboxId, removalGatedTaskId)
                analytics.track(
                    AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION,
                    pcCareStockProofAnalyticsProps(
                        fieldKey = row.fieldKey,
                        mediaKind = pcCareMediaKindFromMime(row.mimeType),
                        status = latestDetail?.status.orEmpty(),
                        outcome = if (result is AppResult.Ok) "success" else "failure",
                        reason = (result as? AppResult.Err)?.message,
                        source = "reconcile",
                        proofRowId = row.id,
                        proofOutboxItemId = outboxId,
                        serverProofId = row.serverProofId,
                        outboxItemId = (result as? AppResult.Ok)?.value,
                    ),
                )
                return@forEach
            }
            val hasAnimalSlotKey = row.fieldKey.contains(':')
            val tag = row.fieldKey.substringBefore(':')
            val slot = row.fieldKey.substringAfter(':')
            if (hasAnimalSlotKey && tag.isNotBlank() && slot.isNotBlank()) {
                val result = repository.registerSlotProof(taskId, tag, slot, outboxId)
                analytics.track(
                    AnalyticsEvents.PC_CARE_SLOT_REGISTRATION,
                    pcCareAnimalSlotAnalyticsProps(
                        tagKey = tag,
                        tagVerbatim = tag,
                        slotFieldKey = slot,
                        slotKey = row.fieldKey,
                        outcome = if (result is AppResult.Ok) "success" else "failure",
                        reason = (result as? AppResult.Err)?.message,
                        mediaKind = pcCareMediaKindFromMime(row.mimeType),
                        proofRowId = row.id,
                        proofOutboxItemId = outboxId,
                        serverProofId = row.serverProofId,
                        outboxItemId = (result as? AppResult.Ok)?.value,
                    ),
                )
            } else if (pcCareTaskProofSlotKeys.contains(row.fieldKey)) {
                if (!pcCareStockSlotMatchesMime(pcCareSplitRemovalSlotKey(row.fieldKey).second, row.mimeType)) {
                    analytics.track(
                        AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION,
                        pcCareStockProofAnalyticsProps(
                            fieldKey = row.fieldKey,
                            mediaKind = pcCareMediaKindFromMime(row.mimeType),
                            status = latestDetail?.status.orEmpty(),
                            outcome = "failure",
                            reason = "slot_media_mismatch",
                            source = "reconcile",
                            proofRowId = row.id,
                            proofOutboxItemId = outboxId,
                            serverProofId = row.serverProofId,
                        ),
                    )
                    return@forEach
                }
                val slotFieldKey = row.fieldKey
                val (reconcileGatedTaskId, reconcileWireSlot) = pcCareSplitRemovalSlotKey(slotFieldKey)
                val result = repository.registerTaskProof(
                    taskId,
                    reconcileWireSlot,
                    outboxId,
                    reconcileGatedTaskId,
                )
                analytics.track(
                    AnalyticsEvents.PC_CARE_STOCK_PROOF_REGISTRATION,
                    pcCareStockProofAnalyticsProps(
                        fieldKey = slotFieldKey,
                        mediaKind = pcCareMediaKindFromMime(row.mimeType),
                        status = latestDetail?.status.orEmpty(),
                        outcome = if (result is AppResult.Ok) "success" else "failure",
                        reason = (result as? AppResult.Err)?.message,
                        source = "reconcile",
                        proofRowId = row.id,
                        proofOutboxItemId = outboxId,
                        serverProofId = row.serverProofId,
                        outboxItemId = (result as? AppResult.Ok)?.value,
                    ),
                )
            }
        }
    }

    private fun pcCareStockProofAnalyticsProps(
        fieldKey: String = PC_CARE_SLOT_STOCK_FRIDGE_VIDEO,
        mediaKind: String = "task_proof",
        status: String = latestDetail?.status.orEmpty(),
        category: String = latestDetail?.category.orEmpty(),
        captureMode: String = latestDetail?.captureMode.orEmpty(),
        action: String? = null,
        outcome: String? = null,
        reason: String? = null,
        source: String? = null,
        proofRowId: String? = null,
        proofOutboxItemId: String? = null,
        serverProofId: String? = null,
        outboxItemId: String? = null,
    ): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.KIND, mediaKind)
        put(AnalyticsEvents.Params.FIELD, fieldKey)
        put("field_key", fieldKey)
        put("task_id", taskId)
        put("feature_surface", if (category == PC_CARE_CATEGORY_FEED_WATER_REMOVAL) "pc_care_feed_water_removal" else "pc_care_stock")
        category.takeIf { it.isNotBlank() }?.let { put("category", it) }
        captureMode.takeIf { it.isNotBlank() }?.let { put("capture_mode", it) }
        status.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.STATUS, it) }
        action?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.ACTION, it) }
        outcome?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTCOME, it) }
        reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        source?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.SOURCE, it) }
        proofRowId?.takeIf { it.isNotBlank() }?.let { put("local_proof_row_id", it) }
        proofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it) }
        serverProofId?.takeIf { it.isNotBlank() }?.let { put("server_proof_id", it) }
        outboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, it) }
    }

    private fun pcCareScanAnalyticsProps(
        tagVerbatim: String,
        outcome: String,
        source: String,
        reason: String? = null,
        outboxItemId: String? = null,
        groupKey: String? = null,
        idempotencyKey: String? = null,
    ): Map<String, String> = buildMap {
        put("task_id", taskId)
        put("feature_surface", "pc_care_scan")
        put(AnalyticsEvents.Params.RFID, tagVerbatim)
        put("normalized_rfid", tagVerbatim.trim().lowercase())
        latestDetail?.category?.takeIf { it.isNotBlank() }?.let { put("category", it) }
        latestDetail?.captureMode?.takeIf { it.isNotBlank() }?.let { put("capture_mode", it) }
        latestDetail?.status?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.STATUS, it) }
        put(AnalyticsEvents.Params.OUTCOME, outcome)
        put(AnalyticsEvents.Params.SOURCE, source)
        reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        outboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, it) }
        groupKey?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.GROUP_KEY, it) }
        idempotencyKey?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.IDEMPOTENCY_KEY, it) }
    }

    private fun pcCareAnimalSlotAnalyticsProps(
        tagKey: String,
        tagVerbatim: String,
        slotFieldKey: String,
        slotKey: String,
        outcome: String,
        mediaKind: String = "video",
        legacyKind: String? = null,
        action: String? = null,
        reason: String? = null,
        source: String? = null,
        proofRowId: String? = null,
        proofOutboxItemId: String? = null,
        serverProofId: String? = null,
        outboxItemId: String? = null,
    ): Map<String, String> = buildMap {
        put(AnalyticsEvents.Params.KIND, legacyKind ?: mediaKind)
        if (legacyKind != null) put("media_kind", mediaKind)
        put(AnalyticsEvents.Params.FIELD, slotFieldKey)
        put("field_key", slotKey)
        put("slot_field_key", slotFieldKey)
        put("task_id", taskId)
        put(AnalyticsEvents.Params.RFID, tagVerbatim)
        put("normalized_rfid", tagKey)
        put("feature_surface", "pc_care_animal_slot")
        latestDetail?.category?.takeIf { it.isNotBlank() }?.let { put("category", it) }
        latestDetail?.captureMode?.takeIf { it.isNotBlank() }?.let { put("capture_mode", it) }
        action?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.ACTION, it) }
        put(AnalyticsEvents.Params.OUTCOME, outcome)
        latestDetail?.status?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.STATUS, it) }
        reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        source?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.SOURCE, it) }
        proofRowId?.takeIf { it.isNotBlank() }?.let { put("local_proof_row_id", it) }
        proofOutboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it) }
        serverProofId?.takeIf { it.isNotBlank() }?.let { put("server_proof_id", it) }
        outboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, it) }
    }

    private fun pcCareTaskProofTraceProps(): Map<String, String> {
        val ui = state.value
        val slots = (ui.taskProofSlots + listOfNotNull(ui.taskProofSlot, ui.taskProofPhotoSlot, ui.taskProofVideoSlot))
            .distinctBy { it.fieldKey }
        return buildMap {
            slots.firstNotNullOfOrNull { it.localProofRowId?.takeIf(String::isNotBlank) }?.let {
                put("local_proof_row_id", it)
                put(AnalyticsEvents.Params.PROOF_ID, it)
            }
            slots.firstNotNullOfOrNull { it.proofOutboxItemId?.takeIf(String::isNotBlank) }?.let {
                put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it)
            }
            slots.firstNotNullOfOrNull { it.serverProofId?.takeIf(String::isNotBlank) }?.let {
                put("server_proof_id", it)
            }
            slots.mapNotNull { it.localProofRowId?.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
                put("local_proof_row_ids", it.joinToString(","))
            }
            slots.mapNotNull { it.proofOutboxItemId?.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
                put("proof_outbox_item_ids", it.joinToString(","))
            }
            slots.mapNotNull { it.serverProofId?.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
                put("server_proof_ids", it.joinToString(","))
            }
        }
    }

    private fun pcCareAnimalSlotSubmitTraceProps(
        outcome: String,
        source: String,
        reason: String? = null,
        outboxItemId: String? = null,
    ): Map<String, String> = buildMap {
        val animalProofs = latestProofs.filter { it.fieldKey.contains(':') && it.syncStatus != CaptureSyncStatus.FAILED }
        put("task_id", taskId)
        put("feature_surface", "pc_care_animal_slot")
        latestDetail?.category?.takeIf { it.isNotBlank() }?.let { put("category", it) }
        latestDetail?.captureMode?.takeIf { it.isNotBlank() }?.let { put("capture_mode", it) }
        latestDetail?.status?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.STATUS, it) }
        put(AnalyticsEvents.Params.ACTION, "submit")
        put(AnalyticsEvents.Params.OUTCOME, outcome)
        put(AnalyticsEvents.Params.SOURCE, source)
        reason?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.REASON, it.take(MAX_REASON_CHARS)) }
        outboxItemId?.takeIf { it.isNotBlank() }?.let { put(AnalyticsEvents.Params.OUTBOX_ITEM_ID, it) }
        latestAnimals.mapNotNull { it.normalizedTag.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
            put("normalized_rfids", it.joinToString(","))
        }
        latestAnimals.mapNotNull { it.tagVerbatim.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
            put(AnalyticsEvents.Params.RFID, it.first())
            put("rfids", it.joinToString(","))
        }
        animalProofs.map { it.fieldKey }.takeIf { it.isNotEmpty() }?.let { put("field_keys", it.joinToString(",")) }
        animalProofs.map { it.fieldKey.substringAfter(':') }.distinct().takeIf { it.isNotEmpty() }?.let {
            put(AnalyticsEvents.Params.FIELD, it.joinToString(","))
        }
        animalProofs.map { it.id }.takeIf { it.isNotEmpty() }?.let {
            put("local_proof_row_id", it.first())
            put("local_proof_row_ids", it.joinToString(","))
        }
        animalProofs.mapNotNull { it.outboxItemId?.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
            put(AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID, it.first())
            put("proof_outbox_item_ids", it.joinToString(","))
        }
        animalProofs.mapNotNull { it.serverProofId?.takeIf(String::isNotBlank) }.takeIf { it.isNotEmpty() }?.let {
            put("server_proof_id", it.first())
            put("server_proof_ids", it.joinToString(","))
        }
    }

    private fun pcCareMediaKindFromMime(mimeType: String): String =
        if (mimeType.startsWith("image/", ignoreCase = true)) "photo" else "video"

    private fun pcCareTaskProofMediaKind(slotKey: String): String =
        if (slotKey.endsWith(PC_CARE_SLOT_STOCK_FRIDGE_PHOTO)) "photo" else "video"

    private fun pcCareStockSlotMatchesMime(fieldKey: String, mimeType: String): Boolean =
        when (fieldKey) {
            PC_CARE_SLOT_STOCK_FRIDGE_PHOTO -> mimeType.startsWith("image/", ignoreCase = true)
            PC_CARE_SLOT_STOCK_FRIDGE_VIDEO,
            // Both removal slots are VIDEO (backend slot contract, maintainer decision 2026-09-03).
            PC_CARE_SLOT_FEED_VIDEO,
            PC_CARE_SLOT_WATER_VIDEO,
            -> mimeType.startsWith("video/", ignoreCase = true)
            else -> false
        }

    // ---- State assembly ----------------------------------------------------------------------

    private fun pcCareIsTaskProofMode(detail: PcCareTaskDto?): Boolean =
        // The route category settles the fridge-stock face before the detail arrives, so the
        // scan-and-record row never flashes on the way in. feed_water_removal (maintainer
        // decision 2026-09-03) is the second task_proof face; its cards live in the Deworming
        // tab, so its own capture_mode/category is what settles it.
        routeCategory == PC_CARE_CATEGORY_INVENTORY_VACCINE ||
            routeCategory == PC_CARE_CATEGORY_FEED_WATER_REMOVAL ||
            detail?.captureMode == PC_CARE_CAPTURE_MODE_TASK_PROOF ||
            detail?.category == PC_CARE_CATEGORY_INVENTORY_VACCINE ||
            detail?.category == PC_CARE_CATEGORY_FEED_WATER_REMOVAL

    /**
     * A round-grain removal's slot key carries the PEN as well as the slot, because the same
     * two slot names repeat once per pen and a bare slot key would collide across them. The
     * capture pipeline keys everything by this string, so nothing else needs to know a pen
     * exists; only the slot builder writes it and only the register call reads it back.
     */
    private fun pcCareRemovalSlotKey(gatedTaskId: String, slot: String) = gatedTaskId + "::" + slot

    /** Splits [pcCareRemovalSlotKey] back into (gated task id, slot); blank id when not one. */
    private fun pcCareSplitRemovalSlotKey(key: String): Pair<String, String> {
        val at = key.indexOf("::")
        return if (at < 0) "" to key else key.substring(0, at) to key.substring(at + 2)
    }

    /** True on the feed & water removal face — two backend-served VIDEO slots, no fridge pair. */
    private fun pcCareIsFeedWaterRemoval(detail: PcCareTaskDto?): Boolean =
        routeCategory == PC_CARE_CATEGORY_FEED_WATER_REMOVAL ||
            detail?.category == PC_CARE_CATEGORY_FEED_WATER_REMOVAL

    private fun pcCareTaskProofPreviewEvent(detail: PcCareTaskDto?): String =
        if (pcCareIsFeedWaterRemoval(detail)) {
            AnalyticsEvents.PC_CARE_FEED_WATER_PROOF_PREVIEW
        } else {
            AnalyticsEvents.PC_CARE_STOCK_PROOF_PREVIEW
        }

    private fun pcCareTaskTitle(detail: PcCareTaskDto?): String {
        val raw = categoryTitle.trim()
        val normalized = raw.lowercase().replace("%20", " ").replace("_", " ")
        if (
            pcCareIsFeedWaterRemoval(detail) ||
            raw == PC_CARE_CATEGORY_FEED_WATER_REMOVAL ||
            normalized == "feed water removal"
        ) {
            return "Remove feed & water"
        }
        return raw.ifBlank { detail?.category.orEmpty() }
    }

    private fun pcCareEffectiveExpectedSlots(
        detail: PcCareTaskDto?,
        removalPens: List<PcCareRemovalPenDto>,
    ): List<PcCareSlotDto> {
        if (detail == null) return emptyList()
        if (!pcCareIsTaskProofMode(detail)) return detail.expectedSlots
        return pcCareTaskProofExpectedSlots(detail, removalPens)
    }

    private fun pcCareTaskProofExpectedSlots(
        detail: PcCareTaskDto,
        removalPens: List<PcCareRemovalPenDto> = local.value.removalPens,
    ): List<PcCareSlotDto> {
        val byKey = detail.expectedSlots.associateBy { it.fieldKey }
        if (pcCareIsFeedWaterRemoval(detail)) {
            // A ROUND's removal is proved PEN BY PEN: the backend's two slots repeat once per
            // pen, each keyed and labelled by the pen so the operator (and later the verifier)
            // can tell which pen a clip proves. A legacy single-pen removal has no pens and
            // keeps the flat two-slot face byte for byte.
            val pens = removalPens
            if (pens.isNotEmpty()) {
                val slots = if (detail.expectedSlots.isNotEmpty()) {
                    detail.expectedSlots
                } else {
                    listOf(
                        PcCareSlotDto(fieldKey = PC_CARE_SLOT_FEED_VIDEO, label = "Feed removal video"),
                        PcCareSlotDto(fieldKey = PC_CARE_SLOT_WATER_VIDEO, label = "Water removal video"),
                    )
                }
                return pens.flatMap { pen ->
                    slots.map { slot ->
                        slot.copy(
                            fieldKey = pcCareRemovalSlotKey(pen.gatedTaskId, slot.fieldKey),
                            // The pen label is BACKEND-COMPOSED and rendered verbatim; the phone
                            // never builds a pen name of its own.
                            label = pen.penLabel + " · " + slot.label,
                        )
                    }
                }
            }
            // The removal card's slot set is BACKEND-SERVED (feed_video + water_video with their
            // own labels/descriptions); the fallbacks below only cover a detail cached by an
            // older server that had not sent slots yet.
            if (detail.expectedSlots.isNotEmpty()) return detail.expectedSlots
            return listOf(
                PcCareSlotDto(fieldKey = PC_CARE_SLOT_FEED_VIDEO, label = "Feed removal video"),
                PcCareSlotDto(fieldKey = PC_CARE_SLOT_WATER_VIDEO, label = "Water removal video"),
            )
        }
        return listOf(
            byKey[PC_CARE_SLOT_STOCK_FRIDGE_PHOTO] ?: PcCareSlotDto(
                fieldKey = PC_CARE_SLOT_STOCK_FRIDGE_PHOTO,
                label = "Fridge stock photo",
                description = "Take a clear photo of the vaccine stock available in the fridge",
            ),
            byKey[PC_CARE_SLOT_STOCK_FRIDGE_VIDEO] ?: PcCareSlotDto(
                fieldKey = PC_CARE_SLOT_STOCK_FRIDGE_VIDEO,
                label = "Fridge stock video",
                description = "Record the vaccine stock available in the fridge for the scheduled vaccination",
            ),
        )
    }

    private fun pcCareEffectiveTaskProofs(
        detail: PcCareTaskDto?,
        removalPens: List<PcCareRemovalPenDto>,
    ): List<PcCareTaskProofDto> {
        if (!pcCareIsFeedWaterRemoval(detail) || removalPens.isEmpty()) {
            return detail?.taskProofs.orEmpty()
        }
        val penProofs = removalPens.flatMap { pen ->
            listOfNotNull(
                pen.feedProofRef
                    .takeIf { it.isNotBlank() }
                    ?.let {
                        PcCareTaskProofDto(
                            slotKey = pcCareRemovalSlotKey(pen.gatedTaskId, PC_CARE_SLOT_FEED_VIDEO),
                            proofRef = it,
                        )
                    },
                pen.waterProofRef
                    .takeIf { it.isNotBlank() }
                    ?.let {
                        PcCareTaskProofDto(
                            slotKey = pcCareRemovalSlotKey(pen.gatedTaskId, PC_CARE_SLOT_WATER_VIDEO),
                            proofRef = it,
                        )
                    },
            )
        }
        return detail?.taskProofs.orEmpty() + penProofs
    }

    private fun hydrateTaskProofPreviews(detail: PcCareTaskDto?) {
        val now = System.currentTimeMillis()
        val missing = pcCareEffectiveTaskProofs(detail, local.value.removalPens)
            .filter { it.proofRef.isNotBlank() }
            .filter { proof ->
                val cached = local.value.taskProofPreviewUrls[proof.slotKey]
                cached == null ||
                    cached.proofRef != proof.proofRef ||
                    now - cached.resolvedAtMs >= TASK_PROOF_PREVIEW_URL_TTL_MS
            }
        missing.forEach { proof ->
            val resolvedAtMs = System.currentTimeMillis()
            local.update { bits ->
                val cached = bits.taskProofPreviewUrls[proof.slotKey]
                if (
                    cached?.proofRef == proof.proofRef &&
                    cached.resolvedAtMs >= resolvedAtMs - TASK_PROOF_PREVIEW_URL_TTL_MS
                ) {
                    bits
                } else {
                    bits.copy(
                        taskProofPreviewUrls = bits.taskProofPreviewUrls + (
                            proof.slotKey to TaskProofPreviewUrl(
                                proofRef = proof.proofRef,
                                url = pcCareBackendProofUrl(proof.proofRef),
                                resolvedAtMs = resolvedAtMs,
                            )
                        ),
                    )
                }
            }
        }
    }

    private fun hydrateAnimalProofPreviews(detail: PcCareTaskDto?, animals: List<PcCareAnimalRowEntity>) {
        val now = System.currentTimeMillis()
        animals.forEach { animal ->
            decodeServerSlots(json, animal.serverSlotsJson)
                .filter { it.proofRef.isNotBlank() }
                .forEach { slot ->
                    val cacheKey = pcCareSlotProofFieldKey(animal.normalizedTag, slot.fieldKey)
                    val cached = local.value.animalProofPreviewUrls[cacheKey]
                    if (
                        cached != null &&
                        cached.proofRef == slot.proofRef &&
                        now - cached.resolvedAtMs < TASK_PROOF_PREVIEW_URL_TTL_MS
                    ) {
                        return@forEach
                    }
                    val resolvedAtMs = System.currentTimeMillis()
                    local.update { bits ->
                        val existing = bits.animalProofPreviewUrls[cacheKey]
                        if (
                            existing?.proofRef == slot.proofRef &&
                            existing.resolvedAtMs >= resolvedAtMs - TASK_PROOF_PREVIEW_URL_TTL_MS
                        ) {
                            bits
                        } else {
                            bits.copy(
                                animalProofPreviewUrls = bits.animalProofPreviewUrls + (
                                    cacheKey to TaskProofPreviewUrl(
                                        proofRef = slot.proofRef,
                                        url = pcCareBackendProofUrl(slot.proofRef),
                                        resolvedAtMs = resolvedAtMs,
                                    )
                                ),
                            )
                        }
                    }
                }
        }
    }

    private fun recoverProofPreviewUrl(slotFieldKey: String, tagKey: String?) {
        if (tagKey.isNullOrBlank()) {
            local.update { it.copy(taskProofPreviewUrls = it.taskProofPreviewUrls - slotFieldKey) }
        } else {
            local.update {
                it.copy(
                    animalProofPreviewUrls = it.animalProofPreviewUrls -
                        pcCareSlotProofFieldKey(tagKey, slotFieldKey),
                )
            }
        }
        hydrateTaskProofPreviews(latestDetail)
        hydrateAnimalProofPreviews(latestDetail, latestAnimals)
    }

    private fun trackProofPreviewAction(
        slotFieldKey: String,
        mediaKind: String,
        action: String,
        tagKey: String?,
    ) {
        val previewAction = ProofPreviewActionTrace.from(action)
        val animal = tagKey?.let { key -> latestAnimals.firstOrNull { it.normalizedTag == key } }
        if (animal != null) {
            val previewSlot = state.value.animals
                .firstOrNull { it.key == animal.normalizedTag }
                ?.slots
                ?.firstOrNull { it.fieldKey == slotFieldKey }
            analytics.track(
                AnalyticsEvents.PC_CARE_SLOT_PROOF_PREVIEW,
                pcCareAnimalSlotAnalyticsProps(
                    tagKey = animal.normalizedTag,
                    tagVerbatim = animal.tagVerbatim,
                    slotFieldKey = slotFieldKey,
                    slotKey = pcCareSlotProofFieldKey(animal.normalizedTag, slotFieldKey),
                    outcome = previewAction.outcome,
                    mediaKind = mediaKind,
                    action = previewAction.action,
                    reason = previewAction.reason,
                    proofRowId = previewSlot?.localProofRowId,
                    proofOutboxItemId = previewSlot?.proofOutboxItemId,
                    serverProofId = previewSlot?.serverProofId,
                ),
            )
            return
        }
        val detail = latestDetail
        val currentState = state.value
        val previewSlot = listOfNotNull(
            currentState.taskProofPhotoSlot,
            currentState.taskProofVideoSlot,
            currentState.taskProofSlot,
        ).firstOrNull { it.fieldKey == slotFieldKey }
            ?: currentState.taskProofSlots.firstOrNull { it.fieldKey == slotFieldKey }
        analytics.track(
            pcCareTaskProofPreviewEvent(detail),
            pcCareStockProofAnalyticsProps(
                fieldKey = slotFieldKey,
                mediaKind = mediaKind,
                status = detail?.status.orEmpty(),
                category = detail?.category.orEmpty(),
                captureMode = detail?.captureMode.orEmpty(),
                action = previewAction.action,
                outcome = previewAction.outcome,
                reason = previewAction.reason,
                source = "proof_preview",
                proofRowId = previewSlot?.localProofRowId,
                proofOutboxItemId = previewSlot?.proofOutboxItemId,
                serverProofId = previewSlot?.serverProofId,
            ),
        )
    }

    private fun buildState(
        detail: PcCareTaskDto?,
        animals: List<PcCareAnimalRowEntity>,
        proofs: List<ProofCaptureRow>,
        roster: List<String>,
        bits: LocalBits,
    ): PcCareTaskUiState {
        val locked = isLifecycleLocked(detail) || bits.submitQueued || monitorView
        val expectedSlots = pcCareEffectiveExpectedSlots(detail, bits.removalPens)
        val rosterMode = detail?.captureMode == PC_CARE_CAPTURE_MODE_ROSTER
        val taskProofMode = pcCareIsTaskProofMode(detail)
        val effectiveTaskProofs = pcCareEffectiveTaskProofs(detail, bits.removalPens)
        trackBusinessProofAcks(detail, animals, effectiveTaskProofs)
        val rosterRows = if (rosterMode) {
            pcCareBuildRosterRows(
                expectedSlots,
                roster,
                animals,
                proofs,
                bits.capturingSlotKey,
                json,
                bits.animalProofPreviewUrls,
            )
        } else {
            emptyList()
        }
        val evaluation = if (taskProofMode) {
            pcCareEvaluateTaskProofSubmit(
                expectedSlots,
                proofs,
                effectiveTaskProofs,
                bits.capturingSlotKey,
                missingCopy = if (pcCareIsFeedWaterRemoval(detail)) {
                    "Record the feed removal and water removal videos first" // mobile-contract:ignore: device-local pre-sync gate copy
                } else {
                    "Record the fridge stock photo and video first" // mobile-contract:ignore: device-local pre-sync gate copy
                },
            )
        } else {
            pcCareEvaluateSubmit(expectedSlots, animals, proofs, json)
        }
        // Per-animal drill focus: the scanned row's live chips, or a fresh all-empty card set
        // while the entry scan is still landing in Room.
        val focusAnimal = if (focusTagKey.isNotBlank()) {
            pcCareBuildAnimalUis(
                expectedSlots,
                animals.filter { it.normalizedTag == focusTagKey },
                proofs,
                bits.capturingSlotKey,
                json,
                bits.animalProofPreviewUrls,
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
            title = pcCareTaskTitle(detail),
            locationDisplay = detail?.let { d -> d.taskLabel.ifBlank { d.operationalLocationDisplay.ifBlank { d.shedLabel } } }.orEmpty(),
            parkLabel = detail?.parkLabel.orEmpty(),
            dateLabel = detail?.plannedBusinessDate.orEmpty(),
            assigneeLine = detail?.assigneeNames.orEmpty().joinToString(", "),
            isLocked = locked,
            lockNotice = when {
                detail?.status == PC_CARE_STATUS_COMPLETED -> "Checked and approved"
                detail?.status == PC_CARE_STATUS_PENDING_VERIFICATION || bits.submitQueued -> "In review"
                monitorView -> "Viewing only"
                else -> ""
            },
            reworkReason = detail?.takeIf { it.status == PC_CARE_STATUS_REWORK }?.reworkReason.orEmpty(),
            scanInput = bits.scanInput,
            scanNotice = bits.scanNotice,
            animals = pcCareBuildAnimalUis(
                expectedSlots,
                animals,
                proofs,
                bits.capturingSlotKey,
                json,
                bits.animalProofPreviewUrls,
            ),
            inventoryRequirements = detail?.inventoryRequirements.orEmpty().map {
                PcCareInventoryRequirementUi(
                    vaccineLabel = it.vaccineLabel,
                    requiredDosesLabel = if (it.requiredDoses == 1) "1 dose" else "${it.requiredDoses} doses",
                )
            },
            taskProofMode = taskProofMode,
            // The removal face renders the GENERIC slot list; the fridge face keeps its
            // dedicated photo/video pair. Never both.
            taskProofSlots = if (taskProofMode && pcCareIsFeedWaterRemoval(detail)) {
                expectedSlots.map { slot ->
                    pcCareBuildTaskProofSlot(
                        slot = slot,
                        proofs = proofs,
                        taskProofs = effectiveTaskProofs,
                        capturingSlotKey = bits.capturingSlotKey,
                        remotePreviewUrls = bits.taskProofPreviewUrls,
                        category = detail?.category.orEmpty(),
                    )
                }
            } else {
                emptyList()
            },
            taskProofSlot = if (taskProofMode && !pcCareIsFeedWaterRemoval(detail)) {
                expectedSlots.firstOrNull { it.fieldKey == PC_CARE_SLOT_STOCK_FRIDGE_PHOTO }?.let { slot ->
                    pcCareBuildTaskProofSlot(
                        slot,
                        proofs,
                        effectiveTaskProofs,
                        bits.capturingSlotKey,
                        bits.taskProofPreviewUrls,
                        detail?.category.orEmpty(),
                    )
                }
            } else {
                null
            },
            taskProofPhotoSlot = if (taskProofMode && !pcCareIsFeedWaterRemoval(detail)) {
                expectedSlots.firstOrNull { it.fieldKey == PC_CARE_SLOT_STOCK_FRIDGE_PHOTO }?.let { slot ->
                    pcCareBuildTaskProofSlot(
                        slot = slot,
                        proofs = proofs,
                        taskProofs = effectiveTaskProofs,
                        capturingSlotKey = bits.capturingSlotKey,
                        remotePreviewUrls = bits.taskProofPreviewUrls,
                        category = detail?.category.orEmpty(),
                    )
                }
            } else {
                null
            },
            taskProofVideoSlot = if (taskProofMode && !pcCareIsFeedWaterRemoval(detail)) {
                expectedSlots.firstOrNull { it.fieldKey == PC_CARE_SLOT_STOCK_FRIDGE_VIDEO }?.let { slot ->
                    pcCareBuildTaskProofSlot(
                        slot = slot,
                        proofs = proofs,
                        taskProofs = effectiveTaskProofs,
                        capturingSlotKey = bits.capturingSlotKey,
                        remotePreviewUrls = bits.taskProofPreviewUrls,
                        category = detail?.category.orEmpty(),
                    )
                }
            } else {
                null
            },
            animalCountLabel = when {
                taskProofMode -> ""
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
                taskProofMode -> evaluation.blockedReason
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
            // PC Director's verdict bar (maintainer decision 2026-09-02): offered ONLY on a
            // submitted stock task, only to the approve-capable viewer — never mid-capture.
            verdictOffered = approveView && taskProofMode &&
                detail?.status == PC_CARE_STATUS_PENDING_VERIFICATION,
            verdictInFlight = bits.verdictInFlight,
            showRejectDialog = bits.showRejectDialog,
            rejectReasonInput = bits.rejectReasonInput,
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
        internal const val PC_CARE_CAPTURE_MODE_TASK_PROOF = "task_proof"
        internal const val PC_CARE_CATEGORY_INVENTORY_VACCINE = "inventory_vaccine"
        internal const val PC_CARE_CATEGORY_FEED_WATER_REMOVAL = "feed_water_removal"
        internal const val PC_CARE_SLOT_FEED_VIDEO = "feed_video"
        internal const val PC_CARE_SLOT_WATER_VIDEO = "water_video"
        internal const val PC_CARE_SLOT_STOCK_FRIDGE_PHOTO = "stock_fridge_photo"
        internal const val PC_CARE_SLOT_STOCK_FRIDGE_VIDEO = "stock_fridge_video"

        const val ARG_TASK_ID = "task_id"
        const val ARG_CATEGORY = "category"
        const val ARG_TITLE = "title"
        const val ARG_TAG_KEY = "tag_key"
        const val ARG_TAG_VERBATIM = "tag_verbatim"
        const val ARG_MONITOR = "monitor"
        const val ARG_APPROVE = "approve"

        /** Backend stock-verdict contract tokens (domain.StockVerdict*). */
        internal const val STOCK_VERDICT_APPROVE = "approve"
        internal const val STOCK_VERDICT_REJECT = "reject"

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

private data class PcCareCapturedTaskProof(
    val localUri: String,
    val mimeType: String,
    val startedAtMs: Long,
    val endedAtMs: Long,
    val captureSource: String,
)

// ---------------------------------------------------------------------------
// Pure derivation helpers (unit-tested directly — no ViewModel needed).
// ---------------------------------------------------------------------------

internal fun isSuspiciousShortNumericRfid(tagVerbatim: String): Boolean {
    val digitsOnly = tagVerbatim.filter { it.isDigit() }
    return digitsOnly.length == tagVerbatim.trim().length && digitsOnly.length in 1 until MIN_NUMERIC_RFID_LENGTH
}

private const val MIN_NUMERIC_RFID_LENGTH = 12

/** The ONE builder of a slot's local proof field key: `<normalizedTag>:<slotFieldKey>`. */
internal fun pcCareSlotProofFieldKey(normalizedTag: String, slotFieldKey: String): String =
    "$normalizedTag:$slotFieldKey"

internal fun pcCareSlotHintLabel(minDurationHintSeconds: Int): String =
    if (minDurationHintSeconds > 0) "Record at least $minDurationHintSeconds seconds" else ""

internal fun pcCareProofPolicy(captureSource: String): ProofPolicy =
    ProofPolicy.Default.copy(
        proofMode = "per_animal_slot_video",
        featureSurface = "pc_care",
        featureCategory = "pc_care",
        subjectScope = ProofSubject.OTHER.wireValue,
        expectedSubjects = listOf(ProofSubject.OTHER.wireValue),
        captureSource = captureSource,
        // One active clip per slot field is the real local bound. The backend owns any
        // feature-specific total limits; Android must not pool the whole task under a subject cap.
        maximumCountPerField = 1,
    )

private fun pcCareProofPolicy(captureSource: String, category: String): ProofPolicy =
    pcCareProofPolicy(captureSource).copy(featureCategory = category.trim().ifBlank { "pc_care" })

private fun String.isPcCareRepeatableTaskProofCategory(): Boolean =
    when (trim()) {
        PcCareTaskViewModel.PC_CARE_CATEGORY_FEED_WATER_REMOVAL,
        PcCareTaskViewModel.PC_CARE_CATEGORY_INVENTORY_VACCINE,
        -> true
        else -> false
    }

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
    remotePreviewUrls: Map<String, TaskProofPreviewUrl> = emptyMap(),
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
    val serverSlot = serverSlots.firstOrNull { it.fieldKey == slot.fieldKey && it.proofRef.isNotBlank() }
    // MERGE RULE: this phone's own capture wins while it is still local work. Once the blob has
    // uploaded, final green waits for the PC Care slot-register business write to be visible in
    // the server slot. That keeps "Video sent" from meaning only "GCS blob uploaded".
    val localRow = animalProofs
        .filter { it.fieldKey == slotKey }
        .maxByOrNull { it.capturedAtMs }
    if (serverSlot != null && localRow?.processingStatus != ProofProcessingStatus.RECORD_AGAIN) {
        val remotePreview = remotePreviewUrls[slotKey]?.takeIf { it.proofRef == serverSlot.proofRef }
        val previewPath = remotePreview?.url.orEmpty().ifBlank { localRow?.previewUri().orEmpty() }
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            description = slot.description,
            state = if (localRow?.serverProofId == serverSlot.proofRef) PcCareSlotState.SYNCED else PcCareSlotState.PEER,
            statusLabel = if (localRow?.serverProofId == serverSlot.proofRef) {
                "Video sent"
            } else if (serverSlot.capturedByName.isNotBlank()) {
                "Captured by ${serverSlot.capturedByName}"
            } else {
                "Captured by a teammate"
            },
            hintLabel = hint,
            canRecord = false,
            previewPath = previewPath,
            previewIdentity = serverSlot.proofRef,
            previewKind = localRow?.mimeType?.let(::pcCarePreviewKind) ?: PcCareProofPreviewKind.VIDEO,
            localProofRowId = localRow?.id,
            proofOutboxItemId = localRow?.outboxItemId,
            serverProofId = serverSlot.proofRef,
        )
    }
    if (localRow != null) {
        val previewPath = localRow.processedUri ?: localRow.localUri
        val previewKind = pcCarePreviewKind(localRow.mimeType)
        return when (localRow.processingStatus) {
            ProofProcessingStatus.UPLOADED -> {
                if (serverSlot != null && serverSlot.proofRef == localRow.serverProofId) {
                    PcCareSlotChipUi(
                        fieldKey = slot.fieldKey,
                        label = slot.label,
                        description = slot.description,
                        state = PcCareSlotState.SYNCED,
                        statusLabel = "Video sent",
                        hintLabel = hint,
                        canRecord = false,
                        previewPath = previewPath,
                        previewIdentity = localRow.serverProofId ?: localRow.id,
                        previewKind = previewKind,
                        localProofRowId = localRow.id,
                        proofOutboxItemId = localRow.outboxItemId,
                        serverProofId = serverSlot.proofRef,
                    )
                } else {
                    PcCareSlotChipUi(
                        fieldKey = slot.fieldKey,
                        label = slot.label,
                        description = slot.description,
                        state = PcCareSlotState.WORKING,
                        statusLabel = "Video uploaded, saving to task...",
                        hintLabel = hint,
                        canRecord = false,
                        previewPath = previewPath,
                        previewIdentity = localRow.serverProofId ?: localRow.id,
                        previewKind = previewKind,
                        localProofRowId = localRow.id,
                        proofOutboxItemId = localRow.outboxItemId,
                        serverProofId = localRow.serverProofId,
                    )
                }
            }
            ProofProcessingStatus.RECORD_AGAIN -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
                state = PcCareSlotState.FAILED,
                statusLabel = "Retry video",
                hintLabel = hint,
                canRecord = true,
                previewPath = previewPath,
                previewIdentity = localRow.id,
                previewKind = previewKind,
                localProofRowId = localRow.id,
                proofOutboxItemId = localRow.outboxItemId,
                serverProofId = localRow.serverProofId,
            )
            else -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
                state = if (localRow.syncStatus == CaptureSyncStatus.FAILED) PcCareSlotState.FAILED else PcCareSlotState.WORKING,
                statusLabel = localRow.processingStatus.operatorLabel,
                hintLabel = hint,
                canRecord = localRow.syncStatus == CaptureSyncStatus.FAILED,
                previewPath = previewPath,
                previewIdentity = localRow.id,
                previewKind = previewKind,
                localProofRowId = localRow.id,
                proofOutboxItemId = localRow.outboxItemId,
                serverProofId = localRow.serverProofId,
            )
        }
    }
    if (serverSlot != null) {
        val remotePreview = remotePreviewUrls[slotKey]?.takeIf { it.proofRef == serverSlot.proofRef }
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
            previewPath = remotePreview?.url.orEmpty(),
            previewIdentity = serverSlot.proofRef,
            previewKind = PcCareProofPreviewKind.VIDEO,
            serverProofId = serverSlot.proofRef,
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

internal fun pcCareBuildTaskProofSlot(
    slot: PcCareSlotDto,
    proofs: List<ProofCaptureRow>,
    taskProofs: List<PcCareTaskProofDto>,
    capturingSlotKey: String?,
    remotePreviewUrls: Map<String, TaskProofPreviewUrl> = emptyMap(),
    category: String = PcCareTaskViewModel.PC_CARE_CATEGORY_INVENTORY_VACCINE,
): PcCareSlotChipUi {
    val hint = pcCareSlotHintLabel(slot.minDurationHintSeconds)
    val serverProof = taskProofs.firstOrNull { it.slotKey == slot.fieldKey && it.proofRef.isNotBlank() }
    val expectedKind = pcCareTaskProofExpectedPreviewKind(slot.fieldKey)
    if (capturingSlotKey == slot.fieldKey) {
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
    val localRow = proofs
        .filter { it.fieldKey == slot.fieldKey }
        .maxByOrNull { it.capturedAtMs }
    val remotePreview = remotePreviewUrls[slot.fieldKey]
    val serverPreviewUrl = remotePreview?.url.orEmpty()
    val localInProgressPreview = localRow?.takeIf {
        serverProof?.proofRef != it.serverProofId &&
            it.syncStatus != CaptureSyncStatus.FAILED &&
            it.processingStatus != ProofProcessingStatus.UPLOADED &&
            it.processingStatus != ProofProcessingStatus.RECORD_AGAIN &&
            !it.previewUri().isNullOrBlank()
    }
    if (localInProgressPreview != null) {
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            description = slot.description,
            state = PcCareSlotState.WORKING,
            statusLabel = localInProgressPreview.processingStatus.operatorLabel,
            hintLabel = hint,
            canRecord = false,
            previewPath = localInProgressPreview.previewUri().orEmpty(),
            previewIdentity = localInProgressPreview.id,
            previewKind = pcCarePreviewKind(localInProgressPreview.mimeType),
            localProofRowId = localInProgressPreview.id,
            proofOutboxItemId = localInProgressPreview.outboxItemId,
            serverProofId = localInProgressPreview.serverProofId,
        )
    }
    if (
        localRow?.processingStatus == ProofProcessingStatus.UPLOADED &&
        localRow.serverProofId?.isNotBlank() == true &&
        serverProof?.proofRef != localRow.serverProofId
    ) {
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            description = slot.description,
            state = PcCareSlotState.WORKING,
            statusLabel = "Proof uploaded, saving to task...",
            hintLabel = hint,
            canRecord = false,
            previewPath = localRow.previewUri().orEmpty(),
            previewIdentity = localRow.serverProofId ?: localRow.id,
            previewKind = pcCarePreviewKind(localRow.mimeType),
            localProofRowId = localRow.id,
            proofOutboxItemId = localRow.outboxItemId,
            serverProofId = localRow.serverProofId,
        )
    }
    if (
        localRow?.processingStatus == ProofProcessingStatus.RECORD_AGAIN ||
        (localRow?.syncStatus == CaptureSyncStatus.FAILED && !localRow.outboxItemId.isNullOrBlank())
    ) {
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            description = slot.description,
            state = PcCareSlotState.FAILED,
            statusLabel = "Retry proof",
            hintLabel = hint,
            canRecord = true,
            previewPath = localRow.previewUri().orEmpty(),
            previewIdentity = localRow.id,
            previewKind = pcCarePreviewKind(localRow.mimeType),
            localProofRowId = localRow.id,
            proofOutboxItemId = localRow.outboxItemId,
            serverProofId = localRow.serverProofId,
        )
    }
    if (serverProof != null) {
        val byline = serverProof.capturedByName
            .takeIf { it.isNotBlank() }
            ?.let { "Captured by $it" }
            ?: "Proof sent"
        val previewRow = proofs
            .filter {
                it.fieldKey == slot.fieldKey &&
                    it.syncStatus == CaptureSyncStatus.SYNCED &&
                    !it.serverProofId.isNullOrBlank()
            }
            .maxByOrNull { it.capturedAtMs }
        return PcCareSlotChipUi(
            fieldKey = slot.fieldKey,
            label = slot.label,
            description = slot.description,
            state = PcCareSlotState.SYNCED,
            statusLabel = byline,
            hintLabel = hint,
            canRecord = category.isPcCareRepeatableTaskProofCategory(),
            previewPath = serverPreviewUrl.ifBlank { previewRow?.previewUri().orEmpty() },
            previewIdentity = serverProof.proofRef,
            previewKind = previewRow?.mimeType?.let(::pcCarePreviewKind) ?: expectedKind,
            localProofRowId = previewRow?.id,
            proofOutboxItemId = previewRow?.outboxItemId,
            serverProofId = serverProof.proofRef,
        )
    }
    if (localRow != null) {
        return when (localRow.processingStatus) {
            ProofProcessingStatus.UPLOADED -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
                state = PcCareSlotState.WORKING,
                statusLabel = "Proof uploaded, saving to task...",
                hintLabel = hint,
                canRecord = false,
                previewPath = remotePreview
                    ?.takeIf { it.proofRef == localRow.serverProofId }
                    ?.url
                    .orEmpty()
                    .ifBlank { localRow.previewUri().orEmpty() },
                previewIdentity = localRow.serverProofId ?: localRow.id,
                previewKind = pcCarePreviewKind(localRow.mimeType),
                localProofRowId = localRow.id,
                proofOutboxItemId = localRow.outboxItemId,
                serverProofId = localRow.serverProofId,
            )
            ProofProcessingStatus.RECORD_AGAIN -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
                state = PcCareSlotState.FAILED,
                statusLabel = "Retry proof",
                hintLabel = hint,
                canRecord = true,
                previewPath = localRow.previewUri().orEmpty(),
                previewIdentity = localRow.id,
                previewKind = pcCarePreviewKind(localRow.mimeType),
                localProofRowId = localRow.id,
                proofOutboxItemId = localRow.outboxItemId,
                serverProofId = localRow.serverProofId,
            )
            else -> PcCareSlotChipUi(
                fieldKey = slot.fieldKey,
                label = slot.label,
                description = slot.description,
                state = if (localRow.syncStatus == CaptureSyncStatus.FAILED) PcCareSlotState.FAILED else PcCareSlotState.WORKING,
                statusLabel = localRow.processingStatus.operatorLabel,
                hintLabel = hint,
                canRecord = localRow.syncStatus == CaptureSyncStatus.FAILED,
                previewPath = localRow.previewUri().orEmpty(),
                previewIdentity = localRow.id,
                previewKind = pcCarePreviewKind(localRow.mimeType),
                localProofRowId = localRow.id,
                proofOutboxItemId = localRow.outboxItemId,
                serverProofId = localRow.serverProofId,
            )
        }
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

private fun pcCarePreviewKind(mimeType: String): PcCareProofPreviewKind =
    if (mimeType.startsWith("image/", ignoreCase = true)) PcCareProofPreviewKind.PHOTO else PcCareProofPreviewKind.VIDEO

private fun pcCareTaskProofExpectedPreviewKind(fieldKey: String): PcCareProofPreviewKind =
    if (fieldKey == PcCareTaskViewModel.PC_CARE_SLOT_STOCK_FRIDGE_PHOTO) {
        PcCareProofPreviewKind.PHOTO
    } else {
        PcCareProofPreviewKind.VIDEO
    }

private fun pcCareAbsoluteProofUrl(raw: String): String {
    val trimmed = raw.trim()
    return when {
        trimmed.startsWith("http://") || trimmed.startsWith("https://") -> trimmed
        trimmed.startsWith("/") -> BuildConfig.API_BASE_URL.trimEnd('/') + trimmed
        else -> trimmed
    }
}

private fun pcCareBackendProofUrl(proofRef: String): String =
    pcCareAbsoluteProofUrl("/app/proofs/${proofRef.trim()}/download")

internal data class TaskProofPreviewUrl(
    val proofRef: String,
    val url: String,
    val resolvedAtMs: Long,
)

private const val TASK_PROOF_PREVIEW_URL_TTL_MS = 10 * 60 * 1000L

internal fun pcCareBuildAnimalUis(
    expectedSlots: List<PcCareSlotDto>,
    animals: List<PcCareAnimalRowEntity>,
    proofs: List<ProofCaptureRow>,
    capturingSlotKey: String?,
    json: Json,
    remotePreviewUrls: Map<String, TaskProofPreviewUrl> = emptyMap(),
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
                pcCareSlotChip(slot, animal.normalizedTag, animalProofs, serverSlots, capturingSlotKey, remotePreviewUrls)
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
    remotePreviewUrls: Map<String, TaskProofPreviewUrl> = emptyMap(),
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
            pcCareSlotChip(slot, tagKey, animalProofs, serverSlots, capturingSlotKey, remotePreviewUrls)
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
            chips.any { it.state == PcCareSlotState.FAILED } -> "Retry upload"
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
 * every animal holds either this phone's durable non-failed local proof row or a
 * server-attributed peer proof ref. A queued local proof is allowed here because the final submit
 * drains in the same outbox group and waits on the proof dependency before it can reach the server.
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
                .filter {
                    it.fieldKey == slotKey &&
                        it.syncStatus != CaptureSyncStatus.FAILED &&
                        it.processingStatus != ProofProcessingStatus.RECORD_AGAIN
                }
                .maxByOrNull { it.capturedAtMs }
            val localQueuedOrSynced = localRow?.outboxItemId?.isNotBlank() == true ||
                (localRow?.syncStatus == CaptureSyncStatus.SYNCED && !localRow.serverProofId.isNullOrBlank())
            val peerCaptured = serverSlots.any { it.fieldKey == slot.fieldKey && it.proofRef.isNotBlank() }
            when {
                localQueuedOrSynced || peerCaptured -> Unit
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

internal fun pcCareEvaluateTaskProofSubmit(
    expectedSlots: List<PcCareSlotDto>,
    proofs: List<ProofCaptureRow>,
    taskProofs: List<PcCareTaskProofDto>,
    capturingSlotKey: String?,
    /** Farm copy naming the missing evidence — the fridge face's wording by default; the feed &
     *  water removal face passes its own (two videos, no photo). */
    missingCopy: String = "Record the fridge stock photo and video first", // mobile-contract:ignore: device-local pre-sync gate copy
): PcCareSubmitEvaluation {
    if (expectedSlots.isEmpty()) {
        return PcCareSubmitEvaluation(ready = false, blockedReason = missingCopy)
    }
    expectedSlots.forEach { slot ->
        if (taskProofs.any { it.slotKey == slot.fieldKey && it.proofRef.isNotBlank() }) return@forEach
        val localRow = proofs
            .filter {
                it.fieldKey == slot.fieldKey &&
                    it.syncStatus != CaptureSyncStatus.FAILED &&
                    it.processingStatus != ProofProcessingStatus.RECORD_AGAIN
            }
            .maxByOrNull { it.capturedAtMs }
        if (localRow?.syncStatus == CaptureSyncStatus.SYNCED && !localRow.serverProofId.isNullOrBlank()) return@forEach
        if (localRow?.outboxItemId?.isNotBlank() == true) return@forEach
        if (capturingSlotKey == slot.fieldKey) return PcCareSubmitEvaluation(ready = false, blockedReason = "Proof is still recording") // mobile-contract:ignore: device-local pre-sync gate copy
        return PcCareSubmitEvaluation(ready = false, blockedReason = missingCopy)
    }
    return PcCareSubmitEvaluation(ready = true)
}
