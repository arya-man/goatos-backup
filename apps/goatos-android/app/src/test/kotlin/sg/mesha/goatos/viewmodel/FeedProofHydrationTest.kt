package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.database.capture.ProofProcessingState
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus

/**
 * HIGH fix state-mapping coverage: a processed-artifact processing/validation failure
 * (CaptureRepository's P1 fix) leaves a row PROCESSING_FAILED_AWAITING_RETRY with
 * [ProofCaptureRow.syncStatus] STILL `PENDING` — nothing enqueues an upload for that state, so
 * nothing ever flips syncStatus away from PENDING. Before this fix [ProofCaptureRow.toProofStatus]
 * mapped purely off syncStatus and showed this row as QUEUED forever, indistinguishable from a
 * normal in-progress upload, with no operator affordance to notice or recover. These tests pin
 * the mapping every feed-distribution/packing/transport screen hydrates from
 * ([FeedDistributionCompleteViewModel], [FeedPackingCompleteViewModel], [FeedTransportViewModel]
 * all call `row.toProofStatus()` in their `hydrateFromProof` path).
 */
class FeedProofHydrationTest {

    @Test
    fun `stuck processing-failed row maps to FAILED even though syncStatus is still PENDING`() {
        val row = proofRow(
            syncStatus = CaptureSyncStatus.PENDING,
            processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
        )

        assertEquals(FeedDistributionProofStatus.FAILED, row.toProofStatus())
    }

    @Test
    fun `stuck processing-failed status is not queued for submit -- it must block, not silently pass`() {
        val status = proofRow(
            syncStatus = CaptureSyncStatus.PENDING,
            processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
        ).toProofStatus()

        assertEquals(false, status.isQueuedForSubmit())
    }

    @Test
    fun `stuck processing-failed row gets an actionable message even with no lastError set`() {
        val row = proofRow(
            syncStatus = CaptureSyncStatus.PENDING,
            processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
            lastError = null,
        )
        val status = row.toProofStatus()

        val message = row.toProofMessage(
            status,
            queued = "Video saved on this phone. It will upload automatically.",
            uploading = "Video upload is in progress.",
            synced = "Video is ready.",
            failed = "Video could not be queued",
        )

        assertNotNull("Must not be null -- the retry button needs SOME label context", message)
        assertEquals(true, message!!.contains("again", ignoreCase = true))
    }

    @Test
    fun `a genuinely still-processing row keeps reading as QUEUED, not FAILED`() {
        val row = proofRow(
            syncStatus = CaptureSyncStatus.PENDING,
            processingState = ProofProcessingState.PROCESSING_MEDIA.name,
        )

        assertEquals(FeedDistributionProofStatus.QUEUED, row.toProofStatus())
    }

    @Test
    fun `normal syncStatus-driven states are unaffected by the processingState check`() {
        assertEquals(
            FeedDistributionProofStatus.SYNCED,
            proofRow(syncStatus = CaptureSyncStatus.SYNCED, processingState = "UPLOAD_CONFIRMED").toProofStatus(),
        )
        assertEquals(
            FeedDistributionProofStatus.UPLOADING,
            proofRow(syncStatus = CaptureSyncStatus.IN_FLIGHT, processingState = "UPLOADING").toProofStatus(),
        )
        assertEquals(
            FeedDistributionProofStatus.FAILED,
            proofRow(syncStatus = CaptureSyncStatus.FAILED, processingState = "DEAD_LETTER").toProofStatus(),
        )
        assertEquals(
            FeedDistributionProofStatus.QUEUED,
            proofRow(syncStatus = CaptureSyncStatus.PENDING, processingState = "CAPTURED_ORIGINAL").toProofStatus(),
        )
    }

    private fun proofRow(
        syncStatus: CaptureSyncStatus,
        processingState: String,
        lastError: String? = null,
    ) = ProofCaptureRow(
        id = "proof-1",
        fieldKey = "feed_distribution_video",
        proofSubject = ProofSubject.SHED,
        subjectId = "shed-1",
        localUri = "file:///video.mp4",
        mimeType = "video/mp4",
        caption = null,
        capturedAtMs = 1_000L,
        capturedStartMs = 1_000L,
        capturedEndMs = 2_000L,
        capturedByPrincipalId = "operator-1",
        syncStatus = syncStatus,
        serverProofId = null,
        outboxItemId = null,
        lastError = lastError,
        processingState = processingState,
    )
}
