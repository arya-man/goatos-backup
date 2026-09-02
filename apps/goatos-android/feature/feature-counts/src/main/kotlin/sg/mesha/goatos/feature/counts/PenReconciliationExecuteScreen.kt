package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; PenReconciliationExecuteViewModel (in :app) owns the
// counts_pen_reconciliation_* AnalyticsEvents (open / video captured / completed) and the
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind

/**
 * The Reconcile EXECUTE screen (`/counts/reconcile/execute/{card_id}`) — an L1 hosted destination
 * with Up/Back and no root chrome (Android navigation-stack invariant).
 *
 * This is where an operator standing in the park returns a strayed animal to its registered pen:
 *  1. See the animal — the scanned tag, where it was FOUND, where it BELONGS.
 *  2. Record the MANDATORY pen return video live with the in-app camera.
 *  3. **Mark done** — enqueues the completion to the durable outbox, so a press in a shed with no
 *     signal is safe and replays under one stable key. A verifier reviews the video afterwards;
 *     there is no approver step.
 */

@Immutable
data class PenReconciliationExecuteUiState(
    val cardId: String = "",
    val loading: Boolean = true,
    val notFound: Boolean = false,
    /** The tag exactly as scanned — what the operator reads on the animal's ear. */
    val scannedIdentifier: String = "",
    val goatDisplayId: String = "",
    /** Backend-composed pen label where the animal was scanned. Rendered verbatim. */
    val foundLabel: String = "",
    /** Backend-composed pen label the register says the animal lives in. Rendered verbatim. */
    val belongsLabel: String = "",
    /** The verifier's reason when this card came back for rework; rendered verbatim. */
    val reworkReason: String? = null,
    /** Mandatory video: false until captured/queued. Gates [canComplete]. */
    val videoCaptured: Boolean = false,
    /**
     * Local file path of the recorded clip so the operator can WATCH what they shot before
     * submitting — the same review affordance the feed proof screens give. Restored from the
     * queued upload's own local file on re-entry; null when no clip exists yet or the file is
     * gone (the preview then simply hides, never blocks the flow).
     */
    val videoPreviewPath: String? = null,
    val isCapturingVideo: Boolean = false,
    val videoMessage: String? = null,
    /** The "Mark done" write result. */
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val canComplete: Boolean = false,
    /**
     * Server-confirmed submission: nothing is left to do on this card, so the host pops back to
     * the Reconcile list and shows [submissionNotice] there.
     */
    val returnToList: Boolean = false,
    val submissionNotice: String? = null,
)

sealed interface PenReconciliationExecuteEvent {
    /** Record the mandatory pen return video with the LIVE in-app camera. */
    data object RecordVideo : PenReconciliationExecuteEvent

    /**
     * Replace an already-recorded clip. Distinct from Record so the ViewModel can DROP the queued
     * upload of the take being discarded — a re-record must not leave the verifier two videos.
     */
    data object ReRecordVideo : PenReconciliationExecuteEvent
    data object MarkDone : PenReconciliationExecuteEvent
    data object Back : PenReconciliationExecuteEvent

    /** The host consumed [PenReconciliationExecuteUiState.returnToList]; clear it so it fires once. */
    data object NavigationHandled : PenReconciliationExecuteEvent
}

@Composable
fun PenReconciliationExecuteScreen(
    state: PenReconciliationExecuteUiState,
    onEvent: (PenReconciliationExecuteEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(
            title = "Return the animal",
            subtitle = "Walk it back to its registered pen",
            onBack = { onEvent(PenReconciliationExecuteEvent.Back) },
        )
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            when {
                state.loading -> Text("Loading…", color = MeshaColors.Muted, fontSize = 14.sp)
                state.notFound -> Text(
                    "This card is no longer in your list. It may have been submitted already.",
                    color = MeshaColors.Muted,
                    fontSize = 14.sp,
                )
                else -> {
                    val committed = state.result.status == CountsWriteStatus.QUEUED || state.result.status == CountsWriteStatus.SYNCED
                    PenReturnCard(state)
                    PenReturnVideoCard(
                        captured = state.videoCaptured,
                        capturing = state.isCapturingVideo,
                        committed = committed,
                        previewPath = state.videoPreviewPath,
                        onClick = { onEvent(PenReconciliationExecuteEvent.RecordVideo) },
                        onReRecord = { onEvent(PenReconciliationExecuteEvent.ReRecordVideo) },
                    )
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
                        onClick = { onEvent(PenReconciliationExecuteEvent.MarkDone) },
                    )
                }
            }
        }
    }
}

@Composable
private fun PenReturnCard(state: PenReconciliationExecuteUiState) {
    Column(modifier = penCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = state.scannedIdentifier,
                color = MeshaColors.Ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.W800,
                modifier = Modifier.weight(1f),
            )
            if (state.goatDisplayId.isNotBlank()) {
                Text(state.goatDisplayId, color = MeshaColors.Faint, fontSize = 12.sp)
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text("Found in", color = MeshaColors.Muted, fontSize = 11.sp)
                Text(state.foundLabel, color = MeshaColors.Warn, fontSize = 15.sp, fontWeight = FontWeight.W700)
            }
            Icon(MeshaIcons.ArrowUpDown, contentDescription = "to", tint = MeshaColors.BrandD, modifier = Modifier.size(20.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text("Belongs in", color = MeshaColors.Muted, fontSize = 11.sp)
                Text(state.belongsLabel, color = MeshaColors.Ok, fontSize = 15.sp, fontWeight = FontWeight.W700)
            }
        }
        state.reworkReason?.takeIf { it.isNotBlank() }?.let { reason ->
            Text(
                text = reason,
                color = MeshaColors.Warn,
                fontSize = 12.sp,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(10.dp))
                    .background(MeshaColors.WarnX)
                    .padding(horizontal = 10.dp, vertical = 8.dp),
            )
        }
        Text(
            "The herd register already names the right pen — walk the animal back and film it there.",
            color = MeshaColors.Faint,
            fontSize = 11.sp,
        )
    }
}

@Composable
private fun PenReturnVideoCard(
    captured: Boolean,
    capturing: Boolean,
    committed: Boolean,
    previewPath: String?,
    onClick: () -> Unit,
    onReRecord: () -> Unit = onClick,
) {
    Column(modifier = penCardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                "Pen return video (required)",
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            if (captured) {
                Icon(MeshaIcons.CheckCircle, contentDescription = "captured", tint = MeshaColors.Ok, modifier = Modifier.size(18.dp))
            }
        }
        // The recorded clip, playable in place (the shared proof preview the feed screens use):
        // the operator reviews what they actually shot before sending it to the verifier. A
        // missing/unplayable local file hides the preview and leaves Re-record as the way out.
        if (captured && !capturing && !previewPath.isNullOrBlank()) {
            var previewFailed by remember(previewPath) { mutableStateOf(false) }
            if (!previewFailed) {
                ProofMediaPreview(
                    path = previewPath,
                    kind = ProofMediaPreviewKind.Video,
                    onPlaybackFailure = { previewFailed = true },
                )
            } else {
                Text(
                    "The recorded video can't be played back on this phone. It is still saved — re-record if you want to check it.",
                    color = MeshaColors.Faint,
                    fontSize = 11.sp,
                )
            }
        }
        when {
            capturing -> PenReturnActionButton(
                icon = null,
                label = "Recording…",
                enabled = false,
                loading = true,
                onClick = {},
            )
            else -> Row {
                PenReturnActionButton(
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
                "Live camera only. This evidence is reviewed after the task.",
                color = MeshaColors.Faint,
                fontSize = 11.sp,
            )
        }
    }
}

@Composable
private fun PenReturnActionButton(
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
private fun penCardModifier(): Modifier = Modifier
    .fillMaxWidth()
    .clip(RoundedCornerShape(16.dp))
    .background(MeshaColors.Surf)
    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    .padding(14.dp)
