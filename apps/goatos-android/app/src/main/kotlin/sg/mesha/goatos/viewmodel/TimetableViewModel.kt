package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
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
 * positions/leave/backups; all CRUD stays web-only (TRD §14).
 *
 * Offline-first (docs/decisions/android-offline-first.md): observes Room cache via
 * [RosterRepository.observeTimetable]; refresh via [RosterRepository.refreshTimetable]
 * runs in the background. A failed refresh keeps cached data on screen and sets
 * `isOffline=true` to show a sync indicator. An empty cache on cold start is honest.
 *
 * Loads via `GET /app/roster/timetable` (operator-scoped — never the admin `/admin/roster`
 * surface, which is RosterRead-gated and 403s for operators) and maps each `EnrichedPosition`
 * 1:1 into a [TimetableRow] — the only "logic" here is glue-mapping backend enums
 * (tier/status) to short labels, the same allowance AlertsScreen's tone -> pill mapping gets.
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

    // Track refresh state separately so we can show "syncing" while cached data persists
    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

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

        // Start observing Room cache immediately (stale-while-revalidate)
        repo.observeTimetable(centerId).collect { dto ->
            _state.update { current ->
                if (dto != null) {
                    // Cache hit: render cached data (refresh flag is separate)
                    dto.toTimetableUiState()
                } else if (current.rows.isEmpty() && _isRefreshing.value.not()) {
                    // Cache miss on first load and not currently refreshing: show empty
                    TimetableUiState(errorCode = null)
                } else {
                    // Keep prior state while refreshing
                    current
                }
            }

            // Trigger background refresh (stale-while-revalidate pattern)
            if (_isRefreshing.value.not()) {
                refreshInBackground(centerId)
            }
        }
    }

    private fun refreshInBackground(centerId: String) = viewModelScope.launch {
        _isRefreshing.value = true
        runCatching {
            repo.refreshTimetable(centerId)
        }.onFailure {
            // Keep cached data on screen, set offline flag for sync indicator
            _state.update { current ->
                if (current.rows.isNotEmpty()) {
                    // Refresh failed but we have cached rows: show as stale/offline
                    current.copy(isOffline = true, errorCode = null)
                } else {
                    // No cached data and refresh failed: show honest error
                    TimetableUiState(errorCode = "load_failed")
                }
            }
        }
        _isRefreshing.value = false
    }
}

private fun EnrichedPositionListResponseDto.toTimetableUiState(): TimetableUiState = TimetableUiState(
    rows = items.map { it.toTimetableRow() },
    errorCode = null,
    isOffline = false,  // Refresh succeeded or this is cached, so not offline
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
