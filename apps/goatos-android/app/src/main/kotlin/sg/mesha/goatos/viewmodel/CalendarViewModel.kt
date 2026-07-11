package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.feature.calendar.CalendarEvent
import sg.mesha.goatos.feature.calendar.CalendarHistoryRow
import sg.mesha.goatos.feature.calendar.CalendarItem
import sg.mesha.goatos.feature.calendar.CalendarMonthDay
import sg.mesha.goatos.feature.calendar.CalendarSegment
import sg.mesha.goatos.feature.calendar.CalendarSegmentKind
import sg.mesha.goatos.feature.calendar.CalendarTone
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.feature.calendar.CalendarWeekDay
import sg.mesha.goatos.ui.calendarPlaceholder
import sg.mesha.goatos.ui.sampleCalendarState
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.YearMonth
import java.time.ZoneId
import java.time.format.TextStyle
import java.util.Locale
import javax.inject.Inject

/**
 * Calendar screen state holder — the offline-first REFERENCE for every other screen-read
 * ViewModel (docs/decisions/android-offline-first.md). Room is the UI's single source of
 * truth: [state] is fed by [CalendarRepository.observeEvents], a cache-first [kotlinx.coroutines.flow.Flow]
 * that emits instantly from Room (cached data survives process restarts and screen
 * re-entry) and re-emits the moment a background [CalendarRepository.refreshEvents] upserts
 * new data. [refresh] never writes into [state] directly — it only drives the network call
 * and the transient [CalendarUiState.isRefreshing]/[CalendarUiState.isOffline] flags; the
 * DTO -> UiState mapping in [toCalendarUiState] is unchanged from the network-only version.
 * An empty real response shows an honest empty state; a refresh failure with NO cache ever
 * observed shows an honest error state; a refresh failure WITH cached data keeps rendering
 * that cache and only flips [CalendarUiState.isOffline] — never a blank/loading wall. Segment
 * switch is purely local, and a WEEK-strip day tap is purely local (mock `sel=day;
 * renderWeek()`) — it re-scopes the week agenda list to the tapped day. A MONTH-grid day tap
 * and item taps are navigation, routed by the nav host: the month day opens its own L1 day
 * screen ([CalendarEvent.OpenDay]); items drill to their backend-supplied target.
 */
@HiltViewModel
class CalendarViewModel @Inject constructor(
    private val repo: CalendarRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(calendarPlaceholder("Loading…"))
    val state: StateFlow<CalendarUiState> = _state.asStateFlow()

    /**
     * Raw events from the last cache emission, retained so a day tap can filter to
     * that day's real drives (mock's in-memory `DR[day]` map) without a second network
     * round trip. Never used to fabricate a day's content — a day with no matching event
     * simply filters to an empty list, which renders the honest empty state.
     */
    private var loadedEvents: List<CalendarEventDto> = emptyList()

    init {
        // Cache-first: renders whatever Room already has (possibly nothing, on a cold
        // install) immediately, then re-renders after every successful refresh below.
        viewModelScope.launch {
            repo.observeEvents().collectLatest { resource -> applyResource(resource) }
        }
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observeEvents]
     *  collector above re-emits and updates [state]); on failure it only flips
     *  [CalendarUiState.isOffline] — cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _state.update { it.copy(isRefreshing = true) }
        val result = repo.refreshEvents()
        // SWR: a refresh NEVER replaces what's on screen. On success the observeEvents
        // collector re-emits the fresh cache; on failure the current content stays put and
        // only the offline flag flips — the segments, the week strip, and any cached list keep
        // rendering (never a blank wall). A cold start with no cache yet keeps its calendar
        // skeleton + honest empty state, now flagged offline via [CalendarUiState.isOffline].
        _state.update { it.copy(isRefreshing = false, isOffline = result.isFailure) }
    }

    private fun applyResource(resource: Resource<CalendarEventListResponseDto>) {
        val dto = resource.data
        loadedEvents = dto?.items.orEmpty()
        // dto non-null (even empty) -> a full calendar shell with an honest empty content area;
        // dto null only on a cold cache -> the loading skeleton. Never a blank collapse.
        val base = dto?.toCalendarUiState() ?: calendarPlaceholder("Loading…")
        _state.update { current ->
            base.copy(
                isRefreshing = current.isRefreshing,
                lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
                isOffline = current.isOffline,
            )
        }
    }

    fun onEvent(event: CalendarEvent) {
        when (event) {
            is CalendarEvent.SelectSegment ->
                _state.update { it.copy(selectedSegmentId = event.segmentId) }
            CalendarEvent.Refresh -> refresh()
            is CalendarEvent.TapDay -> selectDay(event.dateKey)
            // Month day + item taps are navigation — handled by the nav host.
            is CalendarEvent.OpenDay -> Unit
            is CalendarEvent.TapItem -> Unit
        }
    }

    /**
     * A WEEK-strip day tap is in-screen selection only (mock: `sel=day; renderWeek()`) — it
     * re-scopes [CalendarUiState.weekItems] and the selected-date label to the tapped day,
     * reading only [loadedEvents] (an empty day renders an honest empty state). A MONTH-grid
     * day tap does NOT come here — it opens the day's own L1 screen ([CalendarEvent.OpenDay],
     * routed by the nav host to CalendarDayScreen).
     */
    private fun selectDay(dateKey: String) {
        val date = runCatching { LocalDate.parse(dateKey) }.getOrNull() ?: return
        _state.update { s ->
            s.copy(
                weekDays = s.weekDays.map { day -> day.copy(isSelected = day.dateKey == dateKey) },
                selectedDateLabel = dateLabel(date),
                weekItems = itemsForDate(date),
            )
        }
    }

    /** The real, currently-loaded events due on [date] — never fabricated. */
    private fun itemsForDate(date: LocalDate): List<CalendarItem> =
        loadedEvents.filter { parseLocalDate(it.dueAt) == date }.map { it.toCalendarItem() }

    private fun CalendarEventListResponseDto.toCalendarUiState(): CalendarUiState {
        // Always returns a valid calendar shell — even for an empty response the segments and
        // the real current-week strip render, and the content area shows an honest empty state
        // (never a collapse to a bare header). Content lists are still built ONLY from real
        // events; nothing is back-filled from the sample base.
        val base = sampleCalendarState()
        val segments = presentation.viewTabs.map { tab ->
            CalendarSegment(
                id = tab.key,
                label = tab.label,
                kind = when {
                    tab.label.contains("month", ignoreCase = true) -> CalendarSegmentKind.Month
                    tab.label.contains("history", ignoreCase = true) -> CalendarSegmentKind.History
                    else -> CalendarSegmentKind.Week
                },
            )
        }
        val today = LocalDate.now(KOLKATA)
        val weekDays = buildWeekDays(items)
        val todayLabel = dateLabel(today)
        // Initial week list is scoped to TODAY's real due events (mirrors the mock's default
        // `sel=today`) — never the whole week dumped at once; a day tap re-scopes this via
        // [selectDay].
        val weekItems = itemsForDate(today)
        val monthDays = buildMonthDays(items)
        val monthLabel = YearMonth.now(KOLKATA).let {
            "${it.month.getDisplayName(TextStyle.FULL, Locale.ENGLISH)} ${it.year}"
        }
        val historyRows = items.take(3).map { ev ->
            CalendarHistoryRow(
                id = ev.eventId,
                title = ev.title,
                subtitle = ev.subtitle,
                badgeLabel = ev.status,
                badgeTone = fromSeverity(ev.severity),
                target = ev.links.route(),
            )
        }
        val monthWeekdayLabels = listOf("S", "M", "T", "W", "T", "F", "S")

        return base.copy(
            // Mock eyebrow is a short module label, NOT the verbose page description —
            // the backend pageSubtitle is a paragraph and must not be dumped as an eyebrow.
            eyebrow = "Vaccination",
            title = presentation.pageTitle.ifBlank { base.title },
            // Segments are app-standard chrome (Week/Month/History); fall back to the
            // default tabs only. Content lists are NEVER back-filled from the sample —
            // an empty real response must render as genuinely empty, not fabricated.
            segments = segments.ifEmpty { base.segments },
            selectedSegmentId = presentation.viewTabs.firstOrNull { it.active }?.key
                ?: segments.firstOrNull()?.id
                ?: base.selectedSegmentId,
            selectedDateLabel = todayLabel,
            windowLabel = null,
            weekDays = weekDays,
            weekItems = weekItems,
            weekEmptyLabel = presentation.emptyState.okMessage.ifBlank { "No drives scheduled" },
            monthLabel = monthLabel,
            monthWeekdayLabels = monthWeekdayLabels,
            monthDays = monthDays,
            monthHint = "Tap a day for its drives · dots = drive days",
            historyRows = historyRows,
            historyEmptyLabel = presentation.emptyState.okMessage.ifBlank { "No past drives" },
        )
    }

    /**
     * "Today · Tue 7 Jul" for the actual day, else just "Tue 7 Jul" — mirrors the mock's
     * `(sel===7?'Today · ':'')+WD[sel]+' '+sel+' Jul'` day label used above the week list.
     */
    private fun dateLabel(date: LocalDate, today: LocalDate = LocalDate.now(KOLKATA)): String {
        val base = "${date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} " +
            "${date.dayOfMonth} ${date.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}"
        return if (date == today) "Today · $base" else base
    }
}

private val KOLKATA: ZoneId = ZoneId.of("Asia/Kolkata")

/**
 * The Mon–Sun week containing today (India business calendar), with per-day due-work
 * dots derived from the events' [CalendarEventDto.dueAt]. Mirrors the mock's `#weekStrip`:
 * day letter + date + a dot on days that carry work; today is the highlighted cell.
 */
private fun buildWeekDays(items: List<CalendarEventDto>): List<CalendarWeekDay> {
    val today = LocalDate.now(KOLKATA)
    val monday = today.minusDays((today.dayOfWeek.value - 1).toLong())
    val counts = items.mapNotNull { parseLocalDate(it.dueAt) }.groupingBy { it }.eachCount()
    return (0..6).map { offset ->
        val date = monday.plusDays(offset.toLong())
        val n = counts[date] ?: 0
        CalendarWeekDay(
            dateKey = date.toString(),
            dayName = date.dayOfWeek.getDisplayName(TextStyle.NARROW, Locale.ENGLISH),
            dayNumber = date.dayOfMonth.toString(),
            dueCountLabel = if (n > 0) "$n due" else "",
            hasWork = n > 0,
            isToday = date == today,
            isSelected = date == today,
        )
    }
}

/**
 * Month grid for the current month (Asia/Kolkata), with leading blank cells so the
 * 1st lands under the correct weekday (Sunday-first calendar). Each cell shows the day
 * number and a dot on days that carry work from the events' [CalendarEventDto.dueAt].
 * Marks today and respects the dot tone based on event severity.
 */
private fun buildMonthDays(items: List<CalendarEventDto>): List<CalendarMonthDay> {
    val today = LocalDate.now(KOLKATA)
    val yearMonth = YearMonth.now(KOLKATA)
    val firstDay = yearMonth.atDay(1)
    val lastDay = yearMonth.atEndOfMonth()

    // Map dates to their highest-severity tone (danger > warn > ok > neutral)
    val dayTones = mutableMapOf<LocalDate, CalendarTone>()
    items.mapNotNull { parseLocalDate(it.dueAt) }.forEach { date ->
        if (date.month == today.month && date.year == today.year) {
            val newTone = fromSeverity(
                items.find { parseLocalDate(it.dueAt) == date }?.severity ?: "neutral"
            )
            dayTones[date] = when {
                dayTones[date] == CalendarTone.Danger -> CalendarTone.Danger
                newTone == CalendarTone.Danger -> CalendarTone.Danger
                dayTones[date] == CalendarTone.Warn -> CalendarTone.Warn
                newTone == CalendarTone.Warn -> CalendarTone.Warn
                else -> newTone
            }
        }
    }

    // Leading blank cells (Sunday = 0, so firstDay.dayOfWeek.value - 1)
    val leadingBlanks = (firstDay.dayOfWeek.value % 7).let { if (it == 0) 0 else it }
    val blanks = (0 until leadingBlanks).map { CalendarMonthDay(dateKey = null, dayNumber = null) }

    // Days of the month
    val days = (1..lastDay.dayOfMonth).map { d ->
        val date = yearMonth.atDay(d)
        CalendarMonthDay(
            dateKey = date.toString(),
            dayNumber = d.toString(),
            hasWork = date in dayTones,
            dotTone = dayTones[date] ?: CalendarTone.Neutral,
            isSelected = date == today,
        )
    }

    return blanks + days
}

internal fun parseLocalDate(due: String): LocalDate? =
    runCatching { OffsetDateTime.parse(due).atZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()

/** The row-navigation route the backend attached to a calendar event ("drive" preferred). */
internal fun Map<String, JsonElement>.route(): String? =
    this["drive"]?.jsonPrimitive?.contentOrNull ?: this["vaccination"]?.jsonPrimitive?.contentOrNull

internal fun fromSeverity(severity: String): CalendarTone = when (severity.lowercase()) {
    "critical", "high" -> CalendarTone.Danger
    "warn", "warning" -> CalendarTone.Warn
    "ok" -> CalendarTone.Ok
    else -> CalendarTone.Muted
}

/**
 * Backend calendar event -> a drillable [CalendarItem]. Shared by [CalendarViewModel] and
 * [CalendarDayViewModel] so the calendar and its L1 day screen map an event identically.
 */
internal fun CalendarEventDto.toCalendarItem(): CalendarItem {
    val target = links.route()
    return CalendarItem(
        id = eventId,
        title = title,
        subtitle = subtitle,
        statusLabel = status,
        statusTone = fromSeverity(severity),
        categoryLabel = vaccineName,
        ctaLabel = if (target != null) "Open" else null,
        target = target,
    )
}

/** "8 July" — the L1 day-detail title (mirrors the mock's `d+' '+M.nm`). */
internal fun calendarDayTitle(date: LocalDate): String =
    "${date.dayOfMonth} ${date.month.getDisplayName(TextStyle.FULL, Locale.ENGLISH)}"
