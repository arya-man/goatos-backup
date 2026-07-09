package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidEvent
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.ui.sampleRfidState
import javax.inject.Inject

/**
 * RFID reader-pairing state holder. Seeds the interim [sampleRfidState] fixture and toggles
 * the connection locally so pair/disconnect feels live ([RfidEvent.Pair] → CONNECTED,
 * [RfidEvent.Disconnect] → DISCONNECTED). Test-read / reader-selection are inert until the
 * real RfidReaderPort is wired.
 *
 * TODO: replace the simulated toggle with the RfidReaderPort / device pairing via AppApi.
 */
@HiltViewModel
class RfidViewModel @Inject constructor() : ViewModel() {

    // TODO: wire when operator endpoints exist (no scoped operator read for the current user yet).
    private val _state = MutableStateFlow(sampleRfidState())
    val state: StateFlow<RfidUiState> = _state.asStateFlow()

    fun onEvent(event: RfidEvent) {
        when (event) {
            RfidEvent.Pair ->
                _state.update {
                    it.copy(
                        connectionState = RfidConnectionState.CONNECTED,
                        statusLabel = "Paired",
                        primaryActionLabel = "Disconnect",
                    )
                }
            RfidEvent.Disconnect ->
                _state.update {
                    it.copy(
                        connectionState = RfidConnectionState.DISCONNECTED,
                        statusLabel = "Not connected",
                        primaryActionLabel = "Pair reader",
                    )
                }
            RfidEvent.TestRead,
            is RfidEvent.SelectReader -> Unit // not yet backed.
        }
    }
}
