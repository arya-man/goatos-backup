package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.database.capture.ProofProcessingState
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus

internal fun ProofCaptureRow.previewUri(): String? =
    processedUri?.takeIf { it.isNotBlank() } ?: localUri.takeIf { it.isNotBlank() }

/** HIGH fix: a processed-artifact validation/processing failure leaves the row
 *  PROCESSING_FAILED_AWAITING_RETRY with [ProofCaptureRow.syncStatus] STILL `PENDING` — nothing
 *  ever calls `dao.updateStatus` for that state (there is nothing to register: the row is
 *  deliberately never enqueued, see CaptureRepository's P1 fix). Mapping purely off [syncStatus]
 *  therefore showed this row as QUEUED forever — indistinguishable from "uploading normally" —
 *  with no operator affordance to notice or recover. Reusing [FeedDistributionProofStatus.FAILED]
 *  here (checked BEFORE the syncStatus branches) puts it through the SAME re-record retry
 *  affordance every screen already renders for a failed upload, with zero new UI states to wire:
 *  a re-record starts a fresh captureReplacingLatest attempt, sidestepping the stuck row entirely. */
internal fun ProofCaptureRow.toProofStatus(): FeedDistributionProofStatus = when {
    processingState == ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name -> FeedDistributionProofStatus.FAILED
    syncStatus == CaptureSyncStatus.SYNCED -> FeedDistributionProofStatus.SYNCED
    syncStatus == CaptureSyncStatus.IN_FLIGHT -> FeedDistributionProofStatus.UPLOADING
    syncStatus == CaptureSyncStatus.FAILED -> FeedDistributionProofStatus.FAILED
    else -> FeedDistributionProofStatus.QUEUED
}

internal fun ProofCaptureRow.toProofMessage(
    status: FeedDistributionProofStatus,
    queued: String,
    uploading: String,
    synced: String,
    failed: String,
): String? = when (status) {
    FeedDistributionProofStatus.EMPTY -> null
    FeedDistributionProofStatus.QUEUED -> queued
    FeedDistributionProofStatus.UPLOADING -> uploading
    FeedDistributionProofStatus.SYNCED -> synced
    FeedDistributionProofStatus.FAILED ->
        lastError ?: processingFailedMessage() ?: failed
}

/** A stuck-processing row has no `lastError` (that column is only ever written alongside an
 *  outbox `syncStatus` transition, which this row never reaches) — fall back to a message
 *  specific enough that "tap to re-record" reads as an action, not a mystery. */
private fun ProofCaptureRow.processingFailedMessage(): String? =
    "Processing failed for this video. Record it again.".takeIf {
        processingState == ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name
    }

internal fun FeedDistributionProofStatus.isQueuedForSubmit(): Boolean =
    this == FeedDistributionProofStatus.QUEUED ||
        this == FeedDistributionProofStatus.UPLOADING ||
        this == FeedDistributionProofStatus.SYNCED
