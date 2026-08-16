package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
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
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.feature.sheds.ShedStatus
import java.time.LocalDate
import java.time.ZoneId

/**
 * Regression: a vaccination-sheds view with NO data for the window (a CEO/leadership principal
 * with no assigned sheds, an operator on a drive-free day, or the loading/offline moment) must
 * still render the real day strip — one prior day (yesterday) through today+5, landing on today —
 * and stay tap-responsive.
 *
 * The bug: the empty/loading/offline branch fell to [sg.mesha.goatos.ui.shedsPlaceholder], which
 * copies `sampleShedsState()` and never resets its hardcoded sample `dayTabs`
 * (`2026-07-22 WED` selected). So the strip rendered the sample dates — never defaulting to today
 * and ignoring day taps — reproduced live on the CEO/CXO "Vaccination" screen (WED 22 selected).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShedsEmptyStateDayStripTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `empty response anchors the day strip on yesterday and lands on today`() = runTest(dispatcher) {
        val vm = ShedsViewModel(EmptyExecutionRepository(), NoopCrashReporter(), sg.mesha.goatos.core.analytics.NoopAnalytics(), FakeShedsBootstrapRepository(), SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val todayDate = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val today = todayDate.toString()
        val yesterday = todayDate.minusDays(1).toString()
        val tabs = vm.state.value.dayTabs

        assertTrue("empty state must still render a day strip", tabs.isNotEmpty())
        assertEquals("day strip must start one day before today", yesterday, tabs.first().dateKey)
        assertEquals("strip is a 7-day window", 7, tabs.size)
        assertEquals("last tab is today+5", todayDate.plusDays(5).toString(), tabs.last().dateKey)
        assertEquals("today must be the selected/landing tab", today, tabs.single { it.isSelected }.dateKey)
    }

    @Test
    fun `yesterday tab is selectable`() = runTest(dispatcher) {
        val vm = ShedsViewModel(EmptyExecutionRepository(), NoopCrashReporter(), sg.mesha.goatos.core.analytics.NoopAnalytics(), FakeShedsBootstrapRepository(), SavedStateHandle())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val yesterday = LocalDate.now(ZoneId.of("Asia/Kolkata")).minusDays(1).toString()
        vm.onEvent(sg.mesha.goatos.feature.sheds.ShedsEvent.SelectDay(yesterday))
        advanceUntilIdle()

        val tabs = vm.state.value.dayTabs
        assertEquals("tapping yesterday selects it", yesterday, tabs.single { it.isSelected }.dateKey)
    }

    @Test
    fun `completed sheds for today remain visible to the operator`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
        val vm = ShedsViewModel(
            EmptyExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        VaccinationExecutionRowDto(
                            shedId = "godel-1",
                            shedName = "Godel 1",
                            physicalShed = "Godel 1",
                            targetCount = 2,
                            doneCount = 2,
                            dueDate = today,
                            workState = "completed",
                            sopStatus = "accepted",
                            proofStatus = "accepted",
                            verificationStatus = "verified",
                        ),
                        VaccinationExecutionRowDto(
                            shedId = "godel-2",
                            shedName = "Godel 2",
                            physicalShed = "Godel 2",
                            targetCount = 3,
                            doneCount = 3,
                            dueDate = today,
                            workState = "completed",
                            sopStatus = "accepted",
                            proofStatus = "accepted",
                            verificationStatus = "verified",
                        ),
                    ),
                    totalCount = 2,
                ),
            ),
            NoopCrashReporter(),
            sg.mesha.goatos.core.analytics.NoopAnalytics(),
            FakeShedsBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(null, state.caption)
        assertEquals(2, state.rows.size)
        assertEquals(listOf("Godel 1", "Godel 2"), state.rows.map { it.name })
        assertTrue(state.rows.all { it.status == ShedStatus.DONE })
        assertEquals(5, state.doneCount)
        assertEquals("5 / 5 done", state.daySummary)
    }

    @Test
    fun `pc director gets leadership shed presentation even when execution is allowed`() = runTest(dispatcher) {
        val vm = ShedsViewModel(
            EmptyExecutionRepository(),
            NoopCrashReporter(),
            sg.mesha.goatos.core.analytics.NoopAnalytics(),
            FakeShedsBootstrapRepository(role = "pc_director"),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(vm.state.value.leadershipMode)
    }
}

private class FakeShedsBootstrapRepository(
    private val role: String = "operator",
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto =
        BootstrapOperatorProfileDto(primaryRoleHint = role)
}

/** Emits a single empty-but-present execution response (hasData = true, zero rows). */
private class EmptyExecutionRepository(
    private val response: VaccinationExecutionResponseDto = VaccinationExecutionResponseDto(),
) : ExecutionRepository {
    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Flow<Resource<VaccinationExecutionResponseDto>> =
        flowOf(Resource(data = response))

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = Result.success(Unit)

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

    override fun observeShed(
        shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): Result<Unit> =
        error("unused")

    override suspend fun findScanRosterByTag(shedId: String, taskId: String?, normalizedTag: String, partitionLabel: String?): ScanRosterRowEntity? =
        error("unused")

    override fun observeScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<StatusCount>> =
        error("unused")

    override suspend fun getScanRosterStatusCountsFor(shedId: String, taskId: String?, obligationIds: List<String>, partitionLabel: String?): List<StatusCount> =
        error("unused")

    override suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): List<StatusCount> =
        error("unused")

    override suspend fun openScanRosterRows(shedId: String, taskId: String?, partitionLabel: String?): List<ScanRosterRowEntity> =
        emptyList()

    override suspend fun siblingPartitionOpenRows(shedId: String, taskId: String?, activePartitionLabel: String?): List<ScanRosterRowEntity> =
        emptyList()

    override suspend fun otherShedOpenRows(shedId: String, taskId: String?): List<ScanRosterRowEntity> =
        emptyList()


    override fun observeScanRosterRows(shedId: String, taskId: String?, windowSize: Int, partitionLabel: String?): Flow<List<ScanRosterRowEntity>> =
        error("unused")

    override fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String?): Flow<Int> =
        error("unused")

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<String>> =
        error("unused")

    override suspend fun scanRosterRowsByGoatIds(shedId: String, taskId: String?, goatIds: List<String>, partitionLabel: String?): List<ScanRosterRowEntity> =
        error("unused")

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?, partitionLabel: String?): Result<Unit> =
        error("unused")
}
