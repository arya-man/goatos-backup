package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the pc_care_*
// AnalyticsEvents + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
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
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import java.time.LocalDate

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

/** Simple previous/next business-date bar. The ViewModel clamps out-of-window selections. */
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
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        DateArrow(label = "Previous day", flip = false) { onSelectDate(selected.minusDays(1)) }
        Text(
            text = selectedDateIso,
            color = MeshaColors.Ink,
            style = MeshaType.pillStrong,
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
