package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardResponseDto

/**
 * `WEIGHING_FASTING_SUBMIT` dispatch (maintainer correction #2, 2026-09-03: the submit is PER
 * SHED — one shed card, its own feed and water videos). The submit references each clip by
 * PROOF_UPLOAD outbox item id and the engine resolves the uploaded proof ids at drain time. Four
 * behaviours are load-bearing:
 *
 *  1. A not-yet-uploaded clip SUSPENDS the WHOLE submit on the proof-dependency wait —
 *     attemptCount does NOT move, so a slow upload can never burn the submit's retry budget;
 *  2. Once both uploads are SUCCEEDED, the submit goes out ONCE to the per-shed endpoint with
 *     the resolved proof ids, under the row's own stored idempotency key as the header;
 *  3. A permanently-failed upload terminalizes the submit with an operator-facing reason instead
 *     of retrying a write the backend would refuse forever;
 *  4. A row missing its shed or either clip reference (a pre-per-shed queued row) is terminal,
 *     never retried.
 */
class WeighingFastingSubmitDispatchTest {

    private fun proofRow(
        id: String,
        status: OutboxStatus,
        proofId: String? = null,
        conflict: Boolean = false,
        attemptCount: Int = 0,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.PROOF_UPLOAD.name,
        // PER-SLOT upload lane — deliberately NOT the submit's group, mirroring production
        // enqueue: the submit must wait by RESOLUTION, not by queue order.
        groupKey = "weighing-fasting:task-1:shed-b:$id",
        idempotencyKey = "proof-key-$id",
        payloadJson = "{}",
        status = status.name,
        attemptCount = attemptCount,
        maxAttempts = 8,
        conflict = conflict,
        createdAt = 0L,
        updatedAt = 0L,
        // Uploads are parked out of drain eligibility so the pass under test dispatches ONLY the
        // submit row; their state is what the resolution reads.
        nextAttemptAt = Long.MAX_VALUE,
        lastError = null,
        resultJson = proofId?.let {
            syncJson.encodeToString(ProofUploadResponseDto(proof = ProofReferenceDto(proofId = it)))
        },
    )

    private fun payload() = WeighingFastingSubmitPayload(
        fastingTaskId = "task-1",
        campaignShedId = "shed-b",
        feedProofOutboxItemId = "feed-b",
        waterProofOutboxItemId = "water-b",
    )

    private fun submitRow(payload: WeighingFastingSubmitPayload = payload()) = OutboxEntity(
        id = "submit-1",
        opType = OutboxOpType.WEIGHING_FASTING_SUBMIT.name,
        groupKey = "weighing-fasting:task-1:shed-b",
        idempotencyKey = "weighing-fasting-submit:task-1:shed-b:feed-b|water-b",
        payloadJson = syncJson.encodeToString(payload),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = 8,
        conflict = false,
        createdAt = 10L,
        updatedAt = 10L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = null,
    )

    @Test
    fun `one pending upload suspends the whole submit without burning retry budget`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true })
        store.insert(proofRow("feed-b", OutboxStatus.SUCCEEDED, proofId = "server-proof-feed-b"))
        store.insert(proofRow("water-b", OutboxStatus.QUEUED))
        store.insert(submitRow())

        engine.drainOnce()

        assertEquals("no submit may reach the backend while either clip is still uploading", 0, api.fastingSubmitCalls.size)
        val row = store.findById("submit-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals("a dependency wait must not consume an attempt", 0, row.attemptCount)
        assertEquals(false, row.conflict)
        assertTrue("the wait re-arms a near-term retry, never a terminal park", row.nextAttemptAt < Long.MAX_VALUE)
    }

    @Test
    fun `both uploads resolved go out as one per-shed request under the stored key`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        api.submitWeighingFastingShedFn = { taskId, shedId, _, _ ->
            WeighingFastingShedCardResponseDto(
                fastingShedCard = WeighingFastingShedCardDto(
                    fastingTaskId = taskId,
                    campaignShedId = shedId,
                    status = "pending_verification",
                ),
            )
        }
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true })
        store.insert(proofRow("feed-b", OutboxStatus.SUCCEEDED, proofId = "server-proof-feed-b"))
        store.insert(proofRow("water-b", OutboxStatus.SUCCEEDED, proofId = "server-proof-water-b"))
        store.insert(submitRow())

        engine.drainOnce()

        assertEquals(1, api.fastingSubmitCalls.size)
        val call = api.fastingSubmitCalls.single()
        assertEquals("task-1", call.fastingTaskId)
        assertEquals("the submit names ITS shed on the path", "shed-b", call.campaignShedId)
        assertEquals(
            "the row's STORED key rides the header verbatim",
            "weighing-fasting-submit:task-1:shed-b:feed-b|water-b",
            call.idempotencyKey,
        )
        assertEquals("a fresh clip resolves to its UPLOADED proof id", "server-proof-feed-b", call.request.feedProofRef)
        assertEquals("server-proof-water-b", call.request.waterProofRef)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("submit-1")!!.status)
    }

    @Test
    fun `a permanently failed upload terminalizes the submit instead of retrying forever`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true })
        store.insert(proofRow("feed-b", OutboxStatus.SUCCEEDED, proofId = "server-proof-feed-b"))
        store.insert(proofRow("water-b", OutboxStatus.FAILED, conflict = true))
        store.insert(submitRow())

        engine.drainOnce()

        assertEquals(0, api.fastingSubmitCalls.size)
        val row = store.findById("submit-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals("a submit that can never succeed must not retry", true, row.conflict)
        assertTrue(
            "the operator is told what to do, never shown a bare status line",
            row.lastError.orEmpty().isNotBlank(),
        )
    }

    @Test
    fun `a queued row missing its shed or a clip reference is terminal, never retried`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true })
        store.insert(submitRow(WeighingFastingSubmitPayload(fastingTaskId = "task-1")))

        engine.drainOnce()

        assertEquals(0, api.fastingSubmitCalls.size)
        val row = store.findById("submit-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals("a pre-per-shed row can never satisfy the per-shed submit", true, row.conflict)
        assertTrue(row.lastError.orEmpty().isNotBlank())
    }
}
