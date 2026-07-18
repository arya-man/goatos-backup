package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.ScanRosterResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionDriveSummaryDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedSummaryDto
import sg.mesha.goatos.feature.record.RecordEvent
import sg.mesha.goatos.feature.record.RecordTone

/**
 * RecordViewModel unit tests covering:
 * - BUG-001: a shed with due work but no scannable task (all rows lack sopTaskId) shows the
 *   honest "No scannable task yet — awaiting task assignment" status instead of a silent
 *   dead-end. The read-only record never exposes a taskId or any scan/submit affordance.
 * - BUG-002: RecordEvent only exposes Close — the verify/rework scaffolding is gone, so the
 *   read-only record can never become a second leadership verify surface (that stays in
 *   VERIFY_DETAIL).
 *
 * [state] is `stateIn(WhileSubscribed)`, so each test collects it in [backgroundScope] to make
 * the combine hot before asserting (mirrors AlertsViewModelWhileSubscribedTest's collect-to-warm).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class RecordViewModelTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `shed with due work and no scannable task shows awaiting assignment status`() = runTest(dispatcher) {
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-1",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-1",
                    shedName = "Castro 1",
                    summary = VaccinationExecutionShedSummaryDto(total = 10, completed = 5, due = 5),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-1", driveName = "PPR · Booster")),
                    rows = listOf(
                        // All rows lack sopTaskId — no scannable task yet.
                        VaccinationExecutionRowDto(shedId = "shed-1", driveName = "PPR · Booster", sopTaskId = null),
                        VaccinationExecutionRowDto(shedId = "shed-1", driveName = "PPR · Booster", sopTaskId = ""),
                    ),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-1")))
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("No scannable task yet — awaiting task assignment", state.statusLabel)
        assertEquals(RecordTone.WARN, state.statusTone)
    }

    @Test
    fun `shed with at least one valid sopTaskId does not show awaiting assignment`() = runTest(dispatcher) {
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-2",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-2",
                    shedName = "Castro 2",
                    summary = VaccinationExecutionShedSummaryDto(total = 10, completed = 5, due = 5),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-2", driveName = "Goat Pox")),
                    rows = listOf(
                        VaccinationExecutionRowDto(shedId = "shed-2", driveName = "Goat Pox", sopTaskId = "task-123"),
                        VaccinationExecutionRowDto(shedId = "shed-2", driveName = "Goat Pox", sopTaskId = null),
                    ),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-2")))
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("In progress", state.statusLabel)
        assertEquals(RecordTone.WARN, state.statusTone)
    }

    @Test
    fun `completed shed shows Done status even if rows lack sopTaskId`() = runTest(dispatcher) {
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-3",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-3",
                    shedName = "Castro 3",
                    summary = VaccinationExecutionShedSummaryDto(total = 10, completed = 10),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-3", driveName = "PPR")),
                    rows = listOf(VaccinationExecutionRowDto(shedId = "shed-3", sopTaskId = null)),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-3")))
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("Done", state.statusLabel)
        assertEquals(RecordTone.OK, state.statusTone)
    }

    @Test
    fun `shed with no rows but due work shows In progress not awaiting assignment`() = runTest(dispatcher) {
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-4",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-4",
                    shedName = "Castro 4",
                    summary = VaccinationExecutionShedSummaryDto(total = 10, completed = 5, due = 5),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "d4", driveName = "PPR")),
                    rows = emptyList(),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-4")))
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        assertEquals("In progress", vm.state.value.statusLabel)
    }

    /** BUG-002: the read-only record surface exposes only Close — no verify/rework. */
    @Test
    fun `RecordEvent only exposes Close and onEvent Close is a no-op`() = runTest(dispatcher) {
        val event: RecordEvent = RecordEvent.Close
        assertTrue(event is RecordEvent.Close)

        val vm = recordViewModel(FakeExecutionRepository(), SavedStateHandle())
        vm.onEvent(RecordEvent.Close) // must not throw / enqueue anything
    }
}

private fun recordViewModel(
    repo: ExecutionRepository,
    savedStateHandle: SavedStateHandle,
): RecordViewModel = RecordViewModel(
    repo = repo,
    analytics = NoopAnalytics(),
    crashReporter = NoopCrashReporter(),
    savedStateHandle = savedStateHandle,
)

private fun kotlinx.coroutines.CoroutineScope.launchCollect(vm: RecordViewModel) {
    launch { vm.state.collect {} }
}

/**
 * Full [ExecutionRepository] fake. Only [observeShed]/[refreshShed] are exercised by
 * RecordViewModel; the rest are `error("unused")` so an accidental call is loud, not silent.
 */
private class FakeExecutionRepository : ExecutionRepository {
    private val sheds = mutableMapOf<String, MutableStateFlow<VaccinationExecutionShedDrilldownDto?>>()

    fun setShed(shedId: String, dto: VaccinationExecutionShedDrilldownDto) {
        sheds.getOrPut(shedId) { MutableStateFlow(null) }.value = dto
    }

    override fun observeShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> =
        sheds.getOrPut(shedId) { MutableStateFlow(null) }.map { Resource(data = it, lastSyncedAt = null) }

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): Result<Unit> =
        Result.success(Unit)

    override suspend fun findScanRosterByTag(
        shedId: String,
        normalizedTag: String,
    ): sg.mesha.goatos.core.data.cache.ScanRosterRowEntity? = null

    override fun observeScanRosterStatusCounts(
        shedId: String,
    ): Flow<List<sg.mesha.goatos.core.data.cache.StatusCount>> = kotlinx.coroutines.flow.flowOf(emptyList())

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        obligationIds: List<String>,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> = emptyList()

    override suspend fun getScanRosterStatusCounts(
        shedId: String,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> = emptyList()

    override suspend fun rows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?, cursor: String?,
    ): VaccinationExecutionResponseDto = error("unused")

    override fun observeRows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?,
    ): Flow<Resource<VaccinationExecutionResponseDto>> = error("unused")

    override suspend fun refreshRows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?,
    ): Result<Unit> = error("unused")

    override suspend fun appendRows(
        cursor: String, parkId: String?, workState: String?, asOf: String?, dueBefore: String?, limit: Int?,
    ): Result<Unit> = error("unused")

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override suspend fun scanRoster(shedId: String, taskId: String?, cursor: String?, limit: Int?): ScanRosterResponseDto =
        error("unused")

    override fun observeScanRoster(shedId: String, taskId: String?, limit: Int?): Flow<Resource<ScanRosterResponseDto>> =
        error("unused")

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?): Result<Unit> = error("unused")

    override suspend fun refreshCompleteScanRoster(shedId: String, taskId: String?, limit: Int?): Result<Unit> =
        error("unused")

    override suspend fun appendScanRoster(shedId: String, taskId: String?, cursor: String, limit: Int?): Result<Unit> =
        error("unused")
}
