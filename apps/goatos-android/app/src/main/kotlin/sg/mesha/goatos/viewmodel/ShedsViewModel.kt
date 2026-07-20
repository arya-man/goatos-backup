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
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedsEvent
import sg.mesha.goatos.feature.sheds.ShedsUiState
import sg.mesha.goatos.feature.sheds.VaccineGroup
import sg.mesha.goatos.ui.sampleShedsState
import sg.mesha.goatos.ui.shedsPlaceholder
import javax.inject.Inject

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

    private companion object {
        const val PAGE_LIMIT = 20
    }

    private var nextCursor: String? = null

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<VaccinationExecutionResponseDto>> =
        repo.observeRows(limit = PAGE_LIMIT).stateIn(
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
        _isRefreshing,
        _isOffline,
        _isLoadingMore
    ) { resource, isRefreshing, isOffline, isLoadingMore ->
        val dto = resource.data
        nextCursor = dto?.nextCursor  // Update pagination cursor for loadMore()
        val base = dto?.toShedsUiState()
            ?: if (resource.hasData) shedsPlaceholder("No sheds scheduled today") else shedsPlaceholder("Loading…")
        base.copy(
            isRefreshing = isRefreshing,
            isLoadingMore = isLoadingMore,
            hasMore = !dto?.nextCursor.isNullOrBlank(),
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        shedsPlaceholder("Loading today's sheds…")
    )

    init {
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observeRows]
     *  collector above re-emits and updates [state]); on failure it only flips
     *  [ShedsUiState.isOffline] — cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        val result = repo.refreshRows(limit = PAGE_LIMIT)
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
        val result = repo.appendRows(cursor = cursor, limit = PAGE_LIMIT)
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
            is ShedsEvent.OpenShedRecord -> Unit // navigation — handled by the nav host.
            ShedsEvent.Back -> Unit // navigation — handled by the nav host.
        }
    }

    private fun VaccinationExecutionResponseDto.toShedsUiState(): ShedsUiState? {
        if (rows.isEmpty()) return null
        val base = sampleShedsState()
        val shedRows = rows.groupBy { it.executionIdentity() }.map { (identity, group) ->
            val first = group.first()
            val status = shedStatusFor(group)
            val counts = executionCounts(group)
            val vaccineGroups = group.groupBy { it.driveName.orEmpty() }
                .filterKeys { it.isNotBlank() }
                .map { (label, driveRows) ->
                    val driveCounts = executionCounts(driveRows)
                    VaccineGroup(
                        label = label,
                        countLabel = "${driveCounts.done}/${driveCounts.target}",
                        full = driveCounts.target > 0 && driveCounts.done >= driveCounts.target,
                    )
                }
            ShedRow(
                id = identity.cardId,
                name = first.shedName,
                // animalStage is a biological stage supplied by the execution contract.
                // A drive label is not a cohort/stage and must not be substituted here.
                animalStage = first.animalStage,
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
        }
        val totals = executionCounts(rows)
        return base.copy(
            title = "Today's sheds",
            // Header scope + the drive date/window are not carried by the execution
            // list endpoint — leave them blank (the screen skips blank meta) rather
            // than inheriting the sample's fabricated values.
            scopeLabel = "",
            date = "",
            window = "",
            // Raw counts — the screen formats + localizes these via *_fmt resources
            // (counts are UI chrome, not backend-owned copy). The label strings below
            // are kept only as a fallback for non-VM sources (placeholder/sample).
            shedCount = rows.map { it.shedId }.distinct().size,
            dueCount = totals.open,
            doneCount = totals.done,
            shedCountLabel = "${shedRows.size} sheds",
            dueLabel = "${totals.open} open",
            dayProgressLabel = percentLabel(totals.done, totals.target),
            dayProgressFraction = fraction(totals.done, totals.target),
            daySummary = "${totals.done} / ${totals.target} done",
            caption = null,
            roleNote = null,
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

/**
 * Humanize raw driveName strings for display in vaccine group chips.
 *
 * Raw inputs like "Preventive Care Vaccination Matrix - hs_first" or "… - ppr_booster"
 * are transformed to human-readable labels like "HS", "PPR · Booster".
 *
 * Rules:
 * - Strip leading prefix (take the part after the last " - ").
 * - Parse antigen token (before the dose suffix) and map to display name.
 * - Dose suffix: _first → no suffix (default), _booster → append " · Booster".
 * - Unknown tokens are Title-Cased with underscores replaced by spaces.
 */
