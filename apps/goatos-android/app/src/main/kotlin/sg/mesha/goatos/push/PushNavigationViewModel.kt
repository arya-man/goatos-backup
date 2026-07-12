package sg.mesha.goatos.push

import androidx.lifecycle.ViewModel
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import javax.inject.Inject

/**
 * Thin Activity-scoped adapter so [sg.mesha.goatos.ui.GoatOsShell] (a `@Composable`, not an
 * Activity/Service) can observe + consume [PendingNavigation] via `hiltViewModel()` — the same
 * access pattern every other shell-level ViewModel in this app uses (see `SyncStatusViewModel`,
 * `ProfileViewModel` call sites in `GoatOsShell`).
 */
@HiltViewModel
class PushNavigationViewModel @Inject constructor(
    private val pendingNavigation: PendingNavigation,
) : ViewModel() {
    val pendingRoute: StateFlow<String?> = pendingNavigation.route

    fun consume() {
        pendingNavigation.consume()
    }
}
