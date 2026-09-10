package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
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
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.WorkBoardLanes
import sg.mesha.goatos.core.data.WorkBoardQuery
import sg.mesha.goatos.core.data.WorkBoardRepository
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.WorkBoardRowDto
import sg.mesha.goatos.core.network.dto.WorkBoardSummaryDto
import sg.mesha.goatos.feature.workboard.WorkBoardEmptyMessage
import sg.mesha.goatos.feature.workboard.WorkBoardEvent

/**
 * The My Work state holder's refresh/offline contract (maintainer decision 2026-09-10), mirroring
 * [FeedDirectionRefreshStateTest]: a page failure marks the screen offline without dropping the
 * cached summary, a settled page clears it, and the chips keep their last-known summary across a
 * failed refresh. Also pins that the park sent is the caller's OWN from the bootstrap profile and
 * that a lane chip narrows the ROWS query while the summary stays board-wide.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WorkBoardRefreshStateTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeWorkBoardRepository(
        initialSummary: WorkBoardSummaryDto? = null,
    ) : WorkBoardRepository {
        val summary = MutableStateFlow(initialSummary)
        val summaryQueries = mutableListOf<WorkBoardQuery>()
        val rowQueries = mutableListOf<WorkBoardQuery>()
        val refreshQueries = mutableListOf<WorkBoardQuery>()
        var failRefresh = false

        override fun observeSummary(query: WorkBoardQuery): Flow<Resource<WorkBoardSummaryDto>> {
            summaryQueries += query
            return summary.map { Resource(data = it, lastSyncedAt = it?.let { 1_000L }) }
        }

        override fun observeRows(query: WorkBoardQuery): Flow<PagingData<WorkBoardRowDto>> {
            rowQueries += query
            return flowOf(PagingData.empty())
        }

        override suspend fun refresh(query: WorkBoardQuery): Result<Unit> {
            refreshQueries += query
            return if (failRefresh) Result.failure(IOException("offline")) else Result.success(Unit)
        }

        override fun observeRow(rowKey: String): Flow<WorkBoardRowDto?> = flowOf(null)
    }

    private fun bootstrap(parkId: String?): BootstrapRepository = object : BootstrapRepository {
        override suspend fun loadNavState(): NavState = error("unused")
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
            parkId?.let { BootstrapOperatorProfileDto(primaryLocationId = it) }
    }

    private fun viewModel(repository: WorkBoardRepository, parkId: String? = "park-1") = WorkBoardViewModel(
        repository = repository,
        bootstrapRepository = bootstrap(parkId),
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
    )

    private val servedSummary = WorkBoardSummaryDto(
        total = 12,
        byLane = mapOf("todo" to 5, "in_progress" to 2, "in_review" to 1, "done" to 4),
        byModule = mapOf("feed" to 8, "health" to 4),
        needsAttention = 3,
        modules = listOf("feed", "health"),
        lanes = WorkBoardLanes.ORDER,
        businessDate = "2026-09-10",
        parkId = "park-1",
        ownRowsOnly = true,
    )

    @Test
    fun `a page failure marks the screen offline and a settled page clears it`() = runTest(dispatcher) {
        val repository = FakeWorkBoardRepository(initialSummary = servedSummary)
        val viewModel = viewModel(repository)
        backgroundScope.launch { viewModel.state.collect {} }
        backgroundScope.launch { viewModel.rows.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(WorkBoardEvent.Refresh)
        advanceUntilIdle()
        assertTrue("a refresh spins until Paging settles", viewModel.state.value.isRefreshing)

        viewModel.onRowsLoadFailed(IOException("paging offline"))
        assertTrue("a failed page marks the screen offline", viewModel.state.value.isOffline)
        assertFalse("a failed page stops the spinner", viewModel.state.value.isRefreshing)
        // The cached summary is untouched: the tiles keep the last-known-good numbers.
        assertTrue(viewModel.state.value.hasSummary)
        assertEquals(4, viewModel.state.value.doneCount)
        assertEquals(8, viewModel.state.value.pendingCount)
        assertEquals(3, viewModel.state.value.needsAttentionCount)

        viewModel.onRowsLoaded()
        assertFalse("a settled page clears the offline mark", viewModel.state.value.isOffline)
    }

    @Test
    fun `a failed summary refresh keeps the last chips and reports offline`() = runTest(dispatcher) {
        val repository = FakeWorkBoardRepository(initialSummary = servedSummary)
        val viewModel = viewModel(repository)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()
        assertEquals(listOf("", "feed", "health"), viewModel.state.value.modules.map { it.key })
        assertEquals(listOf(12, 8, 4), viewModel.state.value.modules.map { it.count })

        repository.failRefresh = true
        viewModel.onEvent(WorkBoardEvent.Refresh)
        advanceUntilIdle()

        assertTrue("a failed summary refresh reports offline", viewModel.state.value.isOffline)
        // The chips and tiles are carried across the failure — never blanked.
        assertEquals(listOf("", "feed", "health"), viewModel.state.value.modules.map { it.key })
        assertEquals(12, viewModel.state.value.lanes.first().count)
        assertTrue(viewModel.state.value.ownRowsOnly)
        assertEquals(WorkBoardEmptyMessage.EMPTY_OWN, viewModel.state.value.emptyMessage)
    }

    @Test
    fun `a cold cache with no network is the error state and loading otherwise`() = runTest(dispatcher) {
        val repository = FakeWorkBoardRepository(initialSummary = null)
        val viewModel = viewModel(repository)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()
        assertFalse(viewModel.state.value.hasSummary)
        assertEquals(WorkBoardEmptyMessage.LOADING, viewModel.state.value.emptyMessage)
        // Before any summary lands the tiles are unknown, not zero.
        assertNull(viewModel.state.value.lastSyncedAt)

        viewModel.onRowsLoadFailed(IOException("offline"))
        assertEquals(WorkBoardEmptyMessage.ERROR, viewModel.state.value.emptyMessage)
    }

    @Test
    fun `the query carries the bootstrap park and a lane chip narrows rows but not the summary`() = runTest(dispatcher) {
        val repository = FakeWorkBoardRepository(initialSummary = servedSummary)
        val viewModel = viewModel(repository, parkId = "park-9")
        backgroundScope.launch { viewModel.state.collect {} }
        backgroundScope.launch { viewModel.rows.collect {} }
        advanceUntilIdle()

        assertEquals("park-9", repository.rowQueries.last().parkId)
        assertEquals("park-9", repository.summaryQueries.last().parkId)
        assertEquals("", repository.rowQueries.last().lane)

        viewModel.onEvent(WorkBoardEvent.SelectLane(WorkBoardLanes.IN_REVIEW))
        advanceUntilIdle()

        // The rows query is narrowed to the lane's states ...
        assertEquals(WorkBoardLanes.IN_REVIEW, repository.rowQueries.last().lane)
        assertEquals("verification_pending", repository.rowQueries.last().stateCsv)
        // ... while the summary the chips read stays board-wide, so the other lanes keep their counts.
        assertEquals("", repository.summaryQueries.last().summaryQuery().lane)
        assertEquals(5, viewModel.state.value.lanes.first { it.key == WorkBoardLanes.TODO }.count)
        assertTrue(viewModel.state.value.lanes.first { it.key == WorkBoardLanes.IN_REVIEW }.selected)

        viewModel.onEvent(WorkBoardEvent.NextDay)
        advanceUntilIdle()
        assertEquals("2026-09-11".length, repository.rowQueries.last().businessDate.length)
        assertFalse(viewModel.state.value.isToday)
    }

    @Test
    fun `a profile with no park sends none and lets the backend resolve it`() = runTest(dispatcher) {
        val repository = FakeWorkBoardRepository(initialSummary = servedSummary)
        val viewModel = viewModel(repository, parkId = null)
        backgroundScope.launch { viewModel.rows.collect {} }
        advanceUntilIdle()

        assertEquals(1, repository.rowQueries.size)
        assertNull(repository.rowQueries.single().parkId)
    }
}
