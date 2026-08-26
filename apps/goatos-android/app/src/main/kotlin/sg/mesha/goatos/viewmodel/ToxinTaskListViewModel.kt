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
        /** "" = every status. The module has no status tabs today; the repository takes one anyway. */
        val status: String = "",
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

    val state: StateFlow<ToxinTaskListUiState> = _isRefreshing
        .map { refreshing ->
            ToxinTaskListUiState(
                title = scope.value.title,
                isRefreshing = refreshing,
                emptyMessage = EMPTY_MESSAGE,
            )
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ToxinTaskListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<ToxinTaskCardUi>> = scope
        .flatMapLatest { current ->
            repository.tasks(current.status).map { page -> page.map { it.toCardUi() } }
        }
        .cachedIn(viewModelScope)

    fun onEvent(event: ToxinTaskListEvent) {
        when (event) {
            ToxinTaskListEvent.Refresh -> refresh()
            is ToxinTaskListEvent.OpenTask -> analytics.track(AnalyticsEventsToxin.TOXIN_TASK_OPENED)
        }
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
                runCatching { repository.invalidateTasks(scope.value.status) }
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
    originLine = originLine,
    stepsDone = stepsDone,
    stepsTotal = stepsTotal,
    // A round that is already sent for review, accepted, or cancelled is closed to the tester:
    // the row keeps its chip and stops opening the capture drill. A cancelled round's replacement
    // arrives as its OWN task (retest = a new round, never an edit), so nothing is lost by this.
    openable = status == TOXIN_STATUS_IN_PROGRESS,
)

internal const val TOXIN_STATUS_IN_PROGRESS = "in_progress"
