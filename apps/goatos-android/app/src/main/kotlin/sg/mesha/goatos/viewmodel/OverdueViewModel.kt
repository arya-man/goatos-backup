package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.OverdueClassification
import sg.mesha.goatos.feature.leadership.OverdueRow
import sg.mesha.goatos.feature.leadership.OverdueUiState
import sg.mesha.goatos.ui.overduePlaceholder
import sg.mesha.goatos.ui.sampleOverdueState
import javax.inject.Inject

/**
 * Overdue-list state holder — offline-first cache-first pattern (see CalendarViewModel).
 * Room is the UI's single source of truth: [state] is fed by [ControlTowerRepository.observeSummary],
 * a cache-first Flow that emits instantly from Room and re-emits after successful refreshes.
 * [refresh] drives network calls and sets isRefreshing/isOffline/lastSyncedAt flags; the DTO -> UiState
 * mapping is unchanged. Refresh failures with cached data keep the cache on screen and set isOffline;
 * failures with no cache ever observed show an honest error state.
 */
@HiltViewModel
class OverdueViewModel @Inject constructor(
    private val repo: ControlTowerRepository,
) : ViewModel() {

    // Upstream Room flow. Kept as a cold Flow and folded into [state] below; the single
    // WhileSubscribed(5_000) on [state] makes the whole chain lifecycle-aware, so this flow is
    // collected only while the UI is subscribed (MOB-010).
    private val observedResource: Flow<Resource<ControlTowerResponseDto>> = repo.observeSummary()

    // Transient flags for manual updates
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)

    // Combines observed resource with transient flags; lifecycle-aware
    val state: StateFlow<OverdueUiState> = combine(
        observedResource,
        _isRefreshing,
        _isOffline
    ) { resource, isRefreshing, isOffline ->
        val dto = resource.data
        val base = dto?.toOverdueUiState()
            ?: if (resource.hasData) overduePlaceholder("No overdue animals") else overduePlaceholder("Loading…")
        base.copy(
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt ?: base.lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        overduePlaceholder("Loading…")
    )

    init {
        refresh()
    }

    fun refresh() = viewModelScope.launch {
        _isRefreshing.value = true
        val result = repo.refreshSummary()
        _isRefreshing.value = false
        _isOffline.value = result.isFailure
    }

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> refresh()
            else -> Unit // navigation / inert — handled by the nav host.
        }
    }

    private fun ControlTowerResponseDto.toOverdueUiState(): OverdueUiState? {
        if (alerts.isEmpty()) return null
        val base = sampleOverdueState()
        val rows = alerts.map { alert ->
            OverdueRow(
                id = alert.rowId,
                title = alert.title,
                subtitle = alert.detail,
                statusLabel = alert.workState.ifBlank { alert.severity },
                classification = if (isMissed(alert.severity)) {
                    OverdueClassification.MISSED
                } else {
                    OverdueClassification.IN_BUFFER
                },
            )
        }
        // Title is rendered by OverdueScreen via the localized overdue_title_fmt (with rows.size);
        // no English title is baked here.
        return base.copy(title = "", rows = rows)
    }

    private fun isMissed(severity: String): Boolean =
        severity.equals("critical", ignoreCase = true) || severity.equals("high", ignoreCase = true)
}
