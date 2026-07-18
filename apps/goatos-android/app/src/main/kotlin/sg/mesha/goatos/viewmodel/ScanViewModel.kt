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
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.ROSTER_SCAN_FIELD_KEY
import sg.mesha.goatos.core.data.capture.RfidScanAttemptOutcome
import sg.mesha.goatos.core.data.capture.RfidScanTagRole
import sg.mesha.goatos.core.data.capture.ScanAttemptRepository
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanFeedTone
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import sg.mesha.goatos.capture.ProofCaptureSource
import javax.inject.Inject

/**
 * Scan (tap-to-scan) state holder — the offline-first pattern (docs/decisions/android-offline-first.md).
 * Room is the UI's single source of truth: [state] is fed by [ExecutionRepository.observeScanRoster],
 * a cache-first Flow that emits instantly from Room and re-emits when a background
 * [ExecutionRepository.refreshScanRoster] upserts new data. [refresh] never writes [state] directly —
 * it only drives the network call and the transient [ScanUiState.isRefreshing]/[ScanUiState.isOffline]
 * flags; the DTO -> UiState mapping is unchanged from the network-only version.
 *
 * Folds each hardware RFID read (keyboard-wedge) into the draft overlay: match the tag against
 * the roster, mark a due animal done, push a live feed row. The backend revalidates on submit —
 * this is a draft overlay, not truth.
 *
 * Capture is gated by [setCaptureActive]: the nav host enables it only while the Scan screen
 * is composed and disables it on navigate-away, so tag reads never land off-screen.
 */
@HiltViewModel
class ScanViewModel @Inject constructor(
    private val repo: ExecutionRepository,
    private val reader: RfidReaderPort,
    private val scanCaptureRepository: ScanCaptureRepository,
    private val scanAttemptRepository: ScanAttemptRepository,
    private val proofCaptureRepository: ProofCaptureRepository,
    private val proofCaptureSource: ProofCaptureSource,
    private val bootstrapRepository: BootstrapRepository,
    private val analytics: AnalyticsPort,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")
    private val taskId: String? = savedStateHandle.get<String>("taskId")
    private var nextCursor: String? = null
    private var readerRefreshJob: Job? = null

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<ScanRosterResponseDto>> =
        (if (shedId != null) {
            repo.observeScanRoster(shedId, taskId, limit = SCAN_PAGE_SIZE)
        } else {
            flowOf(Resource(data = null))
        }).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

    private val observedProofs: StateFlow<List<ProofCaptureRow>> =
        (taskId?.let { proofCaptureRepository.observeProofs(it) } ?: flowOf(emptyList()))
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    private val _operatorAllowed = MutableStateFlow<Boolean?>(null)
    private var currentPrincipalId: String? = null
    private var proofCaptureInFlight = false

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)

    // Draft state for local interactions
    private val _selectedFilter = MutableStateFlow<ScanStatus?>(null)
    private val _rosterExpanded = MutableStateFlow(false)
    private val _selectedVaccineGroupId = MutableStateFlow<String?>(null)

    // Draft scan overlay (survives Room re-emission): obligationIds locally marked DONE (unsynced)
    // by a hardware tag read or manual ring tap, plus the live feed rows. These are overlaid onto
    // the observed roster in the combine so an in-progress scan is not lost when Room re-emits.
    private val _localDone = MutableStateFlow<Set<String>>(emptySet())
    private val _manualDone = MutableStateFlow<Set<String>>(emptySet())
    private val _feed = MutableStateFlow<List<ScanFeedEntry>>(emptyList())

    // Combines observed resource with transient flags + draft overlay; lifecycle-aware.
    // >5 flows > Kotlin's typed combine limit (5), so use the vararg Array<*> form and cast.
    @Suppress("UNCHECKED_CAST")
    val state: StateFlow<ScanUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline,
        _isLoadingMore,
        _selectedFilter,
        _rosterExpanded,
        _selectedVaccineGroupId,
        _localDone,
        _feed,
        reader.status,
        reader.readerName,
        observedProofs,
        _operatorAllowed,
    ) { values: Array<Any?> ->
        val resource = values[0] as Resource<ScanRosterResponseDto>
        val isRefreshing = values[1] as Boolean
        val isOffline = values[2] as Boolean
        val isLoadingMore = values[3] as Boolean
        val selectedFilter = values[4] as ScanStatus?
        val rosterExpanded = values[5] as Boolean
        val selectedGroupId = values[6] as String?
        val localDone = values[7] as Set<String>
        val feed = values[8] as List<ScanFeedEntry>
        val readerStatus = values[9] as RfidReaderStatus
        val readerName = values[10] as String?
        val proofs = values[11] as List<ProofCaptureRow>
        val operatorAllowed = values[12] as Boolean?
        val dto = resource.data
        nextCursor = dto?.nextCursor  // Update pagination cursor for loadMore()
        val base = dto?.let { applyResource(it, localDone, proofs, operatorAllowed == true) } ?: emptyScanState()
        base.copy(
            feed = feed,
            isRefreshing = isRefreshing,
            isLoadingMore = isLoadingMore,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
            selectedFilter = selectedFilter,
            rosterExpanded = rosterExpanded,
            vaccineGroups = base.vaccineGroups.map { group ->
                group.copy(active = group.id == selectedGroupId)
            },
            readerConnection = readerStatus.toScanReaderConnection(readerName),
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        emptyScanState()
    )

    init {
        loadRosterAndRefresh()
        viewModelScope.launch {
            val profile = runCatching { bootstrapRepository.operatorProfile() }.getOrNull()
            currentPrincipalId = profile?.operatorId?.takeIf { it.isNotBlank() }
            _operatorAllowed.value = profile?.primaryRoleHint == OPERATOR_ROLE
        }
        // HOT device stream (RFID reader) — NOT converted; always collected for keyboard-wedge capture
        viewModelScope.launch {
            reader.reads.collect { onTagRead(it.tag) }
        }
    }

    /** Enable/disable keyboard-wedge capture with the Scan screen's composition lifecycle. */
    fun setCaptureActive(active: Boolean) {
        reader.setCaptureEnabled(active)
        if (active) {
            reader.refreshStatus()
            if (readerRefreshJob?.isActive == true) return
            readerRefreshJob = viewModelScope.launch {
                while (true) {
                    reader.refreshStatus()
                    delay(READER_REFRESH_MS)
                }
            }
        } else {
            readerRefreshJob?.cancel()
            readerRefreshJob = null
        }
    }

    override fun onCleared() {
        readerRefreshJob?.cancel()
        reader.setCaptureEnabled(false)
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observedResource]
     *  StateFlow re-emits and updates [state]); on failure it only flips [_isOffline] —
     *  cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        val id = shedId ?: return@launch
        _isRefreshing.value = true
        val result = repo.refreshCompleteScanRoster(id, taskId, limit = SCAN_PAGE_SIZE)
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
    }

    fun loadMore() = viewModelScope.launch {
        val id = shedId ?: return@launch
        val cursor = nextCursor ?: return@launch
        if (_isLoadingMore.value) return@launch
        _isLoadingMore.value = true
        val result = repo.appendScanRoster(id, taskId, cursor, limit = SCAN_PAGE_SIZE)
        _isLoadingMore.value = false
        _isOffline.value = result.isFailure
    }

    private fun loadRosterAndRefresh() {
        val id = shedId ?: return
        AnalyticsFunnels.trackScanStarted(analytics, id)
        refresh()
    }

    fun onEvent(event: ScanEvent) {
        when (event) {
            is ScanEvent.SelectGroup ->
                _selectedVaccineGroupId.value = event.groupId
            is ScanEvent.OpenTile ->
                _selectedFilter.value = if (_selectedFilter.value == event.status) null else event.status
            ScanEvent.OpenList ->
                _rosterExpanded.value = !_rosterExpanded.value
            ScanEvent.Tap -> onManualTap()
            is ScanEvent.CaptureProof -> requestGoatProof(event.goatId)
            is ScanEvent.RetryProof -> retryGoatProof(event.goatId)
            ScanEvent.LoadMore -> loadMore()
            ScanEvent.Submit,
            ScanEvent.Back,
            ScanEvent.ReconnectReader -> Unit // navigation — handled by the host.
        }
    }

    private fun RfidReaderStatus.toScanReaderConnection(readerName: String?): ScanReaderConnection =
        ScanReaderConnection(
            readerName = readerName ?: "RFID reader",
            statusLabel = when (this) {
                RfidReaderStatus.READY -> "Reader connected"
                RfidReaderStatus.PAIRED_NOT_READY -> "Reader disconnected"
                RfidReaderStatus.NOT_PAIRED -> "Reader not paired"
                RfidReaderStatus.PERMISSION_NEEDED -> "Bluetooth permission needed"
                RfidReaderStatus.BLUETOOTH_OFF -> "Bluetooth off"
            },
            connected = this == RfidReaderStatus.READY,
            actionLabel = "Reconnect",
        )

    /** Manual ring tap: advances the next REAL pending roster row (from the current computed
     *  state) to DONE in the draft overlay only. It is not an RFID capture. */
    private fun onManualTap() {
        if (!canAcceptScanInput()) return
        val row = state.value.roster.firstOrNull { it.status == ScanStatus.PENDING } ?: return
        markRowDone(row)
        _manualDone.update { it + row.obligationId }
        recordScanAttempt(
            tag = row.primaryTag,
            row = row,
            outcome = RfidScanAttemptOutcome.ACCEPTED,
            tagRole = RfidScanTagRole.PRIMARY,
            reason = "operator_row_tap",
        )
        recordRosterScan(row, row.primaryTag)
    }

    /** Hardware tag read (keyboard-wedge): match the tag against the current roster and fold it
     *  into the draft overlay — a PENDING match is marked DONE, a SKIPPED/unknown match only
     *  pushes an informational feed row. */
    private fun onTagRead(tag: String) {
        if (!canAcceptScanInput()) return
        val target = normalize(tag)
        if (target.isEmpty()) return
        val row = state.value.roster.firstOrNull { it.matchesTag(target) }
        if (row == null) {
            recordScanAttempt(
                tag = tag,
                row = null,
                outcome = RfidScanAttemptOutcome.UNKNOWN,
                tagRole = RfidScanTagRole.UNKNOWN,
                reason = "unknown_tag",
            )
            _feed.update { prependFeed(ScanFeedEntry(tag, null, "unknown tag · not in this shed", ScanStatus.SKIPPED), it) }
            return
        }
        val tagRole = row.tagRoleFor(target)
        when (row.status) {
            ScanStatus.PENDING -> {
                markRowDone(row)
                _manualDone.update { it - row.obligationId }
                recordScanAttempt(
                    tag = tag,
                    row = row,
                    outcome = RfidScanAttemptOutcome.ACCEPTED,
                    tagRole = tagRole,
                    reason = null,
                )
                recordRosterScan(row = row, tag = tag)
            }
            ScanStatus.DONE -> {
                if (row.obligationId in _manualDone.value) {
                    _manualDone.update { it - row.obligationId }
                    recordScanAttempt(
                        tag = tag,
                        row = row,
                        outcome = RfidScanAttemptOutcome.ACCEPTED,
                        tagRole = tagRole,
                        reason = "manual_done_replaced_by_reader_scan",
                    )
                    recordRosterScan(row = row, tag = tag)
                } else {
                    recordScanAttempt(
                        tag = tag,
                        row = row,
                        outcome = RfidScanAttemptOutcome.DUPLICATE,
                        tagRole = tagRole,
                        reason = "goat_already_scanned",
                    )
                    _feed.update {
                        prependFeed(
                            ScanFeedEntry(
                                row.primaryTag,
                                row.secondaryTag,
                                "already scanned · ${row.vaccineLabel}",
                                ScanStatus.DONE,
                                ScanFeedTone.DUPLICATE,
                            ),
                            it,
                        )
                    }
                }
            }
            ScanStatus.SKIPPED -> {
                recordScanAttempt(
                    tag = tag,
                    row = row,
                    outcome = RfidScanAttemptOutcome.NOT_DUE,
                    tagRole = tagRole,
                    reason = "not_due",
                )
                _feed.update {
                    prependFeed(
                        ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED),
                        it,
                    )
                }
            }
        }
    }

    private fun canAcceptScanInput(): Boolean {
        val current = state.value
        return _operatorAllowed.value == true && current.roster.isNotEmpty() && !current.hasMore
    }

    /** Shared by a real tag-match ([onTagRead]) and a manual ring tap ([onManualTap]): records
     * [row]'s obligation as locally DONE (unsynced) in the draft overlay and pushes a feed row.
     * The combine re-derives the roster + counts from this set on the next emission. */
    private fun markRowDone(row: RosterRow) {
        if (row.obligationId.isBlank()) return
        _localDone.update { it + row.obligationId }
        _feed.update {
            prependFeed(ScanFeedEntry(row.primaryTag, row.secondaryTag, row.vaccineLabel, ScanStatus.DONE), it)
        }
    }

    private fun recordRosterScan(row: RosterRow, tag: String) {
        val selectedTaskId = taskId ?: return
        val capturedTag = tag.ifBlank { row.primaryTag }
        if (normalize(capturedTag).isEmpty()) return
        viewModelScope.launch {
            scanCaptureRepository.recordScan(
                taskId = selectedTaskId,
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = capturedTag,
                goatId = row.goatId,
                obligationId = row.obligationId,
            )
        }
    }

    private fun recordScanAttempt(
        tag: String,
        row: RosterRow?,
        outcome: RfidScanAttemptOutcome,
        tagRole: RfidScanTagRole,
        reason: String?,
    ) {
        val selectedTaskId = taskId ?: return
        val capturedTag = tag.ifBlank { row?.primaryTag.orEmpty() }
        if (normalize(capturedTag).isEmpty()) return
        viewModelScope.launch {
            scanAttemptRepository.recordAttempt(
                taskId = selectedTaskId,
                fieldKey = ROSTER_SCAN_FIELD_KEY,
                tag = capturedTag,
                goatId = row?.goatId,
                obligationId = row?.obligationId,
                outcome = outcome,
                tagRole = tagRole,
                reason = reason,
            )
        }
    }

    private fun applyResource(
        dto: ScanRosterResponseDto,
        localDone: Set<String>,
        proofs: List<ProofCaptureRow>,
        operatorAllowed: Boolean,
    ): ScanUiState {
        val rosterRows = dto.rows.map { dtoRow ->
            // Overlay local (unsynced) DONE edits so an in-progress scan survives Room re-emission.
            val locallyDone = dtoRow.obligationId.isNotBlank() && dtoRow.obligationId in localDone
            val status = if (locallyDone) ScanStatus.DONE else statusOf(dtoRow.status)
            val goatProofs = proofs.filter { it.proofSubject == ProofSubject.GOAT && it.subjectId == dtoRow.goatId }
            RosterRow(
                primaryTag = dtoRow.primaryTag,
                secondaryTag = dtoRow.secondaryTag,
                vaccineLabel = dtoRow.vaccineLabel,
                status = status,
                unsynced = locallyDone,
                goatId = dtoRow.goatId,
                obligationId = dtoRow.obligationId,
                proofClipCount = goatProofs.count { it.syncStatus != CaptureSyncStatus.FAILED },
                proofUploadStatus = proofStatus(goatProofs),
            )
        }
        val done = rosterRows.count { it.status == ScanStatus.DONE }
        val skipped = rosterRows.count { it.status == ScanStatus.SKIPPED }
        val pending = (rosterRows.size - done - skipped).coerceAtLeast(0)
        return emptyScanState().copy(
            roster = rosterRows,
            ringTotal = rosterRows.size,
            ringDone = done,
            doneCount = done,
            pendingCount = pending,
            skippedCount = skipped,
            canSubmit = pending == 0 &&
                dto.nextCursor == null &&
                rosterRows.filter { it.status == ScanStatus.DONE }.all {
                    it.proofUploadStatus == ProofUploadStatus.SYNCED
                },
            scanEnabled = operatorAllowed && rosterRows.isNotEmpty() && dto.nextCursor == null,
            hasMore = dto.nextCursor != null,
        )
    }

    private fun statusOf(raw: String): ScanStatus {
        val s = raw.lowercase()
        return when {
            s.contains("done") || s.contains("complete") -> ScanStatus.DONE
            s.contains("skip") || s.contains("not_due") || s.contains("notdue") || s.contains("missed") -> ScanStatus.SKIPPED
            else -> ScanStatus.PENDING
        }
    }

    private fun proofStatus(proofs: List<ProofCaptureRow>): ProofUploadStatus = when {
        proofs.any { it.syncStatus == CaptureSyncStatus.FAILED } -> ProofUploadStatus.FAILED
        proofs.any { it.syncStatus == CaptureSyncStatus.PENDING || it.syncStatus == CaptureSyncStatus.IN_FLIGHT } ->
            ProofUploadStatus.UPLOADING
        proofs.any { it.syncStatus == CaptureSyncStatus.SYNCED && !it.serverProofId.isNullOrBlank() } ->
            ProofUploadStatus.SYNCED
        else -> ProofUploadStatus.MISSING
    }

    private fun requestGoatProof(goatId: String) {
        val selectedTaskId = taskId ?: return
        if (_operatorAllowed.value != true || goatId.isBlank() || proofCaptureInFlight) return
        val row = state.value.roster.firstOrNull { it.goatId == goatId && it.status == ScanStatus.DONE } ?: return
        proofCaptureInFlight = true
        viewModelScope.launch {
            try {
                val captured = proofCaptureSource.captureVideo() ?: return@launch
                proofCaptureRepository.capture(
                    taskId = selectedTaskId,
                    fieldKey = GOAT_PROOF_FIELD_KEY,
                    subject = ProofSubject.GOAT,
                    subjectId = row.goatId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = null,
                    scopeType = "task",
                    scopeId = selectedTaskId,
                    capturedStartMs = captured.startedAtMs,
                    capturedEndMs = captured.endedAtMs,
                    capturedByPrincipalId = currentPrincipalId,
                )
            } finally {
                proofCaptureInFlight = false
            }
        }
    }

    private fun retryGoatProof(goatId: String) {
        val selectedTaskId = taskId ?: return
        if (_operatorAllowed.value != true || goatId.isBlank()) return
        viewModelScope.launch {
            observedProofs.value
                .filter { it.subjectId == goatId && it.syncStatus == CaptureSyncStatus.FAILED }
                .forEach { proofCaptureRepository.retryUpload(selectedTaskId, it.id) }
        }
    }

private fun normalize(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()

private fun RosterRow.matchesTag(normalizedTag: String): Boolean =
    normalize(primaryTag) == normalizedTag || secondaryTag?.let { normalize(it) == normalizedTag } == true

private fun RosterRow.tagRoleFor(normalizedTag: String): RfidScanTagRole = when {
    normalize(primaryTag) == normalizedTag -> RfidScanTagRole.PRIMARY
    secondaryTag?.let { normalize(it) == normalizedTag } == true -> RfidScanTagRole.SECONDARY
    else -> RfidScanTagRole.UNKNOWN
}

    private fun prependFeed(entry: ScanFeedEntry, existing: List<ScanFeedEntry>): List<ScanFeedEntry> =
        (listOf(entry) + existing).take(MAX_SCAN_FEED_ENTRIES)
}

private const val SCAN_PAGE_SIZE = 20
private const val MAX_SCAN_FEED_ENTRIES = 100
private const val READER_REFRESH_MS = 1_000L
private const val OPERATOR_ROLE = "operator"
private const val GOAT_PROOF_FIELD_KEY = "vaccination_goat_proof"

private fun emptyScanState(): ScanUiState = ScanUiState(
    shedLabel = "",
    cohortLabel = "",
    ringDone = 0,
    ringTotal = 0,
    ringUnitLabel = "",
    tapHint = "Hold the Bluetooth reader near the goat tag. A known tag is marked Done; an unknown tag is marked Skipped.",
    vaccineGroups = emptyList(),
    doneCount = 0,
    pendingCount = 0,
    skippedCount = 0,
    tileLabels = ScanTileLabels("Done", "Pending", "Skipped"),
    feed = emptyList(),
    roster = emptyList(),
    listTitle = "",
    submitLabel = "",
    canSubmit = false,
    scanEnabled = false,
    isRefreshing = true,
    readerConnection = ScanReaderConnection(
        readerName = "RFID reader",
        statusLabel = "Checking reader connection",
        connected = false,
        actionLabel = "Reconnect",
    ),
)
