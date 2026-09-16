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
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenRoutines
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.PenRoutinesRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.penRoutineGrainKey
import sg.mesha.goatos.core.network.dto.PenRoutineTaskDto
import sg.mesha.goatos.feature.penroutines.PenRoutineCardUi
import sg.mesha.goatos.feature.penroutines.PenRoutineFilterUi
import sg.mesha.goatos.feature.penroutines.PenRoutineListEvent
import sg.mesha.goatos.feature.penroutines.PenRoutineListUiState
import sg.mesha.goatos.feature.penroutines.PenRoutineTone
import javax.inject.Inject

/**
 * The Routines L0 list state holder (maintainer instruction 2026-09-16,
 * docs/decisions/pen-routines.md).
 *
 * Offline-first: rows come from the Room-backed Pager in [PenRoutinesRepository.tasks], so the
 * cached ~20-row window renders instantly and the network refresh writes THROUGH Room. Nothing
 * is derived here — every visible word on a card is backend-owned and passed through. The one
 * client-side mark is "Sending", read from the outbox's ACTIVE set through the SAME grain key the
 * projection writes ([penRoutineGrainKey]), so a submit that succeeds or dies retracts it by itself.
 */
@HiltViewModel
class PenRoutineListViewModel @Inject constructor(
    private val repository: PenRoutinesRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
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

    /** Binds the backend nav label the shell routed with. Idempotent — recomposition may repeat it. */
    fun bind(title: String) {
        if (scope.value.fallbackTitle == title) return
        scope.value = scope.value.copy(fallbackTitle = title)
        analytics.track(
            AnalyticsEventsPenRoutines.LIST_OPENED,
            mapOf(
                AnalyticsEvents.Params.SOURCE to "routines_tab",
                AnalyticsEvents.Params.COUNT to repository.pageMeta.value.openCount.toString(),
            ),
        )
    }

    val state: StateFlow<PenRoutineListUiState> = combine(
        _isRefreshing,
        scope,
        repository.pageMeta,
    ) { refreshing, current, meta ->
        val selectedChip = meta.filters.firstOrNull { chip ->
            if (current.filter.isBlank()) chip.selected else chip.key == current.filter
        }
        PenRoutineListUiState(
            // The backend's own page title once a page has landed; the nav label until then.
            title = meta.title.ifBlank { current.fallbackTitle },
            isRefreshing = refreshing,
            emptyMessage = selectedChip?.emptyMessage?.takeIf { it.isNotBlank() }
                ?: meta.filters.firstOrNull()?.emptyMessage?.takeIf { it.isNotBlank() },
            filters = meta.filters.map { chip ->
                PenRoutineFilterUi(
                    key = chip.key,
                    label = chip.label,
                    count = chip.count,
                    selected = if (current.filter.isBlank()) chip.selected else chip.key == current.filter,
                    emptyMessage = chip.emptyMessage,
                )
            },
        )
    }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PenRoutineListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<PenRoutineCardUi>> = scope
        .flatMapLatest { current -> repository.tasks(current.filter) }
        .cachedIn(viewModelScope)
        .combine(syncRepository.observeSubmittedForReviewGrains()) { page, sending ->
            page.map { task -> task.toCardUi(sending = penRoutineGrainKey(task.taskId, task.rowVersion) in sending) }
        }

    fun onEvent(event: PenRoutineListEvent) {
        when (event) {
            PenRoutineListEvent.Refresh -> refresh()
            is PenRoutineListEvent.SelectFilter -> selectFilter(event.key)
            is PenRoutineListEvent.OpenTask -> analytics.track(
                AnalyticsEventsPenRoutines.DETAIL_OPENED,
                mapOf("task_id" to event.taskId, AnalyticsEvents.Params.SOURCE to "list_card"),
            )
        }
    }

    private fun selectFilter(key: String) {
        if (scope.value.filter == key) return
        scope.value = scope.value.copy(filter = key)
    }

    /** Paging surfaced a load failure. The cached rows keep serving; this only reports it. */
    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "pen routine list page load failed")
        analytics.track(
            AnalyticsEventsPenRoutines.FAILURE,
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
internal fun PenRoutineTaskDto.toCardUi(sending: Boolean = false): PenRoutineCardUi = PenRoutineCardUi(
    listKey = taskId,
    taskId = taskId,
    title = title,
    penLabel = operationalLocationDisplay,
    parkName = parkName,
    reasonLine = reasonLine,
    evidenceLine = evidenceLine,
    stateChip = stateChip,
    tone = PenRoutineTone.from(stateTone),
    // A task already with the verifier or done is never "sending", whatever the outbox still
    // holds: the submit reached the server. Work still owed (open, or sent back) may be on the
    // wire; the card's chip says which.
    sending = sending && (status == PEN_ROUTINE_STATUS_OPEN || status == PEN_ROUTINE_STATUS_REWORK),
    done = workState == PEN_ROUTINE_WORK_STATE_COMPLETED,
)
