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
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MeshaColors.Bg)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Text(
            text = stringResource(R.string.feed_complete_back),
            color = MeshaColors.Muted,
            fontSize = 13.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.clickable { onEvent(FeedPackingCompleteEvent.Back) },
        )

        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(text = state.shedLabel, color = MeshaColors.Ink, fontSize = 20.sp, fontWeight = FontWeight.W800)
            val subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" · ")
            if (subtitle.isNotBlank()) {
                Text(text = subtitle, color = MeshaColors.Muted, fontSize = 13.sp)
            }
        }

        Text(
            text = stringResource(R.string.feed_pack_complete_caption),
            color = MeshaColors.Faint,
            fontSize = 12.sp,
        )

        // Tile — MANDATORY live in-app camera packing video.
        Text(
            text = stringResource(R.string.feed_pack_complete_video_title),
            color = MeshaColors.Ink,
            fontSize = 13.sp,
            fontWeight = FontWeight.W700,
        )
        when {
            state.videoCaptured -> FeedPackCompleteActionButton(
                label = stringResource(R.string.feed_pack_complete_video_recorded),
                enabled = false,
                primary = false,
                onClick = {},
            )
            state.isCapturingVideo -> FeedPackCompleteActionButton(
                label = stringResource(R.string.feed_pack_complete_video_uploading),
                enabled = false,
                primary = false,
                loading = true,
                onClick = {},
            )
            else -> Row {
                FeedPackCompleteActionButton(
                    label = stringResource(R.string.feed_pack_complete_record_video),
                    enabled = !committed,
                    primary = false,
                    modifier = Modifier.weight(1f),
                    onClick = { onEvent(FeedPackingCompleteEvent.RecordPackingVideo) },
                )
            }
        }
        state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }

        Spacer(modifier = Modifier.height(2.dp))

        FeedPackCompleteActionButton(
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

@Composable
private fun FeedPackCompleteActionButton(
    label: String,
    enabled: Boolean,
    primary: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    loading: Boolean = false,
) {
    val bg = when {
        !enabled -> MeshaColors.Hair
        primary -> MeshaColors.Brand
        else -> MeshaColors.Surf
    }
    val fg = when {
        !enabled -> MeshaColors.Faint
        primary -> MeshaColors.Surf
        else -> MeshaColors.Ink
    }
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .then(if (primary) Modifier else Modifier.border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp)))
            .then(if (enabled) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(vertical = 14.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(16.dp),
                strokeWidth = 2.dp,
                color = MeshaColors.Muted,
            )
            Spacer(modifier = Modifier.size(10.dp))
        }
        Text(text = label, color = fg, fontSize = 15.sp, fontWeight = FontWeight.W800)
    }
}
