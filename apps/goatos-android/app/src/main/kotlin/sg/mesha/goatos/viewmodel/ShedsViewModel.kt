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
import javax.inject.Inject

/**
 * Today's-sheds / drive-status state holder. Seeds the interim [sampleShedsState]
 * fixture for an instant first frame, then loads the real vaccination execution rows via
 * [ExecutionRepository.rows] and groups them into one [ShedRow] per shed in
 * [toShedsUiState]. On error/empty the sample is kept. [ShedsEvent.Refresh] reloads;
 * [ShedsEvent.OpenShedRecord] is navigation, routed by the host.
 */
@HiltViewModel
class ShedsViewModel @Inject constructor(
    private val repo: ExecutionRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(sampleShedsState())
    val state: StateFlow<ShedsUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.rows() }
            .onSuccess { dto -> dto.toShedsUiState()?.let { _state.value = it } }
            .onFailure { /* keep the sample so the screen is never blank */ }
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
        val shedRows = rows.groupBy { it.shedId }.map { (_, shedRows) ->
            val first = shedRows.first()
            val status = shedStatusFor(shedRows)
            val vaccineGroups = shedRows
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
                inShed = shedRows.size.toString(),
                due = shedRows.size.toString(),
                done = "0",
                progressLabel = "0%",
                progressFraction = 0f,
            )
        }
        return base.copy(
            title = "Today's sheds",
            shedCountLabel = "${shedRows.size} sheds",
            rows = shedRows,
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
        val allDone = rows.all { row ->
            val work = row.workState.lowercase()
            work.contains("completed") || work.contains("done")
        }
        return if (allDone) ShedStatus.DONE else ShedStatus.PENDING
    }
}

private fun ShedStatus.readable(): String = name.lowercase().replaceFirstChar { it.uppercase() }

private fun String.readableState(): String =
    replace('_', ' ').replaceFirstChar { it.uppercase() }
