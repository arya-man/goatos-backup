package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.dto.EnrichedPositionDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.feature.timetable.PositionTier
import sg.mesha.goatos.feature.timetable.TimetableEvent
import sg.mesha.goatos.feature.timetable.TimetableRow
import sg.mesha.goatos.feature.timetable.TimetableUiState
import javax.inject.Inject

/**
 * Timetable (HRMS shift roster) screen state holder. READ-ONLY mirror of the operator's
 * center Position & Coverage roster (docs/hr/roster-rbac-design.md) — the app never writes
 * positions/leave/backups; all CRUD stays web-only (TRD §14). Loads via
 * [RosterRepository.timetable] (`GET /app/roster/timetable`, operator-scoped — never the
 * admin `/admin/roster` surface, which is RosterRead-gated and 403s for operators) and
 * maps each `EnrichedPosition` 1:1 into a [TimetableRow] — the only "logic" here is
 * glue-mapping backend enums (tier/status) to short labels, the same allowance
 * AlertsScreen's tone -> pill mapping gets.
 *
 * The `center_id` query param comes from the bootstrap operator profile's
 * `primary_location_id` (the principal's HR center scope). A principal with no center
 * assigned (e.g. an all-parks leadership profile with no HR seat) gets an honest empty
 * state rather than a guessed scope.
 */
@HiltViewModel
class TimetableViewModel @Inject constructor(
    private val repo: RosterRepository,
    private val bootstrap: BootstrapRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(TimetableUiState())
    val state: StateFlow<TimetableUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun onEvent(event: TimetableEvent) {
        when (event) {
            TimetableEvent.Refresh -> load()
        }
    }

    private fun load() = viewModelScope.launch {
        val centerId = runCatching { bootstrap.operatorProfile()?.primaryLocationId }.getOrNull()
        if (centerId.isNullOrBlank()) {
            _state.value = TimetableUiState(errorCode = "no_center")
            return@launch
        }
        runCatching { repo.timetable(centerId) }
            .onSuccess { dto -> _state.value = dto.toTimetableUiState() }
            .onFailure { _state.value = TimetableUiState(errorCode = "load_failed") }
    }
}

private fun EnrichedPositionListResponseDto.toTimetableUiState(): TimetableUiState = TimetableUiState(
    rows = items.map { it.toTimetableRow() },
)

private fun EnrichedPositionDto.toTimetableRow(): TimetableRow = TimetableRow(
    id = positionId,
    positionLabel = positionCode.ifBlank { "" },
    positionLabelFallback = positionCode.isBlank(),
    tier = when (positionTier) {
        "assistant" -> PositionTier.ASSISTANT
        "manager" -> PositionTier.MANAGER
        "head" -> PositionTier.HEAD
        "director" -> PositionTier.DIRECTOR
        "cxo" -> PositionTier.CXO
        else -> PositionTier.UNKNOWN
    },
    holderName = personDisplayName?.ifBlank { null },
    weekOffLabel = weekOff?.ifBlank { null }?.replaceFirstChar { it.uppercase() }?.take(3) ?: "",
    weekOffLabelFallback = weekOff.isNullOrBlank(),
    backupLabel = backupGroup?.ifBlank { null } ?: "",
    backupLabelFallback = backupGroup.isNullOrBlank(),
    isBackupSlot = isBackupSlot,
    statusLabel = status.replaceFirstChar { it.uppercase() },
    statusLabelFallback = status.isBlank(),
    isActive = status.equals("active", ignoreCase = true),
)
