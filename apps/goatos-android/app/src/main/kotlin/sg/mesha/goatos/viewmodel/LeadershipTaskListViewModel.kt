package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.boot.NavStateRefreshSignal
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsLeadershipTasks
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.LeadershipTasksRepository
import sg.mesha.goatos.core.network.dto.LeadershipTaskDto
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskCardUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskFilterUi
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskListEvent
import sg.mesha.goatos.feature.leadershiptasks.LeadershipTaskListUiState
import javax.inject.Inject

/**
 * The Leadership Tasks L0 list state holder (maintainer request 2026-09-04).
 *
 * Offline-first: rows come from the Room-backed Pager in [LeadershipTasksRepository.tasks], so
 * the cached ~20-row window renders instantly and the network refresh writes THROUGH Room.
 * Nothing is derived here — every visible word on a card is backend-owned and passed through.
 * The "+" is shown only when the backend's page said `can_raise`; nothing here asks who the
 * caller is.
 */
@HiltViewModel
class LeadershipTaskListViewModel @Inject constructor(
    private val repository: LeadershipTasksRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    private val navRefresh: NavStateRefreshSignal,
) : ViewModel() {

    private data class Scope(
        /** The selected backend filter KEY. "" means the backend default before any tap. */
        val filter: String = "",
        val fallbackTitle: String = "",
        /** Bumped by refresh so an unchanged scope is still a NEW value (StateFlow conflates). */
        val refreshNonce: Int = 0,
    )

    private val scope = MutableStateFlow(Scope())
    private val _isRefreshing = MutableStateFlow(false)

    init {
        // The page facts land once per list refresh; each landing means the backend just counted
        // the caller's unseen tasks, so the drawer/bar badge is re-read to match. `drop(1)` skips
        // the StateFlow's initial empty value, which is not a refresh.
        viewModelScope.launch {
            repository.pageMeta.drop(1).collect { navRefresh.request() }
        }
    }

    /** Binds the backend nav label the shell routed with. Idempotent — recomposition may repeat it. */
    fun bind(title: String) {
        if (scope.value.fallbackTitle == title) return
        scope.value = scope.value.copy(fallbackTitle = title)
        analytics.track(AnalyticsEventsLeadershipTasks.LIST_VIEWED)
    }

    val state: StateFlow<LeadershipTaskListUiState> = combine(
        _isRefreshing,
        scope,
        repository.pageMeta,
    ) { refreshing, current, meta ->
        val selectedChip = meta.filters.firstOrNull { chip ->
            if (current.filter.isBlank()) chip.selected else chip.key == current.filter
        }
        LeadershipTaskListUiState(
            // The backend's own page title once a page has landed; the nav label until then.
            title = meta.title.ifBlank { current.fallbackTitle },
            isRefreshing = refreshing,
            emptyMessage = selectedChip?.emptyMessage?.takeIf { it.isNotBlank() }
                ?: meta.filters.firstOrNull()?.emptyMessage?.takeIf { it.isNotBlank() },
            filters = meta.filters.map { chip ->
                LeadershipTaskFilterUi(
                    key = chip.key,
                    label = chip.label,
                    count = chip.count,
                    selected = if (current.filter.isBlank()) chip.selected else chip.key == current.filter,
                    emptyMessage = chip.emptyMessage,
                )
            },
            canRaise = meta.canRaise,
        )
    }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), LeadershipTaskListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<LeadershipTaskCardUi>> = scope
        .flatMapLatest { current ->
            repository.tasks(current.filter).map { page -> page.map { it.toCardUi() } }
        }
        .cachedIn(viewModelScope)

    fun onEvent(event: LeadershipTaskListEvent) {
        when (event) {
            LeadershipTaskListEvent.Refresh -> refresh()
            is LeadershipTaskListEvent.SelectFilter -> selectFilter(event.key)
            is LeadershipTaskListEvent.OpenTask -> analytics.track(AnalyticsEventsLeadershipTasks.TASK_OPENED)
            LeadershipTaskListEvent.RaiseTask -> Unit
        }
    }

    private fun selectFilter(key: String) {
        if (scope.value.filter == key) return
        scope.value = scope.value.copy(filter = key)
    }

    /** Paging surfaced a load failure. The cached rows keep serving; this only reports it. */
    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "leadership task list page load failed")
        analytics.track(
            AnalyticsEventsLeadershipTasks.FAILURE,
            mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS)),
        )
    }

    /** Non-blocking by contract: a failure leaves the cached rows on screen. */
    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                runCatching { repository.invalidateTasks(scope.value.filter) }
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val MAX_REASON_CHARS = 120
    }
}

/** Maps one backend task row to its list card. Backend copy is rendered verbatim. */
internal fun LeadershipTaskDto.toCardUi(): LeadershipTaskCardUi = LeadershipTaskCardUi(
    listKey = taskId,
    taskId = taskId,
    numberLabel = numberLabel,
    statusChip = statusChip,
    title = title,
    metaLine = metaLine,
    attachmentCount = attachmentCount,
    unseen = isUnseenForCaller(),
)

/**
 * "New to me": not yet seen AND the caller is the one who can act on it. A raiser's row also
 * carries `is_seen = false` until the CXO opens it, but that is news about the CXO, not a task
 * the raiser has yet to read — so the rail is keyed on the assignee's own capability.
 */
internal fun LeadershipTaskDto.isUnseenForCaller(): Boolean = !isSeen && isAssignee
