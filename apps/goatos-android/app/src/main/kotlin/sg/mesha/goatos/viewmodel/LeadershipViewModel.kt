package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.LeadershipUiState
import sg.mesha.goatos.ui.sampleLeadershipState
import javax.inject.Inject

/**
 * Leadership overview state holder. Seeds the interim [sampleLeadershipState] fixture.
 * [LeadershipEvent.Refresh] re-emits the state; the drill events
 * ([LeadershipEvent.DecisionTapped] → reschedule, [LeadershipEvent.KpiTapped] → overdue)
 * are navigation, routed by the host. Remaining taps are inert until their reads land.
 *
 * TODO: replace the fake seed with the leadership scope_token reads via AppApi.
 */
@HiltViewModel
class LeadershipViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(sampleLeadershipState())
    val state: StateFlow<LeadershipUiState> = _state.asStateFlow()

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            LeadershipEvent.Refresh -> _state.value = sampleLeadershipState()
            // Everything else is either navigation (routed by the host) or not yet backed.
            else -> Unit
        }
    }
}
