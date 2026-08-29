package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedPackingCompleteViewModel (:app), which owns
// every capture/enqueue side effect this screen triggers via events.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * Feed-PACKING completion detail (L2), reached by tapping a shed-session row on Feed Packing. The
 * verifier-gated flow: the operator records ONE MANDATORY packing video, then submits. Submit
 * enables only once the video is captured (client-side gate; the backend also rejects a blank
 * proof `422 proof_required`). Submitting flips the shed-session to `pending_verification` —
 * NOTHING is completed until a verifier approves.
 *
 * Simpler than [FeedDistributionCompleteScreen] (which needs a feed video AND a water proof):
 * packing needs only the single mandatory video, camera-only capture, no photo option. This is a
 * SEPARATE screen from [FeedCompleteScreen] (the untouched instant packing/direction path); the old
 * route stays reachable but Packing rows now open THIS screen instead.
 */

enum class FeedPackingCompleteStatus { QUEUED, SYNCED, FAILED }

@androidx.compose.runtime.Immutable
data class FeedPackingCompleteResultUi(val status: FeedPackingCompleteStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedPackingCompleteUiState(
    val shedLabel: String = "",
    val sessionLabel: String = "",
    val workflowLabel: String = "",
    val isCapturingVideo: Boolean = false,
    val videoCaptured: Boolean = false,
    val videoMessage: String? = null,
    val videoPreviewPath: String? = null,
    val videoStatus: FeedDistributionProofStatus = FeedDistributionProofStatus.EMPTY,
    val canComplete: Boolean = false,
    val isSyncing: Boolean = false,
    val result: FeedPackingCompleteResultUi? = null,
    /**
     * The session already went to the verifier (or was approved), so there is nothing to record
     * here.
     *
     * Backend-owned: it comes from the row's lifecycle bucket, NOT from the local capture draft.
     * The draft was the only signal this screen had, and a reinstall wipes it — which is how an
     * operator was shown an empty form for work already queued and re-shot a video the backend then
     * discarded as an idempotent replay (STG 2026-08-09).
     */
    val alreadySubmitted: Boolean = false,
) {
    /** The mandatory video is queued locally and the write is not already committed. */
    val submitEnabled: Boolean
        get() = !alreadySubmitted && canComplete && videoCaptured && !isCapturingVideo &&
            videoStatus.isQueuedForSubmit() &&
            result?.status != FeedPackingCompleteStatus.SYNCED && result?.status != FeedPackingCompleteStatus.QUEUED

    /** Recording is offered only while the session is still the operator's to act on. */
    val captureEnabled: Boolean get() = !alreadySubmitted
}

sealed interface FeedPackingCompleteEvent {
    /** Record the packing video with the LIVE in-app camera. */
    data object RecordPackingVideo : FeedPackingCompleteEvent

    /** Replace the recorded clip; the ViewModel drops the discarded take's queued upload. */
    data object ReRecordPackingVideo : FeedPackingCompleteEvent

    data object MarkDone : FeedPackingCompleteEvent
    data object SyncNow : FeedPackingCompleteEvent
    data object Back : FeedPackingCompleteEvent
}

@Composable
fun FeedPackingCompleteScreen(
    state: FeedPackingCompleteUiState,
    onEvent: (FeedPackingCompleteEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedPackingCompleteStatus.SYNCED ||
        state.result?.status == FeedPackingCompleteStatus.QUEUED
    val subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" · ")
    Scaffold(
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.shedLabel.ifBlank { stringResource(R.string.feed_packing_title) },
                eyebrow = "FEED PACKING",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = subtitle,
                onBack = { onEvent(FeedPackingCompleteEvent.Back) },
                actions = {
                    SyncIconButton(
                        isSyncing = state.isSyncing,
                        onSync = { onEvent(FeedPackingCompleteEvent.SyncNow) },
                    )
                },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(12.dp),
        ) {
            if (state.alreadySubmitted) {
                item {
                    FeedDistStatusCardBody(
                        text = stringResource(R.string.feed_complete_already_submitted_body),
                        tone = MeshaColors.Muted,
                    )
                }
            }

            item {
                FeedPackingStatusCard(
                    state = state,
                    committed = committed,
                    onRetrySubmit = { onEvent(FeedPackingCompleteEvent.MarkDone) },
                )
            }
            item {
                FeedDistProofAction(
                    title = stringResource(R.string.feed_pack_complete_record_video),
                    subtitle = stringResource(R.string.feed_pack_complete_video_title),
                    icon = MeshaIcons.Video,
                    captured = state.videoCaptured,
                    status = state.videoStatus,
                    previewPath = state.videoPreviewPath,
                    previewKind = FeedDistPreviewKind.Video,
                    capturedLabel = proofLabel(state.videoStatus, stringResource(R.string.feed_pack_complete_video_recorded)),
                    loading = state.isCapturingVideo,
                    loadingLabel = stringResource(R.string.feed_pack_complete_video_uploading),
                    retryLabel = stringResource(R.string.feed_pack_complete_retry_video),
                    replaceLabel = stringResource(R.string.feed_proof_rerecord),
                    enabled = state.captureEnabled && !committed && !state.isCapturingVideo,
                    message = state.videoMessage,
                    onClick = {
                        if (state.videoCaptured) {
                            onEvent(FeedPackingCompleteEvent.ReRecordPackingVideo)
                        } else {
                            onEvent(FeedPackingCompleteEvent.RecordPackingVideo)
                        }
                    },
                    showAction = !state.alreadySubmitted,
                )
            }
        }
    }
}

@Composable
private fun FeedPackingStatusCard(
    state: FeedPackingCompleteUiState,
    committed: Boolean,
    onRetrySubmit: () -> Unit,
) {
    val completionFailed = state.result?.status == FeedPackingCompleteStatus.FAILED
    val statusText = when {
        committed -> stringResource(R.string.feed_pack_complete_submitted)
        completionFailed -> state.result.message
        state.submitEnabled -> stringResource(R.string.feed_pack_complete_ready_to_submit)
        state.videoCaptured -> stringResource(R.string.feed_pack_complete_waiting_sync)
        else -> stringResource(R.string.feed_pack_complete_need_video)
    }
    val tone = when {
        committed -> MeshaColors.Ok
        completionFailed -> MeshaColors.Danger
        state.videoCaptured -> MeshaColors.BrandD
        else -> MeshaColors.Muted
    }
    androidx.compose.foundation.layout.Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
        verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(10.dp),
    ) {
        Text(text = stringResource(R.string.feed_pack_complete_caption), color = MeshaColors.Muted, style = MeshaType.body)
        Text(text = statusText, color = tone, style = MeshaType.caption)
        if (state.submitEnabled) {
            FeedDistRetryButton(label = stringResource(R.string.feed_pack_complete_submit), onClick = onRetrySubmit)
        } else if (completionFailed) {
            FeedDistRetryButton(label = stringResource(R.string.feed_pack_complete_retry_submit), onClick = onRetrySubmit)
        }
    }
}

/** internal (not private): also used by FeedDistributionCompleteScreen and FeedTransportScreen for
 *  the same-shaped "already submitted" body. */
@Composable
internal fun FeedDistStatusCardBody(text: String, tone: Color) {
    androidx.compose.foundation.layout.Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
    ) {
        Text(text = text, color = tone, style = MeshaType.body)
    }
}
