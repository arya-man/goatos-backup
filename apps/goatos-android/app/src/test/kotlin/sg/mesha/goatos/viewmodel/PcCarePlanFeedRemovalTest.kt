package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.network.dto.PcCarePlannerShedDto
import sg.mesha.goatos.core.network.dto.PcCarePlannerShedsDto
import sg.mesha.goatos.feature.pccare.PcCarePlanEvent
import sg.mesha.goatos.feature.pccare.PcCarePlanStep
import java.time.LocalDate

/**
 * The deworming wizard's feed & water removal half (maintainer decision 2026-09-03):
 *
 *  - the toggle is offered ONLY on the deworming wizard;
 *  - toggle ON blocks REVIEW until removal people are picked (mutation check: drop the
 *    removal-operator branch in `nextStep` and that test goes red);
 *  - the create request carries the two new fields exactly when the toggle is ON, and NEITHER
 *    when it is off (injection deworming stays byte-compatible with the pre-feature payload).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCarePlanFeedRemovalTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun repositoryWithOnePen() = FakePcCareRepository().apply {
        plannerSheds = PcCarePlannerShedsDto(
            sheds = listOf(
                PcCarePlannerShedDto(
                    shedId = "shed-1",
                    shedLabel = "Castro",
                    partitionLabel = "Part 1",
                    operationalLocationDisplay = "Castro - 1",
                ),
            ),
        )
    }

    private fun viewModel(repository: FakePcCareRepository) = PcCarePlanViewModel(
        repository = repository,
        submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) },
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
    )

    /** Walks the wizard through DATE -> PARK -> PEN -> OPERATORS with one of everything picked. */
    private suspend fun kotlinx.coroutines.test.TestScope.walkToOperators(vm: PcCarePlanViewModel) {
        vm.onEvent(PcCarePlanEvent.NextStep) // DATE -> PARK (today preselected)
        vm.onEvent(PcCarePlanEvent.SelectPark("park-1"))
        vm.onEvent(PcCarePlanEvent.NextStep) // PARK -> PEN (loads pens)
        advanceUntilIdle()
        vm.onEvent(PcCarePlanEvent.SelectPen("shed-1", "Part 1"))
        vm.onEvent(PcCarePlanEvent.NextStep) // PEN -> OPERATORS
        vm.onEvent(PcCarePlanEvent.ToggleOperator("op-1"))
        assertEquals(PcCarePlanStep.OPERATORS, vm.state.value.step)
    }

    @Test
    fun `toggle is offered only on the deworming wizard`() = runTest(dispatcher) {
        val vm = viewModel(repositoryWithOnePen())
        vm.bindWizard("deworming", "Deworming")
        assertEquals(true, vm.state.value.feedRemovalOffered)

        vm.bindWizard("ticks_removal", "Ticks removal")
        assertEquals(false, vm.state.value.feedRemovalOffered)
    }

    @Test
    fun `toggle on blocks review until removal people are picked`() = runTest(dispatcher) {
        val vm = viewModel(repositoryWithOnePen())
        vm.bindWizard("deworming", "Deworming")
        walkToOperators(vm)
        vm.onEvent(PcCarePlanEvent.ToggleFeedRemoval)

        vm.onEvent(PcCarePlanEvent.NextStep)
        assertEquals("review is refused while nobody removes feed & water", PcCarePlanStep.OPERATORS, vm.state.value.step)
        assertTrue(vm.state.value.message.orEmpty().isNotBlank())

        vm.onEvent(PcCarePlanEvent.ToggleRemovalOperator("op-2"))
        vm.onEvent(PcCarePlanEvent.NextStep)
        assertEquals(PcCarePlanStep.REVIEW, vm.state.value.step)
    }

    @Test
    fun `create with toggle on carries both new fields and the chosen removal people`() = runTest(dispatcher) {
        val repository = repositoryWithOnePen()
        val vm = viewModel(repository)
        vm.bindWizard("deworming", "Deworming")
        walkToOperators(vm)
        vm.onEvent(PcCarePlanEvent.ToggleFeedRemoval)
        vm.onEvent(PcCarePlanEvent.ToggleRemovalOperator("op-2"))

        vm.onEvent(PcCarePlanEvent.Create)
        advanceUntilIdle()

        val (_, request) = repository.createRequests.single()
        assertEquals(true, request.feedRemovalRequired)
        assertEquals(listOf("op-2"), request.removalOperatorUserIds)
        assertEquals("deworming", request.category)
        assertEquals("shed-1", request.shedId)
    }

    @Test
    fun `create with toggle off sends neither field`() = runTest(dispatcher) {
        val repository = repositoryWithOnePen()
        val vm = viewModel(repository)
        vm.bindWizard("deworming", "Deworming")
        walkToOperators(vm)

        vm.onEvent(PcCarePlanEvent.Create)
        advanceUntilIdle()

        val (_, request) = repository.createRequests.single()
        assertNull("injection deworming's payload must stay byte-compatible", request.feedRemovalRequired)
        assertNull(request.removalOperatorUserIds)
    }

    @Test
    fun `create with toggle on but nobody picked is refused before any network call`() = runTest(dispatcher) {
        val repository = repositoryWithOnePen()
        val vm = viewModel(repository)
        vm.bindWizard("deworming", "Deworming")
        walkToOperators(vm)
        vm.onEvent(PcCarePlanEvent.ToggleFeedRemoval)

        vm.onEvent(PcCarePlanEvent.Create)
        advanceUntilIdle()

        assertEquals("the refusal happens before any write", 0, repository.createRequests.size)
        assertTrue(vm.state.value.message.orEmpty().isNotBlank())
    }

    @Test
    fun `toggle on bumps a too-early selected day to the earliest allowed one and says so`() = runTest(dispatcher) {
        val vm = viewModel(repositoryWithOnePen())
        vm.bindWizard("deworming", "Deworming")
        // The wizard preselects TODAY, which never has a removal evening ahead of it.
        val before = vm.state.value.selectedDate
        vm.onEvent(PcCarePlanEvent.ToggleFeedRemoval)

        val after = vm.state.value.selectedDate
        assertTrue("today can never carry a removal evening", after > before)
        assertEquals(vm.state.value.minSelectableDateIso, after)
        assertTrue("a moved day is said out loud, never silent", vm.state.value.message.orEmpty().isNotBlank())

        // And a later attempt to reselect a too-early day is refused.
        vm.onEvent(PcCarePlanEvent.SelectDate(LocalDate.parse(before)))
        assertEquals(after, vm.state.value.selectedDate)
    }

    @Test
    fun `a create failure surfaces a message instead of silently keeping the spinner`() = runTest(dispatcher) {
        val repository = repositoryWithOnePen().apply { failNextCreateWith = RuntimeException("boom") }
        val vm = viewModel(repository)
        vm.bindWizard("deworming", "Deworming")
        walkToOperators(vm)

        vm.onEvent(PcCarePlanEvent.Create)
        advanceUntilIdle()

        assertEquals(false, vm.state.value.creating)
        assertTrue(vm.state.value.message.orEmpty().isNotBlank())
    }
}
