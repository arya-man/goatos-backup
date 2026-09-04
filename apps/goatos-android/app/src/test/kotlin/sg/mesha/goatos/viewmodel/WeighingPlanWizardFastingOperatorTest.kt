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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import java.time.ZoneId
import java.time.ZonedDateTime

/**
 * The weighing wizard's feed & water removal half (maintainer decision 2026-09-03):
 *
 *  - the task cannot be SAVED without the removal operator (mutation check: delete the
 *    fasting-operator gate in [WeighingPlanWizardViewModel.commit] and
 *    `commit without a removal operator is refused with a sentence` goes red);
 *  - the chosen person rides the create draft as `fastingOperatorUserId`;
 *  - the DATE step never offers today (mutation check: base the option list on `today` again in
 *    `toUiState` and `the date step never offers today` goes red).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WeighingPlanWizardFastingOperatorTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun buildViewModel(
        repository: WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository,
    ) = WeighingPlanWizardViewModel(
        repository = repository,
        repeatSeedStore = WeighingRepeatSeedStore(),
        analytics = NoopAnalytics(),
        crashReporter = NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(),
    )

    /** Drives a fresh create wizard to a saveable state minus the removal operator. */
    private suspend fun kotlinx.coroutines.test.TestScope.walkToSaveable(
        vm: WeighingPlanWizardViewModel,
        repository: WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository,
    ) {
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        val earliest = earliestPlannableDateWithFeedRemoval(
            ZonedDateTime.now(ZoneId.of("Asia/Kolkata")),
        ).toString()
        vm.selectDate(earliest)
        repository.catalogRefreshGate.complete(Unit)
        advanceUntilIdle()
        vm.selectPark("park-cbe")
        advanceUntilIdle()
        vm.toggleBucket("loc-yashoda-1")
        advanceUntilIdle()
        assertEquals(1, vm.state.value.addedCount)
    }

    @Test
    fun `commit without a removal operator is refused with a sentence`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val vm = buildViewModel(repository)
        walkToSaveable(vm, repository)

        vm.commit(publish = true)
        advanceUntilIdle()

        assertNull("no write may leave the phone without the removal operator", repository.lastCreateDraft)
        assertNull(vm.state.value.savedCampaignId)
        assertTrue(
            "the refusal names the missing answer, never a dead button",
            vm.state.value.message.orEmpty().contains("feed & water", ignoreCase = true),
        )
    }

    @Test
    fun `the chosen removal operator rides the create draft`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val vm = buildViewModel(repository)
        walkToSaveable(vm, repository)

        vm.selectFastingOperator("user-dinakar")
        vm.commit(publish = true)
        advanceUntilIdle()

        assertEquals("user-dinakar", repository.lastCreateDraft?.fastingOperatorUserId)
        assertEquals("campaign-new", vm.state.value.savedCampaignId)
    }

    @Test
    fun `an operator outside the chosen park is refused`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val vm = buildViewModel(repository)
        walkToSaveable(vm, repository)

        vm.selectFastingOperator("user-from-nowhere")
        vm.commit(publish = true)
        advanceUntilIdle()

        assertNull("a person the park does not offer can never be assigned", repository.lastCreateDraft)
    }

    /**
     * An EDIT opens with the removal operator the task already carries (PR 176 review finding):
     * the backend echoes `fasting_operator_user_id`, the seed carries it, and the wizard prefills
     * it so an otherwise-valid edit is not blocked on a re-pick. Mutation check: drop the
     * `fastingOperatorUserId` seed line from the wizard's initial state and this goes red.
     */
    @Test
    fun `an edit opens with the task's existing removal operator prefilled`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val seedStore = WeighingRepeatSeedStore()
        seedStore.stage(
            sourceCampaignId = "campaign-cbe",
            seed = sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeed(
                parkId = "park-cbe",
                parkName = "CBE",
                sourceDateLabel = "Wed 5 Aug",
                buckets = listOf(
                    sg.mesha.goatos.feature.weighing.plan.WeighingRepeatBucket(
                        "loc-yashoda-1", "individual_animal", "user-pramod",
                    ),
                ),
                editCampaignId = "campaign-cbe",
                editWeighDate = "2026-08-05",
                fastingOperatorUserId = "user-dinakar",
            ),
        )
        val vm = WeighingPlanWizardViewModel(
            repository = repository,
            repeatSeedStore = seedStore,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(
                mapOf(sg.mesha.goatos.ui.Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
            ),
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        repository.catalogRefreshGate.complete(Unit)
        advanceUntilIdle()

        assertEquals("user-dinakar", vm.state.value.fastingOperatorUserId)
        assertEquals("Dinakar", vm.state.value.fastingOperatorLabel)
    }

    /** A REPEAT is a new task: nothing about who removes feed & water carries over. */
    @Test
    fun `a repeat does not carry the source task's removal operator`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val seedStore = WeighingRepeatSeedStore()
        seedStore.stage(
            sourceCampaignId = "campaign-cbe",
            seed = sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeed(
                parkId = "park-cbe",
                parkName = "CBE",
                sourceDateLabel = "Wed 5 Aug",
                buckets = listOf(
                    sg.mesha.goatos.feature.weighing.plan.WeighingRepeatBucket(
                        "loc-yashoda-1", "individual_animal", "user-pramod",
                    ),
                ),
                fastingOperatorUserId = "user-dinakar",
            ),
        )
        val vm = WeighingPlanWizardViewModel(
            repository = repository,
            repeatSeedStore = seedStore,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(
                mapOf(sg.mesha.goatos.ui.Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
            ),
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        repository.catalogRefreshGate.complete(Unit)
        advanceUntilIdle()

        assertNull(vm.state.value.fastingOperatorUserId)
    }

    @Test
    fun `the date step never offers today`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val vm = buildViewModel(repository)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val todayIst = java.time.LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
        val earliest = earliestPlannableDateWithFeedRemoval(
            ZonedDateTime.now(ZoneId.of("Asia/Kolkata")),
        ).toString()
        val offered = vm.state.value.dateOptions.map { it.isoDate }
        assertTrue("the wizard must offer days", offered.isNotEmpty())
        assertTrue(
            "today's removal evening was yesterday, so today can never be weighed: offered=$offered",
            todayIst !in offered,
        )
        assertEquals("the first offered day is the 20:00 IST rule's earliest", earliest, offered.first())
    }

    @Test
    fun `a date before the earliest allowed one is refused`() = runTest(dispatcher) {
        val repository = WeighingPlanWizardEditHydrationTest.RaceReproducingWeighingRepository()
        val vm = buildViewModel(repository)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val todayIst = java.time.LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
        vm.selectDate(todayIst)
        advanceUntilIdle()

        assertNull("today has no removal evening ahead of it", vm.state.value.selectedDate)
    }
}
