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
            Resource()
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
    }

    fun loadMore() = viewModelScope.launch {
        val cursor = nextCursor ?: return@launch
        if (_isLoadingMore.value) return@launch
        _isLoadingMore.value = true
        val result = repo.appendRows(cursor = cursor, limit = PAGE_LIMIT)
        _isLoadingMore.value = false
        _isOffline.value = result.isFailure
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
            val total = group.size
            val doneCount = group.count { it.isDone() }
            val vaccineGroups = group
                .mapNotNull { it.driveName }
                .distinct()
                .map { VaccineGroup(label = humanizeDriveName(it), countLabel = "", full = false) }
            ShedRow(
                id = identity.cardId,
                name = first.shedName,
                cohort = first.animalStage.ifBlank { first.driveName.orEmpty() },
                status = status,
                statusLabel = first.workState.ifBlank { status.readable() }.let { it.readableState() },
                vaccineGroups = vaccineGroups,
                inShed = total.toString(),
                due = total.toString(),
                done = doneCount.toString(),
                progressLabel = percentLabel(doneCount, total),
                progressFraction = fraction(doneCount, total),
                shedId = identity.shedId,
                driveId = identity.driveId,
                batchId = identity.batchId,
                taskId = identity.taskId,
                sopVersionId = identity.sopVersionId,
                taskRowVersion = identity.taskRowVersion,
            )
        }
        val totalDue = rows.size
        val totalDone = rows.count { it.isDone() }
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
            dueCount = totalDue,
            doneCount = totalDone,
            shedCountLabel = "${shedRows.size} sheds",
            dueLabel = "$totalDue due",
            dayProgressLabel = percentLabel(totalDone, totalDue),
            dayProgressFraction = fraction(totalDone, totalDue),
            daySummary = "$totalDone / $totalDue done",
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
        val allDone = rows.all { it.isDone() }
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

private data class ExecutionIdentity(
    val shedId: String,
    val driveId: String?,
    val batchId: String?,
    val taskId: String?,
    val sopVersionId: String?,
    val taskRowVersion: Int?,
) {
    val cardId: String = taskId ?: batchId ?: driveId ?: "shed:$shedId"
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
private fun humanizeDriveName(raw: String): String {
    val trimmed = raw.trim().takeIf { it.isNotBlank() } ?: return ""

    // Strip the prefix (take part after last " - ")
    val suffix = if (" - " in trimmed) trimmed.substringAfterLast(" - ") else trimmed
    if (suffix.isBlank()) return ""

    // Parse antigen and dose: split on the last underscore that precedes _first/_booster
    val (antigen, doseSuffix) = when {
        suffix.endsWith("_first") -> suffix.dropLast(6) to "first"
        suffix.endsWith("_booster") -> suffix.dropLast(8) to "booster"
        else -> suffix to ""
    }

    if (antigen.isBlank()) return ""

    // Map antigen token to display name
    val antigenLabel = when (antigen.lowercase()) {
        "hs" -> "HS"
        "ppr" -> "PPR"
        "fmd" -> "FMD"
        "et_tt" -> "ET+TT"
        "sheep_pox" -> "Sheep Pox"
        "blue_tongue" -> "Blue Tongue"
        "goat_pox" -> "Goat Pox"
        else -> antigen.replace('_', ' ').replaceFirstChar { it.uppercase() }
    }

    // Append dose suffix if present
    return if (doseSuffix == "booster") "$antigenLabel · Booster" else antigenLabel
}
