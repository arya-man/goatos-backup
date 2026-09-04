package sg.mesha.goatos.feature.leadershiptasks

import org.junit.Assert.assertEquals
import org.junit.Test

/** The two numeric labels the screens compose themselves — clocks and sizes, never sentences. */
class LeadershipTaskFormatTest {
    @Test
    fun `clock renders minutes and seconds, and hours only past an hour`() {
        assertEquals("0:00", formatClock(0L))
        assertEquals("0:42", formatClock(42_000L))
        assertEquals("1:05", formatClock(65_000L))
        assertEquals("1:00:01", formatClock(3_601_000L))
        assertEquals("0:00", formatClock(-5L))
    }

    @Test
    fun `bytes render as KB, MB or GB and blank when unknown`() {
        assertEquals("", formatBytes(0L))
        assertEquals("1 KB", formatBytes(512L))
        assertEquals("820 KB", formatBytes(820L * 1024L))
        assertEquals("1.5 MB", formatBytes(1_572_864L))
        assertEquals("2.0 GB", formatBytes(2L * 1024L * 1024L * 1024L))
    }
}
