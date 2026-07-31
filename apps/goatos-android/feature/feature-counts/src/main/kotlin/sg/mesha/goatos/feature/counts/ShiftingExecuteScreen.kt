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
data class ShiftingFeedItemUi(val label: String, val quantityGrams: String)

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
    val highPriority: Boolean = false,
    val feedConfigStatus: String = "not_required",
    val feedConfigBlockedReason: String? = null,
    val feedConfigFingerprint: String? = null,
    val feedTargetStage: String? = null,
    val feedItems: List<ShiftingFeedItemUi> = emptyList(),
    /** Mandatory video: false until captured/queued. Gates [canComplete]. */
    val videoCaptured: Boolean = false,
    val isCapturingVideo: Boolean = false,
    val videoMessage: String? = null,
    val feedPackingVideoCaptured: Boolean = false,
    val feedGivenVideoCaptured: Boolean = false,
    val capturingStep: String? = null,
    /** The "Mark done" write result. */
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val canComplete: Boolean = false,
)

sealed interface ShiftingExecuteEvent {
    /** Record the mandatory move video with the LIVE in-app camera. */
    data object RecordVideo : ShiftingExecuteEvent
    data object RecordFeedPackingVideo : ShiftingExecuteEvent
    data object RecordFeedGivenVideo : ShiftingExecuteEvent

    /**
     * Replace an already-recorded clip. Distinct from Record so the ViewModel can DROP the queued
     * upload of the take being discarded — a re-record must not leave the verifier two videos.
     */
    data object ReRecordVideo : ShiftingExecuteEvent
    data object ReRecordFeedPackingVideo : ShiftingExecuteEvent
    data object ReRecordFeedGivenVideo : ShiftingExecuteEvent
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
                    val committed = state.result.status == CountsWriteStatus.QUEUED || state.result.status == CountsWriteStatus.SYNCED
                    MovementCard(state)
                    if (state.highPriority) FeedRequirementCard(state)
                    EvidenceVideoCard(
                        title = "Shifting video (required)", captured = state.videoCaptured,
                        capturing = state.capturingStep == "shifting", committed = committed,
                        onClick = { onEvent(ShiftingExecuteEvent.RecordVideo) },
                        onReRecord = { onEvent(ShiftingExecuteEvent.ReRecordVideo) },
                    )
                    if (state.highPriority) {
                        EvidenceVideoCard(
                            title = "Feed packing video (required)", captured = state.feedPackingVideoCaptured,
                            capturing = state.capturingStep == "packing", committed = committed,
                            onClick = { onEvent(ShiftingExecuteEvent.RecordFeedPackingVideo) },
                            onReRecord = { onEvent(ShiftingExecuteEvent.ReRecordFeedPackingVideo) },
                        )
                        EvidenceVideoCard(
                            title = "Feed given to animal video (required)", captured = state.feedGivenVideoCaptured,
                            capturing = state.capturingStep == "feeding", committed = committed,
                            onClick = { onEvent(ShiftingExecuteEvent.RecordFeedGivenVideo) },
                            onReRecord = { onEvent(ShiftingExecuteEvent.ReRecordFeedGivenVideo) },
                        )
                    }
                    state.videoMessage?.let { Text(it, color = MeshaColors.Faint, fontSize = 11.sp) }
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
private fun FeedRequirementCard(state: ShiftingExecuteUiState) {
    Column(modifier = cardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("Feed required for this shifting", color = MeshaColors.Ink, fontSize = 14.sp, fontWeight = FontWeight.W700)
        state.feedTargetStage?.let { Text("Operational stage: $it", color = MeshaColors.Muted, fontSize = 12.sp) }
        if (state.feedConfigStatus == "ready") {
            state.feedItems.forEach { item ->
                Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(item.label, color = MeshaColors.Ink, fontSize = 13.sp)
                    Text("${item.quantityGrams} g", color = MeshaColors.BrandD, fontSize = 13.sp, fontWeight = FontWeight.W700)
                }
            }
        } else {
            Text(
                state.feedConfigBlockedReason ?: "Feed configuration is unavailable. Refresh before continuing.",
                color = MeshaColors.Warn,
                fontSize = 12.sp,
            )
        }
        Text("This packing proof belongs only to this Shifting task.", color = MeshaColors.Faint, fontSize = 11.sp)
    }
}

@Composable
private fun EvidenceVideoCard(
    title: String,
    captured: Boolean,
    capturing: Boolean,
    committed: Boolean,
    onClick: () -> Unit,
    onReRecord: () -> Unit = onClick,
) {
    Column(modifier = cardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(title, color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
            if (captured) {
                Icon(MeshaIcons.CheckCircle, contentDescription = "captured", tint = MeshaColors.Ok, modifier = Modifier.size(18.dp))
            }
        }
        when {
            capturing -> VideoActionButton(
                icon = null,
                label = "Recording…",
                enabled = false,
                loading = true,
                onClick = {},
            )
            else -> Row {
                VideoActionButton(
                    icon = MeshaIcons.Video,
                    label = if (captured) "Re-record" else "Record live video",
                    enabled = !committed,
                    modifier = Modifier.weight(1f),
                    onClick = if (captured) onReRecord else onClick,
                )
            }
        }
        if (!captured && !capturing) {
            Text(
                "Live camera only. This evidence is reviewed after the task; it does not control the herd move.",
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
            // Color-only state change; the 18dp icon frame stays fixed regardless of enabled.
            val iconTint = if (enabled) MeshaColors.BrandD else MeshaColors.Faint
            Icon(icon, contentDescription = null, tint = iconTint, modifier = Modifier.size(18.dp))
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
