package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.test.StandardTestDispatcher
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
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDestinationParkDto
import sg.mesha.goatos.core.network.dto.CountsDestinationShedDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.GoatLocationPathDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.feature.counts.BirthDeathEvent
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathMode

/**
 * BirthDeathViewModel is now SELECTOR/SCAN-driven, so these lock in the two things the PR#11
 * follow-up existed to fix:
 *
 *  - **Birth placement is chosen, not typed.** Park and shed come from the shifting-destinations
 *    catalog (names shown, ids submitted) and BOTH are REQUIRED — an operator can never record a
 *    newborn into no shed or into a hand-typed UUID.
 *  - **The death target is a resolved animal, not a typed UUID + record version.** The operator
 *    searches a tag, selects one animal, and the death write carries that animal's OWN `row_version`
 *    from the search result — never a value the operator typed.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class BirthDeathViewModelValidationTest {
    private val dispatcher = StandardTestDispatcher()

    private lateinit var syncRepository: RecordingCountsSyncRepository
    private lateinit var countsRepository: FakeBirthDeathCountsRepository
    private lateinit var analytics: AnalyticsPort
    private lateinit var crashReporter: CrashReporter
    private lateinit var savedStateHandle: SavedStateHandle

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        syncRepository = RecordingCountsSyncRepository()
        countsRepository = FakeBirthDeathCountsRepository()
        analytics = NoopAnalyticsPort()
        crashReporter = NoopTestCrashReporter()
        savedStateHandle = SavedStateHandle()
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newViewModel() = BirthDeathViewModel(
        syncRepository,
        countsRepository,
        analytics,
        crashReporter,
        savedStateHandle,
    )

    // --- Birth: placement is chosen and required ---------------------------------------------

    @Test
    fun `birth submit disabled until park and shed are selected`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle() // let the destinations catalog emit

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-01-01"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, "2026-01-15"))

        assertFalse(
            "Placement is required — submit stays disabled with no park/shed chosen",
            vm.state.value.canSubmit,
        )

        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        assertFalse("Shed still unchosen", vm.state.value.canSubmit)

        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertTrue("Both placement ids chosen — submit enabled", vm.state.value.canSubmit)
    }

    @Test
    fun `birth submits the selected placement ids, never typed text`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-01-01"))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, "2026-01-15"))
        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastBirth
        assertTrue("A birth was enqueued", request != null)
        assertEquals(PARK_ID, request!!.parkId)
        assertEquals(SHED_ID, request.shedId)
    }

    @Test
    fun `selecting a different park resets the shed`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID))
        vm.onEvent(BirthDeathEvent.SelectShed(SHED_ID))
        assertEquals(SHED_ID, vm.state.value.shedId)

        vm.onEvent(BirthDeathEvent.SelectPark(PARK_ID_2))
        assertEquals("Changing park clears the shed", "", vm.state.value.shedId)
    }

    // --- Death: target is a resolved animal carrying its own row_version ----------------------

    @Test
    fun `death submit disabled until an animal is selected`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found dead in the shed"))

        assertFalse(
            "No animal selected yet — submit stays disabled",
            vm.state.value.canSubmit,
        )
    }

    @Test
    fun `death submit enabled after selecting a searched animal`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(BirthDeathEvent.LookupAnimals)
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found dead in the shed"))

        assertTrue("Animal + reason present — submit enabled", vm.state.value.canSubmit)
    }

    @Test
    fun `death write carries the selected animal's row_version, not a typed value`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        vm.onEvent(BirthDeathEvent.EditAnimalQuery("TAG-77"))
        vm.onEvent(BirthDeathEvent.LookupAnimals)
        advanceUntilIdle()
        vm.onEvent(BirthDeathEvent.SelectAnimal(GOAT_ID))
        vm.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Found dead in the shed"))

        vm.onEvent(BirthDeathEvent.Submit)
        advanceUntilIdle()

        val request = syncRepository.lastDeath
        assertTrue("A death was enqueued", request != null)
        assertEquals("Death targets the resolved goat_id", GOAT_ID, request!!.goatId)
        assertEquals("row_version comes from the search result", ANIMAL_ROW_VERSION, request.rowVersion)
    }

    private companion object {
        const val PARK_ID = "11111111-1111-1111-1111-111111111111"
        const val PARK_ID_2 = "22222222-2222-2222-2222-222222222222"
        const val SHED_ID = "33333333-3333-3333-3333-333333333333"
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
        const val ANIMAL_ROW_VERSION = 7
    }
}

// --- Fakes -----------------------------------------------------------------------------------

/** Captures the enqueued birth/death requests so a test can assert the exact wire payload. */
private class RecordingCountsSyncRepository : SyncRepository {
    var lastBirth: CountsBirthEventRequestDto? = null
    var lastDeath: CountsDeathEventRequestDto? = null

    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
    override suspend fun enqueueCountsBirth(groupKey: String, idempotencyKey: String, request: CountsBirthEventRequestDto): AppResult<String> {
        lastBirth = request
        return AppResult.Ok("outbox-birth-1")
    }
    override suspend fun enqueueCountsDeath(groupKey: String, idempotencyKey: String, request: CountsDeathEventRequestDto): AppResult<String> {
        lastDeath = request
        return AppResult.Ok("outbox-death-1")
    }
}

/**
 * Mirrors Room: the destinations catalog emits one park with one shed, and a tag lookup resolves to
 * one animal carrying its own row_version — the exact contract the death write round-trips.
 */
private class FakeBirthDeathCountsRepository : CountsRepository {
    override fun observeHerdSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): Flow<Resource<HerdRegisterSummaryResponseDto>> =
        MutableStateFlow(Resource(data = HerdRegisterSummaryResponseDto()))

    override suspend fun refreshHerdSummary(
        lifecycleStatus: String?,
        parkId: String?,
        breed: String?,
        sex: String?,
    ): Result<Unit> = Result.success(Unit)

    override fun observeBreakdownTotals(
        query: CountsBreakdownQuery,
    ): Flow<Resource<CountsBreakdownResponseDto>> =
        MutableStateFlow(Resource(data = CountsBreakdownResponseDto()))

    override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> =
        flowOf(PagingData.empty<CountsBreakdownRowDto>()).map { it }

    override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
        MutableStateFlow(
            Resource(
                data = CountsShiftingDestinationsResponseDto(
                    parks = listOf(
                        CountsDestinationParkDto(
                            parkId = "11111111-1111-1111-1111-111111111111",
                            name = "North Park",
                            sheds = listOf(
                                CountsDestinationShedDto(
                                    shedId = "33333333-3333-3333-3333-333333333333",
                                    name = "Shed A",
                                ),
                            ),
                        ),
                        CountsDestinationParkDto(
                            parkId = "22222222-2222-2222-2222-222222222222",
                            name = "South Park",
                            sheds = listOf(
                                CountsDestinationShedDto(
                                    shedId = "55555555-5555-5555-5555-555555555555",
                                    name = "Shed B",
                                ),
                            ),
                        ),
                    ),
                ),
            ),
        )

    override suspend fun refreshShiftingDestinations(): Result<Unit> = Result.success(Unit)

    override suspend fun lookupAnimals(
        query: String,
        parkId: String?,
        shedId: String?,
    ): Result<List<GoatSearchItemDto>> = Result.success(
        listOf(
            GoatSearchItemDto(
                goatId = "44444444-4444-4444-4444-444444444444",
                displayId = "G-77",
                animalIdentifier1 = "TAG-77",
                sex = "female",
                lifecycleStatus = "alive",
                rowVersion = 7,
                locationPath = GoatLocationPathDto(display = "North Park / Shed A"),
            ),
        ),
    )
}

private class NoopAnalyticsPort : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopTestCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
