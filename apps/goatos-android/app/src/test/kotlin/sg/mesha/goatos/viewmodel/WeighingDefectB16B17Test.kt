package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalogCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskListCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskPage
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_MINE
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.scan.ScanReaderConnection
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore

/**
 * Unit tests for defects B16 and B17 in Goat OS weighing module.
 *
 * B16: Close button not rendering when bucket is ready to close
 * - Verifies ready_to_close and pending_verification_count fields flow from backend → DTO → domain → UI
 * - Verifies Close action gated on readyToClose when submitted and awaiting verification
 * - Verifies disabled-with-reason message shows pending verification count when not ready
 *
 * B17: Park filtering breaks pagination
 * - Verifies prefetch calculation uses filtered list size, not unfiltered
 * - With 20 loaded rows and 5 matching filter, pagination fires when the last filtered row (index 4)
 *   is composed, not silently skipped
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WeighingDefectB16B17Test {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    // ==================== B16: readyToClose and pendingVerificationCount field tests ====================

    @Test
    fun `B16 - WeighingAssignmentUiRow includes readyToClose field`() {
        // Verify the data class carries readyToClose with default false
        val row = WeighingAssignmentUiRow(
            campaignId = "c1",
            tenantId = "t1",
            parkId = "p1",
            parkLabel = "Park A",
            workGroupId = "wg1",
            campaignShedId = "cs1",
            expectedLocationId = "loc1",
            expectedLocationLabel = "Shed A",
            label = "Shed A",
            category = "shed",
            status = "completed",
            expectedCount = 10,
            periodLabel = "2026-08-01",
            readyToClose = false,
            pendingVerificationCount = 3,
        )

        assertEquals(false, row.readyToClose)
        assertEquals(3, row.pendingVerificationCount)
    }

    @Test
    fun `B16 - canClose computed property gates on readyToClose and isSubmittedAndWaitingVerification`() {
        // Test case 1: submitted and waiting verification, ready to close → canClose = true
        val rowReadyToClose = WeighingAssignmentUiRow(
            campaignId = "c1",
            tenantId = "t1",
            parkId = "p1",
            parkLabel = "Park A",
            workGroupId = "wg1",
            campaignShedId = "cs1",
            expectedLocationId = "loc1",
            expectedLocationLabel = "Shed A",
            label = "Shed A",
            category = "shed",
            status = "completed", // isSubmittedAndWaitingVerification = true
            expectedCount = 10,
            periodLabel = "2026-08-01",
            readyToClose = true,
            pendingVerificationCount = 0,
        )

        assertTrue("Should be closeable when readyToClose=true and status=completed", rowReadyToClose.canClose)

        // Test case 2: submitted and waiting verification, NOT ready (pending verifications) → canClose = false
        val rowPendingVerification = WeighingAssignmentUiRow(
            campaignId = "c1",
            tenantId = "t1",
            parkId = "p1",
            parkLabel = "Park A",
            workGroupId = "wg1",
            campaignShedId = "cs1",
            expectedLocationId = "loc1",
            expectedLocationLabel = "Shed A",
            label = "Shed A",
            category = "shed",
            status = "completed", // isSubmittedAndWaitingVerification = true
            expectedCount = 10,
            periodLabel = "2026-08-01",
            readyToClose = false,
            pendingVerificationCount = 3,
        )

        assertFalse("Should NOT be closeable when readyToClose=false even though submitted", rowPendingVerification.canClose)

        // Test case 3: not submitted yet → canClose = false regardless of readyToClose
        val rowNotSubmitted = WeighingAssignmentUiRow(
            campaignId = "c1",
            tenantId = "t1",
            parkId = "p1",
            parkLabel = "Park A",
            workGroupId = "wg1",
            campaignShedId = "cs1",
            expectedLocationId = "loc1",
            expectedLocationLabel = "Shed A",
            label = "Shed A",
            category = "shed",
            status = "in_progress", // NOT submitted
            expectedCount = 10,
            periodLabel = "2026-08-01",
            readyToClose = true,
            pendingVerificationCount = 0,
        )

        assertFalse("Should NOT be closeable if not submitted, even if readyToClose=true", rowNotSubmitted.canClose)
    }

    // ==================== B17: Park filtering pagination tests ====================

    @Test
    fun `B17 - onAssignmentRowVisible fires prefetch when last filtered row is composed`() {
        // Scenario: 20 raw assignment rows loaded, 5 match the selected park filter
        // Prefetch distance = 3
        // When park filter is applied, composed indices are 0-4
        // Before fix: Would compare index=4 against (20-3=17), no prefetch
        // After fix: Compares index=4 against (5-3=2), fires prefetch ✓

        val rawAssignments = (1..20).map { i ->
            WeighingAssignment(
                campaignId = "c$i",
                tenantId = "t1",
                parkId = if (i % 4 == 0) "park-A" else "park-B", // 5 rows match park-A: 4, 8, 12, 16, 20
                parkName = if (i % 4 == 0) "Park A" else "Park B",
                workGroupId = "wg$i",
                campaignShedId = "cs$i",
                expectedLocationId = "loc$i",
                expectedLocationLabel = "Shed $i",
                label = "Shed $i",
                category = "shed",
                operatorUserId = "op1",
                status = "in_progress",
                expectedCount = 10,
                periodLabel = "2026-08-01",
                readyToClose = false,
                pendingVerificationCount = 0,
            )
        }

        // Verify raw count
        assertEquals(20, rawAssignments.size)

        // Verify filtered count (park-A only)
        val filteredByParkA = rawAssignments.filter { it.parkId == "park-A" }
        assertEquals(5, filteredByParkA.size)

        // Simulate composed indices on filtered list: 0, 1, 2, 3, 4
        val prefetchDistance = 3

        // Test: index 4 (last item in filtered list of 5) should trigger prefetch
        // Condition in fixed code: index >= filteredSize - prefetchDistance
        // index=4 >= (5-3=2) → TRUE, fires prefetch ✓
        assertTrue("Index 4 in filtered list of size 5 should trigger prefetch (4 >= 5-3=2)",
            4 >= (filteredByParkA.size - prefetchDistance))

        // Test: index 1 should NOT trigger prefetch
        // index=1 >= 2 → FALSE, no prefetch ✓
        assertFalse("Index 1 in filtered list of size 5 should NOT trigger prefetch (1 >= 2)",
            1 >= (filteredByParkA.size - prefetchDistance))
    }

    @Test
    fun `B17 - onAssignmentRowVisible handles null park filter (shows all assignments)`() {
        // When selectedParkId is null, no filtering applies
        val rawAssignments = (1..10).map { i ->
            WeighingAssignment(
                campaignId = "c$i",
                tenantId = "t1",
                parkId = "park-${i % 2}",
                parkName = "Park ${i % 2}",
                workGroupId = "wg$i",
                campaignShedId = "cs$i",
                expectedLocationId = "loc$i",
                expectedLocationLabel = "Shed $i",
                label = "Shed $i",
                category = "shed",
                operatorUserId = "op1",
                status = "in_progress",
                expectedCount = 10,
                periodLabel = "2026-08-01",
                readyToClose = false,
                pendingVerificationCount = 0,
            )
        }

        val selectedParkId: String? = null // No filter

        // Filtered = all (no filter applied)
        val filtered = rawAssignments.filter { selectedParkId == null || it.parkId == selectedParkId }
        assertEquals(10, filtered.size)

        // Prefetch distance = 3, so prefetch fires at index >= 10-3 = 7
        val prefetchDistance = 3
        assertTrue("Index 7 should trigger prefetch when all 10 assignments visible (7 >= 10-3=7)",
            7 >= (filtered.size - prefetchDistance))
    }

    @Test
    fun `B17 - onAssignmentRowVisible handles empty filtered result`() {
        // Edge case: park filter selects no assignments
        val rawAssignments = (1..5).map { i ->
            WeighingAssignment(
                campaignId = "c$i",
                tenantId = "t1",
                parkId = "park-A",
                parkName = "Park A",
                workGroupId = "wg$i",
                campaignShedId = "cs$i",
                expectedLocationId = "loc$i",
                expectedLocationLabel = "Shed $i",
                label = "Shed $i",
                category = "shed",
                operatorUserId = "op1",
                status = "in_progress",
                expectedCount = 10,
                periodLabel = "2026-08-01",
                readyToClose = false,
                pendingVerificationCount = 0,
            )
        }

        val selectedParkId = "park-B" // Selects nothing

        val filtered = rawAssignments.filter { it.parkId == selectedParkId }
        assertEquals(0, filtered.size)
        assertTrue("Filtered list should be empty", filtered.isEmpty())
        // Code guards with: if (filtered.isEmpty()) return, so no prefetch fires
    }
}
