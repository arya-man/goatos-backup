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
import org.junit.Assert.assertNotNull
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
import sg.mesha.goatos.core.network.dto.VerificationSourceRef
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import okhttp3.ResponseBody.Companion.toResponseBody
import retrofit2.HttpException
import retrofit2.Response

/**
 * Verifier feature segregation regressions. Each top-level feature must observe and refresh
 * only its backend-selected page category so videos from different module pages cannot mix.
 * Also tests error handling: failed fetches render distinct error states, not "Queue clear".
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
    fun `an unknown module reads nothing rather than vaccination proofs`() = runTest(dispatcher) {
        // The backend composes a verify entry per feature the verifier holds duty on, including
        // modules a given client may not know yet. Coercing those to vaccination showed a verifier
        // another module's proofs and invited a verdict on work they were never asked to review.
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("module" to "future_module")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(emptyList<String?>(), repo.refreshedCategories)
        assertEquals(emptyList<String?>(), repo.observedCategories)
        assertNull(vm.state.value.selectedModule)
        assertTrue(vm.state.value.isUnsupportedModule)
    }

    @Test
    fun `a known module-only link opens its landing category`() = runTest(dispatcher) {
        val repo = FakeVerifyQueueRepository()
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("module" to "counts")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf("shifting_move"), repo.refreshedCategories)
        assertEquals(listOf("shifting_move"), repo.observedCategories)
        assertFalse(vm.state.value.isUnsupportedModule)
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

    @Test
    fun `a 403 module_scope_forbidden renders as refusal state not empty queue`() = runTest(dispatcher) {
        // A verifier opened the app on a module they're not assigned to. The backend refuses
        // the queue fetch with 403 module_scope_forbidden. The old code rendered "Queue clear"
        // (empty state with positive tone), wasting 40 minutes debugging. The fix: detect the
        // error and show it distinctly.
        val repo = FakeVerifyQueueRepository(
            error = HttpException(
                Response.error<String>(
                    403,
                    """{"code":"module_scope_forbidden","message":"verifier is not assigned to this module"}""".toResponseBody()
                )
            )
        )
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // After a fetch attempt, hasLoadedOnce is true (set in finally), rows are empty,
        // and queueError is the backend's message.
        assertTrue(vm.state.value.hasLoadedOnce)
        assertTrue(vm.state.value.rows.isEmpty())
        assertTrue(vm.state.value.queueFailed)
        assertNotNull(vm.state.value.queueError)
        assertTrue(vm.state.value.queueError!!.contains("not assigned"))
    }

    @Test
    fun `a generic 500 error renders as failure state with fallback message`() = runTest(dispatcher) {
        // A backend error with no readable message (corrupted body, or empty envelope).
        val repo = FakeVerifyQueueRepository(
            error = HttpException(
                Response.error<String>(500, "Internal Server Error".toResponseBody())
            )
        )
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(vm.state.value.hasLoadedOnce)
        assertTrue(vm.state.value.rows.isEmpty())
        // The FACT of the failure reaches the screen so it cannot render "Queue clear"...
        assertTrue(vm.state.value.queueFailed)
        // ...while the words do not: this body carries no readable envelope, so the server
        // said nothing, and the fallback copy is a translated string resolved by the
        // composable rather than an English literal the ViewModel could never translate.
        assertNull(vm.state.value.queueError)
    }

    @Test
    fun `cached rows survive a fetch failure`() = runTest(dispatcher) {
        // A queue fetch fails, but we have cached rows from a previous success.
        // The error should NOT blank the cache — show the rows with the error cleared
        // so the user can still review offline.
        val cachedRows = listOf(
            FakeVerificationQueueItem(itemId = "item1", category = "vaccination_proof")
        )
        val repo = FakeVerifyQueueRepository(
            cachedItems = cachedRows,
            error = HttpException(
                Response.error<String>(500, "Server error".toResponseBody())
            )
        )
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = FakeVerifySyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // We have cached rows, so error is suppressed (queueError stays null).
        assertTrue(vm.state.value.hasLoadedOnce)
        assertFalse(vm.state.value.rows.isEmpty())
        assertFalse(vm.state.value.queueFailed)
        assertNull(vm.state.value.queueError)
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
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationBatchClose(batchId: String): AppResult<String> = AppResult.Ok("close-$batchId")
    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private data class FakeVerificationQueueItem(
    val itemId: String,
    val category: String,
)

private class FakeVerifyQueueRepository(
    private val cachedItems: List<FakeVerificationQueueItem> = emptyList(),
    private val error: Throwable? = null,
) : VerificationRepository {
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
                data = if (cachedItems.isNotEmpty()) {
                    VerificationQueueResponseDto(
                        items = cachedItems.map { fake ->
                            sg.mesha.goatos.core.network.dto.VerificationQueueItem(
                                itemId = fake.itemId,
                                category = fake.category,
                                status = "pending",
                                source = VerificationSourceRef(
                                    taskId = null,
                                    submissionId = null,
                                    refType = "test",
                                ),
                                media = emptyList(),
                            )
                        },
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
                    )
                } else {
                    VerificationQueueResponseDto(
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
                    )
                },
                error = error,
            ),
        )
    }

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        refreshQueueCalls++
        refreshedCategories += category
        refreshedScopes += Triple(status, businessDate, missed)
        return if (error == null) {
            Result.success(Unit)
        } else {
            Result.failure(error)
        }
    }

    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = error("unused")
    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
    override fun observeLeadershipVideos(category: String?, windowSize: Int) = flowOf(emptyList<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>())
    override fun observeLeadershipTitle(category: String?, windowSize: Int) = flowOf("")
    override suspend fun refreshLeadershipVideos(category: String?, windowSize: Int, reset: Boolean) =
        sg.mesha.goatos.core.common.AppResult.Ok(Unit)
}
