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
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsToxin
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.ToxinRepository
import sg.mesha.goatos.core.network.dto.ToxinTaskDto
import sg.mesha.goatos.feature.toxin.ToxinFilterUi
import sg.mesha.goatos.feature.toxin.ToxinTaskCardUi
import sg.mesha.goatos.feature.toxin.ToxinTaskListEvent
import sg.mesha.goatos.feature.toxin.ToxinTaskListUiState
import javax.inject.Inject

/**
 * The Toxin module's L0 list state holder (module toxin, maintainer decision 2026-08-25 —
 * `docs/decisions/toxin-testing-module.md`).
 *
 * Offline-first: rows come from the Room-backed Pager in [ToxinRepository.tasks], so the cached
 * ~20-row window renders instantly and the network refresh writes THROUGH Room. Nothing is
 * derived here — every visible word on a card (`context_line`, `status_chip`, `origin_line`) is
 * backend-owned and passed through verbatim.
 */
@HiltViewModel
class ToxinTaskListViewModel @Inject constructor(
    private val repository: ToxinRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Scope(
        /** The selected backend filter KEY. "" means the backend default (All) on first load,
         *  before any chip has been tapped. */
        val filter: String = "",
        val title: String = "",
        /** Bumped by refresh so an unchanged scope is still a NEW value (StateFlow conflates). */
        val refreshNonce: Int = 0,
    )

    private val scope = MutableStateFlow(Scope())
    private val _isRefreshing = MutableStateFlow(false)

    /** Binds the backend nav label the shell routed with. Idempotent — recomposition may repeat it. */
    fun bind(title: String) {
        if (scope.value.title == title) return
        scope.value = scope.value.copy(title = title)
        analytics.track(AnalyticsEventsToxin.TOXIN_LIST_VIEWED)
    }

    val state: StateFlow<ToxinTaskListUiState> = combine(
        _isRefreshing,
        scope,
        repository.filters,
    ) { refreshing, current, chips ->
        // The empty line follows the SELECTED slice: an empty Completed list is not the same
        // news as an empty Pending one. Falls back to the module-level line before the first
        // refresh has delivered any chips.
        val selectedChip = chips.firstOrNull { chip ->
            if (current.filter.isBlank()) chip.selected else chip.key == current.filter
        }
        ToxinTaskListUiState(
            title = current.title,
            isRefreshing = refreshing,
            emptyMessage = selectedChip?.emptyMessage?.takeIf { it.isNotBlank() } ?: EMPTY_MESSAGE,
            // Chips are BACKEND-COMPOSED, labels and counts alike. Selection follows the
            // response until the user taps, then follows the tap so the chip highlights
            // immediately instead of waiting for the refetch to land.
            filters = chips.map { chip ->
                ToxinFilterUi(
                    key = chip.key,
                    label = chip.label,
                    count = chip.count,
                    selected = if (current.filter.isBlank()) chip.selected else chip.key == current.filter,
                    emptyMessage = chip.emptyMessage,
                )
            },
        )
    }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ToxinTaskListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<ToxinTaskCardUi>> = scope
        .flatMapLatest { current ->
            repository.tasks(current.filter).map { page -> page.map { it.toCardUi() } }
        }
        .cachedIn(viewModelScope)

    fun onEvent(event: ToxinTaskListEvent) {
        when (event) {
            ToxinTaskListEvent.Refresh -> refresh()
            is ToxinTaskListEvent.SelectFilter -> selectFilter(event.key)
            is ToxinTaskListEvent.OpenTask -> analytics.track(AnalyticsEventsToxin.TOXIN_TASK_OPENED)
        }
    }

    /** Re-scopes the list to a backend filter key. Each key is its own cached Room window, so
     *  switching back to one already loaded renders from cache while it refreshes. */
    private fun selectFilter(key: String) {
        if (scope.value.filter == key) return
        scope.value = scope.value.copy(filter = key)
        analytics.track(
            AnalyticsEventsToxin.TOXIN_LIST_FILTERED,
            mapOf(AnalyticsEvents.Params.REASON to key),
        )
    }

    /** Paging surfaced a load failure. The cached rows keep serving; this only reports it. */
    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "toxin task list page load failed")
        analytics.track(
            AnalyticsEventsToxin.TOXIN_FAILURE,
            mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS)),
        )
    }

    /** Non-blocking by contract: a failure leaves the cached rows on screen. */
    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                // Drop the freshness marker FIRST so the re-created pager refetches instead of
                // TTL-skipping — an explicit refresh means "show me the server's list now".
                // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                runCatching { repository.invalidateTasks(scope.value.filter) }
                // A NEW value, not an equal one: MutableStateFlow conflates on equality.
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val EMPTY_MESSAGE = "No feed loads waiting for a test"
        const val MAX_REASON_CHARS = 120
    }
}

/** Maps one backend task row to its list card. Backend copy is rendered verbatim. */
internal fun ToxinTaskDto.toCardUi(): ToxinTaskCardUi = ToxinTaskCardUi(
    listKey = taskId,
    taskId = taskId,
    contextLine = contextLine,
    statusChip = statusChip,
    statusTone = statusTone,
    isOverdue = isOverdue,
    originLine = originLine,
    stepsDone = stepsDone,
    stepsTotal = stepsTotal,
    // Two independent reasons a card does not open, and both are the backend's answer:
    //
    //   the ROUND is closed   — already sent for review, accepted, or cancelled. The row keeps
    //                           its chip and stops opening the capture drill; a cancelled round's
    //                           replacement arrives as its OWN task (retest = a new round, never
    //                           an edit), so nothing is lost.
    //   the PERSON only watches — a CEO/CXO sees every load's test and casts the verdict, but
    //                           never films a step (maintainer decision 2026-08-26). Their card
    //                           is a summary, not a way in. `canExecute` is the caller's own
    //                           toxin.execute, resolved server-side per request.
    openable = status == TOXIN_STATUS_IN_PROGRESS && canExecute,
)

internal const val TOXIN_STATUS_IN_PROGRESS = "in_progress"
