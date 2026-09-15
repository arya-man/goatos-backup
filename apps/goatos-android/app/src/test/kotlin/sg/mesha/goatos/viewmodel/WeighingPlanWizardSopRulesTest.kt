package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.weighing.WeighingSopRules
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime

/**
 * WEIGHING SOP on the plan wizard (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md):
 * the published rules decide what the DATE step offers, whether the removal step is shown and
 * whether the save carries a removal operator.
 *
 *  - `optional`: today is offered; the planner's toggle decides; declining sends NO operator and
 *    `feedWaterRemovalRequested = false` (mutation check: drop the `removalApplies()` branch in
 *    `commit` and `under optional, declining the removal saves without an operator` goes red);
 *  - `off`: the removal step is absent, today is offered, no operator travels;
 *  - `required` (and no rules at all): the 2026-09-03 behaviour exactly.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WeighingPlanWizardSopRulesTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun rules(mode: String) = WeighingSopRules.Seeded.copy(version = 3, removalMode = mode)

    private fun buildViewModel(repository: WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository) =
        WeighingPlanWizardViewModel(
            repository = repository,
            repeatSeedStore = WeighingRepeatSeedStore(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            bootstrapRepository = FakeCutoffBootstrapRepository(),
            savedStateHandle = SavedStateHandle(),
        )

    private fun today(): String = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()

    private suspend fun kotlinx.coroutines.test.TestScope.walkToSaveable(
        vm: WeighingPlanWizardViewModel,
        repository: WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository,
        isoDate: String,
    ) {
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        vm.selectDate(isoDate)
        repository.catalogRefreshGate.complete(Unit)
        advanceUntilIdle()
        vm.selectPark("park-cbe")
        advanceUntilIdle()
        vm.toggleBucket("loc-yashoda-1")
        advanceUntilIdle()
        assertEquals(1, vm.state.value.addedCount)
    }

    @Test
    fun `under optional, today is offered and declining the removal saves without an operator`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository().apply { plannerSop = rules("optional") }
        val vm = buildViewModel(repository)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        assertEquals("optional", vm.state.value.removalMode)
        assertEquals("today is a real plan when the removal is not required", today(), vm.state.value.dateOptions.first().isoDate)

        walkToSaveable(vm, repository, today())
        // Today's evening is already gone, so the removal cannot apply to it: the toggle reads off.
        assertFalse(vm.state.value.removalPossible)
        assertFalse(vm.state.value.removalApplies)

        vm.commit(publish = true)
        advanceUntilIdle()
        val draft = repository.lastCreateDraft
        assertEquals("", draft?.fastingOperatorUserId)
        assertEquals(false, draft?.feedWaterRemovalRequested)
        assertEquals("campaign-new", vm.state.value.savedCampaignId)
    }

    @Test
    fun `under optional, keeping the removal on a later day still demands the operator`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository().apply { plannerSop = rules("optional") }
        val vm = buildViewModel(repository)
        val later = earliestPlannableDateWithFeedRemoval(ZonedDateTime.now(ZoneId.of("Asia/Kolkata")), java.time.LocalTime.of(20, 0)).toString()
        walkToSaveable(vm, repository, later)
        assertTrue(vm.state.value.removalPossible)
        assertTrue("the toggle defaults ON", vm.state.value.removalApplies)

        vm.commit(publish = true)
        advanceUntilIdle()
        assertNull("the removal is on, so the operator is still mandatory", repository.lastCreateDraft)

        vm.selectFastingOperator("user-dinakar")
        vm.commit(publish = true)
        advanceUntilIdle()
        assertEquals("user-dinakar", repository.lastCreateDraft?.fastingOperatorUserId)
        assertEquals(true, repository.lastCreateDraft?.feedWaterRemovalRequested)

        // The planner switches it off: the operator is dropped and the choice travels.
        val repository2 = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository().apply { plannerSop = rules("optional") }
        val vm2 = buildViewModel(repository2)
        walkToSaveable(vm2, repository2, later)
        vm2.selectFastingOperator("user-dinakar")
        vm2.setFeedWaterRemovalRequested(false)
        vm2.commit(publish = true)
        advanceUntilIdle()
        assertEquals("", repository2.lastCreateDraft?.fastingOperatorUserId)
        assertEquals(false, repository2.lastCreateDraft?.feedWaterRemovalRequested)
    }

    @Test
    fun `under off, the removal step is absent and the save carries no operator`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository().apply { plannerSop = rules("off") }
        val vm = buildViewModel(repository)
        walkToSaveable(vm, repository, today())
        assertEquals("off", vm.state.value.removalMode)
        assertFalse(vm.state.value.removalApplies)
        // The toggle is not a word under OFF.
        vm.setFeedWaterRemovalRequested(true)
        vm.commit(publish = true)
        advanceUntilIdle()
        assertEquals("", repository.lastCreateDraft?.fastingOperatorUserId)
        assertNull("no per-task choice is sent when the SOP decides", repository.lastCreateDraft?.feedWaterRemovalRequested)
    }

    @Test
    fun `under required, or with no rules at all, today is never offered and the operator is mandatory`() = runTest(dispatcher) {
        for (published in listOf<WeighingSopRules?>(rules("required"), null)) {
            val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository().apply { plannerSop = published }
            val vm = buildViewModel(repository)
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()
            assertTrue("today is never offered", vm.state.value.dateOptions.none { it.isoDate == today() })
            assertEquals("required", vm.state.value.removalMode)
            assertTrue(vm.state.value.removalApplies)
        }
    }

    @Test
    fun `a way of weighing the SOP does not offer cannot be picked`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository().apply {
            plannerSop = rules("required").copy(modes = listOf("individual_animal"))
        }
        val vm = buildViewModel(repository)
        val later = earliestPlannableDateWithFeedRemoval(ZonedDateTime.now(ZoneId.of("Asia/Kolkata")), java.time.LocalTime.of(20, 0)).toString()
        walkToSaveable(vm, repository, later)
        assertEquals(listOf("individual_animal"), vm.state.value.allowedCategories)
        // The default pick follows the offered modes, and the unoffered one is refused.
        assertEquals("individual_animal", vm.state.value.configRows.single().category)
        vm.setBucketCategory(vm.state.value.configRows.single().locationId, "per_shed_partition")
        advanceUntilIdle()
        assertEquals("individual_animal", vm.state.value.configRows.single().category)
    }
}
