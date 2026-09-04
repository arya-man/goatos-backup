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
            FastingPill(label = card.statusLabel, fg = fg, bg = bg)
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

/** Which of the shed's two clips a slot holds. */
enum class WeighingFastingSlotKind { FEED, WATER }

@Immutable
data class WeighingFastingSlotUi(
    val fieldKey: String,
    val kind: WeighingFastingSlotKind = WeighingFastingSlotKind.FEED,
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
)

@Immutable
data class WeighingFastingDetailUiState(
    /** Backend-owned subject label, verbatim — the screen title body. */
    val title: String = "",
    val dateLabel: String = "",
    val status: String = "",
    /** True once this shed card can no longer be recorded (submitted / approved). */
    val isReadOnly: Boolean = false,
    /** Farm copy for the lock ("Submitted — video in review", "Approved"); blank while open. */
    val lockNotice: String = "",
    /** The verifier's rejection sentence for THIS shed, verbatim; blank unless sent back. */
    val reworkReason: String = "",
    /** THIS shed's two live-camera slots. */
    val feedSlot: WeighingFastingSlotUi = WeighingFastingSlotUi(fieldKey = "", title = ""),
    val waterSlot: WeighingFastingSlotUi = WeighingFastingSlotUi(
        fieldKey = "",
        kind = WeighingFastingSlotKind.WATER,
        title = "",
    ),
    val submitEnabled: Boolean = false,
    /** Why submit is blocked, in farm language; blank when submittable or already sent. */
    val submitBlockedReason: String = "",
    /** True after the submit was durably queued — the screen flips to the sent state. */
    val submitQueued: Boolean = false,
    val message: String? = null,
    val isSyncing: Boolean = false,
)

sealed interface WeighingFastingDetailEvent {
    data class RecordSlot(val kind: WeighingFastingSlotKind) : WeighingFastingDetailEvent
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
            item(key = "removal-slot-feed") {
                FastingProofAction(
                    slot = state.feedSlot,
                    locked = state.isReadOnly || state.submitQueued,
                    onRecord = { onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.FEED)) },
                )
            }
            item(key = "removal-slot-water") {
                FastingProofAction(
                    slot = state.waterSlot,
                    locked = state.isReadOnly || state.submitQueued,
                    onRecord = { onEvent(WeighingFastingDetailEvent.RecordSlot(WeighingFastingSlotKind.WATER)) },
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
        Text(text = stringResource(R.string.weighing_removal_caption), color = MeshaColors.Muted, style = MeshaType.body)
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
private fun FastingProofAction(slot: WeighingFastingSlotUi, locked: Boolean, onRecord: () -> Unit) {
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
            .clickable(enabled = actionEnabled && !slot.captured, onClick = onRecord)
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
                slot.captured -> Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(22.dp))
                else -> Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(
                text = when {
                    slot.busy -> stringResource(R.string.weighing_removal_recording)
                    slot.captured || failed -> slot.title
                    else -> slot.title
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
                    kind = ProofMediaPreviewKind.Video,
                    onPlaybackFailure = {
                        if (previewToShow == slot.previewPath) localPreviewFailed = true
                    },
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
                FastingCtaButton(
                    label = if (slot.captured || failed) {
                        stringResource(R.string.weighing_removal_record_again)
                    } else {
                        stringResource(R.string.weighing_removal_record)
                    },
                    onClick = onRecord,
                )
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
