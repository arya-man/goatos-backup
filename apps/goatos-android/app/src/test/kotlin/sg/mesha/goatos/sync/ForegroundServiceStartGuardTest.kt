package sg.mesha.goatos.sync

import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class ForegroundServiceStartGuardTest {
    @Test
    fun `boot restricted foreground promotion defers without crashing`() {
        val rejected = RuntimeException("FGS type dataSync not allowed to start from BOOT_COMPLETED")
        var reported: RuntimeException? = null

        val started = startForegroundOrDefer(
            promote = { throw rejected },
            isStartNotAllowed = { it === rejected },
            onStartNotAllowed = { reported = it },
        )

        assertFalse(started)
        assertSame(rejected, reported)
    }

    @Test
    fun `successful foreground promotion continues service work`() {
        var promoted = false

        val started = startForegroundOrDefer(
            promote = { promoted = true },
            isStartNotAllowed = { false },
            onStartNotAllowed = { error("unexpected rejection") },
        )

        assertTrue(started)
        assertTrue(promoted)
    }

    @Test(expected = SecurityException::class)
    fun `unexpected foreground promotion failures remain fatal configuration errors`() {
        startForegroundOrDefer(
            promote = { throw SecurityException("missing foreground service permission") },
            isStartNotAllowed = { false },
            onStartNotAllowed = { error("unexpected rejection") },
        )
    }
}
