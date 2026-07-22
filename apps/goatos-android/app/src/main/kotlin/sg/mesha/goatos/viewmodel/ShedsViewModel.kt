package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
import sg.mesha.goatos.feature.sheds.ShedDayTab
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
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
) : ViewModel() {

    private var nextCursor: String? = null
    private val workWindow = OperatorWorkWindow.today()
    private val _selectedDay = MutableStateFlow(workWindow.today)

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<VaccinationExecutionResponseDto>> =
        repo.observeRows(
            asOf = workWindow.asOf,
            dueBefore = workWindow.dueBefore,
            openOnly = true,
            limit = PAGE_LIMIT,
        ).stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource(data = null)
        )

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<ShedsUiState> = combine(
        observedResource,
        _selectedDay,
        _isRefreshing,
        _isOffline,
        _isLoadingMore
    ) { resource, selectedDay, isRefreshing, isOffline, isLoadingMore ->
        val dto = resource.data
        nextCursor = dto?.nextCursor  // Update pagination cursor for loadMore()
        val isInitialLoading = dto == null && !resource.hasData && resource.error == null && !isOffline
        val base = dto?.toShedsUiState(selectedDay)
            ?: if (resource.hasData) {
                shedsPlaceholder("No sheds scheduled today")
            } else if (isOffline) {
                shedsPlaceholder("Couldn't load vaccination drives. Pull to refresh or try again.")
            } else {
                shedsPlaceholder("Loading…")
            }
        base.copy(
            isRefreshing = isRefreshing,
            isInitialLoading = isInitialLoading,
            isLoadingMore = isLoadingMore,
            hasMore = !dto?.nextCursor.isNullOrBlank(),
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        shedsPlaceholder("Loading today's sheds…").copy(isInitialLoading = true)
    )

    init {
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observeRows]
     *  collector above re-emits and updates [state]); on failure it only flips
     *  [ShedsUiState.isOffline] — cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        val result = repo.refreshRows(
            asOf = workWindow.asOf,
            dueBefore = workWindow.dueBefore,
            openOnly = true,
            limit = PAGE_LIMIT,
        )
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
        result.exceptionOrNull()?.let {
            crashReporter.recordException(it, "vaccination sheds refresh failed")
        }
    }

    fun loadMore() = viewModelScope.launch {
        val cursor = nextCursor ?: return@launch
        if (_isLoadingMore.value) return@launch
        _isLoadingMore.value = true
        val result = repo.appendRows(
            cursor = cursor,
            asOf = workWindow.asOf,
            dueBefore = workWindow.dueBefore,
            openOnly = true,
            limit = PAGE_LIMIT,
        )
        _isLoadingMore.value = false
        _isOffline.value = result.isFailure
        result.exceptionOrNull()?.let {
            crashReporter.recordException(it, "vaccination sheds append failed")
        }
    }

    fun onEvent(event: ShedsEvent) {
        when (event) {
            ShedsEvent.Refresh -> refresh()
            ShedsEvent.LoadMore -> loadMore()
            is ShedsEvent.SelectDay -> selectDay(event.dateKey)
            is ShedsEvent.OpenShedRecord -> Unit // navigation — handled by the nav host.
            ShedsEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun selectDay(dateKey: String) {
        val date = parseExecutionDate(dateKey) ?: return
        if (date < workWindow.today || date > workWindow.lastDay) return
        _selectedDay.value = date
    }

    private fun VaccinationExecutionResponseDto.toShedsUiState(selectedDay: LocalDate): ShedsUiState? {
        if (rows.isEmpty()) return null
        val base = sampleShedsState()
        val weekRows = rows
        val rowsForSelectedDay = weekRows.filter { row ->
            val dueDate = row.currentScheduleDate?.let(::parseExecutionDate)
            val hasOpenWork = row.openCount > 0
            when {
                !hasOpenWork -> false
                dueDate == null -> selectedDay == workWindow.today
                selectedDay == workWindow.today -> !dueDate.isAfter(workWindow.today)
                else -> dueDate == selectedDay
            }
        }
        val shedRows = rowsForSelectedDay.groupBy { it.executionIdentity() }.map { (identity, group) ->
            val first = group.first()
            val scheduleDate = group.mapNotNull { it.currentScheduleDate?.let(::parseExecutionDate) }.minOrNull()
            val status = shedStatusFor(group)
            val counts = executionCounts(group)
            val vaccineGroups = group.groupBy { humanizeVaccineLabel(it.driveName.orEmpty()) }
                .filterKeys { it.isNotBlank() }
                .map { (label, driveRows) ->
                    val driveCounts = executionCounts(driveRows)
                    VaccineGroup(
                        label = label,
                        countLabel = "${driveCounts.open} doses",
                        full = driveCounts.open == 0,
                    )
                }
            ShedRow(
                id = identity.cardId,
                name = first.shedName,
                operatorName = first.owner?.operatorName.orEmpty(),
                physicalShed = first.physicalShed.ifBlank { first.shedName },
                partition = first.partition,
                // animalStage is a biological stage supplied by the execution contract.
                // A drive label is not a cohort/stage and must not be substituted here.
                animalStage = first.animalStage,
                scheduleDateKey = scheduleDate?.toString().orEmpty(),
                scheduleDateLabel = scheduleDate?.let(::shortDateLabel).orEmpty(),
                status = status,
                statusLabel = first.workState.ifBlank { status.readable() }.let { it.readableState() },
                vaccineGroups = vaccineGroups,
                inShed = counts.target.toString(),
                due = counts.open.toString(),
                done = counts.done.toString(),
                progressLabel = percentLabel(counts.done, counts.target),
                progressFraction = fraction(counts.done, counts.target),
                shedId = identity.shedId,
                driveId = identity.driveId,
                batchId = identity.batchId,
                taskId = identity.taskId,
                sopVersionId = identity.sopVersionId,
                taskRowVersion = identity.taskRowVersion,
            )
        }.sortedWith(
            compareBy<ShedRow> { row ->
                row.scheduleDateKey.takeIf { it.isNotBlank() }?.let(::parseExecutionDate) ?: LocalDate.MAX
            }
                .thenBy { it.name.lowercase() }
        )
        val totals = executionCounts(rowsForSelectedDay)
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
            shedCount = rowsForSelectedDay.map { it.shedId }.distinct().size,
            dueCount = totals.open,
            doneCount = totals.done,
            shedCountLabel = "${shedRows.size} sheds",
            dueLabel = "${totals.open} open",
            dayProgressLabel = percentLabel(totals.done, totals.target),
            dayProgressFraction = fraction(totals.done, totals.target),
            daySummary = "${totals.done} / ${totals.target} done",
            caption = null,
            roleNote = null,
            dayTabs = buildOperatorDayTabs(weekRows, workWindow, selectedDay),
            rows = shedRows,
            rosterChanges = emptyList(),
            kernelInfo = null,
        )
    }

    private fun shedStatusFor(rows: List<VaccinationExecutionRowDto>): ShedStatus {
        val anyDelayed = rows.any { row ->
            val work = row.workState.lowercase()
            work.contains("overdue") ||
                work.contains("missed") ||
                work.contains("blocked") ||
                row.severity.equals("critical", ignoreCase = true)
        }
        if (anyDelayed) return ShedStatus.DELAYED
        val counts = executionCounts(rows)
        val allDone = counts.target > 0 && counts.done >= counts.target
        return if (allDone) ShedStatus.DONE else ShedStatus.PENDING
    }

    private fun VaccinationExecutionRowDto.isDone(): Boolean {
        val work = workState.lowercase()
        return work.contains("completed") || work.contains("done")
    }

    private fun percentLabel(done: Int, total: Int): String =
        if (total > 0) "${done * 100 / total}%" else "0%"

    private fun fraction(done: Int, total: Int): Float =
        if (total > 0) done.toFloat() / total else 0f
}

internal data class ExecutionCounts(val target: Int, val open: Int, val done: Int)

/** Execution API rows are aggregated groups. Counts must come from the backend fields, never
 * from List.size (which undercounted a two-goat shed as one because it had one grouped row). */
internal fun executionCounts(rows: List<VaccinationExecutionRowDto>): ExecutionCounts =
    ExecutionCounts(
        target = rows.sumOf { it.targetCount.coerceAtLeast(0) },
        open = rows.sumOf { it.openCount.coerceAtLeast(0) },
        done = rows.sumOf { it.doneCount.coerceAtLeast(0) },
    )

private data class ExecutionIdentity(
    val shedId: String,
    val driveId: String?,
    val batchId: String?,
    val taskId: String?,
    val sopVersionId: String?,
    val taskRowVersion: Int?,
) {
    val cardId: String = executionCardId(shedId, taskId, batchId, driveId)
}

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
): String = buildString {
    append("shed:")
    append(shedId)
    when {
        !taskId.isNullOrBlank() -> append("|task:").append(taskId)
        !batchId.isNullOrBlank() -> append("|batch:").append(batchId)
        !driveId.isNullOrBlank() -> append("|drive:").append(driveId)
    }
}

private fun VaccinationExecutionRowDto.executionIdentity() = ExecutionIdentity(
    shedId = shedId,
    driveId = driveId,
    batchId = batchId,
    taskId = sopTaskId,
    sopVersionId = sopVersionId,
    taskRowVersion = sopTaskRowVersion,
)

private fun ShedStatus.readable(): String = name.lowercase().replaceFirstChar { it.uppercase() }

private fun String.readableState(): String =
    replace('_', ' ').replaceFirstChar { it.uppercase() }

internal data class OperatorWorkWindow(
    val asOf: String?,
    val dueBefore: String,
    val todayLabel: String,
    val windowLabel: String,
    val today: LocalDate,
    val lastDay: LocalDate,
) {
    companion object {
        fun today(now: ZonedDateTime = ZonedDateTime.now(KOLKATA)): OperatorWorkWindow {
            val today = now.toLocalDate()
            val lastDay = today.plusDays((OPERATOR_WINDOW_DAYS - 1).toLong())
            return OperatorWorkWindow(
                // Live operator work is a current-view read. Do not send a phone-generated
                // as_of timestamp: by the time it reaches the API it is already historical,
                // and the backend correctly rejects historical point-in-time execution reads.
                // Omitting as_of lets the server anchor the query to its own current clock.
                asOf = null,
                dueBefore = now.plusDays(OPERATOR_WINDOW_DAYS.toLong())
                    .format(DateTimeFormatter.ISO_OFFSET_DATE_TIME),
                todayLabel = "Today · ${shortDateLabel(today)}",
                windowLabel = "${shortDateLabel(today)} → ${shortDateLabel(lastDay)}",
                today = today,
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
    val todayBacklogCount = rows
        .filter { row ->
            row.openCount > 0 && row.currentScheduleDate?.let(::parseExecutionDate)?.isAfter(window.today) != true
        }
        .let(::executionCounts)
        .open
    return (0 until OPERATOR_WINDOW_DAYS).map { offset ->
        val date = window.today.plusDays(offset.toLong())
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
 * - Dose suffix: _first → no suffix (default), _booster → append " · Booster".
 * - Unknown tokens are Title-Cased with underscores replaced by spaces.
 */
private fun humanizeVaccineLabel(raw: String): String {
    val trimmedRaw = raw.trim()
    if (trimmedRaw.contains("ET+TT") ||
        trimmedRaw.contains("Blue Tongue", ignoreCase = true) ||
        trimmedRaw.contains("Goat Pox", ignoreCase = true) ||
        trimmedRaw.contains("Sheep Pox", ignoreCase = true)
    ) {
        return trimmedRaw
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
        waveDose != null -> " · Dose $waveDose"
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
