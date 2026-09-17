package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedWastageCompleteViewModel (:app), which owns
// every capture/enqueue side effect this screen triggers via events.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.sop.SopCardProgress
import sg.mesha.goatos.core.ui.sop.progress

/**
 * Feed-WASTAGE completion detail (L2), reached by tapping a pen row on Feed Wastage (maintainer
 * decision 2026-08-18). The verifier-gated flow: the operator records ONE MANDATORY live-camera
 * video of the leftover feed in the pen, then submits. Submit enables only once the video is
 * captured (client-side gate; the backend also rejects a blank proof `422 proof_required`).
 * Submitting flips the PEN-DAY to `pending_verification` — NOTHING is completed until a verifier
 * approves, and it is the VERIFIER who records the leftover weight she reads off the clip.
 *
 * Same single-proof shape as [FeedPackingCompleteScreen]; camera-only capture, no photo, no
 * gallery.
 */

enum class FeedWastageCompleteStatus { QUEUED, SYNCED, FAILED }

@androidx.compose.runtime.Immutable
data class FeedWastageCompleteResultUi(val status: FeedWastageCompleteStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedWastageCompleteUiState(
    val shedLabel: String = "",
    val experimentArm: String = "",
    /**
     * FEED SOP (2026-09-16): the wastage CARD the pen-day is proven against -- its slots (the
     * seeded card is one leftover-feed video) and questions, with this phone's capture state.
     * Authored on /feed/sops (the direction SOP's wastage card) and pinned on the sheet.
     */
    val card: FeedSopCardUi = FeedSopCardUi(),
    val canComplete: Boolean = false,
    val isSyncing: Boolean = false,
    val result: FeedWastageCompleteResultUi? = null,
    /**
     * The pen-day already went to the verifier (or was approved), so there is nothing to record
     * here. Backend-owned: it comes from the row's lifecycle bucket first and the live Room/server
     * status after, never from the local capture draft alone (a reinstall wipes the draft — the
     * STG 2026-08-09 packing defect this mirrors).
     */
    val alreadySubmitted: Boolean = false,
) {
    val isFinalSubmitted: Boolean
        get() = alreadySubmitted || result?.status == FeedWastageCompleteStatus.SYNCED || result?.status == FeedWastageCompleteStatus.QUEUED

    /** Every compulsory capture is queued locally, the questions are answered and the write is not committed. */
    val submitEnabled: Boolean
        get() = canComplete && card.readyToSubmit && !card.anyCapturing && !isFinalSubmitted

    /** Recording is offered only while the pen-day is still the operator's to act on. */
    val captureEnabled: Boolean get() = !alreadySubmitted
}

sealed interface FeedWastageCompleteEvent {
    /** Capture one slot of the card with the LIVE in-app camera; [kind] picks the medium of an
     *  `either` slot. A filled slot is re-recorded through the same event. */
    data class CaptureSlot(val slotKey: String, val kind: String? = null) : FeedWastageCompleteEvent
    data class Answer(val questionId: String, val value: String) : FeedWastageCompleteEvent

    data object MarkDone : FeedWastageCompleteEvent
    data object SyncNow : FeedWastageCompleteEvent
    data object Back : FeedWastageCompleteEvent
    data class PreviewAction(val slotKey: String, val action: String) : FeedWastageCompleteEvent
}

@Composable
fun FeedWastageCompleteScreen(
    state: FeedWastageCompleteUiState,
    onEvent: (FeedWastageCompleteEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedWastageCompleteStatus.SYNCED ||
        state.result?.status == FeedWastageCompleteStatus.QUEUED
    Scaffold(
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.shedLabel.ifBlank { stringResource(R.string.feed_wastage_title) },
                eyebrow = "FEED WASTAGE",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = state.experimentArm,
                onBack = { onEvent(FeedWastageCompleteEvent.Back) },
                actions = {
                    SyncIconButton(
                        isSyncing = state.isSyncing,
                        onSync = { onEvent(FeedWastageCompleteEvent.SyncNow) },
                    )
                },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(12.dp),
        ) {
            if (state.alreadySubmitted) {
                item {
                    FeedDistStatusCardBody(
                        text = stringResource(R.string.feed_complete_already_submitted_body),
                        tone = MeshaColors.Muted,
                    )
                }
            }

            item {
                FeedWastageStatusCard(
                    state = state,
                    committed = committed,
                    onRetrySubmit = { onEvent(FeedWastageCompleteEvent.MarkDone) },
                )
            }
            feedSopCardItems(
                card = state.card,
                locked = state.isFinalSubmitted,
                onCapture = { key, kind -> onEvent(FeedWastageCompleteEvent.CaptureSlot(key, kind)) },
                onPlaybackFailed = { },
                onPreviewAction = { key, action -> onEvent(FeedWastageCompleteEvent.PreviewAction(key, action)) },
                onAnswer = { id, v -> onEvent(FeedWastageCompleteEvent.Answer(id, v)) },
            )
        }
    }
}

@Composable
private fun FeedWastageStatusCard(
    state: FeedWastageCompleteUiState,
    committed: Boolean,
    onRetrySubmit: () -> Unit,
) {
    val completionFailed = state.result?.status == FeedWastageCompleteStatus.FAILED
    val statusText = when {
        committed -> stringResource(R.string.feed_wastage_submitted)
        completionFailed -> state.result.message
        else -> when (val progress = state.card.progress(state.submitEnabled)) {
            SopCardProgress.Ready -> stringResource(R.string.feed_wastage_ready_to_submit)
            // A required capture/answer not yet recorded is NAMED -- never "upload in progress" (Realme 2026-09-17).
            is SopCardProgress.StillNeeded -> stringResource(R.string.feed_slot_still_needed, progress.titles.joinToString(", "))
            SopCardProgress.Uploading -> stringResource(R.string.feed_wastage_waiting_sync)
            SopCardProgress.NothingCaptured -> stringResource(R.string.feed_slot_need_all)
        }
    }
    val tone = when {
        committed -> MeshaColors.Ok
        completionFailed -> MeshaColors.Danger
        state.card.anyCaptured -> MeshaColors.BrandD
        else -> MeshaColors.Muted
    }
    androidx.compose.foundation.layout.Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
        verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = state.card.instruction.ifBlank { stringResource(R.string.feed_wastage_complete_caption) },
            color = MeshaColors.Muted,
            style = MeshaType.body,
        )
        Text(text = statusText, color = tone, style = MeshaType.caption)
        if (state.submitEnabled) {
            FeedDistRetryButton(label = stringResource(R.string.feed_wastage_submit), onClick = onRetrySubmit)
        } else if (completionFailed) {
            FeedDistRetryButton(label = stringResource(R.string.feed_wastage_retry_submit), onClick = onRetrySubmit)
        }
    }
}
