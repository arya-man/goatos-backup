package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionResponseDto

/**
 * What a second tap on a REFUSED approval decision does, through the REAL outbox (no fake
 * repository). The decision key is `counts-approval-<direction>:<requestId>`, stable per
 * request and direction, so a second tap after a 409 re-uses the key.
 *
 * Suspected: the dead terminal row is handed back (old error re-shown, nothing re-sent), and a
 * reject with a NEW reason is refused as an idempotency conflict. Pinned here: neither happens.
 * [DefaultSyncRepository.enqueue] re-opens a terminal row IN PLACE (same id, same key, the
 * caller's latest payload, fresh attempt budget) and the decision is re-sent. That is safe on
 * the server too: a refused decision stores no decision key (the counts repository writes
 * `decision_idempotency_key` only on the row it actually decides), so re-sending the same key
 * with a corrected reason is a new attempt, not a conflicting replay.
 */
class CountsApprovalDecisionRetryTest {

    private val dispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private class DecisionApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        val calls = mutableListOf<Triple<String, String, String?>>()
        var refusalsRemaining = 1

        private fun answer(direction: String, key: String, request: CountsApprovalDecisionRequestDto): CountsApprovalDecisionResponseDto {
            calls += Triple(direction, key, request.reason)
            if (refusalsRemaining > 0) {
                refusalsRemaining--
                throw HttpException(
                    Response.error<Unit>(
                        409,
                        """{"code":"death_evidence_incomplete","message":"every step of the death report must be recorded before approval"}"""
                            .toResponseBody("application/json".toMediaType()),
                    ),
                )
            }
            return CountsApprovalDecisionResponseDto(approvalRequestId = "req-1", status = direction)
        }

        override suspend fun approveCountsApproval(requestId: String, idempotencyKey: String, request: CountsApprovalDecisionRequestDto) =
            answer("approved", idempotencyKey, request)

        override suspend fun rejectCountsApproval(requestId: String, idempotencyKey: String, request: CountsApprovalDecisionRequestDto) =
            answer("rejected", idempotencyKey, request)
    }

    private fun repository(api: AppApi, store: FakeOutboxStore = FakeOutboxStore()): Pair<DefaultSyncRepository, FakeOutboxStore> {
        val engine = SyncEngine(store = store, api = api, connectivityGate = { true }, dispatchers = dispatchers, clock = { 0L })
        return DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = { true },
            appScope = CoroutineScope(Dispatchers.Unconfined),
            dispatchers = dispatchers,
            clock = { 0L },
        ) to store
    }

    @Test
    fun `a second approve tap after a terminal refusal re-sends the decision under the same row`() = runBlocking {
        val api = DecisionApi()
        val (repo, store) = repository(api)

        val first = repo.enqueueCountsApprovalDecision("req-1", approve = true, reason = null, idempotencyKey = "counts-approval-approve:req-1")
        val id = (first as AppResult.Ok).value
        val dead = store.findById(id)!!
        assertEquals("FAILED", dead.status)
        assertTrue(dead.conflict)
        assertEquals("death_evidence_incomplete", dead.lastErrorCode)

        val second = repo.enqueueCountsApprovalDecision("req-1", approve = true, reason = null, idempotencyKey = "counts-approval-approve:req-1")
        assertEquals("the terminal row is reopened in place, not replaced", id, (second as AppResult.Ok).value)
        assertEquals("the decision really went out a second time", 2, api.calls.size)
        assertEquals("SUCCEEDED", store.findById(id)!!.status)
    }

    @Test
    fun `a reject with a new reason after a terminal refusal is re-sent, never an idempotency conflict`() = runBlocking {
        val api = DecisionApi()
        val (repo, store) = repository(api)

        val first = repo.enqueueCountsApprovalDecision("req-1", approve = false, reason = "blurry video", idempotencyKey = "counts-approval-reject:req-1")
        val id = (first as AppResult.Ok).value
        assertEquals("FAILED", store.findById(id)!!.status)

        val second = repo.enqueueCountsApprovalDecision("req-1", approve = false, reason = "second video missing", idempotencyKey = "counts-approval-reject:req-1")
        assertTrue("a corrected reason is not a conflict against a dead row", second is AppResult.Ok)
        assertEquals(listOf("blurry video", "second video missing"), api.calls.map { it.third })
        assertEquals("SUCCEEDED", store.findById(id)!!.status)
    }
}
