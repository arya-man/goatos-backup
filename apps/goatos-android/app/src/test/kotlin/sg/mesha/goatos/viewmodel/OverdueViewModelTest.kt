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
 * MOB-010 guardrail (second converted VM): [OverdueViewModel.state] is exposed via
 * `stateIn(SharingStarted.WhileSubscribed(5_000))`, so the upstream Room flow is collected ONLY
 * while the UI is subscribed and stops ~5s after the last subscriber leaves.
 *
 * Uses [UnconfinedTestDispatcher] (sharing runTest's scheduler) so a launched collector subscribes
 * eagerly/synchronously, while `advanceTimeBy` still drives the WhileSubscribed stop timeout.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class OverdueViewModelTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `upstream Room flow is collected only while state has subscribers`() = runTest(dispatcher) {
        val repo = FakeControlTowerRepository()
        val vm = OverdueViewModel(repo)

        // No subscriber -> upstream stays cold despite init { refresh() }.
        assertEquals(0, repo.activeSummaryCollectors)

        val job = launch { vm.state.collect {} }
        assertEquals(1, repo.activeSummaryCollectors)

        job.cancel()
        // Still warm inside the 5s replay window.
        assertEquals(1, repo.activeSummaryCollectors)

        advanceTimeBy(6_000)
        // Past the WhileSubscribed timeout the upstream is cancelled.
        assertEquals(0, repo.activeSummaryCollectors)
    }

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
        ): ControlTowerResponseDto = ControlTowerResponseDto()

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
