package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.FeedCompletionLocalStore

/**
 * Feed Transport and Milk Feeding are TASK-grain (one task id), not shed-session grain, and each
 * renders its own status vocabulary. Both had the 254.mp4 defect: a submit that reached only the
 * outbox left the list showing "Pending" / "Need action" for work the operator had already sent.
 *
 * Every case calls the PRODUCTION rule and the PRODUCTION grain key. Nothing here restates the
 * precedence — a test that copies the logic passes even when the overlay is deleted.
 */
class TaskGrainSubmittedOverlayTest {

    // ---- Feed Transport ----------------------------------------------------------------------

    private fun transportStatus(status: String, store: FeedCompletionLocalStore, taskId: String, rework: String? = null) =
        overlayTransportStatus(
            status = status,
            reworkReason = rework,
            isLocallySubmittedForReview = store.submittedForReviewKeys.value
                .contains(FeedCompletionLocalStore.taskKey("feed-transport", taskId)),
        )

    @Test
    fun `REGRESSION - queued transport submit renders verification_due while backend says due`() {
        val store = FeedCompletionLocalStore()
        assertEquals("due", transportStatus("due", store, "task-1"))

        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("feed-transport", "task-1"))

        assertEquals("verification_due", transportStatus("due", store, "task-1"))
    }

    @Test
    fun `GRAIN_SCOPE - transport submit does not flip another task`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("feed-transport", "task-1"))

        assertEquals("verification_due", transportStatus("due", store, "task-1"))
        assertEquals("due", transportStatus("due", store, "task-2"))
    }

    @Test
    fun `transport - completed and rework are never overlaid`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("feed-transport", "task-1"))

        assertEquals("completed", transportStatus("completed", store, "task-1"))
        assertEquals("due", transportStatus("due", store, "task-1", rework = "Bag count short"))
    }

    // ---- Milk Feeding ------------------------------------------------------------------------

    private fun milkStatus(status: String, store: FeedCompletionLocalStore, taskId: String, rework: String? = null) =
        overlayMilkFeedingStatus(
            verificationStatus = status,
            reworkReason = rework,
            isLocallySubmittedForReview = store.submittedForReviewKeys.value
                .contains(FeedCompletionLocalStore.taskKey("milk-feeding", taskId)),
        )

    @Test
    fun `REGRESSION - queued milk submit renders pending_verification while backend says not_submitted`() {
        val store = FeedCompletionLocalStore()
        assertEquals("not_submitted", milkStatus("not_submitted", store, "task-1"))

        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("milk-feeding", "task-1"))

        assertEquals("pending_verification", milkStatus("not_submitted", store, "task-1"))
    }

    @Test
    fun `GRAIN_SCOPE - milk submit does not flip another task`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("milk-feeding", "task-1"))

        assertEquals("pending_verification", milkStatus("not_submitted", store, "task-1"))
        assertEquals("not_submitted", milkStatus("not_submitted", store, "task-2"))
    }

    @Test
    fun `milk - completed and rework are never overlaid`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("milk-feeding", "task-1"))

        assertEquals("completed", milkStatus("completed", store, "task-1"))
        assertEquals("not_submitted", milkStatus("not_submitted", store, "task-1", rework = "Redo the mixing video"))
    }

    // ---- namespace isolation -----------------------------------------------------------------

    @Test
    fun `NAMESPACE - the same task id in a different flow is a different grain`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("feed-transport", "shared-id"))

        assertEquals("verification_due", transportStatus("due", store, "shared-id"))
        assertEquals(
            "a transport submit must not flip a milk task that happens to share an id",
            "not_submitted",
            milkStatus("not_submitted", store, "shared-id"),
        )
    }

    @Test
    fun `clear() drops task-grain overlays too`() {
        val store = FeedCompletionLocalStore()
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("feed-transport", "task-1"))
        store.markSubmittedForReview(FeedCompletionLocalStore.taskKey("milk-feeding", "task-1"))

        store.clear()

        assertEquals("due", transportStatus("due", store, "task-1"))
        assertEquals("not_submitted", milkStatus("not_submitted", store, "task-1"))
    }
}
