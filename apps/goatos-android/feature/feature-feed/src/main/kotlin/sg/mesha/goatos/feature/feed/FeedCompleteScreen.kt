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
    FeedCaptureScaffold(
        title = state.shedLabel,
        subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" \u00b7 "),
        instruction = stringResource(R.string.feed_complete_caption),
        onBack = { onEvent(FeedCompleteEvent.Back) },
    ) {
        // OPTIONAL video capture. Never gates Done.
        FeedProofCard(title = stringResource(R.string.feed_complete_record_video)) {
            FeedVerificationActionButton(
                label = when {
                    state.videoCaptured -> stringResource(R.string.feed_complete_video_recorded)
                    state.isCapturingVideo -> stringResource(R.string.feed_complete_video_recording)
                    else -> stringResource(R.string.feed_complete_record_video)
                },
                enabled = !state.isCapturingVideo && state.result?.status != FeedCompleteStatus.SYNCED,
                primary = false,
                loading = state.isCapturingVideo,
                onClick = { onEvent(FeedCompleteEvent.RecordVideo) },
            )
            state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        FeedVerificationActionButton(
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
