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
 * Feed-DISTRIBUTION completion detail (L2), reached by tapping a shed-session row on Feed DIRECTION.
 * The verifier-gated flow (docs/decisions/feed-distribution-verification.md): the operator records a
 * MANDATORY feed-distribution video, then the MANDATORY water-distribution proof (photo OR video)
 * enables. Both proofs are required before Submit enables (client-side gate; the backend also rejects
 * a blank proof `422 proof_required`). Submitting flips the shed-session to `pending_verification` —
 * NOTHING is completed until a verifier approves the pair.
 *
 * This is separate from the packing-proof flow.
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
    /** The second proof is actionable only after the first proof has been recorded. */
    val waterCaptureEnabled: Boolean
        get() = videoCaptured && !waterCaptured && !isCapturingVideo && !isCapturingWater &&
            result?.status != FeedDistributionStatus.SYNCED && result?.status != FeedDistributionStatus.QUEUED

    /** Both mandatory proofs are recorded and the write is not already committed. */
    val submitEnabled: Boolean
        get() = canComplete && videoCaptured && waterCaptured && !isCapturingVideo && !isCapturingWater &&
            result?.status != FeedDistributionStatus.SYNCED && result?.status != FeedDistributionStatus.QUEUED
}

sealed interface FeedDistributionEvent {
    /** Record the feed video with the LIVE in-app camera. */
    data object RecordFeedVideo : FeedDistributionEvent

    data object TakeWaterPhoto : FeedDistributionEvent
    data object RecordWaterVideo : FeedDistributionEvent

    /**
     * Replace an already-captured proof. Distinct from the Record/Take events so the ViewModel can
     * DROP the queued upload of the take being discarded — a re-record must not leave the verifier
     * two proofs for one step.
     */
    data object ReRecordFeedVideo : FeedDistributionEvent
    data object ReTakeWaterPhoto : FeedDistributionEvent
    data object ReRecordWaterVideo : FeedDistributionEvent
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
    FeedCaptureScaffold(
        title = state.shedLabel,
        subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" \u00b7 "),
        instruction = stringResource(R.string.feed_dist_caption),
        onBack = { onEvent(FeedDistributionEvent.Back) },
    ) {
        // Step 1 — MANDATORY live feed-distribution video.
        FeedProofCard(title = stringResource(R.string.feed_dist_video_title)) {
            when {
                state.videoCaptured -> FeedVerificationCaptured(
                    label = stringResource(R.string.feed_dist_video_recorded),
                    reRecordLabel = stringResource(R.string.feed_proof_rerecord),
                    onReRecord = { onEvent(FeedDistributionEvent.ReRecordFeedVideo) },
                    enabled = !committed,
                )
                state.isCapturingVideo -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_dist_video_uploading),
                    enabled = false,
                    primary = false,
                    loading = true,
                    onClick = {},
                )
                else -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_dist_record_video),
                    enabled = !state.isCapturingWater && !committed,
                    primary = false,
                    onClick = { onEvent(FeedDistributionEvent.RecordFeedVideo) },
                )
            }
            state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        // Step 2 — MANDATORY live water-distribution proof: photo OR video. Disabled until step 1.
        FeedProofCard(
            title = stringResource(R.string.feed_dist_water_title),
            hint = stringResource(R.string.feed_dist_water_hint),
        ) {
            when {
                state.waterCaptured -> FeedVerificationCaptured(
                    label = stringResource(R.string.feed_dist_water_captured),
                    reRecordLabel = stringResource(R.string.feed_proof_recapture),
                    onReRecord = { onEvent(FeedDistributionEvent.ReRecordWaterVideo) },
                    enabled = !committed,
                )
                state.isCapturingWater -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_dist_water_uploading),
                    enabled = false,
                    primary = false,
                    loading = true,
                    onClick = {},
                )
                else -> Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    FeedVerificationActionButton(
                        label = stringResource(R.string.feed_dist_take_water_photo),
                        enabled = state.waterCaptureEnabled,
                        primary = false,
                        modifier = Modifier.weight(1f),
                        onClick = { onEvent(FeedDistributionEvent.TakeWaterPhoto) },
                    )
                    FeedVerificationActionButton(
                        label = stringResource(R.string.feed_dist_record_water_video),
                        enabled = state.waterCaptureEnabled,
                        primary = false,
                        modifier = Modifier.weight(1f),
                        onClick = { onEvent(FeedDistributionEvent.RecordWaterVideo) },
                    )
                }
            }
            state.waterMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        FeedVerificationActionButton(
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

/**
 * Shared "proof captured" confirmation, with the re-capture affordance beside it.
 *
 * A captured step used to be a dead end: the operator saw a green tick and no way to replace a clip
 * that was unusable (shaky, wrong shed, cut short) — the only escape was to submit bad evidence and
 * wait for the verifier to reject it (maintainer request 2026-07-30). [onReRecord] discards the
 * queued upload and re-opens the camera; omit it for a step that genuinely cannot be redone.
 */
@Composable
internal fun FeedVerificationCaptured(
    label: String,
    reRecordLabel: String? = null,
    onReRecord: (() -> Unit)? = null,
    enabled: Boolean = true,
) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(text = "✓", color = MeshaColors.Ok, fontSize = 14.sp, fontWeight = FontWeight.W800)
            Text(text = label, color = MeshaColors.Ok, fontSize = 13.sp, fontWeight = FontWeight.W700)
        }
        if (onReRecord != null && reRecordLabel != null) {
            FeedVerificationActionButton(
                label = reRecordLabel,
                enabled = enabled,
                primary = false,
                onClick = onReRecord,
            )
        }
    }
}

@Composable
internal fun FeedVerificationActionButton(
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
        // Surf2, not Surf: these buttons now sit INSIDE a Surf proof card (FeedProofCard), so a
        // Surf button would read as a flat panel rather than a control.
        else -> MeshaColors.Surf2
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
