package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidDetailStatus
import sg.mesha.goatos.feature.profile.RfidEvent
import sg.mesha.goatos.feature.profile.RfidReaderRow
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.rfid.RfidReaderDevice
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
    private val analytics: AnalyticsPort,
) : ViewModel() {

    // Lifecycle-safe (MOB-010): `state` IS the WhileSubscribed projection of the hardware status
    // stream, so `reader.status` is collected ONLY while the screen collects `state` (+5s), releasing
    // hardware/Bluetooth polling when backgrounded. It must NOT be bridged into a separate always-on
    // MutableStateFlow via an eager init collector — that permanent subscriber defeats WhileSubscribed
    // and collects the hardware forever (the exact battery drain this fix targets).
    val state: StateFlow<RfidUiState> =
        combine(reader.status, reader.readerName, reader.devices, readerRefreshPulse()) { status, readerName, devices, _ ->
            fromStatus(status, readerName, devices)
        }
            .stateIn(
                viewModelScope,
                SharingStarted.WhileSubscribed(5_000),
                fromStatus(reader.status.value),
            )

    init {
        analytics.track(AnalyticsEvents.RFID_READER_SCREEN_OPENED)
        reader.refreshStatus()
    }

    fun onEvent(event: RfidEvent) {
        when (event) {
            // Android owns HID connection. Only the explicit Bluetooth-settings action
            // leaves the app; row taps and test-read just re-check readiness in place.
            RfidEvent.Pair -> {
                trackAction("open_pairing")
                reader.openSystemPairing()
            }
            RfidEvent.Disconnect -> {
                trackAction("open_pairing_disconnected")
                reader.openSystemPairing()
            }
            RfidEvent.TestRead -> {
                trackAction("test_read")
                reader.refreshStatus()
            }
            is RfidEvent.SelectReader -> {
                trackAction("select_reader")
                reader.refreshStatus()
            }
        }
    }

    private fun trackAction(action: String) {
        analytics.track(
            AnalyticsEvents.RFID_READER_ACTION,
            mapOf(AnalyticsEvents.Params.ACTION to action),
        )
    }

    private fun fromStatus(
        status: RfidReaderStatus,
        readerName: String? = reader.readerName.value,
        devices: List<RfidReaderDevice> = reader.devices.value,
    ): RfidUiState {
        val (detailStatus, connectionState) = when (status) {
            RfidReaderStatus.READY ->
                RfidDetailStatus.READY to RfidConnectionState.CONNECTED
            RfidReaderStatus.PAIRED_NOT_READY ->
                RfidDetailStatus.PAIRED_NOT_READY to RfidConnectionState.DISCONNECTED
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
            readerName = readerName,
            discovered = devices.map { device ->
                RfidReaderRow(
                    id = device.id,
                    name = device.name,
                    detail = device.detail,
                    signalLabel = device.signalLabel,
                )
            },
        )
    }

    private fun readerRefreshPulse() = flow {
        while (true) {
            reader.refreshStatus()
            emit(Unit)
            delay(READER_REFRESH_MS)
        }
    }

    private companion object {
        const val READER_REFRESH_MS = 1_000L
    }
}
