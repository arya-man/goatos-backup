package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidDetailStatus
import sg.mesha.goatos.feature.profile.RfidEvent
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import javax.inject.Inject

/**
 * RFID reader-pairing state holder, backed by the real [RfidReaderPort] (V1 keyboard-wedge).
 * The screen only shows readiness and routes the operator to the system Bluetooth pairing
 * flow — Android owns the HID connection, so there is no in-app "connect". Status is derived
 * live from InputManager + Bluetooth (docs/mobile/rfid-keyboard-reader.md).
 */
@HiltViewModel
class RfidViewModel @Inject constructor(
    private val reader: RfidReaderPort,
) : ViewModel() {

    private val _state = MutableStateFlow(fromStatus(reader.status.value))
    val state: StateFlow<RfidUiState> = _state.asStateFlow()

    init {
        reader.refreshStatus()
        // Lifecycle-safe observer (MOB-010): stateIn(...WhileSubscribed...) stops collecting
        // the RFID hardware status stream when unsubscribed for 5s, releasing hardware
        // resources when the screen is backgrounded.
        viewModelScope.launch {
            reader.status
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), reader.status.value)
                .collect { _state.value = fromStatus(it) }
        }
    }

    fun onEvent(event: RfidEvent) {
        when (event) {
            // Android owns HID connection — every action opens the system pairing flow,
            // except a test-read which just re-checks readiness.
            RfidEvent.Pair,
            RfidEvent.Disconnect,
            is RfidEvent.SelectReader -> reader.openSystemPairing()
            RfidEvent.TestRead -> reader.refreshStatus()
        }
    }

    private fun fromStatus(status: RfidReaderStatus): RfidUiState {
        val (detailStatus, connectionState) = when (status) {
            RfidReaderStatus.READY ->
                RfidDetailStatus.READY to RfidConnectionState.CONNECTED
            RfidReaderStatus.PAIRED_NOT_READY ->
                RfidDetailStatus.PAIRED_NOT_READY to RfidConnectionState.SCANNING
            RfidReaderStatus.NOT_PAIRED ->
                RfidDetailStatus.NOT_PAIRED to RfidConnectionState.DISCONNECTED
            RfidReaderStatus.PERMISSION_NEEDED ->
                RfidDetailStatus.PERMISSION_NEEDED to RfidConnectionState.DISCONNECTED
            RfidReaderStatus.BLUETOOTH_OFF ->
                RfidDetailStatus.BLUETOOTH_OFF to RfidConnectionState.DISCONNECTED
        }
        return RfidUiState(
            detailStatus = detailStatus,
            connectionState = connectionState,
            // The keyboard-wedge port surfaces no standing device name (only per-read
            // RfidRead.deviceName), so leave this null — the screen omits the name row rather than
            // showing a hardcoded label. A real reader name from a future port is passed through
            // verbatim (a device identifier is never localized); the header title is a localized
            // static string owned by RfidScreen.
            readerName = null,
        )
    }
}
