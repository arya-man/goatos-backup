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
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.data.FeedWastageQuery
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.data.sync.submittedGrainKey
import sg.mesha.goatos.core.network.dto.FeedFilterOptionsDto
import sg.mesha.goatos.core.network.dto.FeedWastageWorklistPageDto
import sg.mesha.goatos.core.ui.operationalLocationLabel
import sg.mesha.goatos.feature.feed.FeedDropdownOption
import sg.mesha.goatos.feature.feed.FeedFilterUi
import sg.mesha.goatos.feature.feed.FeedWastageEvent
import sg.mesha.goatos.feature.feed.FeedWastageRowUi
import sg.mesha.goatos.feature.feed.FeedWastageSummaryUi
import sg.mesha.goatos.feature.feed.FeedWastageUiState
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * Feed Wastage (per-EXPERIMENT-pen leftover-feed worklist, maintainer decision 2026-08-18)
 * read-screen state holder. Same offline-first shape as [FeedPackingViewModel], one axis simpler:
 * wastage is measured on the FEED day itself — what is left over after that day's feeding — so the
 * selected date IS the backend `target_date`, with no packing-day +1 arithmetic. The grain is the
 * PEN-DAY, so there is no session filter, and every row is an experiment pen, so there is no
 * workflow filter either.
 */
@HiltViewModel
class FeedWastageViewModel @Inject constructor(
    private val repo: FeedRepository,
    private val submittedGrains: SubmittedGrainsSource,
    private val bootstrapRepository: BootstrapRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _filters = MutableStateFlow(FeedWastageSelection())
    private val _canExecuteWastage = MutableStateFlow(false)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val observed: StateFlow<FeedWastageEnvelope> = _filters
        .flatMapLatest { selection -> repo.observeWastageTotals(selection.toQuery()) }
        .scan(FeedWastageEnvelope()) { carried, resource ->
            val fresh = resource.data?.filters
            FeedWastageEnvelope(
                resource = resource,
                filters = if (fresh != null && fresh.parks.isNotEmpty()) fresh else carried.filters,
            )
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FeedWastageEnvelope())

    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    val state: StateFlow<FeedWastageUiState> = combine(
        observed,
        _filters,
        _canExecuteWastage,
        _isRefreshing,
        _isOffline,
    ) { envelope, selection, canExecuteWastage, isRefreshing, isOffline ->
        val dto = envelope.resource.data
        val hasSummary = dto != null
        FeedWastageUiState(
            title = TITLE,
            targetDateLabel = selection.targetDate,
            today = todayIso(),
            minDate = minFeedDayIso(),
            canCapture = canExecuteWastage && selection.targetDate == todayIso(),
            filters = envelope.filters.toFilterUi(selection),
            summary = dto?.toSummaryUi() ?: FeedWastageSummaryUi(),
            hasSummary = hasSummary,
            emptyMessage = when {
                // A day whose experiment sheet has not been issued serves no pens ON PURPOSE; the
                // backend sentence names when it arrives, so prefer it over the generic empty copy.
                hasSummary && dto.summary.totalPens == 0 ->
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
        FeedWastageUiState(
            title = TITLE,
            emptyMessage = LOADING_MESSAGE,
            targetDateLabel = todayIso(),
            today = todayIso(),
            minDate = minFeedDayIso(),
            canCapture = true,
        ),
    )

    // Combined with the OUTBOX-derived submitted-for-review set — a just-submitted pen shows
    // "in review" immediately (offline-first), converging on the backend bucket once the write
    // syncs. Derived from the outbox, never an in-memory set, so a submit that succeeds or dies
    // leaves the set by itself (the 254.mp4 stranded-badge class).
    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<FeedWastageRowUi>> =
        combine(_filters, submittedGrains.observe()) { selection, submitted -> selection to submitted }
            .flatMapLatest { (selection, submitted) ->
                val feedDay = selection.toQuery().targetDate
                repo.wastageRows(selection.toQuery())
                    .map { page -> page.map { it.toRowUi(submitted, feedDay) } }
            }
            .cachedIn(viewModelScope)

    init {
        analytics.track(
            AnalyticsEvents.FEED_WASTAGE_VIEWED,
            mapOf(
                AnalyticsEvents.Params.SOURCE to SCREEN_FEED_WASTAGE,
                AnalyticsEvents.Params.KIND to KIND_WASTAGE,
            ),
        )
        viewModelScope.launch {
            _canExecuteWastage.value = runCatching {
                bootstrapRepository.operatorProfile()?.primaryRoleHint == ROLE_OPERATOR
            }.onFailure {
                crashReporter.recordException(it, "feed wastage execute-role bootstrap failed")
            }.getOrDefault(false)
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "feed wastage page load failed")
        analytics.track(
            AnalyticsEvents.FEED_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to KIND_WASTAGE,
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onEvent(event: FeedWastageEvent) {
        when (event) {
            FeedWastageEvent.Refresh -> refresh()
            is FeedWastageEvent.SelectPark -> selectPark(event.parkId)
            is FeedWastageEvent.SelectStatus -> selectStatus(event.status)
            is FeedWastageEvent.SelectDate -> selectDate(event.date)
            is FeedWastageEvent.OpenRow -> analytics.track(
                AnalyticsEvents.FEED_ROW_TAPPED,
                mapOf(
                    AnalyticsEvents.Params.SOURCE to SCREEN_FEED_WASTAGE,
                    AnalyticsEvents.Params.KIND to KIND_WASTAGE,
                    AnalyticsEvents.Params.SHED_ID to event.shedId,
                    AnalyticsEvents.Params.PARTITION_LABEL to event.partitionLabel,
                    AnalyticsEvents.Params.ACTION to ACTION_OPEN_ROW,
                ),
            )
            FeedWastageEvent.ClearFilters -> clearFilters()
        }
    }

    private fun refresh() {
        _isRefreshing.value = false
        _isOffline.value = false
        // A NEW value, not an equal one: MutableStateFlow conflates on equality (see
        // FeedPackingViewModel.refresh's kdoc for the defect this avoids).
        _filters.value = _filters.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
    }

    private fun selectPark(parkId: String) {
        val current = _filters.value
        if (current.parkId == parkId) return
        _filters.value = current.copy(parkId = parkId)
        trackFilter(DIMENSION_FARM, parkId)
    }

    private fun selectStatus(status: String) {
        val current = _filters.value
        if (current.status == status) return
        _filters.value = current.copy(status = status)
        trackFilter(DIMENSION_STATUS, status)
    }

    private fun clearFilters() {
        val current = _filters.value
        if (current.status.isBlank()) return
        _filters.value = current.copy(status = "")
        trackFilter(DIMENSION_ALL, value = "")
    }

    // The axis is the FEED day: today is capturable, a past day within the window is view-only
    // history, the future is out. The date bar and DatePicker already clamp; this is the backstop.
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

    private fun minFeedDayIso(): String =
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

    private fun FeedFilterOptionsDto.toFilterUi(selection: FeedWastageSelection): FeedFilterUi {
        val parkOptions = parks.map { FeedDropdownOption(it.parkId, it.label) }
        val activeParkId = selection.parkId.ifBlank { servedParkId }
        return FeedFilterUi(
            parks = parkOptions,
            selectedParkId = activeParkId,
            selectedParkLabel = parkOptions.firstOrNull { it.key == activeParkId }?.label,
            status = selection.status,
        )
    }

    private fun FeedWastageWorklistPageDto.toSummaryUi(): FeedWastageSummaryUi = FeedWastageSummaryUi(
        totalPens = summary.totalPens,
        pendingPens = summary.pendingPens,
        inReviewPens = summary.inReviewPens,
        completedPens = summary.completedPens,
    )

    private fun sg.mesha.goatos.core.network.dto.FeedWastageRowDto.toRowUi(
        locallySubmittedForReview: Set<String>,
        feedDay: String,
    ): FeedWastageRowUi {
        // The SAME builder the outbox projection uses — one definition, so the two cannot disagree.
        val isLocallySubmittedForReview = locallySubmittedForReview.contains(
            submittedGrainKey(feedDay),
        )
        // Precedence lives in ONE place — [overlayVerificationStatus] — shared with every other
        // verifier-gated feed list.
        val overlaidLifecycleStatus = overlayVerificationStatus(
            backendStatus = lifecycleStatus,
            reworkReason = reworkReason,
            isLocallySubmitted = isLocallySubmittedForReview,
            inReviewToken = IN_REVIEW_PENDING_VERIFICATION,
        )
        return FeedWastageRowUi(
            // Full identity: shed + pen + FEED DAY — never the shed alone. The day is in the key
            // even though the list shows one day at a time, so a re-scoped page can never recycle
            // another day's row state.
            listKey = "$feedDay|$grainKey",
            parkId = parkId,
            parkLabel = parkLabel,
            shedId = shedId,
            // Shed + partition, never the bare shed name: two pens of one shed would otherwise
            // print as identical lines. Prefers the backend-composed display.
            shedLabel = operationalLocationDisplay.ifBlank { operationalLocationLabel(shedLabel, partitionLabel) },
            partitionLabel = partitionLabel.orEmpty(),
            experimentArm = experimentArm,
            headCount = headCount,
            completed = completed,
            lifecycleStatus = overlaidLifecycleStatus,
            reworkReason = reworkReason,
            wastageKg = wastageKg.orEmpty(),
        )
    }

    private data class FeedWastageSelection(
        val parkId: String = "",
        // "" = every status; else a backend verification-lifecycle bucket.
        val status: String = "",
        // The FEED day (wastage is measured the day the feed was served — no +1 axis).
        val targetDate: String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString(),
        /** Bumped by refresh so an unchanged selection is still a NEW value. */
        val refreshNonce: Int = 0,
    ) {
        fun toQuery(): FeedWastageQuery = FeedWastageQuery(
            parkId = parkId,
            targetDate = targetDate,
            status = status.takeIf { it.isNotBlank() },
        )
    }

    private data class FeedWastageEnvelope(
        val resource: Resource<FeedWastageWorklistPageDto> = Resource(data = null),
        val filters: FeedFilterOptionsDto = FeedFilterOptionsDto(),
    )

    private companion object {
        const val INDIA_ZONE = "Asia/Kolkata"
        const val KIND_WASTAGE = "wastage"
        const val SCREEN_FEED_WASTAGE = "feed_wastage"
        const val ACTION_OPEN_ROW = "open_row"
        const val PAST_WINDOW_DAYS = 30L
        const val TITLE = "Feed Wastage"
        const val LOADING_MESSAGE = "Loading wastage worklist…"
        const val EMPTY_MESSAGE = "No experiment pens owe a leftover-feed video for this farm and day"
        const val ERROR_MESSAGE = "Couldn't load the worklist. Tap refresh to retry."
        const val DIMENSION_FARM = "farm"
        const val DIMENSION_STATUS = "status"
        const val DIMENSION_DATE = "date"
        const val DIMENSION_ALL = "all"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
        const val ROLE_OPERATOR = "operator"
    }
}
