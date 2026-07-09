package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.OverdueClassification
import sg.mesha.goatos.feature.leadership.OverdueRow
import sg.mesha.goatos.feature.leadership.OverdueUiState
import sg.mesha.goatos.ui.sampleOverdueState
import javax.inject.Inject

/**
 * Overdue-list state holder (leadership follow-up). Seeds the interim [sampleOverdueState]
 * fixture for an instant first frame, then loads the real process-integrity alerts via
 * [ControlTowerRepository.summary] and maps them to overdue rows in [toOverdueUiState],
 * classifying each by backend severity. On error/empty the sample is kept.
 * [LeadershipEvent.Refresh] reloads; row taps / back are navigation, routed by the host.
 */
@HiltViewModel
class OverdueViewModel @Inject constructor(
    private val repo: ControlTowerRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(sampleOverdueState())
    val state: StateFlow<OverdueUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.summary() }
            .onSuccess { dto -> dto.toOverdueUiState()?.let { _state.value = it } }
            .onFailure { /* keep the sample so the screen is never blank */ }
    }

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> load()
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
        return base.copy(rows = rows)
    }

    private fun isMissed(severity: String): Boolean =
        severity.equals("critical", ignoreCase = true) || severity.equals("high", ignoreCase = true)
}
