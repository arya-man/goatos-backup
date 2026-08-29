package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import java.io.IOException
import java.time.LocalDate
import java.time.ZoneId
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.MilkFeedingRepository
import sg.mesha.goatos.core.data.MilkPreparationRepository
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto
import sg.mesha.goatos.feature.counts.MilkFeedingListEvent
import sg.mesha.goatos.feature.counts.MilkPreparationListEvent

/**
 * Judge finding #1/#2: the date-chevron fix landed on Milk Preparation
 * (a7c27afd1) but not on Milk Feeding, the screen the device bug report was
 * actually about. Both list ViewModels must re-subscribe `repo.observe(newDate)`
 * when NavigateDate is dispatched -- a plain immutable `val` date silently drops
 * the chevron tap (MOB-010 collect-in-launch class of bug: no flatMapLatest
 * re-subscribe means the old subscription is never cancelled and the new date
 * is never observed).
 */
@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class MilkDateNavViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private val IST: ZoneId = ZoneId.of("Asia/Kolkata")

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `milk preparation NavigateDate re-subscribes repo observe with the new date`() = runTest(dispatcher) {
        val repo = TrackingMilkPreparationRepository()
        val drafts = TrackingDraftRepository()
        val viewModel = MilkPreparationListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = drafts)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val today = LocalDate.now(IST).toString()
        val yesterday = LocalDate.now(IST).minusDays(1).toString()
        assertEquals("initial subscription must observe today", listOf(today), repo.observedDates)

        viewModel.onEvent(MilkPreparationListEvent.NavigateDate(-1))
        advanceUntilIdle()

        assertEquals(
            "NavigateDate(-1) must cancel the old subscription and re-subscribe repo.observe(yesterday)",
            listOf(today, yesterday),
            repo.observedDates,
        )
        assertEquals("the old subscription must be cancelled, not left running", 1, repo.activeSubscriptions)
    }

    @Test
    fun `milk preparation date label drops the Today prefix once navigated away`() = runTest(dispatcher) {
        val repo = TrackingMilkPreparationRepository()
        val drafts = TrackingDraftRepository()
        val viewModel = MilkPreparationListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = drafts)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertTrue(
            "today's selection must read Today · <date>",
            viewModel.state.value.dateLabel.startsWith("Today · "),
        )

        viewModel.onEvent(MilkPreparationListEvent.NavigateDate(-1))
        advanceUntilIdle()

        assertTrue(
            "a past day must NOT be mislabeled Today",
            !viewModel.state.value.dateLabel.startsWith("Today · "),
        )
    }

    @Test
    fun `milk preparation NavigateDate is clamped at today`() = runTest(dispatcher) {
        val repo = TrackingMilkPreparationRepository()
        val drafts = TrackingDraftRepository()
        val viewModel = MilkPreparationListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = drafts)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val today = LocalDate.now(IST).toString()
        viewModel.onEvent(MilkPreparationListEvent.NavigateDate(1))
        advanceUntilIdle()

        assertEquals(
            "navigating forward past today must be clamped, not allowed into the future",
            listOf(today),
            repo.observedDates,
        )
    }

    // --- Milk Feeding: the screen the device bug report was actually about ---

    @Test
    fun `milk feeding NavigateDate re-subscribes repo observe with the new date`() = runTest(dispatcher) {
        val repo = TrackingMilkFeedingRepository()
        val drafts = TrackingDraftRepository()
        val viewModel = MilkFeedingListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = drafts)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val today = LocalDate.now(IST).toString()
        val yesterday = LocalDate.now(IST).minusDays(1).toString()
        assertEquals("initial subscription must observe today", listOf(today), repo.observedDates)

        viewModel.onEvent(MilkFeedingListEvent.NavigateDate(-1))
        advanceUntilIdle()

        assertEquals(
            "NavigateDate(-1) must cancel the old subscription and re-subscribe repo.observe(yesterday) -- " +
                "this is the device-reported bug: chevrons on the Feeding screen did nothing",
            listOf(today, yesterday),
            repo.observedDates,
        )
        assertEquals("the old subscription must be cancelled, not left running (MOB-010 class)", 1, repo.activeSubscriptions)
    }

    @Test
    fun `milk feeding date label drops the Today prefix once navigated away`() = runTest(dispatcher) {
        val repo = TrackingMilkFeedingRepository()
        val drafts = TrackingDraftRepository()
        val viewModel = MilkFeedingListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = drafts)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        assertTrue(
            "today's selection must read Today · <date>",
            viewModel.state.value.dateLabel.startsWith("Today · "),
        )

        viewModel.onEvent(MilkFeedingListEvent.NavigateDate(-1))
        advanceUntilIdle()

        assertTrue(
            "a past day must NOT be mislabeled Today",
            !viewModel.state.value.dateLabel.startsWith("Today · "),
        )
    }

    @Test
    fun `milk feeding NavigateDate is clamped at today`() = runTest(dispatcher) {
        val repo = TrackingMilkFeedingRepository()
        val drafts = TrackingDraftRepository()
        val viewModel = MilkFeedingListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = drafts)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val today = LocalDate.now(IST).toString()
        viewModel.onEvent(MilkFeedingListEvent.NavigateDate(1))
        advanceUntilIdle()

        assertEquals(
            "navigating forward past today must be clamped, not allowed into the future",
            listOf(today),
            repo.observedDates,
        )
    }

    @Test
    fun `milk feeding failed manual refresh keeps cached rows and marks screen offline`() = runTest(dispatcher) {
        val repo = TrackingMilkFeedingRepository()
        val viewModel = MilkFeedingListViewModel(
            repo = repo,
            submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) },
            drafts = TrackingDraftRepository(),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        repo.refreshResult = Result.failure(IOException("offline"))
        viewModel.onEvent(MilkFeedingListEvent.Refresh)
        advanceUntilIdle()

        assertEquals("cached task remains visible after refresh failure", 1, viewModel.state.value.cards.size)
        assertTrue("failed network refresh must be visible as offline", viewModel.state.value.isOffline)

        repo.refreshResult = Result.success(Unit)
        viewModel.onEvent(MilkFeedingListEvent.Refresh)
        advanceUntilIdle()

        assertTrue("a successful retry clears the offline state", !viewModel.state.value.isOffline)
    }

    // --- Calendar jump (date-bar picker), 2026-08-27: SelectDate lands in one hop ---

    @Test
    fun `milk preparation SelectDate re-subscribes with the picked date and caps the future`() = runTest(dispatcher) {
        val repo = TrackingMilkPreparationRepository()
        val viewModel = MilkPreparationListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = TrackingDraftRepository())
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val today = LocalDate.now(IST).toString()
        val lastWeek = LocalDate.now(IST).minusDays(7).toString()
        viewModel.onEvent(MilkPreparationListEvent.SelectDate(lastWeek))
        advanceUntilIdle()
        assertEquals("a calendar pick must land in ONE re-subscription, not chevron steps", listOf(today, lastWeek), repo.observedDates)
        assertTrue("a picked past day is not Today", !viewModel.state.value.isToday)

        viewModel.onEvent(MilkPreparationListEvent.SelectDate(LocalDate.now(IST).plusDays(3).toString()))
        advanceUntilIdle()
        assertEquals("a future pick is clamped to today", listOf(today, lastWeek, today), repo.observedDates)
        assertTrue("clamped-to-today selection reads as Today again", viewModel.state.value.isToday)
    }

    @Test
    fun `milk feeding SelectDate re-subscribes with the picked date`() = runTest(dispatcher) {
        val repo = TrackingMilkFeedingRepository()
        val viewModel = MilkFeedingListViewModel(repo = repo, submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) }, drafts = TrackingDraftRepository())
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        val today = LocalDate.now(IST).toString()
        val lastWeek = LocalDate.now(IST).minusDays(7).toString()
        viewModel.onEvent(MilkFeedingListEvent.SelectDate(lastWeek))
        advanceUntilIdle()
        assertEquals(listOf(today, lastWeek), repo.observedDates)
        assertTrue(!viewModel.state.value.isToday)
    }
}

/** Counts subscriptions so the test can prove flatMapLatest actually CANCELS the prior
 *  repo.observe() call rather than leaving a second collector running alongside it. */
private class TrackingMilkPreparationRepository : MilkPreparationRepository {
    val observedDates = mutableListOf<String>()
    var activeSubscriptions = 0
        private set

    override fun observe(preparationDate: String): Flow<Resource<MilkPreparationPageDto>> {
        observedDates += preparationDate
        activeSubscriptions++
        return callbackFlow {
            trySend(Resource(data = MilkPreparationPageDto(farmTasks = emptyList())))
            awaitClose { activeSubscriptions-- }
        }
    }

    override suspend fun refresh(preparationDate: String): Result<Unit> = Result.success(Unit)
}

private class TrackingMilkFeedingRepository : MilkFeedingRepository {
    val observedDates = mutableListOf<String>()
    var refreshResult: Result<Unit> = Result.success(Unit)
    var activeSubscriptions = 0
        private set

    override fun observe(feedingDate: String, parkId: String, sessionNo: Int?): Flow<Resource<MilkFeedingPageDto>> {
        observedDates += feedingDate
        activeSubscriptions++
        return callbackFlow {
            trySend(
                Resource(
                    data = MilkFeedingPageDto(
                        items = listOf(
                            sg.mesha.goatos.core.network.dto.MilkFeedingTaskDto(
                                taskId = "task-1",
                                parkId = "park-1",
                                feedingDate = feedingDate,
                                sessionNo = 1,
                                dueTime = "08:00",
                                available = true,
                                verificationStatus = "not_submitted",
                            ),
                        ),
                    ),
                ),
            )
            awaitClose { activeSubscriptions-- }
        }
    }

    override suspend fun refresh(feedingDate: String, parkId: String, sessionNo: Int?): Result<Unit> = refreshResult
}

private class TrackingDraftRepository : CaptureDraftRepository {
    override suspend fun find(flowKey: String, entityId: String): CaptureDraft = CaptureDraft()
    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> = MutableStateFlow(CaptureDraft())
    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) = Unit
    override suspend fun putProof(flowKey: String, entityId: String, step: String, outboxItemId: String, fingerprint: String?) = Unit
    override suspend fun putSubmit(flowKey: String, entityId: String, idempotencyKey: String?, outboxItemId: String?) = Unit
    override suspend fun clearProof(flowKey: String, entityId: String, step: String) = Unit
    override suspend fun clear(flowKey: String, entityId: String) = Unit
    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = MutableStateFlow(emptyMap())
}
