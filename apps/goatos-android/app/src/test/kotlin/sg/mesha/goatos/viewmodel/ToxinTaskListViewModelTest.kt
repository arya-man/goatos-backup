package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsToxin
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.network.dto.ToxinTaskDto
import sg.mesha.goatos.feature.toxin.ToxinTaskListEvent

/**
 * The Toxin L0 list state holder (module toxin, maintainer decision 2026-08-25).
 *
 * What these hold: backend copy reaches the card VERBATIM, a round that is no longer the tester's
 * to work does not open, and an explicit refresh drops the freshness marker so the pager actually
 * refetches instead of TTL-skipping.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ToxinTaskListViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a card renders backend copy verbatim and composes none of it`() {
        val card = ToxinTaskDto(
            taskId = "task-1",
            status = "in_progress",
            statusChip = "In progress",
            contextLine = "Maize · Kumar Traders · Load 4 · 25 Aug",
            originLine = "Retest after an invalid strip",
            stepsDone = 3,
            stepsTotal = 7,
            canExecute = true,
        ).toCardUi()

        assertEquals("task-1", card.listKey)
        assertEquals("In progress", card.statusChip)
        assertEquals("Maize · Kumar Traders · Load 4 · 25 Aug", card.contextLine)
        assertEquals("Retest after an invalid strip", card.originLine)
        assertEquals(3, card.stepsDone)
        assertEquals(7, card.stepsTotal)
        assertTrue(card.openable)
    }

    @Test
    fun `a round that is no longer in progress keeps its chip but does not open`() {
        // A submitted, accepted, or cancelled round is closed to the tester. A cancelled round's
        // replacement arrives as its OWN task (retest = a new round, never an edit), so closing
        // this row strands nothing.
        listOf("pending_review", "accepted", "cancelled").forEach { status ->
            val card = ToxinTaskDto(taskId = "task-$status", status = status, statusChip = "Chip", canExecute = true).toCardUi()
            assertFalse("status=$status must not open", card.openable)
            assertEquals("Chip", card.statusChip)
        }
    }

    @Test
    fun `a watcher sees the same open round as a card that does not open`() {
        // Maintainer decision 2026-08-26: CEO/CXO watches and casts the verdict; only the named
        // testers run the test. Their card still carries the full backend chip and context line —
        // it simply stops being a way in, so nobody films a step the server would refuse.
        val watched = ToxinTaskDto(
            taskId = "task-1",
            status = "in_progress",
            statusChip = "In progress",
            contextLine = "Maize · Kumar Traders · Load 4 · 25 Aug",
            canExecute = false,
        ).toCardUi()

        assertFalse("a watcher's card must not open", watched.openable)
        assertEquals("In progress", watched.statusChip)
        assertEquals("Maize · Kumar Traders · Load 4 · 25 Aug", watched.contextLine)
    }

    @Test
    fun `refresh drops the scope freshness marker so the pager refetches`() = runTest(dispatcher) {
        val repository = FakeToxinRepository()
        val analytics = RecordingAnalytics()
        val vm = ToxinTaskListViewModel(repository, analytics, NoopCrashReporter())
        vm.bind("Tests")
        advanceUntilIdle()

        vm.onEvent(ToxinTaskListEvent.Refresh)
        advanceUntilIdle()

        assertEquals(listOf(""), repository.invalidatedStatuses)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsToxin.TOXIN_LIST_VIEWED })
    }

    @Test
    fun `opening a task and a page load failure are both instrumented`() = runTest(dispatcher) {
        val repository = FakeToxinRepository()
        val analytics = RecordingAnalytics()
        val vm = ToxinTaskListViewModel(repository, analytics, NoopCrashReporter())
        vm.bind("Tests")

        vm.onEvent(ToxinTaskListEvent.OpenTask("task-1"))
        vm.onRowsLoadFailed(IllegalStateException("network down"))
        advanceUntilIdle()

        assertTrue(analytics.events.any { it.name == AnalyticsEventsToxin.TOXIN_TASK_OPENED })
        val failure = analytics.events.single { it.name == AnalyticsEventsToxin.TOXIN_FAILURE }
        // The REAL reason, never a fabricated code.
        assertEquals("network down", failure.props[AnalyticsEvents.Params.REASON])
    }
}
