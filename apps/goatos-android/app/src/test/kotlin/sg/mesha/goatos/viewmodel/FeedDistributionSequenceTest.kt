package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedDistributionQuestionUi
import sg.mesha.goatos.feature.feed.FeedDistributionSlotUi
import sg.mesha.goatos.feature.feed.FeedDistributionUiState
import sg.mesha.goatos.feature.feed.FeedSlotCaptureKind

/**
 * Pins the collaboration contract (docs/product/feed-proof-collaboration.md) on the card-driven
 * slot list: slots are independent and parallel, none gates another, and the submit waits only
 * for every COMPULSORY slot to hold a queued/uploading/synced proof (an optional slot never gates).
 * The guard `check-feed-proof-collaboration-guard.mjs` fails if this file is deleted.
 */
class FeedDistributionSequenceTest {
    private fun slot(key: String, captured: Boolean = false, status: FeedDistributionProofStatus = FeedDistributionProofStatus.EMPTY, isCapturing: Boolean = false, required: Boolean = true) =
        FeedDistributionSlotUi(slotKey = key, title = key, captured = captured, status = status, isCapturing = isCapturing, required = required)

    private val weight = "feed_distribution_feed_weight_photo"
    private val feed = "feed_distribution_video"
    private val water = "feed_distribution_water_video"

    @Test
    fun `every slot is enabled by its own state alone`() {
        // Empty slots are all enabled at once — nothing has to come first.
        assertTrue(slot(weight).captureEnabled)
        assertTrue(slot(water).captureEnabled)
        // A sibling mid-capture does not lock this slot.
        val state = FeedDistributionUiState(slots = listOf(slot(weight), slot(feed, captured = true, isCapturing = true), slot(water)))
        assertTrue(state.slot(water)!!.captureEnabled)
        assertTrue(state.slot(weight)!!.captureEnabled)
        // A captured slot can be re-recorded.
        assertTrue(slot(weight, captured = true, status = FeedDistributionProofStatus.SYNCED).captureEnabled)
        // Only its OWN in-flight capture disables a slot.
        assertFalse(slot(water, isCapturing = true).captureEnabled)
    }

    @Test
    fun `submit waits for every compulsory slot and only for queued proof uploads`() {
        assertFalse(
            FeedDistributionUiState(
                slots = listOf(slot(weight, true, FeedDistributionProofStatus.SYNCED), slot(feed, true, FeedDistributionProofStatus.SYNCED), slot(water)),
                canComplete = true,
            ).submitEnabled,
        )
        assertFalse(
            FeedDistributionUiState(
                slots = listOf(
                    slot(weight, true, FeedDistributionProofStatus.SYNCED),
                    slot(feed, true, FeedDistributionProofStatus.FAILED),
                    slot(water, true, FeedDistributionProofStatus.SYNCED),
                ),
                canComplete = true,
            ).submitEnabled,
        )
        assertTrue(
            FeedDistributionUiState(
                slots = listOf(
                    slot(weight, true, FeedDistributionProofStatus.QUEUED),
                    slot(feed, true, FeedDistributionProofStatus.UPLOADING),
                    slot(water, true, FeedDistributionProofStatus.SYNCED),
                ),
                canComplete = true,
            ).submitEnabled,
        )
        // A card with no slots yet (not even the seeded fallback) cannot be submitted.
        assertFalse(FeedDistributionUiState(canComplete = true).submitEnabled)
    }

    @Test
    fun `an optional slot the card added never gates the submit but a captured one must be queued`() {
        val trough = "trough_after_feeding"
        val filled = listOf(
            slot(weight, true, FeedDistributionProofStatus.SYNCED),
            slot(feed, true, FeedDistributionProofStatus.SYNCED),
            slot(water, true, FeedDistributionProofStatus.SYNCED),
        )
        assertTrue(FeedDistributionUiState(slots = filled + slot(trough, required = false), canComplete = true).submitEnabled)
        assertTrue(FeedDistributionUiState(slots = filled + slot(trough, required = false), canComplete = true).compulsorySlotsFilled)
        assertFalse(
            FeedDistributionUiState(slots = filled + slot(trough, true, FeedDistributionProofStatus.FAILED, required = false), canComplete = true).submitEnabled,
        )
        // A compulsory slot the card added gates like the seeded three.
        assertFalse(FeedDistributionUiState(slots = filled + slot(trough, required = true), canComplete = true).submitEnabled)
    }

    @Test
    fun `an either slot accepts a photo or a video`() {
        val either = FeedDistributionSlotUi(slotKey = "trough", title = "Trough", captureKind = FeedSlotCaptureKind.EITHER)
        assertTrue(either.captureEnabled)
        assertTrue(either.copy(captured = true, capturedKind = FeedSlotCaptureKind.PHOTO, status = FeedDistributionProofStatus.QUEUED).readyForSubmit)
        assertTrue(either.copy(captured = true, capturedKind = FeedSlotCaptureKind.VIDEO, status = FeedDistributionProofStatus.QUEUED).readyForSubmit)
    }

    @Test
    fun `required questions gate the submit only when they apply`() {
        val filled = listOf(slot(weight, true, FeedDistributionProofStatus.SYNCED), slot(feed, true, FeedDistributionProofStatus.SYNCED), slot(water, true, FeedDistributionProofStatus.SYNCED))
        val q1 = FeedDistributionQuestionUi(id = "leftover", kind = "choice", title = "Leftover?", required = true, options = listOf("yes" to "Yes", "no" to "No"))
        val q2 = FeedDistributionQuestionUi(id = "how_much", kind = "number", title = "How much", required = true, onlyIfQuestion = "leftover", onlyIfValue = "yes")
        val base = FeedDistributionUiState(slots = filled, questions = listOf(q1, q2), canComplete = true)
        assertFalse(base.submitEnabled)
        assertTrue(base.copy(answers = mapOf("leftover" to "no")).submitEnabled)
        assertFalse(base.copy(answers = mapOf("leftover" to "yes")).submitEnabled)
        assertTrue(base.copy(answers = mapOf("leftover" to "yes", "how_much" to "2.5")).submitEnabled)
    }
}
