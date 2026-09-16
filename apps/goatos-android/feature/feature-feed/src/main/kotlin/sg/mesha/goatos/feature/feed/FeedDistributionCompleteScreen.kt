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
enum class FeedDistributionProofStatus { EMPTY, QUEUED, UPLOADING, SYNCED, FAILED }

fun FeedDistributionProofStatus.isQueuedForSubmit(): Boolean =
    this == FeedDistributionProofStatus.QUEUED ||
        this == FeedDistributionProofStatus.UPLOADING ||
        this == FeedDistributionProofStatus.SYNCED

/** What a slot accepts, from the card: video | photo | either. */
object FeedSlotCaptureKind {
    const val VIDEO = "video"
    const val PHOTO = "photo"
    const val EITHER = "either"
}

/**
 * One capture slot of the card, with this phone's view of it. Backend-owned words (title, hint,
 * kind, required) come from the card; the rest is capture/upload state.
 */
@androidx.compose.runtime.Immutable
data class FeedDistributionSlotUi(
    /** The card's slot key = the proof register field_key stamped on the upload. */
    val slotKey: String,
    val title: String,
    val hint: String = "",
    /** video | photo | either -- what the slot accepts, from the card. */
    val captureKind: String = FeedSlotCaptureKind.VIDEO,
    /** False for an optional capture the crew may leave empty. */
    val required: Boolean = true,
    /** What was actually captured here (an `either` slot records the pick). */
    val capturedKind: String = FeedSlotCaptureKind.VIDEO,
    val isCapturing: Boolean = false,
    val captured: Boolean = false,
    val status: FeedDistributionProofStatus = FeedDistributionProofStatus.EMPTY,
    val message: String? = null,
    val previewPath: String? = null,
    val previewIdentity: String = "feed-distribution:$slotKey",
    val remoteUrl: String? = null,
) {
    /**
     * A slot is enabled by ITS OWN state alone -- never by a sibling's. That independence is the
     * collaboration contract (docs/product/feed-proof-collaboration.md) and is machine-checked.
     */
    val captureEnabled: Boolean
        get() = !isCapturing

    /** Ready to be named on the submit: captured and durable, or still uploading (the outbox waits). */
    val readyForSubmit: Boolean
        get() = captured && status.isQueuedForSubmit()
}

/** One authored question of the card, rendered verbatim; the answer is the operator's. */
@androidx.compose.runtime.Immutable
data class FeedDistributionQuestionUi(
    val id: String,
    val kind: String,
    val title: String,
    val hint: String = "",
    val required: Boolean = false,
    val options: List<Pair<String, String>> = emptyList(),
    val allowOther: Boolean = false,
    val unit: String = "",
    /** Hidden unless an earlier pick-one holds this value. */
    val onlyIfQuestion: String = "",
    val onlyIfValue: String = "",
)

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

    /** Every applicable required question has an answer. */
    val requiredAnswersGiven: Boolean
        get() = questions.filter { it.required && appliesTo(it) }.all { !answers[it.id].isNullOrBlank() }

    fun appliesTo(q: FeedDistributionQuestionUi): Boolean =
        q.onlyIfQuestion.isBlank() || answers[q.onlyIfQuestion] == q.onlyIfValue

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
        state.submitEnabled -> stringResource(R.string.feed_dist_ready_to_submit)
        state.compulsorySlotsFilled && !state.requiredAnswersGiven -> stringResource(R.string.feed_slot_answer_questions)
        state.anyCaptured -> stringResource(R.string.feed_dist_waiting_sync)
        else -> stringResource(R.string.feed_slot_need_all)
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
internal fun proofLabel(status: FeedDistributionProofStatus, recordedLabel: String): String = when (status) {
    FeedDistributionProofStatus.SYNCED -> stringResource(R.string.feed_dist_proof_synced)
    FeedDistributionProofStatus.UPLOADING -> stringResource(R.string.feed_dist_proof_uploading)
    FeedDistributionProofStatus.QUEUED -> stringResource(R.string.feed_dist_proof_waiting)
    FeedDistributionProofStatus.FAILED -> stringResource(R.string.feed_dist_proof_failed)
    FeedDistributionProofStatus.EMPTY -> recordedLabel
}

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
    /** A second verb for an `either` slot ("Take photo" beside "Record video"). */
    secondaryLabel: String? = null,
    onSecondaryClick: () -> Unit = {},
    onPreviewAction: (String) -> Unit = {},
) {
    val failed = status == FeedDistributionProofStatus.FAILED
    val synced = status == FeedDistributionProofStatus.SYNCED
    val uploading = loading || status == FeedDistributionProofStatus.UPLOADING
    val border = when {
        failed -> MeshaColors.Danger
        synced -> MeshaColors.Ok
        captured -> MeshaColors.Brand.copy(alpha = 0.5f)
        else -> MeshaColors.Hair
    }
    val iconBg = when {
        failed -> MeshaColors.Danger.copy(alpha = 0.14f)
        synced -> MeshaColors.Ok.copy(alpha = 0.14f)
        captured -> MeshaColors.Brand.copy(alpha = 0.14f)
        else -> MeshaColors.Surf2
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .clickable(enabled = enabled && !captured && !uploading, onClick = onClick)
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.size(42.dp).clip(RoundedCornerShape(14.dp)).background(iconBg),
            contentAlignment = Alignment.Center,
        ) {
            when {
                uploading -> CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp, color = MeshaColors.Brand)
                failed -> Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.Danger, modifier = Modifier.size(22.dp))
                synced -> Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Ok, modifier = Modifier.size(22.dp))
                captured -> Icon(icon, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(22.dp))
                else -> Icon(icon, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = when {
                    uploading -> loadingLabel
                    failed -> retryLabel
                    captured -> capturedLabel
                    else -> title
                },
                color = if (failed) MeshaColors.Danger else if (enabled || captured || uploading) MeshaColors.Ink else MeshaColors.Faint,
                style = MeshaType.cardTitle,
            )
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            var localPreviewFailed by remember(previewPath) { mutableStateOf(false) }
            val previewToShow = if (!previewPath.isNullOrBlank() && !localPreviewFailed) {
                previewPath
            } else {
                remotePreviewUrl ?: previewPath
            }
            if (!previewToShow.isNullOrBlank()) {
                FeedDistPreview(
                    path = previewToShow,
                    kind = previewKind,
                    mediaIdentity = previewIdentity,
                    playbackEnabled = playbackEnabled,
                    onPlaybackFailure = {
                        if (previewToShow == previewPath) {
                            localPreviewFailed = true
                        }
                        onPlaybackFailure()
                    },
                    onPreviewAction = onPreviewAction,
                )
                if (showAction) {
                    FeedDistRetryButton(label = if (failed) retryLabel else replaceLabel, enabled = enabled, onClick = onClick)
                }
            } else if (!uploading && showAction) {
                FeedDistRetryButton(label = if (captured || failed) replaceLabel else title, enabled = enabled, onClick = onClick)
            }
            if (secondaryLabel != null && !uploading && showAction) {
                FeedDistRetryButton(label = secondaryLabel, enabled = enabled, onClick = onSecondaryClick)
            }
            if (!message.isNullOrBlank()) {
                Text(text = message, color = MeshaColors.Faint, style = MeshaType.caption)
            }
        }
    }
}

@Composable
private fun FeedDistPreview(
    path: String,
    kind: FeedDistPreviewKind,
    mediaIdentity: String,
    playbackEnabled: Boolean = true,
    onPlaybackFailure: () -> Unit = {},
    onPreviewAction: (String) -> Unit = {},
) {
    ProofMediaPreview(
        path = path,
        kind = kind,
        mediaIdentity = mediaIdentity,
        onPlaybackFailure = onPlaybackFailure,
        expandable = true,
        onPreviewAction = onPreviewAction,
        playbackEnabled = playbackEnabled,
        inlineRemotePhoto = kind == FeedDistPreviewKind.Photo,
    )
}

@Composable
internal fun FeedDistRetryButton(label: String, enabled: Boolean = true, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (enabled) MeshaColors.OnBrand else MeshaColors.Muted,
        style = MeshaType.cta,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf2)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 13.dp),
    )
}


/**
 * One authored question of the card. Pick-one renders its choices as tappable chips; pick-many
 * toggles; number and text are a single field. Words are the card's, verbatim.
 */
@Composable
internal fun FeedSopQuestionCard(
    question: FeedDistributionQuestionUi,
    answer: String,
    otherText: String,
    enabled: Boolean,
    onAnswer: (String) -> Unit,
    onOther: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = question.title + if (question.required) " *" else "",
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
        )
        if (question.hint.isNotBlank()) {
            Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
        }
        when (question.kind) {
            "choice", "multi" -> {
                val picked = if (question.kind == "multi") answer.split(",").filter { it.isNotBlank() }.toSet() else setOf(answer)
                androidx.compose.foundation.layout.FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    question.options.forEach { (value, label) ->
                        val on = value in picked
                        Text(
                            text = label,
                            color = if (on) MeshaColors.OnBrand else MeshaColors.Ink,
                            style = MeshaType.cta,
                            modifier = Modifier
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(14.dp))
                                .background(if (on) MeshaColors.Brand else MeshaColors.Surf2)
                                .clickable(enabled = enabled) {
                                    if (question.kind == "multi") {
                                        val next = if (on) picked - value else picked + value
                                        onAnswer(question.options.map { it.first }.filter { it in next }.joinToString(","))
                                    } else {
                                        onAnswer(if (on) "" else value)
                                    }
                                }
                                .padding(horizontal = 12.dp, vertical = 10.dp),
                        )
                    }
                }
                if (question.kind == "choice" && question.allowOther && answer == "other") {
                    androidx.compose.material3.OutlinedTextField(
                        value = otherText,
                        onValueChange = onOther,
                        enabled = enabled,
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            "number" -> androidx.compose.material3.OutlinedTextField(
                value = answer,
                onValueChange = { v -> if (v.isEmpty() || v.matches(Regex("^-?\\d*(\\.\\d*)?$"))) onAnswer(v) },
                enabled = enabled,
                singleLine = true,
                suffix = if (question.unit.isNotBlank()) ({ Text(question.unit) }) else null,
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Decimal),
                modifier = Modifier.fillMaxWidth(),
            )
            else -> androidx.compose.material3.OutlinedTextField(
                value = answer,
                onValueChange = onAnswer,
                enabled = enabled,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
