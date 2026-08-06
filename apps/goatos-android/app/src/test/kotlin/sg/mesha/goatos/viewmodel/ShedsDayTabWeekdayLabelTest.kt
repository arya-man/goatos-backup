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
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import java.time.LocalDate
import java.time.format.TextStyle
import java.util.Locale

/**
 * Regression guard for the vaccination-sheds "rhythm strip" day selector: each of the 7 day
 * tabs must carry a non-blank weekday abbreviation (WED, THU, ...) alongside its day-of-month
 * number, so an operator never sees a bare "6" with no indication it is Thursday. This backs
 * [sg.mesha.goatos.feature.sheds.ShedsScreen] `DayTabs`, which renders `tab.dayLabel` (weekday)
 * above `tab.dateLabel` (day-of-month) — see ShedsScreen.kt ~line 699-702, fed by
 * `buildOperatorDayTabs` in ShedsViewModel.kt.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShedsDayTabWeekdayLabelTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `every day tab carries a non-blank weekday abbreviation above the day number`() = runTest(dispatcher) {
        val vm = ShedsViewModel(
            WeekdayEmptyExecutionRepository(),
            NoopCrashReporter(),
            NoopAnalytics(),
            FakeWeekdayBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val tabs = vm.state.value.dayTabs
        assertTrue("expected 7 day tabs", tabs.size == 7)
        tabs.forEach { tab ->
            assertTrue(
                "day tab ${tab.dateKey} must have a non-blank weekday label, got '${tab.dayLabel}'",
                tab.dayLabel.isNotBlank(),
            )
            val date = LocalDate.parse(tab.dateKey)
            val expectedWeekday = date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH).uppercase(Locale.ENGLISH)
            assertEquals(expectedWeekday, tab.dayLabel)
            assertEquals(date.dayOfMonth.toString(), tab.dateLabel)
        }
    }
}

private class FakeWeekdayBootstrapRepository : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto =
        BootstrapOperatorProfileDto(primaryRoleHint = "operator")
}

private class WeekdayEmptyExecutionRepository(
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
    ): Flow<Resource<VaccinationExecutionResponseDto>> = flowOf(Resource(data = response))

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
