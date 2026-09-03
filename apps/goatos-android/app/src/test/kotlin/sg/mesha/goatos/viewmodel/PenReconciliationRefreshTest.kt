package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import app.cash.turbine.test
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.PenReconciliationMeta
import sg.mesha.goatos.core.data.PenReconciliationRepository
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.feature.counts.PenReconciliationEvent

/**
 * The Reconcile list's selection contract, mirroring [ShiftingPendingRefreshTest]:
 *  - a Refresh must actually REFETCH (an equal Selection is conflated away by StateFlow, the
 *    2026-08-13 silent-refresh defect class);
 *  - selecting a status chip resubscribes the paged read scoped to that status;
 *  - an unknown status key is ignored rather than fetched.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PenReconciliationRefreshTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `refresh resubscribes the paged read so fresh server state can arrive`() = runTest(dispatcher) {
        val repo = CountingPenReconciliationRepository()
        val viewModel = newViewModel(repo)

        viewModel.rows.test {
            awaitItem()
            advanceUntilIdle()
            assertEquals("initial subscription", 1, repo.subscriptions.size)

            viewModel.onEvent(PenReconciliationEvent.Refresh)
            advanceUntilIdle()
            assertEquals(
                "a refresh must refetch; an equal Selection is conflated away and never reaches the repo",
                2,
                repo.subscriptions.size,
            )

            viewModel.onEvent(PenReconciliationEvent.Refresh)
            advanceUntilIdle()
            assertEquals("every refresh refetches, not just the first", 3, repo.subscriptions.size)

            cancelAndIgnoreRemainingEvents()
        }
    }

    @Test
    fun `selecting a status scopes the paged read to that backend bucket`() = runTest(dispatcher) {
        val repo = CountingPenReconciliationRepository()
        val viewModel = newViewModel(repo)

        viewModel.rows.test {
            awaitItem()
            advanceUntilIdle()
            assertEquals(listOf("all"), repo.subscriptions)

            viewModel.onEvent(PenReconciliationEvent.SelectStatus("open"))
            advanceUntilIdle()
            assertEquals(listOf("all", "open"), repo.subscriptions)

            // An unknown status key is ignored — never sent to the backend as a filter.
            viewModel.onEvent(PenReconciliationEvent.SelectStatus("bogus"))
            advanceUntilIdle()
            assertEquals(listOf("all", "open"), repo.subscriptions)

            cancelAndIgnoreRemainingEvents()
        }
    }

    private fun newViewModel(repo: PenReconciliationRepository) = PenReconciliationViewModel(
        repo = repo,
        syncRepository = NoopShiftingSyncRepository(),
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
    )
}

private class CountingPenReconciliationRepository : PenReconciliationRepository {
    /** Every status the paged read was (re)subscribed with, in order. */
    val subscriptions = mutableListOf<String>()

    override val meta: StateFlow<PenReconciliationMeta> = MutableStateFlow(PenReconciliationMeta())

    override fun cards(status: String): Flow<PagingData<CountsPenReconciliationCardDto>> = flow {
        subscriptions += status
        emit(PagingData.empty())
    }

    override suspend fun forgetCompleted(cardId: String) = Unit

    override suspend fun findCached(cardId: String): CountsPenReconciliationCardDto? = null
}
