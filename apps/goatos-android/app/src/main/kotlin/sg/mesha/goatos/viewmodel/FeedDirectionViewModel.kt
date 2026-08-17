package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.distinctUntilChanged
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
import sg.mesha.goatos.core.ui.operationalLocationLabel
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.sync.SubmittedGrainsSource
import sg.mesha.goatos.core.data.sync.submittedGrainKey
import sg.mesha.goatos.core.data.FeedCompletionLocalStore
import sg.mesha.goatos.core.data.FeedDirectionQuery
import sg.mesha.goatos.core.data.FeedRepository
import sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto
import sg.mesha.goatos.core.network.dto.FeedFilterOptionsDto
import sg.mesha.goatos.feature.feed.FeedDirectionEvent
import sg.mesha.goatos.feature.feed.FeedDirectionRowUi
import sg.mesha.goatos.feature.feed.FeedDirectionSummaryUi
import sg.mesha.goatos.feature.feed.FeedDirectionUiState
import sg.mesha.goatos.feature.feed.FeedDropdownOption
import sg.mesha.goatos.feature.feed.FeedFilterUi
import sg.mesha.goatos.feature.feed.FeedItemQtyUi
import sg.mesha.goatos.feature.feed.FeedItemTotalUi
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/**
 * Feed Direction (generated sheet) read-screen state holder — the offline-first pattern
 * (docs/decisions/android-offline-first.md). Room is the UI's single source of truth:
 * [FeedRepository.observeDirectionTotals] emits the whole-scope summary + backend-owned filter
 * vocabulary from Room and re-emits when a background refresh upserts; [rows] is a Paging 3 flow
 * whose RemoteMediator fills Room page-by-page.
 *
 * KPI integrity: the totals here are the backend's whole-scope summary, NEVER re-summed from the
 * ~20 rows currently paged in. Filter vocabulary (farm/shed) is backend-owned too — the screen
 * holds no park/shed list of its own.
 */
@HiltViewModel
class FeedDirectionViewModel @Inject constructor(
    private val repo: FeedRepository,
    private val feedCompletionStore: FeedCompletionLocalStore,
    private val submittedGrains: SubmittedGrainsSource,
    private val bootstrapRepository: BootstrapRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _filters = MutableStateFlow(FeedDirectionSelection())
    private val _canExecuteDirection = MutableStateFlow(false)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val observed: StateFlow<FeedDirectionEnvelope> = _filters
        .flatMapLatest { selection -> repo.observeDirectionTotals(selection.toQuery()) }
        .scan(FeedDirectionEnvelope()) { carried, resource ->
            val fresh = resource.data?.filters
            // Carry the last usable filter vocabulary forward: a newly-selected scope's cache is
            // cold and arrives with empty parks, and emptying the dropdowns the instant a filter is
            // applied would strand the operator. Parks are the whole-tenant vocabulary, not
            // narrowed by the filter, so the previous scope's list is the same list.
            FeedDirectionEnvelope(
                resource = resource,
                filters = if (fresh != null && fresh.parks.isNotEmpty()) fresh else carried.filters,
            )
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FeedDirectionEnvelope())

    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    /** The single in-flight refresh monitor; a new tap cancels the previous one so stale
     *  collectors can never clear the spinner or flash isOffline out of order. */
    private var refreshMonitorJob: Job? = null

    val state: StateFlow<FeedDirectionUiState> = combine(
        observed,
        _filters,
        _canExecuteDirection,
        _isRefreshing,
        _isOffline,
    ) { envelope, selection, canExecuteDirection, isRefreshing, isOffline ->
        val dto = envelope.resource.data
        val hasSummary = dto != null
        FeedDirectionUiState(
            title = TITLE,
            targetDateLabel = selection.targetDate,
            today = todayIso(),
            canCapture = canExecuteDirection && selection.targetDate == todayIso(),
            filters = envelope.filters.toFilterUi(selection),
            summary = dto?.toSummaryUi() ?: FeedDirectionSummaryUi(),
            hasSummary = hasSummary,
            emptyMessage = when {
                // Gated day (before normal 07:00 / experiment 14:00): no rows is the intended state,
                // and the backend sentence says when the sheet arrives. See FeedPackingViewModel.
                hasSummary && dto.summary.rowCount == 0 ->
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
        FeedDirectionUiState(
            title = TITLE,
            emptyMessage = LOADING_MESSAGE,
            targetDateLabel = todayIso(),
            today = todayIso(),
            canCapture = true,
        ),
    )

    // Rows combine the paged Room window with the optimistic local-completion set, so a shed-session
    // the operator just marked done shows completed immediately (offline-first) and converges on the
    // backend `completed` flag once the write syncs. A change to either re-subscribes the pager.
    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<FeedDirectionRowUi>> =
        combine(
            _filters,
            feedCompletionStore.completedKeys,
            // Outbox-derived: the badge retracts by itself when the row succeeds or dies.
            submittedGrains.observe(),
        ) { selection, completed, submitted -> Triple(selection, completed, submitted) }
            .flatMapLatest { (selection, completed, submitted) ->
                val targetDate = selection.toQuery().targetDate
                repo.directionRows(selection.toQuery())
                    .map { page -> page.map { it.toRowUi(completed, submitted, targetDate) } }
            }
            .cachedIn(viewModelScope)

    init {
        analytics.track(AnalyticsEvents.FEED_DIRECTION_VIEWED)
        viewModelScope.launch {
            _canExecuteDirection.value = runCatching {
                bootstrapRepository.operatorProfile()?.primaryRoleHint == ROLE_OPERATOR
            }.onFailure {
                crashReporter.recordException(it, "feed direction execute-role bootstrap failed")
            }.getOrDefault(false)
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "feed direction page load failed")
        analytics.track(
            AnalyticsEvents.FEED_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "direction",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onEvent(event: FeedDirectionEvent) {
        when (event) {
            FeedDirectionEvent.Refresh -> refresh()
            is FeedDirectionEvent.SelectPark -> selectPark(event.parkId)
            is FeedDirectionEvent.SelectShed -> selectShed(event.shedId)
            is FeedDirectionEvent.SelectWorkflow -> selectWorkflow(event.workflow)
            is FeedDirectionEvent.SelectSession -> selectSession(event.sessionNo)
            is FeedDirectionEvent.SelectStatus -> selectStatus(event.status)
            is FeedDirectionEvent.SelectDate -> selectDate(event.date)
            is FeedDirectionEvent.OpenRow -> analytics.track(
                AnalyticsEvents.FEED_ROW_TAPPED,
                mapOf(
                    AnalyticsEvents.Params.KIND to KIND_DIRECTION,
                    AnalyticsEvents.Params.SHED_ID to event.shedId,
                    AnalyticsEvents.Params.SESSION_NO to event.sessionNo.toString(),
                ),
            )
            is FeedDirectionEvent.AlreadySubmittedRow -> analytics.track(
                AnalyticsEvents.FEED_DISTRIBUTION_REOPEN_BLOCKED,
                mapOf(
                    AnalyticsEvents.Params.SHED_ID to event.shedId,
                    AnalyticsEvents.Params.SESSION_NO to event.sessionNo.toString(),
                ),
            )
            FeedDirectionEvent.ClearFilters -> clearFilters()
        }
    }

    // The paged rows revalidate through the RemoteMediator; Refresh just resets the transient
    // offline flag and lets the observed summary re-request via a fresh subscription.
    private fun refresh() {
        _isRefreshing.value = true
        _isOffline.value = false
        val priorSyncedAt = observed.value.resource.lastSyncedAt ?: 0L
        // Re-emit the current selection so both the summary observe and the pager re-subscribe.
        // A NEW value, not an equal one: MutableStateFlow conflates on equality and these
        // selections are data classes, so a bare copy() emitted nothing and flatMapLatest
        // stayed on the same page -- a refresh that silently did not refetch. Same defect
        // fixed in ShiftingPendingViewModel on 2026-08-13.
        _filters.value = _filters.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
        // ONE monitor per refresh; a new tap cancels the previous so overlapping taps can't race
        // each other's spinner/offline writes. Success is judged by an ACTUAL network probe --
        // never by lastSyncedAt movement alone, because a fresh-enough cache legitimately serves
        // without a network hit and a timestamp that stays put then reads as a FALSE offline
        // (field report 2026-08-15: list flashed "syncing" then "offline" with the server up).
        refreshMonitorJob?.cancel()
        refreshMonitorJob = viewModelScope.launch {
            val query = _filters.value.toQuery()
            val reachable = kotlinx.coroutines.withTimeoutOrNull(5_000L) {
                repo.probeDirectionSummary(query)
            } ?: false
            if (reachable) {
                // Give the re-subscribed mediator/summary a beat to land its upsert before the
                // spinner clears, so the "Updated just now" caption reflects the refetch.
                kotlinx.coroutines.withTimeoutOrNull(3_000L) {
                    observed
                        .map { it.resource.lastSyncedAt ?: 0L }
                        .first { it > priorSyncedAt }
                }
            }
            _isRefreshing.value = false
            _isOffline.value = !reachable
        }
    }

    private fun selectPark(parkId: String) {
        val current = _filters.value
        if (current.parkId == parkId) return
        // A shed belongs to exactly one park, so a park change RESETS the shed to avoid filtering to
        // a shed in the park just navigated away from.
        _filters.value = current.copy(parkId = parkId, shedId = "")
        trackFilter(DIMENSION_FARM, parkId)
    }

    private fun selectShed(shedId: String) {
        val current = _filters.value
        if (current.shedId == shedId) return
        _filters.value = current.copy(shedId = shedId)
        trackFilter(DIMENSION_SHED, shedId)
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
        if (current.shedId.isBlank() && current.workflow.isBlank() && current.session == 0 && current.status.isBlank()) return
        // Keep the selected park (it is required scope); clear the narrowing filters.
        _filters.value = current.copy(shedId = "", workflow = "", session = 0, status = "")
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

    private fun FeedFilterOptionsDto.toFilterUi(selection: FeedDirectionSelection): FeedFilterUi {
        val parkOptions = parks.map { FeedDropdownOption(it.parkId, it.label) }
        val shedOptions = sheds.map { FeedDropdownOption(it.shedId, it.label) }
        // Backend-owned session vocabulary; the key is the session_no the backend expects back.
        val sessionOptions = sessions.map { FeedDropdownOption(it.sessionNo.toString(), it.label) }
        // With no explicit park chosen, the active park is the one the backend served (the default),
        // so the dropdown shows the real park rather than a blank.
        val activeParkId = selection.parkId.ifBlank { servedParkId }
        return FeedFilterUi(
            parks = parkOptions,
            selectedParkId = activeParkId,
            selectedParkLabel = parkOptions.firstOrNull { it.key == activeParkId }?.label,
            sheds = shedOptions,
            selectedShedId = selection.shedId,
            selectedShedLabel = shedOptions.firstOrNull { it.key == selection.shedId }?.label,
            workflow = selection.workflow,
            sessions = sessionOptions,
            selectedSessionNo = selection.session,
            selectedSessionLabel = sessionOptions.firstOrNull { it.key == selection.session.toString() }?.label,
            status = selection.status,
        )
    }

    private fun FeedDirectionPreviewPageDto.toSummaryUi(): FeedDirectionSummaryUi = FeedDirectionSummaryUi(
        shedCount = summary.shedCount,
        rowCount = summary.rowCount,
        blockedCount = summary.blockedCount,
        totalsByItem = summary.totalKgByFeedItem.map { FeedItemTotalUi(it.feedItem, it.quantityKg, it.blockedCells) },
    )

    private fun sg.mesha.goatos.core.network.dto.FeedDirectionRowDto.toRowUi(
        locallyCompleted: Set<String>,
        locallySubmittedForReview: Set<String>,
        targetDate: String,
    ): FeedDirectionRowUi = FeedDirectionRowUi(
        grainKey = grainKey,
        parkId = parkId,
        parkLabel = parkLabel,
        shedId = shedId,
        sessionNo = sessionNo,
        // Shed + partition, never the bare shed name: a feed/packing row is one OPERATIONAL
        // LOCATION, so Castro 1 and Castro 2 share a shed_id and would otherwise print as two
        // identical "Castro" lines the operator cannot tell apart.
        shedLabel = operationalLocationLabel(shedLabel, partitionLabel),
        partitionLabel = partitionLabel.orEmpty(),
        shedTag = shedTag,
        breed = breed,
        rationGroup = rationGroup,
        experimentArm = experimentArm,
        sessionLabel = sessionLabel,
        headCount = headCount,
        headCountInformational = headCountInformational,
        workflow = workflow,
        items = items.map { FeedItemQtyUi(it.feedItem, it.quantityKg, it.isBlocked, it.blockedReason?.detail.orEmpty()) },
        sessionTotalKg = sessionTotalKg,
        blocked = blocked,
        overduePending = overduePending,
        // Backend truth OR the optimistic local overlay for a just-completed shed-session.
        completed = completed || locallyCompleted.contains(FeedCompletionLocalStore.key(shedId, partitionLabel, sessionNo, workflow)),
        // The CHIP renders lifecycleStatus, not `completed` — overlaying only the boolean above left
        // a just-submitted row reading "Pending" (the 254.mp4 defect, same class as Feed Packing).
        // Direction/Distribution rows carry no rework channel, so that rung passes "".
        lifecycleStatus = overlayVerificationStatus(
            backendStatus = lifecycleStatus,
            // Direction/Distribution rows carry no rework channel.
            reworkReason = "",
            isLocallySubmitted = locallySubmittedForReview.contains(
                // Tapping a Feed Direction row opens the DISTRIBUTION capture, which enqueues
                // FEED_DISTRIBUTION_COMPLETE *with this row's partitionLabel* (see
                // FeedDistributionCompleteViewModel). The lookup MUST use the same partition or the
                // keys never match on a partitioned shed — Castro 1 and Castro 2 share a shed_id —
                // and the badge silently never appears: 254.mp4, reopened.
                submittedGrainKey(targetDate),
            ),
            inReviewToken = IN_REVIEW_PENDING_VERIFICATION,
        ),
    )

    private data class FeedDirectionSelection(
        val parkId: String = "",
        val shedId: String = "",
        val workflow: String = "",
        // 0 = every session (unfiltered); a positive value is a backend session_no.
        val session: Int = 0,
        // "" = every status; else a backend verification-lifecycle bucket
        // (pending | pending_verification | completed).
        val status: String = "",
        // The feed day. Feed is generated for exactly one Asia/Kolkata business day; today is the
        // default the operator dispatches against. Reactive (not a fixed val) so the date bar can
        // step it to a past day and re-query, same as every other filter here.
        val targetDate: String = LocalDate.now(ZoneId.of(INDIA_ZONE)).toString(),
        /** Bumped by refresh so an unchanged selection is still a NEW value. */
        val refreshNonce: Int = 0,
    ) {
        fun toQuery(): FeedDirectionQuery = FeedDirectionQuery(
            parkId = parkId,
            targetDate = targetDate,
            shedId = shedId.takeIf { it.isNotBlank() },
            session = session.takeIf { it != 0 },
            workflow = workflow.takeIf { it.isNotBlank() },
            status = status.takeIf { it.isNotBlank() },
            refreshNonce = refreshNonce,
        )
    }

    private data class FeedDirectionEnvelope(
        val resource: Resource<FeedDirectionPreviewPageDto> = Resource(data = null),
        val filters: FeedFilterOptionsDto = FeedFilterOptionsDto(),
    )

    private companion object {
        const val INDIA_ZONE = "Asia/Kolkata"
        const val KIND_DIRECTION = "direction"
        const val ROLE_OPERATOR = "operator"
        const val TITLE = "Feed Direction"
        const val LOADING_MESSAGE = "Loading feed sheet…"
        const val EMPTY_MESSAGE = "No feed rows for this farm and day"
        const val ERROR_MESSAGE = "Couldn't load the feed sheet. Tap refresh to retry."
        const val DIMENSION_FARM = "farm"
        const val DIMENSION_SHED = "shed"
        const val DIMENSION_WORKFLOW = "workflow"
        const val DIMENSION_SESSION = "session"
        const val DIMENSION_STATUS = "status"
        const val DIMENSION_DATE = "date"
        const val DIMENSION_ALL = "all"
        const val ACTION_SET = "set"
        const val ACTION_CLEARED = "cleared"
    }
}
