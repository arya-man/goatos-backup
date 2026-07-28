package sg.mesha.goatos.viewmodel

import android.view.KeyEvent
import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.IndividualWeighingDraft
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedWeighingDraft
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScanMatch
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

@OptIn(ExperimentalCoroutinesApi::class)
class WeighingViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `planner create uses selected shed categories instead of first three defaults`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository()
        val vm = weighingViewModel(repository = repository)
        backgroundScope.launch(dispatcher) {
            vm.state.collect {}
        }

        advanceUntilIdle()
        val initial = vm.state.value.plannerParks.single().sheds
        assertEquals(
            listOf("shed-1", "shed-2", "shed-3"),
            initial.filter { it.selected }.map { it.locationId },
        )

        vm.togglePlannerShed("shed-1")
        vm.togglePlannerShed("shed-4")
        vm.setPlannerShedCategory("shed-2", "individual_animal")
        vm.setPlannerShedCategory("shed-3", "per_shed_partition")
        vm.setPlannerShedCategory("shed-4", "individual_animal")
        vm.createOrEditDefaultPlan()
        advanceUntilIdle()

        val draft = repository.createdDraft
        requireNotNull(draft)
        assertEquals("park-cpt", draft.parkId)
        assertEquals("operator-amit", draft.operatorUserId)
        assertEquals(
            listOf("shed-2", "shed-3", "shed-4"),
            draft.sheds.map { it.locationId },
        )
        assertEquals(
            listOf("individual_animal", "per_shed_partition", "individual_animal"),
            draft.sheds.map { it.category },
        )
        assertNull(repository.updatedDraft)
    }

    @Test
    fun `read failure hides raw localhost transport detail from weighing screen`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(
            plannerCatalogResult = AppResult.Err("Failed to connect to localhost/127.0.0.1:8080"),
        )
        val vm = weighingViewModel(repository = repository)
        backgroundScope.launch(dispatcher) {
            vm.state.collect {}
        }

        advanceUntilIdle()

        assertEquals(
            "Couldn't load weighing. Check the laptop backend or network, then refresh.",
            vm.state.value.message,
        )
    }

    private fun weighingViewModel(repository: FakeWeighingRepository): WeighingViewModel =
        WeighingViewModel(
            repository = repository,
            bootstrapRepository = LeadershipBootstrapRepository,
            reader = FakeRfidReaderPort(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(),
        )

    private object LeadershipBootstrapRepository : BootstrapRepository {
        override suspend fun loadNavState(): NavState = NavState.Empty
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
    }

    private class FakeRfidReaderPort : RfidReaderPort {
        private val readsFlow = MutableSharedFlow<RfidRead>()
        override val status: StateFlow<RfidReaderStatus> = MutableStateFlow(RfidReaderStatus.READY)
        override val reads: SharedFlow<RfidRead> = readsFlow
        override val readerName: StateFlow<String?> = MutableStateFlow("Test reader")
        override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())
        override fun refreshStatus() {}
        override fun openSystemPairing() {}
        override fun setCaptureEnabled(enabled: Boolean) {}
        override fun setCompletionKeySwallowEnabled(enabled: Boolean) {}
        override fun onKeyEvent(event: KeyEvent): Boolean = false
    }

    private class FakeWeighingRepository(
        private val plannerCatalogResult: AppResult<WeighingPlannerCatalog>? = null,
    ) : WeighingRepository {
        var createdDraft: WeighingPlanDraft? = null
            private set
        var updatedDraft: WeighingPlanDraft? = null
            private set

        override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
            flowOf(WeighingScopeState(emptyList(), emptyList(), emptyList(), 0))

        override suspend fun listAssignments(): AppResult<List<WeighingAssignment>> = AppResult.Ok(emptyList())

        override suspend fun plannerCatalog(periodStartDate: String): AppResult<WeighingPlannerCatalog> =
            plannerCatalogResult ?: AppResult.Ok(
                WeighingPlannerCatalog(
                    parks = listOf(
                        WeighingPlannerPark(
                            parkId = "park-cpt",
                            name = "CPT - Channapatna",
                            kidCount = 278,
                            sheds = listOf(
                                WeighingPlannerShed("shed-1", "Castro 1", 80),
                                WeighingPlannerShed("shed-2", "Castro 2", 64),
                                WeighingPlannerShed("shed-3", "Godel 2 - Part 1", 56),
                                WeighingPlannerShed("shed-4", "Gandhi 1", 78),
                            ),
                            existingCampaign = null,
                        ),
                    ),
                    operators = listOf(WeighingPlannerOperator("operator-amit", "Amit Kumar", "AMIT")),
                ),
            )

        override suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?> {
            createdDraft = draft
            return AppResult.Ok(null)
        }

        override suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?> {
            updatedDraft = draft
            return AppResult.Ok(null)
        }

        override suspend fun refreshScope(campaignId: String, workGroupId: String, campaignShedId: String, maxRows: Int): AppResult<Int> =
            AppResult.Ok(0)

        override suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>) {}

        override suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch =
            WeighingScanMatch(null, "unknown", null, null)

        override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> =
            AppResult.Err("not used")

        override suspend fun attachIndividualProof(scopeKey: String, animalId: String, proofCaptureId: String, serverProofId: String?) {}

        override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
            AppResult.Err("not used")

        override suspend fun attachShedPartitionProof(scopeKey: String, proofCaptureId: String, serverProofId: String?) {}

        override suspend fun discardEditableIndividual(scopeKey: String, animalId: String) {}
    }
}
