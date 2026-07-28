package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
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
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.CountsBreedsResponseDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsDestinationShedDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.GoatLocationPathDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.feature.counts.ShiftingEvent

@OptIn(ExperimentalCoroutinesApi::class)
class ShiftingViewModelEligibilityTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `dead RFID is rejected by lookup before it can be selected`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "dead")))
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00001"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()

        assertTrue(vm.state.value.animalMatches.isEmpty())
        assertNull(vm.state.value.selectedAnimal)
        assertFalse(vm.state.value.canSubmit)
        assertEquals("This animal is no longer active and cannot be shifted.", vm.state.value.animalLookupMessage)
    }

    @Test
    fun `selecting a live goat locks destination farm to its current farm`() = runTest(dispatcher) {
        val vm = newViewModel(listOf(animal(lifecycle = "alive")))
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))

        assertEquals(CBE_PARK_ID, vm.state.value.destinationParkId)
        assertEquals(listOf(CBE_SHED_ID), vm.state.value.shedsForSelectedPark.map { it.shedId })

        // Even an injected/stale UI event cannot switch this goat to another farm.
        vm.onEvent(ShiftingEvent.SelectDestinationPark(CPT_PARK_ID))
        assertEquals(CBE_PARK_ID, vm.state.value.destinationParkId)
    }

    @Test
    fun `successful shifting submission clears the draft and requests return to Actions`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync)
        advanceUntilIdle()

        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        assertFalse(vm.state.value.canSubmit)
        vm.onEvent(ShiftingEvent.SelectManagementStageMode("keep_current"))
        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()

        sync.succeed("shift-outbox")
        advanceUntilIdle()

        assertEquals("", vm.state.value.animalQuery)
        assertNull(vm.state.value.selectedAnimal)
        // A successful child form returns to Actions; it must not leave its success banner on the
        // now-empty form, which is the current broken behaviour.
        assertNull(vm.state.value.lastRecordedMessage)
        assertTrue(vm.state.value.returnToActions)
        vm.onEvent(ShiftingEvent.NavigationHandled)
        assertFalse(vm.state.value.returnToActions)
    }

    @Test
    fun `Mother is sent only as the selected management stage`() = runTest(dispatcher) {
        val sync = NoopShiftingSyncRepository()
        val vm = newViewModel(listOf(animal(lifecycle = "alive")), sync)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.EditAnimalQuery("CBE-ASSUMED-RFID-00002"))
        vm.onEvent(ShiftingEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(ShiftingEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(ShiftingEvent.SelectDestinationShed(CBE_SHED_ID))
        vm.onEvent(ShiftingEvent.SelectManagementStageMode("select_stage"))
        vm.onEvent(ShiftingEvent.SelectTargetManagementStage("Mother"))
        vm.onEvent(ShiftingEvent.Submit)
        advanceUntilIdle()
        assertEquals("select_stage", sync.lastShiftingRequest?.managementStageMode)
        assertEquals("Mother", sync.lastShiftingRequest?.targetManagementStage)
    }

    private fun newViewModel(
        matches: List<GoatSearchItemDto>,
        syncRepository: NoopShiftingSyncRepository = NoopShiftingSyncRepository(),
    ) = ShiftingViewModel(
        syncRepository = syncRepository,
        countsRepository = FakeShiftingCountsRepository(matches),
        analytics = NoopShiftingAnalytics(),
        crashReporter = NoopShiftingCrashReporter(),
        savedStateHandle = SavedStateHandle(),
    )

    private fun animal(lifecycle: String) = GoatSearchItemDto(
        goatId = GOAT_ID,
        displayId = "G-000325",
        animalIdentifier1 = "CBE-ASSUMED-RFID-00001",
        lifecycleStatus = lifecycle,
        locationPath = GoatLocationPathDto(
            display = "Coimbatore / Castro 1",
            parkId = CBE_PARK_ID,
            parkName = "Coimbatore",
            shedId = CBE_SHED_ID,
            shedName = "Castro 1",
        ),
    )

    private companion object {
        const val GOAT_ID = "d8337607-6e21-41c9-a703-a7b73ae4e545"
        const val CBE_PARK_ID = "00000000-0000-4000-8000-000000003001"
        const val CBE_SHED_ID = "43071c6e-3b00-47a9-860c-1bbacb570575"
        const val CPT_PARK_ID = "00000000-0000-4000-8000-000000003002"
    }
}

private class FakeShiftingCountsRepository(
    private val matches: List<GoatSearchItemDto>,
) : CountsRepository {
    override fun observeHerdSummary(lifecycleStatus: String?, parkId: String?, breed: String?, sex: String?): Flow<Resource<HerdRegisterSummaryResponseDto>> =
        flowOf(Resource(data = HerdRegisterSummaryResponseDto()))
    override suspend fun refreshHerdSummary(lifecycleStatus: String?, parkId: String?, breed: String?, sex: String?): Result<Unit> = Result.success(Unit)
    override fun observeBreakdownTotals(query: CountsBreakdownQuery): Flow<Resource<CountsBreakdownResponseDto>> =
        flowOf(Resource(data = CountsBreakdownResponseDto()))
    override fun observeBirthBreeds(): Flow<Resource<CountsBreedsResponseDto>> =
        flowOf(Resource(data = CountsBreedsResponseDto()))
    override suspend fun refreshBirthBreeds(): Result<Unit> = Result.success(Unit)
    override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> = flowOf(PagingData.empty())
    override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
        MutableStateFlow(
            Resource(
                data = CountsShiftingDestinationsResponseDto(
					managementStages = listOf("K0", "K1", "Mother"),
                    parks = listOf(
                        CountsDestinationParkDto(
                            parkId = "00000000-0000-4000-8000-000000003001",
                            name = "Coimbatore",
                            sheds = listOf(CountsDestinationShedDto("43071c6e-3b00-47a9-860c-1bbacb570575", "Castro 2")),
                        ),
                        CountsDestinationParkDto(
                            parkId = "00000000-0000-4000-8000-000000003002",
                            name = "Channapatna",
                            sheds = listOf(CountsDestinationShedDto("53071c6e-3b00-47a9-860c-1bbacb570575", "Castro 2")),
                        ),
                    ),
                ),
            ),
        )
    override suspend fun refreshShiftingDestinations(): Result<Unit> = Result.success(Unit)
    override suspend fun lookupAnimals(query: String, parkId: String?, shedId: String?): Result<List<GoatSearchItemDto>> = Result.success(matches)
}

private class NoopShiftingSyncRepository : SyncRepository {
    var lastShiftingRequest: CountsShiftingEventRequestDto? = null
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    fun succeed(itemId: String) {
        val now = System.currentTimeMillis()
        status.value = SyncStatus(
            online = true,
            pendingCount = 0,
            inFlightCount = 0,
            failedCount = 0,
            deadLetterCount = 0,
            lastSyncAt = now,
            items = listOf(
                SyncQueueItem(
                    id = itemId,
                    opType = "COUNTS_SHIFTING",
                    groupKey = CBE_SHED_ID_FOR_SYNC,
                    status = SyncItemStatus.SUCCEEDED,
                    attemptCount = 1,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = now,
                    updatedAt = now,
                    lastError = null,
                ),
            ),
        )
    }
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
    override suspend fun enqueueCountsShifting(groupKey: String, idempotencyKey: String, request: CountsShiftingEventRequestDto): AppResult<String> {
        lastShiftingRequest = request
        return AppResult.Ok("shift-outbox")
    }

    private companion object {
        const val CBE_SHED_ID_FOR_SYNC = "43071c6e-3b00-47a9-860c-1bbacb570575"
    }
}

private class NoopShiftingAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopShiftingCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
