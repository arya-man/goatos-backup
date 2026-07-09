package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.OverdueUiState
import sg.mesha.goatos.ui.sampleOverdueState
import javax.inject.Inject

/**
 * Overdue-list state holder (leadership follow-up). Seeds the interim [sampleOverdueState]
 * fixture. [LeadershipEvent.Refresh] re-emits; [LeadershipEvent.OverdueRowTapped] (→ reschedule)
 * and [LeadershipEvent.Back] are navigation, routed by the host.
 *
 * TODO: replace the fake seed with the overdue read (backend buffer classification) via AppApi.
 */
@HiltViewModel
class OverdueViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleOverdueState())
    val state: StateFlow<OverdueUiState> = _state.asStateFlow()

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> _state.value = sampleOverdueState()
            else -> Unit // navigation / inert — handled by the nav host.
        }
    }
}
