package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto

/**
 * The two feed completions do NOT share a grain, and the difference is load-bearing on the wire.
 *
 *  - PACKING is a PEN-DAY. One video covers the pen's morning and evening shares together
 *    (maintainer decision 2026-08-10), so `session_no` must NOT be sent -- the route rejects it
 *    outright rather than accepting and ignoring it.
 *  - DISTRIBUTION is still a PEN-SESSION and MUST keep sending `session_no`.
 *
 * Written because nothing covered these payloads at all. While implementing the packing merge the
 * `session_no` line was removed from the DISTRIBUTION dispatcher by mistake; only a required
 * constructor argument made that a compile error. Had the field carried a default it would have
 * shipped silently and every distribution evening session would have collapsed onto its morning --
 * marking a shed fed that nobody had fed.
 */
class FeedCompletionPayloadContractTest {

    private val json = Json { ignoreUnknownKeys = true }
    /** Matches SyncEngine/SyncRepository: defaults are omitted, so an absent key means "not sent". */
    private val wire = Json { encodeDefaults = false }

    // -----------------------------------------------------------------------
    // The durable outbox: a row queued by an EARLIER build must still decode
    // -----------------------------------------------------------------------

    /**
     * The outbox holds an operator's recorded-but-unsynced video across an app upgrade. A pre-merge
     * row carries `session_no`; if the property were dropped outright, decoding would throw and the
     * row would die terminally -- losing work the operator has already done and been told was saved.
     *
     * The legacy row simply loses its session, which is exactly right: its pen-day row absorbs it.
     */
    @Test
    fun `a packing outbox row queued before the merge still decodes`() {
        val legacy = """
            {"park_id":"park-1","shed_id":"shed-1","partition_label":"Part 3","session_no":2,
             "target_date":"2026-08-10","workflow":"normal",
             "packing_proof_outbox_item_id":"outbox-9"}
        """.trimIndent()

        val payload = json.decodeFromString<FeedPackingCompletePayload>(legacy)

        assertEquals("shed-1", payload.shedId)
        assertEquals("Part 3", payload.partitionLabel)
        assertEquals("2026-08-10", payload.targetDate)
        assertEquals("outbox-9", payload.packingProofOutboxItemId)
        // Read back, never re-sent. See the field's kdoc.
        assertEquals(2, payload.sessionNo)
    }

    /** A row written by the CURRENT build carries no session at all and must decode just as well. */
    @Test
    fun `a packing outbox row written after the merge decodes without a session`() {
        val current = """
            {"park_id":"park-1","shed_id":"shed-1","partition_label":"Part 3",
             "target_date":"2026-08-10","workflow":"normal",
             "packing_proof_outbox_item_id":"outbox-9"}
        """.trimIndent()

        val payload = json.decodeFromString<FeedPackingCompletePayload>(current)

        assertEquals(0, payload.sessionNo)
        assertEquals("Part 3", payload.partitionLabel)
    }

    // -----------------------------------------------------------------------
    // What actually goes on the wire
    // -----------------------------------------------------------------------

    @Test
    fun `the packing request never carries a session`() {
        val body = wire.encodeToString(
            FeedPackingCompleteRequestDto(
                parkId = "park-1",
                shedId = "shed-1",
                partitionLabel = "Part 3",
                targetDate = "2026-08-10",
                workflow = "normal",
                packingProofRef = "proof-1",
            ),
        )

        // `additionalProperties: false` on the contract means a stray session_no is a 400, not a
        // silently-ignored field -- so sending one would break every packing submission.
        assertFalse("packing body must not carry session_no: $body", body.contains("session_no"))
        // The PEN must still travel: it is part of the completion's identity, and omitting it makes
        // one video close out every pen of the shed (migration 000137).
        assertTrue(body.contains(""""partition_label":"Part 3""""))
        assertTrue(body.contains(""""target_date":"2026-08-10""""))
        assertTrue(body.contains(""""packing_proof_ref":"proof-1""""))
    }

    @Test
    fun `the distribution request still carries its session`() {
        val body = wire.encodeToString(
            FeedDistributionCompleteRequestDto(
                parkId = "park-1",
                shedId = "shed-1",
                partitionLabel = "Part 3",
                sessionNo = 2,
                targetDate = "2026-08-10",
                workflow = "normal",
                distributionProofRef = "proof-feed",
                waterProofRef = "proof-water",
            ),
        )

        // THE REGRESSION THIS FILE EXISTS FOR. Distribution did not merge: dropping its session
        // would key the evening completion onto the morning's row, and the second submission would
        // come back as an already-pending no-op -- an operator shown success for work nothing
        // recorded, and a shed marked fed once when it was fed twice.
        assertTrue("distribution body must carry session_no: $body", body.contains(""""session_no":2"""))
        assertTrue(body.contains(""""partition_label":"Part 3""""))
        assertTrue(body.contains(""""water_proof_ref":"proof-water""""))
    }

    /**
     * The distribution payload must round-trip its session too -- the wire test above only proves
     * the DTO can carry one, not that the queued row still holds it.
     */
    @Test
    fun `a distribution outbox row round-trips its session`() {
        val payload = FeedDistributionCompletePayload(
            parkId = "park-1",
            shedId = "shed-1",
            partitionLabel = "Part 3",
            sessionNo = 2,
            targetDate = "2026-08-10",
            workflow = "normal",
            distributionProofOutboxItemId = "outbox-1",
            waterProofOutboxItemId = "outbox-2",
        )

        val decoded = json.decodeFromString<FeedDistributionCompletePayload>(
            Json.encodeToString(payload),
        )

        assertEquals(2, decoded.sessionNo)
        assertEquals("Part 3", decoded.partitionLabel)
    }
}
