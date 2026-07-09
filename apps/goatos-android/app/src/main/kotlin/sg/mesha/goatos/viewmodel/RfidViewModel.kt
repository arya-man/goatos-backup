package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidEvent
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import sg.mesha.goatos.ui.sampleRfidState
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
        val base = sampleRfidState()
        return when (status) {
            RfidReaderStatus.READY -> base.copy(
                statusLabel = "Ready",
                connectionState = RfidConnectionState.CONNECTED,
                readerName = "RFID reader",
                readerDetail = "Ready to scan",
                primaryActionLabel = "Test read",
                testLabel = "Test read",
            )
            RfidReaderStatus.PAIRED_NOT_READY -> base.copy(
                statusLabel = "Paired · not connected",
                connectionState = RfidConnectionState.SCANNING,
                readerName = "RFID reader",
                readerDetail = "Paired — reconnect in Bluetooth settings",
                primaryActionLabel = "Open Bluetooth settings",
                testLabel = null,
            )
            RfidReaderStatus.NOT_PAIRED -> base.copy(
                statusLabel = "Not paired",
                connectionState = RfidConnectionState.DISCONNECTED,
                readerName = null,
                readerDetail = "Pair the reader in Bluetooth settings, then return",
                primaryActionLabel = "Pair reader",
                testLabel = null,
            )
            RfidReaderStatus.PERMISSION_NEEDED -> base.copy(
                statusLabel = "Allow Nearby devices",
                connectionState = RfidConnectionState.DISCONNECTED,
                readerName = null,
                readerDetail = "Grant Nearby devices to detect the reader",
                primaryActionLabel = "Open Bluetooth settings",
                testLabel = null,
            )
            RfidReaderStatus.BLUETOOTH_OFF -> base.copy(
                statusLabel = "Bluetooth off",
                connectionState = RfidConnectionState.DISCONNECTED,
                readerName = null,
                readerDetail = "Turn on Bluetooth to use the reader",
                primaryActionLabel = "Open Bluetooth settings",
                testLabel = null,
            )
        }
    }
}
