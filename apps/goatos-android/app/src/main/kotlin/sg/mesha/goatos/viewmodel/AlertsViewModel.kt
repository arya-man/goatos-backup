package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsEvent
import sg.mesha.goatos.feature.profile.AlertsUiState
import sg.mesha.goatos.ui.alertsPlaceholder
import sg.mesha.goatos.ui.sampleAlertsState
import javax.inject.Inject

/**
 * Alerts / notifications state holder — the offline-first REFERENCE for every other screen-read
 * ViewModel (docs/decisions/android-offline-first.md). Room is the UI's single source of
 * truth: [state] is fed by [ControlTowerRepository.observeSummary], a cache-first [kotlinx.coroutines.flow.Flow]
 * that emits instantly from Room (cached data survives process restarts and screen
 * re-entry) and re-emits the moment a background [ControlTowerRepository.refreshSummary] upserts
 * new data. [refresh] never writes into [state] directly — it only drives the network call
 * and the transient [AlertsUiState.isRefreshing]/[AlertsUiState.isOffline] flags; the
 * DTO -> UiState mapping in [toAlertsUiState] is unchanged from the network-only version.
 * An empty real response shows an honest empty state; a refresh failure with NO cache ever
 * observed shows an honest error state; a refresh failure WITH cached data keeps rendering
 * that cache and only flips [AlertsUiState.isOffline] — never a blank/loading wall.
 * Rows are marked read locally so the list feels live: [AlertsEvent.MarkAllRead] clears
 * every unread flag, [AlertsEvent.OpenAlert] clears the tapped row.
 */
@HiltViewModel
class AlertsViewModel @Inject constructor(
    private val repo: ControlTowerRepository,
) : ViewModel() {

    // Upstream Room flow, lifecycle-aware via WhileSubscribed(5_000)
    private val observedResource: StateFlow<Resource<ControlTowerResponseDto>> =
        repo.observeSummary().stateIn(
            viewModelScope,
            SharingStarted.WhileSubscribed(5_000),
            Resource()
        )

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _localReadState = MutableStateFlow<Set<String>>(emptySet())

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<AlertsUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline,
        _localReadState
    ) { resource, isRefreshing, isOffline, readSet ->
        val dto = resource.data
        val base = dto?.toAlertsUiState()
            ?: if (resource.hasData) alertsPlaceholder("No alerts") else alertsPlaceholder("Loading…")
        base.copy(
            rows = base.rows.map { it.copy(unread = it.id !in readSet) },
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        alertsPlaceholder("Loading alerts…")
    )

    init {
        refresh()
    }

    /** Network side of stale-while-revalidate: upserts Room on success (the [observedResource]
     *  StateFlow re-emits and updates [state]); on failure it only flips [isOffline] —
     *  cached content, if any, stays on screen. */
    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        val result = repo.refreshSummary()
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
    }

    fun onEvent(event: AlertsEvent) {
        when (event) {
            AlertsEvent.MarkAllRead -> {
                // Mark all rows as read in local state (not persisted to backend)
                _localReadState.value = state.value.rows.mapNotNull {
                    if (it.unread) it.id else null
                }.toSet()
            }
            is AlertsEvent.OpenAlert -> {
                // Mark tapped row as read in local state
                _localReadState.value = _localReadState.value + event.id
            }
            AlertsEvent.Refresh -> refresh()
        }
    }

    private fun ControlTowerResponseDto.toAlertsUiState(): AlertsUiState? {
        if (alerts.isEmpty()) return null
        val base = sampleAlertsState()
        val rows = alerts.map { alert ->
            AlertRow(
                id = alert.rowId,
                title = alert.title,
                body = alert.detail,
                timeLabel = "",
                tone = when (alert.severity.lowercase()) {
                    "critical", "high" -> AlertTone.CRITICAL
                    "warn", "warning" -> AlertTone.WARN
                    else -> AlertTone.INFO
                },
                unread = true,
            )
        }
        return base.copy(rows = rows)
    }
}
