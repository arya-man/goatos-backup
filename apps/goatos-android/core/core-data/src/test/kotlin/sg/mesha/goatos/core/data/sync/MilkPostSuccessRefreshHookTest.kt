package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.MilkPreparationRepository
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto

/**
 * MILK_FEEDING_SUBMIT / MILK_PREPARATION_SUBMIT reconcile via [PostSuccessRefreshHook], not a
 * server-truth row write: the observed cache is a whole-page KV blob
 * (`CountsBreakdownMetaCacheDao`, see [sg.mesha.goatos.core.data.MilkFeedingRepository] /
 * [sg.mesha.goatos.core.data.MilkPreparationRepository]), so there is no per-row target to write
 * into — the correct reconcile is "go fetch the page again".
 *
 * RED before this fix: `postSuccessRefreshHooks` did not exist, so a drain success left the
 * milk lists/detail showing the stale pre-submit state until a manual pull-to-refresh.
 * GREEN after: `SyncEngine.reconcileFeatureSuccess` invokes the registered hook, which calls
 * `repository.refresh(...)` with the exact page key the submit targeted.
 */
class MilkPostSuccessRefreshHookTest {

    private class RecordingMilkFeedingRepository : MilkFeedingRepository {
        val refreshCalls = mutableListOf<Triple<String, String, Int?>>()
        override fun observe(feedingDate: String, parkId: String, sessionNo: Int?): Flow<Resource<MilkFeedingPageDto>> =
            flowOf(Resource(data = null))
        override suspend fun refresh(feedingDate: String, parkId: String, sessionNo: Int?): Result<Unit> {
            refreshCalls += Triple(feedingDate, parkId, sessionNo)
            return Result.success(Unit)
        }
    }

    private class RecordingMilkPreparationRepository : MilkPreparationRepository {
        val refreshCalls = mutableListOf<String>()
        override fun observe(preparationDate: String): Flow<Resource<MilkPreparationPageDto>> =
            flowOf(Resource(data = null))
        override suspend fun refresh(preparationDate: String): Result<Unit> {
            refreshCalls += preparationDate
            return Result.success(Unit)
        }
    }

    @Test
    fun `MILK_FEEDING_SUBMIT success triggers a page refresh for the submitted key`() = runBlocking {
        val milkFeedingRepo = RecordingMilkFeedingRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.MILK_FEEDING_SUBMIT to milkFeedingSubmitRefreshHook(milkFeedingRepo),
            ),
        )

        val payload = MilkFeedingSubmitPayload(
            taskId = "task-1",
            parkId = "park-1",
            feedingDate = "2026-08-16",
            sessionNo = 1,
            answers = sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto(
                watchlistAnswers = emptyList(),
                totalKidsFed = 10,
                attempt1NotDrinking = 0,
                attempt2NotDrinking = 0,
                newRefusals = emptyList(),
                udderMilkNotDrinking = 0,
                orsNotDrinking = 0,
            ),
            cleanBottlesProofOutboxItemId = "proof-1",
            mixingAndFillingProofOutboxItemId = "proof-2",
        )

        val item = OutboxEntity(
            id = "item-milk-feeding-1",
            opType = OutboxOpType.MILK_FEEDING_SUBMIT.name,
            groupKey = "task-1",
            idempotencyKey = "key-1",
            payloadJson = syncJson.encodeToString(payload),
            status = OutboxStatus.SUCCEEDED.name,
            attemptCount = 1,
            maxAttempts = 3,
            conflict = false,
            createdAt = 0L,
            updatedAt = 1000L,
            nextAttemptAt = Long.MAX_VALUE,
            lastError = null,
            resultJson = "{}",
        )

        store.insert(item)
        engine.drainOnce()

        assertEquals(
            "MILK_FEEDING_SUBMIT success should refresh the exact submitted feedingDate/parkId/sessionNo page",
            listOf(Triple("2026-08-16", "park-1", 1)),
            milkFeedingRepo.refreshCalls,
        )
    }

    @Test
    fun `MILK_PREPARATION_SUBMIT success triggers a page refresh for the submitted date`() = runBlocking {
        val milkPrepRepo = RecordingMilkPreparationRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.MILK_PREPARATION_SUBMIT to milkPreparationSubmitRefreshHook(milkPrepRepo),
            ),
        )

        val payload = MilkPreparationSubmitPayload(
            parkId = "park-1",
            preparationDate = "2026-08-16",
            goatMilkUsed = true,
            answers = MilkPreparationAnswersPayload(
                morningMilkCollectedLitres = 1.0,
                eveningMilkCollectedLitres = 1.0,
                goatMilkQuantityLitres = 1.0,
                boilingTemperatureC = 90.0,
                cooledTemperatureC = 40.0,
                uhtMilkQuantityLitres = 1.0,
                citricAcidGrams = 2.0,
            ),
            proofOutboxItemIds = mapOf(
                "uht_milk_quantity" to "proof-1",
                "citric_acid_mixing" to "proof-2",
            ),
        )

        val item = OutboxEntity(
            id = "item-milk-prep-1",
            opType = OutboxOpType.MILK_PREPARATION_SUBMIT.name,
            groupKey = "prep-park-1",
            idempotencyKey = "key-2",
            payloadJson = syncJson.encodeToString(payload),
            status = OutboxStatus.SUCCEEDED.name,
            attemptCount = 1,
            maxAttempts = 3,
            conflict = false,
            createdAt = 0L,
            updatedAt = 1000L,
            nextAttemptAt = Long.MAX_VALUE,
            lastError = null,
            resultJson = "{}",
        )

        store.insert(item)
        engine.drainOnce()

        assertEquals(
            "MILK_PREPARATION_SUBMIT success should refresh the exact submitted preparationDate page",
            listOf("2026-08-16"),
            milkPrepRepo.refreshCalls,
        )
    }
}
