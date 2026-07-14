package sg.mesha.goatos.update

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
 * Starts [Allowed] and only ever flips to [Blocked] — never back. Once a session has
 * seen the build is below the floor, a later refresh that fails to fetch (and so
 * fails open to [UpdateDecision.Allowed]) must NOT flap the gate open. Clearing the
 * block requires actually updating, which reinstalls the app → new process → fresh
 * [Allowed] start with the new versionCode.
 */
sealed interface UpdateGateUiState {
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

    private val _state = MutableStateFlow<UpdateGateUiState>(UpdateGateUiState.Allowed)
    val state: StateFlow<UpdateGateUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            // Sticky block: only ever transition into Blocked. See UpdateGateUiState.
            (gate.check() as? UpdateDecision.ForceUpdate)?.let {
                _state.value = UpdateGateUiState.Blocked(updateUrl = it.updateUrl)
            }
        }
    }
}
