package sg.mesha.goatos.boot

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.model.nav.NavState

/**
 * Loads the backend-driven nav state once at boot (MVI: a single observable
 * [StateFlow]). Hilt replaces the manual factory in the DI pass.
 */
class BootstrapViewModel(
    private val repo: BootstrapRepository,
) : ViewModel() {

    private val _navState = MutableStateFlow(NavState.Empty)
    val navState: StateFlow<NavState> = _navState.asStateFlow()

    init {
        viewModelScope.launch {
            runCatching { repo.loadNavState() }.getOrNull()?.let { _navState.value = it }
        }
    }

    companion object {
        fun factory(repo: BootstrapRepository): ViewModelProvider.Factory =
            object : ViewModelProvider.Factory {
                @Suppress("UNCHECKED_CAST")
                override fun <T : ViewModel> create(modelClass: Class<T>): T =
                    BootstrapViewModel(repo) as T
            }
    }
}
