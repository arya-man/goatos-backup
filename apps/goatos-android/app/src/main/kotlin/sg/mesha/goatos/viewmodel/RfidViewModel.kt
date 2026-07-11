package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
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
        viewModelScope.launch {
            reader.status.collect { _state.value = fromStatus(it) }
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
        val (detailStatus, connectionState, readerName) = when (status) {
            RfidReaderStatus.READY ->
                Triple(RfidDetailStatus.READY, RfidConnectionState.CONNECTED, "RFID reader")
            RfidReaderStatus.PAIRED_NOT_READY ->
                Triple(RfidDetailStatus.PAIRED_NOT_READY, RfidConnectionState.SCANNING, "RFID reader")
            RfidReaderStatus.NOT_PAIRED ->
                Triple(RfidDetailStatus.NOT_PAIRED, RfidConnectionState.DISCONNECTED, null)
            RfidReaderStatus.PERMISSION_NEEDED ->
                Triple(RfidDetailStatus.PERMISSION_NEEDED, RfidConnectionState.DISCONNECTED, null)
            RfidReaderStatus.BLUETOOTH_OFF ->
                Triple(RfidDetailStatus.BLUETOOTH_OFF, RfidConnectionState.DISCONNECTED, null)
        }
        return RfidUiState(
            title = "RFID reader",
            detailStatus = detailStatus,
            connectionState = connectionState,
            readerName = readerName,
        )
    }
}
