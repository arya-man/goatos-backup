package sg.mesha.goatos.rfid

import android.view.KeyEvent
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * Readiness of the field RFID reader. V1 is a Bluetooth HID keyboard-wedge reader
 * (see docs/mobile/rfid-keyboard-reader.md): Android owns the HID connection; Goat OS
 * only observes whether tag reads can arrive right now and guides the operator to the
 * system pairing flow.
 */
enum class RfidReaderStatus {
    /** External input device present (a reader), or a recent successful read. Green. */
    READY,

    /** A bonded reader exists but no matching input device is active. Amber. */
    PAIRED_NOT_READY,

    /** No matching paired reader. */
    NOT_PAIRED,

    /** Android 12+ Nearby-devices permission missing for paired-device state. */
    PERMISSION_NEEDED,

    /** Bluetooth adapter disabled. */
    BLUETOOTH_OFF,
}

/** One tag read captured from the keyboard-wedge reader. */
data class RfidRead(
    val tag: String,
    val deviceName: String? = null,
    val capturedAtDeviceMs: Long,
)

/** A reader-ish device Android currently exposes to the app. */
data class RfidReaderDevice(
    val id: String,
    val name: String,
    val detail: String,
    val signalLabel: String,
)

/**
 * Port for the RFID reader. The V1 adapter is [KeyboardWedgeRfidReader]; a vendor
 * BLE/SDK adapter can implement the same port later without touching feature code.
 * Feature/UI modules never import Bluetooth/InputManager directly — they consume this.
 */
interface RfidReaderPort {
    val status: StateFlow<RfidReaderStatus>
    val reads: SharedFlow<RfidRead>
    val readerName: StateFlow<String?>
    val devices: StateFlow<List<RfidReaderDevice>>

    /** Recompute readiness (call on resume / input-device or Bluetooth change). */
    fun refreshStatus()

    /** Open the Android Bluetooth settings/pairing flow — Android owns HID connection. */
    fun openSystemPairing()

    /** Route gating: only capture hardware key events while an RFID-accepting screen is active. */
    fun setCaptureEnabled(enabled: Boolean)

    /** Route gating: while on Scan, RFID Enter/Tab must never fall through to focused UI controls. */
    fun setCompletionKeySwallowEnabled(enabled: Boolean)

    /** Feed a hardware key event; returns true if consumed as (part of) a tag read. */
    fun onKeyEvent(event: KeyEvent): Boolean
}
