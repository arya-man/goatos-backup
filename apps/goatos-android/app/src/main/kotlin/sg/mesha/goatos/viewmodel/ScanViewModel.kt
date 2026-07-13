package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
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

    private val _state = MutableStateFlow(emptyScanState())
    val state: StateFlow<ScanUiState> = _state.asStateFlow()

    // Lifecycle-aware StateFlow: automatically cancel when viewModelScope clears
    private val scanRosterFlow: StateFlow<Resource<ScanRosterResponseDto>> =
        if (!shedId.isNullOrBlank() && !taskId.isNullOrBlank()) {
            repo.observeScanRoster(shedId, taskId, limit = SCAN_PAGE_SIZE)
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource.loading())
        } else {
            MutableStateFlow(Resource.loading())
        }

    init {
        // Cache-first: renders whatever Room already has (possibly nothing, on a cold
        // install) immediately, then re-renders after every successful refresh below.
        viewModelScope.launch {
            scanRosterFlow.collect { resource ->
                applyResource(resource)
            }
        }
        loadRosterAndRefresh()
        viewModelScope.launch {
            reader.reads.collect { onTagRead(it.tag) }
        }
    }

    /** Enable/disable keyboard-wedge capture with the Scan screen's composition lifecycle. */
    fun setCaptureActive(active: Boolean) = reader.setCaptureEnabled(active)

    override fun onCleared() {
        reader.setCaptureEnabled(false)
    }

    /** Network side of stale-while-revalidate: upserts Room on success; on failure it only flips
     *  [ScanUiState.isOffline] — cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        val id = shedId ?: return@launch
        val selectedTask = taskId ?: return@launch
        _state.update { it.copy(isRefreshing = true) }
        val result = repo.refreshScanRoster(id, selectedTask, limit = SCAN_PAGE_SIZE)
        _state.update { current ->
            when {
                result.isSuccess -> current.copy(isRefreshing = false, isOffline = false)
                // A cache was already rendered (lastSyncedAt set by a prior emission) —
                // keep it on screen and just surface the offline signal.
                current.lastSyncedAt != null -> current.copy(isRefreshing = false, isOffline = true)
                // Never synced, ever: no cache to fall back to.
                else -> current.copy(isRefreshing = false, isOffline = true)
            }
        }
    }

    fun loadMore() = viewModelScope.launch {
        val id = shedId ?: return@launch
        val selectedTask = taskId ?: return@launch
        val cursor = nextCursor ?: return@launch
        if (_state.value.isLoadingMore) return@launch
        _state.update { it.copy(isLoadingMore = true) }
        val result = repo.appendScanRoster(id, selectedTask, cursor, limit = SCAN_PAGE_SIZE)
        _state.update { current ->
            current.copy(
                isLoadingMore = false,
                isOffline = result.isFailure,
            )
        }
    }

    private fun loadRosterAndRefresh() {
        if (shedId.isNullOrBlank() || taskId.isNullOrBlank()) return
        refresh()
    }

    private fun applyResource(resource: Resource<ScanRosterResponseDto>) {
        val dto = resource.data
        nextCursor = dto?.nextCursor
        _state.update { current ->
            if (dto != null) {
                current.withRoster(dto.rows, hasMore = dto.nextCursor != null)
                    .copy(
                        isRefreshing = current.isRefreshing,
                        lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
                        isOffline = current.isOffline,
                        hasMore = dto.nextCursor != null,
                        isLoadingMore = current.isLoadingMore,
                    )
            } else {
                current.copy(
                    isRefreshing = current.isRefreshing,
                    lastSyncedAt = current.lastSyncedAt,
                    isOffline = current.isOffline,
                )
            }
        }
    }

    fun onEvent(event: ScanEvent) {
        when (event) {
            is ScanEvent.SelectGroup ->
                _state.update { current ->
                    current.copy(vaccineGroups = current.vaccineGroups.map { it.copy(active = it.id == event.groupId) })
                }
            is ScanEvent.OpenTile ->
                // Toggle: tapping the already-active tile clears the filter (mock's
                // Done/Pending/Skipped chips → scan-list overlay, folded onto the tile itself).
                _state.update { current ->
                    current.copy(selectedFilter = if (current.selectedFilter == event.status) null else event.status)
                }
            ScanEvent.OpenList ->
                // Mock's "Tap Done · Pending · Skipped to see the animals" hint — opens the
                // full (unfiltered) roster overlay; toggles closed on a second tap.
                _state.update { current -> current.copy(rosterExpanded = !current.rosterExpanded) }
            ScanEvent.Tap -> onManualTap()
            ScanEvent.LoadMore -> loadMore()
            ScanEvent.Submit, ScanEvent.Back -> Unit // navigation — handled by the host.
        }
    }

    /**
     * The reader-ring tap (mock: `wrap.addEventListener('click', tap)`, which advances the
     * scan and adds a feed row). Real hardware reads arrive via [reader]'s keyboard-wedge
     * flow ([onTagRead]) regardless of this tap; the ring itself is the manual-confirm path
     * for a shed with no reader paired/ready. It advances the next REAL pending roster row to
     * done — never a fabricated tag/animal like the mock's random `rid()` — so the roster
     * stays data-truthful. A no-op when nothing is left pending (mirrors the mock's idle tap).
     */
    private fun onManualTap() {
        _state.update { s ->
            val index = s.roster.indexOfFirst { it.status == ScanStatus.PENDING }
            if (index < 0) s else markRowDone(s, index)
        }
    }

    private fun ScanUiState.withRoster(dtoRows: List<ScanRosterRowDto>, hasMore: Boolean): ScanUiState {
        val localByObligation = roster
            .filter { it.unsynced && it.obligationId.isNotBlank() }
            .associateBy { it.obligationId }
        val roster = dtoRows.map {
            val local = localByObligation[it.obligationId]
            RosterRow(
                primaryTag = it.primaryTag,
                secondaryTag = it.secondaryTag,
                vaccineLabel = it.vaccineLabel,
                status = local?.status ?: statusOf(it.status),
                unsynced = local?.unsynced == true,
                goatId = it.goatId,
                obligationId = it.obligationId,
            )
        }
        val done = roster.count { it.status == ScanStatus.DONE }
        val skipped = roster.count { it.status == ScanStatus.SKIPPED }
        val pending = (roster.size - done - skipped).coerceAtLeast(0)
        return copy(
            roster = roster,
            feed = feed,
            ringTotal = roster.size,
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
