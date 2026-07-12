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
        val cursor = _nextCursor.value ?: return@launch
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
            is ScanEvent.SelectGroup,
            is ScanEvent.OpenTile,
            ScanEvent.OpenList,
            ScanEvent.Tap -> {
                // Local UI state (vaccine group, tile filter, roster expansion, manual tap)
                // is NOT persisted in ViewModel — these are transient view state.
                // The observed roster and draft feeds are what persist from Room.
                when (event) {
                    ScanEvent.Tap -> {} // Manual tap would need a separate reducer for draft state
                    else -> {} // Filter/group/expansion state is UI-only
                }
            }
            ScanEvent.LoadMore -> loadMore()
            ScanEvent.Submit, ScanEvent.Back -> Unit // navigation — handled by the host.
        }
    }
            ringDone = done,
            doneCount = done,
            pendingCount = pending,
            skippedCount = skipped,
            canSubmit = pending == 0 && !hasMore,
            scanEnabled = true,
        )
    }

    private fun onTagRead(tag: String) {
        val target = normalize(tag)
        if (target.isEmpty()) return
        _state.update { s ->
            val index = s.roster.indexOfFirst {
                normalize(it.primaryTag) == target || it.secondaryTag?.let { t -> normalize(t) == target } == true
            }
            if (index < 0) {
                s.copy(feed = prependFeed(ScanFeedEntry(tag, null, "unknown tag · not in this shed", ScanStatus.SKIPPED), s.feed))
            } else {
                val row = s.roster[index]
                when (row.status) {
                    ScanStatus.PENDING -> markRowDone(s, index)
                    ScanStatus.DONE -> s
                    ScanStatus.SKIPPED -> s.copy(
                        feed = prependFeed(
                            ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED),
                            s.feed,
                        ),
                    )
                }
            }
        }
    }

    /** Shared by a real tag-match ([onTagRead]) and a manual ring tap ([onManualTap]): marks
     * roster row [index] (already PENDING) DONE, pushes a feed row, and rolls the counts. */
    private fun markRowDone(s: ScanUiState, index: Int): ScanUiState {
        val row = s.roster[index]
        val roster = s.roster.toMutableList().also { it[index] = row.copy(status = ScanStatus.DONE, unsynced = true) }
        return s.copy(
            roster = roster,
            feed = prependFeed(ScanFeedEntry(row.primaryTag, row.secondaryTag, row.vaccineLabel, ScanStatus.DONE), s.feed),
            ringDone = (s.ringDone + 1).coerceAtMost(s.ringTotal),
            doneCount = s.doneCount + 1,
            pendingCount = (s.pendingCount - 1).coerceAtLeast(0),
            canSubmit = s.pendingCount - 1 <= 0 && !s.hasMore,
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
