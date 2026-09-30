package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.Json
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenRoutines
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.PenRoutinePageMeta
import sg.mesha.goatos.core.data.PenRoutineQuery
import sg.mesha.goatos.core.network.dto.PenRoutinePenOptionDto
import sg.mesha.goatos.core.network.dto.PenRoutineTabDto
import sg.mesha.goatos.core.ui.filters.WorklistDateWindow
import sg.mesha.goatos.core.ui.filters.WorklistStatus
import java.time.LocalDate
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_SCOPE_ALL_PENS
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_SCOPE_PARK
import sg.mesha.goatos.core.network.dto.PenRoutineTaskDto
import sg.mesha.goatos.feature.penroutines.PenRoutineListEvent
import sg.mesha.goatos.feature.penroutines.PenRoutineTone

/**
 * The Routines L0 list state holder (maintainer instruction 2026-09-16).
 *
 * What these hold: backend copy reaches the card VERBATIM and the card composes none of it; the
 * "Sending" mark follows the outbox's active grain set and never outranks a server `Done`; the
 * filter chips and their empty copy are the backend's; a page load failure is reported, never
 * swallowed.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PenRoutineListViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a card renders backend copy verbatim and composes none of it`() {
        val card = penRoutineTask(stateChip = "Delayed since 14 Sep", stateTone = "danger").toCardUi()

        assertEquals(PEN_ROUTINE_TEST_TASK_ID, card.listKey)
        assertEquals("Pen cleaning · Castro 2 · Coimbatore", card.title)
        // The pen label is the backend's operational_location_display, never re-composed.
        assertEquals("Castro 2", card.penLabel)
        assertEquals("Every day", card.reasonLine)
        assertEquals("2 questions · 1 photo · check in", card.evidenceLine)
        assertEquals("Delayed since 14 Sep", card.stateChip)
        assertEquals(PenRoutineTone.DANGER, card.tone)
        assertFalse(card.sending)
        assertFalse(card.done)
    }

    @Test
    fun `a general park task renders with no pen line`() {
        val card = penRoutineParkTask().toCardUi()

        assertTrue(card.parkTask)
        assertEquals("", card.penLabel)
        assertEquals("Medicine store check · Coimbatore", card.title)
        assertEquals(PEN_ROUTINE_TEST_TASK_ID, card.listKey)
        assertFalse(penRoutineTask().toCardUi().parkTask)
    }

    @Test
    fun `a task without scope_kind decodes as all_pens`() {
        val json = Json { ignoreUnknownKeys = true }
        val legacy = json.decodeFromString<PenRoutineTaskDto>(
            """{"task_id":"t-1","title":"Pen cleaning · Castro 2 · Coimbatore","operational_location_display":"Castro 2"}""",
        )
        assertEquals(PEN_ROUTINE_SCOPE_ALL_PENS, legacy.scopeKind)
        assertFalse(legacy.toCardUi().parkTask)
        assertEquals("Castro 2", legacy.toCardUi().penLabel)

        val park = json.decodeFromString<PenRoutineTaskDto>(
            """{"task_id":"t-2","scope_kind":"park","shed_id":"","operational_location_display":""}""",
        )
        assertEquals(PEN_ROUTINE_SCOPE_PARK, park.scopeKind)
        assertTrue(park.toCardUi().parkTask)
    }

    @Test
    fun `the sending mark follows the outbox and never outranks the gate`() {
        assertTrue(penRoutineTask().toCardUi(sending = true).sending)
        // Work the verifier sent back may be on the wire again.
        assertTrue(penRoutineTask(status = "rework", stateChip = "Sent back", stateTone = "danger").toCardUi(sending = true).sending)
        // A submit already with the verifier reached the server: never "sending".
        val inReview = penRoutineTask(status = "pending_verification", stateChip = "In review", stateTone = "review").toCardUi(sending = true)
        assertFalse(inReview.sending)
        assertFalse(inReview.done)
        // A task the server already calls done is done, whatever the outbox still holds.
        val done = penRoutineTask(status = "completed", workState = "completed", stateChip = "Done", stateTone = "success", verified = true).toCardUi(sending = true)
        assertFalse(done.sending)
        assertTrue(done.done)
    }

    @Test
    fun `filters and empty copy are the backend's and the selected chip picks the empty line`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository()
        val vm = PenRoutineListViewModel(repository, RecordingPenRoutineSyncRepository(), RecordingAnalytics(), NoopCrashReporter())
        vm.bind("Routines")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Before any page: the nav label is the title.
        assertEquals("Routines", vm.state.value.title)
        assertTrue(vm.state.value.filters.isEmpty())

        repository.emitPageMeta(PenRoutinePageMeta(title = "Routines", filters = penRoutineFilters(selected = "todo"), openCount = 4))
        advanceUntilIdle()

        assertEquals(listOf("todo", "done"), vm.state.value.filters.map { it.key })
        assertEquals(listOf("To do", "Done"), vm.state.value.filters.map { it.label })
        assertEquals(listOf(4, 9), vm.state.value.filters.map { it.count })
        assertEquals("No routines due today.", vm.state.value.emptyMessage)

        // Tapping a chip re-scopes the pager on the backend KEY and the empty line follows it.
        vm.onEvent(PenRoutineListEvent.SelectFilter("done"))
        advanceUntilIdle()
        assertEquals("Nothing done yet.", vm.state.value.emptyMessage)
        assertTrue(vm.state.value.filters.single { it.key == "done" }.selected)
        val rowsJob = backgroundScope.launch { vm.rows.collect {} }
        advanceUntilIdle()
        assertTrue("the pager was re-scoped on the tapped key", "done" in repository.requestedFilters)
        rowsJob.cancel()
        stateJob.cancel()
    }

    @Test
    fun `refresh drops the scope's freshness marker and a load failure is reported`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository()
        val analytics = RecordingAnalytics()
        val vm = PenRoutineListViewModel(repository, RecordingPenRoutineSyncRepository(), analytics, NoopCrashReporter())
        vm.bind("Routines")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(PenRoutineListEvent.Refresh)
        advanceUntilIdle()
        assertEquals(listOf(""), repository.invalidatedFilters)

        vm.onRowsLoadFailed(IllegalStateException("socket closed"))
        val failure = analytics.events.single { it.name == AnalyticsEventsPenRoutines.FAILURE }
        assertEquals("socket closed", failure.props[AnalyticsEvents.Params.REASON])
        assertTrue(analytics.events.any { it.name == AnalyticsEventsPenRoutines.LIST_OPENED })
        stateJob.cancel()
    }

    @Test
    fun `a tab-bound list asks for its tab, titles itself from the tab and draws only its filters`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository()
        val analytics = RecordingAnalytics()
        val vm = PenRoutineListViewModel(repository, RecordingPenRoutineSyncRepository(), analytics, NoopCrashReporter())
        vm.today = { LocalDate.of(2026, 10, 1) }
        vm.bind("Fumigation", "fumigation")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        val rowsJob = backgroundScope.launch { vm.rows.collect {} }
        advanceUntilIdle()

        // Before a page lands: the bar item's label; the request already names the tab, unnarrowed.
        assertEquals("Fumigation", vm.state.value.title)
        assertEquals(PenRoutineQuery(tab = "fumigation"), repository.requestedQueries.last())
        assertTrue("no Routines request leaked from the tab", repository.requestedQueries.none { it.tab.isBlank() })

        // Another tab's page facts never reach this one.
        repository.emitPageMeta(PenRoutinePageMeta(title = "Routines", filters = penRoutineFilters()), tab = "")
        repository.emitPageMeta(
            PenRoutinePageMeta(
                title = "Fumigation rounds",
                filters = penRoutineFilters(selected = "todo"),
                tab = PenRoutineTabDto(key = "fumigation", label = "Fumigation rounds", filters = listOf("status", "pen")),
                penOptions = listOf(
                    PenRoutinePenOptionDto(
                        value = "shed-castro|2",
                        shedId = "shed-castro",
                        partitionLabel = "2",
                        label = "Castro 2",
                        operationalLocationDisplay = "Castro 2",
                        parkName = "Coimbatore",
                        count = 3,
                    ),
                ),
            ),
            tab = "fumigation",
        )
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("Fumigation rounds", state.title)
        val bar = requireNotNull(state.tabFilters)
        assertTrue(bar.showStatus)
        assertFalse("date is not offered by this tab", bar.showDate)
        assertTrue(bar.showPen)
        assertEquals(4, bar.pendingCount)
        assertEquals(9, bar.completedCount)
        assertEquals(WorklistStatus.PENDING, bar.status)
        assertEquals(listOf("Castro 2"), bar.penOptions.map { it.label })

        // Status -> the backend's done key; pen -> the option's own token; date -> ISO window.
        vm.onEvent(PenRoutineListEvent.SelectFilter("done"))
        vm.onEvent(PenRoutineListEvent.SelectPen(bar.penOptions.single().pen))
        vm.onEvent(PenRoutineListEvent.SelectDateWindow(WorklistDateWindow(LocalDate.of(2026, 9, 28), LocalDate.of(2026, 10, 1))))
        advanceUntilIdle()
        assertEquals(
            PenRoutineQuery(filter = "done", tab = "fumigation", dueFrom = "2026-09-28", dueTo = "2026-10-01", pen = "shed-castro|2"),
            repository.requestedQueries.last(),
        )
        assertEquals(WorklistStatus.COMPLETED, vm.state.value.tabFilters?.status)

        // Clearing the date drops it from the request again.
        vm.onEvent(PenRoutineListEvent.SelectDateWindow(null))
        advanceUntilIdle()
        assertEquals("", repository.requestedQueries.last().dueFrom)

        val filterEvents = analytics.events.filter { it.name == AnalyticsEventsPenRoutines.FILTER_CHANGED }
        assertEquals(listOf("status", "pen", "date", "date"), filterEvents.map { it.props[AnalyticsEvents.Params.KIND] })
        assertTrue(filterEvents.all { it.props[AnalyticsEventsPenRoutines.Params.TAB_KEY] == "fumigation" })
        rowsJob.cancel()
        stateJob.cancel()
    }

    @Test
    fun `the Routines list has no tab bar and requests exactly what it always did`() = runTest(dispatcher) {
        val repository = FakePenRoutinesRepository()
        val vm = PenRoutineListViewModel(repository, RecordingPenRoutineSyncRepository(), RecordingAnalytics(), NoopCrashReporter())
        vm.bind("Routines")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        val rowsJob = backgroundScope.launch { vm.rows.collect {} }
        repository.emitPageMeta(PenRoutinePageMeta(title = "Routines", filters = penRoutineFilters()))
        advanceUntilIdle()
        assertNull(vm.state.value.tabFilters)
        assertEquals(PenRoutineQuery(), repository.requestedQueries.last())
        rowsJob.cancel()
        stateJob.cancel()
    }
}
