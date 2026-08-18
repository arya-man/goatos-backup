package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import java.io.IOException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedDirectionQuery
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedDirectionRowDto

@OptIn(ExperimentalCoroutinesApi::class)
class FeedDirectionRefreshStateTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `paging failure stays visible after successful connectivity probe`() = runTest(dispatcher) {
        val probeResult = CompletableDeferred<Boolean>()
        val repository = object : FeedRepository by FakeFeedRepository() {
            override fun observeDirectionTotals(query: FeedDirectionQuery): Flow<Resource<FeedDirectionPreviewPageDto>> =
                flowOf(Resource(data = FeedDirectionPreviewPageDto()))

            override fun directionRows(query: FeedDirectionQuery): Flow<PagingData<FeedDirectionRowDto>> =
                flowOf(PagingData.empty())

            override suspend fun probeDirectionSummary(query: FeedDirectionQuery): Boolean = probeResult.await()
        }
        val viewModel = FeedDirectionViewModel(
            repo = repository,
            feedCompletionStore = FeedCompletionLocalStore(),
            submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) },
            bootstrapRepository = object : BootstrapRepository {
                override suspend fun loadNavState(): NavState = error("unused")
                override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
            },
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(sg.mesha.goatos.feature.feed.FeedDirectionEvent.Refresh)
        viewModel.onRowsLoadFailed(IOException("paging offline"))
        probeResult.complete(true)
        advanceUntilIdle()

        assertTrue("paging failure must override a successful summary probe", viewModel.state.value.isOffline)
        assertFalse("a failed page load must stop the refresh indicator", viewModel.state.value.isRefreshing)

        viewModel.onRowsLoaded()
        advanceUntilIdle()
        assertFalse("a successful page retry clears the paging failure", viewModel.state.value.isOffline)
    }
}
