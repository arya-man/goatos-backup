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
 * the backend resolves who is covering, until when, and the exact wording.
 *
 * Offline-first (docs/decisions/android-offline-first.md): observes Room cache via
 * [RosterRepository.observeCoverage]; refresh via [RosterRepository.refreshCoverage]
 * runs in the background. Explicitly distinguishes:
 * - [CoverageState.HasCoverage]: cached or fresh response with has_coverage=true
 * - [CoverageState.NoCoverage]: cached or fresh response with has_coverage=false
 * - [CoverageState.Unknown]: no cache and no successful refresh (offline/error)
 *
 * A null [state] or [CoverageState.Unknown] hides the banner entirely. The ViewModel
 * shows stale cached coverage (NoCoverage or HasCoverage) on re-entry and refreshes
 * in the background; a failed refresh keeps the last cached state, never blanking to
 * "unknown" if data exists.
 */
sealed interface CoverageState {
    data class HasCoverage(val text: String) : CoverageState
    data object NoCoverage : CoverageState
    data object Unknown : CoverageState
}

@HiltViewModel
class CoverageBannerViewModel @Inject constructor(
    private val repo: RosterRepository,
) : ViewModel() {

    private val _state = MutableStateFlow<CoverageBannerUiState?>(null)
    val state: StateFlow<CoverageBannerUiState?> = _state.asStateFlow()

    // Internal state tracking for offline/error handling
    private val _coverageState = MutableStateFlow<CoverageState>(CoverageState.Unknown)
    val coverageState: StateFlow<CoverageState> = _coverageState.asStateFlow()

    // Track refresh state so we can distinguish "I've never refreshed" from "I tried and failed"
    private val _isRefreshing = MutableStateFlow(false)
    val isRefreshing: StateFlow<Boolean> = _isRefreshing.asStateFlow()

    init {
        load()
    }

    /** Re-fetches the current coverage status. Safe to call again on refresh/retry. */
    fun load() = viewModelScope.launch {
        // Start observing Room cache immediately (stale-while-revalidate)
        repo.observeCoverage().collect { dto ->
            val newState = if (dto?.coverage?.hasCoverage == true) {
                dto.coverage.bannerText?.ifBlank { null }?.let { CoverageState.HasCoverage(it) }
                    ?: CoverageState.NoCoverage
            } else if (dto != null) {
                // Cached or fresh response with has_coverage=false
                CoverageState.NoCoverage
            } else {
                // No cache yet; will refresh to get real state
                CoverageState.Unknown
            }

            _coverageState.value = newState
            updateBannerState(newState)

            // Trigger background refresh on first load if no cache
            if (_isRefreshing.value.not()) {
                refreshInBackground()
            }
        }
    }

    private fun refreshInBackground() = viewModelScope.launch {
        _isRefreshing.value = true
        // refreshCoverage never throws; it returns false on a network failure (cache kept).
        // On success the Room flow re-emits and the collector recomputes the state. On
        // failure the cache (if any) stays visible; with no cache the state stays Unknown —
        // a distinct "coverage unknown/offline" that is NOT the same as NoCoverage.
        repo.refreshCoverage()
        _isRefreshing.value = false
    }

    private fun updateBannerState(state: CoverageState) {
        _state.value = when (state) {
            is CoverageState.HasCoverage -> CoverageBannerUiState(text = state.text)
            else -> null  // NoCoverage and Unknown both hide the banner
        }
    }
}
