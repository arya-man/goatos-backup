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
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.network.isConnectivityFailure
import sg.mesha.goatos.core.network.dto.WorkflowCardDto
import sg.mesha.goatos.core.network.dto.WorkflowChipsDto
import sg.mesha.goatos.core.network.dto.WorkflowOverdueDateDto
import sg.mesha.goatos.feature.counts.WorkflowCardBucket
import sg.mesha.goatos.feature.counts.WorkflowCardUi
import sg.mesha.goatos.feature.counts.WorkflowChipUi
import sg.mesha.goatos.feature.counts.WorkflowListEvent
import sg.mesha.goatos.feature.counts.WorkflowListUiState
import sg.mesha.goatos.feature.counts.WorkflowModuleUi
import sg.mesha.goatos.feature.counts.WorkflowOverdueDateUi
import java.time.Duration
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import javax.inject.Inject

/**
 * The Birth/Death work-list screens (`/counts/birth`, `/counts/death` —
 * docs/decisions/birth-death-workflows.md). One shared implementation, TWO Hilt ViewModels
 * ([BirthWorkflowListViewModel] / [DeathWorkflowListViewModel]) because each L0 route is a
 * distinct destination whose module is fixed by the route, not a nav argument.
 *
 * Offline-first: [rows] is a Paging 3 flow whose RemoteMediator fills Room page-by-page and the
 * UI observes Room; the chips are the backend's own per-day counts observed from their Room blob.
 * The date is an Asia/Kolkata business date (AGENTS.md time semantics) capped at today; group
 * buckets are client-side PRESENTATION derived from each card's own backend fields.
 */
abstract class WorkflowListViewModel(
    private val module: WorkflowModuleUi,
    private val repo: WorkflowsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Selection(val dateIso: String, val filter: String = FILTER_ALL)

    private val moduleKey = when (module) {
        WorkflowModuleUi.DEATH -> MODULE_DEATH
        // A LENS keyword, not a workflow module: the backend serves the same card DTO from the
        // colostrum-day grain (docs/decisions/colostrum-milk-module.md).
        WorkflowModuleUi.COLOSTRUM -> MODULE_COLOSTRUM
        else -> MODULE_BIRTH
    }

    private val _selection = MutableStateFlow(Selection(dateIso = todayIso()))
    private val _isOffline = MutableStateFlow(false)
    private val _lastSyncedAt = MutableStateFlow<Long?>(null)
    private val _isRefreshing = MutableStateFlow(false)

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<WorkflowCardUi>> = _selection
        .flatMapLatest { selection -> repo.cards(moduleKey, selection.dateIso, selection.filter) }
        .map { page -> page.map { it.toCardUi() } }
        .cachedIn(viewModelScope)

    @OptIn(ExperimentalCoroutinesApi::class)
    private val chips: Flow<WorkflowChipsDto?> = _selection
        .flatMapLatest { selection -> repo.observeChips(moduleKey, selection.dateIso) }

    private val overdueDates: Flow<List<WorkflowOverdueDateDto>> = repo.observeOverdueDates(moduleKey)

    private val summaries = combine(chips, overdueDates) { chipCounts, dates -> chipCounts to dates }

    val state: StateFlow<WorkflowListUiState> = combine(
        _selection,
        summaries,
        _isOffline,
        _lastSyncedAt,
        _isRefreshing,
    ) { selection, summary, isOffline, lastSyncedAt, isRefreshing ->
        val (chipCounts, overdueDateRows) = summary
        val today = todayIso()
        WorkflowListUiState(
            module = module,
            subtitle = chipCounts?.let {
                "${it.all} ${if (module == WorkflowModuleUi.COLOSTRUM) SUBTITLE_KIDS else SUBTITLE_OPEN}"
            } ?: "",
            dateIso = selection.dateIso,
            dateLabel = dateLabel(selection.dateIso, today),
            isToday = selection.dateIso >= today,
            chips = (chipCounts ?: WorkflowChipsDto()).toChipUi(),
            overdueDates = overdueDateRows.mapNotNull { it.toUi() },
            selectedFilter = selection.filter,
            isRefreshing = isRefreshing,
            lastSyncedAt = lastSyncedAt,
            isOffline = isOffline,
            emptyMessage = workflowEmptyMessage(module, isOffline),
            isErrorEmpty = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        WorkflowListUiState(module = module, dateIso = todayIso(), dateLabel = dateLabel(todayIso(), todayIso())),
    )

    init {
        analytics.track(
            AnalyticsEvents.WORKFLOW_LIST_VIEWED,
            mapOf(AnalyticsEvents.Params.KIND to moduleKey),
        )
    }

    fun onEvent(event: WorkflowListEvent) {
        when (event) {
            WorkflowListEvent.Refresh -> Unit // Paging refresh is triggered at the call site.
            is WorkflowListEvent.SelectFilter -> selectFilter(event.key)
            WorkflowListEvent.PrevDay -> shiftDay(-1)
            WorkflowListEvent.NextDay -> shiftDay(1)
            WorkflowListEvent.Today -> selectDate(todayIso())
            is WorkflowListEvent.SelectDate -> selectDate(event.dateIso)
            is WorkflowListEvent.OpenOverdueDate -> openOverdueDate(event.dateIso)
            is WorkflowListEvent.OpenCard -> analytics.track(
                AnalyticsEvents.WORKFLOW_CARD_OPENED,
                mapOf(AnalyticsEvents.Params.KIND to moduleKey),
            )
            WorkflowListEvent.AddNew, WorkflowListEvent.Back -> Unit // navigation — nav host.
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        _isRefreshing.value = false
        _isOffline.value = error.isConnectivityFailure()
        crashReporter.recordException(error, "$moduleKey workflow list page load failed")
        analytics.track(
            AnalyticsEvents.COUNTS_READ_FAILURE,
            mapOf(
                AnalyticsEvents.Params.KIND to "${moduleKey}_workflows",
                AnalyticsEvents.Params.REASON to (error.message ?: "unknown"),
            ),
        )
    }

    fun onRowsLoading() {
        _isRefreshing.value = true
    }

    fun onRowsLoaded() {
        _isRefreshing.value = false
        _isOffline.value = false
        _lastSyncedAt.value = System.currentTimeMillis()
    }

    private fun selectFilter(key: String) {
        val current = _selection.value
        if (current.filter == key) return
        _selection.value = current.copy(filter = key)
    }

    private fun shiftDay(days: Long) {
        val current = LocalDate.parse(_selection.value.dateIso)
        selectDate(current.plusDays(days).toString())
    }

    /** Business dates are capped at today IST — future days have no birth/death events. */
    private fun selectDate(dateIso: String) {
        val parsed = runCatching { LocalDate.parse(dateIso) }.getOrNull() ?: return
        val today = LocalDate.now(IST)
        val capped = if (parsed.isAfter(today)) today else parsed
        val current = _selection.value
        if (current.dateIso == capped.toString()) return
        _selection.value = current.copy(dateIso = capped.toString())
    }

    private fun openOverdueDate(dateIso: String) {
        val parsed = runCatching { LocalDate.parse(dateIso) }.getOrNull() ?: return
        if (parsed.isAfter(LocalDate.now(IST))) return
        _selection.value = Selection(dateIso = parsed.toString(), filter = FILTER_OVERDUE)
    }

    // -----------------------------------------------------------------------
    // DTO -> UI mapping (presentation only; every business value is backend-owned)
    // -----------------------------------------------------------------------

    private fun WorkflowChipsDto.toChipUi(): List<WorkflowChipUi> = buildList {
        add(WorkflowChipUi(FILTER_ALL, CHIP_ALL, all))
        add(WorkflowChipUi(FILTER_OVERDUE, CHIP_OVERDUE, overdue))
        add(WorkflowChipUi(FILTER_DUE, CHIP_DUE, due))
        add(WorkflowChipUi(FILTER_COMPLETED, CHIP_COMPLETED, completed))
        // Colostrum has no awaiting-video bucket: verification is enqueued once per WHOLE kid
        // workflow, so one day's feeds can never occupy it. The backend rejects the filter, so
        // rendering the chip would offer a permanent zero that 400s when tapped.
        if (module != WorkflowModuleUi.COLOSTRUM) {
            add(WorkflowChipUi(FILTER_AWAITING_VIDEO, CHIP_AWAITING_VIDEO, awaitingVideo))
        }
    }

    private fun WorkflowOverdueDateDto.toUi(): WorkflowOverdueDateUi? {
        val parsed = runCatching { LocalDate.parse(date) }.getOrNull() ?: return null
        if (workflowCount < 1) return null
        return WorkflowOverdueDateUi(
            dateIso = parsed.toString(),
            dateLabel = parsed.format(OVERDUE_DATE_FORMAT),
            workflowCount = workflowCount,
        )
    }

    private fun WorkflowCardDto.toCardUi(): WorkflowCardUi {
        val operatorSubmitted = isOperatorSubmitted()
        val bucket = workflowCardBucket(
            state = state,
            awaitingVerification = awaitingVerification,
            operatorSubmitted = operatorSubmitted,
            overdue = nextAction?.overdue == true,
        )
        val eventMoment = eventAt.toIstMomentLabel()
        val meta = buildList {
            if (eventMoment.isNotBlank()) {
                add("${if (moduleKey == MODULE_DEATH) META_DIED else META_BORN} $eventMoment")
            }
            if (shedLabel.isNotBlank()) add(shedLabel)
            if (subject.breed.isNotBlank()) add(subject.breed)
        }.joinToString(" · ")
        val next = nextAction
        return WorkflowCardUi(
            workflowId = workflowId,
            displayId = if (templateKey == TEMPLATE_BIRTH_MOTHER) {
                subject.tag.ifBlank { subject.displayId }
            } else {
                subject.displayId.ifBlank { subject.tag }
            },
            roleLabel = subject.roleLabel,
            metaLine = meta,
            actionsDone = actionsDone,
            actionsTotal = actionsTotal,
            nextKindLabel = if (next == null) NEXT_DONE else NEXT_LABEL,
            nextTitle = when {
                next != null -> next.title
                awaitingVerification -> AWAITING_VERIFICATION
                else -> ALL_DONE
            },
            dueLabel = next?.let { dueChipLabel(it.dueAt, it.overdue) }.orEmpty(),
            overdue = next?.overdue == true,
            bucket = bucket,
            operatorSubmitted = operatorSubmitted,
        )
    }

    private fun String.toIstMomentLabel(): String = runCatching {
        Instant.parse(this).atZone(IST).format(MOMENT_FORMAT)
    }.getOrDefault("")

    /** "2h late" for overdue work; the IST wall time (today) or short date otherwise. */
    private fun dueChipLabel(dueAt: String?, overdue: Boolean): String {
        val instant = dueAt?.let { runCatching { Instant.parse(it) }.getOrNull() } ?: return ""
        if (overdue) {
            val late = Duration.between(instant, Instant.now())
            return when {
                late.toDays() >= 1 -> "${late.toDays()}$LATE_DAYS"
                late.toHours() >= 1 -> "${late.toHours()}$LATE_HOURS"
                else -> "${late.toMinutes().coerceAtLeast(1)}$LATE_MINUTES"
            }
        }
        val zoned = instant.atZone(IST)
        return if (zoned.toLocalDate() == LocalDate.now(IST)) {
            zoned.format(TIME_FORMAT)
        } else {
            zoned.format(DATE_FORMAT)
        }
    }

    private fun dateLabel(dateIso: String, todayIso: String): String {
        val parsed = runCatching { LocalDate.parse(dateIso) }.getOrNull() ?: return dateIso
        val label = parsed.format(DAY_FORMAT)
        return if (dateIso == todayIso) "$TODAY_PREFIX$label" else label
    }

    private fun todayIso(): String = LocalDate.now(IST).toString()

    private companion object {
        val IST: ZoneId = ZoneId.of("Asia/Kolkata")
        val MOMENT_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM HH:mm")
        val TIME_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
        val DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")
        val DAY_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM")
        val OVERDUE_DATE_FORMAT: DateTimeFormatter = DateTimeFormatter.ofPattern("EEE, d MMM")

        const val MODULE_BIRTH = "birth"
        const val MODULE_DEATH = "death"
        const val MODULE_COLOSTRUM = "colostrum"
        const val TEMPLATE_BIRTH_MOTHER = "birth_mother"
        const val STATE_COMPLETED = "completed"
        const val FILTER_ALL = "all"
        const val FILTER_OVERDUE = "overdue"
        const val FILTER_DUE = "due"
        const val FILTER_COMPLETED = "completed"
        const val FILTER_AWAITING_VIDEO = "awaiting_video"

        const val CHIP_ALL = "All"
        const val CHIP_OVERDUE = "Overdue"
        const val CHIP_DUE = "Due"
        const val CHIP_COMPLETED = "Completed"
        const val CHIP_AWAITING_VIDEO = "Awaiting video"

        const val SUBTITLE_OPEN = "events today"
        // A colostrum card is a KID with feeds due on the selected day, not an event on that day.
        const val SUBTITLE_KIDS = "kids to feed"
        const val NEXT_LABEL = "Next"
        const val NEXT_DONE = "Done"
        const val AWAITING_VERIFICATION = "Awaiting verification"
        const val ALL_DONE = "All actions complete"
        const val META_BORN = "Born"
        const val META_DIED = "Died"
        const val LATE_DAYS = "d late"
        const val LATE_HOURS = "h late"
        const val LATE_MINUTES = "m late"
        const val TODAY_PREFIX = "Today · "

    }
}

internal fun workflowCardBucket(
    state: String,
    awaitingVerification: Boolean,
    operatorSubmitted: Boolean,
    overdue: Boolean,
): WorkflowCardBucket = when {
    awaitingVerification || operatorSubmitted -> WorkflowCardBucket.IN_REVIEW
    state == "completed" -> WorkflowCardBucket.COMPLETED
    overdue -> WorkflowCardBucket.OVERDUE
    else -> WorkflowCardBucket.DUE
}

/** The operator is finished once every visible action is done and no next action remains. This is
 * deliberately separate from admin approval and verifier status, neither of which is an operator
 * step. Completed historical cards remain openable as read-only records. */
internal fun WorkflowCardDto.isOperatorSubmitted(): Boolean =
    state != "completed" && actionsTotal > 0 && actionsDone >= actionsTotal && nextAction == null

/** `/counts/birth` — the Birth module's L0 work list. */
@HiltViewModel
class BirthWorkflowListViewModel @Inject constructor(
    repo: WorkflowsRepository,
    analytics: AnalyticsPort,
    crashReporter: CrashReporter,
) : WorkflowListViewModel(WorkflowModuleUi.BIRTH, repo, analytics, crashReporter)

/** `/counts/death` — the Death module's L0 work list. */
@HiltViewModel
class DeathWorkflowListViewModel @Inject constructor(
    repo: WorkflowsRepository,
    analytics: AnalyticsPort,
    crashReporter: CrashReporter,
) : WorkflowListViewModel(WorkflowModuleUi.DEATH, repo, analytics, crashReporter)

/**
 * `/counts/colostrum` — the Milk module's Colostrum work list.
 *
 * Same implementation as Birth because it renders the same card DTO; only the grain behind it
 * differs (that date's feeds, not the kid's whole task list). Completing a feed here writes the
 * same row the Birth screen writes (docs/decisions/colostrum-milk-module.md).
 */
@HiltViewModel
class ColostrumWorkflowListViewModel @Inject constructor(
    repo: WorkflowsRepository,
    analytics: AnalyticsPort,
    crashReporter: CrashReporter,
) : WorkflowListViewModel(WorkflowModuleUi.COLOSTRUM, repo, analytics, crashReporter)

internal fun workflowEmptyMessage(module: WorkflowModuleUi, isOffline: Boolean): String = when {
    isOffline -> "Couldn't load the work list. It will appear once you're back online."
    module == WorkflowModuleUi.BIRTH -> "No birth follow-up work for this day."
    module == WorkflowModuleUi.COLOSTRUM -> "No colostrum feeds for this day."
    else -> "Nothing to follow up for this day."
}
