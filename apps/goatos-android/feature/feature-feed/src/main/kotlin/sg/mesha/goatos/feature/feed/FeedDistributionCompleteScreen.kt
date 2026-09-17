package sg.mesha.goatos.feature.feed

// telemetry:exempt presentational screen — analytics (AnalyticsPort.track) and Crashlytics
// (CrashReporter.recordException) are wired in FeedDistributionCompleteViewModel (:app), which owns
// every capture/enqueue side effect this screen triggers via events.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.media3.common.util.UnstableApi
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.sop.SopProofAction
import sg.mesha.goatos.core.ui.sop.SopProofStatus
import sg.mesha.goatos.core.ui.sop.SopQuestionCard
import sg.mesha.goatos.core.ui.sop.SopCardUi
import sg.mesha.goatos.core.ui.sop.SopQuestionUi
import sg.mesha.goatos.core.ui.sop.SopRetryButton
import sg.mesha.goatos.core.ui.sop.SopSlotCaptureKind
import sg.mesha.goatos.core.ui.sop.SopSlotUi
import sg.mesha.goatos.core.ui.sop.isQueuedForSubmit as sopQueuedForSubmit
import sg.mesha.goatos.core.ui.sop.sopProofLabel
import sg.mesha.goatos.core.ui.sop.SopCardProgress
import sg.mesha.goatos.core.ui.sop.progress

/**
 * Feed-DISTRIBUTION completion detail (L2), reached by tapping a shed-session row on Feed DIRECTION.
 *
 * FEED SOP (maintainer decision 2026-09-16): the captures this screen asks for are the CARD the
 * sheet was issued under -- served by the backend as [FeedDistributionSlotUi] rows in card order
 * (today: feed weight photo, feed video, water video; tomorrow whatever the farm publishes). The
 * screen holds no slot list of its own. Slots stay INDEPENDENT and PARALLEL: any operator on the
 * park records any slot in any order, a slot's enablement depends only on its own state, and
 * submit is session-level once every COMPULSORY slot is filled -- by any mix of phones.
 *
 * This is separate from the packing-proof flow.
 */

enum class FeedDistributionStatus { QUEUED, SYNCED, FAILED }
/**
 * The SOP card models moved to core-ui (sg.mesha.goatos.core.ui.sop, SOP → operator parity,
 * 2026-09-16) so the herd operations capture forms render the same card; the feed names stay as
 * typealiases so every feed screen, ViewModel, test and guard keeps reading as before.
 */
typealias FeedDistributionProofStatus = SopProofStatus

fun FeedDistributionProofStatus.isQueuedForSubmit(): Boolean = sopQueuedForSubmit()

/** What a slot accepts, from the card: video | photo | either. */
typealias FeedSlotCaptureKind = SopSlotCaptureKind

typealias FeedDistributionSlotUi = SopSlotUi

typealias FeedDistributionQuestionUi = SopQuestionUi

@androidx.compose.runtime.Immutable
data class FeedDistributionResultUi(val status: FeedDistributionStatus, val message: String)

@androidx.compose.runtime.Immutable
data class FeedDistributionUiState(
    val shedLabel: String = "",
    val sessionLabel: String = "",
    val workflowLabel: String = "",
    /** The card's instruction, verbatim; blank before the card arrives. */
    val instruction: String = "",
    /** The card's slots in card order. Empty only before the card (or the seeded fallback) is known. */
    val slots: List<FeedDistributionSlotUi> = emptyList(),
    val questions: List<FeedDistributionQuestionUi> = emptyList(),
    /** {question id: answer} as the operator typed / picked it (multi = comma-joined values). */
    val answers: Map<String, String> = emptyMap(),
    val canComplete: Boolean = false,
    val isSyncing: Boolean = false,
    val result: FeedDistributionResultUi? = null,
    /**
     * The shed-session already went to the verifier (or was approved/rejected) somewhere else, so
     * there is nothing to record here. Backend-owned: derived from the row's lifecycle bucket, NOT
     * from local capture-draft presence — a reinstall wipes the draft, which previously left an
     * empty, fully-editable form for work already submitted (STG 2026-08-09; mirrors
     * [FeedPackingCompleteUiState.alreadySubmitted]).
     */
    val alreadySubmitted: Boolean = false,
) {
    val isFinalSubmitted: Boolean
        get() = alreadySubmitted ||
            result?.status == FeedDistributionStatus.SYNCED || result?.status == FeedDistributionStatus.QUEUED

    val anyCaptured: Boolean
        get() = slots.any { it.captured }

    val anyCapturing: Boolean
        get() = slots.any { it.isCapturing }

    /** Every compulsory slot is captured (any phone). Optional slots never gate the submit. */
    val compulsorySlotsFilled: Boolean
        get() = slots.isNotEmpty() && slots.filter { it.required }.all { it.captured }

    /** The answers judged exactly as the shared SOP card (and the server) judge them. */
    private val answerCard: SopCardUi get() = SopCardUi(questions = questions, answers = answers)

    /** Every question the card asks is answered the way the server will accept. */
    val requiredAnswersGiven: Boolean
        get() = answerCard.requiredAnswersGiven

    fun appliesTo(q: FeedDistributionQuestionUi): Boolean = answerCard.appliesTo(q)

    /** Proof uploads may still be queued; the completion outbox resolves them before syncing. */
    val submitEnabled: Boolean
        get() = canComplete &&
            slots.isNotEmpty() &&
            slots.filter { it.required }.all { it.readyForSubmit } &&
            slots.filter { it.captured }.all { it.status.isQueuedForSubmit() } &&
            requiredAnswersGiven &&
            !isFinalSubmitted

    fun slot(slotKey: String): FeedDistributionSlotUi? = slots.firstOrNull { it.slotKey == slotKey }
}

sealed interface FeedDistributionEvent {
    /** Capture one slot with the LIVE in-app camera; [kind] picks video or photo for an `either`
     *  slot, null = the slot's own kind. */
    data class CaptureSlot(val slotKey: String, val kind: String? = null) : FeedDistributionEvent
    data class Answer(val questionId: String, val value: String) : FeedDistributionEvent
    data object MarkDone : FeedDistributionEvent
    data object SyncNow : FeedDistributionEvent
    data object Back : FeedDistributionEvent
    data class SlotPlaybackFailed(val slotKey: String) : FeedDistributionEvent
    data class ProofPreviewAction(val fieldKey: String, val action: String) : FeedDistributionEvent
}

@Composable
fun FeedDistributionCompleteScreen(
    state: FeedDistributionUiState,
    onEvent: (FeedDistributionEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedDistributionStatus.SYNCED ||
        state.result?.status == FeedDistributionStatus.QUEUED
    val subtitle = listOf(state.sessionLabel, state.workflowLabel)
        .filter { it.isNotBlank() }
        .joinToString(" · ")

    Scaffold(
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = state.shedLabel.ifBlank { "Feed distribution" },
                eyebrow = "FEED DISTRIBUTION",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = subtitle,
                onBack = { onEvent(FeedDistributionEvent.Back) },
                actions = {
                    SyncIconButton(
                        isSyncing = state.isSyncing,
                        onSync = { onEvent(FeedDistributionEvent.SyncNow) },
                    )
                },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // ALREADY SUBMITTED: the session went to the verifier (or was decided) elsewhere, so
            // there is nothing to record. The captured proofs stay VISIBLE below (read-only —
            // every capture path is gated on isFinalSubmitted): operators still need to see WHAT
            // was submitted and by whom; only the actions disappear.
            if (state.alreadySubmitted) {
                item {
                    FeedDistStatusCardBody(
                        text = stringResource(R.string.feed_dist_session_submitted_body),
                        tone = MeshaColors.Muted,
                    )
                }
            } else {
                item {
                    FeedDistStatusCard(
                        state = state,
                        committed = committed,
                        onRetrySubmit = { onEvent(FeedDistributionEvent.MarkDone) },
                    )
                }
            }
            // THE CARD'S SLOTS and QUESTIONS, in card order (shared with packing/wastage/transport).
            // The captured proofs stay VISIBLE when locked — operators still need to see WHAT was
            // submitted and by whom; only the actions disappear.
            feedSopCardItems(
                card = FeedSopCardUi(state.instruction, state.slots, state.questions, state.answers),
                locked = state.isFinalSubmitted,
                onCapture = { key, kind -> onEvent(FeedDistributionEvent.CaptureSlot(key, kind)) },
                onPlaybackFailed = { key -> onEvent(FeedDistributionEvent.SlotPlaybackFailed(key)) },
                onPreviewAction = { key, action -> onEvent(FeedDistributionEvent.ProofPreviewAction(key, action)) },
                onAnswer = { id, v -> onEvent(FeedDistributionEvent.Answer(id, v)) },
            )
        }
    }
}

@Composable
private fun FeedDistStatusCard(
    state: FeedDistributionUiState,
    committed: Boolean,
    onRetrySubmit: () -> Unit,
) {
    val completionFailed = state.result?.status == FeedDistributionStatus.FAILED
    val statusText = when {
        committed -> stringResource(R.string.feed_dist_submitted)
        completionFailed -> state.result.message
        else -> when (val progress = FeedSopCardUi(state.instruction, state.slots, state.questions, state.answers).progress(state.submitEnabled)) {
            SopCardProgress.Ready -> stringResource(R.string.feed_dist_ready_to_submit)
            // A required capture/answer not yet recorded is NAMED -- never "upload in progress" (Realme 2026-09-17).
            is SopCardProgress.StillNeeded -> stringResource(R.string.feed_slot_still_needed, progress.titles.joinToString(", "))
            SopCardProgress.Uploading -> stringResource(R.string.feed_dist_waiting_sync)
            SopCardProgress.NothingCaptured -> stringResource(R.string.feed_slot_need_all)
        }
    }
    val tone = when {
        committed -> MeshaColors.Ok
        completionFailed -> MeshaColors.Danger
        state.compulsorySlotsFilled -> MeshaColors.BrandD
        else -> MeshaColors.Muted
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        // The card's own instruction leads; the app's generic caption is the fallback before the
        // card is known (backend-owned words, never a slot list of the app's own).
        Text(text = state.instruction.ifBlank { stringResource(R.string.feed_dist_caption) }, color = MeshaColors.Muted, style = MeshaType.body)
        Text(text = statusText, color = tone, style = MeshaType.caption)
        if (state.submitEnabled) {
            FeedDistRetryButton(label = stringResource(R.string.feed_dist_submit), onClick = onRetrySubmit)
        } else if (completionFailed) {
            FeedDistRetryButton(label = stringResource(R.string.feed_dist_retry_submit), onClick = onRetrySubmit)
        }
    }
}

@Composable
internal fun proofLabel(status: FeedDistributionProofStatus, recordedLabel: String): String = sopProofLabel(status, recordedLabel)

typealias FeedDistPreviewKind = ProofMediaPreviewKind

@Composable
internal fun FeedDistProofAction(
    title: String,
    subtitle: String,
    icon: ImageVector,
    captured: Boolean,
    status: FeedDistributionProofStatus,
    previewPath: String?,
    previewIdentity: String,
    previewKind: FeedDistPreviewKind,
    capturedLabel: String,
    loading: Boolean,
    loadingLabel: String,
    retryLabel: String,
    replaceLabel: String,
    enabled: Boolean,
    message: String?,
    onClick: () -> Unit,
    remotePreviewUrl: String? = null,
    playbackEnabled: Boolean = true,
    onPlaybackFailure: () -> Unit = {},
    showAction: Boolean = true,
    secondaryLabel: String? = null,
    onSecondaryClick: () -> Unit = {},
    onPreviewAction: (String) -> Unit = {},
) = SopProofAction(
    title = title, subtitle = subtitle, icon = icon, captured = captured, status = status,
    previewPath = previewPath, previewIdentity = previewIdentity, previewKind = previewKind,
    capturedLabel = capturedLabel, loading = loading, loadingLabel = loadingLabel, retryLabel = retryLabel,
    replaceLabel = replaceLabel, enabled = enabled, message = message, onClick = onClick,
    remotePreviewUrl = remotePreviewUrl, playbackEnabled = playbackEnabled, onPlaybackFailure = onPlaybackFailure,
    showAction = showAction, secondaryLabel = secondaryLabel, onSecondaryClick = onSecondaryClick, onPreviewAction = onPreviewAction,
)

@Composable
internal fun FeedDistRetryButton(label: String, enabled: Boolean = true, onClick: () -> Unit) =
    SopRetryButton(label = label, enabled = enabled, onClick = onClick)

@Composable
internal fun FeedSopQuestionCard(
    question: FeedDistributionQuestionUi,
    answer: String,
    otherText: String,
    enabled: Boolean,
    onAnswer: (String) -> Unit,
    onOther: (String) -> Unit,
) = SopQuestionCard(question = question, answer = answer, otherText = otherText, enabled = enabled, onAnswer = onAnswer, onOther = onOther)
