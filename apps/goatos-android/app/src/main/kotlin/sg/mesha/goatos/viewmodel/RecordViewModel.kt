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
 * Read-only shed/drive record state holder. Seeds the interim [sampleRecordState] fixture
 * for an instant first frame. The shed to open is passed as the `shedId` nav arg (read from
 * [SavedStateHandle]); it loads THAT shed's drilldown via [ExecutionRepository.shed]. Only
 * when no shed id was supplied does it fall back to the first execution shed. Mapped in
 * [toRecordUiState]. On error/empty the sample is kept. The record is read-only; the only
 * event ([RecordEvent.Close]) is navigation.
 */
@HiltViewModel
class RecordViewModel @Inject constructor(
    private val repo: ExecutionRepository,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val shedId: String? = savedStateHandle.get<String>("shedId")

    private val _state = MutableStateFlow(sampleRecordState())
    val state: StateFlow<RecordUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching {
            val id = shedId ?: repo.rows().rows.firstOrNull()?.shedId
            id?.let { repo.shed(it) }
        }
            .onSuccess { drilldown -> drilldown?.toRecordUiState()?.let { _state.value = it } }
            .onFailure { /* keep the sample so the screen is never blank */ }
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
            groups = groups.ifEmpty { base.groups },
            countLabel = "${summary.completed} doses",
            statusLabel = if (complete) "Done" else "In progress",
            statusTone = if (complete) RecordTone.OK else RecordTone.WARN,
        )
    }
}
