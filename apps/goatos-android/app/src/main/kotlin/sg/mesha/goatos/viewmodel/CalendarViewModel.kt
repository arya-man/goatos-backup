package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonPrimitive
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.data.CalendarScheduleQuery
import sg.mesha.goatos.core.network.dto.CalendarDateMarkerDto
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.CalendarFilterOptionsDto
import sg.mesha.goatos.core.network.dto.CalendarPresentationDto
import sg.mesha.goatos.core.network.dto.DriveSummaryDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
import sg.mesha.goatos.feature.calendar.CalendarDriveSummary
import sg.mesha.goatos.feature.calendar.CalendarEvent
import sg.mesha.goatos.feature.calendar.CalendarFilterOption
import sg.mesha.goatos.feature.calendar.CalendarHistoryRow
import sg.mesha.goatos.feature.calendar.CalendarItem
import sg.mesha.goatos.feature.calendar.CalendarMonthDay
import sg.mesha.goatos.feature.calendar.CalendarMonthFilterOptions
import sg.mesha.goatos.feature.calendar.CalendarMonthFilters
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
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val today = LocalDate.now(KOLKATA)
    private val weekRange = calendarWeekRange(today)
    private val historyRange = calendarHistoryRange(today)

    private val _selectedDay = MutableStateFlow(today)
    private val _selectedSegmentId = MutableStateFlow<String?>(MONTH_SEGMENT)
    private val _monthFilters = MutableStateFlow(
        CalendarMonthFilters(year = today.year, month = today.monthValue),
    )
    private val _monthQuery = MutableStateFlow(initialMonthQuery(today))
    private val _historyActive = MutableStateFlow(false)

    @OptIn(ExperimentalCoroutinesApi::class)
    val monthItems: Flow<PagingData<CalendarItem>> = _monthQuery
        .flatMapLatest { query -> repo.schedule(query) }
        .map { page -> page.map { event -> event.toCalendarItem() } }
        .cachedIn(viewModelScope)

    // Upstream Room flows, lifecycle-aware via WhileSubscribed(5_000)
    private val weekOverviewResource: StateFlow<Resource<CalendarEventListResponseDto>> =
        repo.observeEvents(
            dateFrom = weekRange.dateFrom,
            dateTo = weekRange.dateTo,
            includeDateMarkers = true,
            limit = 1,
        ).stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))

    @OptIn(ExperimentalCoroutinesApi::class)
    private val monthMetadataResource: StateFlow<Resource<CalendarEventListResponseDto>> =
        _monthQuery
            .flatMapLatest { query -> repo.observeScheduleMetadata(query) }
            .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))

    @OptIn(ExperimentalCoroutinesApi::class)
    private val historyResource: StateFlow<Resource<CalendarEventListResponseDto>> =
        _historyActive.flatMapLatest { active ->
            if (!active) {
                flowOf(Resource(data = null))
            } else {
                repo.observeEvents(
                    status = COMPLETED_STATUS,
                    dateFrom = historyRange.dateFrom,
                    dateTo = historyRange.dateTo,
                    limit = CALENDAR_PAGE_SIZE,
                )
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))

    // Selected day resource is special because it changes based on user selection
    // flatMapLatest automatically cancels old collection and starts new when selectedDay changes
    @OptIn(ExperimentalCoroutinesApi::class)
    private val selectedDayResource: StateFlow<Resource<CalendarEventListResponseDto>> =
        _selectedDay.flatMapLatest { selectedDay ->
            repo.observeEvents(
                dateFrom = selectedDay.toString(),
                dateTo = selectedDay.toString(),
                limit = CALENDAR_PAGE_SIZE,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))

    // Transient flags
    private val _selectedDayLoadingMore = MutableStateFlow(false)
    private val _historyLoadingMore = MutableStateFlow(false)
    private val _refreshInFlight = MutableStateFlow(false)
    private val _offline = MutableStateFlow(false)
    private val _refreshError = MutableStateFlow<String?>(null)

    // Kotlin's typed `combine` overloads only cover up to 5 flows; this screen mixes 10 flows,
    // so we use the vararg form (one Array<*> param) and cast each element by its known index.
    @Suppress("UNCHECKED_CAST")
    val state: StateFlow<CalendarUiState> = combine(
        weekOverviewResource,
        monthMetadataResource,
        selectedDayResource,
        historyResource,
        _selectedDay,
        _selectedSegmentId,
        _monthFilters,
        _selectedDayLoadingMore,
        _historyLoadingMore,
        _refreshInFlight,
        _offline,
        _refreshError,
    ) { values: Array<Any?> ->
        buildCalendarState(
            week = values[0] as Resource<CalendarEventListResponseDto>,
            month = values[1] as Resource<CalendarEventListResponseDto>,
            selectedDay = values[2] as Resource<CalendarEventListResponseDto>,
            history = values[3] as Resource<CalendarEventListResponseDto>,
            currentSelectedDay = values[4] as LocalDate,
            selectedSegmentId = values[5] as String?,
            monthFilters = values[6] as CalendarMonthFilters,
            dayLoadingMore = values[7] as Boolean,
            historyLoadingMore = values[8] as Boolean,
            refreshInFlight = values[9] as Boolean,
            offline = values[10] as Boolean,
            refreshError = values[11] as String?,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        // R50-009: On cold start, show loading state; on cold cache + failed refresh, error handling
        // is via buildCalendarState checking if data is null with offline flag set
        calendarPlaceholder("Loading…")
    )

    init {
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _selectedDayLoadingMore.value = false
        _historyLoadingMore.value = false
        _refreshInFlight.value = true
        _refreshError.value = null

        val requests = mutableListOf(
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
                    dateFrom = _selectedDay.value.toString(),
                    dateTo = _selectedDay.value.toString(),
                    limit = CALENDAR_PAGE_SIZE,
                )
            },
        )
        if (_historyActive.value) {
            requests += async {
                repo.refreshEvents(
                    status = COMPLETED_STATUS,
                    dateFrom = historyRange.dateFrom,
                    dateTo = historyRange.dateTo,
                    limit = CALENDAR_PAGE_SIZE,
                )
            }
        }
        val results = requests.awaitAll()

        _refreshInFlight.value = false
        _offline.value = results.any { it.isFailure }
        _refreshError.value = results.firstNotNullOfOrNull { it.exceptionOrNull()?.message }
        results.forEach { reportFailure(it, "calendar refresh failed") }
    }

    fun onEvent(event: CalendarEvent) {
        when (event) {
            is CalendarEvent.SelectSegment -> {
                _selectedSegmentId.value = event.segmentId
                when (event.segmentId) {
                    MONTH_SEGMENT -> activateMonth()
                    HISTORY_SEGMENT -> {
                        val firstOpen = !_historyActive.value
                        _historyActive.value = true
                        if (firstOpen) refreshHistoryOnly()
                    }
                }
            }

            is CalendarEvent.ApplyMonthFilters -> applyMonthFilters(event.filters)
            CalendarEvent.ClearMonthFilters -> applyMonthFilters(
                CalendarMonthFilters(year = today.year, month = today.monthValue),
            )
            CalendarEvent.Refresh -> refresh()
            is CalendarEvent.TapDay -> selectDay(event.dateKey)
            CalendarEvent.LoadMoreWeek -> loadMoreSelectedDay()
            CalendarEvent.LoadMoreHistory -> loadMoreHistory()
            is CalendarEvent.OpenDay -> Unit
            is CalendarEvent.TapItem -> {
                // Completed-history rows open the read-only RecordViewModel, which emits its own
                // record-open event. Everything else is a drive drill and advances the funnel.
                if (!event.target.orEmpty().startsWith("record/")) {
                    AnalyticsFunnels.trackDriveOpened(analytics, event.itemId)
                }
            }
        }
    }

    private fun activateMonth() {
        val filters = _monthFilters.value
        val range = monthRange(filters)
        _monthQuery.value = CalendarScheduleQuery(
            parkId = filters.parkId,
            shedId = filters.shedId,
            vaccine = filters.vaccine,
            status = filters.status,
            dateFrom = range.dateFrom,
            dateTo = range.dateTo,
        )
    }

    private fun applyMonthFilters(filters: CalendarMonthFilters) {
        if (filters.month !in 1..12 || filters.year !in 2000..2200) return
        _monthFilters.value = filters.copy(
            parkId = filters.parkId?.takeIf(String::isNotBlank),
            shedId = filters.shedId?.takeIf(String::isNotBlank),
            vaccine = filters.vaccine?.takeIf(String::isNotBlank),
            status = filters.status?.takeIf(String::isNotBlank),
        )
        activateMonth()
    }

    private fun refreshHistoryOnly() = viewModelScope.launch {
        _refreshInFlight.value = true
        val result = repo.refreshEvents(
            status = COMPLETED_STATUS,
            dateFrom = historyRange.dateFrom,
            dateTo = historyRange.dateTo,
            limit = CALENDAR_PAGE_SIZE,
        )
        _refreshInFlight.value = false
        _offline.value = result.isFailure
        reportFailure(result, "calendar history refresh failed")
    }

    private fun selectDay(dateKey: String) {
        val date = runCatching { LocalDate.parse(dateKey) }.getOrNull() ?: return
        if (date == _selectedDay.value) return
        _selectedDay.value = date
        _selectedDayLoadingMore.value = false
        viewModelScope.launch {
            val result = repo.refreshEvents(
                dateFrom = date.toString(),
                dateTo = date.toString(),
                limit = CALENDAR_PAGE_SIZE,
            )
            _offline.value = result.isFailure
            reportFailure(result, "calendar day refresh failed")
        }
    }

    private fun loadMoreSelectedDay() = viewModelScope.launch {
        val cursor = selectedDayResource.value.data?.nextCursor ?: return@launch
        _selectedDayLoadingMore.value = true
        // MOB-004: append the next page INTO Room; the observed flow re-emits the merged window.
        val result = repo.appendEvents(
            cursor = cursor,
            dateFrom = _selectedDay.value.toString(),
            dateTo = _selectedDay.value.toString(),
            limit = CALENDAR_PAGE_SIZE,
        )
        _offline.value = result.isFailure
        reportFailure(result, "calendar day append failed")
        _selectedDayLoadingMore.value = false
    }

    private fun loadMoreHistory() = viewModelScope.launch {
        val cursor = historyResource.value.data?.nextCursor ?: return@launch
        _historyLoadingMore.value = true
        // MOB-004: append the next page INTO Room; the observed flow re-emits the merged window.
        val result = repo.appendEvents(
            cursor = cursor,
            status = COMPLETED_STATUS,
            dateFrom = historyRange.dateFrom,
            dateTo = historyRange.dateTo,
            limit = CALENDAR_PAGE_SIZE,
        )
        _offline.value = result.isFailure
        reportFailure(result, "calendar history append failed")
        _historyLoadingMore.value = false
    }

    private fun reportFailure(result: Result<Unit>, message: String) {
        result.exceptionOrNull()?.let { crashReporter.recordException(it, message) }
    }

    private fun buildCalendarState(
        week: Resource<CalendarEventListResponseDto>,
        month: Resource<CalendarEventListResponseDto>,
        selectedDay: Resource<CalendarEventListResponseDto>,
        history: Resource<CalendarEventListResponseDto>,
        currentSelectedDay: LocalDate,
        selectedSegmentId: String?,
        monthFilters: CalendarMonthFilters,
        dayLoadingMore: Boolean,
        historyLoadingMore: Boolean,
        refreshInFlight: Boolean,
        offline: Boolean,
        refreshError: String?,
    ): CalendarUiState {
        val base = sampleCalendarState()
        val presentation = activePresentation(week, month, selectedDay, history)
        val segments = buildSegments(presentation, base)
        val resolvedSegmentId = resolveSelectedSegment(segments, presentation, base, selectedSegmentId)

        // MOB-004: items come straight from the observed Room row, which already holds every
        // appended page (bounded keyset window) — no ViewModel-side accumulation.
        val dayItems = selectedDay.data?.items.orEmpty()
            .sortedBy { it.currentScheduleDate }
        val historyItems = history.data?.items.orEmpty()
            .filter { it.status == COMPLETED_STATUS }
            .sortedByDescending { it.currentScheduleDate }

        return base.copy(
            eyebrow = "Vaccination",
            title = presentation?.pageTitle?.ifBlank { base.title } ?: base.title,
            selectedDateLabel = dateLabel(currentSelectedDay),
            windowLabel = when (resolvedSegmentId) {
                MONTH_SEGMENT -> monthLabel(monthFilters.year, monthFilters.month)
                "history" -> "${historyRange.dateFrom} → ${historyRange.dateTo}"
                else -> null
            },
            isRefreshing = refreshInFlight,
            lastSyncedAt = listOfNotNull(
                week.lastSyncedAt,
                month.lastSyncedAt,
                selectedDay.lastSyncedAt,
                history.lastSyncedAt,
            ).maxOrNull(),
            isOffline = offline,
            errorMessage = refreshError?.takeIf {
                week.data == null && month.data == null && selectedDay.data == null && history.data == null
            }?.let { "Calendar could not load. Check your connection and try again." },
            segments = segments,
            selectedSegmentId = resolvedSegmentId,
            weekDays = buildWeekDays(week.data?.dateMarkers.orEmpty(), currentSelectedDay, today),
            weekItems = dayItems.map { it.toCalendarItem() },
            weekEmptyLabel = presentation?.emptyState?.okMessage?.ifBlank { base.weekEmptyLabel } ?: base.weekEmptyLabel,
            weekHasMore = selectedDay.data?.nextCursor != null,
            weekLoadingMore = dayLoadingMore,
            monthLabel = monthLabel(monthFilters.year, monthFilters.month),
            monthWeekdayLabels = emptyList(),
            monthDays = emptyList(),
            monthHint = "",
            monthFilters = monthFilters,
            monthFilterOptions = month.data?.filterOptions?.toUi() ?: CalendarMonthFilterOptions(),
            monthEmptyLabel = presentation?.emptyState?.okMessage?.ifBlank {
                "No vaccination drives match these filters"
            } ?: "No vaccination drives match these filters",
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
            historyHasMore = history.data?.nextCursor != null,
            historyLoadingMore = historyLoadingMore,
        )
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
        selectedSegmentId: String?,
    ): String =
        selectedSegmentId?.takeIf { id -> segments.any { it.id == id } }
            ?: presentation?.viewTabs?.firstOrNull { it.active }?.key?.takeIf { key -> segments.any { it.id == key } }
            ?: segments.firstOrNull()?.id
            ?: base.selectedSegmentId

    private fun activePresentation(
        week: Resource<CalendarEventListResponseDto>,
        month: Resource<CalendarEventListResponseDto>,
        selectedDay: Resource<CalendarEventListResponseDto>,
        history: Resource<CalendarEventListResponseDto>,
    ): CalendarPresentationDto? =
        listOfNotNull(
            week.data?.presentation?.takeIf(::hasPresentation),
            month.data?.presentation?.takeIf(::hasPresentation),
            selectedDay.data?.presentation?.takeIf(::hasPresentation),
            history.data?.presentation?.takeIf(::hasPresentation),
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
private const val MONTH_SEGMENT = "month"
private const val HISTORY_SEGMENT = "history"
internal const val COMPLETED_STATUS = "completed"
internal const val CALENDAR_PAGE_SIZE = 20
private const val HISTORY_WINDOW_DAYS = 45L

internal data class CalendarDateRange(val dateFrom: String, val dateTo: String)

private fun initialMonthQuery(today: LocalDate): CalendarScheduleQuery {
    val range = monthRange(CalendarMonthFilters(year = today.year, month = today.monthValue))
    return CalendarScheduleQuery(dateFrom = range.dateFrom, dateTo = range.dateTo)
}

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

private fun monthRange(filters: CalendarMonthFilters): CalendarDateRange =
    YearMonth.of(filters.year, filters.month).let {
        CalendarDateRange(it.atDay(1).toString(), it.atEndOfMonth().toString())
    }

private fun monthLabel(year: Int, month: Int): String =
    YearMonth.of(year, month).let {
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
            dayName = date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH),
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
    runCatching { LocalDate.parse(due) }.getOrNull()
        ?: runCatching { OffsetDateTime.parse(due).atZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()

internal fun Map<String, JsonElement>.route(): String? =
    this["drive"]?.jsonPrimitive?.contentOrNull ?: this["vaccination"]?.jsonPrimitive?.contentOrNull

internal fun CalendarEventDto.routeTarget(): String? {
    if (status == COMPLETED_STATUS && !shedId.isNullOrBlank()) return "record/$shedId"
    val workflowHref = links["workflow"]?.jsonPrimitive?.contentOrNull
    if (!workflowHref.isNullOrBlank() && !shedId.isNullOrBlank()) {
        val taskId = eventId.removePrefix("calendar:").takeIf { it.isNotBlank() && it != eventId }
        return taskId?.let { "scan/$shedId?task_id=$it" } ?: "vaccination"
    }
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
    val scheduleDate = currentScheduleDate
    val localDate = parseLocalDate(scheduleDate)
    return CalendarItem(
        id = eventId,
        title = title,
        subtitle = subtitle,
        aggregated = aggregated,
        allDay = allDay,
        timeLabel = if (allDay) "" else calendarTimeLabel(scheduleDate),
        summaryPrimary = summaryPrimary,
        summarySecondary = summarySecondary,
        shedCount = shedCount,
        vaccineCount = vaccineCount,
        targetCount = targetCount,
        vaccineLabels = vaccineLabels,
        shedLabels = shedLabels,
        dateLabel = localDate?.let {
            "${it.dayOfMonth} ${it.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} · " +
                it.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)
        }.orEmpty(),
        dateKey = localDate?.toString(),
        parkLabel = parkCode.orEmpty(),
        parkId = parkId,
        driveSummary = driveSummary?.toCalendarDriveSummary(),
        statusLabel = status,
        statusTone = calendarTone(),
        categoryLabel = vaccineName,
        ctaLabel = if (target != null) "Open" else null,
        target = target,
    )
}

private fun CalendarFilterOptionsDto.toUi(): CalendarMonthFilterOptions = CalendarMonthFilterOptions(
    parks = parks.map { CalendarFilterOption(it.value, it.label, it.parentValue) },
    sheds = sheds.map { CalendarFilterOption(it.value, it.label, it.parentValue) },
    vaccines = vaccines.map { CalendarFilterOption(it.value, it.label, it.parentValue) },
    statuses = statuses.map { CalendarFilterOption(it.key, it.label) },
    months = months.map { CalendarFilterOption(it.key, it.label) },
    years = years.map { CalendarFilterOption(it.key, it.label) },
)

/** Straight field mapping — see [DriveSummaryDto] / [CalendarDriveSummary] docs. */
internal fun DriveSummaryDto.toCalendarDriveSummary(): CalendarDriveSummary = CalendarDriveSummary(
    parkName = parkName,
    dueDateLabel = formatDriveDueDate(currentScheduleDate),
    shedCount = shedCount,
    shedsCompleted = shedsCompleted,
    vaccineLabels = vaccineLabels,
    totalCount = totalCount,
    completedCount = completedCount,
    totalAnimals = totalAnimals,
    completedAnimals = completedAnimals,
    remainingCount = remainingCount,
    dueCount = dueCount,
    overdueCount = overdueCount,
    deferredCount = deferredCount,
    ownerLabel = ownerLabel,
)

internal fun formatDriveDueDate(dueDate: String): String =
    runCatching {
        LocalDate.parse(dueDate).let { date ->
            "${date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} " +
                "${date.dayOfMonth} ${date.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)}"
        }
    }.getOrDefault(dueDate)

internal fun calendarTimeLabel(dueAt: String): String =
    runCatching {
        OffsetDateTime.parse(dueAt)
            .atZoneSameInstant(KOLKATA)
            .toLocalTime()
            .let { "%02d:%02d".format(it.hour, it.minute) }
    }.getOrDefault("")

internal fun calendarDayTitle(date: LocalDate): String =
    "${date.dayOfMonth} ${date.month.getDisplayName(TextStyle.FULL, Locale.ENGLISH)}"
