package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.ui.sampleScanState
import javax.inject.Inject

/**
 * Scan (tap-to-scan) state holder. Seeds the interim [sampleScanState] fixture.
 * [ScanEvent.SelectGroup] locally highlights the tapped vaccine chip so the filter
 * feels live; [ScanEvent.Submit] / [ScanEvent.Back] are navigation, routed by the host.
 *
 * TODO: replace the fake seed with the shed roster/status read + scan writes via AppApi.
 */
@HiltViewModel
class ScanViewModel @Inject constructor() : ViewModel() {

    // TODO: wire when operator endpoints exist (no scoped operator read for the current user yet).
    private val _state = MutableStateFlow(sampleScanState())
    val state: StateFlow<ScanUiState> = _state.asStateFlow()

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
            // Local affordances with no fake-data mutation yet.
            ScanEvent.Tap,
            ScanEvent.OpenList,
            is ScanEvent.OpenTile -> Unit
            // Navigation — handled by the nav host.
            ScanEvent.Submit,
            ScanEvent.Back -> Unit
        }
    }
}
