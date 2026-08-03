package sg.mesha.goatos.feature.calendar

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
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
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.ui.CoverageBanner
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
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
    monthItems: LazyPagingItems<CalendarItem>? = null,
    onEvent: (CalendarEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val selected = state.segments.firstOrNull { it.id == state.selectedSegmentId }
        ?: state.segments.firstOrNull()
    var showMonthFilters by rememberSaveable { mutableStateOf(false) }
    val listState = rememberLazyListState()
    // Refresh-on-open (offline-first stale-while-revalidate): auto-sync every time the screen
    // resumes/foregrounds, not just on first ViewModel creation. Without this, a retained
    // ViewModel on the nav backstack shows the value it fetched once — so data that changed on the
    // server after that first load (e.g. a drive-date move / a fixed dose count) stayed stale until
    // the user tapped the manual sync button. The Room cache keeps the last value visible while the
    // background refresh runs; it never blanks the screen.
    RefreshOnResume { onEvent(CalendarEvent.Refresh) }
    LaunchedEffect(
        listState,
        selected?.kind,
        state.weekHasMore,
        state.weekLoadingMore,
        state.weekItems.size,
    ) {
        val shouldAutoLoad = when (selected?.kind) {
            CalendarSegmentKind.Week -> state.weekHasMore && !state.weekLoadingMore && state.weekItems.isNotEmpty()
            else -> false
        }
        if (!shouldAutoLoad) return@LaunchedEffect
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { lastVisibleIndex ->
                if (lastVisibleIndex >= listState.layoutInfo.totalItemsCount - 4) {
                    when (selected?.kind) {
                        CalendarSegmentKind.Week ->
                            if (state.weekHasMore && !state.weekLoadingMore) onEvent(CalendarEvent.LoadMoreWeek)
                        else -> Unit
                    }
                }
            }
    }

    LazyColumn(
        state = listState,
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg)
            .padding(horizontal = Gutter),
    ) {
        item {
            CalendarHeader(
                state = state,
                onEvent = onEvent,
                onOpenFilters = { showMonthFilters = true },
            )
        }
        state.errorMessage?.let { message ->
            item {
                Column(Modifier.fillMaxWidth().padding(vertical = 16.dp)) {
                    Text(text = message, color = MeshaColors.Danger, fontWeight = FontWeight.W600)
                    Text(
                        text = "Retry",
                        color = MeshaColors.Brand,
                        fontWeight = FontWeight.W700,
                        modifier = Modifier.padding(top = 8.dp).clickable { onEvent(CalendarEvent.Refresh) },
                    )
                }
            }
        }
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
            CalendarSegmentKind.Month -> monthContent(
                state = state,
                monthItems = monthItems,
                onEvent = onEvent,
            )
            null -> Unit
        }
        item { Spacer(Modifier.size(24.dp)) }
    }

    if (showMonthFilters) {
        MonthFilterSheet(
            state = state,
            onDismiss = { showMonthFilters = false },
            onApply = {
                onEvent(CalendarEvent.ApplyMonthFilters(it))
                showMonthFilters = false
            },
            onClear = {
                onEvent(CalendarEvent.ClearMonthFilters)
                showMonthFilters = false
            },
        )
    }
}

/* --------------------------------------------------------------------------- */
/* Header                                                                      */
/* --------------------------------------------------------------------------- */

/**
 * Calendar header.
 *
 * The hamburger is no longer drawn here. It used to be — Calendar was one of only two screens
 * that remembered to read `LocalDrawerOpener` and render a menu button, which is exactly how the
 * app ended up with modules that had no drawer at all. The leading affordance now comes from
 * [MeshaScreenHeader], driven by the shell's backend-composed L0 membership.
 */
@Composable
private fun CalendarHeader(
    state: CalendarUiState,
    onEvent: (CalendarEvent) -> Unit,
    onOpenFilters: () -> Unit,
) {
    val window = state.windowLabel
    val activeFilterCount = state.monthFilters.secondaryFilterCount
    MeshaScreenHeader(
        // Static screen chrome — localized client-side (the VM values are the English module
        // name/title; the visible chrome must follow the app locale).
        title = stringResource(R.string.calendar_title),
        eyebrow = stringResource(R.string.calendar_eyebrow).uppercase().takeIf { state.eyebrow.isNotEmpty() },
        subtitle = if (window.isNullOrEmpty()) {
            state.selectedDateLabel
        } else {
            "${state.selectedDateLabel}  ·  $window"
        }.takeIf { it.isNotEmpty() },
        contentPadding = PaddingValues(top = 16.dp, bottom = 12.dp),
        below = {
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
        },
        actions = {
            HeaderIconButton(
                onClick = onOpenFilters,
                icon = MeshaIcons.Filter,
                contentDescription = if (activeFilterCount > 0) {
                    "${stringResource(R.string.calendar_filters)} ($activeFilterCount)"
                } else {
                    stringResource(R.string.calendar_filters)
                },
            )
            Spacer(Modifier.size(8.dp))
            SyncIconButton(
                isSyncing = state.isRefreshing,
                onSync = { onEvent(CalendarEvent.Refresh) },
                contentDescription = stringResource(R.string.calendar_button_refresh),
            )
        },
    )
}

@Composable
private fun HeaderIconButton(
    onClick: () -> Unit,
    icon: androidx.compose.ui.graphics.vector.ImageVector = MeshaIcons.Refresh,
    contentDescription: String,
) {
    Box(
        Modifier
            .size(48.dp)
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
}

/* --------------------------------------------------------------------------- */
/* Week                                                                        */
/* --------------------------------------------------------------------------- */

private fun androidx.compose.foundation.lazy.LazyListScope.weekContent(
    state: CalendarUiState,
    onEvent: (CalendarEvent) -> Unit,
) {
    item {
        val dayCellHeight = if (state.weekDays.any { it.dueCountLabel.isNotEmpty() || it.bucketCount > 0 }) 82.dp else 68.dp
        Row(
            Modifier.fillMaxWidth().padding(vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            state.weekDays.forEach { day ->
                WeekDayCell(
                    day = day,
                    modifier = Modifier.weight(1f).height(dayCellHeight),
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
            EventCard(
                item = item,
                onClick = {
                    onEvent(
                        CalendarEvent.TapItem(
                            itemId = item.id,
                            target = item.target,
                            dateKey = item.dateKey,
                            parkId = item.parkId,
                        ),
                    )
                },
            )
        }
        if (state.weekLoadingMore) {
            item {
                InlineLoadingFooter()
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
        verticalArrangement = Arrangement.Center,
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
        val bucketLabel = localizedWeekBucketLabel(day)
        if (bucketLabel.isNotEmpty()) {
            Text(
                text = bucketLabel,
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
private fun localizedWeekBucketLabel(day: CalendarWeekDay): String {
    if (day.bucketCount <= 0) return day.dueCountLabel
    val label = when (day.bucketKey) {
        "overdue" -> R.string.calendar_drive_overdue
        "deferred" -> R.string.calendar_drive_deferred
        else -> R.string.calendar_drive_due
    }
    return stringResource(label, day.bucketCount)
}

@Composable
private fun EventCard(item: CalendarItem, onClick: () -> Unit, showScheduleContext: Boolean = false) {
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
        if (showScheduleContext && (item.dateLabel.isNotEmpty() || item.parkLabel.isNotEmpty())) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(bottom = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = item.dateLabel,
                    color = MeshaColors.Brand2,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.W800,
                )
                Spacer(Modifier.weight(1f))
                if (item.parkLabel.isNotEmpty()) {
                    Text(
                        text = item.parkLabel,
                        color = MeshaColors.Muted,
                        fontSize = 11.5.sp,
                        fontWeight = FontWeight.W700,
                    )
                }
            }
        }
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
            modifier = Modifier.fillMaxWidth(),
        ) {
            StatusPill(humanStatusLabel(item.statusLabel), item.statusTone)
            // Drive cards (v4) carry their own due-date badge below; the generic
            // all-day/time pill would otherwise double up with it (e.g. "Due now" +
            // "Today" + "Today"), so it only renders for non-drive items.
            if ((item.allDay || item.timeLabel.isNotEmpty()) && drive == null) {
                StatusPill(if (item.allDay) stringResource(R.string.calendar_all_day) else item.timeLabel, CalendarTone.Neutral)
            }
            // Drive due date (v4 park-level card) sits alongside the headline status pill.
            drive?.dueDateLabel?.takeIf { it.isNotEmpty() }?.let {
                StatusPill(it, CalendarTone.Neutral)
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
            Column {
                Text(
                    // The drive's OWN name is what distinguishes one multi-day drive from the next.
                    // The fixed "Vaccination drive · <park>" chrome is only a fallback: rendering it
                    // for every row made every day of every drive read as the same untitled card.
                    text = drive?.driveName?.takeIf { it.isNotBlank() }
                        ?: if (drive != null) {
                            stringResource(R.string.calendar_drive_title, drive.parkName)
                        } else {
                            item.title
                        },
                    color = if (drillable) MeshaColors.Ink else MeshaColors.Muted,
                    fontSize = 15.sp,
                    fontWeight = FontWeight.W700,
                )
                // Park stays visible even when the drive name replaced the chrome above.
                drive?.parkName?.takeIf { it.isNotBlank() && drive.driveName.isNotBlank() }?.let { park ->
                    Text(
                        text = park,
                        color = MeshaColors.Muted,
                        fontSize = 11.5.sp,
                        fontWeight = FontWeight.W600,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
        }
        // For drive rows the backend populates subtitle/summaryPrimary/summarySecondary with the
        // same sheds/vaccines/scheduled-dose metrics the option-A ring card now shows, so suppress
        // them when drive_summary is present (drive != null) to avoid rendering the numbers twice.
        if (legacyDriveRowsVisible(drive) && item.subtitle.isNotEmpty()) {
            Text(
                text = item.subtitle,
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W500,
                modifier = Modifier.padding(top = 7.dp),
            )
        }
        if (legacyDriveRowsVisible(drive) && item.summaryPrimary.isNotEmpty()) {
            Text(
                text = item.summaryPrimary,
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
        if (legacyDriveRowsVisible(drive) && item.summarySecondary.isNotEmpty()) {
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
                if (showScheduleContext && item.shedLabels.isNotEmpty()) {
                    Text(
                        text = item.shedLabels.joinToString(" · "),
                        color = MeshaColors.Muted,
                        fontSize = 11.sp,
                        fontWeight = FontWeight.W600,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.padding(top = 6.dp),
                    )
                }
            }
        }
        // The whole card is the tap target (drillable is still driven by ctaLabel above); the
        // redundant "Open"/"Open drive ›" verb is intentionally NOT rendered.
        val ownerLabel = item.assigneeLabel?.takeIf { it.isNotEmpty() }
            ?: drive?.ownerLabel?.takeIf { it.isNotEmpty() }
        if (ownerLabel != null) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.padding(top = 11.dp),
            ) {
                // Footer owner (v4 drive card): who owns follow-up on this drive.
                Text(text = ownerLabel, color = MeshaColors.Muted, fontSize = 12.5.sp, fontWeight = FontWeight.W600)
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
                text = summary.vaccineLabels.joinToString(" · "),
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.size(10.dp))
        }
        // Coverage ring + headline prefer the DISTINCT-ANIMAL grain (a goat due for several vaccines
        // the same day is one animal, completed only when all its drive obligations are), but fall back
        // to the dose counts when a legacy cache / mixed-version response lacks animal counts (CDR-R1),
        // labelled accordingly. Round (not truncate) to match web. due/overdue/deferred chips stay doses.
        val coverage = driveVisibleProgress(summary)
        val pct = drivePctFor(summary, coverage)
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            DriveProgressRing(pct = pct)
            Column {
                Row(verticalAlignment = Alignment.Bottom) {
                    Text(
                        text = coverage.completed.toString(),
                        color = MeshaColors.Ink,
                        fontSize = 17.sp,
                        fontWeight = FontWeight.W800,
                    )
                    Text(
                        text = " / ${coverage.total} ${stringResource(if (coverage.usesAnimals) R.string.calendar_drive_animals else R.string.calendar_drive_doses)}",
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
                // The counts above are THIS DAY's slice. A drive runs over several days, so the
                // whole-drive herd total is the only number that answers "how big is this drive"
                // -- the backend already sends it and the card simply dropped it on the floor.
                summary.driveTotal?.takeIf { it > 0 }?.let { total ->
                    Text(
                        text = stringResource(R.string.calendar_drive_total_animals, total),
                        color = MeshaColors.Faint,
                        fontSize = 11.sp,
                        fontWeight = FontWeight.W600,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
            }
        }
        val chips = driveStatusChips(summary)
        if (chips.isNotEmpty()) {
            Spacer(Modifier.size(12.dp))
            FlowRow(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                chips.forEach { chip ->
                    StatusChip(chip)
                }
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
    items(state.monthDays.chunked(7), key = { week -> week.firstOrNull { it.dateKey != null }?.dateKey ?: week.hashCode().toString() }) { week ->
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

private fun androidx.compose.foundation.lazy.LazyListScope.monthContent(
    state: CalendarUiState,
    monthItems: LazyPagingItems<CalendarItem>?,
    onEvent: (CalendarEvent) -> Unit,
) {
    val fallbackItems = state.monthFallbackItems
    item {
        Row(
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp, bottom = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.calendar_month_schedule),
                    color = MeshaColors.Ink,
                    fontSize = 14.sp,
                    fontWeight = FontWeight.W800,
                )
                Text(
                    text = "${state.monthLabel} · ${stringResource(R.string.calendar_page_size)}",
                    color = MeshaColors.Muted,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W600,
                    modifier = Modifier.padding(top = 3.dp),
                )
            }
        }
    }

    if (monthItems == null) {
        item {
            EmptyState(
                title = state.monthEmptyLabel.ifEmpty { stringResource(R.string.calendar_month_empty) },
                icon = MeshaIcons.Calendar,
            )
        }
        return
    }

    when {
        monthItems.itemCount == 0 && monthItems.loadState.refresh is LoadState.Loading -> item {
            MonthPagingMessage(
                label = stringResource(R.string.calendar_month_loading),
                loading = true,
            )
        }

        monthItems.itemCount == 0 && monthItems.loadState.refresh is LoadState.Error -> item {
            MonthPagingMessage(
                label = stringResource(R.string.calendar_month_load_error),
                actionLabel = stringResource(R.string.calendar_retry),
                onAction = monthItems::retry,
            )
        }

        monthItems.itemCount == 0 && fallbackItems.isNotEmpty() -> items(
            fallbackItems,
            key = { item -> item.id },
        ) { item ->
            EventCard(
                item = item,
                showScheduleContext = true,
                onClick = {
                    onEvent(
                        CalendarEvent.TapItem(
                            itemId = item.id,
                            target = item.target,
                            dateKey = item.dateKey,
                            parkId = item.parkId,
                        ),
                    )
                },
            )
        }

        monthItems.itemCount == 0 -> item {
            EmptyState(
                title = state.monthEmptyLabel.ifEmpty { stringResource(R.string.calendar_month_empty) },
                icon = MeshaIcons.Calendar,
            )
        }

        else -> items(
            count = monthItems.itemCount,
            key = monthItems.itemKey { item -> item.id },
            contentType = { "calendar-schedule-card" },
        ) { index ->
            monthItems[index]?.let { item ->
                EventCard(
                    item = item,
                    showScheduleContext = true,
                    onClick = {
                        onEvent(
                            CalendarEvent.TapItem(
                                itemId = item.id,
                                target = item.target,
                                dateKey = item.dateKey,
                                parkId = item.parkId,
                            ),
                        )
                    },
                )
            }
        }
    }

    when (monthItems.loadState.append) {
        is LoadState.Loading -> item {
            MonthPagingMessage(label = "", loading = true)
        }
        is LoadState.Error -> item {
            MonthPagingMessage(
                label = stringResource(R.string.calendar_month_load_error),
                actionLabel = stringResource(R.string.calendar_retry),
                onAction = monthItems::retry,
            )
        }
        else -> Unit
    }
}

@Composable
private fun MonthPagingMessage(
    label: String,
    loading: Boolean = false,
    actionLabel: String? = null,
    onAction: () -> Unit = {},
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(vertical = 16.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(18.dp), strokeWidth = 2.dp)
            if (label.isNotBlank()) {
                Spacer(Modifier.size(8.dp))
            }
        }
        if (label.isNotBlank()) {
            Text(
                text = label,
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
            )
        }
        if (actionLabel != null) {
            Spacer(Modifier.size(8.dp))
            TextButton(onClick = onAction) { Text(actionLabel) }
        }
    }
}

@Composable
@OptIn(ExperimentalMaterial3Api::class)
private fun MonthFilterSheet(
    state: CalendarUiState,
    onDismiss: () -> Unit,
    onApply: (CalendarMonthFilters) -> Unit,
    onClear: () -> Unit,
) {
    var draft by remember(state.monthFilters) { mutableStateOf(state.monthFilters) }
    val options = state.monthFilterOptions
    val yearOptions = options.years.ifEmpty {
        listOf(CalendarFilterOption(draft.year.toString(), draft.year.toString()))
    }
    val monthOptions = options.months.ifEmpty {
        listOf(
            CalendarFilterOption(
                draft.month.toString().padStart(2, '0'),
                state.monthLabel.substringBefore(' '),
            ),
        )
    }
    val shedOptions = options.sheds.filter { option ->
        draft.parkId == null || option.parentValue == draft.parkId
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = MeshaColors.Surf,
    ) {
        LazyColumn(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(max = 640.dp)
                .padding(horizontal = Gutter),
        ) {
            item {
                Text(
                    text = stringResource(R.string.calendar_filter_title),
                    color = MeshaColors.Ink,
                    fontSize = 20.sp,
                    fontWeight = FontWeight.W800,
                    modifier = Modifier.padding(bottom = 12.dp),
                )
            }
            item {
                FilterChoiceSection(
                    label = stringResource(R.string.calendar_filter_year),
                    options = yearOptions,
                    selectedValue = draft.year.toString(),
                    includeAll = false,
                    onSelect = { value -> value?.toIntOrNull()?.let { draft = draft.copy(year = it) } },
                )
            }
            item {
                FilterChoiceSection(
                    label = stringResource(R.string.calendar_filter_month),
                    options = monthOptions,
                    selectedValue = draft.month.toString().padStart(2, '0'),
                    includeAll = false,
                    onSelect = { value -> value?.toIntOrNull()?.let { draft = draft.copy(month = it) } },
                )
            }
            item {
                FilterChoiceSection(
                    label = stringResource(R.string.calendar_filter_park),
                    options = options.parks,
                    selectedValue = draft.parkId,
                    onSelect = { parkId ->
                        val currentShedStillValid = parkId != null && options.sheds.any {
                            it.value == draft.shedId && it.parentValue == parkId
                        }
                        draft = draft.copy(
                            parkId = parkId,
                            shedId = draft.shedId.takeIf { currentShedStillValid },
                        )
                    },
                )
            }
            item {
                FilterChoiceSection(
                    label = stringResource(R.string.calendar_filter_shed),
                    options = shedOptions,
                    selectedValue = draft.shedId,
                    onSelect = { draft = draft.copy(shedId = it) },
                )
            }
            item {
                FilterChoiceSection(
                    label = stringResource(R.string.calendar_filter_vaccine),
                    options = options.vaccines,
                    selectedValue = draft.vaccine,
                    onSelect = { draft = draft.copy(vaccine = it) },
                )
            }
            item {
                FilterChoiceSection(
                    label = stringResource(R.string.calendar_filter_status),
                    options = options.statuses,
                    selectedValue = draft.status,
                    onSelect = { draft = draft.copy(status = it) },
                )
            }
            item {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(top = 8.dp, bottom = 24.dp),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    TextButton(onClick = onClear) {
                        Text(stringResource(R.string.calendar_filter_clear))
                    }
                    Button(
                        onClick = { onApply(draft) },
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(stringResource(R.string.calendar_filter_apply))
                    }
                }
            }
        }
    }
}

@Composable
private fun FilterChoiceSection(
    label: String,
    options: List<CalendarFilterOption>,
    selectedValue: String?,
    includeAll: Boolean = true,
    onSelect: (String?) -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(bottom = 14.dp)) {
        Text(
            text = label.uppercase(),
            color = MeshaColors.Faint,
            fontSize = 10.5.sp,
            fontWeight = FontWeight.W800,
            letterSpacing = 0.5.sp,
            modifier = Modifier.padding(bottom = 6.dp),
        )
        LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (includeAll) {
                item(key = "all") {
                    CalendarFilterChip(
                        selected = selectedValue == null,
                        onClick = { onSelect(null) },
                        label = stringResource(R.string.calendar_filter_all),
                    )
                }
            }
            items(options, key = { it.value }) { option ->
                CalendarFilterChip(
                    selected = selectedValue == option.value,
                    onClick = { onSelect(option.value) },
                    label = option.label,
                )
            }
        }
    }
}

@Composable
private fun CalendarFilterChip(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    val shape = RoundedCornerShape(18.dp)
    val background = if (selected) MeshaColors.Brand else MeshaColors.Surf
    val border = if (selected) MeshaColors.Brand else MeshaColors.Hair
    val foreground = if (selected) MeshaColors.PageBg else MeshaColors.Ink

    Box(
        modifier = Modifier
            .height(48.dp)
            .clip(shape)
            .background(background)
            .border(1.dp, border, shape)
            .clickable(onClick = onClick)
            .padding(horizontal = 13.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = foreground,
            fontSize = 13.sp,
            fontWeight = FontWeight.W800,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
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
    val listState = rememberLazyListState()
    LaunchedEffect(listState, state.hasMore, state.isLoadingMore, state.items.size) {
        if (!state.hasMore || state.isLoadingMore || state.items.isEmpty()) return@LaunchedEffect
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { lastVisibleIndex ->
                if (lastVisibleIndex >= listState.layoutInfo.totalItemsCount - 4 && state.hasMore && !state.isLoadingMore) {
                    onLoadMore()
                }
            }
    }
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
        LazyColumn(state = listState, modifier = Modifier.fillMaxSize()) {
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
                if (state.isLoadingMore) {
                    item {
                        InlineLoadingFooter()
                    }
                }
            }
            item { Spacer(Modifier.size(24.dp)) }
        }
    }
}

/* --------------------------------------------------------------------------- */
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
private fun InlineLoadingFooter() {
    Box(
        Modifier
            .fillMaxWidth()
            .padding(top = 8.dp, bottom = 4.dp),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(18.dp),
            color = MeshaColors.Muted,
            strokeWidth = 2.dp,
        )
    }
}

/**
 * Status chip for the redesigned drive card: a colored dot (9×9px, rounded) + label.
 * Color and label are derived from the chip key (completed/submitted/due/overdue/deferred).
 */
@Composable
private fun StatusChip(chip: sg.mesha.goatos.feature.calendar.StatusChip) {
    val (dotColor, labelStringRes) = when (chip.key) {
        "completed" -> MeshaColors.Brand to R.string.calendar_drive_done
        "submitted" -> MeshaColors.Warn to R.string.calendar_drive_submitted
        "due" -> MeshaColors.Warn to R.string.calendar_drive_due
        "overdue" -> MeshaColors.Danger to R.string.calendar_drive_overdue
        "deferred" -> MeshaColors.Purple to R.string.calendar_drive_deferred
        else -> MeshaColors.Muted to R.string.calendar_drive_due
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(MeshaColors.Surf3)
            .padding(horizontal = 8.dp, vertical = 5.dp),
    ) {
        Box(
            Modifier
                .size(9.dp)
                .clip(RoundedCornerShape(3.dp))
                .background(dotColor),
        )
        Text(
            text = stringResource(labelStringRes, chip.count),
            color = dotColor,
            fontSize = 12.sp,
            fontWeight = FontWeight.W600,
            maxLines = 1,
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

private fun humanStatusLabel(label: String): String {
    val trimmed = label.trim()
    if (trimmed.isEmpty()) return trimmed
    val normalized = trimmed.replace('_', ' ').replace('-', ' ')
    return normalized.replaceFirstChar { first ->
        if (first.isLowerCase()) first.titlecase() else first.toString()
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
