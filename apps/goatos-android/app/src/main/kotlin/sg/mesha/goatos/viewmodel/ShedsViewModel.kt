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
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.ExecutionParkOptionDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate
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
    private val _selectedDay = MutableStateFlow(initialDay)
    private val _selectedParkId = MutableStateFlow<String?>(null)

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
    private val transientState = combine(
        _selectedDay,
        _isRefreshing,
        _isOffline,
        _isLoadingMore,
    ) { selectedDay, isRefreshing, isOffline, isLoadingMore ->
        ShedsTransientState(selectedDay, isRefreshing, isOffline, isLoadingMore)
    }

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<ShedsUiState> = combine(
        observedResource,
        transientState,
    ) { resource, transient ->
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
            isRefreshing = transient.isRefreshing,
            isInitialLoading = isInitialLoading,
            isLoadingMore = transient.isLoadingMore,
            hasMore = !dto?.nextCursor.isNullOrBlank(),
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = transient.isOffline,
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
            parkId = _selectedParkId.value,
            asOf = workWindow.asOf,
            dueBefore = workWindow.dueBefore,
            openOnly = OPEN_ONLY_QUERY,
            limit = PAGE_LIMIT,
            includeFilterOptions = true,
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
            parkId = _selectedParkId.value,
            asOf = workWindow.asOf,
            dueBefore = workWindow.dueBefore,
            openOnly = OPEN_ONLY_QUERY,
            limit = PAGE_LIMIT,
            includeFilterOptions = true,
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
            is ShedsEvent.SelectPark -> selectPark(event.parkId)
            is ShedsEvent.OpenShedRecord -> Unit // navigation — handled by the nav host.
            ShedsEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun selectDay(dateKey: String) {
        val date = parseExecutionDate(dateKey) ?: return
        // The strip runs firstDay (yesterday) .. lastDay (today+5); every rendered tab,
        // including yesterday, must be selectable.
        if (date < workWindow.firstDay || date > workWindow.lastDay) return
        _selectedDay.value = date
    }

    private fun selectPark(parkId: String?) {
        val normalized = parkId?.takeIf { it.isNotBlank() }
        if (_selectedParkId.value == normalized) return
        nextCursor = null
        _selectedParkId.value = normalized
        refresh()
    }

    private fun VaccinationExecutionResponseDto.toShedsUiState(selectedDay: LocalDate): ShedsUiState? {
        val base = sampleShedsState()
        val weekRows = rows
        val rowsForSelectedDay = weekRows.filter { row ->
            val dueDate = row.currentScheduleDate?.let(::parseExecutionDate)
            val hasVisibleWork = row.hasOperatorVisibleWork()
            when {
                !hasVisibleWork -> false
                dueDate == null -> selectedDay == workWindow.today
                // Today folds in the deep backlog (due strictly before the visible yesterday
                // tab) plus today's own open/review work; completed rows stay on their actual
                // scheduled day so finished shed cards do not disappear or flood today's list.
                selectedDay == workWindow.today ->
                    dueDate.isEqual(workWindow.today) ||
                        (dueDate.isBefore(workWindow.firstDay) && row.hasOpenOrReviewWork())
                else -> dueDate == selectedDay
            }
        }
        val shedRows = rowsForSelectedDay.groupBy { it.executionIdentity() }.map { (identity, group) ->
            val first = group.first()
            val scheduleDate = group.mapNotNull { it.currentScheduleDate?.let(::parseExecutionDate) }.minOrNull()
            val status = shedStatusForRows(group)
            val counts = executionCounts(group)
            val vaccineGroups = group.groupBy { humanizeVaccineLabel(it.driveName.orEmpty()) }
                .filterKeys { it.isNotBlank() }
                .map { (label, driveRows) ->
                    val driveCounts = executionCounts(driveRows)
                    VaccineGroup(
                        label = label,
                        countLabel = "${driveCounts.done}/${driveCounts.target}",
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
                statusLabel = group.reviewAwareStatusLabel(status),
                statusChips = group.statusChips(status),
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
        val visibleWindowTotals = executionCounts(adherenceWindowRows(weekRows, rowsForSelectedDay, selectedDay))
        val pageComplete = nextCursor.isNullOrBlank()
        // Backend-owned "vaccines to carry" for the selected day (full-day, page-independent).
        // The screen renders these numbers verbatim — no client-side summing of shed rows.
        val selectedKey = selectedDay.toString()
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
            shedCount = rowsForSelectedDay.map { it.shedId }.distinct().size,
            dueCount = if (pageComplete) totals.open else 0,
            doneCount = if (pageComplete) totals.done else 0,
            shedCountLabel = "${shedRows.size} sheds",
            dueLabel = if (pageComplete) "${totals.open} open" else "More rows available",
            dayProgressLabel = if (pageComplete) percentLabel(totals.done, totals.target) else "",
            dayProgressFraction = if (pageComplete) fraction(totals.done, totals.target) else 0f,
            daySummary = if (pageComplete) "${totals.done} / ${totals.target} done" else "Load all rows for full-day totals",
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
            adherence = if (pageComplete) protocolAdherenceSummary(adherenceWindowRows(weekRows, rowsForSelectedDay, selectedDay), visibleWindowTotals) else null,
            dayTabs = buildOperatorDayTabs(weekRows, workWindow, selectedDay),
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
}

internal data class ExecutionCounts(val target: Int, val open: Int, val done: Int)

private data class ShedsTransientState(
    val selectedDay: LocalDate,
    val isRefreshing: Boolean,
    val isOffline: Boolean,
    val isLoadingMore: Boolean,
)

internal fun protocolAdherenceSummary(counts: ExecutionCounts): ProtocolAdherenceSummary? =
    protocolAdherenceSummary(emptyList(), counts)

internal fun protocolAdherenceSummary(
    rows: List<VaccinationExecutionRowDto>,
    counts: ExecutionCounts = executionCounts(rows),
): ProtocolAdherenceSummary? {
    if (counts.target <= 0 && counts.done <= 0 && counts.open <= 0) return null
    val accepted = rows.sumOf { row ->
        if (row.isAcceptedForProtocolSummary()) row.doneCount.coerceAtLeast(0) else 0
    }
    val review = rows.count { it.isVerificationPending() }
    return ProtocolAdherenceSummary(
        expectedCount = counts.target,
        submittedCount = counts.done,
        acceptedCount = accepted,
        reviewItemCount = review,
        overdueItemCount = rows.count { it.isOverdueWork() },
        deferredCount = 0,
        acceptedPercent = if (counts.target > 0) (accepted * 100 / counts.target).coerceIn(0, 100) else 0,
    )
}

internal fun shedStatusForRows(rows: List<VaccinationExecutionRowDto>): ShedStatus {
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

/** Execution API rows are aggregated groups. Counts must come from the backend fields, never
 * from List.size (which undercounted a two-goat shed as one because it had one grouped row). */
internal fun executionCounts(rows: List<VaccinationExecutionRowDto>): ExecutionCounts =
    ExecutionCounts(
        target = rows.sumOf { it.targetCount.coerceAtLeast(0) },
        open = rows.sumOf { it.openCount.coerceAtLeast(0) },
        done = rows.sumOf { it.doneCount.coerceAtLeast(0) },
    )

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
}

private fun ShedStatus.toChipTone(): ShedStatusTone = when (this) {
    ShedStatus.DONE -> ShedStatusTone.OK
    ShedStatus.PENDING -> ShedStatusTone.WARN
    ShedStatus.DELAYED -> ShedStatusTone.DANGER
}

private fun VaccinationExecutionRowDto.hasOperatorVisibleWork(): Boolean =
    hasOpenOrReviewWork() || doneCount > 0

private fun VaccinationExecutionRowDto.hasOpenOrReviewWork(): Boolean =
    openCount > 0 || isVerificationPending() || (doneCount > 0 && !isFinalClosed())

private fun VaccinationExecutionRowDto.isVerificationPending(): Boolean =
    proofStatus.equals("uploaded", ignoreCase = true) ||
        verificationStatus.equals("pending", ignoreCase = true) ||
        sopStatus.equals("submitted", ignoreCase = true) ||
        sopStatus.equals("needs_review", ignoreCase = true) ||
        workState.equals("verification_pending", ignoreCase = true)

private fun VaccinationExecutionRowDto.isOverdueWork(): Boolean {
    val work = workState.lowercase()
    if (work.contains("overdue") || work.contains("missed")) return true
    if (isFinalClosed()) return false
    val scheduleDate = currentScheduleDate
        ?.takeIf { it.isNotBlank() }
        ?.let { runCatching { LocalDate.parse(it) }.getOrNull() }
        ?: return false
    return scheduleDate.isBefore(LocalDate.now(ZoneId.systemDefault()))
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

private fun VaccinationExecutionRowDto.isFinalClosed(): Boolean = when (sopStatus.lowercase()) {
    "accepted", "closed", "completed" -> true
    else -> workState.equals("accepted", ignoreCase = true) ||
        workState.equals("closed", ignoreCase = true) ||
        workState.equals("completed", ignoreCase = true)
}

internal fun List<VaccinationExecutionRowDto>.opensSubmittedRecordOnly(): Boolean =
    isNotEmpty() && all { row -> row.hasSubmittedRecord() }

private fun VaccinationExecutionRowDto.hasSubmittedRecord(): Boolean =
    sopStatus.isSubmissionTerminalStatus() ||
        verificationStatus.equals("pending", ignoreCase = true) ||
        verificationStatus.equals("accepted", ignoreCase = true) ||
        verificationStatus.equals("verified", ignoreCase = true) ||
        proofStatus.equals("uploaded", ignoreCase = true) ||
        workState.equals("verification_pending", ignoreCase = true)

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
