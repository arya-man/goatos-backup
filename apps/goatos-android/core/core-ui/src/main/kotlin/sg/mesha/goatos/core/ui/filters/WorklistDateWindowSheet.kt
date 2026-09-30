package sg.mesha.goatos.core.ui.filters

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.R
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.YearMonth
import java.time.format.DateTimeFormatter
import java.util.Locale

// telemetry:exempt Pure presentational picker; the hosting screen tracks the applied filter.

/**
 * The Date control's sheet: a month calendar the reader picks a WINDOW on.
 *
 * Tap a start day, then an end day, for a range. Tap the SAME day twice for that one day. A tap
 * before the start restarts the range from there. Quick picks sit above the grid; Clear returns
 * to the default window (today through the next seven days); Apply hands the window back.
 *
 * The calendar is drawn here rather than through Material's DateRangePicker so it keeps the
 * app's own pill and card vocabulary and so a single day is one tap-twice gesture, which that
 * component has no notion of.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorklistDateWindowSheet(
    initial: WorklistDateWindow?,
    today: LocalDate,
    onApply: (WorklistDateWindow) -> Unit,
    onDismiss: () -> Unit,
    /**
     * What Clear does. Null (every existing host) keeps the shipped behaviour -- apply the default
     * window. A host whose list starts UNNARROWED (a web-authored pen-routine tab) passes its own
     * "drop the date filter" here, so Clear returns to what the reader first saw.
     */
    onClear: (() -> Unit)? = null,
) {
    var from by remember { mutableStateOf<LocalDate?>(initial?.from) }
    var to by remember { mutableStateOf<LocalDate?>(initial?.to) }
    var month by remember { mutableStateOf(YearMonth.from(initial?.from ?: today)) }

    fun tap(day: LocalDate) {
        val start = from
        when {
            start == null || to != null -> { from = day; to = null }
            day == start -> to = day
            day.isBefore(start) -> { from = day; to = null }
            else -> to = day
        }
    }
    fun pick(window: WorklistDateWindow) {
        from = window.from
        to = window.to
        month = YearMonth.from(window.from)
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = MeshaColors.Surf,
        contentColor = MeshaColors.Ink,
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(text = stringResource(R.string.filters_date_label), style = MeshaType.headerTitle, color = MeshaColors.Ink)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                DateField(stringResource(R.string.filters_from), worklistFieldDate(from), active = true, Modifier.weight(1f))
                DateField(stringResource(R.string.filters_to), worklistFieldDate(to ?: from), active = to != null, Modifier.weight(1f))
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                val picks = listOf(
                    stringResource(R.string.filters_today) to WorklistDateWindow(today, today),
                    stringResource(R.string.filters_next_7_days) to WorklistDateWindow.default(today),
                    stringResource(R.string.filters_next_30_days) to WorklistDateWindow(today, today.plusDays(30)),
                    stringResource(R.string.filters_last_7_days) to WorklistDateWindow(today.minusDays(7), today),
                )
                picks.forEach { (label, window) ->
                    val selected = from == window.from && to == window.to
                    QuickPick(label, selected, Modifier.weight(1f)) { pick(window) }
                }
            }
            MonthCalendar(
                month = month,
                today = today,
                from = from,
                to = to,
                onPreviousMonth = { month = month.minusMonths(1) },
                onNextMonth = { month = month.plusMonths(1) },
                onTap = ::tap,
            )
            Text(
                text = stringResource(R.string.filters_calendar_hint),
                style = MeshaType.caption,
                color = MeshaColors.Muted,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SheetButton(
                    label = stringResource(R.string.filters_clear),
                    primary = false,
                    modifier = Modifier.weight(1f),
                ) { onClear?.invoke() ?: onApply(WorklistDateWindow.default(today)) }
                SheetButton(
                    label = stringResource(R.string.filters_apply),
                    primary = true,
                    modifier = Modifier.weight(1.4f),
                ) {
                    val start = from ?: return@SheetButton
                    onApply(WorklistDateWindow(start, to ?: start))
                }
            }
        }
    }
}

@Composable
private fun DateField(label: String, value: String, active: Boolean, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, if (active) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 12.dp, vertical = 8.dp),
    ) {
        Text(text = label.uppercase(), color = MeshaColors.Muted, style = MeshaType.overline)
        Text(text = value, color = MeshaColors.Ink, style = MeshaType.bodyStrong, maxLines = 1)
    }
}

@Composable
private fun QuickPick(label: String, selected: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(
        modifier = modifier
            .height(40.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(if (selected) MeshaColors.BrandTint else MeshaColors.Surf2)
            .border(1.dp, if (selected) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(10.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 4.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (selected) MeshaColors.BrandD else MeshaColors.Ink,
            style = MeshaType.caption,
            maxLines = 1,
            textAlign = TextAlign.Center,
        )
    }
}

@Composable
internal fun SheetButton(label: String, primary: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(
        modifier = modifier
            .height(52.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(if (primary) MeshaColors.Brand else MeshaColors.Surf)
            .border(1.dp, if (primary) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = label, color = if (primary) MeshaColors.OnBrand else MeshaColors.Ink, style = MeshaType.button)
    }
}

private val monthTitle: DateTimeFormatter = DateTimeFormatter.ofPattern("MMMM yyyy", Locale.ENGLISH)

/** Monday-first month grid. Days outside the month are drawn faint and are not tappable. */
@Composable
private fun MonthCalendar(
    month: YearMonth,
    today: LocalDate,
    from: LocalDate?,
    to: LocalDate?,
    onPreviousMonth: () -> Unit,
    onNextMonth: () -> Unit,
    onTap: (LocalDate) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
            MonthArrow(stringResource(R.string.filters_previous_month), flip = false, onClick = onPreviousMonth)
            Text(
                text = month.format(monthTitle),
                style = MeshaType.bodyStrong,
                color = MeshaColors.Ink,
                textAlign = TextAlign.Center,
                modifier = Modifier.weight(1f),
            )
            MonthArrow(stringResource(R.string.filters_next_month), flip = true, onClick = onNextMonth)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            listOf("MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN").forEach { name ->
                Text(
                    text = name,
                    style = MeshaType.overline,
                    color = MeshaColors.Muted,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        val first = month.atDay(1)
        val lead = (first.dayOfWeek.value - DayOfWeek.MONDAY.value + 7) % 7
        val start = first.minusDays(lead.toLong())
        // Six rows cover every month; the trailing overflow days stay faint.
        repeat(6) { week ->
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                repeat(7) { column ->
                    val day = start.plusDays((week * 7 + column).toLong())
                    DayCell(
                        day = day,
                        inMonth = YearMonth.from(day) == month,
                        isToday = day == today,
                        from = from,
                        to = to,
                        onTap = onTap,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
        }
    }
}

@Composable
private fun MonthArrow(contentDescription: String, flip: Boolean, onClick: () -> Unit) {
    Box(
        modifier = Modifier
            .size(48.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(10.dp))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            MeshaIcons.ChevronLeft,
            contentDescription = contentDescription,
            tint = MeshaColors.Ink,
            modifier = Modifier.size(16.dp).graphicsLayer { scaleX = if (flip) -1f else 1f },
        )
    }
}

@Composable
private fun DayCell(
    day: LocalDate,
    inMonth: Boolean,
    isToday: Boolean,
    from: LocalDate?,
    to: LocalDate?,
    onTap: (LocalDate) -> Unit,
    modifier: Modifier = Modifier,
) {
    val end = to ?: from
    val isStart = inMonth && from != null && day == from
    val isEnd = inMonth && end != null && day == end
    val inside = inMonth && from != null && end != null && day.isAfter(from) && day.isBefore(end)
    val single = isStart && isEnd
    val shape = when {
        single -> RoundedCornerShape(12.dp)
        isStart -> RoundedCornerShape(topStart = 12.dp, bottomStart = 12.dp)
        isEnd -> RoundedCornerShape(topEnd = 12.dp, bottomEnd = 12.dp)
        else -> RoundedCornerShape(0.dp)
    }
    val background = when {
        isStart || isEnd -> MeshaColors.Brand
        inside -> MeshaColors.BrandTint
        else -> MeshaColors.Surf
    }
    Box(
        modifier = modifier
            .height(44.dp)
            .clip(if (background == MeshaColors.Surf) RoundedCornerShape(12.dp) else shape)
            .background(background)
            .then(if (isToday && !isStart && !isEnd) Modifier.border(1.5.dp, MeshaColors.Brand, RoundedCornerShape(12.dp)) else Modifier)
            .clickable(enabled = inMonth) { onTap(day) },
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = day.dayOfMonth.toString(),
            style = if (inMonth) MeshaType.bodyStrong else MeshaType.body,
            color = when {
                isStart || isEnd -> MeshaColors.OnBrand
                !inMonth -> MeshaColors.Faint
                else -> MeshaColors.Ink
            },
        )
    }
}
