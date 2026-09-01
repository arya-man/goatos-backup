package sg.mesha.goatos.rfid

import android.view.KeyEvent
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class RfidKeyboardCaptureTest {

    @Test
    fun `enabled capture buffers scanner text and emits on enter`() = runTest {
        var nowMs = 1_000L
        val capture = RfidKeyboardCapture(nowMs = { nowMs })
        val reads = mutableListOf<RfidRead>()
        val collector = launch(UnconfinedTestDispatcher(testScheduler)) {
            capture.reads.toList(reads)
        }

        capture.enabled = true
        keyEventsFor("901").forEach { event ->
            nowMs += 10L
            assertTrue(capture.onKeyEvent(event))
        }
        nowMs += 10L
        assertTrue(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_ENTER, nowMs)))

        assertEquals(listOf("901"), reads.map { it.tag })
        collector.cancel()
    }

    @Test
    fun `scan route can swallow ble enter while capture is temporarily disabled`() = runTest {
        var nowMs = 2_000L
        val capture = RfidKeyboardCapture(nowMs = { nowMs })
        val reads = mutableListOf<RfidRead>()
        val collector = launch(UnconfinedTestDispatcher(testScheduler)) {
            capture.reads.toList(reads)
        }

        capture.enabled = false
        capture.swallowCompletionKeys = true

        assertFalse(capture.onKeyEvent(keyEventsFor("9").single()))
        assertTrue(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_ENTER, nowMs)))
        assertTrue(capture.onKeyEvent(keyUp(KeyEvent.KEYCODE_ENTER, nowMs)))
        assertTrue(reads.isEmpty())
        collector.cancel()
    }

    @Test
    fun `scan emits full text even when the reader pauses longer than old timeout`() = runTest {
        var nowMs = 4_000L
        val capture = RfidKeyboardCapture(nowMs = { nowMs })
        val reads = mutableListOf<RfidRead>()
        val collector = launch(UnconfinedTestDispatcher(testScheduler)) {
            capture.reads.toList(reads)
        }

        capture.enabled = true
        keyEventsFor("901").forEach { event ->
            nowMs += 10L
            assertTrue(capture.onKeyEvent(event))
        }
        nowMs += 701L
        keyEventsFor("007000504518").forEach { event ->
            nowMs += 10L
            assertTrue(capture.onKeyEvent(event))
        }
        nowMs += 10L
        assertTrue(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_ENTER, nowMs)))

        assertEquals(listOf("901007000504518"), reads.map { it.tag })
        collector.cancel()
    }

    @Test
    fun `completed scans are separated by enter even after a long idle gap`() = runTest {
        var nowMs = 5_000L
        val capture = RfidKeyboardCapture(nowMs = { nowMs })
        val reads = mutableListOf<RfidRead>()
        val collector = launch(UnconfinedTestDispatcher(testScheduler)) {
            capture.reads.toList(reads)
        }

        capture.enabled = true
        keyEventsFor("901").forEach { event ->
            nowMs += 10L
            assertTrue(capture.onKeyEvent(event))
        }
        nowMs += 10L
        assertTrue(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_ENTER, nowMs)))

        nowMs += 5_000L
        keyEventsFor("901007000504518").forEach { event ->
            nowMs += 10L
            assertTrue(capture.onKeyEvent(event))
        }
        nowMs += 10L
        assertTrue(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_ENTER, nowMs)))

        assertEquals(listOf("901", "901007000504518"), reads.map { it.tag })
        collector.cancel()
    }

    @Test
    fun `non scan route does not swallow enter or tab while capture is disabled`() = runTest {
        var nowMs = 3_000L
        val capture = RfidKeyboardCapture(nowMs = { nowMs })

        capture.enabled = false
        capture.swallowCompletionKeys = false

        assertFalse(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_ENTER, nowMs)))
        assertFalse(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_TAB, nowMs)))
    }

    @Test
    fun `back key always follows normal navigation path`() = runTest {
        var nowMs = 6_000L
        val capture = RfidKeyboardCapture(nowMs = { nowMs })

        capture.enabled = true
        capture.swallowCompletionKeys = true

        assertFalse(capture.onKeyEvent(keyDown(KeyEvent.KEYCODE_BACK, nowMs)))
        assertFalse(capture.onKeyEvent(keyUp(KeyEvent.KEYCODE_BACK, nowMs)))
    }

    private fun keyEventsFor(text: String): List<KeyEvent> =
        text.map { char ->
            val keyCode = when (char) {
                in '0'..'9' -> KeyEvent.KEYCODE_0 + (char - '0')
                in 'a'..'z' -> KeyEvent.KEYCODE_A + (char - 'a')
                in 'A'..'Z' -> KeyEvent.KEYCODE_A + (char - 'A')
                else -> error("unsupported test key: $char")
            }
            KeyEvent(KeyEvent.ACTION_DOWN, keyCode)
        }

    private fun keyDown(keyCode: Int, timeMs: Long): KeyEvent =
        KeyEvent(timeMs, timeMs, KeyEvent.ACTION_DOWN, keyCode, 0)

    private fun keyUp(keyCode: Int, timeMs: Long): KeyEvent =
        KeyEvent(timeMs, timeMs, KeyEvent.ACTION_UP, keyCode, 0)
}
