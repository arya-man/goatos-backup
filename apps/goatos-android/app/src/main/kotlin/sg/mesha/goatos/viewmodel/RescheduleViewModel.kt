package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.RescheduleUiState
import sg.mesha.goatos.ui.sampleRescheduleState
import javax.inject.Inject

/**
 * Reschedule-form state holder. Seeds the interim [sampleRescheduleState] fixture and owns
 * the two local form interactions: [LeadershipEvent.SegmentSelected] switches the action
 * segment, and [LeadershipEvent.DateSelected] picks a date and enables Confirm.
 * [LeadershipEvent.ConfirmReschedule] / [LeadershipEvent.Back] are navigation, routed by the host.
 *
 * TODO: replace the fake seed + local confirm-gate with the reschedule write via AppApi.
 */
@HiltViewModel
class RescheduleViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleRescheduleState())
    val state: StateFlow<RescheduleUiState> = _state.asStateFlow()

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            is LeadershipEvent.SegmentSelected ->
                _state.update { it.copy(selectedSegmentId = event.id) }
            is LeadershipEvent.DateSelected ->
                _state.update { it.copy(selectedDateId = event.id, confirmEnabled = true) }
            else -> Unit // navigation / inert — handled by the nav host.
        }
    }
}
