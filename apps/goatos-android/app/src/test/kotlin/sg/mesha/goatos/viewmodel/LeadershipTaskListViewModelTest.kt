package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
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
import sg.mesha.goatos.boot.NavStateRefreshSignal
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsLeadershipTasks
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.LeadershipTaskPageMeta
import sg.mesha.goatos.core.network.dto.LeadershipTaskFilterDto
import sg.mesha.goatos.core.network.dto.LeadershipTaskScopeDto
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskListEvent

/**
 * The Leadership Tasks L0 list state holder (maintainer request 2026-09-04).
 *
 * What these hold: backend copy reaches the card VERBATIM, the unseen rail follows the
 * assignee's own payload, the "+" follows `can_raise` and nothing else, and a page landing asks
 * the shell to re-read the badge.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class LeadershipTaskListViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a card renders backend copy verbatim and composes none of it`() {
        val card = leadershipTask(isSeen = false, canChangeStatus = true).copy(attachmentCount = 3).toCardUi()

        assertEquals(LEADERSHIP_TEST_TASK_ID, card.listKey)
        assertEquals("#12", card.numberLabel)
        assertEquals("Open", card.statusChip)
        assertEquals("Fix the CPT water line", card.title)
        assertEquals("Raised by Hemant · 4 Sep 2026", card.metaLine)
        assertEquals(3, card.attachmentCount)
        assertTrue("an unseen assigned task carries the rail", card.unseen)
        val clock = card.deadline ?: error("a task with a deadline carries its clock")
        assertEquals(4, clock.daysTaken)
        assertEquals("4 days", clock.daysTakenLabel)
        assertEquals("15/09/2026 17:00", clock.deadlineLabel)
        assertEquals("Within deadline", clock.stateLabel)
        assertFalse(clock.isLate)
    }

    @Test
    fun `the day counter is the backend's number and colour, and a task without a deadline shows none`() {
        // Maintainer decision 2026-09-14: the phone counts no days and picks no colour.
        val late = leadershipTask(daysTaken = 9, deadlineTone = "late").toCardUi().deadline ?: error("late clock")
        assertEquals(9, late.daysTaken)
        assertTrue(late.isLate)
        assertEquals("Past deadline", late.stateLabel)
        assertEquals("no deadline, no counter", null, leadershipTask(deadlineAt = null, daysTaken = null, deadlineTone = "").toCardUi().deadline)
    }

    @Test
    fun `the unseen rail belongs to the assignee, never the raiser`() {
        // A raiser's row says is_seen=false until the CXO opens it — that is news about the CXO,
        // not a task the raiser has yet to read.
        assertFalse(leadershipTask(isSeen = false, canChangeStatus = false, canEdit = true).toCardUi().unseen)
        assertFalse(leadershipTask(isSeen = true, canChangeStatus = true).toCardUi().unseen)
        assertTrue(leadershipTask(isSeen = false, canChangeStatus = true).toCardUi().unseen)
    }

    @Test
    fun `the raise action follows can_raise and the badge is re-read when a page lands`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository()
        val signal = NavStateRefreshSignal()
        val refreshRequests = mutableListOf<Unit>()
        val signalJob = backgroundScope.launch { signal.requests.collect { refreshRequests += it } }
        val vm = LeadershipTaskListViewModel(repository, RecordingAnalytics(), NoopCrashReporter(), signal)
        vm.bind("Tasks")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Before any page: the nav label is the title, and no "+" — nothing said the caller may raise.
        assertEquals("Tasks", vm.state.value.title)
        assertFalse(vm.state.value.canRaise)
        assertTrue("no badge re-read before a page lands", refreshRequests.isEmpty())

        repository.emitPageMeta(
            LeadershipTaskPageMeta(
                title = "Tasks",
                filters = listOf(
                    LeadershipTaskFilterDto(key = "all", label = "All", count = 4, selected = true, emptyMessage = "No tasks yet"),
                    LeadershipTaskFilterDto(key = "open", label = "Open", count = 2, selected = false, emptyMessage = "Nothing open"),
                    LeadershipTaskFilterDto(key = "done", label = "Done", count = 2, selected = false, emptyMessage = "Nothing done yet"),
                ),
                scopes = listOf(
                    LeadershipTaskScopeDto(key = "assigned_to_me", label = "Assigned to me", count = 2, selected = true, emptyMessage = "No tasks assigned to you yet"),
                    LeadershipTaskScopeDto(key = "assigned_by_me", label = "Assigned by me", count = 4, selected = false, emptyMessage = "No tasks raised by you yet"),
                    LeadershipTaskScopeDto(key = "team_progress", label = "Team progress", count = 9, selected = false, emptyMessage = "No team tasks in progress yet"),
                ),
                unseenCount = 2,
                canRaise = true,
            ),
        )
        advanceUntilIdle()

        assertTrue("+ follows the backend's can_raise", vm.state.value.canRaise)
        assertEquals(listOf("All", "Open", "Done"), vm.state.value.filters.map { it.label })
        assertEquals(listOf(4, 2, 2), vm.state.value.filters.map { it.count })
        assertEquals("all", vm.state.value.filters.single { it.selected }.key)
        assertEquals("assigned_to_me", vm.state.value.scopes.single { it.selected }.key)
        assertEquals("No tasks assigned to you yet", vm.state.value.emptyMessage)
        assertEquals("a landed page re-reads the badge once", 1, refreshRequests.size)

        vm.onEvent(LeadershipTaskListEvent.SelectFilter("open"))
        advanceUntilIdle()
        assertEquals("open", vm.state.value.filters.single { it.selected }.key)
        assertEquals("Nothing open", vm.state.value.emptyMessage)

        vm.onEvent(LeadershipTaskListEvent.SelectScope("team_progress"))
        advanceUntilIdle()
        assertEquals("team_progress", vm.state.value.scopes.single { it.selected }.key)
        assertEquals("all", vm.state.value.filters.single { it.selected }.key)
        assertEquals("No team tasks in progress yet", vm.state.value.emptyMessage)

        signalJob.cancel()
        stateJob.cancel()
    }

    @Test
    fun `refresh drops the scope freshness marker and a page failure is instrumented`() = runTest(dispatcher) {
        val repository = FakeLeadershipTasksRepository()
        val analytics = RecordingAnalytics()
        val vm = LeadershipTaskListViewModel(repository, analytics, NoopCrashReporter(), NavStateRefreshSignal())
        vm.bind("Tasks")
        val rowsJob = backgroundScope.launch { vm.rows.collect {} }
        advanceUntilIdle()

        vm.onEvent(LeadershipTaskListEvent.Refresh)
        vm.onEvent(LeadershipTaskListEvent.OpenTask("task-1"))
        vm.onRowsLoadFailed(IllegalStateException("network down"))
        advanceUntilIdle()

        assertEquals(listOf(""), repository.invalidatedScopes)
        assertEquals(listOf(""), repository.invalidatedFilters)
        assertEquals("", repository.requestedFilters.first())
        assertTrue(analytics.events.any { it.name == AnalyticsEventsLeadershipTasks.LIST_VIEWED })
        assertTrue(analytics.events.any { it.name == AnalyticsEventsLeadershipTasks.TASK_OPENED })
        val failure = analytics.events.single { it.name == AnalyticsEventsLeadershipTasks.FAILURE }
        assertEquals("network down", failure.props[AnalyticsEvents.Params.REASON])

        rowsJob.cancel()
    }
}
