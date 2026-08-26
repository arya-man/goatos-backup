package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedDistributionCompleteViewModel (:app), which owns
// every capture/enqueue side effect this screen triggers via events.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.media3.common.util.UnstableApi
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * Feed-DISTRIBUTION completion detail (L2), reached by tapping a shed-session row on Feed DIRECTION.
 * The operator records a feed weight photo, feed-distribution video, and water-distribution video
 * in any order. Each saved proof shows a preview and can be replaced before final submit. The
 * final verifier-gated completion becomes available only after all three proof uploads sync.
 *
 * This is separate from the packing-proof flow.
 */

enum class FeedDistributionStatus { QUEUED, SYNCED, FAILED }
enum class FeedDistributionProofStatus { EMPTY, QUEUED, UPLOADING, SYNCED, FAILED }

fun FeedDistributionProofStatus.isQueuedForSubmit(): Boolean =
    this == FeedDistributionProofStatus.QUEUED ||
        this == FeedDistributionProofStatus.UPLOADING ||
        this == FeedDistributionProofStatus.SYNCED

@androidx.compose.runtime.Immutable
data class FeedDistributionResultUi(val status: FeedDistributionStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedDistributionUiState(
    val shedLabel: String = "",
    val sessionLabel: String = "",
    val workflowLabel: String = "",
    val isCapturingFeedWeightPhoto: Boolean = false,
    val feedWeightPhotoCaptured: Boolean = false,
    val feedWeightPhotoMessage: String? = null,
    val feedWeightPhotoPreviewPath: String? = null,
    val feedWeightPhotoStatus: FeedDistributionProofStatus = FeedDistributionProofStatus.EMPTY,
    val feedWeightPhotoRemoteUrl: String? = null,
    val isCapturingVideo: Boolean = false,
    val videoCaptured: Boolean = false,
    val videoMessage: String? = null,
    val videoPreviewPath: String? = null,
    val videoStatus: FeedDistributionProofStatus = FeedDistributionProofStatus.EMPTY,
    val videoRemoteUrl: String? = null,
    val isCapturingWaterVideo: Boolean = false,
    val waterVideoCaptured: Boolean = false,
    val waterVideoMessage: String? = null,
    val waterVideoPreviewPath: String? = null,
    val waterVideoStatus: FeedDistributionProofStatus = FeedDistributionProofStatus.EMPTY,
    val waterVideoRemoteUrl: String? = null,
    val canComplete: Boolean = false,
    val isSyncing: Boolean = false,
    val result: FeedDistributionResultUi? = null,
    /**
     * The shed-session already went to the verifier (or was approved/rejected) somewhere else, so
     * there is nothing to record here. Backend-owned: derived from the row's lifecycle bucket, NOT
     * from local capture-draft presence — a reinstall wipes the draft, which previously left an
     * empty, fully-editable form for work already submitted (STG 2026-08-09; mirrors
     * [FeedPackingCompleteUiState.alreadySubmitted]).
     */
    val alreadySubmitted: Boolean = false,
) {
    val feedWeightPhotoCaptureEnabled: Boolean
        get() = !isCapturingFeedWeightPhoto && !isFinalSubmitted

    val waterVideoCaptureEnabled: Boolean
        get() = !isCapturingWaterVideo && !isFinalSubmitted

    val videoCaptureEnabled: Boolean
        get() = !isCapturingVideo && !isFinalSubmitted

    val isFinalSubmitted: Boolean
        get() = alreadySubmitted ||
            result?.status == FeedDistributionStatus.SYNCED || result?.status == FeedDistributionStatus.QUEUED

    /** Proof uploads may still be queued; the completion outbox resolves them before syncing. */
    val submitEnabled: Boolean
        get() = canComplete &&
            feedWeightPhotoStatus.isQueuedForSubmit() &&
            videoStatus.isQueuedForSubmit() &&
            waterVideoStatus.isQueuedForSubmit() &&
            !isFinalSubmitted
}

sealed interface FeedDistributionEvent {
    /** Record the feed video with the LIVE in-app camera. */
    data object RecordFeedVideo : FeedDistributionEvent

    data object TakeFeedWeightPhoto : FeedDistributionEvent
    data object RecordWaterVideo : FeedDistributionEvent
    data object MarkDone : FeedDistributionEvent
    data object SyncNow : FeedDistributionEvent
    data object Back : FeedDistributionEvent
}

@Composable
fun FeedDistributionCompleteScreen(
    state: FeedDistributionUiState,
    onEvent: (FeedDistributionEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedDistributionStatus.SYNCED ||
        state.result?.status == FeedDistributionStatus.QUEUED
    val subtitle = listOf(state.sessionLabel, state.workflowLabel)
        .filter { it.isNotBlank() }
        .joinToString(" · ")

    Scaffold(
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.shedLabel.ifBlank { "Feed distribution" },
                eyebrow = "FEED DISTRIBUTION",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = subtitle,
                onBack = { onEvent(FeedDistributionEvent.Back) },
                actions = {
                    SyncIconButton(
                        isSyncing = state.isSyncing,
                        onSync = { onEvent(FeedDistributionEvent.SyncNow) },
                    )
                },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // ALREADY SUBMITTED: the session went to the verifier (or was decided) elsewhere, so
            // there is nothing to record. The captured proofs stay VISIBLE below (read-only —
            // every capture path is gated on isFinalSubmitted): operators still need to see WHAT
            // was submitted and by whom; only the actions disappear.
            if (state.alreadySubmitted) {
                item {
                    FeedDistStatusCardBody(
                        text = stringResource(R.string.feed_dist_session_submitted_body),
                        tone = MeshaColors.Muted,
                    )
                }
            } else {
                item {
                    FeedDistStatusCard(
                        state = state,
                        committed = committed,
                        onRetrySubmit = { onEvent(FeedDistributionEvent.MarkDone) },
                    )
                }
            }
            item {
                FeedDistProofAction(
                    title = stringResource(R.string.feed_dist_take_feed_weight_photo),
                    subtitle = stringResource(R.string.feed_dist_feed_weight_title),
                    icon = MeshaIcons.Plus,
                    captured = state.feedWeightPhotoCaptured,
                    status = state.feedWeightPhotoStatus,
                    previewPath = state.feedWeightPhotoPreviewPath,
                    previewKind = FeedDistPreviewKind.Photo,
                    capturedLabel = proofLabel(state.feedWeightPhotoStatus, stringResource(R.string.feed_dist_feed_weight_photo_captured)),
                    loading = state.isCapturingFeedWeightPhoto,
                    loadingLabel = stringResource(R.string.feed_dist_water_uploading),
                    retryLabel = stringResource(R.string.feed_dist_retry_feed_weight_photo),
                    replaceLabel = stringResource(R.string.feed_proof_recapture),
                    enabled = state.feedWeightPhotoCaptureEnabled,
                    message = state.feedWeightPhotoMessage,
                    onClick = { onEvent(FeedDistributionEvent.TakeFeedWeightPhoto) },
                    remotePreviewUrl = state.feedWeightPhotoRemoteUrl,
                    showAction = !state.alreadySubmitted,
                )
            }
            item {
                FeedDistProofAction(
                    title = stringResource(R.string.feed_dist_record_video),
                    subtitle = stringResource(R.string.feed_dist_video_title),
                    icon = MeshaIcons.Video,
                    captured = state.videoCaptured,
                    status = state.videoStatus,
                    previewPath = state.videoPreviewPath,
                    previewKind = FeedDistPreviewKind.Video,
                    capturedLabel = proofLabel(state.videoStatus, stringResource(R.string.feed_dist_video_recorded)),
                    loading = state.isCapturingVideo,
                    loadingLabel = stringResource(R.string.feed_dist_video_uploading),
                    retryLabel = stringResource(R.string.feed_dist_retry_feed_video),
                    replaceLabel = stringResource(R.string.feed_proof_rerecord),
                    enabled = !state.isCapturingVideo && !committed && !state.isFinalSubmitted,
                    message = state.videoMessage,
                    onClick = { onEvent(FeedDistributionEvent.RecordFeedVideo) },
                    remotePreviewUrl = state.videoRemoteUrl,
                    showAction = !state.alreadySubmitted,
                )
            }
            item {
                FeedDistProofAction(
                    title = stringResource(R.string.feed_dist_record_water_video),
                    subtitle = stringResource(R.string.feed_dist_water_hint),
                    icon = MeshaIcons.Video,
                    captured = state.waterVideoCaptured,
                    status = state.waterVideoStatus,
                    previewPath = state.waterVideoPreviewPath,
                    previewKind = FeedDistPreviewKind.Video,
                    capturedLabel = proofLabel(state.waterVideoStatus, stringResource(R.string.feed_dist_water_video_captured)),
                    loading = state.isCapturingWaterVideo,
                    loadingLabel = stringResource(R.string.feed_dist_water_uploading),
                    retryLabel = stringResource(R.string.feed_dist_retry_water_video),
                    replaceLabel = stringResource(R.string.feed_proof_rerecord),
                    enabled = state.waterVideoCaptureEnabled,
                    message = state.waterVideoMessage,
                    onClick = { onEvent(FeedDistributionEvent.RecordWaterVideo) },
                    remotePreviewUrl = state.waterVideoRemoteUrl,
                    showAction = !state.alreadySubmitted,
                )
            }
        }
    }
}

@Composable
private fun FeedDistStatusCard(
    state: FeedDistributionUiState,
    committed: Boolean,
    onRetrySubmit: () -> Unit,
) {
    val completionFailed = state.result?.status == FeedDistributionStatus.FAILED
    val statusText = when {
        committed -> stringResource(R.string.feed_dist_submitted)
        completionFailed -> state.result.message
        state.submitEnabled -> stringResource(R.string.feed_dist_ready_to_submit)
        state.feedWeightPhotoCaptured || state.videoCaptured || state.waterVideoCaptured -> stringResource(R.string.feed_dist_waiting_sync)
        else -> stringResource(R.string.feed_dist_need_both)
    }
    val tone = when {
        committed -> MeshaColors.Ok
        completionFailed -> MeshaColors.Danger
        state.feedWeightPhotoCaptured && state.videoCaptured && state.waterVideoCaptured -> MeshaColors.BrandD
        else -> MeshaColors.Muted
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(text = stringResource(R.string.feed_dist_caption), color = MeshaColors.Muted, style = MeshaType.body)
        Text(text = statusText, color = tone, style = MeshaType.caption)
        if (state.submitEnabled) {
            FeedDistRetryButton(label = stringResource(R.string.feed_dist_submit), onClick = onRetrySubmit)
        } else if (completionFailed) {
            FeedDistRetryButton(label = stringResource(R.string.feed_dist_retry_submit), onClick = onRetrySubmit)
        }
    }
}

@Composable
internal fun proofLabel(status: FeedDistributionProofStatus, recordedLabel: String): String = when (status) {
    FeedDistributionProofStatus.SYNCED -> stringResource(R.string.feed_dist_proof_synced)
    FeedDistributionProofStatus.UPLOADING -> stringResource(R.string.feed_dist_proof_uploading)
    FeedDistributionProofStatus.QUEUED -> stringResource(R.string.feed_dist_proof_waiting)
    FeedDistributionProofStatus.FAILED -> stringResource(R.string.feed_dist_proof_failed)
    FeedDistributionProofStatus.EMPTY -> recordedLabel
}

typealias FeedDistPreviewKind = ProofMediaPreviewKind

@Composable
internal fun FeedDistProofAction(
    title: String,
    subtitle: String,
    icon: ImageVector,
    captured: Boolean,
    status: FeedDistributionProofStatus,
    previewPath: String?,
    previewKind: FeedDistPreviewKind,
    capturedLabel: String,
    loading: Boolean,
    loadingLabel: String,
    retryLabel: String,
    replaceLabel: String,
    enabled: Boolean,
    message: String?,
    onClick: () -> Unit,
    remotePreviewUrl: String? = null,
    showAction: Boolean = true,
) {
    val failed = status == FeedDistributionProofStatus.FAILED
    val synced = status == FeedDistributionProofStatus.SYNCED
    val uploading = loading || status == FeedDistributionProofStatus.UPLOADING
    val border = when {
        failed -> MeshaColors.Danger
        synced -> MeshaColors.Ok
        captured -> MeshaColors.Brand.copy(alpha = 0.5f)
        else -> MeshaColors.Hair
    }
    val iconBg = when {
        failed -> MeshaColors.Danger.copy(alpha = 0.14f)
        synced -> MeshaColors.Ok.copy(alpha = 0.14f)
        captured -> MeshaColors.Brand.copy(alpha = 0.14f)
        else -> MeshaColors.Surf2
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .clickable(enabled = enabled && !captured && !uploading, onClick = onClick)
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.size(42.dp).clip(RoundedCornerShape(14.dp)).background(iconBg),
            contentAlignment = Alignment.Center,
        ) {
            when {
                uploading -> CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
                failed -> Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.Danger, modifier = Modifier.size(22.dp))
                synced -> Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Ok, modifier = Modifier.size(22.dp))
                captured -> Icon(icon, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(22.dp))
                else -> Icon(icon, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = when {
                    uploading -> loadingLabel
                    failed -> retryLabel
                    captured -> capturedLabel
                    else -> title
                },
                color = if (failed) MeshaColors.Danger else if (enabled || captured || uploading) MeshaColors.Ink else MeshaColors.Faint,
                style = MeshaType.cardTitle,
            )
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            val previewToShow = previewPath ?: remotePreviewUrl
            if (!previewToShow.isNullOrBlank()) {
                FeedDistPreview(path = previewToShow, kind = previewKind)
                if (showAction) {
                    FeedDistRetryButton(label = if (failed) retryLabel else replaceLabel, enabled = enabled, onClick = onClick)
                }
            } else if (!uploading && showAction) {
                FeedDistRetryButton(label = if (captured || failed) replaceLabel else title, enabled = enabled, onClick = onClick)
            }
            if (!message.isNullOrBlank()) {
                Text(text = message, color = MeshaColors.Faint, style = MeshaType.caption)
            }
        }
    }
}

@Composable
private fun FeedDistPreview(path: String, kind: FeedDistPreviewKind) {
    ProofMediaPreview(path = path, kind = kind)
}

@Composable
internal fun FeedDistRetryButton(label: String, enabled: Boolean = true, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (enabled) MeshaColors.OnBrand else MeshaColors.Muted,
        style = MeshaType.cta,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf2)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 13.dp),
    )
}
