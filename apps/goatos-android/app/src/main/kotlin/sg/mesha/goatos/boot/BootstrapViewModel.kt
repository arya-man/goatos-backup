package sg.mesha.goatos.boot

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.model.nav.NavState
import javax.inject.Inject

/**
 * Load status of the backend-driven bootstrap. A failure is surfaced as [Error]
 * rather than silently collapsing to an empty shell — an unreachable backend or a
 * rejected (401) token must NOT look like a signed-in principal with no modules.
 */
sealed interface BootstrapUiState {
    data object Loading : BootstrapUiState
    data class Ready(val navState: NavState) : BootstrapUiState
    data class Error(val message: String) : BootstrapUiState
}

/**
 * Loads the backend-driven nav state at boot (MVI: a single observable
 * [StateFlow]). Hilt injects the repository. Failures become [BootstrapUiState.Error]
 * so the shell can show a retryable error state instead of a blank/fake shell.
 */
@HiltViewModel
class BootstrapViewModel @Inject constructor(
    private val repo: BootstrapRepository,
) : ViewModel() {

    private val _state = MutableStateFlow<BootstrapUiState>(BootstrapUiState.Loading)
    val state: StateFlow<BootstrapUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.value = BootstrapUiState.Loading
            runCatching { repo.loadNavState() }
                .onSuccess { _state.value = BootstrapUiState.Ready(it) }
                .onFailure {
                    _state.value = BootstrapUiState.Error(
                        "Couldn't load your workspace. Check your connection and try again.",
                    )
                }
        }
    }
}
