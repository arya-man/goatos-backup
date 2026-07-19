package sg.mesha.goatos.rfid

import android.Manifest
import android.annotation.SuppressLint
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
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
    context: Context,
    private val nameHints: List<String> = DEFAULT_HINTS,
) : RfidReaderPort {

    private val context = context.applicationContext
    private val capture = RfidKeyboardCapture()
    private val _status = MutableStateFlow(RfidReaderStatus.NOT_PAIRED)
    private val _readerName = MutableStateFlow<String?>(null)
    private val _devices = MutableStateFlow<List<RfidReaderDevice>>(emptyList())
    private val connectedBtAddresses = mutableSetOf<String>()
    private val disconnectedBtAddresses = mutableSetOf<String>()
    private val inputDeviceListener = object : InputManager.InputDeviceListener {
        override fun onInputDeviceAdded(deviceId: Int) {
            refreshStatus()
        }

        override fun onInputDeviceRemoved(deviceId: Int) {
            refreshStatus()
        }

        override fun onInputDeviceChanged(deviceId: Int) {
            refreshStatus()
        }
    }
    private val bluetoothReceiver = object : BroadcastReceiver() {
        // Lint cannot infer [needsBluetoothConnectPermission]; the explicit runtime gate makes
        // every protected BluetoothDevice access below unreachable without BLUETOOTH_CONNECT.
        @SuppressLint("MissingPermission")
        override fun onReceive(context: Context?, intent: Intent?) {
            if (needsBluetoothConnectPermission()) {
                refreshStatus()
                return
            }
            val action = intent?.action ?: return
            val device = intent.bluetoothDeviceExtra() ?: return
            val name = runCatching { device.name }.getOrNull()
            if (!matchesHint(name)) return
            val address = runCatching { device.address }.getOrNull() ?: return
            synchronized(this@KeyboardWedgeRfidReader) {
                when (action) {
                    BluetoothDevice.ACTION_ACL_CONNECTED -> {
                        disconnectedBtAddresses.remove(address)
                        connectedBtAddresses.add(address)
                    }
                    BluetoothDevice.ACTION_ACL_DISCONNECTED -> {
                        connectedBtAddresses.remove(address)
                        disconnectedBtAddresses.add(address)
                    }
                    BluetoothDevice.ACTION_BOND_STATE_CHANGED -> {
                        if (device.bondState != BluetoothDevice.BOND_BONDED) {
                            connectedBtAddresses.remove(address)
                            disconnectedBtAddresses.remove(address)
                        }
                    }
                }
            }
            refreshStatus()
        }
    }

    override val status: StateFlow<RfidReaderStatus> = _status.asStateFlow()
    override val readerName: StateFlow<String?> = _readerName.asStateFlow()
    override val devices: StateFlow<List<RfidReaderDevice>> = _devices.asStateFlow()
    override val reads: SharedFlow<RfidRead> = capture.reads

    init {
        registerBluetoothReceiver()
        registerInputDeviceListener()
        refreshStatus()
    }

    override fun onKeyEvent(event: KeyEvent): Boolean = capture.onKeyEvent(event)

    override fun setCaptureEnabled(enabled: Boolean) {
        capture.enabled = enabled
        if (enabled) refreshStatus()
    }

    override fun refreshStatus() {
        val visibleDevices = findVisibleDevices()
        _devices.value = visibleDevices
        _readerName.value = visibleDevices.firstOrNull { matchesHint(it.name) }?.name
            ?: visibleDevices.firstOrNull { it.signalLabel == "Ready" }?.name
            ?: visibleDevices.firstOrNull()?.name
        _status.value = computeStatus(visibleDevices)
    }

    private fun computeStatus(visibleDevices: List<RfidReaderDevice>): RfidReaderStatus {
        if (visibleDevices.any { it.signalLabel == "Ready" }) return RfidReaderStatus.READY
        val adapter = (context.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter
            ?: return RfidReaderStatus.NOT_PAIRED
        if (!adapter.isEnabled) return RfidReaderStatus.BLUETOOTH_OFF
        if (needsBluetoothConnectPermission()) return RfidReaderStatus.PERMISSION_NEEDED
        return if (visibleDevices.isNotEmpty()) RfidReaderStatus.PAIRED_NOT_READY else RfidReaderStatus.NOT_PAIRED
    }

    private fun findVisibleDevices(): List<RfidReaderDevice> {
        val pairedDevices = findBondedBluetoothDevices()
        val disconnectedNames = pairedDevices
            .filter { it.signalLabel == "Disconnected" }
            .map { it.name.lowercase() }
            .toSet()
        val inputDevices = findReaderInputDevices(disconnectedNames)
        return (inputDevices + pairedDevices)
            .distinctBy { it.name.lowercase() }
            .sortedWith(compareByDescending<RfidReaderDevice> { it.signalLabel == "Ready" }.thenBy { it.name })
    }

    private fun findReaderInputDevices(disconnectedNames: Set<String>): List<RfidReaderDevice> {
        val im = context.getSystemService(Context.INPUT_SERVICE) as? InputManager ?: return emptyList()
        return im.inputDeviceIds.asSequence().mapNotNull { id ->
            val device = im.getInputDevice(id) ?: return@mapNotNull null
            val isKeyboard = device.sources and InputDevice.SOURCE_KEYBOARD == InputDevice.SOURCE_KEYBOARD
            if (!isKeyboard || device.isVirtual || !matchesHint(device.name)) return@mapNotNull null
            if (device.name.lowercase() in disconnectedNames) return@mapNotNull null
            RfidReaderDevice(
                id = "input-$id",
                name = device.name,
                detail = "Active keyboard input",
                signalLabel = "Ready",
            )
        }.toList()
    }

    // Lint cannot infer the custom permission predicate; keep the suppression scoped to this
    // function and immediately return before touching any protected adapter/device property.
    @SuppressLint("MissingPermission")
    private fun findBondedBluetoothDevices(): List<RfidReaderDevice> {
        val adapter = (context.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager)?.adapter
            ?: return emptyList()
        if (needsBluetoothConnectPermission()) return emptyList()
        return runCatching {
            adapter.bondedDevices.orEmpty()
                .mapNotNull { device ->
                    val name = runCatching { device.name }.getOrNull()?.ifBlank { null } ?: return@mapNotNull null
                    if (!matchesHint(name)) return@mapNotNull null
                    val address = runCatching { device.address }.getOrNull() ?: name
                    val connected = device.currentConnectionState(address)
                    RfidReaderDevice(
                        id = "bt-$address",
                        name = name,
                        detail = when (connected) {
                            true -> "Connected Bluetooth keyboard"
                            false -> "Paired · not connected"
                            null -> "Paired Bluetooth device"
                        },
                        signalLabel = when (connected) {
                            true -> "Ready"
                            false -> "Disconnected"
                            null -> "Paired"
                        },
                    )
                }
        }.getOrDefault(emptyList())
    }

    private fun BluetoothDevice.currentConnectionState(address: String): Boolean? {
        val reflected = runCatching {
            javaClass.getMethod("isConnected").invoke(this) as? Boolean
        }.getOrNull()
        if (reflected != null) {
            synchronized(this@KeyboardWedgeRfidReader) {
                if (reflected) {
                    disconnectedBtAddresses.remove(address)
                    connectedBtAddresses.add(address)
                } else if (address in connectedBtAddresses) {
                    connectedBtAddresses.remove(address)
                    disconnectedBtAddresses.add(address)
                }
            }
            return reflected
        }
        synchronized(this@KeyboardWedgeRfidReader) {
            if (address in connectedBtAddresses) return true
            if (address in disconnectedBtAddresses) return false
        }
        return null
    }

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

    private fun registerBluetoothReceiver() {
        val filter = IntentFilter().apply {
            addAction(BluetoothDevice.ACTION_ACL_CONNECTED)
            addAction(BluetoothDevice.ACTION_ACL_DISCONNECTED)
            addAction(BluetoothDevice.ACTION_BOND_STATE_CHANGED)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            context.registerReceiver(bluetoothReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("DEPRECATION")
            context.registerReceiver(bluetoothReceiver, filter)
        }
    }

    private fun registerInputDeviceListener() {
        val inputManager = context.getSystemService(Context.INPUT_SERVICE) as? InputManager ?: return
        inputManager.registerInputDeviceListener(inputDeviceListener, null)
    }

    private fun Intent.bluetoothDeviceExtra(): BluetoothDevice? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
        } else {
            @Suppress("DEPRECATION")
            getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
        }

    private companion object {
        // Name/vendor hints for known field readers (e.g. "IDT RHLS-3", "Chainway R3").
        val DEFAULT_HINTS = listOf("rfid", "reader", "idt", "rhls", "chainway", "r3")
    }
}
