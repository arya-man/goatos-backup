package sg.mesha.goatos.feature.weighing

// telemetry:exempt pure stateless renderers; the fasting ViewModels in :app own the
// AnalyticsEventsWeighing + CrashReporter wiring for card open, slot capture, and submit.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.OutlinedTextField
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/** The card's status bucket — a TONE selector only; every visible string rides the row itself. */
enum class WeighingFastingTone { ACTION, IN_REVIEW, SENT_BACK, DONE }

/**
 * ONE feed & water removal card on the operator's weighing list — ONE CARD PER SHED (maintainer
 * correction #2, 2026-09-03).
 *
 * [title] is the BACKEND-OWNED subject_label rendered verbatim (e.g. "Remove feed & water ·
 * Castro 1"); [reworkReason] is the verifier's own sentence about THIS shed. The client composes
 * only the status chip's fixed farm wording and the "Tonight" date convenience — display only,
 * never a gate: a card that arrives is a card to act on, and the serve window is the server's.
 */
@Immutable
data class WeighingFastingCardUiRow(
    /** Stable FULL-identity list key ("removal|<fasting_task_id>|<campaign_shed_id>") — the
     *  (round, shed) pair is the grain; the task id alone repeats across a round's cards. */
    val uiKey: String,
    val fastingTaskId: String,
    val campaignShedId: String,
    /** Backend-owned card title, verbatim. */
    val title: String,
    /** "Tonight" when the removal evening is today in IST, else the removal date. Display only. */
    val dateLabel: String,
    val status: String,
    val statusLabel: String,
    val tone: WeighingFastingTone,
    /** The verifier's rejection sentence, verbatim; blank unless sent back. */
    val reworkReason: String = "",
    /** True for open/sent-back cards — tapping opens the recording screen. */
    val openable: Boolean = false,
)

/**
 * The removal cards section, rendered ABOVE the operator's shed work list. The section title only
 * appears when there are cards — an operator with no removal duty sees nothing extra.
 */
@Composable
fun WeighingFastingSection(
    cards: List<WeighingFastingCardUiRow>,
    onOpenCard: (WeighingFastingCardUiRow) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (cards.isEmpty()) return
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            text = stringResource(R.string.weighing_removal_section_title),
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
        )
        cards.forEach { card ->
            // FULL identity key — a round's cards share the task id, so the shed id is part of it.
            key(card.uiKey) {
                WeighingFastingCardRow(card = card, onOpen = { onOpenCard(card) })
            }
        }
    }
}

@Composable
private fun WeighingFastingCardRow(card: WeighingFastingCardUiRow, onOpen: () -> Unit) {
    val (fg, bg) = card.tone.colors()
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .then(if (card.openable) Modifier.clickable(onClick = onOpen) else Modifier)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            FeedWaterPill()
            if (card.tone != WeighingFastingTone.ACTION) {
                FastingPill(label = card.statusLabel, fg = fg, bg = bg)
            }
            Spacer(Modifier.weight(1f))
            FastingPill(label = card.dateLabel, fg = MeshaColors.Muted, bg = MeshaColors.Surf3)
        }
        Text(
            // Backend-owned subject label, verbatim.
            text = card.title,
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        if (card.reworkReason.isNotBlank()) {
            Text(
                // The verifier's own sentence, verbatim.
                text = card.reworkReason,
                color = MeshaColors.Danger,
                style = MeshaType.caption,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun FeedWaterPill() {
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(MeshaColors.BrandTint)
            .padding(horizontal = 9.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = MeshaIcons.Feed,
            contentDescription = null,
            tint = MeshaColors.BrandD,
            modifier = Modifier.size(15.dp),
        )
        Icon(
            imageVector = MeshaIcons.Water,
            contentDescription = null,
            tint = MeshaColors.BrandD,
            modifier = Modifier.size(15.dp),
        )
        Text(
            text = stringResource(R.string.weighing_removal_card_type),
            color = MeshaColors.BrandD,
            style = MeshaType.pillStrong,
            maxLines = 1,
        )
    }
}

@Composable
private fun WeighingFastingTone.colors(): Pair<Color, Color> = when (this) {
    WeighingFastingTone.ACTION -> MeshaColors.BrandD to MeshaColors.Surf3
    WeighingFastingTone.IN_REVIEW -> MeshaColors.Warn to MeshaColors.WarnX
    WeighingFastingTone.SENT_BACK -> MeshaColors.Danger to MeshaColors.DangerX
    WeighingFastingTone.DONE -> MeshaColors.Ok to MeshaColors.OkX
}

@Composable
private fun FastingPill(label: String, fg: Color, bg: Color) {
    Text(
        text = label,
        color = fg,
        style = MeshaType.pillStrong,
        maxLines = 1,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}

// ---------------------------------------------------------------------------
// Detail screen: ONE SHED — its two live-camera videos + a submit for this shed only
// ---------------------------------------------------------------------------

enum class WeighingFastingSlotStatus { EMPTY, QUEUED, UPLOADING, SYNCED, FAILED }

/**
 * The SEEDED two slots, kept as a name for the legacy pair (feed_video / water_video). Since
 * the weighing SOP authors the slot list (2026-09-15), a slot is identified by its
 * [WeighingFastingSlotUi.slotKey]; this enum only maps the two seeded keys for older callers.
 */
enum class WeighingFastingSlotKind(val slotKey: String) {
    FEED("feed_video"),
    WATER("water_video");

    companion object {
        fun forKey(slotKey: String): WeighingFastingSlotKind? = entries.firstOrNull { it.slotKey == slotKey }
    }
}

/** What a slot captures, as the SOP authored it. */
object WeighingFastingCaptureKind {
    const val VIDEO = "video"
    const val PHOTO = "photo"
    const val EITHER = "either"
}

@Immutable
data class WeighingFastingSlotUi(
    val fieldKey: String,
    val kind: WeighingFastingSlotKind = WeighingFastingSlotKind.FEED,
    /** The SOP's slot key (feed_video, water_video, or any authored key). */
    val slotKey: String = kind.slotKey,
    /** video | photo | either -- what the slot accepts, from the SOP. */
    val captureKind: String = WeighingFastingCaptureKind.VIDEO,
    /** False for an optional capture the operator may leave empty. */
    val required: Boolean = true,
    /** What was actually captured here: video or photo (an `either` slot records the pick). */
    val capturedKind: String = WeighingFastingCaptureKind.VIDEO,
    val title: String,
    /** What the clip must show, in farm words — the row's second line. */
    val hint: String = "",
    val captured: Boolean = false,
    val status: WeighingFastingSlotStatus = WeighingFastingSlotStatus.EMPTY,
    /** Farm-worded live status line ("Video on its way", "Video sent"); blank before capture. */
    val statusLabel: String = "",
    val busy: Boolean = false,
    /** Local file path of THIS device's recording — the inline playable preview. */
    val previewPath: String? = null,
    /** Signed server URL for an already-submitted clip (reinstall / read-only card). */
    val remoteUrl: String? = null,
    /** Local Room proof_capture row id for this device's recording. */
    val localProofRowId: String? = null,
    /** Server proof id/ref when the preview is backed by an already-submitted clip. */
    val serverProofId: String? = null,
)

/**
 * WEIGHING SOP (maintainer decision 2026-09-15): one authored question on the removal card,
 * rendered verbatim from the task's pinned SOP version. Kinds: choice / multi / text / number.
 */
@Immutable
data class WeighingFastingQuestionUi(
    val id: String,
    val kind: String,
    val title: String,
    val hint: String = "",
    val required: Boolean = false,
    val options: List<Pair<String, String>> = emptyList(),
    val allowOther: Boolean = false,
    val unit: String = "",
    /** Farm-worded range line ("0–50") for a number question; blank when unbounded. */
    val rangeLabel: String = "",
    /** False when an "ask only when" condition on an earlier answer is not met; hidden then. */
    val applies: Boolean = true,
)

/** The operator's answer to one question, as typed/picked. Multi answers are the ticked values. */
@Immutable
data class WeighingFastingAnswerUi(
    val value: String = "",
    val values: List<String> = emptyList(),
    val otherText: String = "",
)

@Immutable
data class WeighingFastingDetailUiState(
    /** Backend-owned subject label, verbatim — the screen title body. */
    val title: String = "",
    /** The SOP's operator instruction for this task, verbatim; blank when the SOP has none. */
    val instruction: String = "",
    /** The SOP's authored questions, in order, with their current answers. */
    val questions: List<WeighingFastingQuestionUi> = emptyList(),
    val answers: Map<String, WeighingFastingAnswerUi> = emptyMap(),
    val dateLabel: String = "",
    val status: String = "",
    /** True once this shed card can no longer be recorded (submitted / approved). */
    val isReadOnly: Boolean = false,
    /** Farm copy for the lock ("Submitted — video in review", "Approved"); blank while open. */
    val lockNotice: String = "",
    /** The verifier's rejection sentence for THIS shed, verbatim; blank unless sent back. */
    val reworkReason: String = "",
    /**
     * THIS shed's capture slots, in the SOP's order (WEIGHING SOP: any number, each a video, a
     * photo or either, compulsory or optional). The seeded document has the feed and water clips.
     */
    val slots: List<WeighingFastingSlotUi> = emptyList(),

    val submitEnabled: Boolean = false,
    /** Why submit is blocked, in farm language; blank when submittable or already sent. */
    val submitBlockedReason: String = "",
    /** True after the submit was durably queued — the screen flips to the sent state. */
    val submitQueued: Boolean = false,
    val message: String? = null,
    val isSyncing: Boolean = false,
) {
    /** The seeded two slots by name, for callers that predate authored slots. */
    val feedSlot: WeighingFastingSlotUi get() = slotOrEmpty(WeighingFastingSlotKind.FEED)
    val waterSlot: WeighingFastingSlotUi get() = slotOrEmpty(WeighingFastingSlotKind.WATER)

    private fun slotOrEmpty(kind: WeighingFastingSlotKind): WeighingFastingSlotUi =
        slots.firstOrNull { it.slotKey == kind.slotKey } ?: WeighingFastingSlotUi(fieldKey = "", kind = kind, title = "")
}

sealed interface WeighingFastingDetailEvent {
    /** Record a slot; [photo] picks the camera's photo mode for an `either` slot. */
    data class RecordSlot(val slotKey: String, val photo: Boolean = false) : WeighingFastingDetailEvent {
        companion object {
            operator fun invoke(kind: WeighingFastingSlotKind): RecordSlot = RecordSlot(kind.slotKey)
        }
    }
    data class PreviewAction(val slotKey: String, val action: String) : WeighingFastingDetailEvent {
        companion object {
            operator fun invoke(kind: WeighingFastingSlotKind, action: String): PreviewAction = PreviewAction(kind.slotKey, action)
        }
    }
    /** WEIGHING SOP: an answer to one authored question -- a pick, a tick, typed text or a number. */
    data class SetAnswer(val questionId: String, val value: String) : WeighingFastingDetailEvent
    data class ToggleAnswer(val questionId: String, val value: String) : WeighingFastingDetailEvent
    data class SetOtherText(val questionId: String, val text: String) : WeighingFastingDetailEvent
    data object Submit : WeighingFastingDetailEvent
    data object Refresh : WeighingFastingDetailEvent
    data object DismissMessage : WeighingFastingDetailEvent
}

/**
 * The removal recording screen for ONE SHED (maintainer correction #2, 2026-09-03: one card per
 * shed, one submit per shed): the shed's two live-camera video slots and a submit for THIS shed
 * only — no shed sections, no round roll-up. A hosted NavHost destination with Up/Back and no
 * root chrome; its entry point is the shed's card on the weighing list.
 */
@Composable
fun WeighingFastingDetailScreen(
    state: WeighingFastingDetailUiState,
    onEvent: (WeighingFastingDetailEvent) -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(WeighingFastingDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = stringResource(R.string.weighing_removal_screen_title),
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            subtitle = listOf(state.title, state.dateLabel).filter { it.isNotBlank() }.joinToString(" · "),
            onBack = onBack,
            actions = {
                SyncIconButton(
                    isSyncing = state.isSyncing,
                    onSync = { onEvent(WeighingFastingDetailEvent.Refresh) },
                    contentDescription = stringResource(R.string.weighing_refresh),
                )
            },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (state.reworkReason.isNotBlank()) {
                item(key = "removal-rework") {
                    Text(
                        // The verifier's own sentence about THIS shed, verbatim.
                        text = state.reworkReason,
                        color = MeshaColors.Danger,
                        style = MeshaType.cardSubtitle,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .background(MeshaColors.DangerX)
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                    )
                }
            }
            item(key = "removal-status") {
                FastingStatusCard(state = state, onSubmit = { onEvent(WeighingFastingDetailEvent.Submit) })
            }
            // WEIGHING SOP: the authored questions sit between the status and the two clips, one
            // card each, and only the ones whose "ask only when" holds are shown.
            items(state.questions.filter { it.applies }, key = { "removal-question-${it.id}" }) { question ->
                FastingQuestionCard(
                    question = question,
                    answer = state.answers[question.id] ?: WeighingFastingAnswerUi(),
                    locked = state.isReadOnly || state.submitQueued,
                    onEvent = onEvent,
                )
            }
            items(state.slots, key = { "removal-slot-${it.slotKey}" }) { slot ->
                FastingProofAction(
                    slot = slot,
                    locked = state.isReadOnly || state.submitQueued,
                    onRecord = { photo -> onEvent(WeighingFastingDetailEvent.RecordSlot(slot.slotKey, photo)) },
                    onPreviewAction = { action ->
                        onEvent(WeighingFastingDetailEvent.PreviewAction(slot.slotKey, action))
                    },
                )
            }
            state.message?.takeIf { it.isNotBlank() }?.let { message ->
                item(key = "removal-message") {
                    Text(
                        text = message,
                        color = MeshaColors.Warn,
                        style = MeshaType.cardSubtitle,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .background(MeshaColors.WarnX)
                            .clickable { onEvent(WeighingFastingDetailEvent.DismissMessage) }
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                    )
                }
            }
            item(key = "removal-tail") { Spacer(Modifier.height(48.dp)) }
        }
    }
}

/**
 * The card that carries the round's instruction, its live status line and the SUBMIT action —
 * the exact [FeedDistStatusCard] shape the feed-distribution completion screen uses, so the two
 * gated-proof screens read identically to an operator.
 */
@Composable
private fun FastingStatusCard(state: WeighingFastingDetailUiState, onSubmit: () -> Unit) {
    val statusText = when {
        state.lockNotice.isNotBlank() -> state.lockNotice
        state.submitQueued -> stringResource(R.string.weighing_removal_submitted_line)
        state.submitEnabled -> stringResource(R.string.weighing_removal_ready)
        state.submitBlockedReason.isNotBlank() -> state.submitBlockedReason
        else -> stringResource(R.string.weighing_removal_status_open)
    }
    val tone = when {
        state.status == "completed" -> MeshaColors.Ok
        state.submitQueued -> MeshaColors.Ok
        state.status == "pending_verification" -> MeshaColors.Warn
        state.submitEnabled -> MeshaColors.BrandD
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
        // The SOP's own instruction when the task carries one; the app's caption otherwise.
        Text(
            text = state.instruction.ifBlank { stringResource(R.string.weighing_removal_caption) },
            color = MeshaColors.Muted,
            style = MeshaType.body,
        )
        Text(text = statusText, color = tone, style = MeshaType.caption)
        if (state.submitEnabled) {
            FastingCtaButton(label = stringResource(R.string.weighing_removal_submit), onClick = onSubmit)
        }
    }
}

/**
 * One proof slot in the exact [FeedDistProofAction] shape: state-tinted icon tile, state-driven
 * title, hint line, an inline playable [ProofMediaPreview] once a clip exists, and a re-record
 * action — so recording a removal looks and works exactly like recording a feed distribution.
 */
@Composable
private fun FastingProofAction(
    slot: WeighingFastingSlotUi,
    locked: Boolean,
    onRecord: (photo: Boolean) -> Unit,
    onPreviewAction: (String) -> Unit = {},
) {
    val photoOnly = slot.captureKind == WeighingFastingCaptureKind.PHOTO
    val either = slot.captureKind == WeighingFastingCaptureKind.EITHER
    val previewKind = if (slot.capturedKind == WeighingFastingCaptureKind.PHOTO) ProofMediaPreviewKind.Photo else ProofMediaPreviewKind.Video
    val failed = slot.status == WeighingFastingSlotStatus.FAILED
    val synced = slot.status == WeighingFastingSlotStatus.SYNCED
    val uploading = slot.busy || slot.status == WeighingFastingSlotStatus.UPLOADING
    val border = when {
        failed -> MeshaColors.Danger
        synced -> MeshaColors.Ok
        slot.captured -> MeshaColors.Brand.copy(alpha = 0.5f)
        else -> MeshaColors.Hair
    }
    val iconBg = when {
        failed -> MeshaColors.Danger.copy(alpha = 0.14f)
        synced -> MeshaColors.Ok.copy(alpha = 0.14f)
        slot.captured -> MeshaColors.Brand.copy(alpha = 0.14f)
        else -> MeshaColors.Surf2
    }
    val actionEnabled = !locked && !uploading
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .clickable(enabled = actionEnabled && !slot.captured, onClick = { onRecord(photoOnly) })
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
                slot.captured -> Icon(if (previewKind == ProofMediaPreviewKind.Photo) MeshaIcons.Camera else MeshaIcons.Video, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(22.dp))
                else -> Icon(if (photoOnly) MeshaIcons.Camera else MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = when {
                    slot.busy -> stringResource(R.string.weighing_removal_recording)
                    slot.required -> slot.title
                    else -> "${slot.title} · ${stringResource(R.string.weighing_removal_question_optional)}"
                },
                color = if (failed) MeshaColors.Danger else if (actionEnabled || slot.captured || uploading) MeshaColors.Ink else MeshaColors.Faint,
                style = MeshaType.cardTitle,
            )
            if (slot.hint.isNotBlank()) {
                Text(text = slot.hint, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            }
            var localPreviewFailed by remember(slot.previewPath) { mutableStateOf(false) }
            val previewToShow = if (!slot.previewPath.isNullOrBlank() && !localPreviewFailed) {
                slot.previewPath
            } else {
                slot.remoteUrl ?: slot.previewPath
            }
            if (!previewToShow.isNullOrBlank()) {
                ProofMediaPreview(
                    path = previewToShow,
                    kind = previewKind,
                    mediaIdentity = slot.serverProofId?.takeIf { it.isNotBlank() }
                        ?: slot.localProofRowId?.takeIf { it.isNotBlank() }
                        ?: slot.fieldKey,
                    onPlaybackFailure = {
                        if (previewToShow == slot.previewPath) localPreviewFailed = true
                    },
                    onPreviewAction = onPreviewAction,
                )
            }
            if (slot.statusLabel.isNotBlank()) {
                Text(
                    text = slot.statusLabel,
                    color = when {
                        synced -> MeshaColors.Ok
                        failed -> MeshaColors.Danger
                        else -> MeshaColors.Faint
                    },
                    style = MeshaType.caption,
                )
            }
            if (!locked && !uploading) {
                val again = slot.captured || failed
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (!photoOnly) {
                        FastingCtaButton(
                            label = if (again) stringResource(R.string.weighing_removal_record_again) else stringResource(R.string.weighing_removal_record),
                            onClick = { onRecord(false) },
                        )
                    }
                    if (photoOnly || either) {
                        FastingCtaButton(
                            label = if (again) stringResource(R.string.weighing_removal_photo_again) else stringResource(R.string.weighing_removal_photo),
                            onClick = { onRecord(true) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun FastingCtaButton(label: String, enabled: Boolean = true, onClick: () -> Unit) {
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
 * One authored removal question (WEIGHING SOP): pick-one as radio rows, pick-many as tick rows,
 * text and number as a field. Locked once the card is submitted; the answers then render as
 * recorded.
 */
@Composable
private fun FastingQuestionCard(
    question: WeighingFastingQuestionUi,
    answer: WeighingFastingAnswerUi,
    locked: Boolean,
    onEvent: (WeighingFastingDetailEvent) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = if (question.required) question.title else "${question.title} · ${stringResource(R.string.weighing_removal_question_optional)}",
            color = MeshaColors.Ink,
            style = MeshaType.listTitle,
        )
        if (question.hint.isNotBlank()) {
            Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        when (question.kind) {
            "choice", "multi" -> {
                question.options.forEach { (value, label) ->
                    val picked = if (question.kind == "choice") answer.value == value else value in answer.values
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .background(if (picked) MeshaColors.BrandTint else MeshaColors.Surf2)
                            .clickable(enabled = !locked) {
                                onEvent(
                                    if (question.kind == "choice") {
                                        WeighingFastingDetailEvent.SetAnswer(question.id, value)
                                    } else {
                                        WeighingFastingDetailEvent.ToggleAnswer(question.id, value)
                                    },
                                )
                            }
                            .padding(horizontal = 12.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        Text(
                            text = label,
                            color = if (picked) MeshaColors.BrandD else MeshaColors.Ink,
                            style = MeshaType.body,
                            modifier = Modifier.weight(1f),
                        )
                    }
                }
                if (question.kind == "choice" && question.allowOther && answer.value == "other") {
                    OutlinedTextField(
                        value = answer.otherText,
                        onValueChange = { onEvent(WeighingFastingDetailEvent.SetOtherText(question.id, it)) },
                        enabled = !locked,
                        singleLine = true,
                        placeholder = { Text(stringResource(R.string.weighing_removal_question_other_hint)) },
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            "number" -> {
                OutlinedTextField(
                    value = answer.value,
                    onValueChange = { onEvent(WeighingFastingDetailEvent.SetAnswer(question.id, it)) },
                    enabled = !locked,
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    suffix = if (question.unit.isNotBlank()) ({ Text(question.unit) }) else null,
                    supportingText = if (question.rangeLabel.isNotBlank()) ({ Text(question.rangeLabel) }) else null,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            else -> {
                OutlinedTextField(
                    value = answer.value,
                    onValueChange = { onEvent(WeighingFastingDetailEvent.SetAnswer(question.id, it)) },
                    enabled = !locked,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}
