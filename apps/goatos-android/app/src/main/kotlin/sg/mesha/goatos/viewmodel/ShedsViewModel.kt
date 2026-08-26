package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.ExecutionParkOptionDto
import sg.mesha.goatos.core.network.dto.ShedCardSummaryDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
import sg.mesha.goatos.core.ui.operationalLocationLabel
import sg.mesha.goatos.feature.sheds.ShedDayTab
import sg.mesha.goatos.feature.sheds.CarryVaccine
import sg.mesha.goatos.feature.sheds.DayCarry
import sg.mesha.goatos.feature.sheds.ProtocolAdherenceSummary
import sg.mesha.goatos.feature.sheds.ShedParkFilter
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedStatusChip
import sg.mesha.goatos.feature.sheds.ShedStatusChipKey
import sg.mesha.goatos.feature.sheds.ShedStatusTone
import sg.mesha.goatos.feature.sheds.ShedsEvent
import sg.mesha.goatos.feature.sheds.ShedsUiState
import sg.mesha.goatos.feature.sheds.VaccineGroup
import sg.mesha.goatos.ui.sampleShedsState
import sg.mesha.goatos.ui.shedsPlaceholder
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.util.Locale
import javax.inject.Inject

private const val PAGE_LIMIT = 20
private const val OPERATOR_WINDOW_DAYS = 7
private const val OPEN_ONLY_QUERY = false
private const val CALENDAR_PARK_ARG = "parkId"
private val KOLKATA: ZoneId = ZoneId.of("Asia/Kolkata")

/**
 * Today's-sheds / drive-status state holder — the offline-first pattern for the sheds
 * read screen (docs/decisions/android-offline-first.md). Room is the UI's single source of
 * truth: [state] is fed by [ExecutionRepository.observeRows], a cache-first [Flow]
 * that emits instantly from Room (cached data survives process restarts and screen
 * re-entry) and re-emits the moment a background [ExecutionRepository.refreshRows] upserts
 * new data. [refresh] never writes into [state] directly — it only drives the network call
 * and the transient [ShedsUiState.isRefreshing]/[ShedsUiState.isOffline] flags; the
 * DTO -> UiState mapping in [toShedsUiState] is unchanged from the network-only version.
 * An empty real response shows an honest empty state; a refresh failure with NO cache ever
 * observed shows an honest error state; a refresh failure WITH cached data keeps rendering
 * that cache and only flips [ShedsUiState.isOffline] — never a blank/loading wall.
 * [ShedsEvent.OpenShedRecord] is navigation, routed by the nav host.
 */
@HiltViewModel
class ShedsViewModel @Inject constructor(
    private val repo: ExecutionRepository,
    private val crashReporter: CrashReporter,
    private val analytics: AnalyticsPort,
    private val bootstrapRepository: BootstrapRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private var nextCursor: String? = null
    private val workWindow = OperatorWorkWindow.today()
    private val calendarHosted: Boolean = savedStateHandle.get<String>("calendarHosted") == "true"
    private val initialDay: LocalDate =
        savedStateHandle.get<String>("dateKey")
            ?.let(::parseExecutionDate)
            ?.takeIf { it >= workWindow.firstDay && it <= workWindow.lastDay }
            ?: workWindow.today
    private val initialParkId: String? = savedStateHandle.get<String>(CALENDAR_PARK_ARG)?.takeIf { it.isNotBlank() }
    private val _selectedDay = MutableStateFlow(initialDay)
    // Seeded from the drive card's parkId nav arg (CALENDAR_PARK_ARG) so drilling into a
    // specific park's drive card scopes the shed list to that park from the first load.
    // Previously this always started at null, so the drill silently rendered every park's
    // sheds (the API is correct — it returns the full cross-park set by design; scoping is
    // the client's job) regardless of which park's card was tapped, for every role that uses
    // this shared route (CEO, Director, Park Head, operator).
    private val _selectedParkId = MutableStateFlow<String?>(initialParkId)
    private val _leadershipMode = MutableStateFlow(false)

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedResource: StateFlow<Resource<VaccinationExecutionResponseDto>> =
        _selectedParkId.flatMapLatest { parkId ->
            repo.observeRows(
                parkId = parkId,
                asOf = workWindow.asOf,
                dueBefore = workWindow.dueBefore,
                openOnly = OPEN_ONLY_QUERY,
                limit = PAGE_LIMIT,
                includeFilterOptions = true,
            )
        }.stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)
    // Set the first time a fetch COMPLETES, whatever it returned. lastSyncedAt cannot serve this
    // -- an empty result never sets it -- and isRefreshing flips on every later refresh, so both
    // made the screen either wedge on a spinner or yank already-drawn content away mid-refresh.
    private val _hasLoadedOnce = MutableStateFlow(false)
    private val transientState = combine(
        _selectedDay,
        combine(_isRefreshing, _isOffline, _isLoadingMore, _leadershipMode, _hasLoadedOnce) { r, o, l, m, h ->
            TransientFlags(r, o, l, m, h)
        }
    ) { selectedDay, flags ->
        ShedsTransientState(selectedDay, flags.isRefreshing, flags.isOffline, flags.isLoadingMore, flags.leadershipMode, flags.hasLoadedOnce)
    }

    // Combines observed resource with transient flags; lifecycle-aware.
    //
    // _selectedParkId is a FIRST-CLASS SOURCE here, not a `.value` read inside the block. Read
    // imperatively it was invisible to the combine, so the state only rebuilt when
    // observedResource emitted -- and observedResource is a StateFlow, which conflates equal
    // values. Clearing the park back to "all parks" re-queried and got back the SAME rows, the
    // StateFlow suppressed the duplicate emission, the combine never re-ran, and the UI kept
    // showing the park the user had just cleared. The widen-back control looked dead. State that
    // depends on a value must observe that value.
    val state: StateFlow<ShedsUiState> = combine(
        observedResource,
        transientState,
        _selectedParkId,
    ) { resource, transient, selectedParkId ->
        val dto = resource.data
        nextCursor = dto?.nextCursor  // Update pagination cursor for loadMore()
        val isInitialLoading = dto == null && !resource.hasData && resource.error == null && !transient.isOffline
        val effectiveSelectedDay = dto?.effectiveSelectedDay(transient.selectedDay) ?: transient.selectedDay
        val base = dto?.toShedsUiState(effectiveSelectedDay)
            ?: run {
                val message = when {
                    resource.hasData -> if (effectiveSelectedDay == workWindow.today) {
                        "No sheds scheduled today"
                    } else {
                        "No sheds scheduled for ${shortDateLabel(effectiveSelectedDay)}"
                    }
                    transient.isOffline -> "Couldn't load vaccination drives. Pull to refresh or try again."
                    else -> "Loading…"
                }
                emptyShedsState(
                    message = message,
                    window = workWindow,
                    selectedDay = effectiveSelectedDay,
                    readOnly = calendarHosted,
                    hostedFromCalendar = calendarHosted,
                )
            }
        base.copy(
            hostedFromCalendar = calendarHosted,
            leadershipMode = transient.leadershipMode,
            // Live selection, not the nav-arg seed: selectPark() updates _selectedParkId (which
            // re-triggers observedResource via flatMapLatest), so this must track the same value
            // or the "pinned park" chip / widen-to-all-parks affordance would freeze on the park
            // the user drilled in from, even after they clear it back to all parks.
            selectedParkId = selectedParkId,
            isRefreshing = transient.isRefreshing,
            isInitialLoading = isInitialLoading,
            isLoadingMore = transient.isLoadingMore,
            hasMore = !dto?.nextCursor.isNullOrBlank(),
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = transient.isOffline,
            hasLoadedOnce = transient.hasLoadedOnce,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        shedsPlaceholder("Loading today's sheds…").copy(isInitialLoading = true)
    )

    init {
        loadLeadershipMode()
        refresh()
        trackEmptyRoster()
        trackScreenViewed()
    }

    /**
     * Fires ONCE, the first time [state] leaves the initial-loading moment — the screen-view
     * signal that was entirely missing before: a CEO/CXO/Director landing on this shared
     * leadership-oversight + operator-worklist screen emitted nothing beyond `bootstrap_loaded`,
     * so "did the CEO ever open the Vaccination Overview" was unanswerable from analytics.
     */
    private var screenViewedTracked = false

    private fun trackScreenViewed() {
        viewModelScope.launch {
            combine(state, _leadershipMode) { uiState, leadershipMode ->
                !uiState.isInitialLoading to leadershipMode
            }
                .distinctUntilChanged()
                .collect { (readyToRender, leadershipMode) ->
                    if (readyToRender && !screenViewedTracked) {
                        screenViewedTracked = true
                        analytics.track(
                            AnalyticsEvents.VACCINATION_SHEDS_VIEWED,
                            mapOf(
                                AnalyticsEvents.Params.KIND to
                                    if (leadershipMode) "leadership" else "operator",
                            ),
                        )
                    }
                }
        }
    }

    /**
     * Answers "did this operator ever land on an empty shed list" — before this, an empty roster
     * (no sheds assigned, or a drive-free day) rendered a silent empty-state card with nothing in
     * telemetry to distinguish it from a still-loading or offline screen.
     */
    private fun trackEmptyRoster() {
        viewModelScope.launch {
            state
                .map { it.rows.isEmpty() && !it.isInitialLoading && !it.isOffline }
                .distinctUntilChanged()
                .collect { isEmpty ->
                    if (isEmpty) {
                        analytics.track(
                            AnalyticsEvents.SHEDS_EMPTY_ROSTER,
                            mapOf(AnalyticsEvents.Params.KIND to if (calendarHosted) "calendar_drive" else "vaccination"),
                        )
                    }
                }
        }
    }

    private fun loadLeadershipMode() {
        viewModelScope.launch {
            val role = runCatching { bootstrapRepository.operatorProfile()?.primaryRoleHint }.getOrNull()
            _leadershipMode.value = role.isLeadershipShedsRole()
        }
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observeRows]
     *  collector above re-emits and updates [state]); on failure it only flips
     *  [ShedsUiState.isOffline] — cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        // NOTE: hasLoadedOnce is reset at the SCOPE CHANGE call site (selectPark, below), not
        // here -- see VerifyQueueViewModel's onEvent handlers for the same fix and rationale
        // (a reset done only inside refresh() left a window where a recomposition could show
        // the OLD park's confident answer as if it belonged to the new one). `day` is NOT part
        // of the network scope: selectDay never calls refresh() -- the fetched window already
        // spans the whole 7-day strip and a day switch only re-filters that same cached data
        // client-side, so there is no new read for a day change to reset a marker in front of.
        try {
            // INSIDE the try: a throw from analytics here would skip the finally that sets
            // hasLoadedOnce, leaving the screen permanently blank with a frozen spinner.
            analytics.track(AnalyticsEvents.VACCINATION_REFRESH_ATTEMPTED)
            val result = repo.refreshRows(
                parkId = _selectedParkId.value,
                asOf = workWindow.asOf,
                dueBefore = workWindow.dueBefore,
                openOnly = OPEN_ONLY_QUERY,
                limit = PAGE_LIMIT,
                includeFilterOptions = true,
            )
            _isOffline.value = result.isFailure
            if (result.isSuccess) {
                analytics.track(AnalyticsEvents.VACCINATION_REFRESH_SUCCEEDED)
            }
            result.exceptionOrNull()?.let {
                analytics.track(
                    AnalyticsEvents.VACCINATION_REFRESH_FAILED,
                    mapOf(AnalyticsEvents.Params.REASON to (it::class.simpleName ?: "unknown")),
                )
                crashReporter.recordException(it, "vaccination sheds refresh failed")
            }
        } catch (t: Throwable) {
                    // A cancelled scope is not a failure. Catching Throwable without letting
                    // CancellationException through breaks structured concurrency: rotating the
                    // screen or navigating away would be reported as an error and would publish
                    // state after the scope had already been cancelled.
                    if (t is kotlinx.coroutines.CancellationException) throw t
            // A repository throw must not escape viewModelScope.launch and crash the app --
            // same defect shape fixed in SessionViewModel's dev-session bring-up and in
            // VerifyQueueViewModel.refresh() (see the catch there). Record it and resolve to
            // an honest offline state instead of propagating; hasLoadedOnce still flips in
            // `finally` below so the screen never wedges on the skeleton.
            runCatching { crashReporter.recordException(t, "vaccination sheds refresh failed") }
            _isOffline.value = true
        } finally {
            _isRefreshing.value = false
            // In FINALLY, not after the result: a throw on the way here would otherwise leave the
            // flag false forever and wedge the screen on a spinner over a blank list.
            _hasLoadedOnce.value = true
        }
    }

    fun loadMore() = viewModelScope.launch {
        val cursor = nextCursor ?: return@launch
        if (_isLoadingMore.value) return@launch
        _isLoadingMore.value = true
        analytics.track(AnalyticsEvents.VACCINATION_LOAD_MORE_ATTEMPTED)
        val result = repo.appendRows(
            cursor = cursor,
            parkId = _selectedParkId.value,
            asOf = workWindow.asOf,
            dueBefore = workWindow.dueBefore,
            openOnly = OPEN_ONLY_QUERY,
            limit = PAGE_LIMIT,
            includeFilterOptions = true,
        )
        _isLoadingMore.value = false
        _isOffline.value = result.isFailure
        if (result.isSuccess) {
            analytics.track(AnalyticsEvents.VACCINATION_LOAD_MORE_SUCCEEDED)
        }
        result.exceptionOrNull()?.let {
            analytics.track(
                AnalyticsEvents.VACCINATION_LOAD_MORE_FAILED,
                mapOf(AnalyticsEvents.Params.REASON to (it::class.simpleName ?: "unknown")),
            )
            crashReporter.recordException(it, "vaccination sheds append failed")
        }
    }

    fun onEvent(event: ShedsEvent) {
        when (event) {
            ShedsEvent.Refresh -> refresh()
            ShedsEvent.LoadMore -> loadMore()
            is ShedsEvent.SelectDay -> selectDay(event.dateKey)
            is ShedsEvent.SelectPark -> selectPark(event.parkId)
            is ShedsEvent.OpenShedRecord -> Unit // navigation — handled by the nav host.
            ShedsEvent.Back -> Unit // navigation — handled by the nav host.
            is ShedsEvent.OpenBlocked -> trackOpenBlocked(event)
        }
    }

    /**
     * Answers "was this tap actually blocked, and why" — before this, every one of these gates
     * (no permission, shed owned by another operator, scheduled for a future day, already
     * submitted) only ever showed a Toast: the operator saw a dead end and nothing recorded which
     * gate it was.
     */
    private fun trackOpenBlocked(event: ShedsEvent.OpenBlocked) {
        analytics.track(
            AnalyticsEvents.VACCINATION_OPEN_BLOCKED,
            buildMap {
                put(AnalyticsEvents.Params.REASON, event.reason)
                event.shedId?.let { put(AnalyticsEvents.Params.SHED_ID, it) }
            },
        )
    }

    private fun selectDay(dateKey: String) {
        val date = parseExecutionDate(dateKey) ?: return
        // The strip runs firstDay (yesterday) .. lastDay (today+5); every rendered tab,
        // including yesterday, must be selectable.
        if (date < workWindow.firstDay || date > workWindow.lastDay) return
        _selectedDay.value = date
        analytics.track(
            AnalyticsEvents.VACCINATION_DAY_SELECTED,
            mapOf(AnalyticsEvents.Params.DIMENSION to "day"),
        )
    }

    private fun selectPark(parkId: String?) {
        val normalized = parkId?.takeIf { it.isNotBlank() }
        if (_selectedParkId.value == normalized) return
        nextCursor = null
        _selectedParkId.value = normalized
        analytics.track(
            AnalyticsEvents.VACCINATION_PARK_FILTER_APPLIED,
            mapOf(
                AnalyticsEvents.Params.DIMENSION to "park",
                AnalyticsEvents.Params.ACTION to if (normalized != null) "set" else "cleared",
            ),
        )
        // Reset HERE, at the scope change, not inside refresh() -- see refresh()'s NOTE above.
        _hasLoadedOnce.value = false
        refresh()
    }

    private fun VaccinationExecutionResponseDto.toShedsUiState(selectedDay: LocalDate): ShedsUiState? {
        val base = sampleShedsState()
        val weekRows = rows
        val rowsForSelectedDay = weekRows.filter { row ->
            row.isVisibleForOperatorDay(selectedDay, workWindow)
        }
        // Group by the exact key rendered by Compose. Do not group by metadata that is not also
        // present in ShedRow.id: the same shed/partition/task can arrive as multiple backend rows
        // (for example BT + SP rows, or a task row version/sop version split) but must remain one
        // UI card with one LazyColumn key.
        // Prefer backend-computed card summaries (page-independent) over row-level folds.
        val cardSummaries = cardSummaries
        val shedRows = rowsForSelectedDay.groupBy { it.executionCardId() }.map { (cardId, group) ->
            val first = group.first()
            val scheduleDate = group.mapNotNull { it.currentScheduleDate?.let(::parseExecutionDate) }.minOrNull()

            // Prefer backend-computed card summary (page-independent, covers all rows for the card).
            // Fall back to row-level computation for older API responses without cardSummaries.
            val cardSummary = cardSummaries?.get(cardId)
            val status: ShedStatus
            val counts: ExecutionCounts
            val vaccineGroups: List<VaccineGroup>
            val effectiveDone: Int

            if (cardSummary != null) {
                // Use backend summary: it's authoritative and page-independent
                status = cardSummaryStatusToShedStatus(cardSummary.status, cardSummary.needsRedo)
                counts = ExecutionCounts(
                    target = cardSummary.targetCount,
                    open = cardSummary.openCount,
                    done = cardSummary.doneCount,
                )
                effectiveDone = cardSummary.doneCount
                vaccineGroups = cardSummary.vaccineGroups.map { summary ->
                    VaccineGroup(
                        label = summary.label,
                        countLabel = "", // Backend summary doesn't include per-vaccine counts; client computes if needed
                        full = summary.full,
                    )
                }
            } else {
                // Fall back to row-level computation for backward compat
                status = shedStatusForRows(group)
                counts = executionCardCounts(group)
                effectiveDone = effectiveCardDoneCount(group)
                vaccineGroups = group.flatMap { row ->
                    row.vaccineLabels.ifEmpty { listOfNotNull(row.driveName) }
                        .map { humanizeVaccineLabel(it) }
                        .filter { it.isNotBlank() }
                        .map { label -> label to row }
                }
                    .groupBy({ it.first }, { it.second })
                    .map { (label, driveRows) ->
                        val driveCounts = executionCardCounts(driveRows)
                        val driveDone = effectiveCardDoneCount(driveRows)
                        VaccineGroup(
                            label = label,
                            countLabel = "$driveDone/${driveCounts.target}",
                            full = driveCounts.open == 0 && driveRows.none { it.needsRedo() },
                        )
                    }
            }
            ShedRow(
                id = cardId,
                // The shed CARD TITLE. It must carry the backend-composed operational location,
                // or a partitioned shed shows its bare name and every partition of that shed
                // reads identically on the operator's list ("Mandela 2" three times instead of
                // "Mandela 2 - Part 3"). Falls back to shedName for older API responses.
                name = first.operationalLocationDisplay.ifBlank { operationalLocationLabel(first.shedName, first.partitionLabel ?: first.partition) },
                parkId = first.parkId,
                parkName = first.parkName,
                operatorName = first.owner?.operatorName.orEmpty(),
                physicalShed = first.physicalShed.ifBlank { first.shedName },
                partition = first.partition,
                partitionLabel = first.partitionLabel
                    ?: first.partition.takeIf { executionPartitionKey(it) != "whole" },
                // animalStage is a biological stage supplied by the execution contract.
                // A drive label is not a cohort/stage and must not be substituted here.
                animalStage = first.animalStage,
                scheduleDateKey = scheduleDate?.toString().orEmpty(),
                scheduleDateLabel = scheduleDate?.let(::shortDateLabel).orEmpty(),
                status = status,
                statusLabel = group.reviewAwareStatusLabel(status),
                statusChips = group.statusChips(status),
                vaccineGroups = vaccineGroups,
                inShed = counts.target.toString(),
                due = counts.open.toString(),
                done = effectiveDone.toString(),
                // Verifier-side count, kept separate from `done` (see effectiveDoneCount's
                // doc) so a card never reads "5 DONE" while only 2 have actually cleared review.
                accepted = group.maxOfOrNull { it.acceptedAnimalCount() }?.coerceAtLeast(0).orZero().toString(),
                progressLabel = percentLabel(effectiveDone, counts.target),
                progressFraction = redoAwareFraction(
                    effectiveDone,
                    counts.target,
                    needsRedo = group.any { it.needsRedo() },
                ),
                shedId = first.shedId,
                driveId = first.driveId,
                batchId = first.batchId,
                taskId = first.sopTaskId,
                sopVersionId = first.sopVersionId,
                taskRowVersion = first.sopTaskRowVersion,
                opensRecordOnly = group.opensSubmittedRecordOnly(),
                canOpen = scheduleDate == null || !scheduleDate.isAfter(workWindow.today),
            )
        }.sortedWith(
            compareBy<ShedRow> { row ->
                row.scheduleDateKey.takeIf { it.isNotBlank() }?.let(::parseExecutionDate) ?: LocalDate.MAX
            }
                .thenBy { it.name.lowercase() }
        )
        val totals = executionCounts(rowsForSelectedDay)
        val totalsEffectiveDone = effectiveDoneCount(rowsForSelectedDay)
        val pageComplete = nextCursor.isNullOrBlank()
        // Backend-owned "vaccines to carry" for the selected day (full-day, page-independent).
        // The screen renders these numbers verbatim — no client-side summing of shed rows.
        val selectedKey = selectedDay.toString()
        val operationalLocationCount = rowsForSelectedDay
            .map { it.shedId to executionPartitionKey(it.partitionLabel ?: it.partition) }
            .distinct()
            .size
        val carry = carrySummary?.carryByDay?.firstOrNull { it.date == selectedKey }?.let { day ->
            DayCarry(
                totalRemaining = day.totalRemaining,
                vaccines = day.vaccineBreakdown
                    .filter { it.remainingDoses > 0 }
                    .map { CarryVaccine(label = it.vaccineLabel, remaining = it.remainingDoses) },
            )
        }
        return base.copy(
            title = "Next 7 days",
            // Vaccination operators do not need the executive Calendar's week/month/history
            // drive cards. This screen is their shed-first work queue: today is the landing
            // anchor, and the backend query is scoped to today → today+1 week.
            scopeLabel = "",
            date = if (selectedDay == workWindow.today) "Today · ${shortDateLabel(selectedDay)}" else shortDateLabel(selectedDay),
            window = workWindow.windowLabel,
            // Raw counts — the screen formats + localizes these via *_fmt resources
            // (counts are UI chrome, not backend-owned copy). The label strings below
            // are kept only as a fallback for non-VM sources (placeholder/sample).
            shedCount = operationalLocationCount,
            dueCount = if (pageComplete) totals.open else 0,
            doneCount = if (pageComplete) totalsEffectiveDone else 0,
            shedCountLabel = "$operationalLocationCount sheds",
            dueLabel = if (pageComplete) "${totals.open} open" else "More rows available",
            dayProgressLabel = if (pageComplete) percentLabel(totalsEffectiveDone, totals.target) else "",
            dayProgressFraction = if (pageComplete) fraction(totalsEffectiveDone, totals.target) else 0f,
            daySummary = if (pageComplete) "$totalsEffectiveDone / ${totals.target} done" else "Load all rows for full-day totals",
            caption = if (shedRows.isEmpty()) {
                if (selectedDay == workWindow.today) {
                    "No sheds scheduled today"
                } else {
                    "No sheds scheduled for ${shortDateLabel(selectedDay)}"
                }
            } else {
                null
            },
            roleNote = null,
            adherence = protocolAdherenceSummary(rowsForSelectedDay, totals, isComplete = pageComplete),
            dayTabs = buildOperatorDayTabs(weekRows, workWindow, selectedDay),
            taskListOnly = true,
            parkFilters = filterOptions?.parks.orEmpty().toShedParkFilters(_selectedParkId.value),
            rows = shedRows,
            hostedFromCalendar = calendarHosted,
            // Leadership oversight read: shed list is read-only, opening into the scan/execute
            // loop is blocked (backend-owned; operators get viewerReadOnly=false).
            canOpenShed = !viewerReadOnly,
            carry = carry,
            rosterChanges = emptyList(),
            kernelInfo = null,
        )
    }

    private fun VaccinationExecutionResponseDto.effectiveSelectedDay(selectedDay: LocalDate): LocalDate {
        if (!calendarHosted || selectedDay != workWindow.today) return selectedDay
        val hasRowsToday = rows.any { row ->
            row.openCount > 0 && row.dueDate?.let(::parseExecutionDate)?.let { due ->
                !due.isAfter(workWindow.today)
            } == true
        }
        if (hasRowsToday) return selectedDay
        return rows.asSequence()
            .filter { it.openCount > 0 }
            .mapNotNull { it.dueDate?.let(::parseExecutionDate) }
            .filter { it >= workWindow.today && it <= workWindow.lastDay }
            .minOrNull()
            ?: selectedDay
    }

    private fun VaccinationExecutionRowDto.isDone(): Boolean {
        val work = workState.lowercase()
        return work.contains("completed") || work.contains("done")
    }

    private fun percentLabel(done: Int, total: Int): String =
        if (total > 0) "${done * 100 / total}%" else "0%"

    private fun fraction(done: Int, total: Int): Float =
        if (total > 0) done.toFloat() / total else 0f

    /**
     * A shed with work sent back must NEVER render a full bar, whatever the counts say.
     *
     * The honest count comes from the backend (`done=4 open=1` when one of five was rejected),
     * and that already lands short of 100%. This is the second line of defence: if the backend
     * ever reports done==target while a row is still flagged for redo, the operator would see a
     * complete shed with outstanding work inside it -- the exact defect that made a rejected
     * shed read as finished. Capping keeps the number honest AND the bar honest.
     */
    private fun redoAwareFraction(done: Int, total: Int, needsRedo: Boolean): Float {
        val raw = fraction(done, total)
        return if (needsRedo) raw.coerceAtMost(MAX_INCOMPLETE_FRACTION) else raw
    }
}

private const val MAX_INCOMPLETE_FRACTION = 0.99f

internal data class ExecutionCounts(val target: Int, val open: Int, val done: Int)

private data class TransientFlags(
    val isRefreshing: Boolean,
    val isOffline: Boolean,
    val isLoadingMore: Boolean,
    val leadershipMode: Boolean,
    val hasLoadedOnce: Boolean,
)

private data class ShedsTransientState(
    val selectedDay: LocalDate,
    val isRefreshing: Boolean,
    val isOffline: Boolean,
    val isLoadingMore: Boolean,
    val leadershipMode: Boolean,
    val hasLoadedOnce: Boolean = false,
)

internal fun String?.isLeadershipShedsRole(): Boolean {
    val normalized = this?.lowercase(Locale.US)?.replace('-', '_') ?: return false
    return normalized == "ceo" ||
        normalized == "cxo" ||
        normalized == "director" ||
        normalized == "pc_director" ||
        normalized.endsWith("_director")
}

internal fun protocolAdherenceSummary(counts: ExecutionCounts): ProtocolAdherenceSummary? =
    protocolAdherenceSummary(emptyList(), counts)

internal fun protocolAdherenceSummary(
    rows: List<VaccinationExecutionRowDto>,
    counts: ExecutionCounts = executionCounts(rows),
    isComplete: Boolean = true,
): ProtocolAdherenceSummary? {
    if (counts.target <= 0 && counts.done <= 0 && counts.open <= 0) return null
    val accepted = rows.sumOf { row -> row.acceptedAnimalCount() }
    val review = rows.count { it.isVerificationPending() }
    return ProtocolAdherenceSummary(
        expectedCount = counts.target,
        submittedCount = counts.done,
        acceptedCount = accepted,
        reviewItemCount = review,
        overdueItemCount = rows.count { it.isOverdueWork() },
        // Animals sent back are counted from the rows that carry a redo state, so a rejection is
        // its own number on the CEO card instead of hiding in the gap between submitted and
        // accepted.
        sentBackCount = rows.filter { it.needsRedo() }.sumOf { it.openCount.coerceAtLeast(0) },
        deferredCount = 0,
        acceptedPercent = if (counts.target > 0) (accepted * 100 / counts.target).coerceIn(0, 100) else 0,
        isComplete = isComplete,
    )
}

/**
 * Convert backend status string + redo state to ShedStatus enum.
 * Backend maps: rejected→SENT_BACK, overdue→DELAYED, completed→DONE, due→PENDING
 */
internal fun cardSummaryStatusToShedStatus(backendStatus: String, needsRedo: Boolean): ShedStatus {
    if (needsRedo) return ShedStatus.SENT_BACK
    return when (backendStatus.lowercase()) {
        "rejected", "deferred" -> ShedStatus.SENT_BACK
        "overdue", "missed", "blocked" -> ShedStatus.DELAYED
        "completed" -> ShedStatus.DONE
        else -> ShedStatus.PENDING
    }
}

internal fun shedStatusForRows(rows: List<VaccinationExecutionRowDto>): ShedStatus {
    // Work the verifier SENT BACK is its own state, checked before the late/blocked family.
    // It used to fall through to PENDING entirely, so a rejected shed rendered as a green
    // "In progress" card with a full bar and the operator could not see there was anything to
    // redo. Folding it into DELAYED fixed the visibility but said "Overdue", which is a
    // different and wrong reason: the work is not late, it came back.
    if (rows.any { it.needsRedo() }) return ShedStatus.SENT_BACK
    val anyDelayed = rows.any { row ->
        val work = row.workState.lowercase()
        work.contains("overdue") ||
            work.contains("missed") ||
            work.contains("blocked") ||
            row.severity.equals("critical", ignoreCase = true)
    }
    if (anyDelayed) return ShedStatus.DELAYED
    val allFinalClosed = rows.isNotEmpty() && rows.all { it.isFinalClosed() }
    return if (allFinalClosed) ShedStatus.DONE else ShedStatus.PENDING
}

/** True when the row's workState means the operator's submitted work was sent back and
 * must be redone (verifier rejection, or the shed/task was deferred out of this window).
 * Backend values: `rejected`, `deferred` (vaccinationexecution/domain/types.go). */
internal fun VaccinationExecutionRowDto.needsRedo(): Boolean {
    val work = workState.lowercase()
    return work.contains("rejected") || work.contains("deferred")
}

/**
 * The backend owns this count. `executionDisplayCounts` already excludes REJECTED completions
 * from done, so a shed where 1 of 5 animals was sent back reports `done=4 open=1` — the four
 * that stand, plus the one to redo.
 *
 * This must NOT re-apply that discount on the device. Zeroing the whole row's doneCount because
 * the row carries a rejection wiped four accepted animals off the operator's card: it read
 * `DONE 0` and an empty bar, telling him to redo the entire shed when only one animal came back.
 * Correcting a backend-owned number twice is how a client and its server end up disagreeing.
 */
internal fun effectiveDoneCount(rows: List<VaccinationExecutionRowDto>): Int =
    rows.sumOf { row -> row.doneCount.coerceAtLeast(0) }

private fun effectiveCardDoneCount(rows: List<VaccinationExecutionRowDto>): Int =
    rows.maxOfOrNull { row -> row.doneCount.coerceAtLeast(0) }.orZero()

/** Execution API rows are aggregated groups. Counts must come from the backend fields, never
 * from List.size (which undercounted a two-goat shed as one because it had one grouped row). */
internal fun executionCounts(rows: List<VaccinationExecutionRowDto>): ExecutionCounts =
    ExecutionCounts(
        target = rows.sumOf { it.targetCount.coerceAtLeast(0) },
        open = rows.sumOf { it.openCount.coerceAtLeast(0) },
        done = rows.sumOf { it.doneCount.coerceAtLeast(0) },
    )

private fun executionCardCounts(rows: List<VaccinationExecutionRowDto>): ExecutionCounts =
    ExecutionCounts(
        target = rows.maxOfOrNull { it.targetCount.coerceAtLeast(0) }.orZero(),
        open = rows.maxOfOrNull { it.openCount.coerceAtLeast(0) }.orZero(),
        done = rows.maxOfOrNull { it.doneCount.coerceAtLeast(0) }.orZero(),
    )

private fun Int?.orZero(): Int = this ?: 0

internal fun adherenceWindowRows(
    rows: List<VaccinationExecutionRowDto>,
    selectedDayRows: List<VaccinationExecutionRowDto>,
    selectedDay: LocalDate,
): List<VaccinationExecutionRowDto> {
    val selectedDriveKeys = selectedDayRows.mapNotNull { it.adherenceDriveKey() }.toSet()
    return rows.filter { row ->
        val scheduleDate = row.currentScheduleDate
        row.hasOperatorVisibleWork() &&
            (scheduleDate?.takeIf { it.isNotBlank() }?.let(::parseExecutionDate) ?: selectedDay) <= selectedDay &&
            (selectedDriveKeys.isEmpty() || row.adherenceDriveKey() in selectedDriveKeys)
    }
}

private fun VaccinationExecutionRowDto.adherenceDriveKey(): String? =
    batchId?.takeIf { it.isNotBlank() }
        ?: driveId?.takeIf { it.isNotBlank() }
        ?: sopTaskId?.takeIf { it.isNotBlank() }

/**
 * A park-level vaccination task can legitimately span several sheds. Compose lazy-list keys
 * therefore cannot use task/batch/drive identity alone: every shed card needs its own stable key
 * while retaining the execution identity that distinguishes multiple drives in one shed.
 */
internal fun executionCardId(
    shedId: String,
    taskId: String?,
    batchId: String?,
    driveId: String?,
    partitionLabel: String? = null,
): String = buildString {
    append("shed:")
    append(shedId)
    append("|partition:")
    append(executionPartitionKey(partitionLabel))
    when {
        !taskId.isNullOrBlank() -> append("|task:").append(taskId)
        !batchId.isNullOrBlank() -> append("|batch:").append(batchId)
        !driveId.isNullOrBlank() -> append("|drive:").append(driveId)
    }
}

internal fun VaccinationExecutionRowDto.executionCardId(): String =
    executionCardId(
        shedId = shedId,
        taskId = sopTaskId,
        batchId = batchId,
        driveId = driveId,
        partitionLabel = partitionLabel ?: partition,
    )

private fun executionPartitionKey(raw: String?): String {
    val normalized = raw.orEmpty().trim().lowercase()
        .replace(Regex("^part[\\s]+"), "")
    return normalized.ifBlank { "whole" }
}

private fun List<ExecutionParkOptionDto>.toShedParkFilters(selectedParkId: String?): List<ShedParkFilter> =
    mapNotNull { option ->
        val id = option.parkId.takeIf { it.isNotBlank() } ?: return@mapNotNull null
        val label = option.name.ifBlank { option.code.ifBlank { id.take(8) } }
        ShedParkFilter(parkId = id, label = label, isSelected = id == selectedParkId)
    }

private fun ShedStatus.readable(): String = name.lowercase().replaceFirstChar { it.uppercase() }

private fun List<VaccinationExecutionRowDto>.reviewAwareStatusLabel(status: ShedStatus): String {
    val inReview = any { row -> row.isVerificationPending() }
    if (inReview) return "In review"
    return firstOrNull()?.workState.orEmpty().ifBlank { status.readable() }.readableState()
}

private fun List<VaccinationExecutionRowDto>.statusChips(status: ShedStatus): List<ShedStatusChip> {
	val primary =
		if (any { it.isVerificationPending() }) {
			ShedStatusChip(ShedStatusChipKey.IN_REVIEW, ShedStatusTone.INFO)
		} else {
			ShedStatusChip(status.toChipKey(), status.toChipTone())
		}
	val overdue = ShedStatusChip(ShedStatusChipKey.OVERDUE, ShedStatusTone.DANGER)
	return if (any { it.isOverdueWork() } && primary.key != overdue.key) listOf(primary, overdue) else listOf(primary)
}

private fun ShedStatus.toChipKey(): ShedStatusChipKey = when (this) {
    ShedStatus.DONE -> ShedStatusChipKey.DONE
    ShedStatus.PENDING -> ShedStatusChipKey.IN_PROGRESS
    ShedStatus.DELAYED -> ShedStatusChipKey.OVERDUE
    ShedStatus.SENT_BACK -> ShedStatusChipKey.SENT_BACK
}

private fun ShedStatus.toChipTone(): ShedStatusTone = when (this) {
    ShedStatus.DONE -> ShedStatusTone.OK
    ShedStatus.PENDING -> ShedStatusTone.WARN
    ShedStatus.DELAYED -> ShedStatusTone.DANGER
    ShedStatus.SENT_BACK -> ShedStatusTone.DANGER
}

private fun VaccinationExecutionRowDto.hasOperatorVisibleWork(): Boolean =
    hasOpenOrReviewWork() || doneCount > 0

private fun VaccinationExecutionRowDto.hasOpenOrReviewWork(): Boolean =
    openCount > 0 || isVerificationPending() || (doneCount > 0 && !isFinalClosed())

internal fun VaccinationExecutionRowDto.isVisibleForOperatorDay(
    selectedDay: LocalDate,
    workWindow: OperatorWorkWindow,
): Boolean {
    val dueDate = currentScheduleDate?.let(::parseExecutionDate)
    return when {
        !hasOperatorVisibleWork() -> false
        dueDate == null -> selectedDay == workWindow.today
        selectedDay == workWindow.today && dueDate.isBefore(workWindow.today) -> hasOpenOrReviewWork()
        selectedDay == workWindow.today -> dueDate == workWindow.today
        else -> dueDate == selectedDay
    }
}

private fun VaccinationExecutionRowDto.isVerificationPending(): Boolean =
    proofStatus.equals("uploaded", ignoreCase = true) ||
        verificationStatus.equals("pending", ignoreCase = true) ||
        sopStatus.equals("submitted", ignoreCase = true) ||
        sopStatus.equals("needs_review", ignoreCase = true) ||
        workState.equals("verification_pending", ignoreCase = true)

/**
 * Overdue-ness is BACKEND-OWNED: `workState` already carries `overdue`/`missed`
 * (vaccinationexecution/app/service.go compares dueAt to as_of and, crucially, returns
 * verification_pending BEFORE it can ever return overdue). The local date comparison
 * below is only a stale-cache safety net for rows served from Room whose workState was
 * computed against an older as_of.
 *
 * That net must carry the same submission term the backend uses — and that the calendar
 * `canonical_read.go` genuine_overdue predicate uses: work that has been SUBMITTED and is
 * awaiting verification is not late. Dropping that term is what painted a red "Overdue"
 * chip next to "In review" on already-submitted sheds after the IST midnight rollover.
 */
private fun VaccinationExecutionRowDto.isOverdueWork(): Boolean {
    val work = workState.lowercase()
    if (work.contains("overdue") || work.contains("missed")) return true
    if (isFinalClosed() || isVerificationPending()) return false
    val scheduleDate = currentScheduleDate
        ?.takeIf { it.isNotBlank() }
        ?.let { runCatching { LocalDate.parse(it) }.getOrNull() }
        ?: return false
    return scheduleDate.isBefore(LocalDate.now(ZoneId.of("Asia/Kolkata")))
}

private fun VaccinationExecutionRowDto.isAcceptedForProtocolSummary(): Boolean =
    sopStatus.equals("accepted", ignoreCase = true) ||
        sopStatus.equals("closed", ignoreCase = true) ||
        sopStatus.equals("completed", ignoreCase = true) ||
        verificationStatus.equals("accepted", ignoreCase = true) ||
        verificationStatus.equals("verified", ignoreCase = true) ||
        workState.equals("accepted", ignoreCase = true) ||
        workState.equals("closed", ignoreCase = true) ||
        workState.equals("completed", ignoreCase = true)

private fun VaccinationExecutionRowDto.acceptedAnimalCount(): Int =
    acceptedCount?.coerceAtLeast(0)
        ?: if (isAcceptedForProtocolSummary()) doneCount.coerceAtLeast(0) else 0

private fun VaccinationExecutionRowDto.isFinalClosed(): Boolean = when (sopStatus.lowercase()) {
    "accepted", "closed", "completed" -> true
    else -> workState.equals("accepted", ignoreCase = true) ||
        workState.equals("closed", ignoreCase = true) ||
        workState.equals("completed", ignoreCase = true)
}

/**
 * True when tapping this shed may ONLY open its read-only record — i.e. there is nothing left
 * for the operator to do here.
 *
 * CORE INVARIANT (backend-owned, see vaccinationexecution/app/service.go
 * computeOperatorLockState): only a FINAL SUBMIT locks the card. Partial review/proof/
 * verification state NEVER locks while openCount > 0. `sopStatus` wording such as
 * "needs_review" or "submitted" describes EVIDENCE state, not remaining work, and must not be
 * read as a lock signal — a card with open=6 whose sopStatus happens to read "needs_review" is
 * NOT done. This was the field bug: a 17/11/6 needs_review card was refused entry because the
 * old predicate treated "needs_review" itself as a terminal submission status.
 *
 * Prefer the backend's own `operatorCanContinue` field. Fall back, ONLY for API responses that
 * predate this field (missing operatorCanContinue), to the pre-existing openCount + terminal
 * sopStatus guard: record-only iff openCount == 0 AND sopStatus is a terminal submission status
 * (submitted/needs_review/accepted/closed/completed). openCount==0 alone is not enough in the
 * fallback -- a shed can reach openCount==0 via a draft/uploaded proof that has not been
 * submitted yet (`sopStatus="draft"`/`"open"`), which must stay open for finalize.
 */
internal fun List<VaccinationExecutionRowDto>.opensSubmittedRecordOnly(): Boolean =
    isNotEmpty() &&
        // Explicit verifier redo ALWAYS vetoes record-only, even against a stale backend
        // operatorCanContinue: the verifier deliberately sent this back to the operator.
        none { row -> row.needsRedo() } &&
        all { row -> row.hasSubmittedRecord() }

/** True when this row's work is submitted-and-locked from the operator's perspective. Prefers the
 *  backend-owned operatorCanContinue (core invariant: only a FINAL SUBMIT locks; partial
 *  review/proof state never locks while openCount > 0); falls back for pre-field API responses to
 *  the explicit openCount + terminal-submission-status guard. */
private fun VaccinationExecutionRowDto.hasSubmittedRecord(): Boolean {
    val canContinue = operatorCanContinue
    return if (canContinue != null) {
        !canContinue
    } else {
        openCount.coerceAtLeast(0) == 0 && sopStatus.isSubmissionTerminalStatus()
    }
}

private fun String.isSubmissionTerminalStatus(): Boolean = when (lowercase()) {
    "submitted", "needs_review", "accepted", "closed", "completed" -> true
    else -> false
}

private fun String.readableState(): String =
    replace('_', ' ').replaceFirstChar { it.uppercase() }

internal data class OperatorWorkWindow(
    val asOf: String?,
    val dueBefore: String,
    val todayLabel: String,
    val windowLabel: String,
    val today: LocalDate,
    val firstDay: LocalDate,
    val lastDay: LocalDate,
) {
    companion object {
        fun today(now: ZonedDateTime = ZonedDateTime.now(KOLKATA)): OperatorWorkWindow {
            val today = now.toLocalDate()
            // The strip shows one prior day so yesterday's slip is visible, then today +5:
            // firstDay = today-1, lastDay = today+5 (OPERATOR_WINDOW_DAYS tabs). Landing stays
            // on today (see initialDay). Matches the leadership Calendar week window.
            val firstDay = today.minusDays(1)
            val lastDay = firstDay.plusDays((OPERATOR_WINDOW_DAYS - 1).toLong())
            return OperatorWorkWindow(
                // Live operator work is a current-view read. Do not send a phone-generated
                // as_of timestamp: by the time it reaches the API it is already historical,
                // and the backend correctly rejects historical point-in-time execution reads.
                // Omitting as_of lets the server anchor the query to its own current clock.
                asOf = null,
                // Upper bound covers lastDay (today+5); the backend still returns past-due
                // rows for the yesterday tab and today's backlog fold.
                dueBefore = now.plusDays((OPERATOR_WINDOW_DAYS - 1).toLong())
                    .format(DateTimeFormatter.ISO_OFFSET_DATE_TIME),
                todayLabel = "Today · ${shortDateLabel(today)}",
                windowLabel = "${shortDateLabel(firstDay)} → ${shortDateLabel(lastDay)}",
                today = today,
                firstDay = firstDay,
                lastDay = lastDay,
            )
        }
    }
}

private fun buildOperatorDayTabs(
    rows: List<VaccinationExecutionRowDto>,
    window: OperatorWorkWindow,
    selectedDay: LocalDate,
): List<ShedDayTab> {
    val countsByDate = rows.groupBy { it.currentScheduleDate?.let(::parseExecutionDate) }
        .filterKeys { it != null }
        .mapKeys { it.key!! }
        .mapValues { (_, dueRows) -> executionCounts(dueRows).open }
    // Today's tab folds in the deep backlog (anything due strictly BEFORE the visible
    // yesterday tab), plus today's own work. Yesterday now has its own tab, so it is
    // excluded here to avoid counting the same slip twice.
    val todayBacklogCount = rows
        .filter { row ->
            if (row.openCount <= 0) return@filter false
            val due = row.currentScheduleDate?.let(::parseExecutionDate) ?: return@filter false
            due.isEqual(window.today) || due.isBefore(window.firstDay)
        }
        .let(::executionCounts)
        .open
    return (0 until OPERATOR_WINDOW_DAYS).map { offset ->
        val date = window.firstDay.plusDays(offset.toLong())
        ShedDayTab(
            dateKey = date.toString(),
            dayLabel = date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH).uppercase(Locale.ENGLISH),
            dateLabel = date.dayOfMonth.toString(),
            countLabel = (if (date == window.today) todayBacklogCount else countsByDate[date])
                ?.takeIf { it > 0 }
                ?.toString()
                .orEmpty(),
            isSelected = date == selectedDay,
        )
    }
}

/**
 * Empty / loading / offline sheds state that KEEPS the real today-anchored day strip.
 *
 * [shedsPlaceholder] copies `sampleShedsState()` and does not reset its hardcoded sample
 * [ShedDayTab]s (2026-07-22 "WED" selected), so a no-data view — a CEO/leadership principal
 * with no assigned sheds, an operator on a drive-free day, or the loading/offline moment —
 * rendered those sample dates: never defaulting to today and ignoring date taps (reproduced
 * live on the CEO/CXO "Vaccination" screen). Rebuild the strip from the live [window] +
 * [selectedDay] so the empty state still lands on today, shows yesterday → today+5, and stays
 * tap-responsive.
 */
internal fun emptyShedsState(
    message: String,
    window: OperatorWorkWindow,
    selectedDay: LocalDate,
    readOnly: Boolean = false,
    hostedFromCalendar: Boolean = false,
): ShedsUiState = shedsPlaceholder(message).copy(
    title = "Next 7 days",
    date = if (selectedDay == window.today) "Today · ${shortDateLabel(selectedDay)}" else shortDateLabel(selectedDay),
    window = window.windowLabel,
    dayTabs = if (readOnly) emptyList() else buildOperatorDayTabs(emptyList(), window, selectedDay),
    taskListOnly = true,
    hostedFromCalendar = hostedFromCalendar,
    canOpenShed = !readOnly,
)

internal fun parseExecutionDate(raw: String): LocalDate? =
    runCatching { LocalDate.parse(raw) }.getOrNull()
        ?: runCatching { OffsetDateTime.parse(raw).atZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()
        ?: runCatching { ZonedDateTime.parse(raw).withZoneSameInstant(KOLKATA).toLocalDate() }.getOrNull()

private fun shortDateLabel(date: LocalDate): String =
    "${date.dayOfWeek.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)} ${date.dayOfMonth} " +
        date.month.getDisplayName(TextStyle.SHORT, Locale.ENGLISH)

/**
 * Humanize raw driveName strings for display in vaccine group chips.
 *
 * Backend drive/config identifiers are transformed to human-readable labels before
 * they reach operator-facing vaccine chips.
 *
 * Rules:
 * - Strip leading prefix (take the part after the last " - ").
 * - Parse antigen token (before the dose suffix) and map to display name.
 * - Dose suffix: _first / numeric waves are internal scheduling detail and are
 *   not operator-facing stock labels; _booster remains meaningful copy.
 * - Unknown tokens are Title-Cased with underscores replaced by spaces.
 */
internal fun humanizeVaccineLabel(raw: String): String {
    val trimmedRaw = raw.trim()
    if (trimmedRaw.contains("ET+TT") ||
        trimmedRaw.contains("Blue Tongue", ignoreCase = true) ||
        trimmedRaw.contains("Goat Pox", ignoreCase = true) ||
        trimmedRaw.contains("Sheep Pox", ignoreCase = true)
    ) {
        return trimmedRaw
            .replace(Regex("\\s*[·-]\\s*Dose\\s+\\d+\\b", RegexOption.IGNORE_CASE), "")
            .trim()
    }
    val code = raw
        .substringAfterLast(" - ", raw)
        .substringAfterLast("•", raw)
        .trim()
        .lowercase(Locale.ENGLISH)
        .replace(Regex("[^a-z0-9]+"), "_")
        .trim('_')
        .removePrefix("preventive_care_vaccination_matrix_")
        .let { code -> if (code == "preventive_care_vaccination_matrix") "vaccination" else code }
    val waveDose = Regex("_(?:dose_)?(\\d+)$").find(code)?.groupValues?.getOrNull(1)
        ?: Regex("_w(\\d+)$").find(code)?.groupValues?.getOrNull(1)
    val dose = when {
        code.endsWith("_booster") -> " · Booster"
        waveDose != null -> ""
        else -> ""
    }
    val antigen = code
        .removeSuffix("_booster")
        .removeSuffix("_first")
        .replace(Regex("_(?:dose_)?\\d+$"), "")
        .replace(Regex("_(adult|kid)_w\\d+$"), "")
        .replace(Regex("_(adult|kid)$"), "")
    val label = when (antigen) {
        "et_tt", "ettt", "et+tt" -> "ET+TT"
        "blue_tongue", "bt" -> "Blue Tongue"
        "ppr" -> "PPR"
        "fmd" -> "FMD"
        "goat_pox", "goatpox" -> "Goat Pox"
        "sheep_pox", "sheeppox" -> "Sheep Pox"
        "hs" -> "HS"
        "vaccination" -> "Vaccination"
        else -> antigen.split('_')
            .filter { it.isNotBlank() }
            .joinToString(" ") { part -> part.replaceFirstChar { ch -> ch.uppercase(Locale.ENGLISH) } }
            .ifBlank { raw }
    }
    return label + dose
}
