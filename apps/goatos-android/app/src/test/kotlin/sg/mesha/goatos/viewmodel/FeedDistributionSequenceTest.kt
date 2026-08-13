package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus

class FeedDistributionSequenceTest {
    @Test
    fun `three proof actions can be reuploaded independently and submit only waits for queued proof uploads`() {
        assertTrue(FeedDistributionUiState().feedWeightPhotoCaptureEnabled)
        assertTrue(FeedDistributionUiState().waterVideoCaptureEnabled)
        assertTrue(FeedDistributionUiState(videoCaptured = true, isCapturingVideo = true).waterVideoCaptureEnabled)
        assertTrue(FeedDistributionUiState(feedWeightPhotoCaptured = true).feedWeightPhotoCaptureEnabled)
        assertTrue(FeedDistributionUiState(waterVideoCaptured = true).waterVideoCaptureEnabled)
        assertFalse(
            FeedDistributionUiState(
                feedWeightPhotoCaptured = true,
                videoCaptured = true,
                waterVideoCaptured = false,
                canComplete = true,
            ).submitEnabled,
        )
        assertFalse(
            FeedDistributionUiState(
                feedWeightPhotoCaptured = true,
                videoCaptured = true,
                waterVideoCaptured = true,
                feedWeightPhotoStatus = FeedDistributionProofStatus.SYNCED,
                videoStatus = FeedDistributionProofStatus.FAILED,
                waterVideoStatus = FeedDistributionProofStatus.SYNCED,
                canComplete = true,
            ).submitEnabled,
        )
        assertTrue(
            FeedDistributionUiState(
                feedWeightPhotoCaptured = true,
                videoCaptured = true,
                waterVideoCaptured = true,
                feedWeightPhotoStatus = FeedDistributionProofStatus.QUEUED,
                videoStatus = FeedDistributionProofStatus.UPLOADING,
                waterVideoStatus = FeedDistributionProofStatus.SYNCED,
                canComplete = true,
            ).submitEnabled,
        )
    }
}
