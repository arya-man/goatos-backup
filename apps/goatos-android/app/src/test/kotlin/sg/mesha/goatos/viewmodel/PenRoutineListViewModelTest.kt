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
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenRoutines
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.PenRoutinePageMeta
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
}
