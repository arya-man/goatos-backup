package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test

class WorkBoardQueryCacheKeyTest {
    @Test
    fun `refresh nonce keeps the same Room cache scope`() {
        val base = WorkBoardQuery(
            parkId = "park-1",
            businessDate = "2026-09-10",
            lane = WorkBoardLanes.IN_REVIEW,
            module = "feed",
            owner = "me",
            refreshNonce = 0,
        )

        val refreshed = base.copy(refreshNonce = 1)

        assertNotEquals(base, refreshed)
        assertEquals(base.roomKey(), refreshed.roomKey())
        assertEquals("board-v1|park-1|2026-09-10|in_review|feed|me|20", refreshed.roomKey())
        // The summary is board-wide: its scope drops the lane/module narrowing but keeps the owner.
        assertEquals("board-v1|park-1|2026-09-10|||me|20", refreshed.summaryKey())
    }

    @Test
    fun `every filter dimension partitions the scope and a missing park never collides`() {
        val base = WorkBoardQuery(parkId = null, businessDate = "2026-09-10")
        assertEquals("board-v1||2026-09-10||||20", base.roomKey())
        assertNotEquals(base.roomKey(), base.copy(parkId = "park-1").roomKey())
        assertNotEquals(base.roomKey(), base.copy(businessDate = "2026-09-11").roomKey())
        assertNotEquals(base.roomKey(), base.copy(lane = WorkBoardLanes.DONE).roomKey())
        assertNotEquals(base.roomKey(), base.copy(module = "health").roomKey())
        assertNotEquals(base.roomKey(), base.copy(owner = "me").roomKey())
    }

    @Test
    fun `a lane chip becomes the backend state csv and no lane means no state filter`() {
        val unfiltered = WorkBoardQuery(parkId = "park-1", businessDate = "2026-09-10")
        assertNull(unfiltered.stateCsv)
        assertNull(unfiltered.moduleCsv)
        assertNull(unfiltered.ownerParam)

        val inProgress = unfiltered.copy(lane = WorkBoardLanes.IN_PROGRESS)
        assertEquals("in_progress,proof_pending,rejected,blocked", inProgress.stateCsv)
        assertEquals("verification_pending", unfiltered.copy(lane = WorkBoardLanes.IN_REVIEW).stateCsv)
        assertEquals("completed", unfiltered.copy(lane = WorkBoardLanes.DONE).stateCsv)
        assertEquals("scheduled,due,overdue,deferred,missed", unfiltered.copy(lane = WorkBoardLanes.TODO).stateCsv)
    }
}
