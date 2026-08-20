package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.AwaitingRfidFilter
import sg.mesha.goatos.core.data.AwaitingRfidRepository
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto
import sg.mesha.goatos.core.network.dto.CountsBreedsResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.TemporaryTaggedGoatDto
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import androidx.paging.PagingData

/**
 * Counts-family operations (birth, death, shifting, approval, promote) reconcile via
 * [PostSuccessRefreshHook], not via direct server-truth row writes — the observed caches are
 * whole-page KV blobs (herd summary, approval lists, shifting pending lists), so there is no
 * per-row target to write into. The correct reconcile is "go fetch the affected page(s) again"
 * or "forget the affected rows from the local cache".
 *
 * RED before these hooks: drain successes left the affected lists showing the stale pre-submit
 * state until a manual pull-to-refresh.
 *
 * GREEN after: `SyncEngine.reconcileFeatureSuccess` invokes the registered hooks, which call
 * `repository.refresh(...)` or `repository.forget(...)` with the exact affected cache keys.
 */
class CountsPostSuccessRefreshHookTest {

    private class RecordingAwaitingRfidRepository : AwaitingRfidRepository {
        val forgottenGoatIds = mutableListOf<String>()

        override fun awaiting(filter: AwaitingRfidFilter): Flow<PagingData<TemporaryTaggedGoatDto>> =
            flowOf(PagingData.empty())

        override suspend fun forgetPromoted(goatId: String) {
            forgottenGoatIds += goatId
        }

        override suspend fun findCached(goatId: String): TemporaryTaggedGoatDto? = null
    }

    private class RecordingCountsRepository : CountsRepository {
        val refreshHerdSummaryCalls = mutableListOf<Unit>()
        val refreshBirthBreedsCalls = mutableListOf<Unit>()
        val refreshShiftingDestinationsCalls = mutableListOf<Unit>()

        override fun observeHerdSummary(
            lifecycleStatus: String?,
            parkId: String?,
            breed: String?,
            sex: String?,
        ): Flow<Resource<HerdRegisterSummaryResponseDto>> = flowOf(Resource(data = null))

        override suspend fun refreshHerdSummary(
            lifecycleStatus: String?,
            parkId: String?,
            breed: String?,
            sex: String?,
        ): Result<Unit> {
            refreshHerdSummaryCalls += Unit
            return Result.success(Unit)
        }

        override fun observeBreakdownTotals(query: sg.mesha.goatos.core.data.CountsBreakdownQuery):
                Flow<Resource<CountsBreakdownResponseDto>> = flowOf(Resource(data = null))

        override fun observeBirthBreeds(): Flow<Resource<CountsBreedsResponseDto>> =
            flowOf(Resource(data = null))

        override suspend fun refreshBirthBreeds(): Result<Unit> {
            refreshBirthBreedsCalls += Unit
            return Result.success(Unit)
        }

        override fun breakdownRows(query: sg.mesha.goatos.core.data.CountsBreakdownQuery):
                Flow<PagingData<sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto>> =
            flowOf(PagingData.empty())

        override fun observeShiftingDestinations():
                Flow<Resource<CountsShiftingDestinationsResponseDto>> = flowOf(Resource(data = null))

        override suspend fun refreshShiftingDestinations(): Result<Unit> {
            refreshShiftingDestinationsCalls += Unit
            return Result.success(Unit)
        }

        override suspend fun lookupAnimals(
            query: String,
            parkId: String?,
            shedId: String?,
        ): Result<List<sg.mesha.goatos.core.network.dto.GoatSearchItemDto>> =
            Result.success(emptyList())
        override suspend fun kidStageDue(): Result<sg.mesha.goatos.core.network.dto.KidStageDueResponseDto> =
            Result.success(sg.mesha.goatos.core.network.dto.KidStageDueResponseDto())
    }

    private class RecordingCountsApprovalRepository : CountsApprovalRepository {
        val forgetDecidedCalls = mutableListOf<String>()

        override fun approvals(status: String): Flow<PagingData<CountsApprovalListItemDto>> =
            flowOf(PagingData.empty())

        override suspend fun forgetDecided(approvalRequestId: String) {
            forgetDecidedCalls += approvalRequestId
        }
    }

    private class RecordingShiftingPendingRepository : ShiftingPendingRepository {
        val forgetExecutedCalls = mutableListOf<String>()
        override val actionsMeta = kotlinx.coroutines.flow.MutableStateFlow(
            sg.mesha.goatos.core.data.ShiftingActionsMeta()
        )

        override fun pending(
            date: String,
            status: String,
        ): Flow<PagingData<CountsShiftingPendingExecutionItemDto>> = flowOf(PagingData.empty())

        override suspend fun forgetExecuted(shiftingEventId: String) {
            forgetExecutedCalls += shiftingEventId
        }

        override suspend fun findCached(shiftingEventId: String): CountsShiftingPendingExecutionItemDto? =
            null
    }

    @Test
    fun `COUNTS_SHIFTING success refreshes destinations and herd summary`() = runBlocking {
        val countsRepo = RecordingCountsRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.COUNTS_SHIFTING to countsShiftingRefreshHook(countsRepo),
            ),
        )

        val payload = CountsShiftingPayload(
            request = CountsShiftingEventRequestDto(
                destinationParkId = "park-1",
                destinationShedId = "shed-2",
                goatIds = listOf("goat-1", "goat-2"),
                category = "management",
            ),
        )

        val item = OutboxEntity(
            id = "item-shifting-1",
            opType = OutboxOpType.COUNTS_SHIFTING.name,
            groupKey = "shed-2",
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
            "COUNTS_SHIFTING success should refresh destinations",
            listOf(Unit),
            countsRepo.refreshShiftingDestinationsCalls,
        )
        assertEquals(
            "COUNTS_SHIFTING success should refresh herd summary",
            listOf(Unit),
            countsRepo.refreshHerdSummaryCalls,
        )
    }

    @Test
    fun `COUNTS_BIRTH success refreshes herd summary and birth breeds`() = runBlocking {
        val countsRepo = RecordingCountsRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.COUNTS_BIRTH to countsBirthRefreshHook(countsRepo),
            ),
        )

        val payload = CountsBirthPayload(
            request = CountsBirthEventRequestDto(
                shedId = "shed-1",
                sex = "M",
                dob = "2026-08-16",
                species = "goat",
                entryDate = "2026-08-16",
                damId = "dam-1",
                litterSize = 1,
            ),
        )

        val item = OutboxEntity(
            id = "item-birth-1",
            opType = OutboxOpType.COUNTS_BIRTH.name,
            groupKey = "shed-1",
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
            "COUNTS_BIRTH success should refresh herd summary",
            listOf(Unit),
            countsRepo.refreshHerdSummaryCalls,
        )
        assertEquals(
            "COUNTS_BIRTH success should refresh birth breeds",
            listOf(Unit),
            countsRepo.refreshBirthBreedsCalls,
        )
    }

    @Test
    fun `COUNTS_DEATH success refreshes herd summary`() = runBlocking {
        val countsRepo = RecordingCountsRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.COUNTS_DEATH to countsDeathRefreshHook(countsRepo),
            ),
        )

        val payload = CountsDeathPayload(
            request = CountsDeathEventRequestDto(
                goatId = "goat-1",
                reason = "health issue",
                rowVersion = 1,
            ),
        )

        val item = OutboxEntity(
            id = "item-death-1",
            opType = OutboxOpType.COUNTS_DEATH.name,
            groupKey = "goat-1",
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
            "COUNTS_DEATH success should refresh herd summary",
            listOf(Unit),
            countsRepo.refreshHerdSummaryCalls,
        )
    }

    @Test
    fun `COUNTS_APPROVAL_APPROVE success forgets the decided request`() = runBlocking {
        val approvalRepo = RecordingCountsApprovalRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.COUNTS_APPROVAL_APPROVE to countsApprovalApproveRefreshHook(approvalRepo),
            ),
        )

        val payload = CountsApprovalDecisionPayload(
            requestId = "request-1",
            request = CountsApprovalDecisionRequestDto(reason = "checked"),
        )

        val item = OutboxEntity(
            id = "item-approval-1",
            opType = OutboxOpType.COUNTS_APPROVAL_APPROVE.name,
            groupKey = "request-1",
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
            "COUNTS_APPROVAL_APPROVE success should forget the decided request",
            listOf("request-1"),
            approvalRepo.forgetDecidedCalls,
        )
    }

    @Test
    fun `COUNTS_APPROVAL_REJECT success forgets the decided request`() = runBlocking {
        val approvalRepo = RecordingCountsApprovalRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.COUNTS_APPROVAL_REJECT to countsApprovalRejectRefreshHook(approvalRepo),
            ),
        )

        val payload = CountsApprovalDecisionPayload(
            requestId = "request-2",
            request = CountsApprovalDecisionRequestDto(reason = "incorrect"),
        )

        val item = OutboxEntity(
            id = "item-rejection-1",
            opType = OutboxOpType.COUNTS_APPROVAL_REJECT.name,
            groupKey = "request-2",
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
            "COUNTS_APPROVAL_REJECT success should forget the decided request",
            listOf("request-2"),
            approvalRepo.forgetDecidedCalls,
        )
    }

    @Test
    fun `SHIFTING_COMPLETE success forgets the executed movement`() = runBlocking {
        val shiftingRepo = RecordingShiftingPendingRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.SHIFTING_COMPLETE to shiftingCompleteRefreshHook(shiftingRepo),
            ),
        )

        val payload = ShiftingCompletePayload(
            shiftingEventId = "movement-1",
            proofOutboxItemId = "proof-1",
        )

        val item = OutboxEntity(
            id = "item-complete-1",
            opType = OutboxOpType.SHIFTING_COMPLETE.name,
            groupKey = "movement-1",
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
            "SHIFTING_COMPLETE success should forget the executed movement",
            listOf("movement-1"),
            shiftingRepo.forgetExecutedCalls,
        )
    }

    @Test
    fun `SHIFTING_CANCEL success forgets the cancelled movement`() = runBlocking {
        val shiftingRepo = RecordingShiftingPendingRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.SHIFTING_CANCEL to shiftingCancelRefreshHook(shiftingRepo),
            ),
        )

        val payload = ShiftingCancelPayload(
            shiftingEventId = "movement-2",
            reason = "cancelled by operator",
        )

        val item = OutboxEntity(
            id = "item-cancel-1",
            opType = OutboxOpType.SHIFTING_CANCEL.name,
            groupKey = "movement-2",
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
            "SHIFTING_CANCEL success should forget the cancelled movement",
            listOf("movement-2"),
            shiftingRepo.forgetExecutedCalls,
        )
    }

    @Test
    fun `COUNTS_PROMOTE_IDENTIFIER success refreshes herd summary`() = runBlocking {
        val countsRepo = RecordingCountsRepository()
        val awaitingRfidRepo = RecordingAwaitingRfidRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            postSuccessRefreshHooks = mapOf(
                OutboxOpType.COUNTS_PROMOTE_IDENTIFIER to countsPromoteIdentifierRefreshHook(
                    countsRepo,
                    awaitingRfidRepo,
                ),
            ),
        )

        val payload = PromoteIdentifierPayload(
            goatId = "goat-1",
            permanentIdentifier = "RFID-123",
            rowVersion = 1,
        )

        val item = OutboxEntity(
            id = "item-promote-1",
            opType = OutboxOpType.COUNTS_PROMOTE_IDENTIFIER.name,
            groupKey = "goat-1",
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
            "COUNTS_PROMOTE_IDENTIFIER success should refresh herd summary",
            listOf(Unit),
            countsRepo.refreshHerdSummaryCalls,
        )
        assertEquals(listOf("goat-1"), awaitingRfidRepo.forgottenGoatIds)
    }
}
