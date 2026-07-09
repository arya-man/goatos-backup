package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
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
 * Today's-sheds / drive-status state holder. Shows a loading placeholder first, then loads
 * the real vaccination execution rows via [ExecutionRepository.rows] and groups them into
 * one [ShedRow] per shed in [toShedsUiState]. An empty real response shows an honest empty
 * state and a failure shows an honest error state — fabricated sheds are NEVER shown as if
 * they were live. [ShedsEvent.Refresh] reloads; [ShedsEvent.OpenShedRecord] is navigation,
 * routed by the host.
 */
@HiltViewModel
class ShedsViewModel @Inject constructor(
    private val repo: ExecutionRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(shedsPlaceholder("Loading today's sheds…"))
    val state: StateFlow<ShedsUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.rows() }
            .onSuccess { dto -> _state.value = dto.toShedsUiState() ?: shedsPlaceholder("No sheds scheduled today") }
            .onFailure { _state.value = shedsPlaceholder("Couldn't load today's sheds. Tap refresh to retry.") }
    }

    fun onEvent(event: ShedsEvent) {
        when (event) {
            ShedsEvent.Refresh -> load()
            is ShedsEvent.OpenShedRecord -> Unit // navigation — handled by the nav host.
        }
    }

    private fun VaccinationExecutionResponseDto.toShedsUiState(): ShedsUiState? {
        if (rows.isEmpty()) return null
        val base = sampleShedsState()
        val shedRows = rows.groupBy { it.shedId }.map { (_, group) ->
            val first = group.first()
            val status = shedStatusFor(group)
            val total = group.size
            val doneCount = group.count { it.isDone() }
            val vaccineGroups = group
                .mapNotNull { it.driveName }
                .distinct()
                .map { VaccineGroup(label = it, countLabel = "", full = false) }
            ShedRow(
                id = first.shedId,
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

private fun ShedStatus.readable(): String = name.lowercase().replaceFirstChar { it.uppercase() }

private fun String.readableState(): String =
    replace('_', ' ').replaceFirstChar { it.uppercase() }
