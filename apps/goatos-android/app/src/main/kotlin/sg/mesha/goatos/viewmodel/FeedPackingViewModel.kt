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
import kotlinx.coroutines.flow.scan
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedPackingQuery
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.network.dto.FeedFilterOptionsDto
import sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto
import sg.mesha.goatos.feature.feed.FeedDropdownOption
import sg.mesha.goatos.feature.feed.FeedFilterUi
import sg.mesha.goatos.feature.feed.FeedItemQtyUi
import sg.mesha.goatos.feature.feed.FeedItemTotalUi
import sg.mesha.goatos.feature.feed.FeedPackingEvent
import sg.mesha.goatos.feature.feed.FeedPackingRowUi
import sg.mesha.goatos.feature.feed.FeedPackingSummaryUi
import sg.mesha.goatos.feature.feed.FeedPackingUiState
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * Feed Packing (per-shed bag worklist) read-screen state holder. Same offline-first shape as
 * [FeedDirectionViewModel]; the worklist endpoint has no shed filter (a packer draws the whole
 * park's bags), so this screen's filters are farm + workflow only.
 */
@HiltViewModel
class FeedPackingViewModel @Inject constructor(
    private val repo: FeedRepository,
    private val feedCompletionStore: FeedCompletionLocalStore,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _filters = MutableStateFlow(FeedPackingSelection())

    @OptIn(ExperimentalCoroutinesApi::class)
    private val observed: StateFlow<FeedPackingEnvelope> = _filters
        .flatMapLatest { selection -> repo.observePackingTotals(selection.toQuery()) }
        .scan(FeedPackingEnvelope()) { carried, resource ->
            val fresh = resource.data?.filters
            FeedPackingEnvelope(
                resource = resource,
                filters = if (fresh != null && fresh.parks.isNotEmpty()) fresh else carried.filters,
            )
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FeedPackingEnvelope())

    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    val state: StateFlow<FeedPackingUiState> = combine(
        observed,
        _filters,
        _isRefreshing,
        _isOffline,
    ) { envelope, selection, isRefreshing, isOffline ->
        val dto = envelope.resource.data
        val hasSummary = dto != null
        FeedPackingUiState(
            title = TITLE,
            targetDateLabel = selection.targetDate,
            today = todayIso(),
            canCapture = selection.targetDate == todayIso(),
            filters = envelope.filters.toFilterUi(selection),
            summary = dto?.toSummaryUi() ?: FeedPackingSummaryUi(),
            hasSummary = hasSummary,
            emptyMessage = when {
                hasSummary && dto.summary.lineCount == 0 -> EMPTY_MESSAGE
                !hasSummary && isOffline -> ERROR_MESSAGE
                !hasSummary -> LOADING_MESSAGE
                else -> null
            },
            isErrorEmpty = !hasSummary && isOffline,
            isRefreshing = isRefreshing,
            lastSyncedAt = envelope.resource.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        FeedPackingUiState(
            title = TITLE,
            emptyMessage = LOADING_MESSAGE,
            targetDateLabel = todayIso(),
            today = todayIso(),
            canCapture = true,
        ),
    )

    // Combined with the optimistic local-completion set — a just-completed shed-session shows
    // completed immediately (offline-first), converging on the backend flag once the write syncs.
    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<FeedPackingRowUi>> =
        combine(_filters, feedCompletionStore.completedKeys) { selection, completed -> selection to completed }
            .flatMapLatest { (selection, completed) ->
                repo.packingRows(selection.toQuery())
                    .map { page -> page.map { it.toRowUi(completed) } }
            }
            .cachedIn(viewModelScope)

    init {
        analytics.track(AnalyticsEvents.FEED_PACKING_VIEWED)
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "feed packing page load failed")
        analytics.track(
            AnalyticsEvents.FEED_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "packing",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onEvent(event: FeedPackingEvent) {
        when (event) {
            FeedPackingEvent.Refresh -> refresh()
            is FeedPackingEvent.SelectPark -> selectPark(event.parkId)
            is FeedPackingEvent.SelectWorkflow -> selectWorkflow(event.workflow)
            is FeedPackingEvent.SelectSession -> selectSession(event.sessionNo)
            is FeedPackingEvent.SelectStatus -> selectStatus(event.status)
            is FeedPackingEvent.SelectDate -> selectDate(event.date)
            // Row-tap navigation is handled by the NavHost (opens the completion detail).
            is FeedPackingEvent.OpenRow -> Unit
            FeedPackingEvent.ClearFilters -> clearFilters()
        }
    }

    private fun refresh() {
        _isRefreshing.value = false
        _isOffline.value = false
        _filters.value = _filters.value.copy()
    }

    private fun selectPark(parkId: String) {
        val current = _filters.value
        if (current.parkId == parkId) return
        _filters.value = current.copy(parkId = parkId)
        trackFilter(DIMENSION_FARM, parkId)
    }

    private fun selectWorkflow(workflow: String) {
        val current = _filters.value
        if (current.workflow == workflow) return
        _filters.value = current.copy(workflow = workflow)
        trackFilter(DIMENSION_WORKFLOW, workflow)
    }

    private fun selectSession(sessionNo: Int) {
        val current = _filters.value
        if (current.session == sessionNo) return
        _filters.value = current.copy(session = sessionNo)
        trackFilter(DIMENSION_SESSION, if (sessionNo == 0) "" else sessionNo.toString())
    }

    private fun selectStatus(status: String) {
        val current = _filters.value
        if (current.status == status) return
        _filters.value = current.copy(status = status)
        trackFilter(DIMENSION_STATUS, status)
    }

    private fun clearFilters() {
        val current = _filters.value
        if (current.workflow.isBlank() && current.session == 0 && current.status.isBlank()) return
        _filters.value = current.copy(workflow = "", session = 0, status = "")
        trackFilter(DIMENSION_ALL, value = "")
    }

    // Ignore a future date outright — the date bar's next-day arrow already disables itself on
    // today and the DatePicker's own SelectableDates already blocks it, so reaching here with a
    // future date would only be a defensive-programming edge case, never the normal path.
    private fun selectDate(date: LocalDate) {
        if (date > LocalDate.now(ZoneId.of(INDIA_ZONE))) return
        val current = _filters.value
        val iso = date.toString()
        if (current.targetDate == iso) return
        _filters.value = current.copy(targetDate = iso)
        trackFilter(DIMENSION_DATE, iso)
    }

    private fun todayIso(): String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString()

    private fun trackFilter(dimension: String, value: String) {
        analytics.track(
            AnalyticsEvents.FEED_FILTER_APPLIED,
            mapOf(
                AnalyticsEvents.Params.DIMENSION to dimension,
                AnalyticsEvents.Params.ACTION to if (value.isBlank()) ACTION_CLEARED else ACTION_SET,
            ),
        )
    }

    private fun FeedFilterOptionsDto.toFilterUi(selection: FeedPackingSelection): FeedFilterUi {
        val parkOptions = parks.map { FeedDropdownOption(it.parkId, it.label) }
        val sessionOptions = sessions.map { FeedDropdownOption(it.sessionNo.toString(), it.label) }
        val activeParkId = selection.parkId.ifBlank { servedParkId }
        return FeedFilterUi(
            parks = parkOptions,
            selectedParkId = activeParkId,
            selectedParkLabel = parkOptions.firstOrNull { it.key == activeParkId }?.label,
            workflow = selection.workflow,
            sessions = sessionOptions,
            selectedSessionNo = selection.session,
            selectedSessionLabel = sessionOptions.firstOrNull { it.key == selection.session.toString() }?.label,
            status = selection.status,
        )
    }

    private fun FeedPackingWorklistPageDto.toSummaryUi(): FeedPackingSummaryUi = FeedPackingSummaryUi(
        shedCount = summary.shedCount,
        lineCount = summary.lineCount,
        blockedLineCount = summary.blockedLineCount,
        totalsByItem = summary.totalKgByFeedItem.map { FeedItemTotalUi(it.feedItem, it.quantityKg, it.blockedCells) },
    )

    private fun sg.mesha.goatos.core.network.dto.FeedPackingRowDto.toRowUi(
        locallyCompleted: Set<String>,
    ): FeedPackingRowUi = FeedPackingRowUi(
        grainKey = grainKey,
        parkId = parkId,
        shedId = shedId,
        sessionNo = sessionNo,
        shedLabel = shedLabel,
        sessionLabel = sessionLabel,
        workflow = workflow,
        experimentArm = experimentArm,
        headCount = headCount,
        items = items.map { FeedItemQtyUi(it.feedItem, it.quantityKg, it.isBlocked, it.blockedReason?.detail.orEmpty()) },
        totalKg = totalKg,
        status = status,
        completed = completed || locallyCompleted.contains(FeedCompletionLocalStore.key(shedId, sessionNo, workflow)),
        lifecycleStatus = lifecycleStatus,
    )

    private data class FeedPackingSelection(
        val parkId: String = "",
        val workflow: String = "",
        // 0 = every session (unfiltered); a positive value is a backend session_no.
        val session: Int = 0,
        // "" = every status; else a backend verification-lifecycle bucket
        // (pending | pending_verification | completed).
        val status: String = "",
        // The feed day. Reactive (not a fixed val) so the date bar can step it to a past day and
        // re-query, same as every other filter here.
        val targetDate: String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString(),
    ) {
        fun toQuery(): FeedPackingQuery = FeedPackingQuery(
            parkId = parkId,
            targetDate = targetDate,
            session = session.takeIf { it != 0 },
            workflow = workflow.takeIf { it.isNotBlank() },
            status = status.takeIf { it.isNotBlank() },
        )
    }

    private data class FeedPackingEnvelope(
        val resource: Resource<FeedPackingWorklistPageDto> = Resource(data = null),
        val filters: FeedFilterOptionsDto = FeedFilterOptionsDto(),
    )

    private companion object {
        const val INDIA_ZONE = "Asia/Kolkata"
        const val TITLE = "Feed Packing"
        const val LOADING_MESSAGE = "Loading packing worklist…"
        const val EMPTY_MESSAGE = "No packing lines for this farm and day"
        const val ERROR_MESSAGE = "Couldn't load the worklist. Tap refresh to retry."
        const val DIMENSION_FARM = "farm"
        const val DIMENSION_WORKFLOW = "workflow"
        const val DIMENSION_SESSION = "session"
        const val DIMENSION_STATUS = "status"
        const val DIMENSION_DATE = "date"
        const val DIMENSION_ALL = "all"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
    }
}
