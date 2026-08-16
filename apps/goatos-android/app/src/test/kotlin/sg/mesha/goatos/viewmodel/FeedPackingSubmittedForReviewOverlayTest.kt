package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.network.dto.FeedPackingRowDto

/**
 * Coverage for the Feed Packing lifecycle-status overlay — the ViewModel-layer fix for the
 * queued-but-unsynced submit bug (254.mp4): the operator submits a packing session, returns to the
 * list, and the row still reads "Pending" because the backend page has not seen the outbox write.
 *
 * EVERY case here calls the PRODUCTION rule [overlayFeedLifecycleStatus] and the PRODUCTION grain
 * key [FeedCompletionLocalStore.key]. Nothing in this file re-implements the precedence. An earlier
 * revision of these tests copied the `when {}` into each test body and asserted against its own
 * copy — which passed even with the overlay deleted from the ViewModel. Do not reintroduce that:
 * a test that restates the logic proves only that the test agrees with itself.
 */
class FeedPackingSubmittedForReviewOverlayTest {

    private fun packingRow(
        shedId: String = "shed-1",
        partitionLabel: String? = "Pen A",
        workflow: String = "experiment",
        sessionNo: Int = 1,
        lifecycleStatus: String,
        reworkReason: String? = null,
    ) = FeedPackingRowDto(
        parkId = "park-1",
        parkLabel = "Farm 1",
        shedId = shedId,
        shedLabel = "Shed 1",
        partitionLabel = partitionLabel,
        operationalLocationDisplay = "",
        sessionNo = sessionNo,
        sessionLabel = "Session 1",
        workflow = workflow,
        experimentArm = "",
        headCount = 10,
        items = emptyList(),
        totalKg = "50.0",
        status = "unknown",
        completed = false,
        lifecycleStatus = lifecycleStatus,
        reworkReason = reworkReason.orEmpty(),
        blockedReasons = emptyList(),
    )

    /** The production grain key for a row, exactly as `toRowUi` derives it. */
    private fun keyOf(row: FeedPackingRowDto) =
        FeedCompletionLocalStore.key(row.shedId, row.partitionLabel, row.sessionNo, row.workflow)

    /** Renders [row] the way the list projection does, against a REAL store. */
    private fun renderedStatus(row: FeedPackingRowDto, store: FeedCompletionLocalStore): String =
        overlayFeedLifecycleStatus(
            lifecycleStatus = row.lifecycleStatus,
            reworkReason = row.reworkReason,
            isLocallySubmittedForReview = store.submittedForReviewKeys.value.contains(keyOf(row)),
        )

    @Test
    fun `REGRESSION - backend pending plus local submitted key renders In review`() {
        val store = FeedCompletionLocalStore()
        val row = packingRow(lifecycleStatus = "pending")

        assertEquals("pending", renderedStatus(row, store))

        store.markSubmittedForReview(keyOf(row))

        assertEquals("backend row is untouched", "pending", row.lifecycleStatus)
        assertEquals("pending_verification", renderedStatus(row, store))
    }

    @Test
    fun `GRAIN_SCOPE - submitting Castro 2 does not flip Castro 1 (different partition)`() {
        val store = FeedCompletionLocalStore()
        val castro1 = packingRow(partitionLabel = "1", lifecycleStatus = "pending")
        val castro2 = packingRow(partitionLabel = "2", lifecycleStatus = "pending")

        store.markSubmittedForReview(keyOf(castro2))

        assertEquals("pending_verification", renderedStatus(castro2, store))
        assertEquals("pending", renderedStatus(castro1, store))
    }

    @Test
    fun `GRAIN_SCOPE - submitting Morning does not flip Evening (different session)`() {
        val store = FeedCompletionLocalStore()
        val morning = packingRow(sessionNo = 1, lifecycleStatus = "pending")
        val evening = packingRow(sessionNo = 2, lifecycleStatus = "pending")

        store.markSubmittedForReview(keyOf(morning))

        assertEquals("pending_verification", renderedStatus(morning, store))
        assertEquals("pending", renderedStatus(evening, store))
    }

    @Test
    fun `GRAIN_SCOPE - submitting one shed does not flip another shed`() {
        val store = FeedCompletionLocalStore()
        val shedA = packingRow(shedId = "shed-A", lifecycleStatus = "pending")
        val shedB = packingRow(shedId = "shed-B", lifecycleStatus = "pending")

        store.markSubmittedForReview(keyOf(shedA))

        assertEquals("pending_verification", renderedStatus(shedA, store))
        assertEquals("pending", renderedStatus(shedB, store))
    }

    @Test
    fun `GRAIN_SCOPE - submitting experiment does not flip normal (different workflow)`() {
        val store = FeedCompletionLocalStore()
        val experiment = packingRow(workflow = "experiment", lifecycleStatus = "pending")
        val normal = packingRow(workflow = "normal", lifecycleStatus = "pending")

        store.markSubmittedForReview(keyOf(experiment))

        assertEquals("pending_verification", renderedStatus(experiment, store))
        assertEquals("pending", renderedStatus(normal, store))
    }

    @Test
    fun `REWORK_OVERRIDE - a reworked line stays Pending so the rejection is not masked`() {
        val store = FeedCompletionLocalStore()
        // A reworked line comes back as "pending" WITH a reason — overlaying it would hide the
        // rework the operator has to act on.
        val row = packingRow(lifecycleStatus = "pending", reworkReason = "Bag count short")

        store.markSubmittedForReview(keyOf(row))

        assertEquals("pending", renderedStatus(row, store))
    }

    @Test
    fun `COMPLETED_OVERRIDE - server acceptance is never downgraded by a stale local key`() {
        val store = FeedCompletionLocalStore()
        val row = packingRow(lifecycleStatus = "completed")

        store.markSubmittedForReview(keyOf(row))

        assertEquals("completed", renderedStatus(row, store))
    }

    @Test
    fun `clear() drops the overlay so the next operator sees backend truth`() {
        val store = FeedCompletionLocalStore()
        val row = packingRow(lifecycleStatus = "pending")

        store.markSubmittedForReview(keyOf(row))
        assertEquals("pending_verification", renderedStatus(row, store))

        store.clear()

        assertEquals("pending", renderedStatus(row, store))
    }
}
