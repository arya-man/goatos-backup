package sg.mesha.goatos.core.ui.filters

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.R
import java.time.LocalDate

// telemetry:exempt Pure presentational filter chrome; the screens that host it own the events.

/**
 * The FILTER BAR itself: row one is the Pending / Completed pills carrying the backend's
 * whole-filter counts, row two is the Date and Pen controls. Tapping Date or Pen opens the
 * matching sheet, which the host renders ([WorklistDateWindowSheet], [WorklistPenSheet]).
 *
 * [showStatus] / [showDate] / [showPen] let a host offer only the controls its list supports (a
 * web-authored pen-routine tab names its own filters, 2026-10-01); a control not shown takes no
 * room. A null [window] means "no date narrowing" and reads "All dates".
 */
@Composable
fun WorklistFilterBar(
    status: WorklistStatus,
    pendingCount: Int,
    completedCount: Int,
    window: WorklistDateWindow?,
    today: LocalDate,
    pen: WorklistPen?,
    onSelectStatus: (WorklistStatus) -> Unit,
    onOpenDate: () -> Unit,
    onOpenPen: () -> Unit,
    modifier: Modifier = Modifier,
    showStatus: Boolean = true,
    showDate: Boolean = true,
    showPen: Boolean = true,
) {
    Column(modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
        if (showStatus) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                WorklistStatusPill(
                    label = stringResource(R.string.filters_status_pending_fmt, pendingCount),
                    selected = status == WorklistStatus.PENDING,
                    onClick = { onSelectStatus(WorklistStatus.PENDING) },
                )
                WorklistStatusPill(
                    label = stringResource(R.string.filters_status_completed_fmt, completedCount),
                    selected = status == WorklistStatus.COMPLETED,
                    onClick = { onSelectStatus(WorklistStatus.COMPLETED) },
                )
            }
        }
        if (showStatus && (showDate || showPen)) Spacer(Modifier.height(8.dp))
        if (showDate || showPen) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (showDate) {
                    WorklistFieldPill(
                        icon = MeshaIcons.Calendar,
                        label = stringResource(R.string.filters_date_label),
                        value = window?.displayLabel(today, stringResource(R.string.filters_today))
                            ?: stringResource(R.string.filters_all_dates),
                        active = window != null,
                        contentDescription = stringResource(R.string.filters_open_date),
                        onClick = onOpenDate,
                        modifier = Modifier.weight(1.25f),
                    )
                }
                if (showPen) {
                    WorklistFieldPill(
                        icon = MeshaIcons.Home,
                        label = stringResource(R.string.filters_pen_label),
                        value = pen?.label ?: stringResource(R.string.filters_all_pens),
                        active = pen != null,
                        contentDescription = stringResource(R.string.filters_open_pen),
                        onClick = onOpenPen,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

/** The status pill: the same 48dp brand/surface pill the weighing tabs have always used. */
@Composable
fun WorklistStatusPill(label: String, selected: Boolean, onClick: () -> Unit) {
    val bg = if (selected) MeshaColors.Brand else MeshaColors.Surf2
    val edge = if (selected) MeshaColors.Brand else MeshaColors.Hair
    val fg = if (selected) MeshaColors.PageBg else MeshaColors.Ink
    Row(
        modifier = Modifier
            .height(48.dp)
            .clip(RoundedCornerShape(24.dp))
            .background(bg)
            .border(1.dp, edge, RoundedCornerShape(24.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = label, color = fg, style = MeshaType.pillStrong, maxLines = 1)
    }
}

/** A FIELD pill: icon, a small label and the current value, with a chevron. 48dp tall. */
@Composable
private fun WorklistFieldPill(
    icon: ImageVector,
    label: String,
    value: String,
    active: Boolean,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .height(48.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, if (active) MeshaColors.Brand2 else MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            .semantics { this.contentDescription = contentDescription }
            .padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(8.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(text = label.uppercase(), color = MeshaColors.Muted, style = MeshaType.overline, maxLines = 1)
            Text(
                text = value,
                color = MeshaColors.Ink,
                style = MeshaType.listTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Icon(MeshaIcons.ChevronDown, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(16.dp))
    }
}
