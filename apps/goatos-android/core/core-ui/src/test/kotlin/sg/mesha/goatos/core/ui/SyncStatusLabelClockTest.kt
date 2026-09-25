package sg.mesha.goatos.core.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * The "Updated just now" / "Offline · updated 3m ago" line on every offline-first screen. On the
 * phone (2026-09-26) the Sales ledger read "just now" and "1m ago" long after the last sync,
 * because the label was read from the clock only when the screen recomposed. The indicator now
 * re-reads the clock when the label would next change; these pin that schedule.
 */
class SyncStatusLabelClockTest {

    private val synced = 1_000_000_000_000L

    @Test
    fun theLabelMovesOnAsTheClockDoes() {
        assertEquals("just now", relativeSyncLabel(synced, synced + 59_000))
        assertEquals("1m ago", relativeSyncLabel(synced, synced + 60_000))
        assertEquals("2m ago", relativeSyncLabel(synced, synced + 130_000))
        assertEquals("1h ago", relativeSyncLabel(synced, synced + 3_600_000))
    }

    @Test
    fun theNextTickLandsExactlyWhereTheLabelChanges() {
        val now = synced + 70_000 // "1m ago"
        val wait = nextSyncLabelChangeInMillis(synced, now)
        assertEquals(50_000L, wait)
        assertEquals("1m ago", relativeSyncLabel(synced, now + wait - 1))
        assertNotEquals(relativeSyncLabel(synced, now), relativeSyncLabel(synced, now + wait))
    }

    @Test
    fun overAnHourTheTickIsHourlyAndOverADayDaily() {
        assertEquals(3_600_000L - 600_000L, nextSyncLabelChangeInMillis(synced, synced + 3_600_000 + 600_000))
        assertEquals(86_400_000L - 1_000L, nextSyncLabelChangeInMillis(synced, synced + 86_400_000 + 1_000))
    }

    @Test
    fun aClockBehindTheSyncNeverSpinsTheTicker() {
        assertEquals(60_000L, nextSyncLabelChangeInMillis(synced, synced - 5_000))
        assertEquals(1_000L, nextSyncLabelChangeInMillis(synced, synced + 59_999))
    }
}
