package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.ui.sampleScanState
import javax.inject.Inject

/**
 * Scan (tap-to-scan) state holder. Enables the keyboard-wedge capture while this screen is
 * active and folds each hardware tag read into the local draft overlay: match the read
 * against the cached shed roster (primary/secondary tag), mark a due animal done, and push a
 * live feed row. The backend revalidates on submit — this is a draft UX overlay, not truth
 * (docs/mobile/rfid-keyboard-reader.md §Matching And Feedback).
 *
 * TODO: seed the roster from the per-shed scan-roster read once that backend endpoint exists;
 * today it renders the sample roster so the capture path is exercised end-to-end.
 */
@HiltViewModel
class ScanViewModel @Inject constructor(
    private val reader: RfidReaderPort,
) : ViewModel() {

    private val _state = MutableStateFlow(sampleScanState())
    val state: StateFlow<ScanUiState> = _state.asStateFlow()

    init {
        reader.setCaptureEnabled(true)
        viewModelScope.launch {
            reader.reads.collect { onTagRead(it.tag) }
        }
    }

    override fun onCleared() {
        reader.setCaptureEnabled(false)
    }

    fun onEvent(event: ScanEvent) {
        when (event) {
            is ScanEvent.SelectGroup ->
                _state.update { current ->
                    current.copy(
                        vaccineGroups = current.vaccineGroups.map {
                            it.copy(active = it.id == event.groupId)
                        },
                    )
                }
            ScanEvent.Tap,
            ScanEvent.OpenList,
            is ScanEvent.OpenTile -> Unit
            // Navigation — handled by the nav host.
            ScanEvent.Submit,
            ScanEvent.Back -> Unit
        }
    }

    /** Fold one captured RFID tag into the draft scan overlay. */
    private fun onTagRead(tag: String) {
        val target = normalize(tag)
        if (target.isEmpty()) return
        _state.update { s ->
            val index = s.roster.indexOfFirst {
                normalize(it.primaryTag) == target || it.secondaryTag?.let { t -> normalize(t) == target } == true
            }
            if (index < 0) {
                // Unknown tag — never creates an animal; show a red skip feed row.
                s.copy(feed = listOf(unknownEntry(tag)) + s.feed)
            } else {
                val row = s.roster[index]
                when (row.status) {
                    ScanStatus.PENDING -> {
                        val roster = s.roster.toMutableList().also {
                            it[index] = row.copy(status = ScanStatus.DONE, unsynced = true)
                        }
                        s.copy(
                            roster = roster,
                            feed = listOf(
                                ScanFeedEntry(row.primaryTag, row.secondaryTag, row.vaccineLabel, ScanStatus.DONE),
                            ) + s.feed,
                            ringDone = (s.ringDone + 1).coerceAtMost(s.ringTotal),
                            doneCount = s.doneCount + 1,
                            pendingCount = (s.pendingCount - 1).coerceAtLeast(0),
                            canSubmit = s.pendingCount - 1 <= 0,
                        )
                    }
                    // Idempotent local duplicate — already recorded, no destructive change.
                    ScanStatus.DONE -> s
                    // Known but not due here — non-destructive red feedback.
                    ScanStatus.SKIPPED -> s.copy(
                        feed = listOf(
                            ScanFeedEntry(row.primaryTag, row.secondaryTag, "not due · ${row.vaccineLabel}", ScanStatus.SKIPPED),
                        ) + s.feed,
                    )
                }
            }
        }
    }

    private fun unknownEntry(tag: String) =
        ScanFeedEntry(primaryTag = tag, secondaryTag = null, vaccineLabel = "unknown tag · not in this shed", status = ScanStatus.SKIPPED)

    /** Tags print with spaces on some readers; compare on alphanumerics only. */
    private fun normalize(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()
}
