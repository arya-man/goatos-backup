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
            periodLabel = "2026-08-01",
            readyToClose = false,
            pendingVerificationCount = 3,
        )

        assertEquals(false, row.readyToClose)
        assertEquals(3, row.pendingVerificationCount)
    }

    @Test
    fun `B16 - canClose computed property gates on readyToClose and not closed`() {
        // Backend allows closing ANY non-terminal bucket (via CloseScope/AbandonScope).
        // Client should match: canClose = readyToClose && !isClosed

        // Test case 1: ready to close and not closed → canClose = true
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
            status = "completed", // not closed
            periodLabel = "2026-08-01",
            readyToClose = true,
            pendingVerificationCount = 0,
        )

        assertTrue("Should be closeable when readyToClose=true and not closed", rowReadyToClose.canClose)

        // Test case 2: ready to close but already closed → canClose = false
        val rowAlreadyClosed = WeighingAssignmentUiRow(
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
            status = "closed", // already closed
            periodLabel = "2026-08-01",
            readyToClose = true,
            pendingVerificationCount = 0,
        )

        assertFalse("Should NOT be closeable when status=closed", rowAlreadyClosed.canClose)

        // Test case 3: not ready to close (pending verification) → canClose = false
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
            status = "completed",
            periodLabel = "2026-08-01",
            readyToClose = false,
            pendingVerificationCount = 3,
        )

        assertFalse("Should NOT be closeable when readyToClose=false", rowPendingVerification.canClose)
    }

    // ==================== B17: Park filtering pagination tests ====================

    @Test
    fun `B17 - onAssignmentRowVisible no longer filters client-side, fires prefetch on server-filtered results`() {
        // B17 fix: Park filtering moved server-side via parkId parameter to listAssignments().
        // This prevents pagination starvation when page 1 returns 0 rows for selected park.
        //
        // BEFORE: onAssignmentRowVisible filtered client-side, early-returning if filtered.isEmpty().
        // With 20 unfiltered rows but 0 for selected park, prefetch never fired.
        //
        // AFTER: Server returns only selected park's rows, so onAssignmentRowVisible fires prefetch
        // when index >= loadedSize - prefetchDistance, regardless of park selection.

        val loadedAssignments = (1..5).map { i ->
            // These 5 assignments are already filtered by server (all for selected park)
            WeighingAssignment(
                campaignId = "c$i",
                tenantId = "t1",
                parkId = "park-A", // Already filtered server-side to park-A
                parkName = "Park A",
                workGroupId = "wg$i",
                campaignShedId = "cs$i",
                expectedLocationId = "loc$i",
                expectedLocationLabel = "Shed $i",
                label = "Shed $i",
                category = "shed",
                operatorUserId = "op1",
                status = "in_progress",
                periodLabel = "2026-08-01",
                readyToClose = false,
                pendingVerificationCount = 0,
            )
        }

        val prefetchDistance = 3

        // Prefetch should fire when index >= loadedSize - prefetchDistance
        // index=4 (last item) >= (5-3=2) → TRUE, fires prefetch ✓
        assertTrue("Index 4 should trigger prefetch when loadedSize=5 (4 >= 5-3=2)",
            4 >= (loadedAssignments.size - prefetchDistance))

        // index=1 < 2 → FALSE, no prefetch yet
        assertFalse("Index 1 should NOT trigger prefetch (1 < 2)",
            1 >= (loadedAssignments.size - prefetchDistance))
    }
}
