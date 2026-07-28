package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedDistributionUiState

class FeedDistributionSequenceTest {
    @Test
    fun `water proof enables only after feed video is captured`() {
        assertFalse(FeedDistributionUiState(videoCaptured = false).waterCaptureEnabled)
        assertTrue(FeedDistributionUiState(videoCaptured = true).waterCaptureEnabled)
        assertFalse(
            FeedDistributionUiState(videoCaptured = true, isCapturingVideo = true).waterCaptureEnabled,
        )
    }
}
