package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.feature.profile.AlertsEvent
import sg.mesha.goatos.feature.profile.AlertsUiState
import sg.mesha.goatos.ui.sampleAlertsState
import javax.inject.Inject

/**
 * Alerts / notifications state holder. Seeds the interim [sampleAlertsState] fixture and marks
 * rows read locally so the list feels live: [AlertsEvent.MarkAllRead] clears every unread flag,
 * [AlertsEvent.OpenAlert] clears the tapped row.
 *
 * TODO: replace the fake seed + local read-state with the FCM / notification-history reads via AppApi.
 */
@HiltViewModel
class AlertsViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleAlertsState())
    val state: StateFlow<AlertsUiState> = _state.asStateFlow()

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
}
