package sg.mesha.goatos.rfid

import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.map

/**
 * Port for the Submit recording-form's `goat_scan` capture (MOB-002,
 * docs/mobile/proof-capture-sync-and-e2e.md §1). One completed tag per emission — the
 * transport (hardware BT-HID keys vs a test double) is swappable and testable behind this
 * interface, mirroring [RfidReaderPort]'s existing production/fake split.
 */
interface ScanSource {
    /** One completed tag id per scan, only while [start] is active. */
    val tags: Flow<String>

    /** Begin accepting scans (production: enables hardware key-event capture). */
    fun start()

    /** Stop accepting scans (production: disables hardware key-event capture so key events
     *  stop being consumed once the scan control is no longer active). */
    fun stop()
}

/**
 * Production [ScanSource]: wraps the already-wired [RfidReaderPort] (BT-HID keyboard-wedge,
 * see `RfidKeyboardCapture`) rather than re-implementing key-event buffering. No vendor SDK,
 * no `EditText` — the port only toggles capture + re-maps [RfidReaderPort.reads] to tag
 * strings for the Submit form's `goat_scan` field.
 */
class BtHidScanSource(private val reader: RfidReaderPort) : ScanSource {
    override val tags: Flow<String> = reader.reads.map { it.tag }

    override fun start() {
        reader.setCaptureEnabled(true)
    }

    override fun stop() {
        reader.setCaptureEnabled(false)
    }
}

/** Test double: feed tags with [emit]; [start]/[stop] just track call counts for assertions. */
class FakeScanSource : ScanSource {
    private val _tags = MutableSharedFlow<String>(
        extraBufferCapacity = 64,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )
    override val tags: Flow<String> = _tags.asSharedFlow()

    var startCount: Int = 0
        private set
    var stopCount: Int = 0
        private set
    val isStarted: Boolean get() = startCount > stopCount

    override fun start() {
        startCount++
    }

    override fun stop() {
        stopCount++
    }

    /** Test-only: emits a completed tag read as if the hardware reader had produced it. */
    suspend fun emit(tag: String) {
        _tags.emit(tag)
    }
}
