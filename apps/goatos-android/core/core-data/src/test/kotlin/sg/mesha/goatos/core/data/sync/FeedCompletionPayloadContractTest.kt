package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto

/**
 * BOTH feed completions are a PEN-SESSION and both MUST send `session_no` on the wire.
 *
 * Packing was briefly a pen-DAY (2026-08-10) and that was reverted on 2026-08-11: a pen's morning and
 * evening bags are packed and filmed separately, so each completion must say which bag it proves.
 *
 * Written because nothing covered these payloads at all. While implementing the pen-day merge the
 * `session_no` line was removed from the DISTRIBUTION dispatcher by mistake; only a required
 * constructor argument made that a compile error. Had the field carried a default it would have
 * shipped silently and every distribution evening session would have collapsed onto its morning --
 * marking a shed fed that nobody had fed. The same collapse is now what a missing packing session
 * would cause, which is why both halves are pinned here.
 */
class FeedCompletionPayloadContractTest {

    private val json = Json { ignoreUnknownKeys = true }
    /** Matches SyncEngine/SyncRepository: defaults are omitted, so an absent key means "not sent". */
    private val wire = Json { encodeDefaults = false }

    // -----------------------------------------------------------------------
    // The durable outbox: a row queued by an EARLIER build must still decode
    // -----------------------------------------------------------------------

    /**
     * The outbox holds an operator's recorded-but-unsynced video across an app upgrade. A row queued
     * by a session-aware build carries `session_no` and must keep it: that is the bag it proves.
     */
    @Test
    fun `a packing outbox row queued with a session keeps it`() {
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
        assertEquals(2, payload.sessionNo)
    }

    /**
     * THE UPGRADE PATH OFF THE PEN-DAY BUILD. A row that build queued carries NO session, so it must
     * still decode -- dropping the property's default would throw and kill an operator's recorded
     * video terminally. It decodes to 0, and SyncEngine maps that to session 1 on dispatch, the same
     * choice migration 000150 makes for the pen-day rows already on the server.
     */
    @Test
    fun `a packing outbox row queued by the pen-day build still decodes`() {
        val penDay = """
            {"park_id":"park-1","shed_id":"shed-1","partition_label":"Part 3",
             "target_date":"2026-08-10","workflow":"normal",
             "packing_proof_outbox_item_id":"outbox-9"}
        """.trimIndent()

        val payload = json.decodeFromString<FeedPackingCompletePayload>(penDay)

        assertEquals(0, payload.sessionNo)
        assertEquals("Part 3", payload.partitionLabel)
    }

    // -----------------------------------------------------------------------
    // What actually goes on the wire
    // -----------------------------------------------------------------------

    @Test
    fun `the packing request carries its session`() {
        val body = wire.encodeToString(
            FeedPackingCompleteRequestDto(
                parkId = "park-1",
                shedId = "shed-1",
                partitionLabel = "Part 3",
                sessionNo = 2,
                targetDate = "2026-08-10",
                workflow = "normal",
                packingProofRef = "proof-1",
            ),
        )

        // Omitting it keys the evening completion onto the morning's row: the second submission comes
        // back as an already-pending no-op, so the operator is shown success for a video nothing
        // recorded and no verifier ever sees. The server rejects a missing/zero session outright,
        // which is the louder half of the same protection.
        assertTrue("packing body must carry session_no: $body", body.contains(""""session_no":2"""))
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
                feedWeightProofRef = "proof-weight",
            ),
        )

        // THE REGRESSION THIS FILE EXISTS FOR. Dropping its session would key the evening completion
        // onto the morning's row, and the second submission would come back as an already-pending
        // no-op -- an operator shown success for work nothing recorded, and a shed marked fed once
        // when it was fed twice.
        assertTrue("distribution body must carry session_no: $body", body.contains(""""session_no":2"""))
        assertTrue(body.contains(""""partition_label":"Part 3""""))
        assertTrue(body.contains(""""water_proof_ref":"proof-water""""))
        assertTrue(body.contains(""""feed_weight_proof_ref":"proof-weight""""))
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
