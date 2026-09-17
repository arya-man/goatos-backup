package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedPackingCompleteViewModel (:app), which owns
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
import androidx.compose.ui.graphics.Color
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
 * Feed-PACKING completion detail (L2), reached by tapping a shed-session row on Feed Packing. The
 * verifier-gated flow: the operator records ONE MANDATORY packing video, then submits. Submit
 * enables only once the video is captured (client-side gate; the backend also rejects a blank
 * proof `422 proof_required`). Submitting flips the shed-session to `pending_verification` —
 * NOTHING is completed until a verifier approves.
 *
 * Simpler than [FeedDistributionCompleteScreen] (which needs a feed video AND a water proof):
 * packing needs only the single mandatory video, camera-only capture, no photo option. This is a
 * SEPARATE screen from [FeedCompleteScreen] (the untouched instant packing/direction path); the old
 * route stays reachable but Packing rows now open THIS screen instead.
 */

enum class FeedPackingCompleteStatus { QUEUED, SYNCED, FAILED }

@androidx.compose.runtime.Immutable
data class FeedPackingCompleteResultUi(val status: FeedPackingCompleteStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedPackingCompleteUiState(
    val shedLabel: String = "",
    val sessionLabel: String = "",
    val workflowLabel: String = "",
    /**
     * FEED SOP (2026-09-16): the packing CARD the bag is proven against -- its slots (the seeded
     * card is one packing video) and questions, with this phone's capture state. Authored on
     * /feed/sops and pinned on the sheet, so a slot added or dropped there reaches this screen
     * without a new build.
     */
    val card: FeedSopCardUi = FeedSopCardUi(),
    val canComplete: Boolean = false,
    val isSyncing: Boolean = false,
    val result: FeedPackingCompleteResultUi? = null,
    /**
     * The session already went to the verifier (or was approved), so there is nothing to record
     * here.
     *
     * Backend-owned: it comes from the row's lifecycle bucket, NOT from the local capture draft.
     * The draft was the only signal this screen had, and a reinstall wipes it — which is how an
     * operator was shown an empty form for work already queued and re-shot a video the backend then
     * discarded as an idempotent replay (STG 2026-08-09).
     */
    val alreadySubmitted: Boolean = false,
) {
    val isFinalSubmitted: Boolean
        get() = alreadySubmitted || result?.status == FeedPackingCompleteStatus.SYNCED || result?.status == FeedPackingCompleteStatus.QUEUED

    /** Every compulsory capture is queued locally, the questions are answered and the write is not committed. */
    val submitEnabled: Boolean
        get() = canComplete && card.readyToSubmit && !card.anyCapturing && !isFinalSubmitted

    /** Recording is offered only while the session is still the operator's to act on. */
    val captureEnabled: Boolean get() = !alreadySubmitted
}

sealed interface FeedPackingCompleteEvent {
    /** Capture one slot of the card with the LIVE in-app camera; [kind] picks the medium of an
     *  `either` slot (null = the slot's own kind). A filled slot is re-recorded through the same
     *  event; the ViewModel drops the discarded take only once the new media exists. */
    data class CaptureSlot(val slotKey: String, val kind: String? = null) : FeedPackingCompleteEvent
    data class Answer(val questionId: String, val value: String) : FeedPackingCompleteEvent

    data object MarkDone : FeedPackingCompleteEvent
    data object SyncNow : FeedPackingCompleteEvent
    data object Back : FeedPackingCompleteEvent
    data class PreviewAction(val slotKey: String, val action: String) : FeedPackingCompleteEvent
}

@Composable
fun FeedPackingCompleteScreen(
    state: FeedPackingCompleteUiState,
    onEvent: (FeedPackingCompleteEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedPackingCompleteStatus.SYNCED ||
        state.result?.status == FeedPackingCompleteStatus.QUEUED
    val subtitle = listOf(state.sessionLabel, state.workflowLabel).filter { it.isNotBlank() }.joinToString(" · ")
    Scaffold(
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.shedLabel.ifBlank { stringResource(R.string.feed_packing_title) },
                eyebrow = "FEED PACKING",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = subtitle,
                onBack = { onEvent(FeedPackingCompleteEvent.Back) },
                actions = {
                    SyncIconButton(
                        isSyncing = state.isSyncing,
                        onSync = { onEvent(FeedPackingCompleteEvent.SyncNow) },
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
                FeedPackingStatusCard(
                    state = state,
                    committed = committed,
                    onRetrySubmit = { onEvent(FeedPackingCompleteEvent.MarkDone) },
                )
            }
            feedSopCardItems(
                card = state.card,
                locked = state.isFinalSubmitted,
                onCapture = { key, kind -> onEvent(FeedPackingCompleteEvent.CaptureSlot(key, kind)) },
                onPlaybackFailed = { },
                onPreviewAction = { key, action -> onEvent(FeedPackingCompleteEvent.PreviewAction(key, action)) },
                onAnswer = { id, v -> onEvent(FeedPackingCompleteEvent.Answer(id, v)) },
            )
        }
    }
}

@Composable
private fun FeedPackingStatusCard(
    state: FeedPackingCompleteUiState,
    committed: Boolean,
    onRetrySubmit: () -> Unit,
) {
    val completionFailed = state.result?.status == FeedPackingCompleteStatus.FAILED
    val statusText = when {
        committed -> stringResource(R.string.feed_pack_complete_submitted)
        completionFailed -> state.result.message
        else -> when (val progress = state.card.progress(state.submitEnabled)) {
            SopCardProgress.Ready -> stringResource(R.string.feed_pack_complete_ready_to_submit)
            // A required capture/answer not yet recorded is NAMED -- never "upload in progress" (Realme 2026-09-17).
            is SopCardProgress.StillNeeded -> stringResource(R.string.feed_slot_still_needed, progress.titles.joinToString(", "))
            SopCardProgress.Uploading -> stringResource(R.string.feed_pack_complete_waiting_sync)
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
            text = state.card.instruction.ifBlank { stringResource(R.string.feed_pack_complete_caption) },
            color = MeshaColors.Muted,
            style = MeshaType.body,
        )
        Text(text = statusText, color = tone, style = MeshaType.caption)
        if (state.submitEnabled) {
            FeedDistRetryButton(label = stringResource(R.string.feed_pack_complete_submit), onClick = onRetrySubmit)
        } else if (completionFailed) {
            FeedDistRetryButton(label = stringResource(R.string.feed_pack_complete_retry_submit), onClick = onRetrySubmit)
        }
    }
}

/** internal (not private): also used by FeedDistributionCompleteScreen and FeedTransportScreen for
 *  the same-shaped "already submitted" body. */
@Composable
internal fun FeedDistStatusCardBody(text: String, tone: Color) {
    androidx.compose.foundation.layout.Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
    ) {
        Text(text = text, color = tone, style = MeshaType.body)
    }
}
