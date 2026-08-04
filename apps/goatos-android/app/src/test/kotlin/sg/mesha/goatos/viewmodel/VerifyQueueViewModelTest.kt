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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
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
import sg.mesha.goatos.core.network.dto.VerificationFilterOptionsDto
import sg.mesha.goatos.core.network.dto.VerificationPageOptionDto
import sg.mesha.goatos.core.network.dto.VerificationStatusOptionDto
import sg.mesha.goatos.feature.verify.VerifyQueueEvent

/**
 * Verifier feature segregation regressions. Each top-level feature must observe and refresh
 * only its backend-selected page category so videos from different module pages cannot mix.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyQueueViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `counts route observes and refreshes only its backend page category`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "shifting_move")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("shifting_move", vm.state.value.selectedCategory)
        assertEquals("shifting_move", repo.observedCategories.last())
        assertEquals("shifting_move", repo.refreshedCategories.last())
        assertFalse(vm.state.value.isRefreshing)
    }

    @Test
    fun `selecting a backend page tab switches the category without a client module enum`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "birth_evidence")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyQueueEvent.SelectCategory("death_evidence"))
        advanceUntilIdle()

        assertFalse(vm.state.value.isRefreshing)
        assertEquals("counts", vm.state.value.moduleKey)
        assertEquals("Counts", vm.state.value.moduleLabel)
        assertEquals("death_evidence", vm.state.value.selectedCategory)
        assertEquals(listOf("birth_evidence", "death_evidence"), repo.refreshedCategories)
    }

    @Test
    fun `secondary status date and missed filters switch the exact cached backend scope`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "shifting_move")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyQueueEvent.SelectStatus("approved"))
        vm.onEvent(VerifyQueueEvent.SelectBusinessDate("2026-07-29"))
        advanceUntilIdle()
        assertEquals(Triple("approved", "2026-07-29", false), repo.refreshedScopes.last())

        vm.onEvent(VerifyQueueEvent.ToggleMissed)
        advanceUntilIdle()
        assertEquals(Triple("pending", null, true), repo.refreshedScopes.last())
        assertEquals("pending", vm.state.value.selectedStatus)
        assertEquals(true, vm.state.value.missedOnly)
    }

    @Test
    fun `a module this client cannot serve reads nothing rather than vaccination proofs`() = runTest(dispatcher) {
        // The backend composes a verify entry per feature the verifier holds duty on, including
        // counts and feed. Coercing those to vaccination showed a Counts verifier another module's
        // proofs and invited a verdict on work they were never asked to review.
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("module" to "counts")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(emptyList<String?>(), repo.refreshedCategories)
        assertEquals(emptyList<String?>(), repo.observedCategories)
        assertNull(vm.state.value.selectedModule)
        assertTrue(vm.state.value.isUnsupportedModule)
    }

    @Test
    fun `an alerts category is passed through verbatim`() = runTest(dispatcher) {
        // /verify/alerts names the category outright; the client must not re-derive it.
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "shifting_move")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf("shifting_move"), repo.observedCategories)
        assertEquals(listOf("shifting_move"), repo.refreshedCategories)
        assertFalse(vm.state.value.isUnsupportedModule)
    }

    @Test
    fun `the drawer href's category wins over a module key this client cannot map`() = runTest(dispatcher) {
        // What bootstrap_copy.go actually composes for a Counts verifier:
        // "/verify?module=counts&category=shifting_move". The module key is unmappable here on
        // purpose -- the server's own category is the answer, so the queue must read it and not
        // fall back to the unsupported-module empty state.
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("module" to "counts", "category" to "shifting_move")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf("shifting_move"), repo.observedCategories)
        assertFalse(vm.state.value.isUnsupportedModule)
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
    val observedCategories = mutableListOf<String?>()
    val refreshedCategories = mutableListOf<String?>()
    val refreshedScopes = mutableListOf<Triple<String?, String?, Boolean?>>()

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = error("unused")

    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> {
        observedCategories += category
        return flowOf(
            Resource(
                data = VerificationQueueResponseDto(
                    items = emptyList(),
                    filterOptions = VerificationFilterOptionsDto(
                        moduleKey = "counts",
                        moduleLabel = "Counts",
                        pages = listOf(
                            VerificationPageOptionDto("birth", "Birth", "birth_evidence"),
                            VerificationPageOptionDto("death", "Death", "death_evidence"),
                        ),
                        statuses = listOf(
                            VerificationStatusOptionDto("due", "Due", "pending"),
                            VerificationStatusOptionDto("approved", "Approved", "approved"),
                            VerificationStatusOptionDto("rejected", "Rejected", "rejected"),
                        ),
                        selectedBusinessDate = businessDate,
                        missedOnly = missed == true,
                        hasMissed = true,
                    ),
                ),
            ),
        )
    }

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        refreshQueueCalls++
        refreshedCategories += category
        refreshedScopes += Triple(status, businessDate, missed)
        return Result.success(Unit)
    }

    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = error("unused")
    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
}
