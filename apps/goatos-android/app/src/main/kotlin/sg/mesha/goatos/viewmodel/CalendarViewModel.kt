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
import sg.mesha.goatos.feature.calendar.CalendarSegment
import sg.mesha.goatos.feature.calendar.CalendarSegmentKind
import sg.mesha.goatos.feature.calendar.CalendarTone
import sg.mesha.goatos.feature.calendar.CalendarUiState
import sg.mesha.goatos.ui.calendarPlaceholder
import sg.mesha.goatos.ui.sampleCalendarState
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
        val weekItems = items.map { it.toCalendarItem() }
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
            weekDays = emptyList(),
            weekItems = weekItems,
            weekEmptyLabel = presentation.emptyState.okMessage.ifBlank { "No drives scheduled" },
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

/** The row-navigation route the backend attached to a calendar event ("drive" preferred). */
private fun Map<String, JsonElement>.route(): String? =
    this["drive"]?.jsonPrimitive?.contentOrNull ?: this["vaccination"]?.jsonPrimitive?.contentOrNull

private fun fromSeverity(severity: String): CalendarTone = when (severity.lowercase()) {
    "critical", "high" -> CalendarTone.Danger
    "warn", "warning" -> CalendarTone.Warn
    "ok" -> CalendarTone.Ok
    else -> CalendarTone.Muted
}
