package sg.mesha.goatos.feature.calendar

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme

/**
 * Calendar — the universal landing for every role (screens.md). Renders the
 * backend-provided [state]: segmented week / month / history, per-day due-work
 * counts, month drive-day dots, and past drive records. It never re-derives which
 * animals are due and never checks role — the drill target on each item is
 * backend-supplied ([CalendarItem.target]), so operator (execute) and leadership
 * (follow-up) share one screen. Stateless: state is hoisted to the caller.
 */
@Composable
fun CalendarScreen(
    state: CalendarUiState,
    onEvent: (CalendarEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val selected = state.segments.firstOrNull { it.id == state.selectedSegmentId }
        ?: state.segments.firstOrNull()

    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(CalTokens.PageBg)
            .padding(horizontal = Gutter),
    ) {
        item { CalendarHeader(state, onEvent) }
        if (state.segments.isNotEmpty()) {
            item {
                SegmentedControl(
                    segments = state.segments,
                    selectedId = state.selectedSegmentId,
                    onSelect = { onEvent(CalendarEvent.SelectSegment(it)) },
                )
                Spacer(Modifier.size(10.dp))
            }
        }

        when (selected?.kind) {
            CalendarSegmentKind.Week -> weekContent(state, onEvent)
            CalendarSegmentKind.Month -> monthContent(state, onEvent)
            CalendarSegmentKind.History -> historyContent(state, onEvent)
            null -> Unit
        }
        item { Spacer(Modifier.size(24.dp)) }
    }
}

/* --------------------------------------------------------------------------- */
/* Header                                                                      */
/* --------------------------------------------------------------------------- */

@Composable
private fun CalendarHeader(state: CalendarUiState, onEvent: (CalendarEvent) -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(top = 16.dp, bottom = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            if (state.eyebrow.isNotEmpty()) {
                Text(
                    text = state.eyebrow.uppercase(),
                    color = CalTokens.Faint,
                    fontSize = 10.5.sp,
                    fontWeight = FontWeight.W700,
                    letterSpacing = 0.6.sp,
                )
            }
            Text(
                text = state.title,
                color = CalTokens.Ink,
                fontSize = 22.sp,
                fontWeight = FontWeight.W700,
            )
            val window = state.windowLabel
            val line = if (window.isNullOrEmpty()) {
                state.selectedDateLabel
            } else {
                "${state.selectedDateLabel}  ·  $window"
            }
            if (line.isNotEmpty()) {
                Text(
                    text = line,
                    color = CalTokens.Muted,
                    fontSize = 12.5.sp,
                    fontWeight = FontWeight.W600,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
        // Mock `.vhead` refresh affordance. (Menu/bell are omitted: single-module chrome has
        // no drawer, and Alerts is already a bottom-nav tab — neither would be a live control.)
        HeaderIconButton(onClick = { onEvent(CalendarEvent.Refresh) })
    }
}

@Composable
private fun HeaderIconButton(onClick: () -> Unit) {
    Box(
        Modifier
            .size(38.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(CalTokens.Surf2)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = MeshaIcons.Refresh,
            contentDescription = "Refresh",
            tint = CalTokens.Muted,
            modifier = Modifier.size(18.dp),
        )
    }
}

/* --------------------------------------------------------------------------- */
/* Segmented control                                                           */
/* --------------------------------------------------------------------------- */

@Composable
private fun SegmentedControl(
    segments: List<CalendarSegment>,
    selectedId: String,
    onSelect: (String) -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(13.dp))
            .background(CalTokens.Surf)
            .border(1.dp, CalTokens.Hair, RoundedCornerShape(13.dp))
            .padding(4.dp),
    ) {
        segments.forEach { seg ->
            val on = seg.id == selectedId
            Box(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(9.dp))
                    .then(if (on) Modifier.background(CalTokens.BrandGradient) else Modifier)
                    .clickable { onSelect(seg.id) }
                    .padding(vertical = 9.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = seg.label,
                    color = if (on) CalTokens.OnBrand else CalTokens.Muted,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/* --------------------------------------------------------------------------- */
/* Week                                                                        */
/* --------------------------------------------------------------------------- */

private fun androidx.compose.foundation.lazy.LazyListScope.weekContent(
    state: CalendarUiState,
    onEvent: (CalendarEvent) -> Unit,
) {
    item {
        Row(
            Modifier.fillMaxWidth().padding(vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            state.weekDays.forEach { day ->
                WeekDayCell(
                    day = day,
                    modifier = Modifier.weight(1f),
                    onClick = { onEvent(CalendarEvent.TapDay(day.dateKey)) },
                )
            }
        }
    }
    item { SectionLabel(state.selectedDateLabel) }
    if (state.weekItems.isEmpty()) {
        item { EmptyCard(state.weekEmptyLabel) }
    } else {
        items(state.weekItems, key = { it.id }) { item ->
            EventCard(item = item, onClick = { onEvent(CalendarEvent.TapItem(item.id)) })
        }
    }
}

@Composable
private fun WeekDayCell(
    day: CalendarWeekDay,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val on = day.isSelected || day.isToday
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .then(if (on) Modifier.background(CalTokens.BrandGradient) else Modifier.background(CalTokens.Surf))
            .border(1.dp, if (on) Color.Transparent else CalTokens.Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            // Mock: today's cell is a taller pill that pops below the row.
            .padding(vertical = if (on) 18.dp else 10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = day.dayName.uppercase(),
            color = if (on) CalTokens.OnBrand else CalTokens.Faint,
            fontSize = 9.5.sp,
            fontWeight = FontWeight.W700,
        )
        Text(
            text = day.dayNumber,
            color = if (on) CalTokens.OnBrand else CalTokens.Ink,
            fontSize = 15.sp,
            fontWeight = FontWeight.W800,
            modifier = Modifier.padding(top = 4.dp),
        )
        if (day.dueCountLabel.isNotEmpty()) {
            Text(
                text = day.dueCountLabel,
                color = if (on) CalTokens.OnBrand else CalTokens.Muted,
                fontSize = 9.5.sp,
                fontWeight = FontWeight.W600,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
        if (day.hasWork) {
            Box(
                Modifier
                    .padding(top = 4.dp)
                    .size(5.dp)
                    .clip(CircleShape)
                    .background(if (on) CalTokens.OnBrand else CalTokens.Brand),
            )
        }
    }
}

@Composable
private fun EventCard(item: CalendarItem, onClick: () -> Unit) {
    val drillable = item.ctaLabel != null && item.target != null
    Column(
        Modifier
            .fillMaxWidth()
            .padding(bottom = 11.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(CalTokens.Surf)
            .border(1.dp, CalTokens.Hair, RoundedCornerShape(18.dp))
            .leftAccent(CalTokens.Brand)
            .then(if (drillable) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(start = 16.dp, top = 15.dp, end = 16.dp, bottom = 15.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            StatusPill(item.statusLabel, item.statusTone)
            Spacer(Modifier.weight(1f))
            item.categoryLabel?.let { StatusPill(it, CalendarTone.Muted) }
        }
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier.padding(top = 10.dp),
        ) {
            // Syringe = Vaccination module marker before the drive name (mock `.evt .nm` icon).
            Icon(
                imageVector = MeshaIcons.Syringe,
                contentDescription = null,
                tint = if (drillable) CalTokens.Brand else CalTokens.Faint,
                modifier = Modifier.size(16.dp),
            )
            Spacer(Modifier.size(8.dp))
            Text(
                text = item.title,
                color = if (drillable) CalTokens.Ink else CalTokens.Muted,
                fontSize = 15.sp,
                fontWeight = FontWeight.W700,
            )
        }
        if (item.subtitle.isNotEmpty()) {
            Text(
                text = item.subtitle,
                color = CalTokens.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W500,
                modifier = Modifier.padding(top = 7.dp),
            )
        }
        item.ctaLabel?.let { cta ->
            Text(
                text = "$cta  ›",
                color = CalTokens.Brand2,
                fontSize = 12.5.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(top = 11.dp),
            )
        }
    }
}

/* --------------------------------------------------------------------------- */
/* Month                                                                       */
/* --------------------------------------------------------------------------- */

private fun androidx.compose.foundation.lazy.LazyListScope.monthContent(
    state: CalendarUiState,
    onEvent: (CalendarEvent) -> Unit,
) {
    item {
        Text(
            text = state.monthLabel,
            color = CalTokens.Ink,
            fontSize = 14.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp),
        )
    }
    if (state.monthWeekdayLabels.isNotEmpty()) {
        item {
            Row(Modifier.fillMaxWidth().padding(bottom = 5.dp)) {
                state.monthWeekdayLabels.forEach { wd ->
                    Text(
                        text = wd,
                        color = CalTokens.Faint,
                        fontSize = 9.5.sp,
                        fontWeight = FontWeight.W700,
                        modifier = Modifier.weight(1f),
                        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                    )
                }
            }
        }
    }
    items(state.monthDays.chunked(7)) { week ->
        Row(
            Modifier.fillMaxWidth().padding(bottom = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            week.forEach { cell ->
                MonthCell(
                    cell = cell,
                    modifier = Modifier.weight(1f),
                    onClick = { cell.dateKey?.let { onEvent(CalendarEvent.TapDay(it)) } },
                )
            }
            repeat(7 - week.size) { Spacer(Modifier.weight(1f)) }
        }
    }
    if (state.monthHint.isNotEmpty()) {
        item {
            Text(
                text = state.monthHint,
                color = CalTokens.Faint,
                fontSize = 10.5.sp,
                modifier = Modifier.fillMaxWidth().padding(vertical = 9.dp),
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
        }
    }
    state.daySheet?.let { sheet ->
        item {
            DaySheet(
                state = sheet,
                onEvent = onEvent,
                modifier = Modifier.padding(top = 6.dp),
            )
        }
    }
}

@Composable
private fun MonthCell(
    cell: CalendarMonthDay,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    if (cell.dayNumber == null) {
        Box(modifier.aspectRatio(1f))
        return
    }
    Column(
        modifier = modifier
            .aspectRatio(1f)
            .clip(RoundedCornerShape(10.dp))
            .background(CalTokens.Surf)
            .border(
                1.dp,
                if (cell.isSelected || cell.hasWork) CalTokens.Brand else CalTokens.Hair,
                RoundedCornerShape(10.dp),
            )
            .clickable(enabled = cell.dateKey != null, onClick = onClick),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = cell.dayNumber,
            color = if (cell.hasWork) CalTokens.Ink else CalTokens.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W600,
        )
        if (cell.hasWork) {
            Box(
                Modifier
                    .padding(top = 3.dp)
                    .size(5.dp)
                    .clip(CircleShape)
                    .background(toneColor(cell.dotTone)),
            )
        }
    }
}

/* --------------------------------------------------------------------------- */
/* Day sheet (ovl-day)                                                         */
/* --------------------------------------------------------------------------- */

/** The `ovl-day` sheet: sheds for a tapped month day. Rendered as a bottom section. */
@Composable
fun DaySheet(
    state: DaySheetUiState,
    onEvent: (CalendarEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(20.dp))
            .background(CalTokens.Surf2)
            .border(1.dp, CalTokens.Hair, RoundedCornerShape(20.dp))
            .padding(top = 12.dp, bottom = 8.dp),
    ) {
        Box(
            Modifier
                .padding(bottom = 10.dp)
                .align(Alignment.CenterHorizontally)
                .size(width = 34.dp, height = 4.dp)
                .clip(CircleShape)
                .background(CalTokens.Hair),
        )
        Text(
            text = state.title,
            color = CalTokens.Ink,
            fontSize = 16.sp,
            fontWeight = FontWeight.W800,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        if (state.items.isEmpty()) {
            Text(
                text = state.emptyLabel,
                color = CalTokens.Muted,
                fontSize = 12.5.sp,
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
        } else {
            Column(Modifier.padding(horizontal = 12.dp, vertical = 4.dp)) {
                state.items.forEach { item ->
                    EventCard(item = item, onClick = { onEvent(CalendarEvent.TapItem(item.id)) })
                }
            }
        }
    }
}

/* --------------------------------------------------------------------------- */
/* History                                                                     */
/* --------------------------------------------------------------------------- */

private fun androidx.compose.foundation.lazy.LazyListScope.historyContent(
    state: CalendarUiState,
    onEvent: (CalendarEvent) -> Unit,
) {
    item { SectionLabel(state.historyLabel) }
    if (state.historyRows.isEmpty()) {
        item { EmptyCard(state.historyEmptyLabel) }
    } else {
        items(state.historyRows, key = { it.id }) { row ->
            HistoryRow(row = row, onClick = { onEvent(CalendarEvent.TapItem(row.id)) })
        }
    }
}

@Composable
private fun HistoryRow(row: CalendarHistoryRow, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(bottom = 9.dp)
            .clip(RoundedCornerShape(15.dp))
            .background(CalTokens.Surf)
            .border(1.dp, CalTokens.Hair, RoundedCornerShape(15.dp))
            .clickable(enabled = row.target != null, onClick = onClick)
            .padding(horizontal = 15.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(38.dp)
                .clip(RoundedCornerShape(11.dp))
                .background(CalTokens.OkX),
            contentAlignment = Alignment.Center,
        ) {
            // Syringe = Vaccination module marker (mock icon set).
            Icon(
                imageVector = MeshaIcons.Syringe,
                contentDescription = null,
                tint = CalTokens.Brand,
                modifier = Modifier.size(17.dp),
            )
        }
        Column(Modifier.weight(1f).padding(horizontal = 11.dp)) {
            Text(
                text = row.title,
                color = CalTokens.Ink,
                fontSize = 13.5.sp,
                fontWeight = FontWeight.W700,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = row.subtitle,
                color = CalTokens.Muted,
                fontSize = 11.sp,
                fontWeight = FontWeight.W500,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        StatusPill(row.badgeLabel, row.badgeTone)
    }
}

/* --------------------------------------------------------------------------- */
/* Shared primitives                                                           */
/* --------------------------------------------------------------------------- */

@Composable
private fun SectionLabel(text: String) {
    if (text.isEmpty()) return
    Text(
        text = text.uppercase(),
        color = CalTokens.Faint,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        letterSpacing = 0.55.sp,
        modifier = Modifier.fillMaxWidth().padding(top = 14.dp, bottom = 6.dp),
    )
}

@Composable
private fun EmptyCard(text: String) {
    Box(
        Modifier
            .fillMaxWidth()
            .padding(bottom = 11.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(CalTokens.Surf)
            .border(1.dp, CalTokens.Hair, RoundedCornerShape(18.dp))
            .padding(22.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            color = CalTokens.Muted,
            fontSize = 12.5.sp,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
    }
}

@Composable
private fun StatusPill(label: String, tone: CalendarTone) {
    Box(
        Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(toneBackground(tone))
            .padding(horizontal = 10.dp, vertical = 4.dp),
    ) {
        Text(
            text = label,
            color = toneText(tone),
            fontSize = 11.sp,
            fontWeight = FontWeight.W700,
            maxLines = 1,
        )
    }
}

/** A 3dp brand accent bar on the card's leading edge (mock `.evt` left border). */
private fun Modifier.leftAccent(color: Color): Modifier = this.drawBehind {
    drawRect(color = color, size = size.copy(width = 3.dp.toPx()))
}

private fun toneColor(tone: CalendarTone): Color = when (tone) {
    CalendarTone.Ok -> CalTokens.Brand
    CalendarTone.Warn -> CalTokens.Warn
    CalendarTone.Danger -> CalTokens.Danger
    CalendarTone.Muted -> CalTokens.Muted
    CalendarTone.Neutral -> CalTokens.Faint
}

private fun toneBackground(tone: CalendarTone): Color = when (tone) {
    CalendarTone.Ok -> CalTokens.OkX
    CalendarTone.Warn -> CalTokens.WarnX
    CalendarTone.Danger -> CalTokens.DangerX
    CalendarTone.Muted, CalendarTone.Neutral -> CalTokens.Surf3
}

private fun toneText(tone: CalendarTone): Color = when (tone) {
    CalendarTone.Ok -> CalTokens.BrandD
    CalendarTone.Warn -> CalTokens.Warn
    CalendarTone.Danger -> CalTokens.Danger
    CalendarTone.Muted, CalendarTone.Neutral -> CalTokens.Muted
}

private val Gutter = 16.dp

/**
 * Dark-theme tokens ported from the mock's CSS custom properties
 * (design-system.md §1). Kept local until core-designsystem exposes GoatOsColors;
 * they match the mock exactly so the screen is a faithful port, not a substitute.
 */
private object CalTokens {
    val Brand = Color(0xFF8AD457)
    val Brand2 = Color(0xFF5FB531)
    val BrandD = Color(0xFFB7EA8C)
    val OnBrand = Color(0xFF08130B)
    val PageBg = Color(0xFF0A0F0C)
    val Surf = Color(0xFF131A15)
    val Surf2 = Color(0xFF1A241D)
    val Surf3 = Color(0xFF222E25)
    val Hair = Color(0xFF28352B)
    val Ink = Color(0xFFECF4EE)
    val Muted = Color(0xFF8FA497)
    val Faint = Color(0xFF5F7367)
    val Warn = Color(0xFFF0B54B)
    val WarnX = Color(0x26F0B54B)
    val Danger = Color(0xFFFB6F63)
    val DangerX = Color(0x26FB6F63)
    val OkX = Color(0x298AD457)
    val BrandGradient = Brush.linearGradient(listOf(Color(0xFF93DA5E), Color(0xFF5FB531)))
}

/* --------------------------------------------------------------------------- */
/* Preview                                                                     */
/* --------------------------------------------------------------------------- */

private fun previewState(): CalendarUiState = CalendarUiState(
    eyebrow = "Vaccination",
    title = "Calendar",
    selectedDateLabel = "Today · Tue 7 Jul",
    windowLabel = "08:00–20:00",
    segments = listOf(
        CalendarSegment("week", "Week", CalendarSegmentKind.Week),
        CalendarSegment("month", "Month", CalendarSegmentKind.Month),
        CalendarSegment("history", "History", CalendarSegmentKind.History),
    ),
    selectedSegmentId = "week",
    weekDays = listOf(
        CalendarWeekDay("d6", "Mon", "6", "0 due", hasWork = false),
        CalendarWeekDay("d7", "Tue", "7", "77 due", hasWork = true, isToday = true),
        CalendarWeekDay("d8", "Wed", "8", "76 due", hasWork = true),
        CalendarWeekDay("d9", "Thu", "9", "0 due", hasWork = false),
        CalendarWeekDay("d10", "Fri", "10", "54 due", hasWork = true),
        CalendarWeekDay("d11", "Sat", "11", "0 due", hasWork = false),
        CalendarWeekDay("d12", "Sun", "12", "0 due", hasWork = false),
    ),
    weekItems = listOf(
        CalendarItem(
            id = "drive-today",
            title = "Today · 4 sheds",
            subtitle = "77 animals due · CBE · Gandhi 1 · Castro 1 · Mandela 1 · Sumathi 1",
            statusLabel = "Due now",
            statusTone = CalendarTone.Ok,
            categoryLabel = "Vaccination",
            ctaLabel = "Open drive",
            target = "sheds?scope_token=abc",
        ),
    ),
    weekEmptyLabel = "No drives scheduled",
    monthLabel = "Jul 2026",
    monthWeekdayLabels = listOf("S", "M", "T", "W", "T", "F", "S"),
    monthDays = buildMonthPreview(),
    monthHint = "Tap a day for its drives · dots = drive days",
    historyLabel = "Past drives · 2",
    historyRows = listOf(
        CalendarHistoryRow("h0", "ET + TT · Primary", "Jul 1 2026 · done", "98%", CalendarTone.Ok, "record/h0"),
        CalendarHistoryRow("h1", "FMD · Booster", "Jun 28 2026 · done", "95%", CalendarTone.Ok, "record/h1"),
    ),
    historyEmptyLabel = "No past drives",
)

private fun buildMonthPreview(): List<CalendarMonthDay> {
    val blanks = List(3) { CalendarMonthDay(dateKey = null, dayNumber = null) }
    val work = mapOf(
        1 to CalendarTone.Ok, 7 to CalendarTone.Ok, 8 to CalendarTone.Warn,
        10 to CalendarTone.Warn, 15 to CalendarTone.Warn, 22 to CalendarTone.Warn, 28 to CalendarTone.Ok,
    )
    val days = (1..31).map { d ->
        CalendarMonthDay(
            dateKey = "jul-$d",
            dayNumber = d.toString(),
            hasWork = work.containsKey(d),
            dotTone = work[d] ?: CalendarTone.Neutral,
            isSelected = d == 8,
        )
    }
    return blanks + days
}

@Preview(showBackground = true, backgroundColor = 0xFF0A0F0C)
@Composable
private fun CalendarScreenPreview() {
    GoatOsTheme {
        CalendarScreen(state = previewState())
    }
}
