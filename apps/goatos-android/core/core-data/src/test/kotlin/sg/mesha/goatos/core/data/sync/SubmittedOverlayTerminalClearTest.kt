package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteRequestDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto

/**
 * The optimistic "In review" badge must not outlive the write it stands for.
 *
 * `markSubmittedForReview` is called the moment a submit is ENQUEUED, so the list stops showing
 * "Pending" for sent work (the 254.mp4 bug). But an outbox row can still die: the server can reject
 * it outright (conflict) or the attempts can run out. Before this, nothing removed the key on that
 * path, so the row kept claiming "In review" for work that never landed — until logout or the next
 * business day. That is strictly WORSE than the stale "Pending" the overlay was added to fix,
 * because the operator stops chasing it.
 *
 * A RETRYABLE failure must NOT clear: that row is still going to be sent, and flapping the chip
 * back to Pending mid-backoff would be its own defect.
 */
class SubmittedOverlayTerminalClearTest {

    private val shedId = "shed-castro"
    private val partition = "2"
    private val sessionNo = 1
    private val workflow = "experiment"

    private fun packingKey() = FeedCompletionLocalStore.key(shedId, partition, sessionNo, workflow)

    private fun queuedPackingComplete(maxAttempts: Int = 8) = OutboxEntity(
        id = "pack-row",
        opType = OutboxOpType.FEED_PACKING_COMPLETE.name,
        groupKey = "feed-pack-$shedId",
        idempotencyKey = "pack-key-1",
        payloadJson = syncJson.encodeToString(
            FeedPackingCompletePayload(
                parkId = "park-1",
                shedId = shedId,
                partitionLabel = partition,
                sessionNo = sessionNo,
                targetDate = "2026-08-17",
                workflow = workflow,
                packingProofOutboxItemId = "proof-item-1",
            ),
        ),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = maxAttempts,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = null,
    )

    /**
     * A SUCCEEDED PROOF_UPLOAD row for the packing video. Without it `resolveUploadedProofRef`
     * throws NonRetryableSyncException and the row terminalizes on the MISSING COUPLING before the
     * API is ever called — which would make a "retryable" test silently exercise the terminal path.
     */
    private fun uploadedProof() = OutboxEntity(
        id = "proof-item-1",
        opType = OutboxOpType.PROOF_UPLOAD.name,
        groupKey = "feed-pack-$shedId",
        idempotencyKey = "proof-key-1",
        payloadJson = "{}",
        status = OutboxStatus.SUCCEEDED.name,
        attemptCount = 1,
        maxAttempts = 8,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = syncJson.encodeToString(
            ProofUploadResponseDto(proof = ProofReferenceDto(proofId = "proof-abc")),
        ),
    )

    /** An API whose packing-complete always fails with [error]. */
    private fun failingApi(error: Throwable) = object : AppApi by FakeAppApi() {
        override suspend fun completeFeedPacking(
            idempotencyKey: String,
            request: FeedPackingCompleteRequestDto,
        ): FeedPackingCompleteResponseDto = throw error
    }

    @Test
    fun `a REJECTED submit clears the optimistic In review badge`() = runBlocking {
        val overlay = FeedCompletionLocalStore()
        overlay.markSubmittedForReview(packingKey())
        assertTrue("precondition: badge is showing", overlay.submittedForReviewKeys.value.contains(packingKey()))

        val store = FakeOutboxStore()
        store.insert(queuedPackingComplete())

        SyncEngine(
            store = store,
            api = failingApi(NonRetryableSyncException("server rejected this packing")),
            connectivityGate = { true },
            clock = { 0L },
            feedCompletionStore = overlay,
        ).drainOnce()

        val row = store.findById("pack-row")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue("a rejection is terminal", row.conflict)
        assertFalse(
            "the badge must be gone once the write is dead — otherwise the list lies about sent work",
            overlay.submittedForReviewKeys.value.contains(packingKey()),
        )
    }

    @Test
    fun `an EXHAUSTED submit clears the optimistic In review badge`() = runBlocking {
        val overlay = FeedCompletionLocalStore()
        overlay.markSubmittedForReview(packingKey())

        val store = FakeOutboxStore()
        // maxAttempts = 1 so the first plain (retryable) failure is already the last one.
        store.insert(queuedPackingComplete(maxAttempts = 1))

        SyncEngine(
            store = store,
            api = failingApi(RuntimeException("network died")),
            connectivityGate = { true },
            clock = { 0L },
            feedCompletionStore = overlay,
        ).drainOnce()

        assertEquals(OutboxStatus.FAILED.name, store.findById("pack-row")!!.status)
        assertFalse(
            "attempts exhausted is just as dead as a rejection",
            overlay.submittedForReviewKeys.value.contains(packingKey()),
        )
    }

    @Test
    fun `a RETRYABLE failure KEEPS the badge — that row is still going to be sent`() = runBlocking {
        val overlay = FeedCompletionLocalStore()
        overlay.markSubmittedForReview(packingKey())

        val store = FakeOutboxStore()
        store.insert(uploadedProof())
        store.insert(queuedPackingComplete(maxAttempts = 8))

        SyncEngine(
            store = store,
            api = failingApi(RuntimeException("transient network blip")),
            connectivityGate = { true },
            clock = { 0L },
            feedCompletionStore = overlay,
        ).drainOnce()

        val row = store.findById("pack-row")!!
        assertFalse("still retryable, not terminal", row.conflict)
        assertTrue(
            "mid-backoff the write is alive, so the badge must stay — flapping it would be its own bug",
            overlay.submittedForReviewKeys.value.contains(packingKey()),
        )
    }

    @Test
    fun `the cleared key is the SAME key the submit ViewModel marked, for every overlaid opType`() {
        val engine = SyncEngine(
            store = FakeOutboxStore(),
            api = FakeAppApi(),
            connectivityGate = { true },
            clock = { 0L },
        )

        // A mismatch here strands the badge forever, so pin each mapping to the production key.
        assertEquals(
            FeedCompletionLocalStore.key(shedId, partition, sessionNo, workflow),
            engine.submittedOverlayKeyOf(queuedPackingComplete()),
        )

        // sessionNo 0 is the pen-day build's "no session"; SyncEngine dispatches it as session 1,
        // so the cleared key must normalise the same way or it will never match.
        val legacy = queuedPackingComplete().copy(
            payloadJson = syncJson.encodeToString(
                FeedPackingCompletePayload(
                    parkId = "park-1",
                    shedId = shedId,
                    partitionLabel = partition,
                    sessionNo = 0,
                    targetDate = "2026-08-17",
                    workflow = workflow,
                    packingProofOutboxItemId = "proof-item-1",
                ),
            ),
        )
        assertEquals(
            FeedCompletionLocalStore.key(shedId, partition, 1, workflow),
            engine.submittedOverlayKeyOf(legacy),
        )
    }

    @Test
    fun `an opType with no list overlay maps to null rather than clearing something random`() {
        val engine = SyncEngine(
            store = FakeOutboxStore(),
            api = FakeAppApi(),
            connectivityGate = { true },
            clock = { 0L },
        )
        val unrelated = queuedPackingComplete().copy(opType = OutboxOpType.SCAN_CAPTURE.name)
        assertEquals(null, engine.submittedOverlayKeyOf(unrelated))
    }

    @Test
    fun `MIDNIGHT - a submit marked yesterday still retracts when it dies today`() {
        val overlay = FeedCompletionLocalStore()
        // What markSubmittedForReview produced at 23:50 on the PREVIOUS business day: same grain
        // identity, different leading date segment.
        val yesterdayKey = "2026-08-16|$shedId|${partition.lowercase()}|$sessionNo|$workflow"
        overlay.markSubmittedForReview(yesterdayKey)

        // What SyncEngine builds when the row terminalises after midnight.
        overlay.clearSubmittedForReview("2026-08-17|$shedId|${partition.lowercase()}|$sessionNo|$workflow")

        assertFalse(
            "a midnight crossing must not strand the badge — identity matches, date must not gate it",
            overlay.submittedForReviewKeys.value.contains(yesterdayKey),
        )
    }

    @Test
    fun `PEN_DAY - a sessionNo 0 submit retracts, because the mark side normalises it too`() {
        val overlay = FeedCompletionLocalStore()
        // The pen-day route supplies sessionNo = 0; the ViewModel now normalises to 1 when marking.
        val marked = FeedCompletionLocalStore.key(shedId, partition, 0.takeIf { it != 0 } ?: 1, workflow)
        overlay.markSubmittedForReview(marked)

        // SyncEngine normalises identically from the payload's 0.
        overlay.clearSubmittedForReview(FeedCompletionLocalStore.key(shedId, partition, 1, workflow))

        assertFalse(
            "raw 0 vs dispatched 1 was the CRITICAL mismatch — both sides must normalise",
            overlay.submittedForReviewKeys.value.contains(marked),
        )
    }

    @Test
    fun `PARTITION - null, blank and whitespace labels all resolve to the same grain`() {
        val whole = FeedCompletionLocalStore.key(shedId, null, sessionNo, workflow)
        assertEquals("blank is the whole shed", whole, FeedCompletionLocalStore.key(shedId, "", sessionNo, workflow))
        assertEquals("whitespace is the whole shed", whole, FeedCompletionLocalStore.key(shedId, "   ", sessionNo, workflow))
        assertEquals(
            "case must not split the grain",
            FeedCompletionLocalStore.key(shedId, "Pen A", sessionNo, workflow),
            FeedCompletionLocalStore.key(shedId, "pen a", sessionNo, workflow),
        )

        val overlay = FeedCompletionLocalStore()
        overlay.markSubmittedForReview(whole)
        overlay.clearSubmittedForReview(FeedCompletionLocalStore.key(shedId, "  ", sessionNo, workflow))
        assertFalse("a blank-vs-null divergence would strand the badge", overlay.submittedForReviewKeys.value.contains(whole))
    }

    @Test
    fun `clearing one grain leaves every other grain alone`() {
        val overlay = FeedCompletionLocalStore()
        val mine = FeedCompletionLocalStore.key(shedId, partition, sessionNo, workflow)
        val other = FeedCompletionLocalStore.key(shedId, partition, sessionNo + 1, workflow)
        overlay.markSubmittedForReview(mine)
        overlay.markSubmittedForReview(other)

        overlay.clearSubmittedForReview(mine)

        assertFalse(overlay.submittedForReviewKeys.value.contains(mine))
        assertTrue("identity match must not be a prefix sweep", overlay.submittedForReviewKeys.value.contains(other))
    }
}
