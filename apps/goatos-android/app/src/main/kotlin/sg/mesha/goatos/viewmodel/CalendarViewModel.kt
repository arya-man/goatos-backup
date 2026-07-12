package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
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
import sg.mesha.goatos.core.network.dto.CalendarDateMarkerDto
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.CalendarPresentationDto
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
 * Calendar screen state holder. The mobile calendar now mirrors the backend contract:
 * overview strips/grids render from tiny `date_markers`, while day/history lists read
 * bounded 20-row pages with explicit `next_cursor` handling instead of overfetching 200
 * rows and pretending the first page is complete.
 */
@HiltViewModel
class CalendarViewModel @Inject constructor(
    private val repo: CalendarRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(calendarPlaceholder("Loading…"))
    val state: StateFlow<CalendarUiState> = _state.asStateFlow()

    private val today = LocalDate.now(KOLKATA)
    private val weekRange = calendarWeekRange(today)
    private val monthRange = calendarMonthRange(today)
    private val historyRange = calendarHistoryRange(today)

    private var selectedDay = today
    private var selectedSegmentId: String? = null
    private var weekOverviewResource = Resource<CalendarEventListResponseDto>(data = null)
    private var monthOverviewResource = Resource<CalendarEventListResponseDto>(data = null)
    private var selectedDayResource = Resource<CalendarEventListResponseDto>(data = null)
    private var historyResource = Resource<CalendarEventListResponseDto>(data = null)

    private var selectedDayExtraItems: List<CalendarEventDto> = emptyList()
    private var historyExtraItems: List<CalendarEventDto> = emptyList()
    private var selectedDayNextCursor: String? = null
    private var historyNextCursor: String? = null
    private var selectedDayLoadingMore = false
    private var historyLoadingMore = false
    private var refreshInFlight = false
    private var offline = false

    private var selectedDayJob: Job? = null

    init {
        observeWeekOverview()
        observeMonthOverview()
        observeHistory()
        observeSelectedDay()
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        refreshInFlight = true
        selectedDayExtraItems = emptyList()
        historyExtraItems = emptyList()
        selectedDayNextCursor = null
        historyNextCursor = null
        selectedDayLoadingMore = false
        historyLoadingMore = false
        rebuildState()

        val results = listOf(
            async {
                repo.refreshEvents(
                    dateFrom = weekRange.dateFrom,
                    dateTo = weekRange.dateTo,
                    includeDateMarkers = true,
                    limit = 1,
                )
            },
            async {
                repo.refreshEvents(
                    dateFrom = monthRange.dateFrom,
                    dateTo = monthRange.dateTo,
                    includeDateMarkers = true,
                    limit = 1,
                )
            },
            async {
                repo.refreshEvents(
                    dateFrom = selectedDay.toString(),
                    dateTo = selectedDay.toString(),
                    limit = CALENDAR_PAGE_SIZE,
                )
            },
            async {
                repo.refreshEvents(
                    status = COMPLETED_STATUS,
                    dateFrom = historyRange.dateFrom,
                    dateTo = historyRange.dateTo,
                    limit = CALENDAR_PAGE_SIZE,
                )
            },
        ).awaitAll()

        refreshInFlight = false
        offline = results.any { it.isFailure }
        rebuildState()
    }

    fun onEvent(event: CalendarEvent) {
        when (event) {
            is CalendarEvent.SelectSegment -> {
                selectedSegmentId = event.segmentId
                rebuildState()
            }

            CalendarEvent.Refresh -> refresh()
            is CalendarEvent.TapDay -> selectDay(event.dateKey)
            CalendarEvent.LoadMoreWeek -> loadMoreSelectedDay()
            CalendarEvent.LoadMoreHistory -> loadMoreHistory()
            is CalendarEvent.OpenDay -> Unit
            is CalendarEvent.TapItem -> Unit
        }
    }

    private fun observeWeekOverview() {
        viewModelScope.launch {
            repo.observeEvents(
                dateFrom = weekRange.dateFrom,
                dateTo = weekRange.dateTo,
                includeDateMarkers = true,
                limit = 1,
            ).collectLatest { resource ->
                weekOverviewResource = resource
                rebuildState()
            }
        }
    }

    private fun observeMonthOverview() {
        viewModelScope.launch {
            repo.observeEvents(
                dateFrom = monthRange.dateFrom,
                dateTo = monthRange.dateTo,
                includeDateMarkers = true,
                limit = 1,
            ).collectLatest { resource ->
                monthOverviewResource = resource
                rebuildState()
            }
        }
    }

    private fun observeHistory() {
        viewModelScope.launch {
            repo.observeEvents(
                status = COMPLETED_STATUS,
                dateFrom = historyRange.dateFrom,
                dateTo = historyRange.dateTo,
                limit = CALENDAR_PAGE_SIZE,
            ).collectLatest { resource ->
                historyResource = resource
                if (historyExtraItems.isEmpty()) {
                    historyNextCursor = resource.data?.nextCursor
                }
                rebuildState()
            }
        }
    }

    private fun observeSelectedDay() {
        selectedDayJob?.cancel()
        selectedDayJob = viewModelScope.launch {
            repo.observeEvents(
                dateFrom = selectedDay.toString(),
                dateTo = selectedDay.toString(),
                limit = CALENDAR_PAGE_SIZE,
            ).collectLatest { resource ->
                selectedDayResource = resource
                if (selectedDayExtraItems.isEmpty()) {
                    selectedDayNextCursor = resource.data?.nextCursor
                }
                rebuildState()
            }
        }
    }

    private fun selectDay(dateKey: String) {
        val date = runCatching { LocalDate.parse(dateKey) }.getOrNull() ?: return
        if (date == selectedDay) return
        selectedDay = date
        selectedDayExtraItems = emptyList()
        selectedDayNextCursor = null
        selectedDayLoadingMore = false
        observeSelectedDay()
        rebuildState()
        viewModelScope.launch {
            val result = repo.refreshEvents(
                dateFrom = date.toString(),
                dateTo = date.toString(),
                limit = CALENDAR_PAGE_SIZE,
            )
            offline = result.isFailure
            rebuildState()
        }
    }

    private fun loadMoreSelectedDay() = viewModelScope.launch {
        val cursor = selectedDayNextCursor ?: return@launch
        selectedDayLoadingMore = true
        rebuildState()
        runCatching {
            repo.events(
                dateFrom = selectedDay.toString(),
                dateTo = selectedDay.toString(),
                cursor = cursor,
                limit = CALENDAR_PAGE_SIZE,
            )
        }.onSuccess { page ->
            selectedDayExtraItems = appendUniqueByEventId(selectedDayExtraItems, page.items)
            selectedDayNextCursor = page.nextCursor
            offline = false
        }.onFailure {
            offline = true
        }
        selectedDayLoadingMore = false
        rebuildState()
    }

    private fun loadMoreHistory() = viewModelScope.launch {
        val cursor = historyNextCursor ?: return@launch
        historyLoadingMore = true
        rebuildState()
        runCatching {
            repo.events(
                status = COMPLETED_STATUS,
                dateFrom = historyRange.dateFrom,
                dateTo = historyRange.dateTo,
                cursor = cursor,
                limit = CALENDAR_PAGE_SIZE,
            )
        }.onSuccess { page ->
            historyExtraItems = appendUniqueByEventId(historyExtraItems, page.items)
            historyNextCursor = page.nextCursor
            offline = false
        }.onFailure {
            offline = true
        }
        historyLoadingMore = false
        rebuildState()
    }

    private fun rebuildState() {
        val base = sampleCalendarState()
        val presentation = activePresentation()
        val segments = buildSegments(presentation, base)
        val selectedSegment = resolveSelectedSegment(segments, presentation, base)
        selectedSegmentId = selectedSegment
        val dayItems = appendUniqueByEventId(selectedDayResource.data?.items.orEmpty(), selectedDayExtraItems)
            .sortedBy { it.dueAt }
        val historyItems = appendUniqueByEventId(historyResource.data?.items.orEmpty(), historyExtraItems)
            .filter { it.status == COMPLETED_STATUS }
            .sortedByDescending { it.dueAt }
        val state = base.copy(
            eyebrow = "Vaccination",
            title = presentation?.pageTitle?.ifBlank { base.title } ?: base.title,
            selectedDateLabel = dateLabel(selectedDay),
            windowLabel = when (selectedSegment) {
                "month" -> monthLabel(today)
                "history" -> "${historyRange.dateFrom} → ${historyRange.dateTo}"
                else -> null
            },
            isRefreshing = refreshInFlight,
            lastSyncedAt = listOfNotNull(
                weekOverviewResource.lastSyncedAt,
                monthOverviewResource.lastSyncedAt,
                selectedDayResource.lastSyncedAt,
                historyResource.lastSyncedAt,
            ).maxOrNull(),
            isOffline = offline,
            segments = segments,
            selectedSegmentId = selectedSegment,
            weekDays = buildWeekDays(weekOverviewResource.data?.dateMarkers.orEmpty(), selectedDay, today),
            weekItems = dayItems.map { it.toCalendarItem() },
            weekEmptyLabel = presentation?.emptyState?.okMessage?.ifBlank { base.weekEmptyLabel } ?: base.weekEmptyLabel,
            weekHasMore = selectedDayNextCursor != null,
            weekLoadingMore = selectedDayLoadingMore,
            monthLabel = monthLabel(today),
            monthWeekdayLabels = listOf("S", "M", "T", "W", "T", "F", "S"),
            monthDays = buildMonthDays(monthOverviewResource.data?.dateMarkers.orEmpty(), today),
            monthHint = base.monthHint,
            historyLabel = "",
            historyCount = historyItems.size,
            historyRows = historyItems.map { event ->
                CalendarHistoryRow(
                    id = event.eventId,
                    title = event.title,
                    subtitle = event.subtitle,
                    badgeLabel = event.status,
                    badgeTone = CalendarTone.Ok,
                    target = event.routeTarget(),
                )
            },
            historyEmptyLabel = presentation?.emptyState?.okMessage?.ifBlank { base.historyEmptyLabel } ?: base.historyEmptyLabel,
            historyHasMore = historyNextCursor != null,
            historyLoadingMore = historyLoadingMore,
        )
        _state.value = state
    }

    private fun buildSegments(
        presentation: CalendarPresentationDto?,
        base: CalendarUiState,
    ): List<CalendarSegment> =
        presentation?.viewTabs?.map { tab ->
            CalendarSegment(
                id = tab.key,
                label = tab.label,
                kind = when (tab.key) {
                    "month" -> CalendarSegmentKind.Month
                    "history" -> CalendarSegmentKind.History
                    else -> CalendarSegmentKind.Week
                },
            )
        }?.ifEmpty { base.segments } ?: base.segments

    private fun resolveSelectedSegment(
        segments: List<CalendarSegment>,
        presentation: CalendarPresentationDto?,
        base: CalendarUiState,
    ): String =
        selectedSegmentId?.takeIf { id -> segments.any { it.id == id } }
            ?: presentation?.viewTabs?.firstOrNull { it.active }?.key?.takeIf { key -> segments.any { it.id == key } }
            ?: segments.firstOrNull()?.id
            ?: base.selectedSegmentId

    private fun activePresentation(): CalendarPresentationDto? =
        listOfNotNull(
            weekOverviewResource.data?.presentation?.takeIf(::hasPresentation),
            monthOverviewResource.data?.presentation?.takeIf(::hasPresentation),
            selectedDayResource.data?.presentation?.takeIf(::hasPresentation),
            historyResource.data?.presentation?.takeIf(::hasPresentation),
        ).firstOrNull()

    private fun hasPresentation(presentation: CalendarPresentationDto): Boolean =
        presentation.pageTitle.isNotBlank() ||
            presentation.pageSubtitle.isNotBlank() ||
            presentation.viewTabs.isNotEmpty() ||
            presentation.ownerTabs.isNotEmpty() ||
            presentation.workstreamTabs.isNotEmpty()

    private fun dateLabel(date: LocalDate, today: LocalDate = LocalDate.now(KOLKATA)): String {
        val base = "${date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} " +
            "${date.dayOfMonth} ${date.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}"
        return if (date == today) "Today · $base" else base
    }
}

private val KOLKATA: ZoneId = ZoneId.of("Asia/Kolkata")
internal const val COMPLETED_STATUS = "completed"
internal const val CALENDAR_PAGE_SIZE = 20
private const val HISTORY_WINDOW_DAYS = 45L

internal data class CalendarDateRange(val dateFrom: String, val dateTo: String)

internal fun calendarWeekRange(today: LocalDate): CalendarDateRange {
    val monday = today.minusDays((today.dayOfWeek.value - DayOfWeek.MONDAY.value).toLong())
    return CalendarDateRange(monday.toString(), monday.plusDays(6).toString())
}

internal fun calendarMonthRange(today: LocalDate): CalendarDateRange {
    val month = YearMonth.from(today)
    return CalendarDateRange(month.atDay(1).toString(), month.atEndOfMonth().toString())
}

internal fun calendarHistoryRange(today: LocalDate): CalendarDateRange =
    CalendarDateRange(today.minusDays(HISTORY_WINDOW_DAYS - 1).toString(), today.toString())

private fun monthLabel(today: LocalDate): String =
    YearMonth.from(today).let {
        "${it.month.getDisplayName(TextStyle.FULL, Locale.ENGLISH)} ${it.year}"
    }

/**
 * The Mon–Sun week strip now renders from authoritative backend date markers instead of
 * re-parsing the fetched event list to invent overview dots/counts client-side.
 */
internal fun buildWeekDays(
    markers: List<CalendarDateMarkerDto>,
    selectedDay: LocalDate,
    today: LocalDate,
): List<CalendarWeekDay> {
    val monday = today.minusDays((today.dayOfWeek.value - DayOfWeek.MONDAY.value).toLong())
    val byDate = markers.associateBy { it.date }
    return (0..6).map { offset ->
        val date = monday.plusDays(offset.toLong())
        val marker = byDate[date.toString()]
        val openCount = marker?.openCount ?: 0
        CalendarWeekDay(
            dateKey = date.toString(),
            dayName = date.dayOfWeek.getDisplayName(TextStyle.NARROW, Locale.ENGLISH),
            dayNumber = date.dayOfMonth.toString(),
            dueCountLabel = if (openCount > 0) "$openCount due" else "",
            hasWork = openCount > 0,
            isToday = date == today,
            isSelected = date == selectedDay,
        )
    }
}

/**
 * The month grid also renders only from backend date markers. Open work keeps the stronger
 * brand dot, while accepted-history-only days still get a muted marker so past completions stay
 * visible on the calendar without being misread as active work.
 */
internal fun buildMonthDays(markers: List<CalendarDateMarkerDto>, today: LocalDate): List<CalendarMonthDay> {
    val yearMonth = YearMonth.from(today)
    val firstDay = yearMonth.atDay(1)
    val lastDay = yearMonth.atEndOfMonth()
    val byDate = markers.associateBy { it.date }
    val leadingBlanks = (firstDay.dayOfWeek.value % 7).let { if (it == 0) 0 else it }
    val blanks = (0 until leadingBlanks).map { CalendarMonthDay(dateKey = null, dayNumber = null) }
    val days = (1..lastDay.dayOfMonth).map { dayNumber ->
        val date = yearMonth.atDay(dayNumber)
        val marker = byDate[date.toString()]
        val openCount = marker?.openCount ?: 0
        val completedCount = marker?.completedCount ?: 0
        val hasWork = openCount > 0
        val hasCompletedHistory = completedCount > 0
        CalendarMonthDay(
            dateKey = date.toString(),
            dayNumber = dayNumber.toString(),
            hasWork = hasWork,
            hasCompletedHistory = hasCompletedHistory,
            dotTone = when {
                hasWork -> CalendarTone.Ok
                hasCompletedHistory -> CalendarTone.Muted
                else -> CalendarTone.Neutral
            },
            isSelected = date == today,
        )
    }
    return blanks + days
}

internal fun parseLocalDate(due: String): LocalDate? =
    runCatching { OffsetDateTime.parse(due).atZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()

internal fun Map<String, JsonElement>.route(): String? =
    this["drive"]?.jsonPrimitive?.contentOrNull ?: this["vaccination"]?.jsonPrimitive?.contentOrNull

internal fun CalendarEventDto.routeTarget(): String? {
    if (status == COMPLETED_STATUS && !shedId.isNullOrBlank()) return "record/$shedId"
    val workflowHref = links["workflow"]?.jsonPrimitive?.contentOrNull
    if (!workflowHref.isNullOrBlank() && !shedId.isNullOrBlank()) return "scan/$shedId"
    return links.route()
}

internal fun fromSeverity(severity: String): CalendarTone = when (severity.lowercase()) {
    "critical", "high" -> CalendarTone.Danger
    "warn", "warning" -> CalendarTone.Warn
    "ok" -> CalendarTone.Ok
    else -> CalendarTone.Muted
}

internal fun CalendarEventDto.calendarTone(): CalendarTone =
    if (status == COMPLETED_STATUS) CalendarTone.Ok else fromSeverity(severity)

internal fun CalendarEventDto.toCalendarItem(): CalendarItem {
    val target = routeTarget()
    return CalendarItem(
        id = eventId,
        title = title,
        subtitle = subtitle,
        aggregated = aggregated,
        allDay = allDay,
        timeLabel = if (allDay) "" else calendarTimeLabel(dueAt),
        summaryPrimary = summaryPrimary,
        summarySecondary = summarySecondary,
        shedCount = shedCount,
        vaccineCount = vaccineCount,
        targetCount = targetCount,
        vaccineLabels = vaccineLabels,
        statusLabel = status,
        statusTone = calendarTone(),
        categoryLabel = vaccineName,
        ctaLabel = if (target != null) "Open" else null,
        target = target,
    )
}

internal fun calendarTimeLabel(dueAt: String): String =
    runCatching {
        OffsetDateTime.parse(dueAt)
            .atZoneSameInstant(KOLKATA)
            .toLocalTime()
            .let { "%02d:%02d".format(it.hour, it.minute) }
    }.getOrDefault("")

internal fun calendarDayTitle(date: LocalDate): String =
    "${date.dayOfMonth} ${date.month.getDisplayName(TextStyle.FULL, Locale.ENGLISH)}"

internal fun appendUniqueByEventId(
    existing: List<CalendarEventDto>,
    incoming: List<CalendarEventDto>,
): List<CalendarEventDto> =
    (existing + incoming).associateBy { it.eventId }.values.toList()
