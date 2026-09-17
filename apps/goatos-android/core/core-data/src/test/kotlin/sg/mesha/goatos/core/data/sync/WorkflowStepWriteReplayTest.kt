package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.FakeAppApi

/**
 * Realme E2E 2026-09-17: reopening a workflow card showed "Idempotency key already belongs to a
 * different queued write." The reopened screen re-sent a step it had already queued, now carrying
 * the proof's SERVER id beside the same upload outbox id (the capture row had synced meanwhile).
 * That server id is only a pruning-survival hint for the same proof, not a different request, so the
 * re-send must collapse onto the original write -- and no technical wording may ever reach an operator.
 */
class WorkflowStepWriteReplayTest {

    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private fun repository(store: FakeOutboxStore = FakeOutboxStore()): DefaultSyncRepository {
        // Offline: the test is about what is QUEUED, not what drains.
        val engine = SyncEngine(store = store, api = FakeAppApi(), connectivityGate = { false }, dispatchers = dispatchers, clock = { 0L })
        return DefaultSyncRepository(store = store, engine = engine, connectivityGate = { false }, appScope = CoroutineScope(Dispatchers.Unconfined), dispatchers = dispatchers, clock = { 0L })
    }

    @Test
    fun `re-sending a queued step with its proof's server id collapses onto the original write`() = runBlocking {
        val store = FakeOutboxStore()
        val repo = repository(store)
        val first = repo.enqueueWorkflowActionComplete(
            groupKey = "wf-1", idempotencyKey = "wf-complete:step-1:5542d48a", workflowId = "wf-1", actionId = "step-1",
            proofOutboxItems = listOf(WorkflowProofOutboxRef(outboxItemId = "proof-outbox-1", kind = "photo")),
        )
        val again = repo.enqueueWorkflowActionComplete(
            groupKey = "wf-1", idempotencyKey = "wf-complete:step-1:5542d48a", workflowId = "wf-1", actionId = "step-1",
            proofOutboxItems = listOf(WorkflowProofOutboxRef(outboxItemId = "proof-outbox-1", proofRef = "server-proof-1", kind = "photo")),
        )

        assertTrue(first is AppResult.Ok)
        assertTrue("the same step with the same proof is the same write, got $again", again is AppResult.Ok)
        assertEquals((first as AppResult.Ok).value, (again as AppResult.Ok).value)
        assertEquals(1, store.snapshot().size)
    }

    @Test
    fun `a genuinely different write under a used key is refused in farm words`() = runBlocking {
        val repo = repository()
        repo.enqueueWorkflowActionAnswer(
            groupKey = "wf-1", idempotencyKey = "wf-answer:step-2", workflowId = "wf-1", actionId = "step-2", answerValue = "yes",
        )
        val different = repo.enqueueWorkflowActionAnswer(
            groupKey = "wf-1", idempotencyKey = "wf-answer:step-2", workflowId = "wf-1", actionId = "step-2", answerValue = "no",
        )

        val message = (different as AppResult.Err).message
        listOf("idempotency", "queued write", "key").forEach { banned ->
            assertFalse("operator copy must not say '$banned': $message", message.contains(banned, ignoreCase = true))
        }
    }
}
