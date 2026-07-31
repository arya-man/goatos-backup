package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedPackingCompleteViewModel (:app), which owns
// every capture/enqueue side effect this screen triggers via events.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

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
    val canComplete: Boolean = false,
    val result: FeedPackingCompleteResultUi? = null,
) {
    /** The mandatory video is recorded and the write is not already committed. */
    val submitEnabled: Boolean
        get() = canComplete && videoCaptured && !isCapturingVideo &&
            result?.status != FeedPackingCompleteStatus.SYNCED && result?.status != FeedPackingCompleteStatus.QUEUED
}

sealed interface FeedPackingCompleteEvent {
    /** Record the packing video with the LIVE in-app camera. */
    data object RecordPackingVideo : FeedPackingCompleteEvent

    /** Replace the recorded clip; the ViewModel drops the discarded take's queued upload. */
    data object ReRecordPackingVideo : FeedPackingCompleteEvent

    data object MarkDone : FeedPackingCompleteEvent
    data object Back : FeedPackingCompleteEvent
}

@Composable
fun FeedPackingCompleteScreen(
    state: FeedPackingCompleteUiState,
    onEvent: (FeedPackingCompleteEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedPackingCompleteStatus.SYNCED ||
        state.result?.status == FeedPackingCompleteStatus.QUEUED
    FeedCaptureScaffold(
        title = state.shedLabel,
        subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" \u00b7 "),
        instruction = stringResource(R.string.feed_pack_complete_caption),
        onBack = { onEvent(FeedPackingCompleteEvent.Back) },
    ) {
        // MANDATORY live in-app camera packing video.
        FeedProofCard(title = stringResource(R.string.feed_pack_complete_video_title)) {
            when {
                state.videoCaptured -> FeedVerificationCaptured(
                    label = stringResource(R.string.feed_pack_complete_video_recorded),
                    reRecordLabel = stringResource(R.string.feed_proof_rerecord),
                    onReRecord = { onEvent(FeedPackingCompleteEvent.ReRecordPackingVideo) },
                    enabled = !committed,
                )
                state.isCapturingVideo -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_pack_complete_video_uploading),
                    enabled = false,
                    primary = false,
                    loading = true,
                    onClick = {},
                )
                else -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_pack_complete_record_video),
                    enabled = !committed,
                    primary = false,
                    onClick = { onEvent(FeedPackingCompleteEvent.RecordPackingVideo) },
                )
            }
            state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        FeedVerificationActionButton(
            label = stringResource(R.string.feed_pack_complete_submit),
            enabled = state.submitEnabled,
            primary = true,
            onClick = { onEvent(FeedPackingCompleteEvent.MarkDone) },
        )
        if (!state.submitEnabled && !committed && !state.videoCaptured) {
            Text(text = stringResource(R.string.feed_pack_complete_need_video), color = MeshaColors.Faint, fontSize = 12.sp)
        }

        state.result?.let { result ->
            val tone = when (result.status) {
                FeedPackingCompleteStatus.FAILED -> MeshaColors.Danger
                else -> MeshaColors.Ok
            }
            Text(text = result.message, color = tone, fontSize = 13.sp, fontWeight = FontWeight.W700)
        }
    }
}
