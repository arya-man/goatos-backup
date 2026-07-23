package sg.mesha.goatos.feature.feed

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
 * Feed-direction completion detail (L2), reached by tapping a shed-session row on Feed Direction or
 * Feed Packing. The operator optionally records a video, then taps Done to mark the shed-session fed.
 *
 * Offline-first: Done enqueues an idempotent outbox write and the row shows as completed immediately
 * (optimistic), converging on the backend's `completed` flag once the write syncs. The video is
 * OPTIONAL and never gates completion.
 */

enum class FeedCompleteStatus { QUEUED, SYNCED, FAILED }

@androidx.compose.runtime.Immutable
data class FeedCompleteResultUi(val status: FeedCompleteStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedCompleteUiState(
    val shedLabel: String = "",
    val sessionLabel: String = "",
    val workflowLabel: String = "",
    val isCapturingVideo: Boolean = false,
    val videoCaptured: Boolean = false,
    val videoMessage: String? = null,
    val canComplete: Boolean = true,
    val result: FeedCompleteResultUi? = null,
)

sealed interface FeedCompleteEvent {
    data object RecordVideo : FeedCompleteEvent
    data object MarkDone : FeedCompleteEvent
    data object Back : FeedCompleteEvent
}

@Composable
fun FeedCompleteScreen(
    state: FeedCompleteUiState,
    onEvent: (FeedCompleteEvent) -> Unit = {},
) {
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
            modifier = Modifier.clickable { onEvent(FeedCompleteEvent.Back) },
        )

        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(text = state.shedLabel, color = MeshaColors.Ink, fontSize = 20.sp, fontWeight = FontWeight.W800)
            val subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" · ")
            if (subtitle.isNotBlank()) {
                Text(text = subtitle, color = MeshaColors.Muted, fontSize = 13.sp)
            }
        }

        Text(
            text = stringResource(R.string.feed_complete_caption),
            color = MeshaColors.Faint,
            fontSize = 12.sp,
        )

        // OPTIONAL video capture. Never gates Done.
        FeedActionButton(
            label = when {
                state.videoCaptured -> stringResource(R.string.feed_complete_video_recorded)
                state.isCapturingVideo -> stringResource(R.string.feed_complete_video_recording)
                else -> stringResource(R.string.feed_complete_record_video)
            },
            enabled = !state.isCapturingVideo && state.result?.status != FeedCompleteStatus.SYNCED,
            primary = false,
            onClick = { onEvent(FeedCompleteEvent.RecordVideo) },
        )
        state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }

        Spacer(modifier = Modifier.height(2.dp))

        FeedActionButton(
            label = stringResource(R.string.feed_complete_done),
            enabled = state.canComplete && !state.isCapturingVideo,
            primary = true,
            onClick = { onEvent(FeedCompleteEvent.MarkDone) },
        )

        state.result?.let { result ->
            val tone = when (result.status) {
                FeedCompleteStatus.FAILED -> MeshaColors.Danger
                else -> MeshaColors.Ok
            }
            Text(text = result.message, color = tone, fontSize = 13.sp, fontWeight = FontWeight.W700)
        }
    }
}

@Composable
private fun FeedActionButton(
    label: String,
    enabled: Boolean,
    primary: Boolean,
    onClick: () -> Unit,
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
        modifier = Modifier
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
