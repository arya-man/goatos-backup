package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.feature.verify.VerifyModuleTab
import sg.mesha.goatos.feature.verify.VerifyQueueEvent

/**
 * R50-031 regression: [VerifyQueueViewModel.refresh] sets `_isRefreshing = true` up front, then
 * early-returns when the selected tab has no backing category. Every REVIEW-queue tab is now wired
 * (Shifting/Packing/Feed direction all back a real category), so the surviving early-return path is
 * the ACTION queue, where those same tabs have no leadership close/action step and resolve to a
 * null category. Before the fix, that early return skipped the line that flips `_isRefreshing` back
 * to false, so a pull-to-refresh left the spinner spinning forever. The fix moves the reset into a
 * `finally` block.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyQueueViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `refreshing a null-category tab still clears isRefreshing via finally`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        // actionMode=true: in the ACTION queue, Shifting/Packing/Feed direction have no leadership
        // close/action step, so categoryForModule returns null for them — the early-return path.
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("actionMode" to true)),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        // The constructor's own initial refresh() runs on the default VACCINATION tab, so it
        // hits repo.refreshActionQueue exactly once before this test does anything.
        assertFalse(vm.state.value.isRefreshing)
        val actionCallsAfterInit = repo.refreshActionQueueCalls

        // SHIFTING has no action queue, so it exercises refresh()'s early-return path.
        vm.onEvent(VerifyQueueEvent.SelectModule(VerifyModuleTab.SHIFTING))
        advanceUntilIdle()

        vm.onEvent(VerifyQueueEvent.Refresh)
        advanceUntilIdle()

        assertFalse(
            "R50-031: refresh() must clear isRefreshing via finally even on its early-return " +
                "path for a tab with no backing queue",
            vm.state.value.isRefreshing,
        )
        // The early-return path must never have reached the real network refresh again.
        assertEquals(actionCallsAfterInit, repo.refreshActionQueueCalls)
    }

    @Test
    fun `refreshing the vaccination tab still clears isRefreshing after a real refresh`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(repo = repo, syncRepo = FakeVerifySyncRepository(), analytics = NoopAnalytics(), savedStateHandle = SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyQueueEvent.Refresh)
        advanceUntilIdle()

        assertFalse(vm.state.value.isRefreshing)
        assertEquals(2, repo.refreshQueueCalls) // once from init, once from this explicit Refresh
    }

    @Test
    fun `death tab loads the approved death evidence category`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(repo, FakeVerifySyncRepository(), NoopAnalytics(), SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyQueueEvent.SelectModule(VerifyModuleTab.DEATH))
        advanceUntilIdle()

        assertEquals("death_evidence", repo.lastRefreshCategory)
    }

    @Test
    fun `birth tab has its own queue category ready for the later birth producer`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(repo, FakeVerifySyncRepository(), NoopAnalytics(), SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyQueueEvent.SelectModule(VerifyModuleTab.BIRTH))
        advanceUntilIdle()

        assertEquals("birth_evidence", repo.lastRefreshCategory)
    }
}

private class FakeVerifySyncRepository : SyncRepository {
    override fun observeStatus() = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationBatchClose(batchId: String): AppResult<String> = AppResult.Ok("close-$batchId")
    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private class FakeVerifyQueueRepository : VerificationRepository {
    var refreshQueueCalls = 0
        private set
    var refreshActionQueueCalls = 0
        private set
    var lastRefreshCategory: String? = null
        private set

    override suspend fun queue(category: String?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = error("unused")

    override fun observeQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = VerificationQueueResponseDto(items = emptyList())))

    override suspend fun refreshQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        refreshQueueCalls++
        lastRefreshCategory = category
        return Result.success(Unit)
    }

    override suspend fun appendQueue(cursor: String, category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = VerificationQueueResponseDto(items = emptyList())))
    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        refreshActionQueueCalls++
        return Result.success(Unit)
    }
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
}
