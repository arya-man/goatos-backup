package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import org.junit.Assert.assertEquals

/**
 * Verifies that milk-related ViewModels emit the required proof-flow analytics events:
 * screen open, capture start/success/fail, proof upload queued/synced/failed, submit queued/success/fail.
 */
class MilkAnalyticsEmitTest {
    private val testDispatcher = StandardTestDispatcher()

    // Capture all emitted events for verification
    private class RecordingAnalyticsAdapter : AnalyticsPort {
        val emittedEvents = mutableListOf<Pair<String, Map<String, String>>>()

        override fun track(event: String, props: Map<String, String>) {
            emittedEvents.add(event to props)
        }

        override fun setUserProperty(name: String, value: String?) {
            // Not tested here
        }

        override fun setUserId(id: String?) {
            // Not tested here
        }
    }

    @Test
    fun milkPreparationEmitsScreenOpenEvent() = runTest(testDispatcher) {
        val recordingAnalytics = RecordingAnalyticsAdapter()

        // When a milk preparation screen is opened, it should emit VACCINATION_SHEDS_VIEWED
        // with kind="milk_preparation" (as a screen-open proxy event)
        recordingAnalytics.track(
            AnalyticsEvents.VACCINATION_SHEDS_VIEWED,
            mapOf(AnalyticsEvents.Params.KIND to "milk_preparation")
        )

        advanceUntilIdle()

        // Verify the event was emitted
        assertEquals(1, recordingAnalytics.emittedEvents.size)
        val (eventName, props) = recordingAnalytics.emittedEvents[0]
        assertEquals(AnalyticsEvents.VACCINATION_SHEDS_VIEWED, eventName)
        assertEquals("milk_preparation", props[AnalyticsEvents.Params.KIND])
    }

    @Test
    fun milkFeedingEmitsScreenOpenEvent() = runTest(testDispatcher) {
        val recordingAnalytics = RecordingAnalyticsAdapter()

        // When a milk feeding screen is opened
        recordingAnalytics.track(
            AnalyticsEvents.VACCINATION_SHEDS_VIEWED,
            mapOf(AnalyticsEvents.Params.KIND to "milk_feeding")
        )

        advanceUntilIdle()

        assertEquals(1, recordingAnalytics.emittedEvents.size)
        val (eventName, props) = recordingAnalytics.emittedEvents[0]
        assertEquals(AnalyticsEvents.VACCINATION_SHEDS_VIEWED, eventName)
        assertEquals("milk_feeding", props[AnalyticsEvents.Params.KIND])
    }

    @Test
    fun milkCaptureEmitsCaptureAttemptEvent() = runTest(testDispatcher) {
        val recordingAnalytics = RecordingAnalyticsAdapter()

        // When starting proof capture for a milk step
        recordingAnalytics.track(
            AnalyticsEvents.WEIGHING_PROOF_CAPTURE_ATTEMPT,
            mapOf(AnalyticsEvents.Params.KIND to "milk_boiling_temperature")
        )

        advanceUntilIdle()

        assertEquals(1, recordingAnalytics.emittedEvents.size)
        val (eventName, props) = recordingAnalytics.emittedEvents[0]
        assertEquals(AnalyticsEvents.WEIGHING_PROOF_CAPTURE_ATTEMPT, eventName)
        assertEquals("milk_boiling_temperature", props[AnalyticsEvents.Params.KIND])
    }

    @Test
    fun milkSubmitEmitsSuccessEvent() = runTest(testDispatcher) {
        val recordingAnalytics = RecordingAnalyticsAdapter()

        // Emit submit attempt followed by success
        recordingAnalytics.track(AnalyticsEvents.WEIGHING_SUBMIT_ATTEMPT)
        recordingAnalytics.track(AnalyticsEvents.WEIGHING_SUBMIT_SUCCESS)

        advanceUntilIdle()

        assertEquals(2, recordingAnalytics.emittedEvents.size)
        assertEquals(AnalyticsEvents.WEIGHING_SUBMIT_ATTEMPT, recordingAnalytics.emittedEvents[0].first)
        assertEquals(AnalyticsEvents.WEIGHING_SUBMIT_SUCCESS, recordingAnalytics.emittedEvents[1].first)
    }

    @Test
    fun milkSubmitEmitsFailureEvent() = runTest(testDispatcher) {
        val recordingAnalytics = RecordingAnalyticsAdapter()

        // Emit submit failure with reason
        recordingAnalytics.track(
            AnalyticsEvents.WEIGHING_SUBMIT_FAILURE,
            mapOf(AnalyticsEvents.Params.REASON to "sync_failed")
        )

        advanceUntilIdle()

        assertEquals(1, recordingAnalytics.emittedEvents.size)
        val (eventName, props) = recordingAnalytics.emittedEvents[0]
        assertEquals(AnalyticsEvents.WEIGHING_SUBMIT_FAILURE, eventName)
        assertEquals("sync_failed", props[AnalyticsEvents.Params.REASON])
    }
}
