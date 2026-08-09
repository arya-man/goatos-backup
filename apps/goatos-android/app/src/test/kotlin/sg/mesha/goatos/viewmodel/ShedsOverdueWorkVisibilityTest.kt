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
 * P0 regression: operator's overdue vaccination work must be visible on today's screen.
 *
 * The bug: on Sun 2 Aug, Saturday's (1 Aug) overdue work with open/review status was
 * invisible on today's list because the day filter checked `dueDate.isBefore(firstDay)`
 * instead of `dueDate.isBefore(today)`. Since Saturday == firstDay (yesterday), the
 * condition was false, and Saturday's rows fell through to the `else` branch.
 *
 * The fix: fold in ALL backlog work (open, review, or already completed) whose due date is
 * on or before today:
 *   !dueDate.isAfter(workWindow.today)
 *
 * Completed backlog rows now ALSO stay on today's list, not just their original due date --
 * a maintainer-specified product decision (2026-08-05): a shed the operator finished must
 * stay visible until its DRIVE closes, and the drive closing is exactly the moment the
 * backend stops returning the row at all (it falls outside workWindow.asOf/dueBefore).
 * As long as the API still sends it, the card stays. This superseded an earlier version of
 * this same test file that asserted completed backlog rows should NOT appear on today --
 * that assertion encoded the defect this rule now fixes (a completed shed silently
 * vanishing from the operator's list mid-drive). See ShedsViewModelTest's
 * "a completed backlog shed stays on today's list and is marked non-openable" for the
 * pinning test on the fixed behaviour.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShedsOverdueWorkVisibilityTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `open work due yesterday appears on today's tab`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val yesterday = today.minusDays(1).toString()

        val vm = ShedsViewModel(
            OverdueTestExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        // Saturday work with open status — should appear on Sunday's tab
                        VaccinationExecutionRowDto(
                            shedId = "gandhi-1",
                            shedName = "Gandhi 1",
                            physicalShed = "Gandhi 1",
                            targetCount = 5,
                            openCount = 5,
                            doneCount = 0,
                            dueDate = yesterday,
                            workState = "in_progress",
                            sopStatus = "pending",
                            proofStatus = "",
                            verificationStatus = "",
                        ),
                    ),
                    totalCount = 1,
                ),
            ),
            NoopCrashReporter(),
            sg.mesha.goatos.core.analytics.NoopAnalytics(),
            OverdueFakeBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("open work due yesterday should appear on today's list", 1, state.rows.size)
        assertEquals("Gandhi 1", state.rows[0].name)
        assertEquals("5", state.rows[0].due)
    }

    @Test
    fun `completed work due yesterday also stays visible on today's tab until its drive closes`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val yesterday = today.minusDays(1).toString()

        val vm = ShedsViewModel(
            OverdueTestExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        // Saturday work that is completed — the backend is still returning it
                        // (its drive has not closed), so it must stay visible on Sunday too.
                        VaccinationExecutionRowDto(
                            shedId = "gandhi-2",
                            shedName = "Gandhi 2",
                            physicalShed = "Gandhi 2",
                            targetCount = 5,
                            openCount = 0,
                            doneCount = 5,
                            dueDate = yesterday,
                            workState = "completed",
                            sopStatus = "accepted",
                            proofStatus = "accepted",
                            verificationStatus = "verified",
                        ),
                    ),
                    totalCount = 1,
                ),
            ),
            NoopCrashReporter(),
            sg.mesha.goatos.core.analytics.NoopAnalytics(),
            OverdueFakeBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("completed work must stay visible on today's list while its drive is active", 1, state.rows.size)
        assertEquals("Gandhi 2", state.rows[0].name)
    }

    @Test
    fun `deep backlog with open work appears on today's tab`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val deepBacklog = today.minusDays(7).toString() // A week before the visible window

        val vm = ShedsViewModel(
            OverdueTestExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        // Very old work with open status — should appear on today
                        VaccinationExecutionRowDto(
                            shedId = "yashoda-1",
                            shedName = "Yashoda 1",
                            physicalShed = "Yashoda 1",
                            targetCount = 10,
                            openCount = 10,
                            doneCount = 0,
                            dueDate = deepBacklog,
                            workState = "in_review",
                            sopStatus = "submitted",
                            proofStatus = "uploaded",
                            verificationStatus = "pending",
                        ),
                    ),
                    totalCount = 1,
                ),
            ),
            NoopCrashReporter(),
            sg.mesha.goatos.core.analytics.NoopAnalytics(),
            OverdueFakeBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("deep backlog with open work should appear on today", 1, state.rows.size)
        assertEquals("Yashoda 1", state.rows[0].name)
    }

    @Test
    fun `both open and completed work due yesterday both appear on yesterday's tab`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val yesterday = today.minusDays(1).toString()

        val vm = ShedsViewModel(
            OverdueTestExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        VaccinationExecutionRowDto(
                            shedId = "gandhi-1",
                            shedName = "Gandhi 1",
                            physicalShed = "Gandhi 1",
                            targetCount = 5,
                            openCount = 5,
                            doneCount = 0,
                            dueDate = yesterday,
                            workState = "in_progress",
                            sopStatus = "pending",
                            proofStatus = "",
                            verificationStatus = "",
                        ),
                        VaccinationExecutionRowDto(
                            shedId = "gandhi-2",
                            shedName = "Gandhi 2",
                            physicalShed = "Gandhi 2",
                            targetCount = 5,
                            openCount = 0,
                            doneCount = 5,
                            dueDate = yesterday,
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
            OverdueFakeBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Select yesterday to verify both rows appear there
        vm.onEvent(sg.mesha.goatos.feature.sheds.ShedsEvent.SelectDay(yesterday))
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("both open and completed work should appear on yesterday's tab", 2, state.rows.size)
        assertEquals(listOf("Gandhi 1", "Gandhi 2"), state.rows.map { it.name })
    }

    @Test
    fun `mixing yesterday open and completed shows both on today, and both on yesterday`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val yesterday = today.minusDays(1).toString()

        val vm = ShedsViewModel(
            OverdueTestExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        VaccinationExecutionRowDto(
                            shedId = "gandhi-1",
                            shedName = "Gandhi 1",
                            physicalShed = "Gandhi 1",
                            targetCount = 5,
                            openCount = 5,
                            doneCount = 0,
                            dueDate = yesterday,
                            workState = "in_progress",
                            sopStatus = "pending",
                            proofStatus = "",
                            verificationStatus = "",
                        ),
                        VaccinationExecutionRowDto(
                            shedId = "gandhi-2",
                            shedName = "Gandhi 2",
                            physicalShed = "Gandhi 2",
                            targetCount = 5,
                            openCount = 0,
                            doneCount = 5,
                            dueDate = yesterday,
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
            OverdueFakeBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Verify today shows both the open AND the completed work from yesterday — the
        // completed shed stays visible until its drive closes, not just on its own due date.
        val stateToday = vm.state.value
        assertEquals("today should show both open and completed work from yesterday", 2, stateToday.rows.size)
        assertEquals(listOf("Gandhi 1", "Gandhi 2"), stateToday.rows.map { it.name })

        // Now select yesterday to verify both appear there
        vm.onEvent(sg.mesha.goatos.feature.sheds.ShedsEvent.SelectDay(yesterday))
        advanceUntilIdle()

        val stateYesterday = vm.state.value
        assertEquals("yesterday should show both open and completed work", 2, stateYesterday.rows.size)
        assertEquals(listOf("Gandhi 1", "Gandhi 2"), stateYesterday.rows.map { it.name })
    }

    @Test
    fun `deep backlog completed work stays visible on today while its drive is still active`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val deepBacklog = today.minusDays(7).toString()

        val vm = ShedsViewModel(
            OverdueTestExecutionRepository(
                VaccinationExecutionResponseDto(
                    rows = listOf(
                        // Deep backlog completed work — the backend is still returning this row
                        // (its drive has not closed), so the operator must still see it today.
                        VaccinationExecutionRowDto(
                            shedId = "godel-1",
                            shedName = "Godel 1",
                            physicalShed = "Godel 1",
                            targetCount = 8,
                            openCount = 0,
                            doneCount = 8,
                            dueDate = deepBacklog,
                            workState = "completed",
                            sopStatus = "accepted",
                            proofStatus = "accepted",
                            verificationStatus = "verified",
                        ),
                    ),
                    totalCount = 1,
                ),
            ),
            NoopCrashReporter(),
            sg.mesha.goatos.core.analytics.NoopAnalytics(),
            OverdueFakeBootstrapRepository(),
            SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("completed deep backlog should stay visible while its drive is active", 1, state.rows.size)
        assertEquals("Godel 1", state.rows[0].name)
    }
}

private class OverdueFakeBootstrapRepository(
    private val role: String = "operator",
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto =
        BootstrapOperatorProfileDto(primaryRoleHint = role)
}

private class OverdueTestExecutionRepository(
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
