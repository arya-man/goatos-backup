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
 *
 * The verifier-gated flow (docs/decisions/feed-distribution-verification.md) is THREE mandatory
 * captures, each unlocking the next:
 *
 *  1. the feed-weight PHOTO, taken while the feed is still on the scale;
 *  2. the feed-distribution VIDEO;
 *  3. the water-distribution VIDEO.
 *
 * All three are required before Submit enables (client-side gate; the backend also rejects a blank or
 * wrong-kind proof `422 proof_required`). Submitting flips the shed-session to `pending_verification`
 * — NOTHING is completed until a verifier approves the set.
 *
 * WHY THE ORDER IS ENFORCED rather than suggested: the weight photo can only be taken before the feed
 * is given out, so a screen that let the operator shoot it last would be asking for a staged photo of
 * a scale that no longer holds that pen's feed.
 *
 * Water is VIDEO-ONLY since 2026-08-11 (maintainer decision). The photo affordance is deliberately
 * gone rather than hidden: a still of a full trough proves a trough is full, not that this operator
 * filled it today.
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
    val isCapturingWeight: Boolean = false,
    val weightCaptured: Boolean = false,
    val weightMessage: String? = null,
    val isCapturingVideo: Boolean = false,
    val videoCaptured: Boolean = false,
    val videoMessage: String? = null,
    val isCapturingWater: Boolean = false,
    val waterCaptured: Boolean = false,
    val waterMessage: String? = null,
    val canComplete: Boolean = false,
    val result: FeedDistributionResultUi? = null,
    /**
     * The session already went to the verifier (or was approved), so there is nothing to record.
     *
     * Backend-owned: from the row's lifecycle bucket, NOT the local capture draft. The draft was
     * the only signal this screen had and a reinstall wipes it, which is how an operator was shown
     * an empty form for work already queued (STG 2026-08-09).
     */
    val alreadySubmitted: Boolean = false,
) {
    /** True once the write is committed (queued or synced) — every capture affordance closes. */
    private val committed: Boolean
        get() = result?.status == FeedDistributionStatus.SYNCED || result?.status == FeedDistributionStatus.QUEUED

    /** No capture may start while another is in flight, or after the write is committed. */
    private val captureIdle: Boolean
        get() = !alreadySubmitted && !committed && !isCapturingWeight && !isCapturingVideo && !isCapturingWater

    /** Step 1. The weight photo opens the flow — nothing gates it but the screen being actionable. */
    val weightCaptureEnabled: Boolean
        get() = captureIdle && !weightCaptured

    /** Step 2. The distribution video is actionable only after the weight photo exists. */
    val videoCaptureEnabled: Boolean
        get() = captureIdle && weightCaptured && !videoCaptured

    /** Step 3. The water video is actionable only after the distribution video exists. */
    val waterCaptureEnabled: Boolean
        get() = captureIdle && videoCaptured && !waterCaptured

    /** ALL THREE mandatory proofs are recorded and the write is not already committed. */
    val submitEnabled: Boolean
        get() = canComplete && weightCaptured && videoCaptured && waterCaptured &&
            !isCapturingWeight && !isCapturingVideo && !isCapturingWater && !committed
}

sealed interface FeedDistributionEvent {
    /** Step 1: photograph the weighed feed with the LIVE in-app camera. */
    data object TakeFeedWeightPhoto : FeedDistributionEvent

    /** Step 2: record the feed-distribution video with the LIVE in-app camera. */
    data object RecordFeedVideo : FeedDistributionEvent

    /** Step 3: record the water-distribution video. Video-only since 2026-08-11. */
    data object RecordWaterVideo : FeedDistributionEvent

    /**
     * Replace an already-captured proof. Distinct from the Record/Take events so the ViewModel can
     * DROP the queued upload of the take being discarded — a re-record must not leave the verifier
     * two proofs for one step.
     */
    data object ReTakeFeedWeightPhoto : FeedDistributionEvent
    data object ReRecordFeedVideo : FeedDistributionEvent
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
        // ALREADY SUBMITTED: both proofs went to the verifier, so there is nothing to record. The
        // operator still reaches this screen — a tapped row must open — but sees the state rather
        // than an empty form, which is what let a second set of proofs be shot for queued work.
        if (state.alreadySubmitted) {
            FeedProofCard(title = stringResource(R.string.feed_complete_already_submitted_title)) {
                Text(
                    text = stringResource(R.string.feed_complete_already_submitted_body),
                    color = MeshaColors.Muted,
                    fontSize = 13.sp,
                )
            }
            return@FeedCaptureScaffold
        }

        // Step 1 — MANDATORY live feed-weight PHOTO, taken while the feed is still on the scale.
        FeedProofCard(
            title = stringResource(R.string.feed_dist_weight_title),
            hint = stringResource(R.string.feed_dist_weight_hint),
        ) {
            when {
                state.weightCaptured -> FeedVerificationCaptured(
                    label = stringResource(R.string.feed_dist_weight_captured),
                    reRecordLabel = stringResource(R.string.feed_proof_recapture),
                    onReRecord = { onEvent(FeedDistributionEvent.ReTakeFeedWeightPhoto) },
                    enabled = !committed,
                )
                state.isCapturingWeight -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_dist_weight_uploading),
                    enabled = false,
                    primary = false,
                    loading = true,
                    onClick = {},
                )
                else -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_dist_take_weight_photo),
                    enabled = state.weightCaptureEnabled,
                    primary = false,
                    onClick = { onEvent(FeedDistributionEvent.TakeFeedWeightPhoto) },
                )
            }
            state.weightMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        // Step 2 — MANDATORY live feed-distribution video. Disabled until step 1 exists.
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
                    enabled = state.videoCaptureEnabled,
                    primary = false,
                    onClick = { onEvent(FeedDistributionEvent.RecordFeedVideo) },
                )
            }
            state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        // Step 3 — MANDATORY live water-distribution VIDEO. Disabled until step 2 exists.
        FeedProofCard(
            title = stringResource(R.string.feed_dist_water_title),
            hint = stringResource(R.string.feed_dist_water_hint),
        ) {
            when {
                state.waterCaptured -> FeedVerificationCaptured(
                    label = stringResource(R.string.feed_dist_water_captured),
                    reRecordLabel = stringResource(R.string.feed_proof_rerecord),
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
                // ONE button. The photo affordance was removed with the 2026-08-11 video-only rule;
                // do not restore it as a second option here.
                else -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_dist_record_water_video),
                    enabled = state.waterCaptureEnabled,
                    primary = false,
                    onClick = { onEvent(FeedDistributionEvent.RecordWaterVideo) },
                )
            }
            state.waterMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
        }

        FeedVerificationActionButton(
            label = stringResource(R.string.feed_dist_submit),
            enabled = state.submitEnabled,
            primary = true,
            onClick = { onEvent(FeedDistributionEvent.MarkDone) },
        )
        if (!state.submitEnabled && !committed &&
            !(state.weightCaptured && state.videoCaptured && state.waterCaptured)
        ) {
            Text(text = stringResource(R.string.feed_dist_need_all), color = MeshaColors.Faint, fontSize = 12.sp)
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
