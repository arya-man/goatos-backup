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
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * MOB-010 guardrail (mirrors [AlertsViewModelWhileSubscribedTest]): proves [TimetableViewModel.state]
 * is exposed via `stateIn(SharingStarted.WhileSubscribed(5_000))` over a flow where [state] itself
 * is the ONLY subscriber of the Room [RosterRepository.observeTimetable] flow, and that the
 * background refresh triggered by [kotlinx.coroutines.flow.onStart] fires exactly once per
 * subscription -- not once per Room emission.
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
class TimetableViewModelWhileSubscribedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `upstream Room flow is collected only while state has subscribers`() = runTest(dispatcher) {
        val repo = FakeRosterRepository().apply { setCachedTimetable(timetable("P1")) }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")
        val viewModel = TimetableViewModel(repo, bootstrap)

        // No UI subscriber yet -> WhileSubscribed keeps the upstream cold.
        assertEquals(0, repo.activeTimetableCollectors)

        // Subscribe (simulates the screen collecting state) — Unconfined subscribes synchronously.
        val job1 = launch { viewModel.state.collect {} }
        assertEquals(1, repo.activeTimetableCollectors)

        // Unsubscribe (screen backgrounded). Within the 5s window it stays warm...
        job1.cancel()
        assertEquals(1, repo.activeTimetableCollectors)

        // ...but after the 5s WhileSubscribed timeout it stops — no forever collection.
        advanceTimeBy(6_000)
        assertEquals(0, repo.activeTimetableCollectors)

        // Returning to the screen restarts the upstream collection.
        val job2 = launch { viewModel.state.collect {} }
        assertEquals(1, repo.activeTimetableCollectors)

        job2.cancel()
    }

    @Test
    fun `background refresh fires once per subscription, not once per Room emission`() = runTest(dispatcher) {
        val repo = FakeRosterRepository().apply { setCachedTimetable(timetable("P1")) }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")
        val viewModel = TimetableViewModel(repo, bootstrap)

        // Subscribing activates the flow and its `onStart { refreshInBackground(...) }` fires once.
        val job = launch { viewModel.state.collect {} }
        assertEquals(1, repo.refreshCount)

        // Simulate the successful refresh's own Room upsert re-emitting. If the old bug were
        // present (refresh triggered per Room emission) this would bump refreshCount again.
        repo.emitFreshTimetable(timetable("P1-refreshed"))
        assertEquals("Room re-emit must NOT re-trigger a refresh", 1, repo.refreshCount)

        // A second unrelated re-emission still must not grow the refresh count.
        repo.emitFreshTimetable(timetable("P1-refreshed-again"))
        assertEquals("refresh count stays bounded across repeated emissions", 1, repo.refreshCount)

        job.cancel()
    }

    private fun timetable(code: String) =
        EnrichedPositionListResponseDto(items = listOf(EnrichedPositionDto(positionId = "p1", positionCode = code)))

    /** Fake repo whose observe flow tracks how many collectors are currently active. */
    private class FakeRosterRepository : RosterRepository {
        private val timetableFlow = MutableStateFlow<EnrichedPositionListResponseDto?>(null)
        var activeTimetableCollectors = 0
            private set
        var refreshCount = 0
            private set

        fun setCachedTimetable(data: EnrichedPositionListResponseDto?) { timetableFlow.value = data }
        fun emitFreshTimetable(data: EnrichedPositionListResponseDto) { timetableFlow.value = data }

        override fun observeTimetable(centerId: String): Flow<EnrichedPositionListResponseDto?> =
            object : Flow<EnrichedPositionListResponseDto?> {
                override suspend fun collect(collector: FlowCollector<EnrichedPositionListResponseDto?>) {
                    activeTimetableCollectors++
                    try {
                        timetableFlow.collect(collector)
                    } finally {
                        activeTimetableCollectors--
                    }
                }
            }

        override fun observeCoverage(): Flow<MyCoverageResponseDto?> =
            MutableStateFlow<MyCoverageResponseDto?>(null)

        override suspend fun refreshTimetable(centerId: String, limit: Int?): Boolean {
            refreshCount++
            return true
        }

        override suspend fun refreshCoverage(): Boolean = true
    }

    private class FakeBootstrapRepository(private val centerId: String?) : BootstrapRepository {
        override suspend fun loadNavState(): NavState = NavState(NavChrome.MINIMAL, emptyList())
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
            BootstrapOperatorProfileDto(displayName = "Test User", primaryLocationId = centerId)
    }
}
