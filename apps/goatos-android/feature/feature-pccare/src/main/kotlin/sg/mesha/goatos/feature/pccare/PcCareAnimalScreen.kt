package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderer; PcCareTaskViewModel (in :app) owns the pc_care_*
// AnalyticsEvents + CrashReporter wiring for scans, captures, and submits.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * ONE animal's video capture (the roster drill, Feed Direction's completion-screen shape): each
 * expected video is its own clearly-labeled card — what the clip is for is backend-owned copy —
 * with live capture/upload state and its own Record button. The trimming categories show the
 * before / while / after triple; the quick categories show their single video.
 */
@Composable
fun PcCareAnimalScreen(
    state: PcCareTaskUiState,
    onEvent: (PcCareTaskEvent) -> Unit = {},
) {
    val animal = state.focusAnimal
    Scaffold(
        containerColor = MeshaColors.PageBg,
        topBar = {
            MeshaScreenHeader(
                title = animal?.tagLabel ?: "",
                eyebrow = state.title.uppercase(),
                eyebrowColor = MeshaColors.BrandD,
                subtitle = listOf(state.locationDisplay, state.dateLabel)
                    .filter { it.isNotBlank() }
                    .joinToString(" · ")
                    .takeIf { it.isNotBlank() },
                onBack = { onEvent(PcCareTaskEvent.Back) },
            )
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (state.reworkReason.isNotBlank()) {
                item(key = "rework") { PcCareAnimalBanner(text = state.reworkReason, danger = true) }
            }
            if (state.isLocked && state.lockNotice.isNotBlank()) {
                item(key = "lock") { PcCareAnimalBanner(text = state.lockNotice, danger = false) }
            }
            if (animal != null) {
                if (animal.scannedByLine.isNotBlank()) {
                    item(key = "scanned_by") {
                        Text(
                            text = animal.scannedByLine,
                            color = MeshaColors.Muted,
                            style = MeshaType.caption,
                        )
                    }
                }
                items(
                    count = animal.slots.size,
                    key = { index -> animal.slots[index].fieldKey },
                ) { index ->
                    PcCareProofCard(
                        slot = animal.slots[index],
                        stepLabel = if (animal.slots.size > 1) "Video ${index + 1} of ${animal.slots.size}" else "",
                        locked = state.isLocked,
                        onRecord = { onEvent(PcCareTaskEvent.RecordSlot(animal.key, animal.slots[index].fieldKey)) },
                    )
                }
            }
            state.message?.let { message ->
                item(key = "message") {
                    Text(text = message, color = MeshaColors.Warn, style = MeshaType.caption)
                }
            }
        }
    }
}

@Composable
private fun PcCareAnimalBanner(text: String, danger: Boolean) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (danger) MeshaColors.DangerX else MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .padding(12.dp),
    ) {
        Text(
            text = text,
            color = if (danger) MeshaColors.Danger else MeshaColors.Ink,
            style = MeshaType.cardSubtitle,
        )
    }
}

/**
 * One video's card — the FeedDistProofAction shape: state-toned icon box (spinner while the
 * camera/upload runs, check when the clip is in, warn on failure), the backend-owned label plus
 * the backend-owned sentence saying what the clip must show, the live status line, and the
 * card's own Record / Record again button.
 */
@Composable
private fun PcCareProofCard(
    slot: PcCareSlotChipUi,
    stepLabel: String,
    locked: Boolean,
    onRecord: () -> Unit,
) {
    val working = slot.state == PcCareSlotState.WORKING
    val done = slot.state == PcCareSlotState.SYNCED || slot.state == PcCareSlotState.PEER
    val failed = slot.state == PcCareSlotState.FAILED
    val border = when {
        failed -> MeshaColors.Danger
        done -> MeshaColors.Ok
        working -> MeshaColors.Brand.copy(alpha = 0.5f)
        else -> MeshaColors.Hair
    }
    val iconBg = when {
        failed -> MeshaColors.Danger.copy(alpha = 0.14f)
        done -> MeshaColors.Ok.copy(alpha = 0.14f)
        working -> MeshaColors.Brand.copy(alpha = 0.14f)
        else -> MeshaColors.Surf2
    }
    val canRecord = !locked && slot.canRecord
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, border, RoundedCornerShape(18.dp))
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier.size(42.dp).clip(RoundedCornerShape(14.dp)).background(iconBg),
            contentAlignment = Alignment.Center,
        ) {
            when {
                working -> CircularProgressIndicator(
                    modifier = Modifier.size(18.dp),
                    strokeWidth = 2.dp,
                    color = MeshaColors.Brand,
                )
                failed -> Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.Danger, modifier = Modifier.size(22.dp))
                done -> Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Ok, modifier = Modifier.size(22.dp))
                else -> Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(22.dp))
            }
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            if (stepLabel.isNotBlank()) {
                Text(text = stepLabel, color = MeshaColors.Faint, style = MeshaType.caption)
            }
            // Backend-owned slot label, verbatim.
            Text(text = slot.label, color = MeshaColors.Ink, style = MeshaType.cardTitle)
            // Backend-owned sentence saying what this clip must show, verbatim.
            if (slot.description.isNotBlank()) {
                Text(text = slot.description, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            }
            if (slot.hintLabel.isNotBlank()) {
                Text(text = slot.hintLabel, color = MeshaColors.Faint, style = MeshaType.caption)
            }
            if (slot.statusLabel.isNotBlank()) {
                Text(
                    text = slot.statusLabel,
                    color = when {
                        failed -> MeshaColors.Danger
                        done -> MeshaColors.Ok
                        working -> MeshaColors.Warn
                        else -> MeshaColors.Muted
                    },
                    style = MeshaType.caption,
                )
            }
            if (canRecord && !working) {
                Text(
                    text = when (slot.state) {
                        PcCareSlotState.EMPTY -> "Record"
                        PcCareSlotState.FAILED -> "Record again"
                        else -> "Record again"
                    },
                    color = MeshaColors.OnBrand,
                    style = MeshaType.cta,
                    modifier = Modifier
                        .minimumInteractiveComponentSize()
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Brand)
                        .clickable(onClick = onRecord)
                        .padding(horizontal = 12.dp, vertical = 13.dp),
                )
            }
        }
    }
}
