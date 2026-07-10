package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.ui.CoverageBannerUiState
import javax.inject.Inject

/**
 * HRMS coverage banner (docs/hr/roster-rbac-design.md S4.6/S4.8) for a landing screen
 * (today: Calendar). Shows the server-composed banner text ONLY when
 * `GET /app/roster/my-coverage` reports `has_coverage = true` for the authenticated
 * principal — no scope/identity bridge to guess client-side (TRD §14 dumb-renderer):
 * the backend resolves who is covering, until when, and the exact wording. A null
 * [state] (or a response with `has_coverage = false`) hides the banner entirely.
 */
@HiltViewModel
class CoverageBannerViewModel @Inject constructor(
    private val repo: RosterRepository,
) : ViewModel() {

    private val _state = MutableStateFlow<CoverageBannerUiState?>(null)
    val state: StateFlow<CoverageBannerUiState?> = _state.asStateFlow()

    init {
        load()
    }

    /** Re-fetches the current coverage status. Safe to call again on refresh/retry. */
    fun load() = viewModelScope.launch {
        val coverage = runCatching { repo.myCoverage().coverage }.getOrNull()
        _state.value = if (coverage?.hasCoverage == true) {
            coverage.bannerText?.ifBlank { null }?.let { CoverageBannerUiState(text = it) }
        } else {
            null
        }
    }
}
