package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsClock
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.clock.ClockPunchDirection
import sg.mesha.goatos.core.data.ClockPunchOutcome
import sg.mesha.goatos.core.data.ClockRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.ClockEntryDto
import sg.mesha.goatos.core.network.dto.ClockPresenceResponseDto
import sg.mesha.goatos.core.network.dto.ClockStatusResponseDto
import sg.mesha.goatos.core.ui.ClockReminderBannerUiState
import sg.mesha.goatos.feature.clock.ClockDateChipUi
import sg.mesha.goatos.feature.clock.ClockDetailLineUi
import sg.mesha.goatos.feature.clock.ClockFilterChipUi
import sg.mesha.goatos.feature.clock.ClockPersonDayUiState
import sg.mesha.goatos.feature.clock.ClockPersonEventUi
import sg.mesha.goatos.feature.clock.ClockRecentEntryUi
import sg.mesha.goatos.feature.clock.ClockRefusalUi
import sg.mesha.goatos.feature.clock.ClockTeamRowUi
import sg.mesha.goatos.feature.clock.ClockTeamSectionUi
import sg.mesha.goatos.feature.clock.ClockTeamTileUi
import sg.mesha.goatos.feature.clock.ClockUiState
import java.time.Duration
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

/*
 * Clock In / Clock Out ViewModels (module clock, maintainer decision 2026-08-27 —
 * docs/features/clock-in-out/plan.md §4). All copy is backend-owned (the status/presence
 * contracts' `copy` maps) — these classes only SELECT and TEMPLATE it; the one client-composed
 * figure anywhere is the ticking elapsed rendering of a backend-served `clock_in_at`.
 */

private val IST: ZoneId = ZoneId.of("Asia/Kolkata")

/** Applies a backend `%s` template; a template without `%s` is returned verbatim. */
private fun template(copyLine: String, value: String): String =
    if (copyLine.contains("%s")) copyLine.replace("%s", value) else copyLine

private fun ClockEntryDto.toRecentUi(): ClockRecentEntryUi = ClockRecentEntryUi(
    // Full identity per the stable-list-key rule: member id + business date.
    listKey = "$workforceMemberId:$businessDate",
    dateLabel = businessDate,
    timeLine = listOfNotNull(
        clockInLabel.takeIf { it.isNotBlank() },
        clockOutLabel?.takeIf { it.isNotBlank() },
    ).joinToString(" – "),
    hoursLabel = hoursLabel.orEmpty(),
    flags = flags.map { it.label },
)

/**
 * The shell-global not-clocked-in reminder (plan §4.3). Observes the Room-cached status blob —
 * the SAME cache the My Clock screen renders from — and shows the banner ONLY while the
 * backend's `banner_text` is non-empty. Refresh runs on every subscription activation (app
 * resume re-activates after `WhileSubscribed`'s stop timeout) and after every sync drain via the
 * CLOCK_IN/CLOCK_OUT post-success refresh hooks in AppModule, so the banner disappears the
 * moment a queued clock-in lands.
 */
@HiltViewModel
class ClockStatusViewModel @Inject constructor(
    private val repo: ClockRepository,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    private val _isRefreshing = MutableStateFlow(false)
    private var lastShownText: String? = null

    val banner: StateFlow<ClockReminderBannerUiState?> = repo.observeStatus()
        .onStart { refreshInBackground() }
        .map { dto -> dto?.bannerText?.takeIf { it.isNotBlank() }?.let { ClockReminderBannerUiState(it) } }
        .onEach { state ->
            // Fire shown-analytics once per visibility transition, not once per Room re-emit.
            if (state != null && state.text != lastShownText) {
                analytics.track(AnalyticsEventsClock.CLOCK_BANNER_SHOWN)
            }
            lastShownText = state?.text
        }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    fun onBannerTapped() {
        analytics.track(AnalyticsEventsClock.CLOCK_BANNER_TAPPED)
    }

    /** Called by the shell after a sync sheet interaction or an explicit resume signal. */
    fun refresh() = refreshInBackground()

    private fun refreshInBackground() {
        if (_isRefreshing.value) return
        viewModelScope.launch {
            _isRefreshing.value = true
            repo.refreshStatus()
            _isRefreshing.value = false
        }
    }
}

/** My Clock (route `/clock`, plan §4.1) state holder. */
@HiltViewModel
class ClockViewModel @Inject constructor(
    private val repo: ClockRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val _isRefreshing = MutableStateFlow(false)
    private val _refusal = MutableStateFlow<ClockRefusalUi?>(null)
    private val _punchInFlight = MutableStateFlow(false)
    private var pendingWatch: Job? = null

    val state: StateFlow<ClockUiState> = combine(
        repo.observeStatus(),
        _isRefreshing,
        _refusal,
        // Durable pending signal from the outbox table OR the tap-local flag —
        // a queued punch must stay visible across navigation and process death
        // until it drains (E2E finding 2026-08-28).
        combine(repo.observePendingPunch(), _punchInFlight) { queued, inFlight ->
            queued ?: if (inFlight) "in_flight" else null
        },
    ) { dto, refreshing, refusal, pending ->
        composeState(dto, refreshing, refusal, pending)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ClockUiState())

    fun refresh() {
        if (_isRefreshing.value) return
        viewModelScope.launch {
            _isRefreshing.value = true
            repo.refreshStatus()
            _isRefreshing.value = false
        }
    }

    /** The big button: direction follows the CURRENT backend state. Re-runs the mock scan. */
    fun punch() {
        val current = state.value
        if (!current.punchEnabled) return
        val direction = if (current.stateKey == "clocked_in") ClockPunchDirection.OUT else ClockPunchDirection.IN
        analytics.track(
            if (direction == ClockPunchDirection.IN) {
                AnalyticsEventsClock.CLOCK_IN_ATTEMPTED
            } else {
                AnalyticsEventsClock.CLOCK_OUT_ATTEMPTED
            },
        )
        viewModelScope.launch {
            when (val outcome = repo.punch(direction)) {
                is ClockPunchOutcome.Blocked -> {
                    analytics.track(
                        AnalyticsEventsClock.CLOCK_IN_REFUSED_MOCK,
                        mapOf(AnalyticsEvents.Params.REASON to if (outcome.verdict.mockFix) "mock_fix" else "mock_app"),
                    )
                    _refusal.value = ClockRefusalUi(
                        message = template(
                            state.value.refusalTemplate,
                            outcome.verdict.appLabels.joinToString(", "),
                        ),
                        checkAgainLabel = state.value.checkAgainLabel,
                    )
                }
                is ClockPunchOutcome.NoLocation -> {
                    analytics.track(
                        AnalyticsEventsClock.CLOCK_REFUSED_NO_LOCATION,
                        mapOf(AnalyticsEvents.Params.REASON to if (outcome.permissionMissing) "permission" else "no_fix"),
                    )
                    _refusal.value = ClockRefusalUi(
                        message = state.value.locationRequiredMessage,
                        checkAgainLabel = state.value.checkAgainLabel,
                    )
                }
                is ClockPunchOutcome.Enqueued -> {
                    analytics.track(
                        if (direction == ClockPunchDirection.IN) {
                            AnalyticsEventsClock.CLOCK_IN_SUCCEEDED
                        } else {
                            AnalyticsEventsClock.CLOCK_OUT_SUCCEEDED
                        },
                    )
                    _refusal.value = null
                    watchPending(outcome.outboxItemId)
                }
                is ClockPunchOutcome.Failed -> {
                    crashReporter.recordException(
                        IllegalStateException(outcome.message),
                        "clock punch enqueue failed",
                    )
                    // The durable write never landed; refresh so the screen shows server truth.
                    refresh()
                }
            }
        }
    }

    /** The refusal panel's re-scan: clears the panel and runs the whole punch path again. */
    fun checkAgain() {
        _refusal.value = null
        punch()
    }

    /** Optimistic in-flight tracking: the button stays disabled while the punch row is pending. */
    private fun watchPending(outboxItemId: String) {
        _punchInFlight.value = true
        pendingWatch?.cancel()
        pendingWatch = viewModelScope.launch {
            syncRepository.observeItem(outboxItemId).collect { item ->
                when (item?.status) {
                    null, SyncItemStatus.SUCCEEDED, SyncItemStatus.FAILED -> {
                        _punchInFlight.value = false
                        // The post-success hook refreshes too; this covers the terminal-conflict
                        // path (already_clocked_in / mock refusal) so the screen lands on truth.
                        repo.refreshStatus()
                        pendingWatch?.cancel()
                    }
                    else -> _punchInFlight.value = true
                }
            }
        }
    }

    private fun composeState(
        dto: ClockStatusResponseDto?,
        refreshing: Boolean,
        refusal: ClockRefusalUi?,
        pending: String?,
    ): ClockUiState {
        val copy = dto?.copy.orEmpty()
        val stateKey = dto?.state.orEmpty()
        val entry = dto?.entry
        val inFlight = pending != null
        val baseHeadline = when (stateKey) {
            "clocked_in" -> template(copy["state.clocked_in"].orEmpty(), entry?.clockInLabel.orEmpty())
            // An auto-closed day has NO hours (never invented); trim the
            // template's dangling separator rather than showing "· ".
            "clocked_out" -> template(copy["state.clocked_out"].orEmpty(), entry?.hoursLabel.orEmpty())
                .trim().trimEnd('·').trim()
            else -> copy["state.not_clocked_in"].orEmpty()
        }
        // A punch waiting on the outbox owns the headline until it drains: the
        // operator saved a real action and must see it acknowledged. A cached
        // payload from before the pending copy shipped falls back to the plain
        // state headline (the button below is still disabled either way).
        val pendingKey = when {
            pending == "clock_out" || (pending != null && stateKey == "clocked_in") -> "state.clock_out_pending"
            pending != null -> "state.clock_in_pending"
            else -> null
        }
        val headline = pendingKey?.let { copy[it].orEmpty().ifBlank { baseHeadline } } ?: baseHeadline
        val punchLabel = when (stateKey) {
            "clocked_in" -> copy["action.clock_out"]?.takeIf { it.isNotBlank() }
            "clocked_out" -> null
            else -> copy["action.clock_in"]?.takeIf { it.isNotBlank() }
        }
        return ClockUiState(
            title = copy["module.title"].orEmpty(),
            isRefreshing = refreshing,
            hasStatus = dto != null,
            stateHeadline = headline,
            locationLine = entry?.locationLabel.orEmpty(),
            flags = entry?.flags?.map { it.label }.orEmpty(),
            punchLabel = punchLabel,
            punchEnabled = !inFlight,
            recentTitle = copy["recent.title"].orEmpty(),
            emptyRecent = copy["empty.recent"].orEmpty(),
            recent = dto?.recentEntries.orEmpty().map { it.toRecentUi() },
            refusal = refusal,
            stateKey = stateKey,
            refusalTemplate = dto?.punchRefusedCopy.orEmpty(),
            checkAgainLabel = copy["check_again"].orEmpty(),
            locationRequiredMessage = copy["refusal.location"].orEmpty()
                // A cached status from before the location-mandatory rule shipped has no
                // key yet; the refusal panel must still say the business thing.
                .ifBlank { "Turn on location to clock in — your location is required." },
        )
    }
}

/** Team — the leadership presence board (route `/clock/team`, plan §4.4) state holder. */
@HiltViewModel
class ClockTeamViewModel @Inject constructor(
    private val repo: ClockRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Filter(
        val date: String = "",
        val parkId: String = "",
        val designation: String = "",
        val bucket: String = "",
        val query: String = "",
    )

    private val filter = MutableStateFlow(Filter())
    private val _isRefreshing = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)
    private val page = MutableStateFlow<ClockPresenceResponseDto?>(null)

    /**
     * Accumulated rows for the CURRENT filter only — replaced wholesale on every filter change or
     * refresh, appended by keyset pages while scrolling. Bounded: the grain is one row per active
     * workforce member for one day, so the roster size itself caps growth; MAX_ROWS is a hard
     * safety valve against a pathological cursor loop (android-bounded-memory rule).
     */
    private val rows = MutableStateFlow<List<sg.mesha.goatos.core.network.dto.ClockPresenceRowDto>>(emptyList())
    private val tick = MutableStateFlow(OffsetDateTime.now())
    private var nextCursor: String = ""
    private var debounce: Job? = null
    private var opened = false

    init {
        viewModelScope.launch {
            while (true) {
                delay(30_000)
                tick.value = OffsetDateTime.now()
            }
        }
    }

    val state: StateFlow<sg.mesha.goatos.feature.clock.ClockTeamUiState> = combine(
        page,
        rows,
        filter,
        combine(_isRefreshing, _isLoadingMore) { a, b -> a to b },
        tick,
    ) { dto, currentRows, currentFilter, (refreshing, loadingMore), now ->
        composeState(dto, currentRows, currentFilter, refreshing, loadingMore, now)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), sg.mesha.goatos.feature.clock.ClockTeamUiState())

    fun open() {
        if (opened) return
        opened = true
        analytics.track(AnalyticsEventsClock.CLOCK_TEAM_OPENED)
        refresh()
    }

    fun refresh() {
        if (_isRefreshing.value) return
        viewModelScope.launch { loadFirstPage() }
    }

    fun selectDate(date: String) = applyFilter { it.copy(date = date) }

    fun selectTile(key: String) = applyFilter { it.copy(bucket = if (it.bucket == key) "" else key) }

    fun selectPark(key: String) = applyFilter { it.copy(parkId = if (it.parkId == key) "" else key) }

    fun selectDesignation(key: String) =
        applyFilter { it.copy(designation = if (it.designation == key) "" else key) }

    fun queryChanged(query: String) {
        filter.value = filter.value.copy(query = query)
        debounce?.cancel()
        debounce = viewModelScope.launch {
            delay(300)
            loadFirstPage()
        }
    }

    fun loadMore() {
        if (_isLoadingMore.value || _isRefreshing.value) return
        val cursor = nextCursor
        if (cursor.isBlank() || rows.value.size >= MAX_ROWS) return
        viewModelScope.launch {
            _isLoadingMore.value = true
            val current = filter.value
            repo.fetchPresence(
                date = current.date.ifBlank { null },
                parkId = current.parkId.ifBlank { null },
                designation = current.designation.ifBlank { null },
                bucket = current.bucket.ifBlank { null },
                q = current.query.ifBlank { null },
                cursor = cursor,
            ).onSuccess { dto ->
                nextCursor = dto.nextCursor
                val known = rows.value.map { it.workforceMemberId }.toHashSet()
                rows.value = rows.value + dto.rows.filter { it.workforceMemberId !in known }
            }.onFailure { failure ->
                crashReporter.recordException(failure, "clock presence next-page load failed")
            }
            _isLoadingMore.value = false
        }
    }

    private fun applyFilter(mutate: (Filter) -> Filter) {
        filter.value = mutate(filter.value)
        viewModelScope.launch { loadFirstPage() }
    }

    private suspend fun loadFirstPage() {
        _isRefreshing.value = true
        val current = filter.value
        repo.fetchPresence(
            date = current.date.ifBlank { null },
            parkId = current.parkId.ifBlank { null },
            designation = current.designation.ifBlank { null },
            bucket = current.bucket.ifBlank { null },
            q = current.query.ifBlank { null },
            cursor = null,
        ).onSuccess { dto ->
            page.value = dto
            rows.value = dto.rows
            nextCursor = dto.nextCursor
        }.onFailure { failure ->
            crashReporter.recordException(failure, "clock presence load failed")
        }
        _isRefreshing.value = false
    }

    private fun composeState(
        dto: ClockPresenceResponseDto?,
        currentRows: List<sg.mesha.goatos.core.network.dto.ClockPresenceRowDto>,
        currentFilter: Filter,
        refreshing: Boolean,
        loadingMore: Boolean,
        now: OffsetDateTime,
    ): sg.mesha.goatos.feature.clock.ClockTeamUiState {
        val copy = dto?.copy.orEmpty()
        val isToday = dto?.isToday ?: true
        val selectedDate = currentFilter.date.ifBlank { dto?.businessDate ?: LocalDate.now(IST).toString() }
        val dates = (0 until DATE_STRIP_DAYS).map { offset ->
            val date = LocalDate.now(IST).minusDays(offset.toLong())
            ClockDateChipUi(
                date = date.toString(),
                label = date.format(DATE_CHIP_FORMAT),
                selected = date.toString() == selectedDate,
            )
        }
        val summary = dto?.summary
        val workingLabel = if (isToday) copy["summary.working"] else copy["summary.worked"] ?: copy["summary.working"]
        val tiles = listOf(
            ClockTeamTileUi("working", workingLabel.orEmpty(), summary?.working ?: 0, currentFilter.bucket == "working"),
            ClockTeamTileUi("clocked_out", copy["summary.clocked_out"].orEmpty(), summary?.clockedOut ?: 0, currentFilter.bucket == "clocked_out"),
            ClockTeamTileUi("not_clocked_in", copy["summary.not_clocked_in"].orEmpty(), summary?.notClockedIn ?: 0, currentFilter.bucket == "not_clocked_in"),
            ClockTeamTileUi("flagged", copy["summary.flagged"].orEmpty(), summary?.flagged ?: 0, currentFilter.bucket == "flagged"),
        ).filter { it.label.isNotBlank() }
        val allLabel = copy["filter.all"].orEmpty()
        val parkChips = buildList {
            if (allLabel.isNotBlank() && dto?.parks.orEmpty().isNotEmpty()) {
                add(ClockFilterChipUi("", allLabel, currentFilter.parkId.isBlank()))
            }
            dto?.parks.orEmpty().forEach { park ->
                add(ClockFilterChipUi(park.id, park.label, currentFilter.parkId == park.id))
            }
        }
        val designationChips = buildList {
            if (allLabel.isNotBlank() && dto?.designations.orEmpty().isNotEmpty()) {
                add(ClockFilterChipUi("", allLabel, currentFilter.designation.isBlank()))
            }
            dto?.designations.orEmpty().forEach { d ->
                add(ClockFilterChipUi(d.code, d.label, currentFilter.designation == d.code))
            }
        }
        val bySection = currentRows.groupBy { it.bucket }
        val sections = SECTION_ORDER.mapNotNull { key ->
            val sectionRows = bySection[key].orEmpty()
            if (sectionRows.isEmpty()) return@mapNotNull null
            val count = when (key) {
                "working" -> summary?.working
                "clocked_out" -> summary?.clockedOut
                else -> summary?.notClockedIn
            }
            val label = when (key) {
                "working" -> workingLabel ?: copy["section.working"]
                "clocked_out" -> copy["section.clocked_out"] ?: copy["summary.clocked_out"]
                else -> copy["section.not_clocked_in"] ?: copy["summary.not_clocked_in"]
            }.orEmpty()
            ClockTeamSectionUi(
                key = key,
                title = if (count != null) "$label · $count" else label,
                rows = sectionRows.map { row ->
                    ClockTeamRowUi(
                        listKey = "${row.workforceMemberId}:${dto?.businessDate.orEmpty()}",
                        memberId = row.workforceMemberId,
                        name = row.personName,
                        subtitle = listOf(row.designation, row.parkLabel)
                            .filter { it.isNotBlank() }
                            .joinToString(" · "),
                        // Backend time_label VERBATIM (it already carries the elapsed
                        // "so far" figure for a working row and is re-composed on every
                        // refresh). Appending a client tick beside it duplicated the
                        // elapsed on screen; the live per-second tick belongs to My
                        // Clock's own entry, not this roster read.
                        timeLine = row.timeLabel,
                        locationLine = row.locationLabel,
                        flags = row.flags.map { it.label },
                        bucket = row.bucket,
                    )
                },
            )
        }
        return sg.mesha.goatos.feature.clock.ClockTeamUiState(
            title = copy["team.title"].orEmpty(),
            isRefreshing = refreshing,
            hasData = dto != null,
            dates = dates,
            tiles = tiles,
            searchPlaceholder = copy["search.placeholder"].orEmpty(),
            query = currentFilter.query,
            parkChips = parkChips,
            designationChips = designationChips,
            sections = sections,
            emptyText = copy["empty.presence"].orEmpty(),
            isLoadingMore = loadingMore,
            hasMore = nextCursor.isNotBlank() && currentRows.size < MAX_ROWS,
        )
    }

    private companion object {
        const val DATE_STRIP_DAYS = 14

        /** Hard in-memory safety valve; the true bound is the active roster size. */
        const val MAX_ROWS = 1_000
        val SECTION_ORDER = listOf("working", "clocked_out", "not_clocked_in")
        val DATE_CHIP_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")
    }
}

/** Person-day drill (route `/clock/team/person/{memberId}?date=`, plan §4.4 item 6). */
@HiltViewModel
class ClockPersonDayViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repo: ClockRepository,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val memberId: String = savedStateHandle.get<String>("memberId").orEmpty()
    private val date: String = savedStateHandle.get<String>("date").orEmpty()

    private val _state = MutableStateFlow(ClockPersonDayUiState())
    val state: StateFlow<ClockPersonDayUiState> = _state.asStateFlow()

    fun refresh() {
        if (_state.value.isRefreshing || memberId.isBlank()) return
        viewModelScope.launch {
            _state.value = _state.value.copy(isRefreshing = true)
            repo.fetchPersonDay(memberId, date.ifBlank { null })
                .onSuccess { dto ->
                    val copy = dto.copy
                    _state.value = ClockPersonDayUiState(
                        isRefreshing = false,
                        hasData = true,
                        name = dto.personName,
                        subtitle = listOf(dto.designation, dto.parkLabel)
                            .filter { it.isNotBlank() }
                            .joinToString(" · "),
                        entryLine = dto.entry?.let { entry ->
                            listOfNotNull(
                                listOfNotNull(
                                    entry.clockInLabel.takeIf { it.isNotBlank() },
                                    entry.clockOutLabel?.takeIf { it.isNotBlank() },
                                ).joinToString(" – ").takeIf { it.isNotBlank() },
                                entry.hoursLabel?.takeIf { it.isNotBlank() },
                            ).joinToString(" · ")
                        }.orEmpty(),
                        dateLabel = dto.businessDate,
                        events = dto.events.map { event ->
                            ClockPersonEventUi(
                                listKey = event.clockEventId,
                                // Backend copy keys the punch kind; blank hides nothing vital
                                // because the times line still identifies the event.
                                title = copy["event.${event.eventType}"]
                                    ?: copy[event.eventType].orEmpty(),
                                flags = emptyList(),
                                lines = buildList {
                                    fun line(labelKey: String, value: String?) {
                                        val v = value?.takeIf { it.isNotBlank() } ?: return
                                        add(ClockDetailLineUi(copy[labelKey].orEmpty(), v))
                                    }
                                    line("detail.captured_at", istTimeLabel(event.capturedAt))
                                    line("detail.recorded_at", istTimeLabel(event.recordedAt))
                                    line("detail.address", event.address)
                                    line(
                                        "detail.coordinates",
                                        if (event.latitude != null && event.longitude != null) {
                                            val accuracy = event.gpsAccuracyM?.let { " (±${it.toInt()} m)" }.orEmpty()
                                            "${event.latitude}, ${event.longitude}$accuracy"
                                        } else {
                                            null
                                        },
                                    )
                                    line(
                                        "detail.device",
                                        listOf(event.deviceModel, event.appVersion)
                                            .filter { it.isNotBlank() }
                                            .joinToString(" · "),
                                    )
                                    line(
                                        "detail.network",
                                        listOfNotNull(
                                            // Backend copy names the connection; the raw
                                            // network_type token must never reach the screen
                                            // (copy-firewall rule).
                                            event.networkType.takeIf { it.isNotBlank() }
                                                ?.let { copy["network.$it"] ?: copy["flag.offline"].takeIf { _ -> it != "online" } },
                                            event.batteryPct?.let { "$it%" },
                                        ).joinToString(" · "),
                                    )
                                },
                            )
                        },
                        recentTitle = copy["recent.title"].orEmpty(),
                        recent = dto.recentDays.map { it.toRecentUi() },
                        emptyText = copy["empty.presence"].orEmpty(),
                    )
                }
                .onFailure { failure ->
                    crashReporter.recordException(failure, "clock person-day load failed")
                    _state.value = _state.value.copy(isRefreshing = false)
                }
        }
    }

    private fun istTimeLabel(rfc3339: String): String? =
        // exception:exempt unparseable instant hides one cosmetic line; the raw event survives server-side.
        runCatching {
            OffsetDateTime.parse(rfc3339).atZoneSameInstant(IST).format(TIME_FORMAT)
        }.getOrNull()

    private companion object {
        val TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm:ss")
    }
}
