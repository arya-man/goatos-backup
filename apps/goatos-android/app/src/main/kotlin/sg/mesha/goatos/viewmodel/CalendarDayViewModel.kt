package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.network.isConnectivityFailure
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
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

    private val statusFilter: String? = if (completedHistoryOnly) COMPLETED_STATUS else null

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<CalendarEventListResponseDto>> =
        if (dateKey != null) {
            repo.observeEvents(status = statusFilter, dateFrom = dateKey, dateTo = dateKey, limit = CALENDAR_PAGE_SIZE)
                .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))
        } else {
            MutableStateFlow(Resource<CalendarEventListResponseDto>(data = null))
        }

    // Transient flags
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)

    // Combined state, lifecycle-aware
    val state: StateFlow<CalendarDayUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline,
        _isLoadingMore
    ) { resource, isRefreshing, isOffline, isLoadingMore ->
        val items = resource.data?.items.orEmpty()
            .sortedBy { it.currentScheduleDate }
            .map { it.toCalendarItem() }
        CalendarDayUiState(
            title = date?.let { calendarDayTitle(it) }.orEmpty(),
            dateKey = dateKey,
            showCompletedHistory = completedHistoryOnly,
            items = items,
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = isOffline,
            hasMore = resource.data?.nextCursor != null,
            isLoadingMore = isLoadingMore,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        CalendarDayUiState(
            title = date?.let { calendarDayTitle(it) }.orEmpty(),
            dateKey = dateKey,
            showCompletedHistory = completedHistoryOnly,
        )
    )

    init {
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _isLoadingMore.value = false
        _isRefreshing.value = true
        val key = dateKey
        if (key == null) {
            _isRefreshing.value = false
            return@launch
        }
        val result = repo.refreshEvents(status = statusFilter, dateFrom = key, dateTo = key, limit = CALENDAR_PAGE_SIZE)
        _isOffline.value = result.exceptionOrNull().isConnectivityFailure()
        _isRefreshing.value = false
    }

    fun loadMore() = viewModelScope.launch {
        val key = dateKey ?: return@launch
        val cursor = observedResource.value.data?.nextCursor ?: return@launch
        _isLoadingMore.value = true
        // MOB-004: append the next page INTO Room; the observed flow re-emits the merged window.
        val result = repo.appendEvents(cursor = cursor, status = statusFilter, dateFrom = key, dateTo = key, limit = CALENDAR_PAGE_SIZE)
        _isOffline.value = result.exceptionOrNull().isConnectivityFailure()
        _isLoadingMore.value = false
    }
}
