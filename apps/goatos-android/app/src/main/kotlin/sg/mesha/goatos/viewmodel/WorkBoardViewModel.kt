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
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.scan
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWorkBoard
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.WorkBoardLanes
import sg.mesha.goatos.core.data.WorkBoardQuery
import sg.mesha.goatos.core.common.datetime.GoatOsDates
import sg.mesha.goatos.core.data.WorkBoardRepository
import sg.mesha.goatos.core.network.dto.WorkBoardRowDto
import sg.mesha.goatos.core.network.dto.WorkBoardSummaryDto
import sg.mesha.goatos.feature.workboard.WorkBoardChipUi
import sg.mesha.goatos.feature.workboard.WorkBoardEmptyMessage
import sg.mesha.goatos.feature.workboard.WorkBoardEvent
import sg.mesha.goatos.feature.workboard.WorkBoardOwnerState
import sg.mesha.goatos.feature.workboard.WorkBoardRowUi
import sg.mesha.goatos.feature.workboard.WorkBoardSeverity
import sg.mesha.goatos.feature.workboard.WorkBoardUiState
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * The Work Board's My Work L0 state holder (maintainer decision 2026-09-10).
 *
 * Offline-first: the summary tiles come from the Room-backed [WorkBoardRepository.observeSummary]
 * and the rows from its Room-backed Pager, so the cached ~20-row window renders instantly and the
 * network refresh writes THROUGH Room. Nothing is derived here — every visible word on a card is
 * backend-owned and passed through, `lane` and `work_state` included. The phone never filters
 * rows by role: the backend clamps an operator to their own rows and says so (`own_rows_only`).
 *
 * The park is the caller's own from the bootstrap operator profile (`primary_location_id`); when
 * the profile carries none the request omits it and the backend resolves a park-scoped caller
 * itself (a tenant-wide caller is told `park_required`, which the screen renders as the error).
 */
@HiltViewModel
class WorkBoardViewModel @Inject constructor(
    private val repository: WorkBoardRepository,
    private val bootstrapRepository: BootstrapRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Selection(
        /** The caller's park once resolved from bootstrap; null when the profile names none. */
        val parkId: String? = null,
        /** No query is issued before the park is known, or the cache scope would change under it. */
        val parkResolved: Boolean = false,
        val dateIso: String = todayIso(),
        val lane: String = "",
        val module: String = "",
        /** Bumped by refresh so an unchanged selection is still a NEW value (StateFlow conflates). */
        val refreshNonce: Int = 0,
    ) {
        fun toQuery(): WorkBoardQuery = WorkBoardQuery(
            parkId = parkId,
            businessDate = dateIso,
            lane = lane,
            module = module,
            refreshNonce = refreshNonce,
        )
    }

    /** The last summary carried across a failed refresh, so the chips never blink away. */
    private data class Envelope(
        val resource: Resource<WorkBoardSummaryDto> = Resource(data = null),
        val modules: List<String> = emptyList(),
    )

    private val _filters = MutableStateFlow(Selection())
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    init {
        viewModelScope.launch {
            // exception:exempt a bootstrap read failure just leaves the park unnamed; the backend
            // resolves a park-scoped caller itself and a tenant-wide caller sees the server's refusal.
            val parkId = runCatching { bootstrapRepository.operatorProfile()?.primaryLocationId }
                .onFailure { crashReporter.recordException(it, "work board park resolve failed") }
                .getOrNull()
                ?.takeIf { it.isNotBlank() }
            _filters.value = _filters.value.copy(parkId = parkId, parkResolved = true)
        }
        analytics.track(AnalyticsEventsWorkBoard.VIEWED, mapOf(AnalyticsEvents.Params.SOURCE to "my_work"))
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    private val observed: StateFlow<Envelope> = _filters
        .filter { it.parkResolved }
        .flatMapLatest { selection -> repository.observeSummary(selection.toQuery()) }
        .scan(Envelope()) { carried, resource ->
            val fresh = resource.data?.modules.orEmpty()
            Envelope(resource = resource, modules = if (fresh.isNotEmpty()) fresh else carried.modules)
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Envelope())

    val state: StateFlow<WorkBoardUiState> = combine(
        observed,
        _filters,
        _isRefreshing,
        _isOffline,
    ) { envelope, selection, isRefreshing, isOffline ->
        val summary = envelope.resource.data
        val hasSummary = summary != null
        val done = summary?.laneCount(WorkBoardLanes.DONE) ?: 0
        val total = summary?.total ?: 0
        WorkBoardUiState(
            dateIso = selection.dateIso,
            dateLabel = dateLabel(selection.dateIso),
            isToday = selection.dateIso == todayIso(),
            lanes = buildList {
                add(WorkBoardChipUi(key = "", count = total, selected = selection.lane.isBlank()))
                WorkBoardLanes.ORDER.forEach { lane ->
                    add(WorkBoardChipUi(key = lane, count = summary?.laneCount(lane) ?: 0, selected = selection.lane == lane))
                }
            },
            modules = if (envelope.modules.isEmpty()) {
                emptyList()
            } else {
                buildList {
                    add(WorkBoardChipUi(key = "", count = total, selected = selection.module.isBlank()))
                    envelope.modules.forEach { module ->
                        add(
                            WorkBoardChipUi(
                                key = module,
                                count = summary?.byModule?.get(module) ?: 0,
                                selected = selection.module == module,
                            ),
                        )
                    }
                }
            },
            doneCount = done,
            pendingCount = (total - done).coerceAtLeast(0),
            needsAttentionCount = summary?.needsAttention ?: 0,
            hasSummary = hasSummary,
            isRefreshing = isRefreshing,
            lastSyncedAt = envelope.resource.lastSyncedAt,
            isOffline = isOffline,
            emptyMessage = when {
                !hasSummary && isOffline -> WorkBoardEmptyMessage.ERROR
                !hasSummary -> WorkBoardEmptyMessage.LOADING
                summary.ownRowsOnly -> WorkBoardEmptyMessage.EMPTY_OWN
                else -> WorkBoardEmptyMessage.EMPTY_ALL
            },
            ownRowsOnly = summary?.ownRowsOnly ?: false,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        WorkBoardUiState(
            dateIso = todayIso(),
            dateLabel = dateLabel(todayIso()),
            isToday = true,
            emptyMessage = WorkBoardEmptyMessage.LOADING,
        ),
    )

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<WorkBoardRowUi>> = _filters
        .filter { it.parkResolved }
        .flatMapLatest { selection -> repository.observeRows(selection.toQuery()) }
        .map { page -> page.map { row -> row.toRowUi() } }
        .cachedIn(viewModelScope)

    fun onEvent(event: WorkBoardEvent) {
        when (event) {
            WorkBoardEvent.Refresh -> refresh()
            is WorkBoardEvent.SelectLane -> selectLane(event.lane)
            is WorkBoardEvent.SelectModule -> selectModule(event.module)
            WorkBoardEvent.PreviousDay -> stepDay(-1)
            WorkBoardEvent.NextDay -> stepDay(1)
            is WorkBoardEvent.OpenRow -> analytics.track(
                AnalyticsEventsWorkBoard.ROW_OPENED,
                mapOf("row_key" to event.rowKey, AnalyticsEvents.Params.SOURCE to "list_card"),
            )
        }
    }

    /** Paging surfaced a load failure. The cached rows keep serving; this only reports it. */
    fun onRowsLoadFailed(error: Throwable) {
        _isRefreshing.value = false
        _isOffline.value = true
        crashReporter.recordException(error, "work board page load failed")
        analytics.track(
            AnalyticsEventsWorkBoard.FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "rows",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS),
            ),
        )
    }

    /** Paging settled a refresh: the spinner stops and a prior page failure is cleared. */
    fun onRowsLoaded() {
        _isRefreshing.value = false
        _isOffline.value = false
    }

    /**
     * Non-blocking by contract: the summary is re-read (a failure marks the screen offline and
     * leaves the cache serving), then the rows are re-paged from the top through a nonce bump.
     * The spinner clears when Paging settles, through [onRowsLoaded] / [onRowsLoadFailed].
     */
    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            val selection = _filters.filter { it.parkResolved }.first()
            repository.refresh(selection.toQuery())
                .onSuccess { _isOffline.value = false }
                .onFailure { error ->
                    _isOffline.value = true
                    crashReporter.recordException(error, "work board summary refresh failed")
                    analytics.track(
                        AnalyticsEventsWorkBoard.FAILURE,
                        mapOf(
                            AnalyticsEvents.Params.KIND to "summary",
                            AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(MAX_REASON_CHARS),
                        ),
                    )
                }
            _filters.value = _filters.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
        }
    }

    private fun selectLane(lane: String) {
        if (_filters.value.lane == lane) return
        _filters.value = _filters.value.copy(lane = lane)
        trackFilter(DIMENSION_LANE, lane)
    }

    private fun selectModule(module: String) {
        if (_filters.value.module == module) return
        _filters.value = _filters.value.copy(module = module)
        trackFilter(DIMENSION_MODULE, module)
    }

    private fun stepDay(days: Long) {
        val current = _filters.value
        val next = runCatching { LocalDate.parse(current.dateIso).plusDays(days).toString() }
            .onFailure { crashReporter.recordException(it, "work board day parse failed") }
            .getOrNull() ?: return
        _filters.value = current.copy(dateIso = next)
        trackFilter(DIMENSION_DATE, next)
    }

    private fun trackFilter(dimension: String, value: String) {
        analytics.track(
            AnalyticsEventsWorkBoard.FILTER_APPLIED,
            mapOf(
                AnalyticsEvents.Params.DIMENSION to dimension,
                AnalyticsEvents.Params.ACTION to if (value.isBlank()) ACTION_CLEARED else ACTION_SET,
            ),
        )
    }

    private companion object {
        const val MAX_REASON_CHARS = 120
        const val DIMENSION_LANE = "lane"
        const val DIMENSION_MODULE = "module"
        const val DIMENSION_DATE = "date"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
    }
}

private const val INDIA_ZONE = "Asia/Kolkata"

/** Today's business date in Asia/Kolkata (the board's time grain is the IST day, never an instant). */
internal fun todayIso(): String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString()

/**
 * The day as the reader sees it: weekday + DD/MM/YYYY through GoatOsDates (maintainer decision
 * 2026-09-10, every visible date renders DD/MM/YYYY). An unparseable value renders as itself.
 */
internal fun dateLabel(iso: String): String =
    // exception:exempt a display formatter over a value the ViewModel itself produced; the raw ISO
    // day is the honest fallback and stepDay already reports a parse failure on the write side.
    runCatching { GoatOsDates.weekdayDate(LocalDate.parse(iso)) }.getOrDefault(iso)

/** Maps one backend board row to its card. Backend copy is rendered verbatim; enums only colour it. */
internal fun WorkBoardRowDto.toRowUi(): WorkBoardRowUi = WorkBoardRowUi(
    rowKey = rowKey,
    module = module,
    title = title,
    subtitle = subtitle,
    penLabel = pen.operationalLocationDisplay,
    parkName = parkName,
    businessDate = businessDate,
    clockLabel = clockLabel,
    workState = workState,
    lane = lane,
    severity = WorkBoardSeverity.from(severity),
    ownerName = owner.name,
    ownerState = WorkBoardOwnerState.from(ownerState),
    done = counts.done,
    pending = counts.pending,
    needsAttention = counts.needsAttention,
    inReview = counts.inReview,
    notStarted = counts.notStarted,
    href = href,
)
