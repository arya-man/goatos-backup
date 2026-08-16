package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.FeedCompletionLocalStore

/**
 * Feed Direction / Feed Distribution share one worklist, and its chip renders `lifecycleStatus`.
 * Before this fix the ViewModel overlaid only the `completed` boolean, so a queued Direction or
 * Distribution submit left the row reading "Pending" — the same defect proven on device for Feed
 * Packing in 254.mp4.
 *
 * These cases call the PRODUCTION rule [overlayFeedLifecycleStatus] and the PRODUCTION grain key
 * [FeedCompletionLocalStore.key]. Nothing here restates the precedence; a test that copies the
 * logic passes even when the overlay is deleted from the ViewModel.
 *
 * Direction rows carry no rework channel, so [overlayFeedLifecycleStatus] is called with a blank
 * reworkReason — mirroring the production call site.
 */
class FeedDirectionSubmittedForReviewOverlayTest {

    private val shed = "shed-castro"

    private fun keyFor(partitionLabel: String?, sessionNo: Int, workflow: String = "experiment") =
        FeedCompletionLocalStore.key(shed, partitionLabel, sessionNo, workflow)

    /** Renders a Direction row the way the list projection does, against a REAL store. */
    private fun renderedStatus(
        backendStatus: String,
        partitionLabel: String?,
        sessionNo: Int,
        store: FeedCompletionLocalStore,
        workflow: String = "experiment",
    ): String = overlayFeedLifecycleStatus(
        lifecycleStatus = backendStatus,
        reworkReason = "",
        isLocallySubmittedForReview = store.submittedForReviewKeys.value
            .contains(keyFor(partitionLabel, sessionNo, workflow)),
    )

    @Test
    fun `REGRESSION - queued Direction submit renders In review while backend still says pending`() {
        val store = FeedCompletionLocalStore()

        assertEquals("pending", renderedStatus("pending", "2", 1, store))

        store.markSubmittedForReview(keyFor("2", 1))

        assertEquals("pending_verification", renderedStatus("pending", "2", 1, store))
    }

    @Test
    fun `GRAIN_SCOPE - Castro 2 does not flip Castro 1 (different partition)`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(keyFor("2", 1))

        assertEquals("pending_verification", renderedStatus("pending", "2", 1, store))
        assertEquals("pending", renderedStatus("pending", "1", 1, store))
    }

    @Test
    fun `GRAIN_SCOPE - Morning does not flip Evening (different session)`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(keyFor("2", 1))

        assertEquals("pending_verification", renderedStatus("pending", "2", 1, store))
        assertEquals("pending", renderedStatus("pending", "2", 2, store))
    }

    @Test
    fun `GRAIN_SCOPE - experiment does not flip normal (different workflow)`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(keyFor("2", 1, "experiment"))

        assertEquals("pending_verification", renderedStatus("pending", "2", 1, store, "experiment"))
        assertEquals("pending", renderedStatus("pending", "2", 1, store, "normal"))
    }

    @Test
    fun `COMPLETED_OVERRIDE - server acceptance is never downgraded by a stale local key`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(keyFor("2", 1))

        assertEquals("completed", renderedStatus("completed", "2", 1, store))
    }

    @Test
    fun `clear() drops the overlay so the next operator sees backend truth`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(keyFor("2", 1))
        assertEquals("pending_verification", renderedStatus("pending", "2", 1, store))

        store.clear()

        assertEquals("pending", renderedStatus("pending", "2", 1, store))
    }
}
