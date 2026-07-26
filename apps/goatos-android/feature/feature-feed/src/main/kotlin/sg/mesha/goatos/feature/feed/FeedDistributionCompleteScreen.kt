package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedDistributionCompleteViewModel (:app), which owns
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
import androidx.compose.foundation.shape.RoundedCornerShape
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
 * Feed-DISTRIBUTION completion detail (L2), reached by tapping a shed-session row on Feed DIRECTION.
 * The verifier-gated flow (docs/decisions/feed-distribution-verification.md): the operator records a
 * MANDATORY feed-distribution video AND a MANDATORY water-distribution proof (photo OR video), then
 * submits. Both proofs are required before Submit enables (client-side gate; the backend also rejects
 * a blank proof `422 proof_required`). Submitting flips the shed-session to `pending_verification` —
 * NOTHING is completed until a verifier approves the pair.
 *
 * This is a SEPARATE screen from [FeedCompleteScreen] (the untouched Packing path); Packing rows
 * still open [FeedCompleteScreen] (instant, optional video, no verifier).
 */

enum class FeedDistributionStatus { QUEUED, SYNCED, FAILED }

@androidx.compose.runtime.Immutable
data class FeedDistributionResultUi(val status: FeedDistributionStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedDistributionUiState(
    val shedLabel: String = "",
    val sessionLabel: String = "",
    val workflowLabel: String = "",
    val isCapturingVideo: Boolean = false,
    val videoCaptured: Boolean = false,
    val videoMessage: String? = null,
    val isCapturingWater: Boolean = false,
    val waterCaptured: Boolean = false,
    val waterMessage: String? = null,
    val canComplete: Boolean = false,
    val result: FeedDistributionResultUi? = null,
) {
    /** Both mandatory proofs are recorded and the write is not already committed. */
    val submitEnabled: Boolean
        get() = canComplete && videoCaptured && waterCaptured && !isCapturingVideo && !isCapturingWater &&
            result?.status != FeedDistributionStatus.SYNCED && result?.status != FeedDistributionStatus.QUEUED
}

sealed interface FeedDistributionEvent {
    data object RecordFeedVideo : FeedDistributionEvent
    data object TakeWaterPhoto : FeedDistributionEvent
    data object RecordWaterVideo : FeedDistributionEvent
    data object MarkDone : FeedDistributionEvent
    data object Back : FeedDistributionEvent
}

@Composable
fun FeedDistributionCompleteScreen(
    state: FeedDistributionUiState,
    onEvent: (FeedDistributionEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedDistributionStatus.SYNCED ||
        state.result?.status == FeedDistributionStatus.QUEUED
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
            modifier = Modifier.clickable { onEvent(FeedDistributionEvent.Back) },
        )

        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(text = state.shedLabel, color = MeshaColors.Ink, fontSize = 20.sp, fontWeight = FontWeight.W800)
            val subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" · ")
            if (subtitle.isNotBlank()) {
                Text(text = subtitle, color = MeshaColors.Muted, fontSize = 13.sp)
            }
        }

        Text(
            text = stringResource(R.string.feed_dist_caption),
            color = MeshaColors.Faint,
            fontSize = 12.sp,
        )

        // Tile 1 — MANDATORY feed-distribution video.
        Text(
            text = stringResource(R.string.feed_dist_video_title),
            color = MeshaColors.Ink,
            fontSize = 13.sp,
            fontWeight = FontWeight.W700,
        )
        FeedDistActionButton(
            label = when {
                state.videoCaptured -> stringResource(R.string.feed_dist_video_recorded)
                state.isCapturingVideo -> stringResource(R.string.feed_dist_video_recording)
                else -> stringResource(R.string.feed_dist_record_video)
            },
            enabled = !state.isCapturingVideo && !state.isCapturingWater && !state.videoCaptured && !committed,
            primary = false,
            onClick = { onEvent(FeedDistributionEvent.RecordFeedVideo) },
        )
        state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }

        Spacer(modifier = Modifier.height(2.dp))

        // Tile 2 — MANDATORY water-distribution proof: photo OR video.
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                text = stringResource(R.string.feed_dist_water_title),
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
            Text(
                text = "· ${stringResource(R.string.feed_dist_water_hint)}",
                color = MeshaColors.Faint,
                fontSize = 12.sp,
            )
        }
        if (state.waterCaptured) {
            Text(
                text = stringResource(R.string.feed_dist_water_captured),
                color = MeshaColors.Ok,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                FeedDistActionButton(
                    label = if (state.isCapturingWater) stringResource(R.string.feed_dist_water_capturing)
                    else stringResource(R.string.feed_dist_take_water_photo),
                    enabled = !state.isCapturingWater && !state.isCapturingVideo && !committed,
                    primary = false,
                    modifier = Modifier.weight(1f),
                    onClick = { onEvent(FeedDistributionEvent.TakeWaterPhoto) },
                )
                FeedDistActionButton(
                    label = stringResource(R.string.feed_dist_record_water_video),
                    enabled = !state.isCapturingWater && !state.isCapturingVideo && !committed,
                    primary = false,
                    modifier = Modifier.weight(1f),
                    onClick = { onEvent(FeedDistributionEvent.RecordWaterVideo) },
                )
            }
        }
        state.waterMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }

        Spacer(modifier = Modifier.height(2.dp))

        FeedDistActionButton(
            label = stringResource(R.string.feed_dist_submit),
            enabled = state.submitEnabled,
            primary = true,
            onClick = { onEvent(FeedDistributionEvent.MarkDone) },
        )
        if (!state.submitEnabled && !committed && !(state.videoCaptured && state.waterCaptured)) {
            Text(text = stringResource(R.string.feed_dist_need_both), color = MeshaColors.Faint, fontSize = 12.sp)
        }

        state.result?.let { result ->
            val tone = when (result.status) {
                FeedDistributionStatus.FAILED -> MeshaColors.Danger
                else -> MeshaColors.Ok
            }
            Text(text = result.message, color = tone, fontSize = 13.sp, fontWeight = FontWeight.W700)
        }
    }
}

@Composable
private fun FeedDistActionButton(
    label: String,
    enabled: Boolean,
    primary: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
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
        Text(text = label, color = fg, fontSize = 15.sp, fontWeight = FontWeight.W800)
    }
}
