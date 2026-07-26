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
import androidx.compose.material3.CircularProgressIndicator
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
 *  2. Add a MANDATORY video of the move — record it live with the in-app camera OR upload one from
 *     the device gallery.
 *  3. **Mark done** — the only action that RELOCATES the animals. It enqueues the complete write to
 *     the durable outbox, so a press in a dead-signal shed is safe and replays under one stable key.
 *
 * The video is REQUIRED and GATES "Mark done" (maintainer decision, 2026-07-26): completion stays
 * disabled until a video is captured, and a verifier must approve it before the move is applied. The
 * clip uploads to GCS through the same signed-URL proof path as vaccination proof, linked to the
 * movement by metadata.
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
    /** Mandatory video: false until captured/queued. Gates [canComplete]. */
    val videoCaptured: Boolean = false,
    val isCapturingVideo: Boolean = false,
    val videoMessage: String? = null,
    /** The "Mark done" write result. */
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val canComplete: Boolean = false,
)

sealed interface ShiftingExecuteEvent {
    /** Record the mandatory move video with the LIVE in-app camera. */
    data object RecordVideo : ShiftingExecuteEvent

    /** Pick the mandatory move video from the device gallery. */
    data object PickVideo : ShiftingExecuteEvent
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
    val committed = state.result.status == CountsWriteStatus.QUEUED || state.result.status == CountsWriteStatus.SYNCED
    Column(modifier = cardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text("Video (required)", color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
            if (state.videoCaptured) {
                Icon(MeshaIcons.CheckCircle, contentDescription = "captured", tint = MeshaColors.Ok, modifier = Modifier.size(18.dp))
            }
        }
        when {
            // Importing/recording in progress: a single, non-tappable loader row so a long
            // gallery import can never be double-triggered.
            state.isCapturingVideo -> VideoActionButton(
                icon = null,
                label = "Adding video…",
                enabled = false,
                loading = true,
                onClick = {},
            )
            // Record live OR upload from the gallery. Re-tapping either replaces the clip, so an
            // operator who filmed the wrong pen can redo it right up until they mark the move done.
            else -> Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                VideoActionButton(
                    icon = MeshaIcons.Video,
                    label = if (state.videoCaptured) "Re-record" else "Record video",
                    enabled = !committed,
                    modifier = Modifier.weight(1f),
                    onClick = { onEvent(ShiftingExecuteEvent.RecordVideo) },
                )
                VideoActionButton(
                    icon = MeshaIcons.Download,
                    label = if (state.videoCaptured) "Re-upload" else "Upload from gallery",
                    enabled = !committed,
                    modifier = Modifier.weight(1f),
                    onClick = { onEvent(ShiftingExecuteEvent.PickVideo) },
                )
            }
        }
        state.videoMessage?.let { Text(it, color = MeshaColors.Faint, fontSize = 11.sp) }
        if (!state.videoCaptured && !state.isCapturingVideo) {
            Text(
                "A video is required. A verifier reviews it before the move is applied.",
                color = MeshaColors.Faint,
                fontSize = 11.sp,
            )
        }
    }
}

@Composable
private fun VideoActionButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector?,
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    loading: Boolean = false,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .then(if (enabled) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp, color = MeshaColors.Muted)
        } else if (icon != null) {
            Icon(icon, contentDescription = null, tint = if (enabled) MeshaColors.BrandD else MeshaColors.Faint, modifier = Modifier.size(18.dp))
        }
        Text(
            text = label,
            color = if (enabled || loading) MeshaColors.Ink else MeshaColors.Faint,
            fontSize = 13.sp,
            fontWeight = FontWeight.W600,
        )
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
