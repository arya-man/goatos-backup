package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.feature.calendar.CalendarDayUiState
import java.time.LocalDate
import javax.inject.Inject

/**
 * L1 day-detail screen state holder (docs/decisions/android-offline-first.md). Opened from a
 * MONTH-grid day tap ([sg.mesha.goatos.feature.calendar.CalendarEvent.OpenDay]); it shows the
 * drives due on that day as its OWN screen, not a sheet appended under the grid.
 *
 * Offline-first: it observes the SAME cache-first [CalendarRepository.observeEvents] stream the
 * calendar uses (Room is the UI's single source of truth) and filters client-side to the tapped
 * date — so the day renders instantly from cache and a background [CalendarRepository.refreshEvents]
 * keeps it fresh, never a blank wall on re-entry. It never fabricates a day's content: a date with
 * no matching event renders an honest empty state.
 */
@HiltViewModel
class CalendarDayViewModel @Inject constructor(
    private val repo: CalendarRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    // Nav arg (the tapped day, as an ISO LocalDate string). Same key the day route declares.
    private val date: LocalDate? =
        savedStateHandle.get<String>("dateKey")?.let { runCatching { LocalDate.parse(it) }.getOrNull() }

    private val _state = MutableStateFlow(
        CalendarDayUiState(
            title = date?.let { calendarDayTitle(it) }.orEmpty(),
            emptyLabel = "No drives scheduled this day",
        ),
    )
    val state: StateFlow<CalendarDayUiState> = _state.asStateFlow()

    init {
        // Cache-first: renders whatever Room already holds for the calendar read (instant),
        // then re-renders after every successful refresh below.
        viewModelScope.launch {
            repo.observeEvents().collectLatest { resource -> applyResource(resource) }
        }
        refresh()
    }

    /** Drives the shared calendar network refresh; on failure the cached day content stays. */
    fun refresh() = viewModelScope.launch {
        _state.update { it.copy(isRefreshing = true) }
        val result = repo.refreshEvents()
        _state.update { current ->
            when {
                result.isSuccess -> current.copy(isRefreshing = false, isOffline = false)
                else -> current.copy(isRefreshing = false, isOffline = true)
            }
        }
    }

    private fun applyResource(resource: Resource<CalendarEventListResponseDto>) {
        val day = date
        val items = if (day == null) {
            emptyList()
        } else {
            resource.data?.items.orEmpty()
                .filter { parseLocalDate(it.dueAt) == day }
                .map { it.toCalendarItem() }
        }
        _state.update { current ->
            current.copy(
                items = items,
                lastSyncedAt = resource.lastSyncedAt ?: current.lastSyncedAt,
            )
        }
    }
}
