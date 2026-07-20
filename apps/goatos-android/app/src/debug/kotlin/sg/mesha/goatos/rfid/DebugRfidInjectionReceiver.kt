package sg.mesha.goatos.rfid

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.SystemClock
import android.util.Log
import android.view.KeyCharacterMap
import android.view.KeyEvent
import dagger.hilt.android.AndroidEntryPoint
import javax.inject.Inject

/**
 * Debug-build E2E adapter for a BT-HID keyboard-wedge RFID burst.
 *
 * `adb shell input text` is delivered through the active IME and does not reliably reach an
 * Activity's hardware-key callbacks when no text field is focused. That makes it the wrong way to
 * simulate a keyboard-wedge reader. This receiver instead converts the tag to real [KeyEvent]s and
 * feeds the production [RfidReaderPort] singleton. Capture must already be enabled by the composed
 * scan route, so off-route injection is rejected exactly like stray hardware keys.
 *
 * The receiver is declared only in `src/debug/AndroidManifest.xml`; release APKs cannot inject
 * scans. It exists to keep physical-device/emulator E2E deterministic when RFID hardware is not
 * attached, without adding a production shortcut around Room/outbox/backend processing.
 */
@AndroidEntryPoint
class DebugRfidInjectionReceiver : BroadcastReceiver() {

    @Inject
    lateinit var reader: RfidReaderPort

    override fun onReceive(context: Context, intent: Intent) {
        val tag = intent.getStringExtra(EXTRA_TAG)?.trim().orEmpty()
        if (tag.isBlank() || tag.length > MAX_TAG_LENGTH) {
            resultCode = RESULT_INVALID_TAG
            return
        }

        val keyMap = KeyCharacterMap.load(KeyCharacterMap.VIRTUAL_KEYBOARD)
        val tagEvents = keyMap.getEvents(tag.toCharArray()).orEmpty()
        var consumed = tagEvents.isNotEmpty()
        tagEvents.forEach { event -> consumed = reader.onKeyEvent(event) && consumed }

        val now = SystemClock.uptimeMillis()
        consumed = reader.onKeyEvent(KeyEvent(now, now, KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_ENTER, 0)) && consumed
        consumed = reader.onKeyEvent(KeyEvent(now, now, KeyEvent.ACTION_UP, KeyEvent.KEYCODE_ENTER, 0)) && consumed

        resultCode = if (consumed) RESULT_INJECTED else RESULT_CAPTURE_INACTIVE
        Log.i(TAG, "RFID E2E injection completed: chars=${tag.length} consumed=$consumed")
    }

    companion object {
        const val ACTION = "sg.mesha.goatos.debug.INJECT_RFID"
        const val EXTRA_TAG = "tag"
        const val RESULT_INJECTED = 0
        const val RESULT_INVALID_TAG = 1
        const val RESULT_CAPTURE_INACTIVE = 2
        private const val MAX_TAG_LENGTH = 128
        private const val TAG = "GoatOsDebugRfid"
    }
}
