package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.weighing.WeighingFastingCardDao
import sg.mesha.goatos.core.data.weighing.WeighingFastingCardEntity
import sg.mesha.goatos.core.data.weighing.WeighingFastingRemoteKeyEntity
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardResponseDto

/**
 * A RETRIED submit replays the server's ORIGINAL snapshot, which can be OLDER than what a
 * list refresh has since cached (the verifier may already have sent this shed back). Mirroring
 * that stale snapshot over newer state regressed a sent-back card to "Submitted" on a real
 * phone (2026-09-03). The reconcile's row_version fence keeps the newest row — now at the
 * per-shed grain (maintainer correction #2: one card per shed, one submit per shed).
 * Mutation-tested: deleting the fence in SyncEngine turns the first case red.
 */
class WeighingFastingReconcileFenceTest {

    private class FakeFastingDao : WeighingFastingCardDao {
        val rows = mutableMapOf<Pair<String, String>, WeighingFastingCardEntity>()
        override suspend fun upsertAll(rowsIn: List<WeighingFastingCardEntity>) {
            rowsIn.forEach { rows[it.fastingTaskId to it.campaignShedId] = it }
        }
        override suspend fun upsert(row: WeighingFastingCardEntity) {
            rows[row.fastingTaskId to row.campaignShedId] = row
        }
        override fun observeWindow(limit: Int): Flow<List<WeighingFastingCardEntity>> = flowOf(rows.values.toList())
        override fun observeCard(fastingTaskId: String, campaignShedId: String): Flow<WeighingFastingCardEntity?> =
            flowOf(rows[fastingTaskId to campaignShedId])
        override suspend fun getCard(fastingTaskId: String, campaignShedId: String): WeighingFastingCardEntity? =
            rows[fastingTaskId to campaignShedId]
        override suspend fun deleteAll() { rows.clear() }
        override suspend fun maxSortIndex(): Long? = rows.values.maxOfOrNull { it.sortIndex }
        override suspend fun upsertRemoteKey(key: WeighingFastingRemoteKeyEntity) {}
        override suspend fun remoteKey(scopeKey: String): WeighingFastingRemoteKeyEntity? = null
        override fun observeRemoteKey(scopeKey: String): Flow<WeighingFastingRemoteKeyEntity?> = flowOf(null)
    }

    private fun dto(status: String, rowVersion: Int) = WeighingFastingShedCardDto(
        fastingTaskId = "task-1",
        campaignShedId = "shed-1",
        shedLabel = "Castro 1",
        subjectLabel = "Remove feed & water · Castro 1",
        plannedWeighDate = "2026-09-04",
        weighBusinessDate = "2026-09-04",
        removalBusinessDate = "2026-09-03",
        status = status,
        rowVersion = rowVersion,
    )

    private fun cached(dao: FakeFastingDao, status: String, rowVersion: Int) = runBlocking {
        dao.upsert(
            WeighingFastingCardEntity(
                fastingTaskId = "task-1",
                campaignShedId = "shed-1",
                sortIndex = 0L,
                status = status,
                removalBusinessDate = "2026-09-03",
                dtoJson = syncJson.encodeToString(WeighingFastingShedCardDto.serializer(), dto(status, rowVersion)),
                updatedAt = 0L,
            ),
        )
    }

    private fun succeededSubmit(responseStatus: String, responseVersion: Int) = OutboxEntity(
        id = "submit-1",
        opType = OutboxOpType.WEIGHING_FASTING_SUBMIT.name,
        groupKey = "weighing-fasting:task-1:shed-1",
        idempotencyKey = "submit-key",
        payloadJson = syncJson.encodeToString(
            WeighingFastingSubmitPayload.serializer(),
            WeighingFastingSubmitPayload(
                fastingTaskId = "task-1",
                campaignShedId = "shed-1",
                feedProofOutboxItemId = "feed-1",
                waterProofOutboxItemId = "water-1",
            ),
        ),
        status = OutboxStatus.SUCCEEDED.name,
        attemptCount = 1,
        maxAttempts = 8,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = syncJson.encodeToString(
            WeighingFastingShedCardResponseDto.serializer(),
            WeighingFastingShedCardResponseDto(fastingShedCard = dto(responseStatus, responseVersion)),
        ),
    )

    @Test
    fun `a stale replayed snapshot never clobbers newer cached state`() = runBlocking {
        val dao = FakeFastingDao()
        cached(dao, status = "rework", rowVersion = 3)
        val store = FakeOutboxStore()
        val engine = SyncEngine(store = store, api = ScriptedAppApi(), connectivityGate = { true }, weighingFastingCardDao = dao)

        engine.reconcileSucceededForTest(succeededSubmit(responseStatus = "pending_verification", responseVersion = 2))

        val kept = dao.rows["task-1" to "shed-1"]!!
        assertEquals("the newer rework row must survive a stale replay's mirror", "rework", kept.status)
    }

    @Test
    fun `a newer response still mirrors into the cache`() = runBlocking {
        val dao = FakeFastingDao()
        cached(dao, status = "open", rowVersion = 1)
        val store = FakeOutboxStore()
        val engine = SyncEngine(store = store, api = ScriptedAppApi(), connectivityGate = { true }, weighingFastingCardDao = dao)

        engine.reconcileSucceededForTest(succeededSubmit(responseStatus = "pending_verification", responseVersion = 2))

        assertEquals("pending_verification", dao.rows["task-1" to "shed-1"]!!.status)
    }
}
