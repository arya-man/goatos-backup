package sg.mesha.goatos.core.data.sync

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response
import sg.mesha.goatos.core.data.PenVisitPageMeta
import sg.mesha.goatos.core.data.PenVisitsRepository
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.PenVisitDetailDto
import sg.mesha.goatos.core.network.dto.PenVisitDto
import sg.mesha.goatos.core.network.dto.PenVisitSubmitRequestDto
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto

/**
 * How a queued pen-visit submit settles against the server's answers, through the REAL engine.
 *
 *  - Two visitors film the same pen offline. The first clip reaches the verifier; the second
 *    visitor's queued submit is then refused `409 visit_in_review`. The pen IS visited, so that
 *    row must settle (SUCCEEDED, carrying the fresh "in review" task) — before this fix it became
 *    a permanent failed row and the card offered "Record again" beside a refusal.
 *  - A submit that lands asks the shell to re-read the bootstrap, so the Tasks "For me" badge
 *    stops counting a visit that was just recorded (before: only leadership lists asked).
 *  - A clip filmed before the visit's day (`409 visit_not_open_yet`) is NOT accepted: it stays a
 *    visible terminal refusal carrying the server's own code and sentence, and the task is
 *    re-read so the card shows the backend's reason and a closed camera.
 */
class PenVisitSubmitSettleTest {

    private class RecordingPenVisits : PenVisitsRepository {
        val persisted = mutableListOf<PenVisitDetailDto>()
        val refreshed = mutableListOf<String>()
        override fun visits(filter: String): Flow<PagingData<PenVisitDto>> = flowOf(PagingData.empty())
        override val pageMeta: StateFlow<PenVisitPageMeta> = MutableStateFlow(PenVisitPageMeta())
        override suspend fun invalidateVisits(filter: String) = Unit
        override fun observeVisit(taskId: String): Flow<PenVisitDto?> = flowOf(null)
        override suspend fun refreshVisit(taskId: String): Boolean {
            refreshed += taskId
            return true
        }
        override suspend fun persistServerDetail(detail: PenVisitDetailDto) {
            persisted += detail
        }
    }

    private fun refusal(code: String, message: String): HttpException = HttpException(
        Response.error<Unit>(409, """{"code":"$code","message":"$message"}""".toResponseBody("application/json".toMediaType())),
    )

    private fun api(submitError: HttpException?, fresh: PenVisitDto): AppApi = object : AppApi by FakeAppApi() {
        override suspend fun submitPenVisit(idempotencyKey: String, taskId: String, request: PenVisitSubmitRequestDto): PenVisitDetailDto {
            submitError?.let { throw it }
            return PenVisitDetailDto(task = fresh)
        }
        override suspend fun getPenVisit(taskId: String): PenVisitDetailDto = PenVisitDetailDto(task = fresh)
    }

    private fun seed(store: FakeOutboxStore) = runBlocking {
        store.insert(
            OutboxEntity(
                id = "proof-1",
                opType = OutboxOpType.PROOF_UPLOAD.name,
                groupKey = penVisitTaskGroupKey(TASK),
                idempotencyKey = "proof-key-1",
                payloadJson = "{}",
                status = OutboxStatus.SUCCEEDED.name,
                createdAt = 0L,
                updatedAt = 0L,
                resultJson = syncJson.encodeToString(ProofUploadResponseDto(proof = ProofReferenceDto(proofId = "srv-proof-1"))),
            ),
        )
        store.insert(
            OutboxEntity(
                id = SUBMIT_ID,
                opType = OutboxOpType.PEN_VISIT_SUBMIT.name,
                groupKey = penVisitTaskGroupKey(TASK),
                idempotencyKey = penVisitSubmitIdempotencyKey(TASK, 3),
                payloadJson = syncJson.encodeToString(PenVisitSubmitPayload(taskId = TASK, rowVersion = 3, proofOutboxItemId = "proof-1")),
                status = OutboxStatus.QUEUED.name,
                createdAt = 1L,
                updatedAt = 1L,
            ),
        )
    }

    private fun engine(store: FakeOutboxStore, api: AppApi, visits: RecordingPenVisits, navRefreshes: MutableList<Unit>) = SyncEngine(
        store,
        api,
        connectivityGate = { true },
        clock = { 10L },
        penVisitsRepository = visits,
        postSuccessRefreshHooks = mapOf(
            OutboxOpType.PEN_VISIT_SUBMIT to penVisitSubmitSuccessHook { navRefreshes += Unit },
        ),
        postTerminalFailureHooks = mapOf(
            OutboxOpType.PEN_VISIT_SUBMIT to penVisitSubmitFailureHook(visits),
        ),
    )

    @Test
    fun `a second visitor's submit refused as already in review settles instead of dead-lettering`() = runBlocking {
        val store = FakeOutboxStore()
        seed(store)
        val visits = RecordingPenVisits()
        val navRefreshes = mutableListOf<Unit>()
        val inReview = PenVisitDto(taskId = TASK, status = "pending_verification", rowVersion = 4)
        engine(store, api(refusal("visit_in_review", "The visit video is with the verifier."), inReview), visits, navRefreshes).drainOnce()

        val row = store.findById(SUBMIT_ID)!!
        assertEquals("the pen is visited: the row settles", OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals("pending_verification", visits.persisted.single().task.status)
        assertEquals("the badge is re-read once the visit is recorded", 1, navRefreshes.size)
    }

    @Test
    fun `a submit that lands asks the shell to re-read the For me badge`() = runBlocking {
        val store = FakeOutboxStore()
        seed(store)
        val visits = RecordingPenVisits()
        val navRefreshes = mutableListOf<Unit>()
        engine(store, api(null, PenVisitDto(taskId = TASK, status = "pending_verification", rowVersion = 4)), visits, navRefreshes).drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById(SUBMIT_ID)!!.status)
        assertEquals(1, navRefreshes.size)
    }

    @Test
    fun `a clip filmed before the visit's day stays a visible refusal and re-reads the task`() = runBlocking {
        val store = FakeOutboxStore()
        seed(store)
        val visits = RecordingPenVisits()
        val navRefreshes = mutableListOf<Unit>()
        val notYet = PenVisitDto(taskId = TASK, status = "open", canSubmit = false, rowVersion = 3)
        engine(store, api(refusal("visit_not_open_yet", "This visit opens on 26/09/2026."), notYet), visits, navRefreshes).drainOnce()

        val row = store.findById(SUBMIT_ID)!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue("terminal, never retried against a date that has not come", row.conflict)
        assertEquals("visit_not_open_yet", row.lastErrorCode)
        assertEquals("This visit opens on 26/09/2026.", row.lastError)
        assertEquals("the task is re-read so the card shows the server's reason", listOf(TASK), visits.refreshed)
        assertTrue(navRefreshes.isEmpty())
    }

    private companion object {
        const val TASK = "task-1"
        const val SUBMIT_ID = "submit-1"
    }
}
