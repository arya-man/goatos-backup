package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.feature.calendar.CalendarDayUiState
import java.time.LocalDate
import javax.inject.Inject

/**
 * L1 day-detail screen. It now uses the same 20-row keyset page size as the calendar
 * drill and exposes honest `hasMore` state instead of silently capping the day list.
 */
@HiltViewModel
class CalendarDayViewModel @Inject constructor(
    private val repo: CalendarRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val date: LocalDate? =
        savedStateHandle.get<String>("dateKey")?.let { runCatching { LocalDate.parse(it) }.getOrNull() }
    private val dateKey: String? = date?.toString()
    private val completedHistoryOnly = savedStateHandle.get<String>("status") == COMPLETED_STATUS

    private val _state = MutableStateFlow(
        CalendarDayUiState(
            title = date?.let { calendarDayTitle(it) }.orEmpty(),
            emptyLabel = if (completedHistoryOnly) "No accepted completion history this day" else "No drives scheduled this day",
        ),
    )
    val state: StateFlow<CalendarDayUiState> = _state.asStateFlow()

    private var resource = Resource<CalendarEventListResponseDto>(data = null)
    private var extraItems: List<CalendarEventDto> = emptyList()
    private var nextCursor: String? = null
    private var loadingMore = false
    private var offline = false

    init {
        if (dateKey != null) {
            viewModelScope.launch {
                repo.observeEvents(status = if (completedHistoryOnly) COMPLETED_STATUS else null, dateFrom = dateKey, dateTo = dateKey, limit = CALENDAR_PAGE_SIZE)
                    .collectLatest { observed ->
                        resource = observed
                        if (extraItems.isEmpty()) {
                            nextCursor = observed.data?.nextCursor
                        }
                        rebuildState()
                    }
            }
        }
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        extraItems = emptyList()
        nextCursor = null
        loadingMore = false
        rebuildState(isRefreshing = true)
        val key = dateKey
        if (key == null) {
            rebuildState(isRefreshing = false)
            return@launch
        }
        val result = repo.refreshEvents(status = if (completedHistoryOnly) COMPLETED_STATUS else null, dateFrom = key, dateTo = key, limit = CALENDAR_PAGE_SIZE)
        offline = result.isFailure
        rebuildState(isRefreshing = false)
    }

    fun loadMore() = viewModelScope.launch {
        val key = dateKey ?: return@launch
        val cursor = nextCursor ?: return@launch
        loadingMore = true
        rebuildState()
        runCatching {
            repo.events(status = if (completedHistoryOnly) COMPLETED_STATUS else null, dateFrom = key, dateTo = key, cursor = cursor, limit = CALENDAR_PAGE_SIZE)
        }.onSuccess { page ->
            extraItems = appendUniqueByEventId(extraItems, page.items)
            nextCursor = page.nextCursor
            offline = false
        }.onFailure {
            offline = true
        }
        loadingMore = false
        rebuildState()
    }

    private fun rebuildState(isRefreshing: Boolean = _state.value.isRefreshing) {
        _state.value = _state.value.copy(
            items = appendUniqueByEventId(resource.data?.items.orEmpty(), extraItems)
                .sortedBy { it.dueAt }
                .map { it.toCalendarItem() },
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: _state.value.lastSyncedAt,
            isOffline = offline,
            hasMore = nextCursor != null,
            isLoadingMore = loadingMore,
        )
    }
}
