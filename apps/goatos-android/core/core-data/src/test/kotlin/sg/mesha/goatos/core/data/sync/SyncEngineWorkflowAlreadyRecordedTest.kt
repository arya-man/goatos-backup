package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WorkflowActionAnswerRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionCompleteRequestDto
import sg.mesha.goatos.core.network.dto.WorkflowActionDto
import sg.mesha.goatos.core.network.dto.WorkflowActionWriteResponseDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto

/**
 * Realme E2E 2026-09-17 (P0): a workflow step write the server already holds came back
 * `409 action_already_completed`, was parked as a CONFLICT row, and that dead row held the
 * workflow's outbox lane -- the next step's proof upload and answers sat QUEUED for minutes and the
 * card stayed stuck. A step the server shows as recorded is settled as done, never a lane blocker,
 * and a phone already holding such a row from an older build recovers on its next drain.
 */
class SyncEngineWorkflowAlreadyRecordedTest {

    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private class WorkflowApi(
        private val serverStatus: String,
        delegate: AppApi = FakeAppApi(),
    ) : AppApi by delegate {
        val completeKeys = mutableListOf<String>()
        val answerKeys = mutableListOf<String>()

        override suspend fun completeWorkflowAction(
            workflowId: String,
            actionId: String,
            idempotencyKey: String,
            request: WorkflowActionCompleteRequestDto,
        ): WorkflowActionWriteResponseDto {
            completeKeys += idempotencyKey
            if (actionId == STEP) throw HttpException(Response.error<Unit>(409, "".toResponseBody(null)))
            return WorkflowActionWriteResponseDto(workflowId = workflowId, actionId = actionId, status = "completed")
        }

        override suspend fun answerWorkflowAction(
            workflowId: String,
            actionId: String,
            idempotencyKey: String,
            request: WorkflowActionAnswerRequestDto,
        ): WorkflowActionWriteResponseDto {
            answerKeys += idempotencyKey
            return WorkflowActionWriteResponseDto(workflowId = workflowId, actionId = actionId, status = "completed")
        }

        override suspend fun getWorkflow(workflowId: String, lens: String?, date: String?): WorkflowDetailResponseDto =
            WorkflowDetailResponseDto(
                workflowId = workflowId,
                module = "reconcile",
                actionsDone = 1,
                actionsTotal = 2,
                actions = listOf(
                    WorkflowActionDto(actionId = STEP, actionKey = "gate_photo", seq = 1, actionType = "action", status = serverStatus),
                    WorkflowActionDto(actionId = NEXT, actionKey = "why_wrong_pen", seq = 2, actionType = "question", status = "pending"),
                ),
            )
    }

    private fun engine(store: FakeOutboxStore, api: AppApi) =
        SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { 10_000L })

    private fun row(id: String, opType: OutboxOpType, payload: String, createdAt: Long, status: OutboxStatus = OutboxStatus.QUEUED, conflict: Boolean = false) =
        OutboxEntity(
            id = id,
            opType = opType.name,
            groupKey = WORKFLOW,
            idempotencyKey = "key-$id",
            payloadJson = payload,
            requestFingerprint = "fp-$id",
            status = status.name,
            attemptCount = if (status == OutboxStatus.FAILED) 1 else 0,
            maxAttempts = 5,
            conflict = conflict,
            createdAt = createdAt,
            updatedAt = createdAt,
            nextAttemptAt = if (conflict) Long.MAX_VALUE else 0L,
            lastError = if (conflict) "this action is already completed" else null,
            resultJson = null,
        )

    private val completePayload = """{"workflow_id":"$WORKFLOW","action_id":"$STEP"}"""
    private val answerPayload = """{"workflow_id":"$WORKFLOW","action_id":"$NEXT","answer_value":"gate_left_open"}"""

    @Test
    fun `a 409 for a step the server already recorded settles as done and frees the lane`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(row("dup-complete", OutboxOpType.WORKFLOW_ACTION_COMPLETE, completePayload, createdAt = 1L))
        store.insert(row("next-answer", OutboxOpType.WORKFLOW_ACTION_ANSWER, answerPayload, createdAt = 2L))
        val api = WorkflowApi(serverStatus = "completed")

        engine(store, api).drainOnce()

        val settled = store.findById("dup-complete")!!
        assertEquals("a step the server holds is done, not a conflict", OutboxStatus.SUCCEEDED.name, settled.status)
        assertEquals("the lane behind it drains", listOf("key-next-answer"), api.answerKeys)
    }

    @Test
    fun `a 409 for a step the server does NOT show recorded stays a visible conflict`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(row("dup-complete", OutboxOpType.WORKFLOW_ACTION_COMPLETE, completePayload, createdAt = 1L))
        val api = WorkflowApi(serverStatus = "pending")

        engine(store, api).drainOnce()

        val failed = store.findById("dup-complete")!!
        assertEquals(OutboxStatus.FAILED.name, failed.status)
        assertTrue(failed.conflict)
    }

    @Test
    fun `a conflict row left by an older build settles on the next drain and the lane moves`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(row("legacy-dup", OutboxOpType.WORKFLOW_ACTION_COMPLETE, completePayload, createdAt = 1L, status = OutboxStatus.FAILED, conflict = true))
        store.insert(row("next-answer", OutboxOpType.WORKFLOW_ACTION_ANSWER, answerPayload, createdAt = 2L))
        val api = WorkflowApi(serverStatus = "in_review")

        engine(store, api).drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("legacy-dup")!!.status)
        assertTrue("the settled row is never re-sent", api.completeKeys.isEmpty())
        assertEquals(listOf("key-next-answer"), api.answerKeys)
    }

    /**
     * Realme E2E 2026-09-17: an older build left a duplicate step write refused 409
     * `action_already_completed`. The verifier then REJECTED the step, so the server shows it in
     * `rework` and the settle above left the row a conflict -- which held the workflow's lane, so
     * the re-shoot's proof uploads and answers stayed queued behind a write that can never matter
     * again (re-sending it would re-submit the rejected proofs).
     */
    @Test
    fun `an already-recorded duplicate for a step since sent back for rework is superseded and frees the lane`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            row("legacy-dup", OutboxOpType.WORKFLOW_ACTION_COMPLETE, completePayload, createdAt = 1L, status = OutboxStatus.FAILED, conflict = true)
                .copy(lastErrorCode = "action_already_completed"),
        )
        store.insert(row("next-answer", OutboxOpType.WORKFLOW_ACTION_ANSWER, answerPayload, createdAt = 2L))
        val api = WorkflowApi(serverStatus = "rework")

        engine(store, api).drainOnce()

        val settled = store.findById("legacy-dup")!!
        assertEquals("a superseded duplicate no longer holds the lane", false, settled.status == OutboxStatus.FAILED.name && settled.conflict)
        assertTrue("the superseded duplicate is never re-sent", api.completeKeys.isEmpty())
        assertEquals("the re-shoot behind it drains", listOf("key-next-answer"), api.answerKeys)
    }

    @Test
    fun `a step write refused for another reason stays a visible conflict while the step is in rework`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            row("refused", OutboxOpType.WORKFLOW_ACTION_COMPLETE, completePayload, createdAt = 1L, status = OutboxStatus.FAILED, conflict = true)
                .copy(lastError = "Record the proof first.", lastErrorCode = "proof_required"),
        )
        val api = WorkflowApi(serverStatus = "rework")

        engine(store, api).drainOnce()

        val failed = store.findById("refused")!!
        assertEquals(OutboxStatus.FAILED.name, failed.status)
        assertTrue(failed.conflict)
    }

    /** Realme 2026-09-17: the settle re-read failed on a flaky network and no later drain ran. */
    @Test
    fun `a settle re-read that fails in transit schedules another pass`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            row("legacy-dup", OutboxOpType.WORKFLOW_ACTION_COMPLETE, completePayload, createdAt = 1L, status = OutboxStatus.FAILED, conflict = true)
                .copy(lastErrorCode = "action_already_completed"),
        )
        store.insert(row("next-answer", OutboxOpType.WORKFLOW_ACTION_ANSWER, answerPayload, createdAt = 2L))
        val offlineApi = object : AppApi by WorkflowApi(serverStatus = "rework") {
            override suspend fun getWorkflow(workflowId: String, lens: String?, date: String?): WorkflowDetailResponseDto =
                throw java.io.IOException("network is unreachable")
        }
        val scheduled = mutableListOf<Long>()
        val engine = SyncEngine(
            store = store, api = offlineApi, connectivityGate = { true }, dispatchers = dispatchers, clock = { 10_000L },
            retryScheduler = SyncRetryScheduler { scheduled += it },
        )

        engine.drainOnce()

        assertEquals("the row is still waiting to be settled", OutboxStatus.FAILED.name, store.findById("legacy-dup")!!.status)
        assertEquals("a retry is scheduled so the lane is not left waiting for a relaunch", 1, scheduled.size)
        assertTrue(scheduled.single() > 10_000L)
    }

    private companion object {
        const val WORKFLOW = "wf-reconcile-1"
        const val STEP = "step-gate-photo"
        const val NEXT = "step-why-wrong-pen"
    }
}
