package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class FeedDirectionQueryCacheKeyTest {
    @Test
    fun `refresh nonce keeps the same Room cache scope`() {
        val base = FeedDirectionQuery(
            parkId = "cbe",
            targetDate = "2026-08-17",
            shedId = "mandela-1",
            workflow = "normal",
            status = "pending",
            refreshNonce = 0,
        )

        val refreshed = base.copy(refreshNonce = 1)

        assertNotEquals(base, refreshed)
        assertEquals(base.roomKey(), refreshed.roomKey())
        assertEquals("direction-v2|cbe|2026-08-17|mandela-1||normal|pending|20", refreshed.roomKey())
    }
}
