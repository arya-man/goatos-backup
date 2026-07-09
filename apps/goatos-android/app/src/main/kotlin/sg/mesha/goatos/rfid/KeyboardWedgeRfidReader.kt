package sg.mesha.goatos.rfid

import android.Manifest
import android.bluetooth.BluetoothManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.hardware.input.InputManager
import android.os.Build
import android.provider.Settings
import android.view.InputDevice
import android.view.KeyEvent
import androidx.core.content.ContextCompat
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * V1 RFID reader: a Bluetooth HID keyboard-wedge. Readiness is derived from
 * [InputManager] (is an external reader present as an input device?) plus the Bluetooth
 * bonded-device state; tag reads are captured from the activity key stream via
 * [RfidKeyboardCapture]. Android owns HID connection — we only observe + guide the operator
 * to the system pairing flow. See docs/mobile/rfid-keyboard-reader.md.
 */
class KeyboardWedgeRfidReader(
    private val context: Context,
    private val nameHints: List<String> = DEFAULT_HINTS,
) : RfidReaderPort {

    private val capture = RfidKeyboardCapture()
    private val _status = MutableStateFlow(RfidReaderStatus.NOT_PAIRED)
    override val status: StateFlow<RfidReaderStatus> = _status.asStateFlow()
    override val reads: SharedFlow<RfidRead> = capture.reads

    init {
        refreshStatus()
    }

    override fun onKeyEvent(event: KeyEvent): Boolean = capture.onKeyEvent(event)

    override fun setCaptureEnabled(enabled: Boolean) {
        capture.enabled = enabled
        if (enabled) refreshStatus()
    }

    override fun refreshStatus() {
        _status.value = computeStatus()
    }

    private fun computeStatus(): RfidReaderStatus {
        if (hasReaderInputDevice()) return RfidReaderStatus.READY
        val adapter = (context.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter
            ?: return RfidReaderStatus.NOT_PAIRED
        if (!adapter.isEnabled) return RfidReaderStatus.BLUETOOTH_OFF
        if (needsBluetoothConnectPermission()) return RfidReaderStatus.PERMISSION_NEEDED
        return if (hasBondedReader(adapter)) RfidReaderStatus.PAIRED_NOT_READY else RfidReaderStatus.NOT_PAIRED
    }

    private fun hasReaderInputDevice(): Boolean {
        val im = context.getSystemService(Context.INPUT_SERVICE) as? InputManager ?: return false
        return im.inputDeviceIds.any { id ->
            val device = im.getInputDevice(id) ?: return@any false
            val isKeyboard = device.sources and InputDevice.SOURCE_KEYBOARD == InputDevice.SOURCE_KEYBOARD
            isKeyboard && !device.isVirtual && matchesHint(device.name)
        }
    }

    private fun hasBondedReader(adapter: android.bluetooth.BluetoothAdapter): Boolean =
        runCatching { adapter.bondedDevices.orEmpty().any { matchesHint(runCatching { it.name }.getOrNull()) } }
            .getOrDefault(false)

    private fun matchesHint(name: String?): Boolean {
        val lower = name?.lowercase() ?: return false
        return nameHints.any { lower.contains(it) }
    }

    private fun needsBluetoothConnectPermission(): Boolean =
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S &&
            ContextCompat.checkSelfPermission(
                context,
                Manifest.permission.BLUETOOTH_CONNECT,
            ) != PackageManager.PERMISSION_GRANTED

    override fun openSystemPairing() {
        val bt = Intent(Settings.ACTION_BLUETOOTH_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        runCatching { context.startActivity(bt) }.onFailure {
            runCatching {
                context.startActivity(Intent(Settings.ACTION_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            }
        }
    }

    private companion object {
        // Name/vendor hints for known field readers (e.g. "IDT RHLS-3", "Chainway R3").
        val DEFAULT_HINTS = listOf("rfid", "reader", "idt", "rhls", "chainway", "r3")
    }
}
