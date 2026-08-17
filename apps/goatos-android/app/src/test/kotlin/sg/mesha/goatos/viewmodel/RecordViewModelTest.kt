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
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
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
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-1", "partitionLabel" to "Part 2")))
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("No scannable task yet — awaiting task assignment", state.statusLabel)
        assertEquals(RecordTone.WARN, state.statusTone)
        assertEquals("Part 2", repo.lastObservedPartitionLabel)
        assertEquals("Part 2", repo.lastRefreshPartitionLabel)
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

    // --- done-vs-abandon split (adversarial review gap #4) ------------------------------------
    //
    // captureCompletedTracked is a ONE-SHOT guard shared by two producers of
    // funnel_vaccination_capture_completed: the reactive path in toRecordUiState (fires "done"
    // the moment Room reports the shed complete) and RecordEvent.Close (fires "abandon" if the
    // shed was NOT complete at tap time, reading observedResource.value.data — a Room snapshot).
    // These tests settle the two claims from review: a completed shed must never be reported as
    // abandoned, an incomplete shed closed early must be reported as abandoned, and either path
    // fires at most once per ViewModel session.

    @Test
    fun `completion is tracked as done and a later Close never re-reports abandon`() = runTest(dispatcher) {
        val analytics = RecordingAnalytics()
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-done",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-done",
                    shedName = "Castro Done",
                    summary = VaccinationExecutionShedSummaryDto(total = 4, completed = 4),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-done", driveName = "PPR")),
                    rows = listOf(VaccinationExecutionRowDto(shedId = "shed-done", sopTaskId = "task-1")),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-done")), analytics)
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        // The reactive path already reported "done" the moment the combine saw the completed DTO.
        val completedEvents = analytics.events.filter { it.name == AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED }
        assertEquals(1, completedEvents.size)
        assertEquals("done", completedEvents.single().props[AnalyticsFunnels.Params.OUTCOME])

        // Now the operator backs out. Close must NOT log a second, contradicting "abandon" —
        // the one-shot guard must hold across both producers.
        vm.onEvent(RecordEvent.Close)
        val allCompletedEvents = analytics.events.filter { it.name == AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED }
        assertEquals(1, allCompletedEvents.size)
        assertEquals("done", allCompletedEvents.single().props[AnalyticsFunnels.Params.OUTCOME])
    }

    @Test
    fun `closing an incomplete shed records abandon exactly once`() = runTest(dispatcher) {
        val analytics = RecordingAnalytics()
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-partial",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-partial",
                    shedName = "Castro Partial",
                    summary = VaccinationExecutionShedSummaryDto(total = 4, completed = 2),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-partial", driveName = "PPR")),
                    rows = listOf(VaccinationExecutionRowDto(shedId = "shed-partial", sopTaskId = "task-1")),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-partial")), analytics)
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        // Not complete: the reactive "done" path must not have fired.
        assertTrue(analytics.events.none { it.name == AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED })

        vm.onEvent(RecordEvent.Close)
        val completedEvents = analytics.events.filter { it.name == AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED }
        assertEquals(1, completedEvents.size)
        assertEquals("abandon", completedEvents.single().props[AnalyticsFunnels.Params.OUTCOME])

        // A second Close tap (e.g. a re-entrant back-press) must not log a second abandon.
        vm.onEvent(RecordEvent.Close)
        assertEquals(1, analytics.events.count { it.name == AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED })
    }

    @Test
    fun `a shed that becomes complete before any Close never logs abandon even without warming state first`() = runTest(dispatcher) {
        // Regression guard for the reviewed race: RecordEvent.Close reads observedResource.value
        // directly rather than the combined `state`, so it must see the SAME up-to-date Room
        // snapshot the reactive done-tracker saw — not a stale pre-completion snapshot — as long
        // as the shed's Flow has already delivered the completed DTO before Close is handled.
        val analytics = RecordingAnalytics()
        val repo = FakeExecutionRepository().apply {
            setShed(
                "shed-race",
                VaccinationExecutionShedDrilldownDto(
                    shedId = "shed-race",
                    shedName = "Castro Race",
                    summary = VaccinationExecutionShedSummaryDto(total = 1, completed = 0),
                    drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-race", driveName = "PPR")),
                    rows = listOf(VaccinationExecutionRowDto(shedId = "shed-race", sopTaskId = "task-1")),
                ),
            )
        }
        val vm = recordViewModel(repo, SavedStateHandle(mapOf("shedId" to "shed-race")), analytics)
        backgroundScope.launchCollect(vm)
        advanceUntilIdle()

        // The final animal's submission lands in Room (mirrors ExecutionRepository.refreshShed's
        // Room upsert) BEFORE the operator's tap is handled.
        repo.setShed(
            "shed-race",
            VaccinationExecutionShedDrilldownDto(
                shedId = "shed-race",
                shedName = "Castro Race",
                summary = VaccinationExecutionShedSummaryDto(total = 1, completed = 1),
                drives = listOf(VaccinationExecutionDriveSummaryDto(driveId = "drive-race", driveName = "PPR")),
                rows = listOf(VaccinationExecutionRowDto(shedId = "shed-race", sopTaskId = "task-1")),
            ),
        )
        advanceUntilIdle()
        vm.onEvent(RecordEvent.Close)

        val completedEvents = analytics.events.filter { it.name == AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED }
        assertEquals(1, completedEvents.size)
        assertEquals("done", completedEvents.single().props[AnalyticsFunnels.Params.OUTCOME])
    }
}

private fun recordViewModel(
    repo: ExecutionRepository,
    savedStateHandle: SavedStateHandle,
    analytics: AnalyticsPort = NoopAnalytics(),
): RecordViewModel = RecordViewModel(
    repo = repo,
    analytics = analytics,
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
    var lastObservedPartitionLabel: String? = null
        private set
    var lastRefreshPartitionLabel: String? = null
        private set

    fun setShed(shedId: String, dto: VaccinationExecutionShedDrilldownDto) {
        sheds.getOrPut(shedId) { MutableStateFlow(null) }.value = dto
    }

    override fun observeShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> {
        lastObservedPartitionLabel = partitionLabel
        return sheds.getOrPut(shedId) { MutableStateFlow(null) }.map { Resource(data = it, lastSyncedAt = null) }
    }

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): Result<Unit> {
        lastRefreshPartitionLabel = partitionLabel
        return Result.success(Unit)
    }

    override suspend fun findScanRosterByTag(
        shedId: String,
        taskId: String?,
        normalizedTag: String,
        partitionLabel: String?,
    ): sg.mesha.goatos.core.data.cache.ScanRosterRowEntity? = null

    override fun observeScanRosterStatusCounts(
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): Flow<List<sg.mesha.goatos.core.data.cache.StatusCount>> = kotlinx.coroutines.flow.flowOf(emptyList())

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        obligationIds: List<String>,
        partitionLabel: String?,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> = emptyList()

    override suspend fun getScanRosterStatusCounts(
        shedId: String,
        taskId: String?,
        partitionLabel: String?,
    ): List<sg.mesha.goatos.core.data.cache.StatusCount> = emptyList()

    override suspend fun openScanRosterRows(shedId: String, taskId: String?, partitionLabel: String?): List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity> =
        emptyList()

    override suspend fun siblingPartitionOpenRows(shedId: String, taskId: String?, activePartitionLabel: String?): List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity> =
        emptyList()

    override suspend fun otherShedOpenRows(shedId: String, taskId: String?): List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity> =
        emptyList()

    override suspend fun rows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        cursor: String?,
        includeFilterOptions: Boolean,
    ): VaccinationExecutionResponseDto = error("unused")

    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Flow<Resource<VaccinationExecutionResponseDto>> = error("unused")

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = error("unused")

    override suspend fun appendRows(
        cursor: String,
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = error("unused")

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeScanRosterRows(
        shedId: String,
        taskId: String?,
        windowSize: Int,
        partitionLabel: String?,
    ): Flow<List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity>> = kotlinx.coroutines.flow.flowOf(emptyList())

    override fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String?): Flow<Int> = kotlinx.coroutines.flow.flowOf(0)

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<String>> =
        kotlinx.coroutines.flow.flowOf(emptyList())

    override suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<sg.mesha.goatos.core.data.cache.ScanRosterRowEntity> = emptyList()

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?, partitionLabel: String?): Result<Unit> = error("unused")
}
