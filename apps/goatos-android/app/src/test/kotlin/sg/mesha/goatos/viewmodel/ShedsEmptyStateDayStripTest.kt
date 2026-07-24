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
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
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
        val vm = ShedsViewModel(EmptyExecutionRepository(), NoopCrashReporter(), SavedStateHandle())
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
}

/** Emits a single empty-but-present execution response (hasData = true, zero rows). */
private class EmptyExecutionRepository : ExecutionRepository {
    override fun observeRows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, openOnly: Boolean?, limit: Int?,
    ): Flow<Resource<VaccinationExecutionResponseDto>> =
        flowOf(Resource(data = VaccinationExecutionResponseDto()))

    override suspend fun refreshRows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, openOnly: Boolean?, limit: Int?,
    ): Result<Unit> = Result.success(Unit)

    override suspend fun rows(
        parkId: String?, workState: String?, asOf: String?, dueBefore: String?, openOnly: Boolean?, limit: Int?, cursor: String?,
    ): VaccinationExecutionResponseDto = error("unused")

    override suspend fun appendRows(
        cursor: String, parkId: String?, workState: String?, asOf: String?, dueBefore: String?, openOnly: Boolean?, limit: Int?,
    ): Result<Unit> = error("unused")

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeShed(
        shedId: String, asOf: String?, dueBefore: String?, limit: Int?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?): Result<Unit> =
        error("unused")

    override suspend fun findScanRosterByTag(shedId: String, taskId: String?, normalizedTag: String): ScanRosterRowEntity? =
        error("unused")

    override fun observeScanRosterStatusCounts(shedId: String, taskId: String?): Flow<List<StatusCount>> =
        error("unused")

    override suspend fun getScanRosterStatusCountsFor(shedId: String, taskId: String?, obligationIds: List<String>): List<StatusCount> =
        error("unused")

    override suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?): List<StatusCount> =
        error("unused")

    override fun observeScanRosterRows(shedId: String, taskId: String?, windowSize: Int): Flow<List<ScanRosterRowEntity>> =
        error("unused")

    override fun observeScanRosterTotal(shedId: String, taskId: String?): Flow<Int> =
        error("unused")

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?): Flow<List<String>> =
        error("unused")

    override suspend fun scanRosterRowsByGoatIds(shedId: String, taskId: String?, goatIds: List<String>): List<ScanRosterRowEntity> =
        error("unused")

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?): Result<Unit> =
        error("unused")
}
