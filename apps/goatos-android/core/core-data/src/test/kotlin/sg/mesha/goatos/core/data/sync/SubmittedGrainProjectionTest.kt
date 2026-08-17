package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus

/**
 * The badge is now DERIVED from the outbox instead of an in-memory set that had to be hand-cleared.
 *
 * That is the whole point of the design: a submit that succeeds, is rejected, or exhausts its
 * attempts leaves the ACTIVE set on its own, so the badge retracts with no clear() to call, no
 * second key to keep in sync, and nothing to lose on process death. These tests pin that property —
 * they are what replaces the old SubmittedOverlayTerminalClearTest.
 */
class SubmittedGrainProjectionTest {

    private fun packingRow(
        id: String,
        status: OutboxStatus,
        conflict: Boolean = false,
        sessionNo: Int = 1,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.FEED_PACKING_COMPLETE.name,
        groupKey = "feed-pack-shed-castro",
        idempotencyKey = "key-$id",
        payloadJson = syncJson.encodeToString(
            FeedPackingCompletePayload(
                parkId = "park-1",
                shedId = "shed-castro",
                partitionLabel = "2",
                sessionNo = sessionNo,
                targetDate = "2026-08-17",
                workflow = "experiment",
                packingProofOutboxItemId = "proof-1",
            ),
        ),
        status = status.name,
        attemptCount = 0,
        maxAttempts = 8,
        conflict = conflict,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = null,
    )

    private val expectedKey =
        shedSessionKey("2026-08-17", "shed-castro", "2", 1, "experiment")

    private fun grains(vararg rows: OutboxEntity): Set<String> = runBlocking {
        val store = FakeOutboxStore()
        rows.forEach { store.insert(it) }
        DefaultSyncRepository.projectSubmittedGrains(store.observeActive().first(), syncJson)
    }

    @Test
    fun `a QUEUED submit projects its grain — the badge appears with no mark() call`() {
        assertTrue(grains(packingRow("r1", OutboxStatus.QUEUED)).contains(expectedKey))
    }

    @Test
    fun `an IN_FLIGHT submit still projects — it is alive and will land`() {
        assertTrue(grains(packingRow("r1", OutboxStatus.IN_FLIGHT)).contains(expectedKey))
    }

    @Test
    fun `a REJECTED submit stops projecting — this is what replaces the terminal clear`() {
        assertFalse(
            "a conflict row is terminal; it must leave the badge set by itself",
            grains(packingRow("r1", OutboxStatus.FAILED, conflict = true)).contains(expectedKey),
        )
    }

    @Test
    fun `a SUCCEEDED submit stops projecting — the backend status takes over`() {
        assertFalse(grains(packingRow("r1", OutboxStatus.SUCCEEDED)).contains(expectedKey))
    }

    @Test
    fun `the pen-day row and its dispatched session project the SAME grain`() {
        // The old design keyed these apart and stranded the badge. Here both rows normalise to one.
        assertEquals(
            setOf(expectedKey),
            grains(packingRow("r1", OutboxStatus.QUEUED, sessionNo = 0)),
        )
    }

    @Test
    fun `an empty outbox projects an empty set, never a stale badge`() {
        assertEquals(emptySet<String>(), grains())
    }
}
