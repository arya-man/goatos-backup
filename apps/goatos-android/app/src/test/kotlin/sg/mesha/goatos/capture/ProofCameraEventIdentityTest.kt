package sg.mesha.goatos.capture

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort

class ProofCameraEventIdentityTest {
    @Test
    fun photoVideoAndRecreatedLaunchersKeepDistinctRequestIdentities() {
        val video = ProofCameraEventIdentity()
        val photo = ProofCameraEventIdentity()
        val restartedVideo = ProofCameraEventIdentity()
        val analytics = RecordingAnalytics()
        val token = 1L // each launcher can legitimately reuse this local counter
        trackProofCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_REQUESTED, token, null,
            captureRequestId = video.requestId(token))
        trackPhotoCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_REQUESTED, token, PhotoCaptureContext(), photo.requestId(token))
        trackPhotoCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_FINALIZED, token, PhotoCaptureContext(), photo.requestId(token))
        trackProofCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_REQUESTED, token, null,
            captureRequestId = restartedVideo.requestId(token))
        trackProofCameraEvent(analytics, AnalyticsEvents.PROOF_CAMERA_FINALIZED, token, null,
            captureRequestId = restartedVideo.requestId(token))
        val ids = analytics.events.map { it.second.getValue("capture_request_id") }
        assertEquals(ids[1], ids[2])
        assertEquals(ids[3], ids[4])
        assertEquals(3, ids.toSet().size)
        assertNotEquals(video.requestId(1), video.requestId(2))
        assertEquals(setOf("1"), analytics.events.map { it.second.getValue("request_token") }.toSet())
    }

    private class RecordingAnalytics : AnalyticsPort {
        val events = mutableListOf<Pair<String, Map<String, String>>>()
        override fun track(event: String, props: Map<String, String>) { events += event to props }
        override fun setUserProperty(name: String, value: String?) = Unit
        override fun setUserId(id: String?) = Unit
    }
}
