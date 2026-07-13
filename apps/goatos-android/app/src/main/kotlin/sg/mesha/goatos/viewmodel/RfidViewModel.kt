package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
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

    // Lifecycle-safe (MOB-010): `state` IS the WhileSubscribed projection of the hardware status
    // stream, so `reader.status` is collected ONLY while the screen collects `state` (+5s), releasing
    // hardware/Bluetooth polling when backgrounded. It must NOT be bridged into a separate always-on
    // MutableStateFlow via an eager init collector — that permanent subscriber defeats WhileSubscribed
    // and collects the hardware forever (the exact battery drain this fix targets).
    val state: StateFlow<RfidUiState> =
        reader.status
            .map { fromStatus(it) }
            .stateIn(
                viewModelScope,
                SharingStarted.WhileSubscribed(5_000),
                fromStatus(reader.status.value),
            )

    init {
        reader.refreshStatus()
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
