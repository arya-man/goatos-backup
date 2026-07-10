package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.dto.PositionDto
import sg.mesha.goatos.core.network.dto.PositionListResponseDto
import sg.mesha.goatos.feature.timetable.PositionTier
import sg.mesha.goatos.feature.timetable.TimetableEvent
import sg.mesha.goatos.feature.timetable.TimetableRow
import sg.mesha.goatos.feature.timetable.TimetableUiState
import javax.inject.Inject

/**
 * Timetable (HRMS shift roster) screen state holder. READ-ONLY mirror of the web
 * Position & Coverage roster (docs/hr/roster-rbac-design.md) — the app never writes
 * positions/leave/backups; all CRUD stays web-only (TRD §14). Loads via
 * [RosterRepository.positions] (no scope filter: the backend already scopes the
 * response to what this principal is authorized to see, so the client never guesses a
 * center/tenant scope) and maps each `Position` 1:1 into a [TimetableRow] — the only
 * "logic" here is glue-mapping backend enums (tier/week-off day/status) to short
 * labels, the same allowance AlertsScreen's tone -> pill mapping gets.
 *
 * OPEN QUESTION (handoff): the `Position` schema (contracts/openapi/admin-api.yaml)
 * exposes only `workforce_member_id` (a raw UUID) for the seat holder — there is no
 * display-name lookup endpoint yet (no `workforce_members` list/read route), so
 * [TimetableRow.holderId] renders the raw id verbatim. It also has no shift-TIME field
 * (only `week_off_weekday`), so the mock's "Shift" column (a clock time) is not
 * rendered — inventing one would violate the no-fake-data rule. Recommend the backend
 * add a resolved `holder_display_name` (or a members lookup) and, if a shift-time
 * concept is still wanted on mobile, a `shift_start_at`/`shift_end_at` pair on Position.
 */
@HiltViewModel
class TimetableViewModel @Inject constructor(
    private val repo: RosterRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(TimetableUiState(subtitle = "Loading…"))
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
        runCatching { repo.positions() }
            .onSuccess { dto -> _state.value = dto.toTimetableUiState() }
            .onFailure { _state.value = TimetableUiState(errorLabel = "Couldn't load the timetable right now.") }
    }
}

private fun PositionListResponseDto.toTimetableUiState(): TimetableUiState = TimetableUiState(
    subtitle = "Shift roster — the operational source for who executes each day.",
    rows = items.map { it.toTimetableRow() },
    emptyLabel = "No positions configured",
)

private fun PositionDto.toTimetableRow(): TimetableRow = TimetableRow(
    id = positionId,
    positionLabel = positionCode.ifBlank { "Position" },
    tier = when (positionTier) {
        "assistant" -> PositionTier.ASSISTANT
        "manager" -> PositionTier.MANAGER
        "head" -> PositionTier.HEAD
        "director" -> PositionTier.DIRECTOR
        "cxo" -> PositionTier.CXO
        else -> PositionTier.UNKNOWN
    },
    holderId = workforceMemberId,
    weekOffLabel = weekOffWeekday?.replaceFirstChar { it.uppercase() }?.take(3) ?: "—",
    backupLabel = backupGroupCode ?: if (isBackupSlot) "Backup slot" else "—",
    statusLabel = status.replaceFirstChar { it.uppercase() }.ifBlank { "—" },
    isActive = status.equals("active", ignoreCase = true),
)
