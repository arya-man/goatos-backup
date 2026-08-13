package sg.mesha.goatos.feature.weighing

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * Verifies that assignment row copy (summary subtitle and action label) renders
 * mode appropriately: per-animal RFID scanning for individual mode, but shed video
 * and lump-sum weight for per_shed_partition mode. This test prevents regression
 * where lumpsum buckets were incorrectly showing individual-mode copy.
 *
 * telemetry:exempt pure data class property test covering bug fix BUG-LUMPSUM-UI.
 */
class WeighingAssignmentModeAwarenessTest {

    private fun individualRow() = WeighingAssignmentUiRow(
        campaignId = "c1",
        tenantId = "t1",
        parkId = "p1",
        parkLabel = "Park A",
        workGroupId = "wg1",
        campaignShedId = "cs1",
        expectedLocationId = "loc1",
        expectedLocationLabel = "Shed 1A",
        label = "Individual weighing",
        category = "individual",
        operatorName = "Operator A",
        status = "in_progress",
        periodLabel = "Week 1",
    )

    private fun lumpsumRow() = WeighingAssignmentUiRow(
        campaignId = "c1",
        tenantId = "t1",
        parkId = "p1",
        parkLabel = "Park A",
        workGroupId = "wg1",
        campaignShedId = "cs2",
        expectedLocationId = "loc2",
        expectedLocationLabel = "Shed 1B",
        label = "Lumpsum weighing",
        category = "per_shed_partition",
        operatorName = "Operator B",
        status = "in_progress",
        periodLabel = "Week 1",
    )

    @Test
    fun `individual mode row detects category correctly`() {
        val row = individualRow()
        assertEquals(false, row.category.equals("per_shed_partition", ignoreCase = true))
    }

    @Test
    fun `lumpsum mode row detects category correctly`() {
        val row = lumpsumRow()
        assertEquals(true, row.category.equals("per_shed_partition", ignoreCase = true))
    }

    @Test
    fun `individual row should use individual summary string resource`() {
        val row = individualRow()
        val resId = assignmentSummaryRes(row)
        assertEquals(R.string.weighing_assignment_summary_individual, resId)
    }

    @Test
    fun `lumpsum row should use lumpsum summary string resource, not individual`() {
        val row = lumpsumRow()
        val resId = assignmentSummaryRes(row)
        assertEquals(R.string.weighing_assignment_summary_lumpsum, resId)
        assertNotEquals(R.string.weighing_assignment_summary_individual, resId)
    }

    @Test
    fun `individual row should use scan animals action string resource`() {
        val row = individualRow()
        val resId = assignmentActionRes(row)
        assertEquals(R.string.weighing_action_scan_animals, resId)
    }

    @Test
    fun `lumpsum row should use record shed action string resource, not scan animals`() {
        val row = lumpsumRow()
        val resId = assignmentActionRes(row)
        assertEquals(R.string.weighing_action_record_lumpsum, resId)
        assertNotEquals(R.string.weighing_action_scan_animals, resId)
    }

    @Test
    fun `case insensitive category comparison handles per_shed_partition variants`() {
        val uppercase = WeighingAssignmentUiRow(
            campaignId = "c1", tenantId = "t1", parkId = "p1", parkLabel = "P",
            workGroupId = "wg1", campaignShedId = "cs1", expectedLocationId = "loc1",
            expectedLocationLabel = "L", label = "Test", category = "PER_SHED_PARTITION",
            operatorName = "Op", status = "in_progress", periodLabel = "W1"
        )
        assertEquals(true, uppercase.category.equals("per_shed_partition", ignoreCase = true))

        val mixedCase = uppercase.copy(category = "Per_Shed_Partition")
        assertEquals(true, mixedCase.category.equals("per_shed_partition", ignoreCase = true))
    }

    @Test
    fun `delayed backlog row reports delayed instead of today's due date state`() {
        val row = individualRow().copy(
            backendStatus = "pending",
            plannedBusinessDate = "2026-07-30",
            dueBusinessDate = "2026-08-11",
        )

        assertEquals(true, row.isDelayedBacklog)
        assertEquals("delayed", row.rawStatus)
    }

    @Test
    fun `submitted delayed assignment row stays locked for verification`() {
        val row = individualRow().copy(
            backendStatus = "completed",
            plannedBusinessDate = "2026-07-30",
            dueBusinessDate = "2026-08-11",
        )

        assertEquals(true, row.isDelayedBacklog)
        assertEquals("completed", row.rawStatus)
        assertEquals(true, row.isSubmittedAndWaitingVerification)
        assertEquals(false, row.isClickable)
    }

    @Test
    fun `sent back submitted assignment row can reopen execution`() {
        val row = individualRow().copy(
            backendStatus = "completed",
            reworkCount = 1,
            plannedBusinessDate = "2026-07-30",
            dueBusinessDate = "2026-08-11",
        )

        assertEquals(false, row.isSubmittedAndWaitingVerification)
        assertEquals(true, row.isClickable)
    }

    @Test
    fun `submitted task detail bucket cannot reopen operator capture unless sent back`() {
        val submitted = taskShedRow(status = "completed", reworked = false)
        assertEquals(false, submitted.canOpenExecution)

        val sentBack = submitted.copy(reworked = true)
        assertEquals(true, sentBack.canOpenExecution)

        val open = taskShedRow(status = "in_progress", reworked = false)
        assertEquals(true, open.canOpenExecution)
    }

    private fun taskShedRow(status: String, reworked: Boolean) = WeighingTaskShedUiRow(
        campaignId = "campaign-1",
        campaignShedId = "campaign-shed-1",
        tenantId = "tenant-1",
        locationId = "location-1",
        shedName = "Castro 1",
        category = "individual",
        operatorLabel = "Amit",
        status = status,
        reworked = reworked,
        animalsWeighedCount = 1,
        animalsSubmittedCount = if (status == "completed") 1 else 0,
        ladderStep = if (status == "completed") 2 else 1,
        canReopen = status == "completed" || reworked,
    )
}
