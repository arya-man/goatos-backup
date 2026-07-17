package sg.mesha.goatos

// telemetry:exempt debug-only hardware harness; product telemetry is wired in the real ViewModels.

import android.Manifest
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothManager
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanResult
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import sg.mesha.goatos.feature.profile.RfidConnectionState
import sg.mesha.goatos.feature.profile.RfidDetailStatus
import sg.mesha.goatos.feature.profile.RfidEvent
import sg.mesha.goatos.feature.profile.RfidReaderRow
import sg.mesha.goatos.feature.profile.RfidScreen
import sg.mesha.goatos.feature.profile.RfidUiState
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanError
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanFeedEntry
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.scan.ScanTileLabels
import sg.mesha.goatos.feature.scan.ScanUiState
import sg.mesha.goatos.feature.scan.VaccineGroup
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

data class BleScanDevice(
    val address: String,
    val name: String,
    val rssi: Int,
    val bonded: Boolean,
)

class BleRfidScanner(context: Context) {
    private val context = context.applicationContext
    private val bluetoothManager = context.getSystemService(Context.BLUETOOTH_SERVICE) as? BluetoothManager
    private val adapter get() = bluetoothManager?.adapter
    private val _devices = MutableStateFlow<List<BleScanDevice>>(emptyList())
    val devices: StateFlow<List<BleScanDevice>> = _devices.asStateFlow()

    private val seen = linkedMapOf<String, BleScanDevice>()
    private var callback: ScanCallback? = null
    private var receiverRegistered = false
    private val discoveryReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.action != BluetoothDevice.ACTION_FOUND) return
            val device = intent.bluetoothDeviceExtra() ?: return
            addClassic(device)
        }
    }

    fun restartScan() {
        stopScan()
        seen.clear()
        _devices.value = emptyList()
        val bluetoothAdapter = adapter ?: return
        if (!hasScanPermission()) return
        if (!receiverRegistered) {
            val filter = IntentFilter(BluetoothDevice.ACTION_FOUND)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                context.registerReceiver(discoveryReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                @Suppress("DEPRECATION")
                context.registerReceiver(discoveryReceiver, filter)
            }
            receiverRegistered = true
        }
        runCatching {
            if (bluetoothAdapter.isDiscovering) bluetoothAdapter.cancelDiscovery()
            bluetoothAdapter.startDiscovery()
        }
        val scanner = bluetoothAdapter.bluetoothLeScanner ?: return
        val scanCallback = object : ScanCallback() {
            override fun onScanResult(callbackType: Int, result: ScanResult) {
                add(result)
            }

            override fun onBatchScanResults(results: MutableList<ScanResult>) {
                results.forEach(::add)
            }
        }
        callback = scanCallback
        runCatching { scanner.startScan(scanCallback) }
    }

    fun stopScan() {
        val scanCallback = callback
        callback = null
        if (hasScanPermission()) {
            if (scanCallback != null) {
                runCatching { adapter?.bluetoothLeScanner?.stopScan(scanCallback) }
            }
            runCatching { adapter?.cancelDiscovery() }
        }
    }

    fun pair(address: String) {
        if (!hasConnectPermission()) return
        val device = runCatching { adapter?.getRemoteDevice(address) }.getOrNull() ?: return
        runCatching { device.createBond() }
    }

    private fun add(result: ScanResult) {
        if (!hasConnectPermission()) return
        val device = result.device ?: return
        val address = runCatching { device.address }.getOrNull() ?: return
        val name = result.scanRecord?.deviceName
            ?: runCatching { device.name }.getOrNull()
            ?: "Unknown BLE device"
        seen[address] = BleScanDevice(
            address = address,
            name = name,
            rssi = result.rssi,
            bonded = device.bondState == BluetoothDevice.BOND_BONDED,
        )
        _devices.value = seen.values.sortedByDescending { it.rssi }
    }

    private fun addClassic(device: BluetoothDevice) {
        if (!hasConnectPermission()) return
        val address = runCatching { device.address }.getOrNull() ?: return
        val name = runCatching { device.name }.getOrNull() ?: "Unknown Bluetooth device"
        seen[address] = BleScanDevice(
            address = address,
            name = name,
            rssi = seen[address]?.rssi ?: -60,
            bonded = device.bondState == BluetoothDevice.BOND_BONDED,
        )
        _devices.value = seen.values.sortedWith(
            compareByDescending<BleScanDevice> { it.bonded }.thenByDescending { it.rssi },
        )
    }

    private fun Intent.bluetoothDeviceExtra(): BluetoothDevice? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            getParcelableExtra(BluetoothDevice.EXTRA_DEVICE, BluetoothDevice::class.java)
        } else {
            @Suppress("DEPRECATION")
            getParcelableExtra(BluetoothDevice.EXTRA_DEVICE)
        }

    private fun hasScanPermission(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.S ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH_SCAN) == PackageManager.PERMISSION_GRANTED

    private fun hasConnectPermission(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.S ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.BLUETOOTH_CONNECT) == PackageManager.PERMISSION_GRANTED
}

@Composable
fun BleRfidScannerScreen(
    scanner: BleRfidScanner,
    hidReader: RfidReaderPort,
    openBluetoothSettings: () -> Unit,
) {
    var mode by remember { mutableStateOf(RfidDebugMode.CONNECT) }
    var discoveryRequested by remember { mutableStateOf(false) }
    val bleDevices by scanner.devices.collectAsStateWithLifecycle()
    val hidStatus by hidReader.status.collectAsStateWithLifecycle()
    val hidReaderName by hidReader.readerName.collectAsStateWithLifecycle()
    val hidDevices by hidReader.devices.collectAsStateWithLifecycle()
    val activeHidDevices = hidDevices.filter {
        it.signalLabel.equals("Ready", ignoreCase = true) ||
            it.signalLabel.equals("Connected", ignoreCase = true)
    }
    val isHidConnected = hidStatus == RfidReaderStatus.READY
    val hasKnownHidReader = hidDevices.isNotEmpty()
    val showDiscovery = discoveryRequested || !hasKnownHidReader

    DisposableEffect(scanner, hidReader) {
        hidReader.refreshStatus()
        scanner.restartScan()
        onDispose { scanner.stopScan() }
    }
    LaunchedEffect(hidReader) {
        while (true) {
            hidReader.refreshStatus()
            delay(1_000)
        }
    }

    if (mode == RfidDebugMode.SCAN) {
        RfidDrivenVaccinationScanScreen(
            reader = hidReader,
            onReconnect = {
                mode = RfidDebugMode.CONNECT
                discoveryRequested = true
                hidReader.refreshStatus()
                scanner.restartScan()
            },
        )
        return
    }

    fun rescanReaders() {
        discoveryRequested = true
        hidReader.refreshStatus()
        scanner.restartScan()
    }

    val hidRows = hidDevices.map { device ->
        RfidReaderRow(
            id = device.id,
            name = device.name,
            detail = device.detail,
            signalLabel = device.signalLabel,
        )
    }
    val bleRows = bleDevices
        .filterNot { ble ->
            hidDevices.any { hid ->
                hid.id == "bt-${ble.address}" || hid.name.equals(ble.name, ignoreCase = true)
            }
        }
        .map { device ->
            RfidReaderRow(
                id = device.address,
                name = device.name,
                detail = device.address,
                signalLabel = if (device.bonded) "Paired" else "${device.rssi} dBm",
            )
        }
    val rows = when {
        isHidConnected -> activeHidDevices.map { device ->
            RfidReaderRow(
                id = device.id,
                name = device.name,
                detail = "Connected keyboard input",
                signalLabel = "Ready",
            )
        }
        showDiscovery -> hidRows + bleRows
        else -> hidRows
    }

    RfidScreen(
        state = RfidUiState(
            detailStatus = when {
                isHidConnected -> RfidDetailStatus.READY
                hasKnownHidReader -> RfidDetailStatus.PAIRED_NOT_READY
                else -> RfidDetailStatus.NOT_PAIRED
            },
            connectionState = when {
                isHidConnected -> RfidConnectionState.CONNECTED
                hasKnownHidReader -> RfidConnectionState.DISCONNECTED
                else -> RfidConnectionState.SCANNING
            },
            readerName = when {
                isHidConnected || hasKnownHidReader -> hidReaderName ?: "RFID reader"
                else -> "Scanning BLE devices"
            },
            pairedLabel = when {
                isHidConnected -> "Connected keyboard"
                hasKnownHidReader && showDiscovery -> "Disconnected · scanning"
                hasKnownHidReader -> "Disconnected"
                else -> "${bleDevices.size} found"
            },
            testReadValue = "Tap a device to pair",
            showHidNote = false,
            showTestRead = false,
            primaryActionLabel = if (isHidConnected) "Done" else null,
            showBluetoothAction = !isHidConnected,
            discovered = rows,
        ),
        onEvent = { event ->
            when (event) {
                RfidEvent.TestRead -> {
                    if (isHidConnected) {
                        mode = RfidDebugMode.SCAN
                    } else {
                        rescanReaders()
                    }
                }
                RfidEvent.Pair,
                RfidEvent.Disconnect -> openBluetoothSettings()
                is RfidEvent.SelectReader -> {
                    if (event.id.startsWith("bt-") || event.id.startsWith("input-")) {
                        hidReader.refreshStatus()
                        discoveryRequested = false
                    } else {
                        scanner.pair(event.id)
                        hidReader.refreshStatus()
                        discoveryRequested = false
                    }
                }
            }
        },
    )
}

private enum class RfidDebugMode { CONNECT, SCAN }

private data class DebugTagRead(val tag: String, val accepted: Boolean) {
    val normalizedTag: String = normalizeRfidTag(tag)
}

@Composable
private fun RfidDrivenVaccinationScanScreen(
    reader: RfidReaderPort,
    onReconnect: () -> Unit,
) {
    var reads by remember { mutableStateOf<List<DebugTagRead>>(emptyList()) }
    var duplicateTag by remember { mutableStateOf<String?>(null) }
    var selectedFilter by remember { mutableStateOf<ScanStatus?>(null) }
    var rosterExpanded by remember { mutableStateOf(false) }
    val status by reader.status.collectAsStateWithLifecycle()
    val readerName by reader.readerName.collectAsStateWithLifecycle()

    DisposableEffect(reader) {
        reader.setCaptureEnabled(true)
        onDispose { reader.setCaptureEnabled(false) }
    }
    LaunchedEffect(reader) {
        reader.reads.collect { read ->
            val tag = read.tag.trim()
            val normalizedTag = normalizeRfidTag(tag)
            if (normalizedTag.isNotBlank()) {
                if (reads.any { it.normalizedTag == normalizedTag }) {
                    duplicateTag = tag
                } else {
                    duplicateTag = null
                    reads = listOf(DebugTagRead(tag = tag, accepted = normalizedTag in sampleGreenRfids)) + reads
                }
            }
        }
    }

    ScanScreen(
        state = vaccinationScanStateForReads(
            reads = reads,
            selectedFilter = selectedFilter,
            rosterExpanded = rosterExpanded,
            readerConnection = status.toScanReaderConnection(readerName),
            duplicateTag = duplicateTag,
        ),
        onEvent = { event ->
            when (event) {
                ScanEvent.Back -> Unit
                ScanEvent.Tap -> reader.refreshStatus()
                ScanEvent.OpenList -> rosterExpanded = !rosterExpanded
                is ScanEvent.OpenTile -> selectedFilter = if (selectedFilter == event.status) null else event.status
                ScanEvent.Submit -> Unit
                ScanEvent.LoadMore -> Unit
                ScanEvent.ReconnectReader -> onReconnect()
                is ScanEvent.SelectGroup -> Unit
            }
        },
    )
}

@Composable
fun DebugVaccinationRfidScanScreen(reader: RfidReaderPort) {
    RfidDrivenVaccinationScanScreen(
        reader = reader,
        onReconnect = { reader.refreshStatus() },
    )
}

private fun vaccinationScanStateForReads(
    reads: List<DebugTagRead>,
    selectedFilter: ScanStatus?,
    rosterExpanded: Boolean,
    readerConnection: ScanReaderConnection,
    duplicateTag: String?,
): ScanUiState {
    val acceptedTags = reads.filter { it.accepted }.map { it.normalizedTag }.toSet()
    val rejectedReads = reads.filterNot { it.accepted }
    val rejectedTags = rejectedReads.map { it.normalizedTag }.toSet()
    val done = acceptedTags.size
    val total = sampleRosterRfids.size
    val skipped = rejectedTags.size
    val pending = sampleRosterRfids.count { it !in acceptedTags && it !in rejectedTags }
    val feed = reads.take(12).map { read ->
        ScanFeedEntry(
            primaryTag = read.tag,
            secondaryTag = null,
            vaccineLabel = if (read.accepted) "FMD + HS · due" else "not due in this shed",
            status = if (read.accepted) ScanStatus.DONE else ScanStatus.SKIPPED,
        )
    }
    val knownRows = sampleRosterRfids.map { tag ->
        RosterRow(
            primaryTag = tag,
            secondaryTag = null,
            vaccineLabel = "FMD + HS · due",
            status = when {
                normalizeRfidTag(tag) in rejectedTags -> ScanStatus.SKIPPED
                normalizeRfidTag(tag) in acceptedTags -> ScanStatus.DONE
                else -> ScanStatus.PENDING
            },
            unsynced = normalizeRfidTag(tag) in acceptedTags || normalizeRfidTag(tag) in rejectedTags,
        )
    }
    val rejectedRows = rejectedReads.distinctBy { it.normalizedTag }.filterNot { it.normalizedTag in sampleRosterRfids }.map { read ->
        RosterRow(
            primaryTag = read.tag,
            secondaryTag = null,
            vaccineLabel = "not due here",
            status = ScanStatus.SKIPPED,
            unsynced = true,
        )
    }
    return ScanUiState(
        shedLabel = "Vaccination · RFID test shed",
        cohortLabel = "Operator scan",
        ringDone = done,
        ringTotal = total,
        ringUnitLabel = "vaccinated",
        tapHint = duplicateTag?.let { "Already scanned $it - not added again" }
            ?: "Scan RFID tag - known tags turn green, unknown tags turn red",
        vaccineGroups = listOf(VaccineGroup("fmd-hs", "FMD + HS", done = done, due = total, active = true)),
        doneCount = done,
        pendingCount = pending,
        skippedCount = skipped,
        tileLabels = ScanTileLabels("Done", "Pending", "Skipped"),
        feed = feed,
        roster = rejectedRows + knownRows,
        listTitle = "Tap Done · Pending · Skipped to see the animals",
        submitLabel = if (pending == 0 && skipped == 0) "Submit shed record" else "Vaccinate all $total ($done/$total)",
        canSubmit = pending == 0,
        scanEnabled = true,
        error = null,
        footNote = "eligible → green + buzz + tone · not due → red + double buzz + alert tone",
        isRefreshing = false,
        lastSyncedAt = System.currentTimeMillis(),
        isOffline = false,
        selectedFilter = selectedFilter,
        rosterExpanded = rosterExpanded,
        readerConnection = readerConnection,
    )
}

private fun RfidReaderStatus.toScanReaderConnection(readerName: String?): ScanReaderConnection =
    ScanReaderConnection(
        readerName = readerName ?: "RFID reader",
        statusLabel = when (this) {
            RfidReaderStatus.READY -> "Reader connected"
            RfidReaderStatus.PAIRED_NOT_READY -> "Reader disconnected"
            RfidReaderStatus.NOT_PAIRED -> "Reader not paired"
            RfidReaderStatus.PERMISSION_NEEDED -> "Bluetooth permission needed"
            RfidReaderStatus.BLUETOOTH_OFF -> "Bluetooth off"
        },
        connected = this == RfidReaderStatus.READY,
        actionLabel = "Reconnect",
    )

private val sampleAcceptedRfids = setOf(
    "901007000504392",
    "901007000504418",
    "901007000504407",
    "901007000504419",
    "901007000504332",
)

private val sampleSkippedRfids = setOf("901007000504407")
private val sampleRosterRfids = sampleAcceptedRfids.toList()
private val sampleGreenRfids = sampleAcceptedRfids - sampleSkippedRfids
private fun normalizeRfidTag(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()

@Composable
fun StandaloneHidRfidScreen(reader: RfidReaderPort) {
    val status by reader.status.collectAsStateWithLifecycle()
    val readerName by reader.readerName.collectAsStateWithLifecycle()
    val devices by reader.devices.collectAsStateWithLifecycle()
    var lastRead by remember { mutableStateOf("") }

    DisposableEffect(reader) {
        reader.setCaptureEnabled(true)
        reader.refreshStatus()
        onDispose { reader.setCaptureEnabled(false) }
    }
    LaunchedEffect(reader) {
        reader.reads.collect { read ->
            lastRead = read.tag
            reader.refreshStatus()
        }
    }

    RfidScreen(
        state = status.toUiState(
            readerName = readerName,
            devices = devices,
            lastRead = lastRead,
        ),
        onEvent = { event ->
            when (event) {
                RfidEvent.Pair,
                RfidEvent.Disconnect -> reader.openSystemPairing()
                RfidEvent.TestRead,
                is RfidEvent.SelectReader -> reader.refreshStatus()
            }
        },
    )
}

private fun RfidReaderStatus.toUiState(
    readerName: String?,
    devices: List<RfidReaderDevice>,
    lastRead: String,
): RfidUiState {
    val (detailStatus, connectionState) = when (this) {
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
        testReadValue = lastRead.ifBlank { "Scan a tag" },
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
