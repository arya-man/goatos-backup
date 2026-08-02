package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
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
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.feature.sheds.ShedStatusChipKey
import java.time.LocalDate
import java.time.ZoneId

/**
 * P1 regression: a shed the operator already SUBMITTED must not paint a red "Overdue" chip.
 *
 * Observed 2026-08-03 on the CEO app: Castro 3 / Mandela 2 / Castro 2 showed
 * "In review" AND "Overdue" together. Backend said workState=verification_pending,
 * severity=watch, openCount=0, reviewCount=5 — no overdue signal anywhere.
 * The client re-derived overdue-ness from a bare `dueDate < today` comparison and
 * dropped the submission term that `canonical_read.go` (genuine_overdue) requires.
 *
 * These tests pin all three arms: submitted-past-due => no chip, genuinely-open
 * past-due => chip still shows, final-closed => no chip.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShedsOverdueChipSubmittedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModelFor(vararg rows: VaccinationExecutionRowDto) = ShedsViewModel(
        ChipTestExecutionRepository(
            VaccinationExecutionResponseDto(rows = rows.toList(), totalCount = rows.size),
        ),
        NoopCrashReporter(),
        ChipTestBootstrapRepository(),
        SavedStateHandle(),
    )

    @Test
    fun `submitted but unverified past-due shed shows In review without Overdue`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val yesterday = today.minusDays(1).toString()

        // Exactly what /app/vaccination/execution returned for Castro 3 on 2026-08-03.
        val vm = viewModelFor(
            VaccinationExecutionRowDto(
                shedId = "castro-3",
                shedName = "Castro 3",
                physicalShed = "Castro 3",
                targetCount = 5,
                openCount = 0,
                doneCount = 5,
                reviewCount = 5,
                dueDate = yesterday,
                workState = "verification_pending",
                severity = "watch",
                sopStatus = "submitted",
                proofStatus = "uploaded",
                verificationStatus = "pending",
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("submitted shed should still be listed", 1, state.rows.size)
        val chips = state.rows[0].statusChips.map { it.key }
        assertTrue("expected In review chip, got $chips", chips.contains(ShedStatusChipKey.IN_REVIEW))
        assertFalse("submitted work must NOT paint Overdue, got $chips", chips.contains(ShedStatusChipKey.OVERDUE))
    }

    @Test
    fun `genuinely open past-due shed still shows Overdue`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
        val yesterday = today.minusDays(1).toString()

        val vm = viewModelFor(
            VaccinationExecutionRowDto(
                shedId = "gandhi-1",
                shedName = "Gandhi 1",
                physicalShed = "Gandhi 1",
                targetCount = 5,
                openCount = 5,
                doneCount = 0,
                dueDate = yesterday,
                workState = "in_progress",
                severity = "urgent",
                sopStatus = "pending",
                proofStatus = "",
                verificationStatus = "",
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(1, state.rows.size)
        val chips = state.rows[0].statusChips.map { it.key }
        assertTrue("real lateness must still be shown, got $chips", chips.contains(ShedStatusChipKey.OVERDUE))
    }

    @Test
    fun `final closed shed shows no Overdue chip`() = runTest(dispatcher) {
        val today = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()

        val vm = viewModelFor(
            VaccinationExecutionRowDto(
                shedId = "mandela-2",
                shedName = "Mandela 2",
                physicalShed = "Mandela 2",
                targetCount = 5,
                openCount = 0,
                doneCount = 5,
                acceptedCount = 5,
                dueDate = today,
                workState = "completed",
                severity = "ok",
                sopStatus = "accepted",
                proofStatus = "accepted",
                verificationStatus = "verified",
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(1, state.rows.size)
        val chips = state.rows[0].statusChips.map { it.key }
        assertFalse("closed work must NOT paint Overdue, got $chips", chips.contains(ShedStatusChipKey.OVERDUE))
    }
}

private class ChipTestBootstrapRepository(
    private val role: String = "operator",
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto =
        BootstrapOperatorProfileDto(primaryRoleHint = role)
}

private class ChipTestExecutionRepository(
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
