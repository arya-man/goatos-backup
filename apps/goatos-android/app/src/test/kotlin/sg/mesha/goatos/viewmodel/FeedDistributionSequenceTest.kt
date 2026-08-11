package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedDistributionResultUi
import sg.mesha.goatos.feature.feed.FeedDistributionStatus
import sg.mesha.goatos.feature.feed.FeedDistributionUiState

/**
 * The feed-distribution capture ORDER (maintainer decision 2026-08-11): weight photo -> feed video
 * -> water video, each step unlocking the next.
 *
 * The order is not cosmetic. The weight photo can only be taken while the feed is still on the
 * scale, so a screen that let the operator shoot it last would be asking for a staged photo of a
 * scale that no longer holds this pen's feed.
 */
class FeedDistributionSequenceTest {

    @Test
    fun `weight photo is the first step and needs nothing before it`() {
        assertTrue(FeedDistributionUiState().weightCaptureEnabled)
        // Already taken -> the button is replaced by the captured state, so the action closes.
        assertFalse(FeedDistributionUiState(weightCaptured = true).weightCaptureEnabled)
    }

    @Test
    fun `feed video enables only after the weight photo is captured`() {
        assertFalse(FeedDistributionUiState(weightCaptured = false).videoCaptureEnabled)
        assertTrue(FeedDistributionUiState(weightCaptured = true).videoCaptureEnabled)
    }

    @Test
    fun `water video enables only after the feed video is captured`() {
        assertFalse(
            FeedDistributionUiState(weightCaptured = true, videoCaptured = false).waterCaptureEnabled,
        )
        assertTrue(
            FeedDistributionUiState(weightCaptured = true, videoCaptured = true).waterCaptureEnabled,
        )
    }

    @Test
    fun `no step is actionable while another capture is in flight`() {
        assertFalse(FeedDistributionUiState(isCapturingWeight = true).weightCaptureEnabled)
        assertFalse(
            FeedDistributionUiState(weightCaptured = true, isCapturingWeight = true).videoCaptureEnabled,
        )
        assertFalse(
            FeedDistributionUiState(
                weightCaptured = true,
                videoCaptured = true,
                isCapturingVideo = true,
            ).waterCaptureEnabled,
        )
    }

    @Test
    fun `submit needs all three proofs`() {
        val allThree = FeedDistributionUiState(
            canComplete = true,
            weightCaptured = true,
            videoCaptured = true,
            waterCaptured = true,
        )
        assertTrue(allThree.submitEnabled)
        // Dropping ANY one of the three closes submit. The weight case is the one that would pass
        // silently if the new step were added to the screen but left out of the gate.
        assertFalse(allThree.copy(weightCaptured = false).submitEnabled)
        assertFalse(allThree.copy(videoCaptured = false).submitEnabled)
        assertFalse(allThree.copy(waterCaptured = false).submitEnabled)
    }

    @Test
    fun `an already-submitted session offers no capture at all`() {
        val submitted = FeedDistributionUiState(alreadySubmitted = true)
        assertFalse(submitted.weightCaptureEnabled)
        assertFalse(submitted.videoCaptureEnabled)
        assertFalse(submitted.waterCaptureEnabled)
    }

    @Test
    fun `a committed write closes every capture and submit`() {
        val committed = FeedDistributionUiState(
            canComplete = true,
            weightCaptured = true,
            videoCaptured = true,
            waterCaptured = true,
            result = FeedDistributionResultUi(FeedDistributionStatus.QUEUED, "queued"),
        )
        assertFalse(committed.submitEnabled)
        assertFalse(committed.copy(weightCaptured = false).weightCaptureEnabled)
    }
}
