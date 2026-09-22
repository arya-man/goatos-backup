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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.network.dto.PcCarePlannerCatalogDto
import sg.mesha.goatos.core.network.dto.PcCareSopDto
import sg.mesha.goatos.core.network.dto.PcCareSopRemovalDto
import sg.mesha.goatos.feature.pccare.PcCarePlanEvent

/**
 * PC CARE SOP (maintainer decision 2026-09-22): the plan wizard's feed & water removal step is
 * the PUBLISHED document's call -- whether it is offered at all, on WHICH work, and whether the
 * planner is asked or simply told. The phone holds no rule of its own; before this the step was
 * `categoryKey == "deworming"` in the ViewModel.
 *
 * Each case is the wizard's OWN answer, read off the state the screen renders.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class PcCarePlanSopRulesTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before fun setUp() = Dispatchers.setMain(dispatcher)
    @After fun tearDown() = Dispatchers.resetMain()

    private fun rules(mode: String, vararg appliesTo: String) = PcCareSopDto(
        version = 7,
        feedWaterRemoval = PcCareSopRemovalDto(mode = mode, appliesTo = appliesTo.toList()),
    )

    private fun viewModel(sop: PcCareSopDto?) = PcCarePlanViewModel(
        repository = FakePcCareRepository().apply { plannerCatalog = PcCarePlannerCatalogDto(sop = sop) },
        submittedGrains = SubmittedGrainsSource { flowOf(emptySet()) },
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
        bootstrapRepository = FakeCutoffBootstrapRepository(),
    )

    @Test
    fun `under optional the planner is asked, on the work the document lists`() = runTest(dispatcher) {
        val vm = viewModel(rules("optional", "deworming"))
        advanceUntilIdle()
        vm.bindWizard("deworming", "Deworming")
        advanceUntilIdle()
        assertTrue("the step is offered on listed work", vm.state.value.feedRemovalOffered)
        assertTrue("the planner is asked", vm.state.value.feedRemovalIsAChoice)
        assertFalse("and is not pre-answered", vm.state.value.feedRemovalRequired)
    }

    @Test
    fun `under optional the step is absent on work the document does not list`() = runTest(dispatcher) {
        val vm = viewModel(rules("optional", "deworming"))
        advanceUntilIdle()
        vm.bindWizard("ticks_removal", "Ticks Removal")
        advanceUntilIdle()
        assertFalse(vm.state.value.feedRemovalOffered)
        assertFalse(vm.state.value.feedRemovalRequired)
    }

    @Test
    fun `under required the removal applies and the toggle is not a choice`() = runTest(dispatcher) {
        val vm = viewModel(rules("required", "deworming", "anti_protozoan"))
        advanceUntilIdle()
        vm.bindWizard("anti_protozoan", "Anti Protozoan")
        advanceUntilIdle()
        assertTrue(vm.state.value.feedRemovalOffered)
        assertFalse("the planner is told, not asked", vm.state.value.feedRemovalIsAChoice)
        assertTrue("and it applies", vm.state.value.feedRemovalRequired)
        // The toggle cannot switch off what the farm's rule requires.
        vm.onEvent(PcCarePlanEvent.ToggleFeedRemoval)
        advanceUntilIdle()
        assertTrue(vm.state.value.feedRemovalRequired)
    }

    @Test
    fun `under off the step is never offered, whatever the work`() = runTest(dispatcher) {
        val vm = viewModel(rules("off", "deworming"))
        advanceUntilIdle()
        vm.bindWizard("deworming", "Deworming")
        advanceUntilIdle()
        assertFalse(vm.state.value.feedRemovalOffered)
        assertFalse(vm.state.value.feedRemovalRequired)
    }

    @Test
    fun `an older server serving no rules keeps the pre-SOP behaviour`() = runTest(dispatcher) {
        // The seeded document: optional, on deworming alone -- what the module always did.
        val vm = viewModel(null)
        advanceUntilIdle()
        vm.bindWizard("deworming", "Deworming")
        advanceUntilIdle()
        assertTrue(vm.state.value.feedRemovalOffered)
        assertTrue(vm.state.value.feedRemovalIsAChoice)
        vm.bindWizard("hoof_trimming", "Hoof Trimming")
        advanceUntilIdle()
        assertFalse("trimming never carried the removal", vm.state.value.feedRemovalOffered)
    }

    @Test
    fun `the wizard offers the removal on any work the document lists, not deworming alone`() = runTest(dispatcher) {
        // The retired rule was `category == deworming` in the ViewModel: a farm that lists ticks
        // removal could not have been offered it at all.
        val vm = viewModel(rules("optional", "deworming", "ticks_removal"))
        advanceUntilIdle()
        vm.bindWizard("ticks_removal", "Ticks Removal")
        advanceUntilIdle()
        assertTrue(vm.state.value.feedRemovalOffered)
    }
}
