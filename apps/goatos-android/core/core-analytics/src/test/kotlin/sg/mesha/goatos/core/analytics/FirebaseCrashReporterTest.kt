package sg.mesha.goatos.core.analytics

import java.io.FileNotFoundException
import java.net.SocketException
import java.net.UnknownHostException
import kotlin.coroutines.cancellation.CancellationException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FirebaseCrashReporterTest {

    private class RecordingCrashlyticsSink : CrashlyticsSink {
        val logs = mutableListOf<String>()
        val nonFatals = mutableListOf<Throwable>()
        val keys = mutableMapOf<String, String>()

        override fun recordException(throwable: Throwable) {
            nonFatals += throwable
        }

        override fun log(message: String) {
            logs += message
        }

        override fun setCustomKey(key: String, value: String) {
            keys[key] = value
        }
    }

    @Test
    fun `connectivity exceptions are not mirrored as Crashlytics non-fatals`() {
        val error = UnknownHostException("Unable to resolve host")

        assertTrue(error.hasNetworkConnectivityCause())
    }

    @Test
    fun `connection reset is treated as connectivity noise`() {
        val error = SocketException("Connection reset")

        assertTrue(error.hasNetworkConnectivityCause())
    }

    @Test
    fun `wrapped connectivity exceptions are not mirrored as Crashlytics non-fatals`() {
        val error = RuntimeException("page load failed", UnknownHostException("Network unreachable"))

        assertTrue(error.hasNetworkConnectivityCause())
    }

    @Test
    fun `local io failures remain Crashlytics non-fatals`() {
        val error = FileNotFoundException("export target missing")

        assertFalse(error.hasNetworkConnectivityCause())
    }

    @Test
    fun `client defects remain Crashlytics non-fatals`() {
        val error = IllegalStateException("mapper broke")

        assertFalse(error.hasNetworkConnectivityCause())
    }

    @Test
    fun `coroutine cancellations are not mirrored as Crashlytics non-fatals`() {
        val error = CancellationException("Job was cancelled")

        assertTrue(error.shouldSkipCrashlyticsNonFatal())
    }

    @Test
    fun `raw retrofit http exceptions are not mirrored as duplicate Crashlytics non-fatals`() {
        val error = retrofit2.HttpException("HTTP 429")

        assertTrue(error.shouldSkipCrashlyticsNonFatal())
    }

    @Test
    fun `recordException keeps breadcrumb but skips non-fatal for network connectivity`() {
        val sink = RecordingCrashlyticsSink()
        val reporter = FirebaseCrashReporter(sink)
        val error = UnknownHostException("Unable to resolve host")

        reporter.recordException(error, "feed direction page load failed")

        assertEquals(listOf("feed direction page load failed"), sink.logs)
        assertTrue(sink.nonFatals.isEmpty())
    }

    @Test
    fun `recordException keeps breadcrumb but skips non-fatal for coroutine cancellation`() {
        val sink = RecordingCrashlyticsSink()
        val reporter = FirebaseCrashReporter(sink)
        val error = CancellationException("Job was cancelled")

        reporter.recordException(error, "verify queue refresh failed")

        assertEquals(listOf("verify queue refresh failed"), sink.logs)
        assertTrue(sink.nonFatals.isEmpty())
    }

    @Test
    fun `recordException keeps breadcrumb but skips non-fatal for raw retrofit http exception`() {
        val sink = RecordingCrashlyticsSink()
        val reporter = FirebaseCrashReporter(sink)
        val error = retrofit2.HttpException("HTTP 429")

        reporter.recordException(error, "feed packing page load failed")

        assertEquals(listOf("feed packing page load failed"), sink.logs)
        assertTrue(sink.nonFatals.isEmpty())
    }

    @Test
    fun `recordException sends non-connectivity defects to Crashlytics`() {
        val sink = RecordingCrashlyticsSink()
        val reporter = FirebaseCrashReporter(sink)
        val error = IllegalStateException("mapper broke")

        reporter.recordException(error, "feed direction page load failed")

        assertEquals(listOf("feed direction page load failed"), sink.logs)
        assertEquals(listOf(error), sink.nonFatals)
    }

    @Test
    fun `custom keys still pass through to Crashlytics`() {
        val sink = RecordingCrashlyticsSink()
        val reporter = FirebaseCrashReporter(sink)

        reporter.setCustomKey("app_version", "0.1.34-stg")

        assertEquals(mapOf("app_version" to "0.1.34-stg"), sink.keys)
    }
}
