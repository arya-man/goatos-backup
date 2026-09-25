package sg.mesha.goatos.feature.workboard

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the
// work_board_* AnalyticsEventsWorkBoard + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/** Card surface shared by the list rows and the detail sections — the pen-visit card. */
@Composable
internal fun workBoardCardModifier(onClick: (() -> Unit)? = null): Modifier {
    var base = Modifier
        .fillMaxWidth()
        .padding(horizontal = 16.dp)
        .clip(RoundedCornerShape(16.dp))
        .background(MeshaColors.Surf)
        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    if (onClick != null) {
        base = base.clickable(onClick = onClick)
    }
    return base.padding(16.dp)
}

/** A small tinted chip; blank copy renders nothing at all. */
@Composable
internal fun WorkBoardChip(label: String, accent: Color, modifier: Modifier = Modifier) {
    if (label.isBlank()) return
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(8.dp))
            .background(accent.copy(alpha = 0.16f))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(text = label, color = accent, style = MeshaType.pillStrong, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** One selectable filter chip with its whole-filter count. */
@Composable
internal fun WorkBoardFilterChip(label: String, count: Int, selected: Boolean, onClick: () -> Unit) {
    val background = if (selected) MeshaColors.BrandTint else MeshaColors.Surf
    val border = if (selected) MeshaColors.BrandD else MeshaColors.Hair
    val labelColor = if (selected) MeshaColors.BrandD else MeshaColors.Muted
    Row(
        modifier = Modifier
            .heightIn(min = 40.dp)
            .clip(RoundedCornerShape(999.dp))
            .background(background)
            .border(1.dp, border, RoundedCornerShape(999.dp))
            .selectable(selected = selected, role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = label, color = labelColor, style = MeshaType.pill)
        Text(text = count.toString(), color = labelColor.copy(alpha = 0.75f), style = MeshaType.pill)
    }
}

/**
 * A horizontally scrolling row of filter chips over a BOUNDED vocabulary (the four lanes, or the
 * handful of modules the caller may see). Never a picker for sheds/animals/dates.
 */
@Composable
internal fun WorkBoardChipRow(
    chips: List<WorkBoardChipUi>,
    label: @Composable (String) -> String,
    onSelect: (String) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        chips.forEach { chip -> // compose-guard:ignore: bounded vocabulary — four lanes or at most ten module keys
            WorkBoardFilterChip(label = label(chip.key), count = chip.count, selected = chip.selected) {
                onSelect(chip.key)
            }
        }
    }
}

/** One summary tile: a whole-filter count with its label. */
@Composable
internal fun WorkBoardStatTile(label: String, value: String, accent: Color, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxHeight()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(text = value, color = accent, style = MeshaType.bodyStrong)
        // Two lines so "Needs attention" is never cut to "Needs a…" on a 360dp phone.
        Text(text = label, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 2, overflow = TextOverflow.Ellipsis)
    }
}

/** Primary (filled) action. Disabled renders dead, never hidden. */
@Composable
internal fun WorkBoardPrimaryButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
) {
    Row(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterHorizontally),
    ) {
        val tint = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint
        if (icon != null) {
            Icon(imageVector = icon, contentDescription = null, tint = tint, modifier = Modifier.size(18.dp))
        }
        Text(text = label, color = tint, style = MeshaType.button, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A label + value line inside a detail card. */
@Composable
internal fun WorkBoardFactLine(label: String, value: String) {
    if (value.isBlank()) return
    Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(text = label, color = MeshaColors.Muted, style = MeshaType.caption, modifier = Modifier.weight(0.4f))
        Text(text = value, color = MeshaColors.Ink, style = MeshaType.bodyStrong, modifier = Modifier.weight(0.6f))
    }
}

/** The clock/severity colour: the wire `severity` decides it, the words stay backend copy. */
internal fun severityAccent(severity: WorkBoardSeverity): Color = when (severity) {
    WorkBoardSeverity.OK -> MeshaColors.Ok
    WorkBoardSeverity.WATCH -> MeshaColors.Info
    WorkBoardSeverity.AT_RISK -> MeshaColors.Warn
    WorkBoardSeverity.BROKEN -> MeshaColors.Danger
}

/** The lane colour — done reads calm, everything else neutral. */
internal fun laneAccent(lane: String): Color = when (lane) {
    "done" -> MeshaColors.Ok
    "in_review" -> MeshaColors.Info
    "in_progress" -> MeshaColors.BrandD
    else -> MeshaColors.Muted
}

/** Farm label for a lane key; the "" chip is "All". */
@Composable
internal fun laneLabel(lane: String): String = when (lane) {
    "todo" -> stringResource(R.string.work_board_lane_todo)
    "in_progress" -> stringResource(R.string.work_board_lane_in_progress)
    "in_review" -> stringResource(R.string.work_board_lane_in_review)
    "done" -> stringResource(R.string.work_board_lane_done)
    "" -> stringResource(R.string.work_board_filter_all)
    else -> lane
}

/** Farm label for a module key; the "" chip is "All". An unknown key renders as itself. */
@Composable
internal fun moduleLabel(module: String): String = when (module) {
    "feed" -> stringResource(R.string.work_board_module_feed)
    "health" -> stringResource(R.string.work_board_module_health)
    "vaccination" -> stringResource(R.string.work_board_module_vaccination)
    "weighing" -> stringResource(R.string.work_board_module_weighing)
    "counts" -> stringResource(R.string.work_board_module_counts)
    "milk" -> stringResource(R.string.work_board_module_milk)
    "pc_care" -> stringResource(R.string.work_board_module_pc_care)
    "toxin" -> stringResource(R.string.work_board_module_toxin)
    "procurement" -> stringResource(R.string.work_board_module_procurement)
    "verification" -> stringResource(R.string.work_board_module_verification)
    "tasks" -> stringResource(R.string.work_board_module_tasks)
    "" -> stringResource(R.string.work_board_filter_all)
    else -> module
}

/** Farm label for a process-integrity work state; an unknown key falls back to its lane. */
@Composable
internal fun workStateLabel(workState: String, lane: String): String = when (workState) {
    "scheduled" -> stringResource(R.string.work_board_state_scheduled)
    "due" -> stringResource(R.string.work_board_state_due)
    "overdue" -> stringResource(R.string.work_board_state_overdue)
    "in_progress" -> stringResource(R.string.work_board_state_in_progress)
    "proof_pending" -> stringResource(R.string.work_board_state_proof_pending)
    "verification_pending" -> stringResource(R.string.work_board_state_verification_pending)
    "rejected" -> stringResource(R.string.work_board_state_rejected)
    "deferred" -> stringResource(R.string.work_board_state_deferred)
    "missed" -> stringResource(R.string.work_board_state_missed)
    "blocked" -> stringResource(R.string.work_board_state_blocked)
    "completed" -> stringResource(R.string.work_board_state_completed)
    else -> laneLabel(lane)
}

/** The owner line: the person's name, or the farm wording for a missing owner / a claim pool. */
@Composable
internal fun ownerLine(row: WorkBoardRowUi): String = when (row.ownerState) {
    WorkBoardOwnerState.ASSIGNED -> row.ownerName
    WorkBoardOwnerState.MISSING -> stringResource(R.string.work_board_owner_missing)
    WorkBoardOwnerState.POOL -> stringResource(R.string.work_board_owner_pool)
}

/** The module's own glyph on a row card; an unknown module wears the generic module grid. */
internal fun moduleIcon(module: String): ImageVector = when (module) {
    "feed" -> MeshaIcons.Feed
    "health" -> MeshaIcons.Health
    "vaccination" -> MeshaIcons.Syringe
    "weighing" -> MeshaIcons.BarChart
    "counts" -> MeshaIcons.Goat
    "milk" -> MeshaIcons.MilkPreparation
    "pc_care" -> MeshaIcons.PcCare
    "toxin" -> MeshaIcons.Warn
    "procurement" -> MeshaIcons.Store
    "verification" -> MeshaIcons.CheckCircle
    "tasks" -> MeshaIcons.Module
    else -> MeshaIcons.Module
}

/** "17 in review · 6 not started": the row's pending work broken down, empty buckets left out. */
@Composable
internal fun pendingSplitLine(row: WorkBoardRowUi): String = listOfNotNull(
    if (row.inReview > 0) stringResource(R.string.work_board_split_in_review_fmt, row.inReview) else null,
    if (row.started > 0) stringResource(R.string.work_board_split_started_fmt, row.started) else null,
    if (row.notStarted > 0) stringResource(R.string.work_board_split_not_started_fmt, row.notStarted) else null,
).joinToString(" · ")
