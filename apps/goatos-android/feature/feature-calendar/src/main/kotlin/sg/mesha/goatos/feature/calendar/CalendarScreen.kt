package sg.mesha.goatos.feature.calendar

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.nav.LocalDrawerOpener
import sg.mesha.goatos.core.ui.CoverageBanner
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import sg.mesha.goatos.feature.calendar.R

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
            .background(MeshaColors.PageBg)
            .padding(horizontal = Gutter),
    ) {
        item { CalendarHeader(state, onEvent) }
        if (state.coverageBanner != null) {
            item {
                CoverageBanner(state = state.coverageBanner, modifier = Modifier.fillMaxWidth())
                Spacer(Modifier.size(10.dp))
            }
        }
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
    val openDrawer = LocalDrawerOpener.current
    Row(
        Modifier.fillMaxWidth().padding(top = 16.dp, bottom = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Menu button to open the module drawer
        HeaderIconButton(onClick = { openDrawer() }, icon = MeshaIcons.Menu, contentDescription = stringResource(R.string.calendar_button_menu))
        Column(Modifier.weight(1f).padding(start = 8.dp)) {
            if (state.eyebrow.isNotEmpty()) {
                Text(
                    // Static module eyebrow — localized client-side (the VM value is the
                    // English module name; the visible chrome must follow the app locale).
                    text = stringResource(R.string.calendar_eyebrow).uppercase(),
                    color = MeshaColors.Faint,
                    fontSize = 10.5.sp,
                    fontWeight = FontWeight.W700,
                    letterSpacing = 0.6.sp,
                )
            }
            Text(
                text = stringResource(R.string.calendar_title),
                color = MeshaColors.Ink,
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
                    color = MeshaColors.Muted,
                    fontSize = 12.5.sp,
                    fontWeight = FontWeight.W600,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            // Offline-first sync/stale affordance (docs/decisions/android-offline-first.md):
            // renders nothing while there is no cache yet — a cold-start/error placeholder
            // above already covers that moment — otherwise "Syncing…" / "Updated Xm ago" /
            // "Offline · updated Xm ago", NEVER a second loading wall over live content.
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                // [CalendarUiState.lastSyncedAt] is only ever non-null once a Room cache row
                // has been observed (set from Resource.lastSyncedAt in the ViewModel), so it
                // doubles as the "do we have anything cached to annotate" signal.
                hasData = state.lastSyncedAt != null,
                isOffline = state.isOffline,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        // Refresh button on the right
        HeaderIconButton(onClick = { onEvent(CalendarEvent.Refresh) }, icon = MeshaIcons.Refresh, contentDescription = stringResource(R.string.calendar_button_refresh))
    }
}

@Composable
private fun HeaderIconButton(onClick: () -> Unit, icon: androidx.compose.ui.graphics.vector.ImageVector = MeshaIcons.Refresh, contentDescription: String = "Button") {
    Box(
        Modifier
            .size(38.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = contentDescription,
            tint = MeshaColors.Muted,
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
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(13.dp))
            .padding(4.dp),
    ) {
        segments.forEach { seg ->
            val on = seg.id == selectedId
            Box(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(9.dp))
                    .then(if (on) Modifier.background(MeshaColors.BrandGradient) else Modifier)
                    .clickable { onSelect(seg.id) }
                    .padding(vertical = 9.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    // Segment labels are fixed chrome — localize by kind, not the VM's English label.
                    text = segmentLabel(seg.kind, seg.label),
                    color = if (on) MeshaColors.OnBrand else MeshaColors.Muted,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** Localized label for a calendar segment, keyed by its kind (fixed UI chrome). */
@Composable
private fun segmentLabel(kind: CalendarSegmentKind, fallback: String): String = when (kind) {
    CalendarSegmentKind.Week -> stringResource(R.string.calendar_segment_week)
    CalendarSegmentKind.Month -> stringResource(R.string.calendar_segment_month)
    CalendarSegmentKind.History -> stringResource(R.string.calendar_segment_history)
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
        item {
            EmptyState(
                title = stringResource(R.string.calendar_week_empty),
                icon = MeshaIcons.Calendar,
            )
        }
    } else {
        items(state.weekItems, key = { it.id }) { item ->
            EventCard(item = item, onClick = { onEvent(CalendarEvent.TapItem(item.id)) })
        }
        if (state.weekHasMore) {
            item {
                LoadMoreButton(
                    label = stringResource(R.string.calendar_load_more_day),
                    loading = state.weekLoadingMore,
                    onClick = { onEvent(CalendarEvent.LoadMoreWeek) },
                )
            }
        }
    }
}

@Composable
private fun WeekDayCell(
    day: CalendarWeekDay,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    // Mock CSS: `.week .d` and `.week .d.today` share the same `padding:10px 0` — the
    // selected cell is distinguished by its gradient background, not by extra height.
    // A tap moves the highlight to whichever day is selected (mirrors mock `sel`), so
    // the selected day — not the literal calendar date — decides the "on" look.
    val on = day.isSelected
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .then(if (on) Modifier.background(MeshaColors.BrandGradient) else Modifier.background(MeshaColors.Surf))
            .border(1.dp, if (on) Color.Transparent else MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            .padding(vertical = 10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = day.dayName.uppercase(),
            color = if (on) MeshaColors.OnBrand else MeshaColors.Faint,
            fontSize = 9.5.sp,
            fontWeight = FontWeight.W700,
        )
        Text(
            text = day.dayNumber,
            color = if (on) MeshaColors.OnBrand else MeshaColors.Ink,
            fontSize = 15.sp,
            fontWeight = FontWeight.W800,
            modifier = Modifier.padding(top = 4.dp),
        )
        if (day.dueCountLabel.isNotEmpty()) {
            Text(
                text = day.dueCountLabel,
                color = if (on) MeshaColors.OnBrand else MeshaColors.Muted,
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
                    .background(if (on) MeshaColors.OnBrand else MeshaColors.Brand),
            )
        }
    }
}

@Composable
private fun EventCard(item: CalendarItem, onClick: () -> Unit) {
    val drillable = item.ctaLabel != null && item.target != null
    val drive = item.driveSummary
    Column(
        Modifier
            .fillMaxWidth()
            .padding(bottom = 11.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .leftAccent(MeshaColors.Brand)
            .then(if (drillable) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(start = 16.dp, top = 15.dp, end = 16.dp, bottom = 15.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            StatusPill(item.statusLabel, item.statusTone)
            Spacer(Modifier.weight(1f))
            // Drive cards (v4) carry their own due-date badge below; the generic
            // all-day/time pill would otherwise double up with it (e.g. "Due now" +
            // "Today" + "Today"), so it only renders for non-drive items.
            if ((item.allDay || item.timeLabel.isNotEmpty()) && drive == null) {
                StatusPill(if (item.allDay) stringResource(R.string.calendar_all_day) else item.timeLabel, CalendarTone.Neutral)
                Spacer(Modifier.size(6.dp))
            }
            // Drive due date (v4 park-level card) sits alongside the headline status pill.
            drive?.dueDateLabel?.takeIf { it.isNotEmpty() }?.let {
                StatusPill(it, CalendarTone.Neutral)
                Spacer(Modifier.size(6.dp))
            }
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
                tint = if (drillable) MeshaColors.Brand else MeshaColors.Faint,
                modifier = Modifier.size(16.dp),
            )
            Spacer(Modifier.size(8.dp))
            Text(
                // Park-level drives (v4) get the fixed "Vaccination drive · <park>" chrome
                // (localized here); everything else keeps the backend-supplied item.title.
                text = if (drive != null) {
                    stringResource(R.string.calendar_drive_title, drive.parkName)
                } else {
                    item.title
                },
                color = if (drillable) MeshaColors.Ink else MeshaColors.Muted,
                fontSize = 15.sp,
                fontWeight = FontWeight.W700,
            )
        }
        // Suppress the flat subtitle/summary rows when a park-level drive_summary is present:
        // the DriveProgressCard below already renders sheds/vaccines/dose totals, so these would
        // duplicate them above the ring (CDR-003). They still render for non-drive-summary rows.
        if (drive == null && item.subtitle.isNotEmpty()) {
            Text(
                text = item.subtitle,
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W500,
                modifier = Modifier.padding(top = 7.dp),
            )
        }
        if (drive == null && item.summaryPrimary.isNotEmpty()) {
            Text(
                text = item.summaryPrimary,
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
        if (drive == null && item.summarySecondary.isNotEmpty()) {
            Text(
                text = item.summarySecondary,
                color = MeshaColors.Muted,
                fontSize = 11.5.sp,
                fontWeight = FontWeight.W600,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        if (item.aggregated) {
            if (drive != null) {
                // v4 park-level drive progress card — rendered purely from the backend
                // drive_summary; no target-row fetch/parse happens here.
                DriveProgressCard(summary = drive, modifier = Modifier.padding(top = 10.dp))
            } else {
                // Legacy fallback while a backend response predates drive_summary.
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(top = 10.dp),
                    horizontalArrangement = Arrangement.spacedBy(7.dp),
                ) {
                    DriveMetric(
                        label = stringResource(R.string.calendar_drive_sheds),
                        value = item.shedCount.toString(),
                        modifier = Modifier.weight(1f),
                    )
                    DriveMetric(
                        label = stringResource(R.string.calendar_drive_vaccines),
                        value = item.vaccineCount.toString(),
                        modifier = Modifier.weight(1f),
                    )
                    DriveMetric(
                        label = stringResource(R.string.calendar_drive_doses),
                        value = item.targetCount.toString(),
                        modifier = Modifier.weight(1f),
                    )
                }
                if (item.vaccineLabels.isNotEmpty()) {
                    Text(
                        text = item.vaccineLabels.joinToString(" · "),
                        color = MeshaColors.Brand2,
                        fontSize = 11.sp,
                        fontWeight = FontWeight.W700,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        }
        val ownerLabel = drive?.ownerLabel?.takeIf { it.isNotEmpty() }
        if (item.ctaLabel != null || ownerLabel != null) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.padding(top = 11.dp),
            ) {
                // Footer owner (v4 drive card): who owns follow-up on this drive.
                ownerLabel?.let {
                    Text(text = it, color = MeshaColors.Muted, fontSize = 12.5.sp, fontWeight = FontWeight.W600)
                    if (item.ctaLabel != null) {
                        Text(text = "  ·  ", color = MeshaColors.Muted, fontSize = 12.5.sp, fontWeight = FontWeight.W600)
                    }
                }
                item.ctaLabel?.let {
                    // ctaLabel non-null = drillable; the verb itself is fixed chrome, localized
                    // here (the VM's English "Open"/"Open drive" is ignored so it follows the
                    // app locale). Drive cards (v4) get the more specific "Open drive →".
                    val (ctaText, ctaGlyph) = if (drive != null) {
                        stringResource(R.string.calendar_drive_open) to "→"
                    } else {
                        stringResource(R.string.calendar_cta_open) to "›"
                    }
                    Text(
                        text = "$ctaText  $ctaGlyph",
                        color = MeshaColors.Brand2,
                        fontSize = 12.5.sp,
                        fontWeight = FontWeight.W700,
                    )
                }
            }
        }
    }
}

/**
 * v4 park-level drive progress card — option A (completion ring), owner-approved.
 * Every value here is a straight [CalendarDriveSummary] field read; nothing is fetched,
 * paginated, or aggregated client-side (mobile-guard: card = summary, not a rollup).
 * Anatomy: a muted "{n} vaccines" subtitle (the full vaccine list lives in the drill
 * target, not the card) + a completion ring/count pair + non-zero due/overdue/deferred
 * chips. The old vaccine-chip FlowRow and two-stat-box + linear-bar layout are gone.
 */
@Composable
// telemetry:exempt DriveProgressCard is a presentational render of the backend drive_summary read model; it adds no new user action or funnel step (the card's tap-to-drill navigation is the pre-existing EventCard onClick, already instrumented at the navigation layer).
private fun DriveProgressCard(summary: CalendarDriveSummary, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth()) {
        if (summary.vaccineLabels.isNotEmpty()) {
            Text(
                text = stringResource(R.string.calendar_drive_vaccines_fmt, summary.vaccineLabels.size),
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
            )
            Spacer(Modifier.size(10.dp))
        }
        val pct = if (summary.totalCount > 0) Math.round(summary.completedCount * 100.0 / summary.totalCount).toInt() else 0
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            DriveProgressRing(pct = pct)
            Column {
                Row(verticalAlignment = Alignment.Bottom) {
                    Text(
                        text = summary.completedCount.toString(),
                        color = MeshaColors.Ink,
                        fontSize = 17.sp,
                        fontWeight = FontWeight.W800,
                    )
                    Text(
                        text = " / ${summary.totalCount} ${stringResource(R.string.calendar_drive_doses)}",
                        color = MeshaColors.Muted,
                        fontSize = 12.5.sp,
                        fontWeight = FontWeight.W600,
                    )
                }
                Text(
                    text = stringResource(
                        R.string.calendar_drive_sheds_done,
                        summary.shedsCompleted,
                        summary.shedCount,
                    ),
                    color = MeshaColors.Muted,
                    fontSize = 11.5.sp,
                    fontWeight = FontWeight.W600,
                    modifier = Modifier.padding(top = 3.dp),
                )
            }
        }
        val remaining = buildList {
            if (summary.dueCount > 0) {
                add(stringResource(R.string.calendar_drive_due, summary.dueCount) to CalendarTone.Warn)
            }
            if (summary.overdueCount > 0) {
                add(stringResource(R.string.calendar_drive_overdue, summary.overdueCount) to CalendarTone.Danger)
            }
            if (summary.deferredCount > 0) {
                add(stringResource(R.string.calendar_drive_deferred, summary.deferredCount) to CalendarTone.Neutral)
            }
        }
        if (remaining.isNotEmpty()) {
            Spacer(Modifier.size(12.dp))
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                remaining.forEach { (label, tone) -> StatusPill(label, tone) }
            }
        }
    }
}

/**
 * Completion ring (option A): a faint track arc + a brand-green progress arc drawn with
 * [Canvas], centered percentage text. Mirrors the SVG ring in the mobile mock
 * (`mock/vaccination-mobile-mock.html` `#ringwrap`/`#ring` and the shed-row `.miniring`).
 */
@Composable
private fun DriveProgressRing(pct: Int, modifier: Modifier = Modifier) {
    val trackColor = MeshaColors.Surf3
    val progressColor = MeshaColors.Brand
    val clampedPct = pct.coerceIn(0, 100)
    Box(modifier.size(68.dp), contentAlignment = Alignment.Center) {
        Canvas(modifier = Modifier.fillMaxSize()) {
            val strokeWidthPx = 7.dp.toPx()
            val diameter = size.minDimension - strokeWidthPx
            val topLeft = Offset((size.width - diameter) / 2f, (size.height - diameter) / 2f)
            val arcSize = Size(diameter, diameter)
            drawArc(
                color = trackColor,
                startAngle = -90f,
                sweepAngle = 360f,
                useCenter = false,
                topLeft = topLeft,
                size = arcSize,
                style = Stroke(width = strokeWidthPx, cap = StrokeCap.Round),
            )
            drawArc(
                color = progressColor,
                startAngle = -90f,
                sweepAngle = 360f * clampedPct / 100f,
                useCenter = false,
                topLeft = topLeft,
                size = arcSize,
                style = Stroke(width = strokeWidthPx, cap = StrokeCap.Round),
            )
        }
        Text(
            text = "$clampedPct%",
            color = MeshaColors.Ink,
            fontSize = 13.sp,
            fontWeight = FontWeight.W800,
        )
    }
}

@Composable
private fun DriveMetric(label: String, value: String, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.PageBg)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .padding(horizontal = 9.dp, vertical = 9.dp),
    ) {
        Text(
            text = label.uppercase(),
            color = MeshaColors.Muted,
            fontSize = 9.sp,
            fontWeight = FontWeight.W700,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = value,
            color = MeshaColors.Ink,
            fontSize = 15.sp,
            fontWeight = FontWeight.W800,
            modifier = Modifier.padding(top = 3.dp),
        )
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
            color = MeshaColors.Ink,
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
                        color = MeshaColors.Faint,
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
                    onClick = {
                        cell.dateKey?.let {
                            onEvent(
                                CalendarEvent.OpenDay(
                                    dateKey = it,
                                    showCompletedHistory = !cell.hasWork && cell.hasCompletedHistory,
                                ),
                            )
                        }
                    },
                )
            }
            repeat(7 - week.size) { Spacer(Modifier.weight(1f)) }
        }
    }
    if (state.monthHint.isNotEmpty()) {
        item {
            Text(
                text = stringResource(R.string.calendar_month_hint),
                color = MeshaColors.Faint,
                fontSize = 10.5.sp,
                modifier = Modifier.fillMaxWidth().padding(vertical = 9.dp),
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
            )
        }
    }
    // A month day tap now opens its own L1 screen (CalendarDayScreen) instead of
    // appending a sheet below the grid — see CalendarEvent.OpenDay / the nav host.
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
            .background(MeshaColors.Surf)
            .border(
                1.dp,
                if (cell.isSelected || cell.hasWork || cell.hasCompletedHistory) MeshaColors.Brand else MeshaColors.Hair,
                RoundedCornerShape(10.dp),
            )
            .clickable(enabled = cell.dateKey != null, onClick = onClick),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = cell.dayNumber,
            color = if (cell.hasWork || cell.hasCompletedHistory) MeshaColors.Ink else MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W600,
        )
        if (cell.hasWork || cell.hasCompletedHistory) {
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
/* Day detail — L1 screen                                                      */
/* --------------------------------------------------------------------------- */

/**
 * The L1 day-detail screen: a full navigation destination showing the drives/sheds due on
 * a tapped month day. Opened by [CalendarEvent.OpenDay] (routed by the nav host), NOT a
 * sheet appended under the calendar grid. Offline-first: renders its cached
 * [CalendarDayUiState] instantly and shows a sync/stale indicator, never a blank wall on
 * re-entry. Tapping a drive drills on via [onItemTap] (the backend-supplied target).
 */
@Composable
fun CalendarDayScreen(
    state: CalendarDayUiState,
    onBack: () -> Unit = {},
    onItemTap: (String) -> Unit = {},
    onLoadMore: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg)
            .padding(horizontal = Gutter),
    ) {
        Row(
            Modifier.fillMaxWidth().padding(top = 16.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HeaderIconButton(
                onClick = onBack,
                icon = MeshaIcons.ChevronLeft,
                contentDescription = stringResource(R.string.calendar_day_back),
            )
            Column(Modifier.weight(1f).padding(start = 12.dp)) {
                Text(
                    text = state.title,
                    color = MeshaColors.Ink,
                    fontSize = 20.sp,
                    fontWeight = FontWeight.W800,
                )
                SyncStatusIndicator(
                    isRefreshing = state.isRefreshing,
                    lastSyncedAt = state.lastSyncedAt,
                    hasData = state.lastSyncedAt != null,
                    isOffline = state.isOffline,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }
        LazyColumn(Modifier.fillMaxSize()) {
            if (state.items.isEmpty()) {
                item {
                    EmptyState(
                        title = when {
                            state.emptyLabel.isNotEmpty() -> state.emptyLabel
                            state.showCompletedHistory -> stringResource(R.string.calendar_history_empty)
                            else -> stringResource(R.string.calendar_day_sheet_empty)
                        },
                        icon = MeshaIcons.Calendar,
                    )
                }
            } else {
                items(state.items, key = { it.id }) { item ->
                    EventCard(item = item, onClick = { onItemTap(item.id) })
                }
                if (state.hasMore) {
                    item {
                        LoadMoreButton(
                            label = stringResource(R.string.calendar_load_more_day),
                            loading = state.isLoadingMore,
                            onClick = onLoadMore,
                        )
                    }
                }
            }
            item { Spacer(Modifier.size(24.dp)) }
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
    item {
        SectionLabel(
            state.historyLabel.ifEmpty {
                stringResource(R.string.calendar_history_label) + " · " + state.historyCount
            },
        )
    }
    if (state.historyRows.isEmpty()) {
        item {
            EmptyState(
                title = state.historyEmptyLabel.ifEmpty { stringResource(R.string.calendar_history_empty) },
                icon = MeshaIcons.Clock,
            )
        }
    } else {
        items(state.historyRows, key = { it.id }) { row ->
            HistoryRow(row = row, onClick = { onEvent(CalendarEvent.TapItem(row.id)) })
        }
        if (state.historyHasMore) {
            item {
                LoadMoreButton(
                    label = stringResource(R.string.calendar_load_more_history),
                    loading = state.historyLoadingMore,
                    onClick = { onEvent(CalendarEvent.LoadMoreHistory) },
                )
            }
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
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(15.dp))
            .clickable(enabled = row.target != null, onClick = onClick)
            .padding(horizontal = 15.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(38.dp)
                .clip(RoundedCornerShape(11.dp))
                .background(MeshaColors.OkX),
            contentAlignment = Alignment.Center,
        ) {
            // Syringe = Vaccination module marker (mock icon set).
            Icon(
                imageVector = MeshaIcons.Syringe,
                contentDescription = null,
                tint = MeshaColors.Brand,
                modifier = Modifier.size(17.dp),
            )
        }
        Column(Modifier.weight(1f).padding(horizontal = 11.dp)) {
            Text(
                text = row.title,
                color = MeshaColors.Ink,
                fontSize = 13.5.sp,
                fontWeight = FontWeight.W700,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = row.subtitle,
                color = MeshaColors.Muted,
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
        color = MeshaColors.Faint,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        letterSpacing = 0.55.sp,
        modifier = Modifier.fillMaxWidth().padding(top = 14.dp, bottom = 6.dp),
    )
}

@Composable
private fun LoadMoreButton(label: String, loading: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .fillMaxWidth()
            .padding(top = 6.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(enabled = !loading, onClick = onClick)
            .padding(vertical = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = if (loading) stringResource(R.string.calendar_loading_more) else label,
            color = if (loading) MeshaColors.Muted else MeshaColors.Brand2,
            fontSize = 12.5.sp,
            fontWeight = FontWeight.W700,
        )
    }
}

@Composable
private fun StatusPill(label: String, tone: CalendarTone) {
    // Derive tone from label keywords for consistent coloring across screens
    val effectiveTone = toneFromStatusLabel(label, tone)
    Box(
        Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(toneBackground(effectiveTone))
            .padding(horizontal = 10.dp, vertical = 4.dp),
    ) {
        Text(
            text = label,
            color = toneText(effectiveTone),
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

/**
 * Maps a status label to the authoritative tone. If the label contains keywords
 * like "due", "done", "delayed", or "overdue", the tone is derived from those
 * keywords, ensuring consistency with detail screens. Otherwise, the provided tone
 * is used as-is.
 */
private fun toneFromStatusLabel(label: String, providedTone: CalendarTone): CalendarTone {
    val lowerLabel = label.lowercase()
    return when {
        lowerLabel.contains("due") || lowerLabel.contains("warning") -> CalendarTone.Warn
        lowerLabel.contains("done") || lowerLabel.contains("completed") -> CalendarTone.Ok
        lowerLabel.contains("delayed") || lowerLabel.contains("overdue") || lowerLabel.contains("missed") -> CalendarTone.Danger
        else -> providedTone
    }
}

private fun toneColor(tone: CalendarTone): Color = when (tone) {
    CalendarTone.Ok -> MeshaColors.Brand
    CalendarTone.Warn -> MeshaColors.Warn
    CalendarTone.Danger -> MeshaColors.Danger
    CalendarTone.Muted -> MeshaColors.Muted
    CalendarTone.Neutral -> MeshaColors.Faint
}

private fun toneBackground(tone: CalendarTone): Color = when (tone) {
    CalendarTone.Ok -> MeshaColors.OkX
    CalendarTone.Warn -> MeshaColors.WarnX
    CalendarTone.Danger -> MeshaColors.DangerX
    CalendarTone.Muted, CalendarTone.Neutral -> MeshaColors.Surf3
}

private fun toneText(tone: CalendarTone): Color = when (tone) {
    CalendarTone.Ok -> MeshaColors.BrandD
    CalendarTone.Warn -> MeshaColors.Warn
    CalendarTone.Danger -> MeshaColors.Danger
    CalendarTone.Muted, CalendarTone.Neutral -> MeshaColors.Muted
}

private val Gutter = MeshaDimens.gutter

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
            subtitle = "",
            aggregated = true,
            statusLabel = "Due now",
            statusTone = CalendarTone.Ok,
            driveSummary = CalendarDriveSummary(
                parkName = "CBE",
                dueDateLabel = "Tue 7 Jul",
                shedCount = 4,
                shedsCompleted = 1,
                vaccineLabels = listOf("FMD", "HS"),
                totalCount = 77,
                completedCount = 20,
                remainingCount = 57,
                dueCount = 40,
                overdueCount = 12,
                deferredCount = 3,
                ownerLabel = "Arun Kumar",
            ),
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
