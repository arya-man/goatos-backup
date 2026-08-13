package sg.mesha.goatos.core.analytics

import org.junit.Test
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue

/**
 * Unit tests for [AnalyticsFunnels] helpers — tests that each funnel helper builds the
 * correct event name and parameter map without transformation/loss.
 */
class AnalyticsFunnelsTest {

    private val capturedEvents = mutableListOf<Pair<String, Map<String, String>>>()

    private val testAnalytics = object : AnalyticsPort {
        override fun track(event: String, props: Map<String, String>) {
            capturedEvents.add(event to props)
        }

        override fun setUserProperty(name: String, value: String?) {}

        override fun setUserId(id: String?) {}
    }

    @Test
    fun `trackDriveOpened emits correct event and parameters`() {
        AnalyticsFunnels.trackDriveOpened(testAnalytics, driveId = "drive-123", parkId = "park-456")

        assertEquals(1, capturedEvents.size)
        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.DRIVE_OPEN, eventName)
        assertEquals("drive-123", params[AnalyticsFunnels.Params.DRIVE_ID])
        assertEquals("park-456", params[AnalyticsFunnels.Params.PARK_ID])
    }

    @Test
    fun `trackDriveOpened omits parkId when null`() {
        AnalyticsFunnels.trackDriveOpened(testAnalytics, driveId = "drive-789")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.DRIVE_OPEN, eventName)
        assertEquals("drive-789", params[AnalyticsFunnels.Params.DRIVE_ID])
        assertTrue("parkId should not be in params when null", AnalyticsFunnels.Params.PARK_ID !in params)
    }

    @Test
    fun `trackScanStarted emits correct event and shed_id`() {
        AnalyticsFunnels.trackScanStarted(testAnalytics, shedId = "shed-123")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.SCAN_STARTED, eventName)
        assertEquals("shed-123", params[AnalyticsFunnels.Params.SHED_ID])
    }

    @Test
    fun `trackScanCompleted emits correct event, shed_id, and scanned_count`() {
        AnalyticsFunnels.trackScanCompleted(testAnalytics, shedId = "shed-456", scannedCount = 42)

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.SCAN_COMPLETED, eventName)
        assertEquals("shed-456", params[AnalyticsFunnels.Params.SHED_ID])
        assertEquals("42", params[AnalyticsFunnels.Params.SCANNED_COUNT])
    }

    @Test
    fun `trackVaccinationCaptureStarted emits correct event and task_id`() {
        AnalyticsFunnels.trackVaccinationCaptureStarted(testAnalytics, taskId = "task-789")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.VACCINATION_CAPTURE_STARTED, eventName)
        assertEquals("task-789", params[AnalyticsFunnels.Params.TASK_ID])
    }

    @Test
    fun `trackVaccinationCaptureCompleted emits correct event, task_id, and outcome`() {
        AnalyticsFunnels.trackVaccinationCaptureCompleted(testAnalytics, taskId = "task-999", outcome = "done")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.VACCINATION_CAPTURE_COMPLETED, eventName)
        assertEquals("task-999", params[AnalyticsFunnels.Params.TASK_ID])
        assertEquals("done", params[AnalyticsFunnels.Params.OUTCOME])
    }

    @Test
    fun `trackSubmitAttempted emits correct event and task_id`() {
        AnalyticsFunnels.trackSubmitAttempted(testAnalytics, taskId = "task-submit-1")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.SUBMIT_ATTEMPTED, eventName)
        assertEquals("task-submit-1", params[AnalyticsFunnels.Params.TASK_ID])
    }

    @Test
    fun `trackSubmitSucceeded emits correct event and task_id`() {
        AnalyticsFunnels.trackSubmitSucceeded(testAnalytics, taskId = "task-success")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.SUBMIT_SUCCEEDED, eventName)
        assertEquals("task-success", params[AnalyticsFunnels.Params.TASK_ID])
    }

    @Test
    fun `trackSubmitFailed emits correct event, task_id, and reason`() {
        AnalyticsFunnels.trackSubmitFailed(testAnalytics, taskId = "task-fail", reason = "network_error")

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.SUBMIT_FAILED, eventName)
        assertEquals("task-fail", params[AnalyticsFunnels.Params.TASK_ID])
        assertEquals("network_error", params[AnalyticsFunnels.Params.REASON])
    }

    @Test
    fun `trackSubmitStatus emits compact status reason and attempt counters`() {
        AnalyticsFunnels.trackSubmitStatus(
            testAnalytics,
            taskId = "task-stuck",
            status = "retrying",
            reason = "stale_scan_roster",
            attemptCount = 2,
            maxAttempts = 5,
        )

        val (eventName, params) = capturedEvents[0]
        assertEquals(AnalyticsFunnels.Events.SUBMIT_STATUS, eventName)
        assertEquals("task-stuck", params[AnalyticsFunnels.Params.TASK_ID])
        assertEquals("retrying", params[AnalyticsFunnels.Params.SUBMIT_STATUS])
        assertEquals("stale_scan_roster", params[AnalyticsFunnels.Params.REASON])
        assertEquals("2", params[AnalyticsFunnels.Params.ATTEMPT_COUNT])
        assertEquals("5", params[AnalyticsFunnels.Params.MAX_ATTEMPTS])
    }

    @Test
    fun `funnel event constants match original event names`() {
        // Verify that the aliased constants still point to the original AnalyticsEvents
        assertEquals(AnalyticsEvents.LOGIN_ATTEMPT, AnalyticsFunnels.Events.LOGIN_ATTEMPT)
        assertEquals(AnalyticsEvents.LOGIN_SUCCESS, AnalyticsFunnels.Events.LOGIN_SUCCESS)
        assertEquals(AnalyticsEvents.LOGIN_FAILURE, AnalyticsFunnels.Events.LOGIN_FAILURE)
        assertEquals(AnalyticsEvents.BOOTSTRAP_LOADED, AnalyticsFunnels.Events.BOOTSTRAP_LOADED)
    }
}
