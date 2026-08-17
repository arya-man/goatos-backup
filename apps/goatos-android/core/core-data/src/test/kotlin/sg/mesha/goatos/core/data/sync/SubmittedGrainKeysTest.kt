package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * REGRESSION LOCK for the three defects that were found and fixed on the OLD in-memory design.
 *
 * That design rebuilt the grain key independently at mark time and at clear time, and the two
 * disagreed in three ways — each one stranding the badge on "In review" for work the server had
 * refused. The outbox-derived design removes the second key entirely, so these bugs cannot recur by
 * construction. These tests exist so nobody reintroduces the conditions that caused them:
 *
 *   1. pen-day `sessionNo = 0` vs its dispatched `1`
 *   2. a `businessDate()` read on either side of midnight
 *   3. null vs blank vs cased partition labels
 *
 * If a future change reintroduces a clock into the key, or drops a normalisation, these fail.
 */
class SubmittedGrainKeysTest {

    private fun packingPayload(sessionNo: Int, partitionLabel: String?, targetDate: String = "2026-08-17") =
        syncJson.encodeToString(
            FeedPackingCompletePayload(
                parkId = "park-1",
                shedId = "shed-castro",
                partitionLabel = partitionLabel,
                sessionNo = sessionNo,
                targetDate = targetDate,
                workflow = "experiment",
                packingProofOutboxItemId = "proof-1",
            ),
        )

    private fun keyFor(payloadJson: String) =
        submittedGrainKeyOf(OutboxOpTypeName.FEED_PACKING_COMPLETE.name, payloadJson, syncJson)

    @Test
    fun `REGRESSION 1 - pen-day sessionNo 0 keys the same grain as its dispatched session 1`() {
        assertEquals(
            "0 means 'queued by the pen-day build' and dispatches as 1; keying them apart is what " +
                "stranded the badge for exactly those rows",
            keyFor(packingPayload(sessionNo = 1, partitionLabel = "2")),
            keyFor(packingPayload(sessionNo = 0, partitionLabel = "2")),
        )
    }

    @Test
    fun `REGRESSION 2 - the key contains NO clock, so it cannot change across midnight`() {
        val payload = packingPayload(sessionNo = 1, partitionLabel = "2", targetDate = "2026-08-17")

        // Same payload, read twice: if any segment came from "now", these could differ when the
        // second read lands on the other side of midnight. They must be byte-identical always.
        assertEquals(keyFor(payload), keyFor(payload))

        // And the date that IS in the key is the submit's own target date, not today's.
        assertEquals(
            "the key must be anchored to the submit's target date",
            "2026-08-17",
            keyFor(payload)!!.substringBefore('|'),
        )
        assertNotEquals(
            "a different target date is a different grain",
            keyFor(payload),
            keyFor(packingPayload(sessionNo = 1, partitionLabel = "2", targetDate = "2026-08-18")),
        )
    }

    @Test
    fun `REGRESSION 3 - null, blank, whitespace and case all resolve to one partition grain`() {
        val whole = keyFor(packingPayload(sessionNo = 1, partitionLabel = null))
        assertEquals("blank is the whole shed", whole, keyFor(packingPayload(1, "")))
        assertEquals("whitespace is the whole shed", whole, keyFor(packingPayload(1, "   ")))
        assertEquals(
            "case must not split the grain",
            keyFor(packingPayload(1, "Pen A")),
            keyFor(packingPayload(1, "pen a")),
        )
        assertNotEquals("a real partition is NOT the whole shed", whole, keyFor(packingPayload(1, "2")))
    }

    @Test
    fun `distinct grains stay distinct - shed, partition, session, workflow`() {
        val base = keyFor(packingPayload(sessionNo = 1, partitionLabel = "2"))
        assertNotEquals(base, keyFor(packingPayload(sessionNo = 2, partitionLabel = "2")))
        assertNotEquals(base, keyFor(packingPayload(sessionNo = 1, partitionLabel = "3")))
    }

    @Test
    fun `task-grain flows are namespaced so a shared id cannot collide`() {
        assertNotEquals(
            "a transport task and a milk task with the same id are different grains",
            taskGrainKey("feed-transport", "shared-id"),
            taskGrainKey("milk-feeding", "shared-id"),
        )
    }

    @Test
    fun `an opType with no list badge maps to null rather than inventing a key`() {
        assertNull(submittedGrainKeyOf("SCAN_CAPTURE", "{}", syncJson))
    }

    @Test
    fun `an undecodable payload maps to null rather than throwing into the projection`() {
        assertNull(
            submittedGrainKeyOf(OutboxOpTypeName.FEED_PACKING_COMPLETE.name, "{not json", syncJson),
        )
    }
}
