package sg.mesha.goatos.push

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Holds a route a notification tap resolved to, until [sg.mesha.goatos.ui.GoatOsShell]'s
 * NavHost exists to consume it.
 *
 * A tap can arrive before auth/bootstrap complete: cold start goes MainActivity -> (login) ->
 * bootstrap -> [sg.mesha.goatos.ui.GoatOsShell] (the ONLY place a real `NavHostController` is
 * created — see [sg.mesha.goatos.ui.GoatOsShell]'s `rememberNavController()`), so a tap intent
 * that starts the process has nowhere to navigate yet. This process-scoped singleton bridges
 * that gap instead of dropping the tap: [sg.mesha.goatos.MainActivity] writes via [set] as soon
 * as the intent is read (`onCreate`/`onNewIntent`, independent of Compose state), and
 * [PushNavigationViewModel] (Activity-scoped, observed from `GoatOsShell`) reads + [consume]s it
 * exactly once the NavHost is actually composed.
 */
@Singleton
class PendingNavigation @Inject constructor() {
    private val _route = MutableStateFlow<String?>(null)
    val route: StateFlow<String?> = _route.asStateFlow()

    fun set(route: String) {
        _route.value = route
    }

    /** Atomically reads and clears the pending route so a later recomposition/collection of
     *  [route] never re-navigates to the same tap twice. */
    fun consume(): String? {
        val current = _route.value
        _route.value = null
        return current
    }
}
