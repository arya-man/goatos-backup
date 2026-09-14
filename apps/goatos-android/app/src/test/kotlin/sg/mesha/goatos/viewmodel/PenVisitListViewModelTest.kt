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
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenVisits
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.PenVisitPageMeta
import sg.mesha.goatos.feature.penvisits.PenVisitListEvent
import sg.mesha.goatos.feature.penvisits.PenVisitTone

/**
 * The pen-visit "For me" L0 list state holder (maintainer decision 2026-09-07).
 *
 * What these hold: backend copy reaches the card VERBATIM and the card composes none of it; the
 * "Sending" mark follows the outbox's active grain set and never outranks a server `Done`; the
 * filter chips and their empty copy are the backend's; a page load failure is reported, never
 * swallowed.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PenVisitListViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a card renders backend copy verbatim and composes none of it`() {
        val card = penVisit(stateChip = "Delayed since 5 Sep", stateTone = "danger").toCardUi()

        assertEquals(PEN_VISIT_TEST_TASK_ID, card.listKey)
        assertEquals("Visit Castro 2 · Coimbatore", card.title)
        // The pen label is the backend's operational_location_display, never shed + partition
        // re-composed on the phone.
        assertEquals("Castro 2", card.penLabel)
        assertEquals("Vaccination yesterday", card.reasonLine)
        assertEquals("Delayed since 5 Sep", card.stateChip)
        assertEquals(PenVisitTone.DANGER, card.tone)
        assertFalse(card.sending)
        assertFalse(card.done)
    }

    @Test
    fun `the sending mark follows the outbox and never outranks the verifier gate`() {
        assertTrue(penVisit().toCardUi(sending = true).sending)
        // A recording the verifier sent back may be on the wire again.
        assertTrue(penVisit(status = "rework", stateChip = "Visit needs another video", stateTone = "danger").toCardUi(sending = true).sending)
        // A clip already with the verifier reached the server: never "sending", whatever the outbox holds.
        val inReview = penVisit(status = "pending_verification", stateChip = "Visit in review", stateTone = "review").toCardUi(sending = true)
        assertFalse(inReview.sending)
        assertFalse(inReview.done)
        // A visit the server already calls done is done, whatever the outbox still holds.
        val done = penVisit(status = "completed", workState = "completed", stateChip = "Visit verified", stateTone = "success", verified = true).toCardUi(sending = true)
        assertFalse(done.sending)
        assertTrue(done.done)
    }

    @Test
    fun `filters and empty copy are the backend's and the selected chip picks the empty line`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository()
        val vm = PenVisitListViewModel(repository, RecordingPenVisitSyncRepository(), RecordingAnalytics(), NoopCrashReporter())
        vm.bind("For me")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Before any page: the nav label is the title.
        assertEquals("For me", vm.state.value.title)
        assertTrue(vm.state.value.filters.isEmpty())

        repository.emitPageMeta(PenVisitPageMeta(title = "For me", filters = penVisitFilters(selected = "todo"), openCount = 2))
        advanceUntilIdle()

        assertEquals(listOf("todo", "done"), vm.state.value.filters.map { it.key })
        assertEquals(listOf("To do", "Done"), vm.state.value.filters.map { it.label })
        assertEquals("No pens to visit today.", vm.state.value.emptyMessage)

        // Tapping a chip re-scopes the pager on the backend KEY and the empty line follows it.
        vm.onEvent(PenVisitListEvent.SelectFilter("done"))
        advanceUntilIdle()
        assertEquals("Nothing visited yet.", vm.state.value.emptyMessage)
        assertTrue(vm.state.value.filters.single { it.key == "done" }.selected)
        stateJob.cancel()
    }

    @Test
    fun `refresh drops the scope's freshness marker and a load failure is reported`() = runTest(dispatcher) {
        val repository = FakePenVisitsRepository()
        val analytics = RecordingAnalytics()
        val vm = PenVisitListViewModel(repository, RecordingPenVisitSyncRepository(), analytics, NoopCrashReporter())
        vm.bind("For me")
        val stateJob = backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(PenVisitListEvent.Refresh)
        advanceUntilIdle()
        assertEquals(listOf(""), repository.invalidatedFilters)

        vm.onRowsLoadFailed(IllegalStateException("socket closed"))
        val failure = analytics.events.single { it.name == AnalyticsEventsPenVisits.FAILURE }
        assertEquals("socket closed", failure.props[AnalyticsEvents.Params.REASON])
        assertTrue(analytics.events.any { it.name == AnalyticsEventsPenVisits.LIST_VIEWED })
        stateJob.cancel()
    }
}
