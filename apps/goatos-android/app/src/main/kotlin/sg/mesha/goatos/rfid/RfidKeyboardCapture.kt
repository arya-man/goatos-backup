package sg.mesha.goatos.rfid

import android.view.KeyEvent
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow

/**
 * Buffers hardware key events from the keyboard-wedge reader into a tag string, and
 * completes a read on Enter/Tab. Guards against stray manual keys with a short inter-key
 * timeout. Consumes only tag characters + completion keys while [enabled]; everything else
 * (Back, volume, etc.) passes through. No EditText anywhere — reads come straight from the
 * activity key-event stream (docs/mobile/rfid-keyboard-reader.md §Scan Capture).
 */
class RfidKeyboardCapture(
    private val completionTimeoutMs: Long = 700L,
    private val nowMs: () -> Long = { System.currentTimeMillis() },
) {
    private val _reads = MutableSharedFlow<RfidRead>(extraBufferCapacity = 16)
    val reads: SharedFlow<RfidRead> = _reads.asSharedFlow()

    @Volatile
    var enabled: Boolean = false

    private val buffer = StringBuilder()
    private var lastKeyAtMs: Long = 0L
    private var deviceName: String? = null

    /** Returns true if the event was consumed as tag input (caller must not pass it on). */
    fun onKeyEvent(event: KeyEvent): Boolean {
        if (!enabled) return false
        val completion = isCompletionKey(event)
        val tagChar = event.unicodeChar != 0 && Character.isLetterOrDigit(event.unicodeChar)
        if (!completion && !tagChar) return false // let non-tag keys through
        if (event.action != KeyEvent.ACTION_DOWN) return true // swallow the UP of a consumed key

        val now = nowMs()
        if (now - lastKeyAtMs > completionTimeoutMs) buffer.setLength(0)
        lastKeyAtMs = now

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
