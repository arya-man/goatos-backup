package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.ROSTER_SCAN_FIELD_KEY
import sg.mesha.goatos.core.data.capture.RfidScanAttemptOutcome
import sg.mesha.goatos.core.data.capture.RfidScanTagRole
import sg.mesha.goatos.core.data.capture.ScannedGoatRow
import sg.mesha.goatos.core.data.capture.ScanAttemptRepository
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanError
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
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
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
    private val tasksRepository: TasksRepository,
    private val analytics: AnalyticsPort,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")?.takeIf { it.isNotBlank() }
    private val taskId: String? = savedStateHandle.get<String>("taskId")?.takeIf { it.isNotBlank() }
    private val sopVersionId: String? = savedStateHandle.get<String>("sopVersionId")?.takeIf { it.isNotBlank() }
    private val taskRowVersion: Int? = savedStateHandle.get<Int>("taskRowVersion")?.takeIf { it > 0 }
    private var readerRefreshJob: Job? = null

    // The visible scan-list window size. loadMore() grows it; the full roster is already local in the
    // per-row SSOT after a refresh, so paging is a LOCAL window advance (page-N works offline), not a
    // network call. Room re-queries the bounded window whenever this changes.
    private val _windowSize = MutableStateFlow(SCAN_PAGE_SIZE)

    // Upstream Room flow: the scan LIST is a BOUNDED keyset window over the per-row SSOT
    // (docs/decisions/mobile-data-fetch-anti-patterns.md — render from a bounded SSOT, never a
    // whole-collection blob). Re-emits on window growth and on every roster upsert; lifecycle-aware.
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedRows: StateFlow<List<ScanRosterRowEntity>> =
        (if (shedId != null) {
            _windowSize.flatMapLatest { size -> repo.observeScanRosterRows(shedId, taskId, windowSize = size) }
        } else {
            flowOf(emptyList())
        }).stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    // Full-roster row count for this scope (page-independent) — drives hasMore and the sync/error gates.
    private val rosterTotal: StateFlow<Int> =
        (if (shedId != null) repo.observeScanRosterTotal(shedId, taskId) else flowOf(0))
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), 0)

    // Every DONE animal across the FULL roster (backend-persisted status), page-independent. The
    // submit proof gate unions this with the session local-done overlay and requires a synced proof
    // for each; see [computeProofGate].
    private val persistedDoneGoatIds: StateFlow<List<String>> =
        (if (shedId != null) repo.observeScanRosterDoneGoatIds(shedId, taskId) else flowOf(emptyList()))
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    // R50-008: full-roster status aggregates (Room GROUP BY, page-independent). Re-emits on every
    // roster upsert; combined into [state] so ring/tile counters are identical for page size 1 and 20.
    private val statusCounts: StateFlow<List<StatusCount>> =
        (if (shedId != null) {
            repo.observeScanRosterStatusCounts(shedId, taskId)
        } else {
            flowOf(emptyList())
        }).stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    private val observedProofs: StateFlow<List<ProofCaptureRow>> =
        (taskId?.let { proofCaptureRepository.observeProofs(it) } ?: flowOf(emptyList()))
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    // R50-027: this task's SOP proof policy (Room-backed via TasksRepository), driving the
    // per-goat capture's default subject instead of a hardcoded ProofSubject.GOAT.
    private val proofPolicy: StateFlow<ProofPolicy> =
        (taskId?.let { id ->
            tasksRepository.observeTaskDetail(id).map { it.data?.proofPolicy ?: ProofPolicy.Default }
        } ?: flowOf(ProofPolicy.Default))
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ProofPolicy.Default)

    /**
     * Durable RFID evidence already written to Room for this task. This is the process-recreation
     * source of truth: a killed/restarted app must render scanned goats as done instead of resetting
     * the operator to 0/N while the outbox and backend still contain those captures.
     */
    private val persistedScans: StateFlow<List<ScannedGoatRow>> =
        (taskId?.let { id ->
            scanCaptureRepository.observeScannedTags(id, ROSTER_SCAN_FIELD_KEY)
        } ?: flowOf(emptyList()))
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), emptyList())

    private val _operatorAllowed = MutableStateFlow<Boolean?>(null)
    private var currentPrincipalId: String? = null
    private var proofCaptureInFlight = false

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)
    private val _refreshError = MutableStateFlow<String?>(null)

    // Draft state for local interactions
    private val _selectedFilter = MutableStateFlow<ScanStatus?>(null)
    private val _rosterExpanded = MutableStateFlow(false)
    private val _selectedVaccineGroupId = MutableStateFlow<String?>(null)

    // Transient draft overlay: obligationIds marked DONE during this ViewModel session by a
    // hardware tag read or manual ring tap. Real hardware reads are also written to Room and
    // re-enter through [persistedScanDone]; manual taps intentionally disappear after recreation.
    private val _localDone = MutableStateFlow<Set<String>>(emptySet())
    private val _manualDone = MutableStateFlow<Set<String>>(emptySet())
    // goatIds marked DONE this session (reader or manual). Unioned into the submit proof-gate's
    // required set so an in-session completion needs a synced proof before submit is enabled, even
    // before a refresh syncs the backend status. Monotonic within a session (a done animal keeps
    // requiring proof); rebuilt from the SSOT after a refresh via [persistedDoneGoatIds].
    private val _localDoneGoatIds = MutableStateFlow<Set<String>>(emptySet())
    private val _feed = MutableStateFlow<List<ScanFeedEntry>>(emptyList())

    // Transient "already scanned" strip shown under the tap-hint card on a re-scan of an
    // already-DONE tag. Does NOT add a feed row (that would pile up duplicates) — cleared on the
    // next ACCEPTED scan and when capture is disabled, so it never lingers stale.
    private val _duplicateNotice = MutableStateFlow<String?>(null)

    // Combines the bounded SSOT window + full-roster aggregates with transient flags + draft overlay;
    // lifecycle-aware. >5 flows > Kotlin's typed combine limit (5), so use the vararg Array<*> form.
    @Suppress("UNCHECKED_CAST")
    val state: StateFlow<ScanUiState> = combine(
        observedRows,
        rosterTotal,
        persistedDoneGoatIds,
        _isRefreshing,
        _isOffline,
        _isLoadingMore,
        _selectedFilter,
        _rosterExpanded,
        _selectedVaccineGroupId,
        persistedScans,
        _localDone,
        _localDoneGoatIds,
        _feed,
        reader.status,
        reader.readerName,
        statusCounts,
        observedProofs,
        _operatorAllowed,
        _refreshError,
        proofPolicy,
        _duplicateNotice,
    ) { values: Array<Any?> ->
        val rows = values[0] as List<ScanRosterRowEntity>
        val total = values[1] as Int
        val persistedDoneGoats = values[2] as List<String>
        val isRefreshing = values[3] as Boolean
        val isOffline = values[4] as Boolean
        val isLoadingMore = values[5] as Boolean
        val selectedFilter = values[6] as ScanStatus?
        val rosterExpanded = values[7] as Boolean
        val selectedGroupId = values[8] as String?
        val persistedScans = values[9] as List<ScannedGoatRow>
        val persistedDone = persistedScans.mapNotNull { it.obligationId?.takeIf(String::isNotBlank) }.toSet()
        val localDone = persistedDone + (values[10] as Set<String>)
        val localDoneGoats = values[11] as Set<String>
        val feed = values[12] as List<ScanFeedEntry>
        val readerStatus = values[13] as RfidReaderStatus
        val readerName = values[14] as String?
        val counts = values[15] as List<StatusCount>
        val proofs = values[16] as List<ProofCaptureRow>
        val operatorAllowed = values[17] as Boolean?
        val refreshError = values[18] as String?
        val policy = values[19] as ProofPolicy
        val duplicateNotice = values[20] as String?
        // Cold cache (no rows persisted) + failed refresh → error/retry state. A warm cache stays on
        // screen; the refresh failure only flips the offline indicator.
        val error = if (total == 0 && refreshError != null) {
            ScanError(message = "Roster could not load. Check your connection and try again.", tag = "cold_cache_failed")
        } else null
        // The scan LIST renders from the bounded SSOT window; page-N animals are present once the
        // full roster is persisted (the whole roster is fetched on refresh).
        //
        // BUG FIX (Scanned-goats list undercounts DONE ring): a goat scanned below the current
        // window — a page-N animal in a shed larger than [SCAN_PAGE_SIZE], or ANY already-done
        // animal after navigate-away+back recreates this ViewModel and resets the window to
        // [SCAN_PAGE_SIZE] — was simply absent from [rows], so it could never appear in
        // [ScanUiState.roster] (and therefore never in the "Scanned goats" DONE-filtered list),
        // even though [persistedDoneGoats]/[localDoneGoats] (page-independent) already counted it
        // in the DONE ring. [computeProofGate] already solves the identical class of problem for
        // `proofActionNeeded` via [repo.scanRosterRowsByGoatIds]; apply the SAME bounded
        // outside-window fetch here so the roster the UI renders is done-complete, not just the
        // ring/tile aggregates.
        val persistedScanGoatIds = persistedScans.mapNotNull { it.goatId?.takeIf(String::isNotBlank) }.toSet()
        val hasMore = total > rows.size
        val doneGoatIds = (persistedDoneGoats.toSet() + localDoneGoats + persistedScanGoatIds)
            .filterTo(mutableSetOf()) { it.isNotBlank() }
        val fullRows = withOutOfWindowDoneRows(rows, doneGoatIds)
        val base = applyRows(
            rows = fullRows,
            total = total,
            hasMore = hasMore,
            localDone = localDone,
            persistedScans = persistedScans,
            proofs = proofs,
            operatorAllowed = operatorAllowed == true,
            isRefreshing = isRefreshing,
            taskId = taskId,
            sopVersionId = sopVersionId,
            taskRowVersion = taskRowVersion,
            requireGoatProof = policy.isPerGoatVideo,
        )
        // Full-roster (page-independent) aggregates overlay the window-derived counts (R50-008).
        val aggregated = applyFullRosterCounts(base, counts, localDone)
        // Submit gate follows the task SOP. Per-goat video mode still requires synced goat clips.
        // Shed-level video mode only gates this scan screen on all goats scanned; the submit form
        // then enforces the required 1..5 shed-level video proof clips.
        val gate = computeProofGate(aggregated, persistedDoneGoats, localDoneGoats, proofs, policy)
        val serverFeed = gate.roster
            .asSequence()
            .filter { it.status == ScanStatus.DONE && !it.scannedAtLabel.isNullOrBlank() }
            .map {
                ScanFeedEntry(
                    primaryTag = it.primaryTag,
                    secondaryTag = it.secondaryTag,
                    vaccineLabel = it.vaccineLabel,
                    status = ScanStatus.DONE,
                    scannedAtLabel = it.scannedAtLabel,
                )
            }
            .toList()
        val localFeedKeys = feed.map { it.primaryTag to it.vaccineLabel }.toSet()
        val mergedFeed = feed + serverFeed.filterNot { (it.primaryTag to it.vaccineLabel) in localFeedKeys }
        gate.copy(
            feed = mergedFeed,
            isRefreshing = isRefreshing,
            isLoadingMore = isLoadingMore,
            lastSyncedAt = rows.maxOfOrNull { it.updatedAt } ?: gate.lastSyncedAt,
            isOffline = isOffline,
            error = error,
            selectedFilter = selectedFilter,
            rosterExpanded = rosterExpanded,
            vaccineGroups = gate.vaccineGroups.map { group ->
                group.copy(active = group.id == selectedGroupId)
            },
            readerConnection = readerStatus.toScanReaderConnection(readerName),
            duplicateNotice = duplicateNotice,
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
            reader.reads.collect { onTagRead(it.tag, it.capturedAtDeviceMs) }
        }
    }

    /** Enable/disable keyboard-wedge capture with the Scan screen's composition lifecycle. */
    fun setCaptureActive(active: Boolean) {
        reader.setCaptureEnabled(active)
        if (active) {
            // Input re-enabled (e.g. returning to the Scan screen) — clear any stale strip from a
            // prior session rather than showing an old duplicate notice.
            _duplicateNotice.value = null
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

    /** Network side of stale-while-revalidate: walks the WHOLE shed roster into the per-row SSOT
     *  (the [observedRows]/[rosterTotal] Flows re-emit and update [state]); on failure it only flips
     *  [_isOffline] — the previously-persisted roster, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        val id = shedId ?: return@launch
        _isRefreshing.value = true
        _refreshError.value = null
        taskId?.let { tasksRepository.refreshTaskDetail(it) }
        val result = repo.refreshScanRoster(id, taskId, limit = SCAN_PAGE_SIZE)
        taskId?.let { tasksRepository.refreshShedCompletionSummary(it) }
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
        _refreshError.value = result.exceptionOrNull()?.message
    }

    /** Reveal the next page of the ALREADY-LOCAL roster by growing the observed SSOT window. No
     *  network call — the whole roster is in Room after [refresh], so page-N works offline. */
    fun loadMore() {
        if (shedId == null) return
        if (_windowSize.value >= rosterTotal.value) return
        _windowSize.update { it + SCAN_PAGE_SIZE }
    }

    private fun loadRosterAndRefresh() {
        val id = shedId ?: return
        AnalyticsFunnels.trackScanStarted(analytics, id)
        taskId?.let { selectedTaskId ->
            viewModelScope.launch {
                scanCaptureRepository.enqueuePendingScans(selectedTaskId, ROSTER_SCAN_FIELD_KEY)
            }
        }
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
        // Manual selection is draft-only. It must never mint an accepted RFID attempt or a
        // durable roster scan; only a subsequent physical reader event can supply that evidence.
    }

    /** Hardware tag read (keyboard-wedge): match the tag against the FULL roster (R50-007: via bounded
     *  Room query, not just the loaded page in state.value.roster.firstOrNull) and fold into draft overlay.
     *  A PENDING match is marked DONE; SKIPPED/unknown only pushes an informational feed row. */
    private fun onTagRead(tag: String, capturedAtMs: Long) {
        if (!canAcceptScanInput()) return
        val target = normalize(tag)
        if (target.isEmpty()) return
        val id = shedId ?: return
        viewModelScope.launch {
            // R50-007: Find by tag in full shed roster via bounded indexed Room query
            val dbRow = repo.findScanRosterByTag(id, taskId, target) ?: run {
                recordScanAttempt(
                    tag = tag,
                    row = null,
                    outcome = RfidScanAttemptOutcome.UNKNOWN,
                    tagRole = RfidScanTagRole.UNKNOWN,
                    reason = "unknown_tag",
                )
                _feed.update {
                    prependFeed(
                        ScanFeedEntry(tag, null, "unknown tag · not in this shed", ScanStatus.SKIPPED, scanTimeLabel(capturedAtMs)),
                        it,
                    )
                }
                return@launch
            }
            // Map DB row to UI row for status and tag-role matching, overlaying the session's
            // local unsynced DONE edits (same overlay as applyResource) so a re-scan of an
            // already-locally-done goat takes the DUPLICATE path, not a second capture.
            val locallyDone = dbRow.obligationId.isNotBlank() && dbRow.obligationId in draftDoneIds()
            val row = RosterRow(
                primaryTag = dbRow.primaryTag,
                secondaryTag = dbRow.secondaryTag,
                vaccineLabel = dbRow.vaccineLabel,
                status = if (locallyDone) ScanStatus.DONE else statusOf(dbRow.status),
                unsynced = locallyDone,
                goatId = dbRow.goatId,
                obligationId = dbRow.obligationId,
            )
            val tagRole = row.tagRoleFor(target)
            when (row.status) {
                ScanStatus.PENDING -> {
                    markRowDone(row, capturedAtMs)
                    _manualDone.update { it - row.obligationId }
                    _duplicateNotice.value = null
                    recordScanAttempt(tag, row, RfidScanAttemptOutcome.ACCEPTED, tagRole, null)
                    recordRosterScan(row, tag, capturedAtMs)
                }
                ScanStatus.DONE -> {
                    if (row.obligationId in _manualDone.value) {
                        _manualDone.update { it - row.obligationId }
                        _duplicateNotice.value = null
                        recordScanAttempt(tag, row, RfidScanAttemptOutcome.ACCEPTED, tagRole, "manual_done_replaced_by_reader_scan")
                        recordRosterScan(row, tag, capturedAtMs)
                    } else {
                        recordScanAttempt(tag, row, RfidScanAttemptOutcome.DUPLICATE, tagRole, "goat_already_scanned")
                        // Re-scanning an already-done tag must NOT pile another row into the feed
                        // (the tag already has a DONE row there) — surface a transient strip instead.
                        _duplicateNotice.value = "Already scanned · ${row.vaccineLabel}"
                    }
                }
                ScanStatus.SKIPPED -> {
                    recordScanAttempt(tag, row, RfidScanAttemptOutcome.NOT_DUE, tagRole, "not_due")
                    _feed.update {
                        prependFeed(
                            ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED, scanTimeLabel(capturedAtMs)),
                            it,
                        )
                    }
                }
            }
        }
    }

    /** RFID/manual input is accepted once the FULL roster is local (any tag validates against the
     *  complete SSOT via [findScanRosterByTag], independent of the visible window). A background
     *  refresh is stale-while-revalidate only; while Room has a roster, it must not block scanning. */
    private fun canAcceptScanInput(): Boolean =
        _operatorAllowed.value == true && rosterTotal.value > 0 && !state.value.hasMore

    private fun draftDoneIds(): Set<String> =
        persistedScans.value.mapNotNull { it.obligationId?.takeIf(String::isNotBlank) }.toSet() + _localDone.value

    /** Shared by a real tag-match ([onTagRead]) and a manual ring tap ([onManualTap]): records
     * [row]'s obligation as locally DONE (unsynced) in the draft overlay and pushes a feed row.
     * The combine re-derives the roster + counts from this set on the next emission. */
    private fun markRowDone(row: RosterRow, capturedAtMs: Long = System.currentTimeMillis()) {
        if (row.obligationId.isBlank()) return
        _localDone.update { it + row.obligationId }
        // Track the goat as done this session so the submit proof gate requires its proof video even
        // before a refresh syncs the backend status (option 2: proof over the full roster).
        if (row.goatId.isNotBlank()) _localDoneGoatIds.update { it + row.goatId }
        _feed.update {
            prependFeed(ScanFeedEntry(row.primaryTag, row.secondaryTag, row.vaccineLabel, ScanStatus.DONE, scanTimeLabel(capturedAtMs)), it)
        }
    }

    private fun recordRosterScan(row: RosterRow, tag: String, capturedAtMs: Long) {
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
                capturedAtMs = capturedAtMs,
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

    /** R50-008: derive ring/tile counters from the FULL shed roster (Room GROUP BY aggregates),
     *  not the loaded page, then overlay the session's local unsynced DONE edits. A local edit only
     *  increments done when the persisted row is not already DONE (no double count after a refresh
     *  syncs the backend truth). Falls back to the page-derived counts only while the row table is
     *  still empty (cold pre-refresh with a leftover blob cache). Counters are therefore identical
     *  for page size 1 and 20 once the roster is persisted. */
    private suspend fun applyFullRosterCounts(
        base: ScanUiState,
        counts: List<StatusCount>,
        localDone: Set<String>,
    ): ScanUiState {
        val id = shedId ?: return base
        val dbTotal = counts.sumOf { it.count }
        if (dbTotal == 0) return base
        val dbDone = counts.filter { statusOf(it.status) == ScanStatus.DONE }.sumOf { it.count }
        val dbSkipped = counts.filter { statusOf(it.status) == ScanStatus.SKIPPED }.sumOf { it.count }
        // Local unsynced DONE overlay: count only ids whose persisted status is not already DONE.
        val extraDone = if (localDone.isEmpty()) {
            0
        } else {
            repo.getScanRosterStatusCountsFor(id, taskId, localDone.toList())
                .filter { statusOf(it.status) != ScanStatus.DONE }
                .sumOf { it.count }
        }
        val done = (dbDone + extraDone).coerceAtMost(dbTotal)
        val pending = (dbTotal - done - dbSkipped).coerceAtLeast(0)
        // canSubmit is decided authoritatively by [computeProofGate] (pending + full-roster proof).
        return base.copy(
            ringTotal = dbTotal,
            ringDone = done,
            doneCount = done,
            pendingCount = pending,
            skippedCount = dbSkipped,
        )
    }

    /** Builds the visible scan-list rows from the BOUNDED SSOT window (never a whole-collection blob).
     *  Counters here are provisional window-derived values overridden by [applyFullRosterCounts]; the
     *  submit gate is decided by [computeProofGate]. `scanEnabled`/`hasMore` derive from the FULL
     *  roster total, not the window, so page-N animals are reachable and scanning is enabled once the
     *  whole roster is local. */
    /** Widens the window-bounded SSOT [rows] with any DONE goat outside the current window,
     *  fetched by id (bounded, [repo.scanRosterRowsByGoatIds]) — the same out-of-window pattern
     *  [computeProofGate] uses for `proofActionNeeded`. Without this, [rows] can be missing a
     *  scanned goat entirely (not just mis-flagged), which is why the "Scanned goats" list could
     *  under-count the DONE ring even inside a single ViewModel session, and worse right after a
     *  navigate-away+back recreates the window at [SCAN_PAGE_SIZE]. */
    private suspend fun withOutOfWindowDoneRows(
        rows: List<ScanRosterRowEntity>,
        doneGoatIds: Set<String>,
    ): List<ScanRosterRowEntity> {
        val id = shedId ?: return rows
        if (doneGoatIds.isEmpty()) return rows
        val windowGoatIds = rows.mapTo(mutableSetOf()) { it.goatId }
        val missingGoatIds = doneGoatIds.filterNot { it in windowGoatIds }
        if (missingGoatIds.isEmpty()) return rows
        val extra = repo.scanRosterRowsByGoatIds(id, taskId, missingGoatIds)
        if (extra.isEmpty()) return rows
        return (rows + extra).sortedBy { it.seq }
    }

    private fun applyRows(
        rows: List<ScanRosterRowEntity>,
        total: Int,
        hasMore: Boolean,
        localDone: Set<String>,
        persistedScans: List<ScannedGoatRow>,
        proofs: List<ProofCaptureRow>,
        operatorAllowed: Boolean,
        isRefreshing: Boolean,
        taskId: String?,
        sopVersionId: String?,
        taskRowVersion: Int?,
        requireGoatProof: Boolean,
    ): ScanUiState {
        // R50-029: group once instead of re-filtering the full proof list per roster row
        // (O(rows * proofs) on every state build) — then a bounded per-row map lookup below.
        val goatProofsBySubject = proofs.filter { it.proofSubject == ProofSubject.GOAT }.groupBy { it.subjectId }
        val scannedAtByObligation = persistedScans
            .mapNotNull { scan ->
                val obligation = scan.obligationId?.takeIf(String::isNotBlank) ?: return@mapNotNull null
                obligation to scan.capturedAtMs
            }
            .toMap()
        val rosterRows = rows.map { it.toRosterRow(localDone, goatProofsBySubject, scannedAtByObligation, requireGoatProof) }
        val done = rosterRows.count { it.status == ScanStatus.DONE }
        val skipped = rosterRows.count { it.status == ScanStatus.SKIPPED }
        val pending = (rosterRows.size - done - skipped).coerceAtLeast(0)
        return emptyScanState().copy(
            shedId = shedId?.takeIf { it.isNotBlank() },
            taskId = taskId?.takeIf { it.isNotBlank() },
            sopVersionId = sopVersionId?.takeIf { it.isNotBlank() },
            taskRowVersion = taskRowVersion?.takeIf { it > 0 },
            roster = rosterRows,
            ringTotal = total.coerceAtLeast(rosterRows.size),
            ringDone = done,
            doneCount = done,
            pendingCount = pending,
            skippedCount = skipped,
            canSubmit = false, // computeProofGate decides
            scanEnabled = operatorAllowed && total > 0 && !hasMore,
            hasMore = hasMore,
        )
    }

    /** Maps one SSOT row to a [RosterRow], overlaying the session's local (unsynced) DONE edits and
     *  the per-goat proof status. */
    private fun ScanRosterRowEntity.toRosterRow(
        localDone: Set<String>,
        goatProofsBySubject: Map<String?, List<ProofCaptureRow>>,
        scannedAtByObligation: Map<String, Long> = emptyMap(),
        requireGoatProof: Boolean = true,
    ): RosterRow {
        val locallyDone = obligationId.isNotBlank() && obligationId in localDone
        val goatProofs = goatProofsBySubject[goatId].orEmpty()
        val capturedAtMs = scannedAtByObligation[obligationId] ?: scannedAtMs
        val serverDone = scannedAtMs != null
        return RosterRow(
            primaryTag = primaryTag,
            secondaryTag = secondaryTag,
            vaccineLabel = vaccineLabel,
            status = if (locallyDone || serverDone) ScanStatus.DONE else statusOf(status),
            unsynced = locallyDone,
            scannedAtLabel = capturedAtMs?.let(::scanTimeLabel),
            goatId = goatId,
            obligationId = obligationId,
            proofRequired = requireGoatProof,
            proofClipCount = goatProofs.count { it.syncStatus != CaptureSyncStatus.FAILED },
            proofUploadStatus = proofStatus(goatProofs),
        )
    }

    /** The submit gate is evaluated over the FULL shed roster, not just the visible window. The proof
     *  requirement itself is SOP-driven: per-goat mode requires synced goat clips here; shed-level
     *  mode lets the operator proceed to the shed submit form, where 1..5 shed videos are enforced. */
    private suspend fun computeProofGate(
        base: ScanUiState,
        persistedDoneGoats: List<String>,
        localDoneGoats: Set<String>,
        proofs: List<ProofCaptureRow>,
        policy: ProofPolicy,
    ): ScanUiState {
        if (policy.isShedLevelVideo) {
            return base.copy(
                canSubmit = base.ringTotal > 0 && base.pendingCount == 0,
                proofActionNeeded = emptyList(),
            )
        }
        val id = shedId ?: return base
        val requiredGoatIds = (persistedDoneGoats.toSet() + localDoneGoats).filter { it.isNotBlank() }.toSet()
        val syncedGoatIds = proofs
            .filter { it.proofSubject == ProofSubject.GOAT && it.syncStatus == CaptureSyncStatus.SYNCED && !it.serverProofId.isNullOrBlank() }
            .mapNotNull { it.subjectId }
            .toSet()
        val missingGoatIds = requiredGoatIds - syncedGoatIds
        val proofComplete = missingGoatIds.isEmpty()
        // Surface the proof-incomplete animals (bounded set) even if they are outside the window.
        val goatProofsBySubject = proofs.filter { it.proofSubject == ProofSubject.GOAT }.groupBy { it.subjectId }
        val actionNeeded = if (missingGoatIds.isEmpty()) {
            emptyList()
        } else {
            repo.scanRosterRowsByGoatIds(id, taskId, missingGoatIds.toList())
                .sortedBy { it.seq }
                .map { it.toRosterRow(emptySet(), goatProofsBySubject) }
        }
        val canSubmit = base.ringTotal > 0 && base.pendingCount == 0 && proofComplete
        return base.copy(canSubmit = canSubmit, proofActionNeeded = actionNeeded)
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
        val policy = proofPolicy.value
        if (!policy.isPerGoatVideo || _operatorAllowed.value != true || goatId.isBlank() || proofCaptureInFlight) return
        // Resolve the goat from the visible window OR the proof-action-needed list — an animal needing
        // a proof re-capture may be below the scroll window (the gate surfaces the full-roster set).
        val current = state.value
        val row = (current.roster + current.proofActionNeeded)
            .firstOrNull { it.goatId == goatId && it.status == ScanStatus.DONE } ?: return
        proofCaptureInFlight = true
        viewModelScope.launch {
            try {
                val captured = proofCaptureSource.captureVideo() ?: return@launch
                proofCaptureRepository.capture(
                    taskId = selectedTaskId,
                    fieldKey = GOAT_PROOF_FIELD_KEY,
                    // R50-027: policy-driven default subject (falls back to GOAT via
                    // ProofPolicy.Default.subjectScope when no policy has loaded yet).
                    subject = policy.defaultSubject,
                    subjectId = row.goatId,
                    localUri = captured.localUri,
                    mimeType = captured.mimeType,
                    caption = null,
                    scopeType = "task",
                    scopeId = selectedTaskId,
                    capturedStartMs = captured.startedAtMs,
                    capturedEndMs = captured.endedAtMs,
                    capturedByPrincipalId = currentPrincipalId,
                    proofPolicy = policy,
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

private val IST_ZONE: ZoneId = ZoneId.of("Asia/Kolkata")
private val SCAN_TIME_FORMATTER: DateTimeFormatter =
    DateTimeFormatter.ofPattern("d MMM, h:mm a 'IST'").withZone(IST_ZONE)

private fun scanTimeLabel(capturedAtMs: Long): String =
    "Scanned ${SCAN_TIME_FORMATTER.format(Instant.ofEpochMilli(capturedAtMs))}"

    // Dedup by primaryTag: a re-scan of a tag already in the feed (unknown/skipped/done paths all
    // funnel through here) replaces its row in place instead of piling up a second entry — the tag
    // moves to the top of the feed with its latest status/time.
    private fun prependFeed(entry: ScanFeedEntry, existing: List<ScanFeedEntry>): List<ScanFeedEntry> =
        (listOf(entry) + existing.filterNot { it.primaryTag == entry.primaryTag }).take(MAX_SCAN_FEED_ENTRIES)
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
    isInitialLoading = true,
)
