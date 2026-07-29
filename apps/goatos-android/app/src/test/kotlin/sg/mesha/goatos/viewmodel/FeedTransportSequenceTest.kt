package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportResultUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus

class FeedTransportSequenceTest {
    @Test
    fun `submit follows distribution proof gating`() {
        assertFalse(FeedTransportCaptureUiState().submitEnabled)
        assertFalse(FeedTransportCaptureUiState(isCapturing = true, videoCaptured = true).submitEnabled)
        assertTrue(FeedTransportCaptureUiState(videoCaptured = true).submitEnabled)
        assertFalse(
            FeedTransportCaptureUiState(
                videoCaptured = true,
                result = FeedTransportResultUi(FeedTransportSubmitStatus.QUEUED, "Submitted"),
            ).submitEnabled,
        )
    }
}
