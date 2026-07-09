package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.ControlTowerRepository
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto
import sg.mesha.goatos.feature.profile.AlertRow
import sg.mesha.goatos.feature.profile.AlertTone
import sg.mesha.goatos.feature.profile.AlertsEvent
import sg.mesha.goatos.feature.profile.AlertsUiState
import sg.mesha.goatos.ui.sampleAlertsState
import javax.inject.Inject

/**
 * Alerts / notifications state holder. Seeds the interim [sampleAlertsState] fixture for an
 * instant first frame, then loads the real process-integrity alerts via
 * [ControlTowerRepository.summary], mapped in [toAlertsUiState]. On error/empty the sample
 * is kept. Rows are marked read locally so the list feels live: [AlertsEvent.MarkAllRead]
 * clears every unread flag, [AlertsEvent.OpenAlert] clears the tapped row.
 */
@HiltViewModel
class AlertsViewModel @Inject constructor(
    private val repo: ControlTowerRepository,
) : ViewModel() {

    private val _state = MutableStateFlow(sampleAlertsState())
    val state: StateFlow<AlertsUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() = viewModelScope.launch {
        runCatching { repo.summary() }
            .onSuccess { dto -> dto.toAlertsUiState()?.let { _state.value = it } }
            .onFailure { /* keep the sample so the screen is never blank */ }
    }

    fun onEvent(event: AlertsEvent) {
        when (event) {
            AlertsEvent.MarkAllRead ->
                _state.update { current ->
                    current.copy(rows = current.rows.map { it.copy(unread = false) })
                }
            is AlertsEvent.OpenAlert ->
                _state.update { current ->
                    current.copy(
                        rows = current.rows.map {
                            if (it.id == event.id) it.copy(unread = false) else it
                        },
                    )
                }
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
