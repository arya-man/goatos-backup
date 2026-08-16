package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.isConnectivityFailure
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

    // Track refresh state separately so we can show "syncing" while cached data persists
    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    // A background refresh failed; folded into [state] below via [combine]. Reset to false
    // whenever a refresh succeeds.
    private val _isOffline = MutableStateFlow(false)

    // Resolved once [state]'s flow starts running (see [roomFlow]); lets a manual
    // [TimetableEvent.Refresh] target the same center without re-resolving the bootstrap
    // profile mid-subscription.
    @Volatile
    private var resolvedCenterId: String? = null

    /**
     * MOB-010 fix: [state] is the SOLE subscriber of the Room [RosterRepository.observeTimetable]
     * flow — it is fed directly by `stateIn(...WhileSubscribed(5_000)...)` with no permanent
     * secondary `.collect` underneath it, so backgrounding the screen for 5s truly stops
     * collecting Room (the prior forever-`collect` inside a `viewModelScope.launch` made
     * `WhileSubscribed` a no-op). [onStart] on the observed flow fires exactly once per
     * subscription activation (not once per Room emission), so a successful background
     * refresh's Room re-emit no longer re-triggers another refresh — the old
     * refresh -> Room re-emit -> refresh forever loop is gone.
     */
    val state: StateFlow<TimetableUiState> = roomFlow()
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), TimetableUiState())

    fun onEvent(event: TimetableEvent) {
        when (event) {
            TimetableEvent.Refresh -> resolvedCenterId?.let { centerId ->
                if (_isRefreshing.value.not()) refreshInBackground(centerId)
            }
        }
    }

    /**
     * Resolves the operator's center, then observes Room cache for that center
     * (stale-while-revalidate). A principal with no center assigned gets an honest
     * `no_center` error and the flow completes without ever touching [repo].
     */
    private fun roomFlow(): Flow<TimetableUiState> = flow {
        val centerId = runCatching { bootstrap.operatorProfile()?.primaryLocationId }.getOrNull()
        if (centerId.isNullOrBlank()) {
            emit(TimetableUiState(errorCode = "no_center"))
            return@flow
        }
        resolvedCenterId = centerId

        emitAll(
            combine(
                repo.observeTimetable(centerId).onStart { refreshInBackground(centerId) },
                _isOffline,
            ) { dto, isOffline ->
                when {
                    // Cache hit: render cached/fresh data; isOffline reflects the last refresh.
                    dto != null -> dto.toTimetableUiState().copy(isOffline = isOffline)
                    // Cache miss and the background refresh failed: honest load error.
                    isOffline -> TimetableUiState(errorCode = "load_failed")
                    // Cache miss, refresh pending or succeeded with nothing to show: honest empty.
                    else -> TimetableUiState(errorCode = null)
                }
            }
        )
    }

    private fun refreshInBackground(centerId: String) = viewModelScope.launch {
        _isRefreshing.value = true
        val result = repo.refreshTimetable(centerId)
        _isOffline.value = result.exceptionOrNull().isConnectivityFailure()
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
