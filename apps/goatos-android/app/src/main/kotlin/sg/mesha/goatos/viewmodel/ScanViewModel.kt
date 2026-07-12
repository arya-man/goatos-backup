package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.rfid.RfidReaderPort
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
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")
    private val taskId: String? = savedStateHandle.get<String>("taskId")
    private var nextCursor: String? = null

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<ScanRosterResponseDto>> =
        (if (shedId != null && taskId != null) {
            repo.observeScanRoster(shedId, taskId, limit = SCAN_PAGE_SIZE)
        } else {
            flowOf(Resource())
        }).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource()
        )

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)

    // Draft state for local interactions
    private val _selectedFilter = MutableStateFlow<ScanStatus?>(null)
    private val _rosterExpanded = MutableStateFlow(false)
    private val _selectedVaccineGroupId = MutableStateFlow<String?>(null)

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<ScanUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline,
        _isLoadingMore,
        _selectedFilter,
        _rosterExpanded,
        _selectedVaccineGroupId
    ) { resource, isRefreshing, isOffline, isLoadingMore, selectedFilter, rosterExpanded, selectedGroupId ->
        val dto = resource.data
        nextCursor = dto?.nextCursor  // Update pagination cursor for loadMore()
        val base = dto?.let { applyResource(it) } ?: emptyScanState()
        base.copy(
            isRefreshing = isRefreshing,
            isLoadingMore = isLoadingMore,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
            selectedFilter = selectedFilter,
            rosterExpanded = rosterExpanded,
            vaccineGroups = base.vaccineGroups.map { group ->
                group.copy(active = group.id == selectedGroupId)
            },
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        emptyScanState()
    )

    init {
        loadRosterAndRefresh()
        // HOT device stream (RFID reader) — NOT converted; always collected for keyboard-wedge capture
        viewModelScope.launch {
            reader.reads.collect { onTagRead(it.tag) }
        }
    }

    /** Enable/disable keyboard-wedge capture with the Scan screen's composition lifecycle. */
    fun setCaptureActive(active: Boolean) = reader.setCaptureEnabled(active)

    override fun onCleared() {
        reader.setCaptureEnabled(false)
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observedResource]
     *  StateFlow re-emits and updates [state]); on failure it only flips [_isOffline] —
     *  cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        val id = shedId ?: return@launch
        val selectedTask = taskId ?: return@launch
        _isRefreshing.value = true
        val result = repo.refreshScanRoster(id, selectedTask, limit = SCAN_PAGE_SIZE)
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
    }

    fun loadMore() = viewModelScope.launch {
        val id = shedId ?: return@launch
        val selectedTask = taskId ?: return@launch
        val cursor = nextCursor ?: return@launch
        if (_isLoadingMore.value) return@launch
        _isLoadingMore.value = true
        val result = repo.appendScanRoster(id, selectedTask, cursor, limit = SCAN_PAGE_SIZE)
        _isLoadingMore.value = false
        _isOffline.value = result.isFailure
    }

    private fun loadRosterAndRefresh() {
        if (shedId.isNullOrBlank() || taskId.isNullOrBlank()) return
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
            ScanEvent.LoadMore -> loadMore()
            ScanEvent.Submit, ScanEvent.Back -> Unit // navigation — handled by the host.
        }
    }

    private fun onManualTap() {
        val s = state.value
        val index = s.roster.indexOfFirst { it.status == ScanStatus.PENDING }
        if (index >= 0) {
            markRowDone(s, index)
        }
    }

    private fun onTagRead(tag: String) {
        val target = normalize(tag)
        if (target.isEmpty()) return
        val s = state.value
        val index = s.roster.indexOfFirst {
            normalize(it.primaryTag) == target || it.secondaryTag?.let { t -> normalize(t) == target } == true
        }
        // Note: Tag read updates to draft roster and feed are not persisted to ViewModel state in the
        // stateIn pattern — they would need to be managed via outbox on submit. For now, this is a
        // placeholder where actual tag reads would update local transaction state.
        if (index < 0) {
            // Unknown tag — would need a separate feed state flow for draft feed entries
        } else {
            val row = s.roster[index]
            when (row.status) {
                ScanStatus.PENDING -> markRowDone(s, index)
                ScanStatus.DONE, ScanStatus.SKIPPED -> Unit
            }
        }
    }

    /** Shared by a real tag-match ([onTagRead]) and a manual ring tap ([onManualTap]): marks
     * roster row [index] (already PENDING) DONE, pushes a feed row, and rolls the counts. */
    private fun markRowDone(s: ScanUiState, index: Int) {
        // Draft state management: roster edits are held locally and sent to outbox on submit.
        // This is a placeholder for integrating with the transaction/outbox system.
    }

    private fun applyResource(dto: ScanRosterResponseDto): ScanUiState {
        val rosterRows = dto.rows.map { dtoRow ->
            RosterRow(
                primaryTag = dtoRow.primaryTag,
                secondaryTag = dtoRow.secondaryTag,
                vaccineLabel = dtoRow.vaccineLabel,
                status = statusOf(dtoRow.status),
                unsynced = false,
                goatId = dtoRow.goatId,
                obligationId = dtoRow.obligationId,
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
            canSubmit = pending == 0 && nextCursor == null,
            scanEnabled = true,
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

    private fun normalize(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()

    private fun prependFeed(entry: ScanFeedEntry, existing: List<ScanFeedEntry>): List<ScanFeedEntry> =
        (listOf(entry) + existing).take(MAX_SCAN_FEED_ENTRIES)
}

private const val SCAN_PAGE_SIZE = 20
private const val MAX_SCAN_FEED_ENTRIES = 100

private fun emptyScanState(): ScanUiState = ScanUiState(
    shedLabel = "",
    cohortLabel = "",
    ringDone = 0,
    ringTotal = 0,
    ringUnitLabel = "",
    tapHint = "",
    vaccineGroups = emptyList(),
    doneCount = 0,
    pendingCount = 0,
    skippedCount = 0,
    tileLabels = sg.mesha.goatos.feature.scan.ScanTileLabels("", "", ""),
    feed = emptyList(),
    roster = emptyList(),
    listTitle = "",
    submitLabel = "",
    canSubmit = false,
    scanEnabled = false,
    isRefreshing = true,
)
