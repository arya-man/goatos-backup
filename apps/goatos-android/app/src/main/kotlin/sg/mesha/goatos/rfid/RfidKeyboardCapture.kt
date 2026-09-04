package sg.mesha.goatos.rfid

import android.view.KeyEvent
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow

/**
 * Buffers hardware key events from the keyboard-wedge reader into a tag string, and
 * completes a read on Enter/Tab. Consumes only tag characters + completion keys while [enabled]; everything else
 * (Back, volume, etc.) passes through. No EditText anywhere — reads come straight from the
 * activity key-event stream (docs/mobile/rfid-keyboard-reader.md §Scan Capture).
 */
class RfidKeyboardCapture(
    private val nowMs: () -> Long = { System.currentTimeMillis() },
) {
    // DROP_OLDEST (not the default SUSPEND): onKeyEvent runs on the main/input thread and can't
    // suspend, so a full buffer would make tryEmit() silently return false and lose the read. A
    // scanner burst that outruns a slow collector must drop the STALEST queued tag, never the tag
    // just scanned — the newest read is always the one the operator is acting on.
    private val _reads = MutableSharedFlow<RfidRead>(
        extraBufferCapacity = 64,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )
    val reads: SharedFlow<RfidRead> = _reads.asSharedFlow()

    @Volatile
    var enabled: Boolean = false

    @Volatile
    var swallowCompletionKeys: Boolean = false

    private val buffer = StringBuilder()
    private var deviceName: String? = null

    fun resetBufferedRead() {
        buffer.setLength(0)
        deviceName = null
    }

    /** Returns true if the event was consumed as tag input (caller must not pass it on). */
    fun onKeyEvent(event: KeyEvent): Boolean {
        val completion = isCompletionKey(event)
        if (!enabled) return completion && swallowCompletionKeys
        val tagChar = event.unicodeChar != 0 && Character.isLetterOrDigit(event.unicodeChar)
        if (!completion && !tagChar) return false // let non-tag keys through
        if (event.action != KeyEvent.ACTION_DOWN) return true // swallow the UP of a consumed key

        val now = nowMs()

        if (completion) {
            val tag = buffer.toString().trim()
            buffer.setLength(0)
            if (tag.isNotEmpty()) {
                _reads.tryEmit(RfidRead(tag = tag, deviceName = deviceName, capturedAtDeviceMs = now))
            }
        } else {
            buffer.append(event.unicodeChar.toChar())
            deviceName = event.device?.name
        }
        return true
    }

    private fun isCompletionKey(e: KeyEvent): Boolean =
        e.keyCode == KeyEvent.KEYCODE_ENTER ||
            e.keyCode == KeyEvent.KEYCODE_NUMPAD_ENTER ||
            e.keyCode == KeyEvent.KEYCODE_TAB
}
