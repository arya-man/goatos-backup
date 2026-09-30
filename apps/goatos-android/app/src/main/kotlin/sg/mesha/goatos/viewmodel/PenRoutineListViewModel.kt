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
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsPenRoutines
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.PenRoutinesRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.penRoutineGrainKey
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_SCOPE_PARK
import sg.mesha.goatos.core.network.dto.PenRoutineTaskDto
import sg.mesha.goatos.feature.penroutines.PenRoutineCardUi
import sg.mesha.goatos.feature.penroutines.PenRoutineFilterUi
import sg.mesha.goatos.feature.penroutines.PenRoutineListEvent
import sg.mesha.goatos.feature.penroutines.PenRoutineListUiState
import sg.mesha.goatos.feature.penroutines.PenRoutineTone
import sg.mesha.goatos.feature.penroutines.PenRoutineTabFiltersUi
import sg.mesha.goatos.feature.penroutines.PEN_ROUTINE_FILTER_DONE
import sg.mesha.goatos.feature.penroutines.PEN_ROUTINE_FILTER_TODO
import sg.mesha.goatos.core.data.PenRoutinePageMeta
import sg.mesha.goatos.core.data.PenRoutineQuery
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_TAB_FILTER_DATE
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_TAB_FILTER_PEN
import sg.mesha.goatos.core.network.dto.PEN_ROUTINE_TAB_FILTER_STATUS
import sg.mesha.goatos.core.network.dto.PenRoutinePenOptionDto
import sg.mesha.goatos.core.ui.filters.WorklistDateWindow
import sg.mesha.goatos.core.ui.filters.WorklistPen
import sg.mesha.goatos.core.ui.filters.WorklistPenOption
import sg.mesha.goatos.core.ui.filters.WorklistStatus
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/** Business dates are Asia/Kolkata (AGENTS.md: UTC never defines a Goat OS business day). */
private const val PEN_ROUTINE_BUSINESS_ZONE = "Asia/Kolkata"

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

    /** The Asia/Kolkata business date the date sheet opens on; a test pins it. */
    internal var today: () -> LocalDate = { LocalDate.now(ZoneId.of(PEN_ROUTINE_BUSINESS_ZONE)) }

    private data class Scope(
        /** The selected backend filter KEY. "" means the backend default before any tap. */
        val filter: String = "",
        val fallbackTitle: String = "",
        /**
         * The web-authored phone tab this list is bound to (maintainer instruction 2026-10-01);
         * "" = the Routines list. Never changes after [bind] — one back-stack entry, one tab.
         */
        val tab: String = "",
        /** The tab's date window; null = no date narrowing (the tab's default, see [bind]). */
        val window: WorklistDateWindow? = null,
        /** The tab's pen; null = every pen. */
        val pen: WorklistPen? = null,
        /** Bumped by refresh so an unchanged scope is still a NEW value (StateFlow conflates). */
        val refreshNonce: Int = 0,
        /** Set once [bind] ran, so the pager never fires a request for the wrong (unbound) tab. */
        val bound: Boolean = false,
    )

    private val scope = MutableStateFlow(Scope())
    private val _isRefreshing = MutableStateFlow(false)

    /**
     * Binds the list to the route it was opened on: [title] is the backend nav label the shell
     * routed with (shown until the page's own title lands) and [tabKey] the web-authored tab, or ""
     * for the Routines list. Idempotent — recomposition may repeat it.
     *
     * A tab opens with NO date narrowing: routine work that was not done keeps its planned date
     * while it rolls forward as delayed, so a "today onwards" default would hide exactly the work
     * a park head is most behind on. The reader narrows by date only when they pick a window.
     */
    fun bind(title: String, tabKey: String = "") {
        val current = scope.value
        if (current.bound && current.fallbackTitle == title && current.tab == tabKey) return
        scope.value = current.copy(fallbackTitle = title, tab = tabKey, bound = true)
        analytics.track(
            AnalyticsEventsPenRoutines.LIST_OPENED,
            buildMap {
                put(AnalyticsEvents.Params.SOURCE, if (tabKey.isBlank()) "routines_tab" else "authored_tab")
                if (tabKey.isNotBlank()) put(AnalyticsEventsPenRoutines.Params.TAB_KEY, tabKey.take(MAX_TAB_KEY_CHARS))
            },
        )
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    private val meta: Flow<PenRoutinePageMeta> = scope
        .map { it.tab }
        .distinctUntilChanged()
        .flatMapLatest { tab -> repository.pageMeta(tab) }

    val state: StateFlow<PenRoutineListUiState> = combine(
        _isRefreshing,
        scope,
        meta,
    ) { refreshing, current, meta ->
        val selectedChip = meta.filters.firstOrNull { chip ->
            if (current.filter.isBlank()) chip.selected else chip.key == current.filter
        }
        PenRoutineListUiState(
            // The backend's own page title once a page has landed (on a tab: the tab's authored
            // label); the nav label until then.
            title = meta.title.ifBlank { meta.tab?.label.orEmpty() }.ifBlank { current.fallbackTitle },
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
            tabFilters = if (current.tab.isBlank()) null else tabFiltersUi(current, meta),
        )
    }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), PenRoutineListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<PenRoutineCardUi>> = scope
        .filter { it.bound }
        .flatMapLatest { current -> repository.tasks(current.query()) }
        .cachedIn(viewModelScope)
        .combine(syncRepository.observeSubmittedForReviewGrains()) { page, sending ->
            page.map { task -> task.toCardUi(sending = penRoutineGrainKey(task.taskId, task.rowVersion) in sending) }
        }

    fun onEvent(event: PenRoutineListEvent) {
        when (event) {
            PenRoutineListEvent.Refresh -> refresh()
            is PenRoutineListEvent.SelectFilter -> selectFilter(event.key)
            is PenRoutineListEvent.SelectDateWindow -> selectWindow(event.window)
            is PenRoutineListEvent.SelectPen -> selectPen(event.pen)
            is PenRoutineListEvent.OpenTask -> analytics.track(
                AnalyticsEventsPenRoutines.DETAIL_OPENED,
                mapOf("task_id" to event.taskId, AnalyticsEvents.Params.SOURCE to "list_card"),
            )
        }
    }

    private fun selectFilter(key: String) {
        if (scope.value.filter == key) return
        scope.value = scope.value.copy(filter = key)
        trackFilter(PEN_ROUTINE_TAB_FILTER_STATUS)
    }

    private fun selectWindow(window: WorklistDateWindow?) {
        if (scope.value.window == window) return
        scope.value = scope.value.copy(window = window)
        trackFilter(PEN_ROUTINE_TAB_FILTER_DATE)
    }

    private fun selectPen(pen: WorklistPen?) {
        if (scope.value.pen == pen) return
        scope.value = scope.value.copy(pen = pen)
        trackFilter(PEN_ROUTINE_TAB_FILTER_PEN)
    }

    /** One bounded event per filter change: which control, on which tab. No values, no ids. */
    private fun trackFilter(kind: String) {
        analytics.track(
            AnalyticsEventsPenRoutines.FILTER_CHANGED,
            buildMap {
                put(AnalyticsEvents.Params.KIND, kind)
                val tab = scope.value.tab
                put(AnalyticsEvents.Params.SOURCE, if (tab.isBlank()) "routines_tab" else "authored_tab")
                if (tab.isNotBlank()) put(AnalyticsEventsPenRoutines.Params.TAB_KEY, tab.take(MAX_TAB_KEY_CHARS))
            },
        )
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
                runCatching { repository.invalidateTasks(scope.value.query()) }
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val MAX_REASON_CHARS = 120
        const val MAX_TAB_KEY_CHARS = 40
    }

    /** The list request this scope names. */
    private fun Scope.query(): PenRoutineQuery = PenRoutineQuery(
        filter = filter,
        tab = tab,
        dueFrom = window?.fromIso.orEmpty(),
        dueTo = window?.toIso.orEmpty(),
        pen = pen?.let { chosen -> penToken(chosen) }.orEmpty(),
    )

    /**
     * The `pen=` token for a chosen pen: the backend's own `pen_options[].value` when the option is
     * still on offer, else the same "<shed_id>|<partition_label>" shape the backend documents.
     */
    private fun penToken(pen: WorklistPen): String =
        penOptionsSnapshot.firstOrNull { it.shedId == pen.shedId && it.partitionLabel == pen.partitionLabel }
            ?.value?.takeIf { it.isNotBlank() }
            ?: pen.key

    /** The last pen options the tab's page carried; read only to echo a pen's opaque token back. */
    @Volatile
    private var penOptionsSnapshot: List<PenRoutinePenOptionDto> = emptyList()

    private fun tabFiltersUi(current: Scope, meta: PenRoutinePageMeta): PenRoutineTabFiltersUi {
        penOptionsSnapshot = meta.penOptions
        val offered = meta.tab?.filters.orEmpty()
        val todo = meta.filters.firstOrNull { it.key == PEN_ROUTINE_FILTER_TODO }
        val done = meta.filters.firstOrNull { it.key == PEN_ROUTINE_FILTER_DONE }
        val selectedKey = current.filter.ifBlank { meta.filters.firstOrNull { it.selected }?.key.orEmpty() }
        return PenRoutineTabFiltersUi(
            showStatus = PEN_ROUTINE_TAB_FILTER_STATUS in offered,
            showDate = PEN_ROUTINE_TAB_FILTER_DATE in offered,
            showPen = PEN_ROUTINE_TAB_FILTER_PEN in offered,
            status = if (selectedKey == PEN_ROUTINE_FILTER_DONE) WorklistStatus.COMPLETED else WorklistStatus.PENDING,
            pendingCount = todo?.count ?: 0,
            completedCount = done?.count ?: 0,
            window = current.window,
            today = today(),
            pen = current.pen,
            penOptions = meta.penOptions.map { option ->
                WorklistPenOption(
                    shedId = option.shedId,
                    partitionLabel = option.partitionLabel,
                    label = option.operationalLocationDisplay.ifBlank { option.label },
                    parkName = option.parkName,
                    count = option.count,
                )
            },
        )
    }
}

/** Maps one backend task row to its list card. Backend copy is rendered verbatim. */
internal fun PenRoutineTaskDto.toCardUi(sending: Boolean = false): PenRoutineCardUi = PenRoutineCardUi(
    listKey = taskId,
    taskId = taskId,
    title = title,
    penLabel = operationalLocationDisplay,
    parkName = parkName,
    parkTask = scopeKind == PEN_ROUTINE_SCOPE_PARK,
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
