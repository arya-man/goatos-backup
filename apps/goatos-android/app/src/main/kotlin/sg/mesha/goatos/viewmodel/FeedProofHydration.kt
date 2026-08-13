package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus

internal fun ProofCaptureRow.previewUri(): String? =
    processedUri?.takeIf { it.isNotBlank() } ?: localUri.takeIf { it.isNotBlank() }

internal fun ProofCaptureRow.toProofStatus(): FeedDistributionProofStatus = when (syncStatus) {
    CaptureSyncStatus.SYNCED -> FeedDistributionProofStatus.SYNCED
    CaptureSyncStatus.IN_FLIGHT -> FeedDistributionProofStatus.UPLOADING
    CaptureSyncStatus.FAILED -> FeedDistributionProofStatus.FAILED
    CaptureSyncStatus.PENDING -> FeedDistributionProofStatus.QUEUED
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
    FeedDistributionProofStatus.FAILED -> lastError ?: failed
}

internal fun FeedDistributionProofStatus.isQueuedForSubmit(): Boolean =
    this == FeedDistributionProofStatus.QUEUED ||
        this == FeedDistributionProofStatus.UPLOADING ||
        this == FeedDistributionProofStatus.SYNCED
