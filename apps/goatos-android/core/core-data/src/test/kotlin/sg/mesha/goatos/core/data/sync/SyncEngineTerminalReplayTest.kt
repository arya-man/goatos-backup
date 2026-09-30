package sg.mesha.goatos.core.data.sync

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.PenVisitPageMeta
import sg.mesha.goatos.core.data.PenVisitsRepository
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.PenVisitDetailDto
import sg.mesha.goatos.core.network.dto.PenVisitDto

/**
 * A SUCCEEDED outbox row's stored response is the server's truth AT THE TIME THE WRITE LANDED.
 * The drain replays it into Room so a process that died between markSucceeded and the Room
 * reconcile still repairs itself -- but ONCE PER PROCESS, never once per pass.
 *
 * Found on the phone 2026-09-07: with the API down, a pen visit's new upload sat retrying while
 * every drain pass re-applied the EARLIER submit's completed payload over the fresh detail, so the
 * screen read "Submitted" above a video that had not gone through. Mutation-tested when written:
 * removing the replayedTerminals check in drainOnce turns the second assertion red.
 */
class SyncEngineTerminalReplayTest {

    private class CountingPenVisits : PenVisitsRepository {
        var persisted = 0
        override fun visits(filter: String): Flow<PagingData<PenVisitDto>> = flowOf(PagingData.empty())
        override val pageMeta: StateFlow<PenVisitPageMeta> = MutableStateFlow(PenVisitPageMeta())
        override suspend fun invalidateVisits(filter: String) = Unit
        override fun observeVisit(taskId: String): Flow<PenVisitDto?> = flowOf(null)
        override suspend fun refreshVisit(taskId: String) = true
        override suspend fun persistServerDetail(detail: PenVisitDetailDto) {
            persisted += 1
        }
    }

    private fun succeededSubmit(id: String = "submit-1") = OutboxEntity(
        id = id,
        opType = OutboxOpType.PEN_VISIT_SUBMIT.name,
        groupKey = penVisitTaskGroupKey("task-1"),
        idempotencyKey = penVisitSubmitIdempotencyKey("task-1", 3),
        payloadJson = syncJson.encodeToString(
            PenVisitSubmitPayload(taskId = "task-1", rowVersion = 3, proofOutboxItemId = "proof-1"),
        ),
        status = OutboxStatus.SUCCEEDED.name,
        attemptCount = 1,
        maxAttempts = DEFAULT_MAX_ATTEMPTS,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = syncJson.encodeToString(
            PenVisitDetailDto(task = PenVisitDto(taskId = "task-1", workState = "completed", rowVersion = 4)),
        ),
    )

    @Test
    fun `a succeeded terminal is replayed into Room once per process, not on every drain`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(succeededSubmit())
        val visits = CountingPenVisits()
        val engine = SyncEngine(
            store,
            ScriptedAppApi(),
            connectivityGate = { true },
            clock = { 0L },
            penVisitsRepository = visits,
        )

        engine.drainOnce()
        assertEquals("first pass after a (re)start repairs Room from the stored response", 1, visits.persisted)

        engine.drainOnce()
        engine.drainOnce()
        assertEquals("later passes in the same process must not re-apply the stale copy", 1, visits.persisted)
    }

    @Test
    fun `a fresh process replays a recent terminal exactly once more`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(succeededSubmit())
        val first = CountingPenVisits()
        SyncEngine(store, ScriptedAppApi(), connectivityGate = { true }, clock = { 0L }, penVisitsRepository = first).drainOnce()
        // A new engine instance is what a process restart looks like to this seam.
        val second = CountingPenVisits()
        val restarted = SyncEngine(store, ScriptedAppApi(), connectivityGate = { true }, clock = { 0L }, penVisitsRepository = second)
        restarted.drainOnce()
        restarted.drainOnce()
        assertEquals(1, first.persisted)
        assertEquals(1, second.persisted)
    }

    private class CountingPenRoutines : sg.mesha.goatos.core.data.PenRoutinesRepository {
        var persisted = 0
        val rowVersions = mutableListOf<Int>()
        override fun tasks(query: sg.mesha.goatos.core.data.PenRoutineQuery): Flow<PagingData<sg.mesha.goatos.core.network.dto.PenRoutineTaskDto>> = flowOf(PagingData.empty())
        override fun pageMeta(tab: String): Flow<sg.mesha.goatos.core.data.PenRoutinePageMeta> = flowOf(sg.mesha.goatos.core.data.PenRoutinePageMeta())
        override suspend fun invalidateTasks(query: sg.mesha.goatos.core.data.PenRoutineQuery) = Unit
        override fun observeTask(taskId: String): Flow<sg.mesha.goatos.core.network.dto.PenRoutineTaskDto?> = flowOf(null)
        override suspend fun refreshTask(taskId: String) = Unit
        override suspend fun persistServerDetail(detail: sg.mesha.goatos.core.network.dto.PenRoutineDetailDto) {
            persisted += 1
            rowVersions += detail.task.rowVersion
        }
    }

    private fun succeededRoutineWrite(id: String, opType: OutboxOpType, payloadJson: String, resultRowVersion: Int) = OutboxEntity(
        id = id,
        opType = opType.name,
        groupKey = penRoutineTaskGroupKey("task-r"),
        idempotencyKey = "$id:key",
        payloadJson = payloadJson,
        status = OutboxStatus.SUCCEEDED.name,
        attemptCount = 1,
        maxAttempts = DEFAULT_MAX_ATTEMPTS,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = syncJson.encodeToString(
            sg.mesha.goatos.core.network.dto.PenRoutineDetailDto(
                task = sg.mesha.goatos.core.network.dto.PenRoutineTaskDto(taskId = "task-r", rowVersion = resultRowVersion),
            ),
        ),
    )

    /** The routine module's two writes ride the same once-per-process replay seam (maintainer
     *  instruction 2026-09-16): the presence punch's and the submit's stored Steps each reach Room
     *  exactly once after a (re)start, never on every pass. */
    @Test
    fun `routine presence and submit terminals are replayed into Room once per process`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            succeededRoutineWrite(
                id = "presence-1",
                opType = OutboxOpType.PEN_ROUTINE_PRESENCE,
                payloadJson = syncJson.encodeToString(
                    PenRoutinePresencePayload(
                        taskId = "task-r",
                        rowVersion = 3,
                        eventType = "enter",
                        capturedAt = "2026-09-16T01:30:00Z",
                        location = sg.mesha.goatos.core.network.dto.PenRoutineLocationDto(status = "captured"),
                        integrity = sg.mesha.goatos.core.network.dto.PenRoutineIntegrityDto(),
                    ),
                ),
                resultRowVersion = 4,
            ),
        )
        store.insert(
            succeededRoutineWrite(
                id = "submit-1",
                opType = OutboxOpType.PEN_ROUTINE_SUBMIT,
                payloadJson = syncJson.encodeToString(
                    PenRoutineSubmitPayload(
                        taskId = "task-r",
                        rowVersion = 4,
                        answers = kotlinx.serialization.json.JsonObject(emptyMap()),
                        proofs = emptyList(),
                    ),
                ),
                resultRowVersion = 5,
            ),
        )
        val routines = CountingPenRoutines()
        val engine = SyncEngine(store, ScriptedAppApi(), connectivityGate = { true }, clock = { 0L }, penRoutinesRepository = routines)

        engine.drainOnce()
        assertEquals(2, routines.persisted)
        assertEquals(listOf(4, 5), routines.rowVersions)

        engine.drainOnce()
        engine.drainOnce()
        assertEquals("later passes in the same process must not re-apply the stale copies", 2, routines.persisted)
    }

    @Test
    fun `bounded key set evicts its oldest key at capacity`() {
        val set = BoundedKeySet(capacity = 2)
        assertEquals(true, set.add("a"))
        assertEquals(true, set.add("b"))
        assertEquals(false, set.add("a"))
        assertEquals(true, set.add("c"))
        assertEquals(false, set.contains("a"))
        assertEquals(true, set.contains("b"))
        assertEquals(true, set.contains("c"))
    }
}
