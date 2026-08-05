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
import sg.mesha.goatos.feature.sheds.ShedStatusTone

/**
 * Real-device regression, 2026-08-04: a verifier rejected shed "Gandhi 1". Backend returned
 * workState="rejected", openCount=0, doneCount=5, targetCount=5, reviewCount=5. Neither
 * [shedStatusForRows] nor [VaccinationExecutionRowDto.isFinalClosed] recognised "rejected"
 * (or "deferred") as a distinct state, so the shed fell through to ShedStatus.PENDING and
 * rendered chip "In progress" / tone WARN with a full green 100% progress bar — the operator
 * had no way to see his work was rejected or that he needed to redo it.
 *
 * These tests pin: rejected shed => OVERDUE-tone chip (not "In progress"), progress bar NOT
 * full even though doneCount==targetCount, and the same for "deferred".
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShedsRejectedStateTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModelFor(vararg rows: VaccinationExecutionRowDto) = ShedsViewModel(
        RejectedTestExecutionRepository(
            VaccinationExecutionResponseDto(rows = rows.toList(), totalCount = rows.size),
        ),
        NoopCrashReporter(),
        sg.mesha.goatos.core.analytics.NoopAnalytics(),
        RejectedTestBootstrapRepository(),
        SavedStateHandle(),
    )

    @Test
    fun `rejected shed does not render as In progress with a full bar`() = runTest(dispatcher) {
        val vm = viewModelFor(
            VaccinationExecutionRowDto(
                shedId = "gandhi-1",
                shedName = "Gandhi 1",
                physicalShed = "Gandhi 1",
                targetCount = 5,
                openCount = 1,
                doneCount = 4,
                reviewCount = 5,
                dueDate = null,
                workState = "rejected",
                severity = "broken",
                sopStatus = "rework_requested",
                proofStatus = "rejected",
                verificationStatus = "rejected",
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(1, state.rows.size)
        val row = state.rows[0]
        val chips = row.statusChips
        assertFalse(
            "rejected shed must NOT show the In progress chip, got ${chips.map { it.key }}",
            chips.any { it.key == ShedStatusChipKey.IN_PROGRESS },
        )
        assertTrue(
            "rejected shed must surface a danger-tone chip so the operator can see it needs redo, got $chips",
            chips.any { it.tone == ShedStatusTone.DANGER },
        )
        assertTrue(
            "progress must not read 100% complete for a rejected shed, got ${row.progressFraction}",
            row.progressFraction < 1f,
        )
        // The four animals that were accepted keep their credit. Zeroing the shed's done count
        // because ONE animal came back told the operator to redo the whole shed -- the opposite
        // of the rule that only rejected animals are re-scanned.
        assertEquals(
            "accepted animals must keep their credit; only the rejected one is outstanding",
            "4",
            row.done,
        )
        assertEquals("the rejected animal is the outstanding work", "1", row.due)
        assertFalse(
            "done count must not equal target while an animal is still to redo, got done=${row.done} target=${row.inShed}",
            row.done == row.inShed,
        )
    }

    @Test
    fun `deferred shed does not render as In progress with a full bar`() = runTest(dispatcher) {
        val vm = viewModelFor(
            VaccinationExecutionRowDto(
                shedId = "castro-1",
                shedName = "Castro 1",
                physicalShed = "Castro 1",
                targetCount = 4,
                openCount = 0,
                doneCount = 4,
                dueDate = null,
                workState = "deferred",
                severity = "broken",
                sopStatus = "pending",
                proofStatus = "",
                verificationStatus = "",
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals(1, state.rows.size)
        val row = state.rows[0]
        assertFalse(
            "deferred shed must NOT show the In progress chip, got ${row.statusChips.map { it.key }}",
            row.statusChips.any { it.key == ShedStatusChipKey.IN_PROGRESS },
        )
        assertTrue(
            "progress must not read 100% complete for a deferred shed, got ${row.progressFraction}",
            row.progressFraction < 1f,
        )
    }
}

private class RejectedTestBootstrapRepository(
    private val role: String = "operator",
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto =
        BootstrapOperatorProfileDto(primaryRoleHint = role)
}

private class RejectedTestExecutionRepository(
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
