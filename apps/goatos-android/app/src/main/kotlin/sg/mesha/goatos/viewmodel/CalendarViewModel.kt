package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
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
 * Calendar screen state holder. Shows a loading placeholder first, then loads the real PC
 * vaccination calendar via [CalendarRepository.events] and maps it in [toCalendarUiState].
 * An empty real response shows an honest empty state and a failure shows an honest error
 * state — a fabricated sample calendar is NEVER shown as if it were live. Segment switch is
 * purely local; day/item taps are navigation, routed by the nav host.
 */
@HiltViewModel
class CalendarViewModel @Inject constructor(
    private val repo: CalendarRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(calendarPlaceholder("Loading…"))
    val state: StateFlow<CalendarUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.events() }
            .onSuccess { dto -> _state.value = dto.toCalendarUiState() ?: calendarPlaceholder("No drives scheduled") }
            .onFailure { _state.value = calendarPlaceholder("Couldn't load the calendar right now.") }
    }

    fun onEvent(event: CalendarEvent) {
        when (event) {
            is CalendarEvent.SelectSegment ->
                _state.update { it.copy(selectedSegmentId = event.segmentId) }
            CalendarEvent.Refresh -> load()
            // Day/item taps are navigation — handled by the nav host.
            is CalendarEvent.TapDay,
            is CalendarEvent.TapItem -> Unit
        }
    }

    private fun CalendarEventListResponseDto.toCalendarUiState(): CalendarUiState? {
        if (items.isEmpty() && presentation.viewTabs.isEmpty()) return null
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
        val weekDays = buildWeekDays(items)
        val todayLabel = LocalDate.now(KOLKATA).let {
            "Today · ${it.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} ${it.dayOfMonth} ${it.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}"
        }
        val weekItems = items.map { it.toCalendarItem() }
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

    private fun CalendarEventDto.toCalendarItem(): CalendarItem {
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

private fun parseLocalDate(due: String): LocalDate? =
    runCatching { OffsetDateTime.parse(due).atZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()

/** The row-navigation route the backend attached to a calendar event ("drive" preferred). */
private fun Map<String, JsonElement>.route(): String? =
    this["drive"]?.jsonPrimitive?.contentOrNull ?: this["vaccination"]?.jsonPrimitive?.contentOrNull

private fun fromSeverity(severity: String): CalendarTone = when (severity.lowercase()) {
    "critical", "high" -> CalendarTone.Danger
    "warn", "warning" -> CalendarTone.Warn
    "ok" -> CalendarTone.Ok
    else -> CalendarTone.Muted
}
