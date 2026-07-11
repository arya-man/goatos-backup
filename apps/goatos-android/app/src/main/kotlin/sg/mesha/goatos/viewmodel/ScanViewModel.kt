package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.ScanRosterRowDto
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.ui.sampleScanState
import javax.inject.Inject

/**
 * Scan (tap-to-scan) state holder. Loads the real per-animal roster for the tapped shed
 * from [ExecutionRepository.scanRoster] and folds each hardware RFID read (keyboard-wedge)
 * into the draft overlay: match the tag against the roster, mark a due animal done, push a
 * live feed row. The backend revalidates on submit — this is a draft overlay, not truth.
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

    private val _state = MutableStateFlow(sampleScanState())
    val state: StateFlow<ScanUiState> = _state.asStateFlow()

    init {
        loadRoster()
        viewModelScope.launch {
            reader.reads.collect { onTagRead(it.tag) }
        }
    }

    /** Enable/disable keyboard-wedge capture with the Scan screen's composition lifecycle. */
    fun setCaptureActive(active: Boolean) = reader.setCaptureEnabled(active)

    override fun onCleared() {
        reader.setCaptureEnabled(false)
    }

    private fun loadRoster() {
        val id = shedId ?: return // no shed threaded → keep the interim sample roster
        viewModelScope.launch {
            runCatching { repo.scanRoster(id) }
                .onSuccess { resp -> _state.update { it.withRoster(resp.rows) } }
                .onFailure { /* keep current so the screen is never blank; backend revalidates on submit */ }
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

    private fun ScanUiState.withRoster(dtoRows: List<ScanRosterRowDto>): ScanUiState {
        val roster = dtoRows.map {
            RosterRow(
                primaryTag = it.primaryTag,
                secondaryTag = it.secondaryTag,
                vaccineLabel = it.vaccineLabel,
                status = statusOf(it.status),
            )
        }
        val done = roster.count { it.status == ScanStatus.DONE }
        val skipped = roster.count { it.status == ScanStatus.SKIPPED }
        val pending = (roster.size - done - skipped).coerceAtLeast(0)
        return copy(
            roster = roster,
            feed = emptyList(),
            ringTotal = roster.size,
            ringDone = done,
            doneCount = done,
            pendingCount = pending,
            skippedCount = skipped,
            canSubmit = pending == 0,
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
                s.copy(feed = listOf(ScanFeedEntry(tag, null, "unknown tag · not in this shed", ScanStatus.SKIPPED)) + s.feed)
            } else {
                val row = s.roster[index]
                when (row.status) {
                    ScanStatus.PENDING -> markRowDone(s, index)
                    ScanStatus.DONE -> s
                    ScanStatus.SKIPPED -> s.copy(
                        feed = listOf(ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED)) + s.feed,
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
            feed = listOf(ScanFeedEntry(row.primaryTag, row.secondaryTag, row.vaccineLabel, ScanStatus.DONE)) + s.feed,
            ringDone = (s.ringDone + 1).coerceAtMost(s.ringTotal),
            doneCount = s.doneCount + 1,
            pendingCount = (s.pendingCount - 1).coerceAtLeast(0),
            canSubmit = s.pendingCount - 1 <= 0,
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
}
