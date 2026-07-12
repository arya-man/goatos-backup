package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.FlowCollector
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto

/**
 * MOB-010 guardrail: proves [AlertsViewModel.state] is exposed with
 * `stateIn(SharingStarted.WhileSubscribed(5_000))`, so the upstream Room flow is collected ONLY
 * while the UI is subscribed — not forever.
 *
 * The prior forever-`collectLatest` bridge kept draining battery/memory by re-processing Room
 * updates even when the screen was backgrounded. WhileSubscribed(5_000) stops the upstream
 * collection ~5s after the last subscriber leaves and restarts it on return.
 *
 * Uses [UnconfinedTestDispatcher] (sharing runTest's scheduler) so a launched collector subscribes
 * eagerly/synchronously, while `advanceTimeBy` still drives the WhileSubscribed stop timeout.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class AlertsViewModelWhileSubscribedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `upstream Room flow is collected only while state has subscribers`() = runTest(dispatcher) {
        val repo = FakeControlTowerRepository()
        val viewModel = AlertsViewModel(repo)

        // No UI subscriber yet -> WhileSubscribed keeps the upstream cold.
        assertEquals(0, repo.activeSummaryCollectors)

        // Subscribe (simulates the screen collecting state) — Unconfined subscribes synchronously.
        val job1 = launch { viewModel.state.collect {} }
        assertEquals(1, repo.activeSummaryCollectors)

        // Unsubscribe (screen backgrounded). Within the 5s window it stays warm...
        job1.cancel()
        assertEquals(1, repo.activeSummaryCollectors)

        // ...but after the 5s WhileSubscribed timeout it stops — no forever collection.
        advanceTimeBy(6_000)
        assertEquals(0, repo.activeSummaryCollectors)

        // Returning to the screen restarts the upstream collection.
        val job2 = launch { viewModel.state.collect {} }
        assertEquals(1, repo.activeSummaryCollectors)

        job2.cancel()
    }

    /** Fake repo whose observe flow tracks how many collectors are currently active. */
    private class FakeControlTowerRepository : ControlTowerRepository {
        private val upstream = MutableStateFlow(Resource<ControlTowerResponseDto>(data = null))
        var activeSummaryCollectors = 0
            private set

        override suspend fun summary(
            parkId: String?,
            shedId: String?,
            workState: String?,
            severity: String?,
            dueBefore: String?,
            asOf: String?,
            cursor: String?,
            limit: Int?,
        ): ControlTowerResponseDto = ControlTowerResponseDto(alerts = emptyList())

        override fun observeSummary(
            parkId: String?,
            shedId: String?,
            workState: String?,
            severity: String?,
            dueBefore: String?,
            asOf: String?,
            cursor: String?,
            limit: Int?,
        ): Flow<Resource<ControlTowerResponseDto>> =
            object : Flow<Resource<ControlTowerResponseDto>> {
                override suspend fun collect(collector: FlowCollector<Resource<ControlTowerResponseDto>>) {
                    activeSummaryCollectors++
                    try {
                        upstream.collect(collector)
                    } finally {
                        activeSummaryCollectors--
                    }
                }
            }

        override suspend fun refreshSummary(
            parkId: String?,
            shedId: String?,
            workState: String?,
            severity: String?,
            dueBefore: String?,
            asOf: String?,
            cursor: String?,
            limit: Int?,
        ): Result<Unit> = Result.success(Unit)
    }
}
