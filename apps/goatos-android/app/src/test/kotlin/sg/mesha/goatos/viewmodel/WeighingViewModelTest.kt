package sg.mesha.goatos.viewmodel

import android.view.KeyEvent
import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
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
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
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
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
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

    @Test
    fun `weight validation accepts positive decimals and rejects invalid values`() {
        assertEquals("12.75", sanitizeWeighingWeightInput("12.75"))
        assertEquals(12.75, parsePositiveWeighingWeight("12.75"))
        assertEquals("", sanitizeWeighingWeightInput("-12"))
        assertEquals("", sanitizeWeighingWeightInput("12.7.5"))
        assertNull(parsePositiveWeighingWeight(""))
        assertNull(parsePositiveWeighingWeight("0"))
        assertNull(parsePositiveWeighingWeight("-1"))
        assertNull(parsePositiveWeighingWeight("NaN"))
        assertNull(parsePositiveWeighingWeight("Infinity"))
    }

    @Test
    fun `animal count validation accepts positive integers only`() {
        assertEquals("125", sanitizeWeighingAnimalCountInput("125"))
        assertEquals(125, parsePositiveWeighingAnimalCount("125"))
        assertEquals("", sanitizeWeighingAnimalCountInput("1.5"))
        assertEquals("", sanitizeWeighingAnimalCountInput("-2"))
        assertEquals("", sanitizeWeighingAnimalCountInput("two"))
        assertNull(parsePositiveWeighingAnimalCount(""))
        assertNull(parsePositiveWeighingAnimalCount("0"))
        assertNull(parsePositiveWeighingAnimalCount("-1"))
        assertNull(parsePositiveWeighingAnimalCount("1.5"))
    }

    @Test
    fun `restored accepted weight shows updating only while edit request is active`() = runTest(dispatcher) {
        val original = acceptedDraft(weightKg = 12.0)
        val gate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(listOf(rosterRow()), listOf(original), emptyList(), 0),
            recordIndividualGate = gate,
        )
        val vm = weighingViewModel(repository, scoped = true)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.recordIndividual(TEST_TAG, "13.5")
        runCurrent()

        assertTrue(vm.state.value.visibleRows.single().weightUpdating)
        assertEquals(13.5, repository.lastCapture?.weightKg)

        gate.complete(AppResult.Ok(original.copy(weightKg = 13.5, syncedToBackend = false)))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.single().weightUpdating)
    }

    @Test
    fun `restored accepted weight clears updating when edit request fails`() = runTest(dispatcher) {
        val original = acceptedDraft(weightKg = 12.0)
        val gate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(listOf(rosterRow()), listOf(original), emptyList(), 0),
            recordIndividualGate = gate,
        )
        val vm = weighingViewModel(repository, scoped = true)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.recordIndividual(TEST_TAG, "13.5")
        runCurrent()
        assertTrue(vm.state.value.visibleRows.single().weightUpdating)

        gate.complete(AppResult.Err("update failed"))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.single().weightUpdating)
        assertEquals("update failed", vm.state.value.message)
    }

    @Test
    fun `free flow saving one animal does not block saving the next animal`() = runTest(dispatcher) {
        val firstGate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val secondGate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(rosterRow(), rosterRow(animalId = SECOND_TAG, rowId = "row-2")),
                emptyList(),
                emptyList(),
                0,
            ),
            recordIndividualGates = ArrayDeque(listOf(firstGate, secondGate)),
        )
        val scans = FakeScanCaptureRepository()
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, TEST_TAG)
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, SECOND_TAG)
        val vm = weighingViewModel(repository, scoped = true, scanCaptureRepository = scans)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.onAnimalWeightInputChange(TEST_TAG, "12.0")
        vm.onAnimalWeightInputChange(SECOND_TAG, "13.5")
        vm.recordIndividual(TEST_TAG, "12.0")
        runCurrent()

        val firstPendingState = vm.state.value.visibleRows.associateBy { it.animalId }
        assertTrue(firstPendingState.getValue(TEST_TAG).weightUpdating)
        assertTrue(firstPendingState.getValue(SECOND_TAG).canSaveWeight)

        vm.recordIndividual(SECOND_TAG, "13.5")
        runCurrent()

        assertEquals(listOf(TEST_TAG, SECOND_TAG), repository.captures.map { it.animalId })
        val bothPendingState = vm.state.value.visibleRows.associateBy { it.animalId }
        assertTrue(bothPendingState.getValue(TEST_TAG).weightUpdating)
        assertTrue(bothPendingState.getValue(SECOND_TAG).weightUpdating)

        firstGate.complete(AppResult.Ok(acceptedDraft(weightKg = 12.0)))
        secondGate.complete(AppResult.Ok(acceptedDraft(animalId = SECOND_TAG, weightKg = 13.5)))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.any { it.weightUpdating })
    }

    private fun weighingViewModel(
        repository: FakeWeighingRepository,
        scoped: Boolean = false,
        scanCaptureRepository: FakeScanCaptureRepository = FakeScanCaptureRepository(),
    ): WeighingViewModel =
        WeighingViewModel(
            repository = repository,
            bootstrapRepository = LeadershipBootstrapRepository,
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptureRepository,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(
                if (scoped) {
                    mapOf(
                        "campaignId" to "campaign-1",
                        "workGroupId" to "group-1",
                        "campaignShedId" to "campaign-shed-1",
                        "weighingCategory" to "individual_animal",
                        "tenantId" to "tenant-1",
                        "expectedLocationId" to "shed-1",
                        "expectedLocationLabel" to "Shed 1",
                    )
                } else {
                    emptyMap()
                },
            ),
        )

    private fun rosterRow(
        animalId: String = TEST_TAG,
        rowId: String = "row-1",
    ) = WeighingRosterRowEntity(
        id = rowId,
        scopeKey = "campaign-1:group-1:campaign-shed-1",
        tenantId = "tenant-1",
        campaignId = "campaign-1",
        workGroupId = "group-1",
        campaignShedId = "campaign-shed-1",
        expectedLocationId = "shed-1",
        expectedLocationLabel = "Shed 1",
        actualLocationId = null,
        actualLocationLabel = null,
        animalId = animalId,
        displayAnimalId = animalId,
        primaryTag = animalId,
        secondaryTag = null,
        normalizedPrimaryTag = animalId,
        normalizedSecondaryTag = null,
        status = "pending",
        availabilityStatus = null,
        seq = 1,
        updatedAt = 1_000,
    )

    private fun acceptedDraft(
        animalId: String = TEST_TAG,
        weightKg: Double,
    ) = IndividualWeighingDraft(
        observationId = "observation-$animalId",
        animalId = animalId,
        scannedIdentifier = animalId,
        weightKg = weightKg,
        capturedAtMs = 1_000,
        proofCaptureId = "proof-$animalId",
        proofReady = true,
        readyToSubmit = true,
        syncedToBackend = true,
        idempotencyKey = "server:observation-1",
        serverProofId = "proof-$animalId",
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
        scopeState: WeighingScopeState = WeighingScopeState(emptyList(), emptyList(), emptyList(), 0),
        private val recordIndividualGate: CompletableDeferred<AppResult<IndividualWeighingDraft>>? = null,
        private val recordIndividualGates: ArrayDeque<CompletableDeferred<AppResult<IndividualWeighingDraft>>> = ArrayDeque(),
    ) : WeighingRepository {
        private val observedScope = MutableStateFlow(scopeState)
        var lastCapture: IndividualWeighingCapture? = null
            private set
        val captures = mutableListOf<IndividualWeighingCapture>()
        var createdDraft: WeighingPlanDraft? = null
            private set
        var updatedDraft: WeighingPlanDraft? = null
            private set

        override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
            observedScope

        override suspend fun listAssignments(): AppResult<List<WeighingAssignment>> = AppResult.Ok(emptyList())

        override suspend fun listLeadershipVideos(): AppResult<List<WeighingLeadershipShed>> =
            AppResult.Ok(emptyList())

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

        override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> {
            lastCapture = capture
            captures += capture
            return recordIndividualGates.removeFirstOrNull()?.await()
                ?: recordIndividualGate?.await()
                ?: AppResult.Err("not used")
        }

        override suspend fun attachIndividualProof(scopeKey: String, animalId: String, proofCaptureId: String, serverProofId: String?) {}

        override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
            AppResult.Err("not used")

        override suspend fun attachShedPartitionProof(scopeKey: String, proofCaptureId: String, serverProofId: String?) {}

        override suspend fun submitIndividualScope(
            campaignId: String,
            campaignShedId: String,
            scannedIdentifiers: List<String>,
        ): AppResult<Unit> =
            AppResult.Ok(Unit)

        override suspend fun discardEditableIndividual(scopeKey: String, animalId: String) {}
    }

    private companion object {
        const val TEST_TAG = "901007000504407"
        const val SECOND_TAG = "901007000504408"
        const val SCOPE_KEY = "campaign-1:group-1:campaign-shed-1"
        const val WEIGHING_SCAN_FIELD_KEY = "weighing_free_flow_scan"
    }
}
