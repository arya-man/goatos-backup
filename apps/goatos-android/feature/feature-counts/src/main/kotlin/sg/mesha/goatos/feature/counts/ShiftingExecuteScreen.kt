package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; ShiftingExecuteViewModel (in :app) owns the
// counts_shifting_execute_* AnalyticsEvents (open / video captured / completed) and the
// CrashReporter non-fatal on every capture and completion failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * The Shifting EXECUTE screen (`/counts/shifting/execute/{id}`) — an L1 hosted destination with
 * Up/Back and no root chrome (Android navigation-stack invariant).
 *
 * This is where an operator standing in the park confirms an approved movement PHYSICALLY happened:
 *  1. See what to move — source shed -> destination shed, priority/category, the animals.
 *  2. Optionally record a video of the move (optional for now).
 *  3. **Mark done** — the only action that RELOCATES the animals. It enqueues the complete write to
 *     the durable outbox, so a press in a dead-signal shed is safe and replays under one stable key.
 *
 * The video is NOT required and does NOT gate "Mark done": an operator may complete a movement they
 * could not film. When captured, the clip uploads to GCS through the same signed-URL proof path as
 * vaccination proof, linked to the movement by metadata — independently of the completion.
 */

/** One animal to move, for the drill-down roster preview. */
@Immutable
data class ShiftingExecuteAnimalUi(
    val goatId: String,
    val displayId: String,
    val tag: String?,
)

@Immutable
data class ShiftingExecuteUiState(
    val shiftingEventId: String = "",
    val loading: Boolean = true,
    val notFound: Boolean = false,
    val sourceLabel: String = "",
    val destinationLabel: String = "",
    val priority: String = "",
    val category: String = "",
    val animalCount: Int = 0,
    val animals: List<ShiftingExecuteAnimalUi> = emptyList(),
    val animalsTruncated: Boolean = false,
    /** Optional video: null until captured/queued. */
    val videoCaptured: Boolean = false,
    val isCapturingVideo: Boolean = false,
    val videoMessage: String? = null,
    /** The "Mark done" write result. */
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val canComplete: Boolean = false,
)

sealed interface ShiftingExecuteEvent {
    data object RecordVideo : ShiftingExecuteEvent
    data object MarkDone : ShiftingExecuteEvent
    data object Back : ShiftingExecuteEvent
}

@Composable
fun ShiftingExecuteScreen(
    state: ShiftingExecuteUiState,
    onEvent: (ShiftingExecuteEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(title = "Do the shifting", subtitle = "Confirm the animals moved", onBack = { onEvent(ShiftingExecuteEvent.Back) })
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            when {
                state.loading -> Text("Loading movement…", color = MeshaColors.Muted, fontSize = 14.sp)
                state.notFound -> Text(
                    "This movement is no longer in your pending queue. It may have been completed or cancelled already.",
                    color = MeshaColors.Muted,
                    fontSize = 14.sp,
                )
                else -> {
                    MovementCard(state)
                    VideoCard(state, onEvent)
                    if (state.result.status != CountsWriteStatus.IDLE) {
                        CountsResultBanner(state.result)
                    }
                    CountsSubmitButton(
                        label = when (state.result.status) {
                            CountsWriteStatus.QUEUED, CountsWriteStatus.SYNCED -> "Done"
                            else -> "Mark done"
                        },
                        enabled = state.canComplete,
                        onClick = { onEvent(ShiftingExecuteEvent.MarkDone) },
                    )
                }
            }
        }
    }
}

@Composable
private fun MovementCard(state: ShiftingExecuteUiState) {
    Column(
        modifier = cardModifier(),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text("From", color = MeshaColors.Muted, fontSize = 11.sp)
                Text(state.sourceLabel, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W700)
            }
            Icon(MeshaIcons.ArrowUpDown, contentDescription = "to", tint = MeshaColors.BrandD, modifier = Modifier.size(20.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text("To", color = MeshaColors.Muted, fontSize = 11.sp)
                Text(state.destinationLabel, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W700)
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            LabelPill(state.priority, MeshaColors.WarnX, MeshaColors.Warn)
            LabelPill(state.category, MeshaColors.OkX, MeshaColors.Ok)
            LabelPill("${state.animalCount} animal${if (state.animalCount == 1) "" else "s"}", MeshaColors.Surf3, MeshaColors.Muted)
        }
        state.animals.forEach { animal ->
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(animal.displayId, color = MeshaColors.Ink, fontSize = 13.sp, modifier = Modifier.weight(1f))
                animal.tag?.let { Text(it, color = MeshaColors.Faint, fontSize = 12.sp) }
            }
        }
        if (state.animalsTruncated) {
            Text("…and more. The full roster is at the destination.", color = MeshaColors.Faint, fontSize = 11.sp)
        }
    }
}

@Composable
private fun VideoCard(state: ShiftingExecuteUiState, onEvent: (ShiftingExecuteEvent) -> Unit) {
    Column(modifier = cardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("Video (optional)", color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
            if (state.videoCaptured) {
                Icon(MeshaIcons.CheckCircle, contentDescription = "captured", tint = MeshaColors.Ok, modifier = Modifier.size(18.dp))
            }
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                .clickable(enabled = !state.isCapturingVideo) { onEvent(ShiftingExecuteEvent.RecordVideo) }
                .padding(14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(18.dp))
            Text(
                text = when {
                    state.isCapturingVideo -> "Opening camera…"
                    state.videoCaptured -> "Re-record video"
                    else -> "Record a video"
                },
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W600,
            )
        }
        state.videoMessage?.let { Text(it, color = MeshaColors.Faint, fontSize = 11.sp) }
    }
}

@Composable
private fun LabelPill(text: String, bg: androidx.compose.ui.graphics.Color, fg: androidx.compose.ui.graphics.Color) {
    if (text.isBlank()) return
    Text(
        text = text,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(bg).padding(horizontal = 10.dp, vertical = 3.dp),
    )
}

@Composable
private fun cardModifier(): Modifier = Modifier
    .fillMaxWidth()
    .clip(RoundedCornerShape(16.dp))
    .background(MeshaColors.Surf)
    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    .padding(14.dp)
