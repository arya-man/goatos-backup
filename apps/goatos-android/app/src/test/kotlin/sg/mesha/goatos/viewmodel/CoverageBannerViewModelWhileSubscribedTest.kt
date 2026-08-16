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
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * MOB-010 guardrail (mirrors [AlertsViewModelWhileSubscribedTest]): proves [CoverageBannerViewModel.state]
 * is exposed via `stateIn(SharingStarted.WhileSubscribed(5_000))` over a flow where [state] itself
 * is the ONLY subscriber of the Room [RosterRepository.observeCoverage] flow, and that the background
 * refresh triggered by [kotlinx.coroutines.flow.onStart] fires exactly once per subscription -- not
 * once per Room emission.
 *
 * The prior bug had a permanent `.collect` inside a `viewModelScope.launch` sitting on top of an
 * inner `stateIn(WhileSubscribed)`, so the upstream Room flow was collected forever (WhileSubscribed
 * was a no-op), and a background refresh fired on EVERY Room emission -- including the emission the
 * refresh itself caused, i.e. refresh -> Room re-emit -> refresh forever.
 *
 * Uses [UnconfinedTestDispatcher] (sharing runTest's scheduler) so a launched collector subscribes
 * eagerly/synchronously, while `advanceTimeBy` still drives the WhileSubscribed stop timeout.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class CoverageBannerViewModelWhileSubscribedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun coverage(has: Boolean, banner: String? = null) =
        MyCoverageResponseDto(coverage = MyCoverageDto(hasCoverage = has, bannerText = banner))

    @Test
    fun `upstream Room flow is collected only while state has subscribers`() = runTest(dispatcher) {
        val repo = FakeRosterRepository().apply { setCachedCoverage(coverage(true, "Covering PM1")) }
        val viewModel = CoverageBannerViewModel(repo)

        // No UI subscriber yet -> WhileSubscribed keeps the upstream cold.
        assertEquals(0, repo.activeCoverageCollectors)

        // Subscribe (simulates the screen collecting state) — Unconfined subscribes synchronously.
        val job1 = launch { viewModel.state.collect {} }
        assertEquals(1, repo.activeCoverageCollectors)

        // Unsubscribe (screen backgrounded). Within the 5s window it stays warm...
        job1.cancel()
        assertEquals(1, repo.activeCoverageCollectors)

        // ...but after the 5s WhileSubscribed timeout it stops — no forever collection.
        advanceTimeBy(6_000)
        assertEquals(0, repo.activeCoverageCollectors)

        // Returning to the screen restarts the upstream collection.
        val job2 = launch { viewModel.state.collect {} }
        assertEquals(1, repo.activeCoverageCollectors)

        job2.cancel()
    }

    @Test
    fun `background refresh fires once per subscription, not once per Room emission`() = runTest(dispatcher) {
        val repo = FakeRosterRepository().apply { setCachedCoverage(coverage(true, "Covering PM1")) }
        val viewModel = CoverageBannerViewModel(repo)

        // Subscribing activates the flow and its `onStart { refreshInBackground() }` fires once.
        val job = launch { viewModel.state.collect {} }
        assertEquals(1, repo.refreshCount)

        // Simulate the successful refresh's own Room upsert re-emitting. If the old bug were
        // present (refresh triggered per Room emission) this would bump refreshCount again.
        repo.emitFreshCoverage(coverage(true, "Covering PM1 refreshed"))
        assertEquals("Room re-emit must NOT re-trigger a refresh", 1, repo.refreshCount)

        // A second unrelated re-emission still must not grow the refresh count.
        repo.emitFreshCoverage(coverage(false))
        assertEquals("refresh count stays bounded across repeated emissions", 1, repo.refreshCount)

        job.cancel()
    }

    /** Fake repo whose observe flow tracks how many collectors are currently active. */
    private class FakeRosterRepository : RosterRepository {
        private val coverageFlow = MutableStateFlow<MyCoverageResponseDto?>(null)
        var activeCoverageCollectors = 0
            private set
        var refreshCount = 0
            private set

        fun setCachedCoverage(data: MyCoverageResponseDto?) { coverageFlow.value = data }
        fun emitFreshCoverage(data: MyCoverageResponseDto) { coverageFlow.value = data }

        override fun observeTimetable(centerId: String): Flow<EnrichedPositionListResponseDto?> =
            MutableStateFlow<EnrichedPositionListResponseDto?>(null)

        override fun observeCoverage(): Flow<MyCoverageResponseDto?> =
            object : Flow<MyCoverageResponseDto?> {
                override suspend fun collect(collector: FlowCollector<MyCoverageResponseDto?>) {
                    activeCoverageCollectors++
                    try {
                        coverageFlow.collect(collector)
                    } finally {
                        activeCoverageCollectors--
                    }
                }
            }

        override suspend fun refreshTimetable(centerId: String, limit: Int?): Result<Unit> = Result.success(Unit)

        override suspend fun refreshCoverage(): Boolean {
            refreshCount++
            return true
        }
    }
}
