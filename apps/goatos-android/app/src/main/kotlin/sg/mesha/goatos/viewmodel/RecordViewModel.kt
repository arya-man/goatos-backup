package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.feature.record.RecordEvent
import sg.mesha.goatos.feature.record.RecordTone
import sg.mesha.goatos.feature.record.RecordUiState
import sg.mesha.goatos.feature.record.VaccineGroupRow
import sg.mesha.goatos.ui.sampleRecordState
import javax.inject.Inject

/**
 * Read-only shed/drive record state holder. Shows a loading placeholder first, then loads the
 * real drilldown for the `shedId` nav arg (read from [SavedStateHandle]) via
 * [ExecutionRepository.shed]; only when no shed id was supplied does it fall back to the first
 * execution shed. Mapped in [toRecordUiState]. An empty/failed load shows an honest empty/error
 * state — the sample record is NEVER shown as if it were live data. The record is read-only;
 * the only event ([RecordEvent.Close]) is navigation.
 */
@HiltViewModel
class RecordViewModel @Inject constructor(
    private val repo: ExecutionRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")

    private val _state = MutableStateFlow(recordPlaceholder("Loading record…"))
    val state: StateFlow<RecordUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching {
            val id = shedId ?: repo.rows().rows.firstOrNull()?.shedId
            id?.let { repo.shed(it) }
        }
            .onSuccess { drilldown ->
                _state.value = drilldown?.toRecordUiState() ?: recordPlaceholder("No record to show for this shed.")
            }
            .onFailure { _state.value = recordPlaceholder("Couldn't load this record right now.") }
    }

    fun onEvent(event: RecordEvent) {
        when (event) {
            RecordEvent.Close -> Unit // navigation — handled by the nav host.
        }
    }

    private fun VaccinationExecutionShedDrilldownDto.toRecordUiState(): RecordUiState {
        val base = sampleRecordState()
        val groups = drives.map { drive ->
            VaccineGroupRow(
                vaccine = drive.driveName ?: drive.driveId.orEmpty(),
                given = summary.completed,
                due = summary.total,
                dose = "",
            )
        }
        val complete = summary.total > 0 && summary.completed >= summary.total
        return base.copy(
            title = "$shedName · record",
            subtitle = "${summary.completed} / ${summary.total} done",
            // Real drives only — a shed with no drives renders empty, never the sample rows.
            groups = groups,
            countLabel = "${summary.completed} doses",
            statusLabel = if (complete) "Done" else "In progress",
            statusTone = if (complete) RecordTone.OK else RecordTone.WARN,
        )
    }

    // Honest non-live state: reuse the sample only for stable chrome, clear the fabricated
    // drive rows, and carry the real message in the subtitle.
    private fun recordPlaceholder(message: String): RecordUiState = sampleRecordState().copy(
        subtitle = message,
        groups = emptyList(),
        countLabel = "",
        statusLabel = "",
        statusTone = RecordTone.WARN,
    )
}
