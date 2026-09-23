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
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.PenReconciliationRepository
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationFiltersDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationParkOptionDto
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
            assertEquals(listOf("all|"), repo.subscriptions)

            viewModel.onEvent(PenReconciliationEvent.SelectStatus("open"))
            advanceUntilIdle()
            assertEquals(listOf("all|", "open|"), repo.subscriptions)

            // An unknown status key is ignored — never sent to the backend as a filter.
            viewModel.onEvent(PenReconciliationEvent.SelectStatus("bogus"))
            advanceUntilIdle()
            assertEquals(listOf("all|", "open|"), repo.subscriptions)

            cancelAndIgnoreRemainingEvents()
        }
    }

    /**
     * The park bar is BACKEND-OWNED (maintainer request 2026-09-15). A single-park operator gets
     * one chip, already selected, and no "All"; a tenant-wide reader gets "All parks" selected
     * plus one chip per park; picking a park resubscribes the paged read with that park; a park
     * the backend never offered is ignored.
     */
    @Test
    fun `park bar renders the backend's options and selection verbatim`() = runTest(dispatcher) {
        val repo = CountingPenReconciliationRepository()
        val viewModel = newViewModel(repo)
        val cbe = CountsPenReconciliationParkOptionDto(parkId = "park-cbe", label = "Coimbatore", code = "CBE")
        val cpt = CountsPenReconciliationParkOptionDto(parkId = "park-cpt", label = "Channapatna", code = "CPT")

        viewModel.state.test {
            awaitItem()

            // Single-park operator: the server already clamped and selected their park.
            repo.metaFlow.value = PenReconciliationMeta(
                filters = CountsPenReconciliationFiltersDto(parks = listOf(cpt.copy(selected = true)), selectedParkId = "park-cpt"),
            )
            advanceUntilIdle()
            val single = expectMostRecentItem().parks
            assertEquals(listOf("Channapatna"), single.map { it.label })
            assertEquals(listOf(true), single.map { it.selected })

            // Tenant-wide reader: "All parks" leads and is selected until they pick one.
            repo.metaFlow.value = PenReconciliationMeta(
                filters = CountsPenReconciliationFiltersDto(parks = listOf(cbe, cpt), selectedParkId = ""),
            )
            advanceUntilIdle()
            val wide = expectMostRecentItem().parks
            assertEquals(listOf("All parks", "Coimbatore", "Channapatna"), wide.map { it.label })
            assertEquals(listOf(true, false, false), wide.map { it.selected })
            assertEquals(listOf("", "CBE", "CPT"), wide.map { it.code })

            cancelAndIgnoreRemainingEvents()
        }

        viewModel.rows.test {
            awaitItem()
            advanceUntilIdle()
            assertEquals(listOf("all|"), repo.subscriptions)

            viewModel.onEvent(PenReconciliationEvent.SelectPark("park-cbe"))
            advanceUntilIdle()
            assertEquals("picking a park resubscribes scoped to it", listOf("all|", "all|park-cbe"), repo.subscriptions)

            viewModel.onEvent(PenReconciliationEvent.SelectPark("park-never-offered"))
            advanceUntilIdle()
            assertEquals("an unoffered park is ignored", listOf("all|", "all|park-cbe"), repo.subscriptions)

            viewModel.onEvent(PenReconciliationEvent.SelectPark(""))
            advanceUntilIdle()
            assertEquals("the All chip clears the clamp", listOf("all|", "all|park-cbe", "all|"), repo.subscriptions)

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
    /** Every (status|park) the paged read was (re)subscribed with, in order. */
    val subscriptions = mutableListOf<String>()

    val metaFlow = MutableStateFlow(PenReconciliationMeta())
    override val meta: StateFlow<PenReconciliationMeta> = metaFlow

    override suspend fun openQuestionnaire(cardId: String): AppResult<String> = AppResult.Ok("wf-$cardId")

    override fun cards(status: String, parkId: String?): Flow<PagingData<CountsPenReconciliationCardDto>> = flow {
        subscriptions += "$status|${parkId.orEmpty()}"
        emit(PagingData.empty())
    }

    override suspend fun forgetCompleted(cardId: String) = Unit

    override suspend fun findCached(cardId: String): CountsPenReconciliationCardDto? = null
}
