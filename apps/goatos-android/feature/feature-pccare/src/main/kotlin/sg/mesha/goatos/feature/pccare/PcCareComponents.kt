package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the pc_care_*
// AnalyticsEvents + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Status chip shared by the worklist cards and the planner monitor list. */
@Composable
internal fun PcCareStatusChip(label: String, tone: PcCareStatusTone, modifier: Modifier = Modifier) {
    if (label.isBlank()) return
    val (bg, fg) = when (tone) {
        PcCareStatusTone.NEUTRAL -> MeshaColors.Surf2 to MeshaColors.Muted
        PcCareStatusTone.REVIEW -> MeshaColors.WarnX to MeshaColors.Warn
        PcCareStatusTone.DANGER -> MeshaColors.DangerX to MeshaColors.Danger
        PcCareStatusTone.DONE -> MeshaColors.OkX to MeshaColors.Ok
    }
    Box(
        modifier = modifier
            .clip(RoundedCornerShape(8.dp))
            .background(bg)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    ) {
        Text(text = label, color = fg, style = MeshaType.pillStrong)
    }
}

/** "Today · 21 Aug"-style label for an ISO business date; null when the string is malformed. */
internal fun pcCareFriendlyDate(iso: String): String? {
    // exception:exempt a malformed date string renders no label; the ViewModel owns the value
    val date = runCatching { LocalDate.parse(iso) }.getOrNull() ?: return null
    val today = LocalDate.now(ZoneId.of("Asia/Kolkata"))
    val dayPart = date.format(DateTimeFormatter.ofPattern("EEE d MMM", Locale.ENGLISH))
    return when (date) {
        today -> "Today · $dayPart"
        today.plusDays(1) -> "Tomorrow · $dayPart"
        today.minusDays(1) -> "Yesterday · $dayPart"
        else -> dayPart
    }
}

/** Previous/next business-date bar with a farm-readable label. The ViewModel clamps the window. */
@Composable
internal fun PcCareDateBar(
    selectedDateIso: String,
    onSelectDate: (LocalDate) -> Unit,
    modifier: Modifier = Modifier,
) {
    // exception:exempt a malformed date string renders no bar; the ViewModel owns the value
    val selected = runCatching { LocalDate.parse(selectedDateIso) }.getOrNull() ?: return
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 6.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        DateArrow(label = "Previous day", flip = false) { onSelectDate(selected.minusDays(1)) }
        Text(
            text = pcCareFriendlyDate(selectedDateIso) ?: selectedDateIso,
            color = MeshaColors.Ink,
            style = MeshaType.bodyStrong,
            textAlign = TextAlign.Center,
            modifier = Modifier.weight(1f),
        )
        DateArrow(label = "Next day", flip = true) { onSelectDate(selected.plusDays(1)) }
    }
}

@Composable
private fun DateArrow(label: String, flip: Boolean, onClick: () -> Unit) {
    Box(
        modifier = Modifier
            .size(48.dp)
            .clip(RoundedCornerShape(10.dp))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = if (flip) MeshaIcons.Chevron else MeshaIcons.ChevronLeft,
            contentDescription = label,
            tint = MeshaColors.Muted,
        )
    }
}

/** Card surface shared by the list rows on the PC Care screens. */
@Composable
internal fun pcCareCardModifier(enabled: Boolean, onClick: (() -> Unit)?): Modifier {
    var base = Modifier
        .fillMaxWidth()
        .padding(horizontal = 16.dp)
        .clip(RoundedCornerShape(16.dp))
        .background(MeshaColors.Surf)
        .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    if (onClick != null) {
        base = base.clickable(enabled = enabled, onClick = onClick)
    }
    return base.padding(14.dp)
}

internal val PcCareDim: Color get() = MeshaColors.Faint

/** Small tappable inline text action (e.g. the planner's "Cancel this task"). */
@Composable
internal fun pcCareInlineActionModifier(onClick: () -> Unit): Modifier =
    Modifier
        .clip(RoundedCornerShape(8.dp))
        .clickable(onClick = onClick)
        .padding(horizontal = 8.dp, vertical = 4.dp)

// ---------------------------------------------------------------------------
// Wizard chrome, ported from the weighing plan wizard so the two planners read
// as one product. Kept feature-local: features do not import each other's UI.
// ---------------------------------------------------------------------------

/** Segment stepper: one bar per wizard step, filled up to the current one. */
@Composable
internal fun PcCareStepper(stepCount: Int, currentIndex: Int, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 18.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        repeat(stepCount) { index ->
            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(3.dp)
                    .clip(RoundedCornerShape(20.dp))
                    .background(if (index <= currentIndex) MeshaColors.Brand else MeshaColors.Surf3),
            )
        }
    }
}

/** Sticky bottom bar: a context line naming what is chosen so far, then the step's actions. */
@Composable
internal fun PcCareWizardActionBar(
    contextLine: String,
    modifier: Modifier = Modifier,
    content: @Composable RowScope.() -> Unit,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(MeshaColors.Bg)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = contextLine,
            color = MeshaColors.Muted,
            style = MeshaType.caption,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}

/** Primary wizard action. Disabled renders as a dead button, never hidden. */
@Composable
internal fun PcCarePrimaryButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 52.dp)
            .clip(RoundedCornerShape(15.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Secondary (ghost) wizard action, matching the weighing surfaces. */
@Composable
internal fun PcCareGhostButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 52.dp)
            .clip(RoundedCornerShape(15.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(15.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Small labelled pill, the weighing TaskPill twin. */
@Composable
internal fun PcCareTaskPill(label: String, fg: Color, bg: Color) {
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
