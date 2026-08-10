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
import sg.mesha.goatos.core.ui.operationalLocationLabel
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
            feedForDateLabel = feedDayIso(selection.targetDate),
            today = todayIso(),
            minDate = minPackingDayIso(),
            canCapture = selection.targetDate == todayIso(),
            filters = envelope.filters.toFilterUi(selection),
            summary = dto?.toSummaryUi() ?: FeedPackingSummaryUi(),
            hasSummary = hasSummary,
            emptyMessage = when {
                // A day whose dispatch clock has not fired serves no bags ON PURPOSE (normal 07:00,
                // experiment 14:00). The backend sentence names when the sheet arrives, so prefer it
                // over the generic "nothing to pack" — otherwise a gated morning reads as a fault.
                hasSummary && dto.summary.lineCount == 0 ->
                    dto.lifecycle.message.ifBlank { EMPTY_MESSAGE }
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
            feedForDateLabel = feedDayIso(todayIso()),
            today = todayIso(),
            minDate = minPackingDayIso(),
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
            is FeedPackingEvent.SelectStatus -> selectStatus(event.status)
            is FeedPackingEvent.SelectDate -> selectDate(event.date)
            is FeedPackingEvent.OpenRow -> analytics.track(
                AnalyticsEvents.FEED_ROW_TAPPED,
                mapOf(
                    AnalyticsEvents.Params.KIND to KIND_PACKING,
                    AnalyticsEvents.Params.SHED_ID to event.shedId,
                ),
            )
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

    private fun selectStatus(status: String) {
        val current = _filters.value
        if (current.status == status) return
        _filters.value = current.copy(status = status)
        trackFilter(DIMENSION_STATUS, status)
    }

    private fun clearFilters() {
        val current = _filters.value
        if (current.workflow.isBlank() && current.status.isBlank()) return
        _filters.value = current.copy(workflow = "", status = "")
        trackFilter(DIMENSION_ALL, value = "")
    }

    // Ignore an out-of-window packing day outright — the date bar's arrows and the DatePicker's own
    // SelectableDates already clamp to [today - PAST_WINDOW_DAYS, today], so reaching here off-window
    // would only be a defensive-programming edge case, never the normal path. The axis is the PACKING
    // day: today is capturable, a past day within the window is view-only history, the future is out.
    private fun selectDate(date: LocalDate) {
        val today = LocalDate.now(ZoneId.of(INDIA_ZONE))
        if (date > today || date < today.minusDays(PAST_WINDOW_DAYS)) return
        val current = _filters.value
        val iso = date.toString()
        if (current.targetDate == iso) return
        _filters.value = current.copy(targetDate = iso)
        trackFilter(DIMENSION_DATE, iso)
    }

    private fun todayIso(): String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString()

    // The feed day the selected PACKING day is for: packing day + 1 (a packer works today on the sheet
    // fed tomorrow). This is the day the backend keys on and the caption states.
    private fun feedDayIso(packingIso: String): String =
        runCatching { LocalDate.parse(packingIso).plusDays(1).toString() }
            .onFailure { crashReporter.recordException(it, "feed packing day parse failed") }
            .getOrDefault(packingIso)

    // The oldest packing day the date bar may reach: today - PAST_WINDOW_DAYS.
    private fun minPackingDayIso(): String =
        LocalDate.now(ZoneId.of(INDIA_ZONE)).minusDays(PAST_WINDOW_DAYS).toString()

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
        val activeParkId = selection.parkId.ifBlank { servedParkId }
        // No session options: the packing screen has no session filter. The backend still serves the
        // park's session vocabulary because feed DIRECTION shares this contract and still filters by
        // it; packing simply does not read it.
        return FeedFilterUi(
            parks = parkOptions,
            selectedParkId = activeParkId,
            selectedParkLabel = parkOptions.firstOrNull { it.key == activeParkId }?.label,
            workflow = selection.workflow,
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
        // Shed + partition, never the bare shed name: a feed/packing row is one OPERATIONAL
        // LOCATION, so Castro 1 and Castro 2 share a shed_id and would otherwise print as two
        // identical "Castro" lines the operator cannot tell apart. Prefers the backend-composed
        // display and falls back to composing it only when an older server omits the field.
        shedLabel = operationalLocationDisplay.ifBlank { operationalLocationLabel(shedLabel, partitionLabel) },
        partitionLabel = partitionLabel.orEmpty(),
        workflow = workflow,
        experimentArm = experimentArm,
        headCount = headCount,
        sessions = sessions.map { session ->
            FeedPackingSessionUi(
                sessionNo = session.sessionNo,
                sessionLabel = session.sessionLabel,
                items = session.items.map {
                    FeedItemQtyUi(it.feedItem, it.quantityKg, it.isBlocked, it.blockedReason?.detail.orEmpty())
                },
                totalKg = session.totalKg,
                status = session.status,
            )
        },
        totalKg = totalKg,
        status = status,
        completed = completed || locallyCompleted.contains(FeedCompletionLocalStore.key(shedId, partitionLabel, 0, workflow)),
        lifecycleStatus = lifecycleStatus,
    )

    private data class FeedPackingSelection(
        val parkId: String = "",
        val workflow: String = "",
        // No session: a packing line is a whole pen-DAY carrying every session as a breakdown, so
        // there is nothing to narrow to. The endpoint no longer accepts the parameter either.
        // "" = every status; else a backend verification-lifecycle bucket
        // (pending | pending_verification | completed).
        val status: String = "",
        // The feed day. Reactive (not a fixed val) so the date bar can step it to a past day and
        // re-query, same as every other filter here.
        val targetDate: String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString(),
    ) {
        fun toQuery(): FeedPackingQuery = FeedPackingQuery(
            parkId = parkId,
            // The backend keys on the FEED day; this selection's axis is the PACKING day, so send
            // packing day + 1 (a packer works today on the sheet fed tomorrow). The Room cache key is
            // built from this query's targetDate, so it partitions by feed day automatically.
            targetDate = runCatching { LocalDate.parse(targetDate).plusDays(1).toString() }
                .getOrDefault(targetDate),
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
        const val KIND_PACKING = "packing"
        // How far back the packing-day picker may browse historical sheets.
        const val PAST_WINDOW_DAYS = 30L
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
