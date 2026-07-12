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
            flowOf(Resource(data = null))
        }).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

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
        val dto = resource.data
        nextCursor = dto?.nextCursor  // Update pagination cursor for loadMore()
        val base = dto?.let { applyResource(it, localDone) } ?: emptyScanState()
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

    /** Manual ring tap: advances the next REAL pending roster row (from the current computed
     *  state) to DONE by recording its obligation in the draft overlay, and pushes a feed row. */
    private fun onManualTap() {
        val row = state.value.roster.firstOrNull { it.status == ScanStatus.PENDING } ?: return
        markRowDone(row)
    }

    /** Hardware tag read (keyboard-wedge): match the tag against the current roster and fold it
     *  into the draft overlay — a PENDING match is marked DONE, a SKIPPED/unknown match only
     *  pushes an informational feed row. */
    private fun onTagRead(tag: String) {
        val target = normalize(tag)
        if (target.isEmpty()) return
        val row = state.value.roster.firstOrNull {
            normalize(it.primaryTag) == target || it.secondaryTag?.let { t -> normalize(t) == target } == true
        }
        if (row == null) {
            _feed.update { prependFeed(ScanFeedEntry(tag, null, "unknown tag · not in this shed", ScanStatus.SKIPPED), it) }
            return
        }
        when (row.status) {
            ScanStatus.PENDING -> markRowDone(row)
            ScanStatus.DONE -> Unit
            ScanStatus.SKIPPED -> _feed.update {
                prependFeed(
                    ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED),
                    it,
                )
            }
        }
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

    private fun applyResource(dto: ScanRosterResponseDto, localDone: Set<String>): ScanUiState {
        val rosterRows = dto.rows.map { dtoRow ->
            // Overlay local (unsynced) DONE edits so an in-progress scan survives Room re-emission.
            val locallyDone = dtoRow.obligationId.isNotBlank() && dtoRow.obligationId in localDone
            val status = if (locallyDone) ScanStatus.DONE else statusOf(dtoRow.status)
            RosterRow(
                primaryTag = dtoRow.primaryTag,
                secondaryTag = dtoRow.secondaryTag,
                vaccineLabel = dtoRow.vaccineLabel,
                status = status,
                unsynced = locallyDone,
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
