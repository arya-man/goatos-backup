package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus

class FeedDistributionSequenceTest {
    @Test
    fun `three proof actions can be reuploaded independently and submit waits for sync`() {
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
                videoStatus = FeedDistributionProofStatus.QUEUED,
                waterVideoStatus = FeedDistributionProofStatus.SYNCED,
                canComplete = false,
            ).submitEnabled,
        )
        assertTrue(
            FeedDistributionUiState(
                feedWeightPhotoCaptured = true,
                videoCaptured = true,
                waterVideoCaptured = true,
                feedWeightPhotoStatus = FeedDistributionProofStatus.SYNCED,
                videoStatus = FeedDistributionProofStatus.SYNCED,
                waterVideoStatus = FeedDistributionProofStatus.SYNCED,
                canComplete = true,
            ).submitEnabled,
        )
    }
}
