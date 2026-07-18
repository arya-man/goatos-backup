package sg.mesha.goatos.update

// telemetry:exempt launch gate runs before auth/bootstrap; it exposes only block/allow shell state,
// and RemoteConfigUpdateGate fails open without user, tenant, or business-action context to track.

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Launch-time force-update gate state.
 *
 * Starts [Checking] so cold launch does not render app data before Remote Config has
 * had one chance to enforce a raised floor. After the first decision it may become
 * [Allowed], or sticky [Blocked] if this build is below the floor. Once blocked, a
 * later refresh that fails open to [UpdateDecision.Allowed] must NOT flap the gate
 * open; clearing the block requires actually updating the app.
 */
sealed interface UpdateGateUiState {
    data object Checking : UpdateGateUiState
    data object Allowed : UpdateGateUiState
    data class Blocked(val updateUrl: String) : UpdateGateUiState
}

/**
 * Checks the [UpdateGate] at launch and on every resume, so a minimum raised while the
 * app is open takes effect the next time it returns to the foreground. Rendered ABOVE
 * auth and bootstrap in `MainActivity`: an out-of-date build is blocked whether or not
 * anyone is signed in.
 */
@HiltViewModel
class UpdateGateViewModel @Inject constructor(
    private val gate: UpdateGate,
) : ViewModel() {

    private val _state = MutableStateFlow<UpdateGateUiState>(UpdateGateUiState.Checking)
    val state: StateFlow<UpdateGateUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            when (val decision = gate.check()) {
                is UpdateDecision.ForceUpdate ->
                    _state.value = UpdateGateUiState.Blocked(updateUrl = decision.updateUrl)

                UpdateDecision.Allowed -> {
                    if (_state.value !is UpdateGateUiState.Blocked) {
                        _state.value = UpdateGateUiState.Allowed
                    }
                }
            }
        }
    }
}
