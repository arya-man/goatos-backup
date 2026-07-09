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
 * Loads the backend-driven nav state once at boot (MVI: a single observable
 * [StateFlow]). Hilt injects the repository.
 */
@HiltViewModel
class BootstrapViewModel @Inject constructor(
    private val repo: BootstrapRepository,
) : ViewModel() {

    private val _navState = MutableStateFlow(NavState.Empty)
    val navState: StateFlow<NavState> = _navState.asStateFlow()

    init {
        viewModelScope.launch {
            runCatching { repo.loadNavState() }.getOrNull()?.let { _navState.value = it }
        }
    }
}
