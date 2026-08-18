package sg.mesha.goatos.core.data.sync

import androidx.paging.PagingData
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.HealthFilters
import sg.mesha.goatos.core.data.HealthRepository
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.HealthCompleteResponseDto
import sg.mesha.goatos.core.network.dto.HealthOpenCaseResponseDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDetailDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemDto
import sg.mesha.goatos.core.network.dto.HealthWorkItemPageDto

class HealthOutboxConvergenceTest {
    private class RecordingHealthRepository(
        private val store: OutboxStore? = null,
    ) : HealthRepository {
        val listRefreshes = mutableListOf<HealthFilters>()
        val listRefreshOutboxStatuses = mutableListOf<String?>()
        val detailRefreshes = mutableListOf<String>()
        val successfulCompletionReconciles = mutableListOf<String>()
        val rejectedCompletionReconciles = mutableListOf<String>()

        override fun workItems(filters: HealthFilters): Flow<PagingData<HealthWorkItemDto>> =
            flowOf(PagingData.empty())

        override fun observePageMeta(filters: HealthFilters): Flow<HealthWorkItemPageDto?> = flowOf(null)
        override fun observeDetail(healthSessionId: String): Flow<HealthWorkItemDetailDto?> = flowOf(null)

        override suspend fun refreshWorkItems(filters: HealthFilters): Result<Unit> {
            listRefreshes += filters
            listRefreshOutboxStatuses += store?.findById("health-case-row")?.status
            return Result.success(Unit)
        }

        override suspend fun refreshCaseOptions(ageBand: String, date: String): Result<Unit> = Result.success(Unit)

        override suspend fun refreshDetail(healthSessionId: String): Result<Unit> {
            detailRefreshes += healthSessionId
            return Result.success(Unit)
        }

        override suspend fun markCompleted(healthSessionId: String) = Unit

        override suspend fun reconcileSuccessfulTreatmentCompletion(healthSessionId: String): Result<Unit> {
            successfulCompletionReconciles += healthSessionId
            return Result.success(Unit)
        }

        override suspend fun reconcileRejectedTreatmentCompletion(healthSessionId: String): Result<Unit> {
            rejectedCompletionReconciles += healthSessionId
            return Result.success(Unit)
        }
    }

    @Test
    fun `case-open success refreshes base and disease lists before the pending row retracts`() = runBlocking {
        val store = FakeOutboxStore()
        val repository = RecordingHealthRepository(store)
        store.insert(caseOpenItem())
        val api = ScriptedAppApi().apply {
            openHealthCaseFn = { _, _ ->
                HealthOpenCaseResponseDto(caseId = "case-1", firstSessionId = "session-1", sessionCount = 3)
            }
        }
        val engine = SyncEngine(
            store = store,
            api = api,
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.HEALTH_CASE_OPEN to healthCaseOpenRefreshHook(repository),
            ),
            preSuccessRefreshHooks = mapOf(
                OutboxOpType.HEALTH_CASE_OPEN to healthCaseOpenRefreshHook(repository),
            ),
        )

        engine.drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("health-case-row")?.status)
        assertEquals(
            listOf(
                HealthFilters(ageBand = "adult", date = "2026-08-18"),
                HealthFilters(ageBand = "adult", date = "2026-08-18", diseaseKey = "fever"),
            ),
            repository.listRefreshes,
        )
        assertEquals(
            listOf(OutboxStatus.IN_FLIGHT.name, OutboxStatus.IN_FLIGHT.name),
            repository.listRefreshOutboxStatuses,
        )
    }

    @Test
    fun `case-open success is reconciled by a new engine after process death`() = runBlocking {
        val repository = RecordingHealthRepository()
        val store = FakeOutboxStore()
        store.insert(caseOpenItem(status = OutboxStatus.SUCCEEDED, resultJson = "{}"))
        val restartedEngine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.HEALTH_CASE_OPEN to healthCaseOpenRefreshHook(repository),
            ),
        )

        restartedEngine.drainOnce()

        assertEquals(2, repository.listRefreshes.size)
    }

    @Test
    fun `treatment success refreshes canonical detail after process death`() = runBlocking {
        val repository = RecordingHealthRepository()
        val store = FakeOutboxStore()
        store.insert(treatmentItem(status = OutboxStatus.SUCCEEDED, resultJson = "{}"))
        val restartedEngine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.HEALTH_TREATMENT_COMPLETE to healthTreatmentCompleteRefreshHook(repository),
            ),
        )

        restartedEngine.drainOnce()

        assertEquals(listOf("health-session-1"), repository.successfulCompletionReconciles)
    }

    @Test
    fun `terminal treatment rejection invokes rollback during the failing drain`() = runBlocking {
        val repository = RecordingHealthRepository()
        val store = FakeOutboxStore()
        store.insert(treatmentItem())
        val api = ScriptedAppApi().apply {
            completeHealthWorkItemFn = { _, _, _ -> throw NonRetryableSyncException("rejected") }
        }
        val engine = SyncEngine(
            store = store,
            api = api,
            connectivityGate = { true },
            postTerminalFailureHooks = mapOf(
                OutboxOpType.HEALTH_TREATMENT_COMPLETE to healthTreatmentCompleteFailureHook(repository),
            ),
        )

        engine.drainOnce()

        val terminal = store.findById("health-treatment-row")!!
        assertEquals(OutboxStatus.FAILED.name, terminal.status)
        assertEquals(true, terminal.conflict)
        assertEquals(listOf("health-session-1"), repository.rejectedCompletionReconciles)
    }

    @Test
    fun `terminal treatment rejection replays rollback after process death`() = runBlocking {
        val repository = RecordingHealthRepository()
        val store = FakeOutboxStore()
        store.insert(treatmentItem(status = OutboxStatus.FAILED, conflict = true))
        val restartedEngine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postTerminalFailureHooks = mapOf(
                OutboxOpType.HEALTH_TREATMENT_COMPLETE to healthTreatmentCompleteFailureHook(repository),
            ),
        )

        restartedEngine.drainOnce()

        assertEquals(listOf("health-session-1"), repository.rejectedCompletionReconciles)
    }

    private fun caseOpenItem(
        status: OutboxStatus = OutboxStatus.QUEUED,
        resultJson: String? = null,
    ) = OutboxEntity(
        id = "health-case-row",
        opType = OutboxOpType.HEALTH_CASE_OPEN.name,
        groupKey = "goat-1",
        idempotencyKey = "health-case-key",
        payloadJson = syncJson.encodeToString(
            HealthCaseOpenPayload(
                goatId = "goat-1",
                diseaseKey = "fever",
                ageBand = "adult",
                startDate = "2026-08-18",
                goatDisplayId = "G-1",
                diseaseName = "Fever",
            ),
        ),
        status = status.name,
        attemptCount = if (status == OutboxStatus.QUEUED) 0 else 1,
        maxAttempts = 3,
        conflict = false,
        createdAt = 1L,
        updatedAt = 2L,
        nextAttemptAt = if (status == OutboxStatus.QUEUED) 0L else Long.MAX_VALUE,
        lastError = null,
        resultJson = resultJson,
    )

    private fun treatmentItem(
        status: OutboxStatus = OutboxStatus.QUEUED,
        conflict: Boolean = false,
        resultJson: String? = null,
    ) = OutboxEntity(
        id = "health-treatment-row",
        opType = OutboxOpType.HEALTH_TREATMENT_COMPLETE.name,
        groupKey = "health-session-1",
        idempotencyKey = "health-treatment-key",
        payloadJson = syncJson.encodeToString(HealthTreatmentCompletePayload("health-session-1")),
        status = status.name,
        attemptCount = if (status == OutboxStatus.QUEUED) 0 else 1,
        maxAttempts = 3,
        conflict = conflict,
        createdAt = 1L,
        updatedAt = 2L,
        nextAttemptAt = if (status == OutboxStatus.QUEUED) 0L else Long.MAX_VALUE,
        lastError = if (status == OutboxStatus.FAILED) "rejected" else null,
        resultJson = resultJson,
    )
}
