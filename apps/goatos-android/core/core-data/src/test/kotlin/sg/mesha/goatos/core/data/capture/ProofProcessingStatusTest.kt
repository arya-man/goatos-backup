package sg.mesha.goatos.core.data.capture

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * HIGH fix regression coverage: PROCESSING_FAILED_AWAITING_RETRY (CaptureRepository's P1 fix —
 * a processed-artifact processing/validation failure that never enqueues an upload) must not
 * fall through [ProofProcessingStatus.fromProcessingState]'s syncStatus=PENDING default and read
 * as "Uploading proof..." forever. It must map to the SAME actionable RECORD_AGAIN status
 * DEAD_LETTER already renders, so every screen keying off [ProofCaptureRow.processingStatus]
 * (e.g. ScanViewModel) surfaces a retry affordance with zero new UI states to wire.
 */
class ProofProcessingStatusTest {

    @Test
    fun `processing-failed-awaiting-retry maps to RECORD_AGAIN even though syncStatus stays PENDING`() {
        val status = ProofProcessingStatus.fromProcessingState(
            processingState = "PROCESSING_FAILED_AWAITING_RETRY",
            syncStatus = CaptureSyncStatus.PENDING,
            uploadOriginal = false,
        )
        assertEquals(ProofProcessingStatus.RECORD_AGAIN, status)
    }

    @Test
    fun `a genuinely still-processing row is unaffected and keeps reading as in-progress`() {
        assertEquals(
            ProofProcessingStatus.PREPARING,
            ProofProcessingStatus.fromProcessingState(
                processingState = "CAPTURED_ORIGINAL",
                syncStatus = CaptureSyncStatus.PENDING,
                uploadOriginal = false,
            ),
        )
        assertEquals(
            ProofProcessingStatus.COMPRESSING,
            ProofProcessingStatus.fromProcessingState(
                processingState = "PROCESSING_MEDIA",
                syncStatus = CaptureSyncStatus.PENDING,
                uploadOriginal = false,
            ),
        )
    }

    @Test
    fun `synced always wins regardless of a stale processingState value`() {
        assertEquals(
            ProofProcessingStatus.UPLOADED,
            ProofProcessingStatus.fromProcessingState(
                processingState = "PROCESSING_FAILED_AWAITING_RETRY",
                syncStatus = CaptureSyncStatus.SYNCED,
                uploadOriginal = false,
            ),
        )
    }
}
