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
            ScanEvent.Tap, ScanEvent.OpenList, is ScanEvent.OpenTile -> Unit
            ScanEvent.Submit, ScanEvent.Back -> Unit // navigation — handled by the host.
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
                    ScanStatus.PENDING -> {
                        val roster = s.roster.toMutableList().also { it[index] = row.copy(status = ScanStatus.DONE, unsynced = true) }
                        s.copy(
                            roster = roster,
                            feed = listOf(ScanFeedEntry(row.primaryTag, row.secondaryTag, row.vaccineLabel, ScanStatus.DONE)) + s.feed,
                            ringDone = (s.ringDone + 1).coerceAtMost(s.ringTotal),
                            doneCount = s.doneCount + 1,
                            pendingCount = (s.pendingCount - 1).coerceAtLeast(0),
                            canSubmit = s.pendingCount - 1 <= 0,
                        )
                    }
                    ScanStatus.DONE -> s
                    ScanStatus.SKIPPED -> s.copy(
                        feed = listOf(ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED)) + s.feed,
                    )
                }
            }
        }
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
