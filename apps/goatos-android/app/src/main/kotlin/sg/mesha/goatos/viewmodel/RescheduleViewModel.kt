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
 * Reschedule-form state holder. Renders the reschedule form shell and owns the two local
 * form interactions: [LeadershipEvent.SegmentSelected] switches the action segment and
 * [LeadershipEvent.DateSelected] picks a date. [LeadershipEvent.Back] is navigation.
 *
 * Confirm is deliberately DISABLED with a reason. The write path is not honestly available:
 * POST /app/vaccination/obligations/{obligation_id}/reschedule currently ignores the
 * obligation_id, keys off the Idempotency-Key header against health-DEFERRED obligations
 * only, and needs the obligation's original defer key (which the mobile client never holds).
 * The control-tower alert that drives this screen also carries no obligation_id yet. Rather
 * than fire a silent no-op that looks like a successful reschedule, Confirm stays off with a
 * plain reason until the backend exposes obligation-id targeting for an overdue obligation.
 */
@HiltViewModel
class RescheduleViewModel @Inject constructor() : ViewModel() {

    private val _state = MutableStateFlow(blockedReschedule())
    val state: StateFlow<RescheduleUiState> = _state.asStateFlow()

    fun onEvent(event: LeadershipEvent) {
        when (event) {
            is LeadershipEvent.SegmentSelected ->
                _state.update { it.copy(selectedSegmentId = event.id) }
            is LeadershipEvent.DateSelected ->
                // Selection is allowed for preview, but Confirm stays disabled — no fake write.
                _state.update { it.copy(selectedDateId = event.id) }
            else -> Unit // navigation / inert — handled by the nav host.
        }
    }

    // The form shell is the only source we have for this screen (no backend reschedule form
    // contract), so it seeds the sample chrome — but Confirm is forced off with an honest reason.
    private fun blockedReschedule(): RescheduleUiState = sampleRescheduleState().copy(
        confirmEnabled = false,
        confirmLabel = "Mobile reschedule — coming soon",
        channelsNote = "Rescheduling from mobile isn't available yet — the backend needs obligation-id targeting for an overdue obligation. Use the web dashboard for now.",
    )
}
