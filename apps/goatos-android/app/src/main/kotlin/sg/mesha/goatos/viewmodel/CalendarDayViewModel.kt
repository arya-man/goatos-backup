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
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.feature.calendar.CalendarDayUiState
import java.time.LocalDate
import javax.inject.Inject

/**
 * L1 day-detail screen. It uses the same 20-row keyset page size as the calendar drill and
 * exposes honest `hasMore` state instead of silently capping the day list.
 *
 * MOB-004: Room is the on-device SSOT for EVERY page, not just page 1. [loadMore] persists the
 * next continuation page into Room via [CalendarRepository.appendEvents]; the observed
 * [CalendarRepository.observeEvents] flow re-emits the merged, bounded keyset window. The
 * ViewModel keeps NO in-memory page accumulation, so page 2+ survives process death and is
 * available offline — the UI never renders a direct-network DTO.
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
            showCompletedHistory = completedHistoryOnly,
        ),
    )
    val state: StateFlow<CalendarDayUiState> = _state.asStateFlow()

    private val statusFilter: String? = if (completedHistoryOnly) COMPLETED_STATUS else null
    private var resource = Resource<CalendarEventListResponseDto>(data = null)
    private var loadingMore = false
    private var offline = false

    init {
        if (dateKey != null) {
            viewModelScope.launch {
                repo.observeEvents(status = statusFilter, dateFrom = dateKey, dateTo = dateKey, limit = CALENDAR_PAGE_SIZE)
                    .collectLatest { observed ->
                        resource = observed
                        rebuildState()
                    }
            }
        }
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        loadingMore = false
        rebuildState(isRefreshing = true)
        val key = dateKey
        if (key == null) {
            rebuildState(isRefreshing = false)
            return@launch
        }
        val result = repo.refreshEvents(status = statusFilter, dateFrom = key, dateTo = key, limit = CALENDAR_PAGE_SIZE)
        offline = result.isFailure
        rebuildState(isRefreshing = false)
    }

    fun loadMore() = viewModelScope.launch {
        val key = dateKey ?: return@launch
        val cursor = resource.data?.nextCursor ?: return@launch
        loadingMore = true
        rebuildState()
        // MOB-004: append the next page INTO Room; the observed flow re-emits the merged window.
        val result = repo.appendEvents(cursor = cursor, status = statusFilter, dateFrom = key, dateTo = key, limit = CALENDAR_PAGE_SIZE)
        offline = result.isFailure
        loadingMore = false
        rebuildState()
    }

    private fun rebuildState(isRefreshing: Boolean = _state.value.isRefreshing) {
        _state.value = _state.value.copy(
            items = resource.data?.items.orEmpty()
                .sortedBy { it.dueAt }
                .map { it.toCalendarItem() },
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: _state.value.lastSyncedAt,
            isOffline = offline,
            hasMore = resource.data?.nextCursor != null,
            isLoadingMore = loadingMore,
        )
    }
}
