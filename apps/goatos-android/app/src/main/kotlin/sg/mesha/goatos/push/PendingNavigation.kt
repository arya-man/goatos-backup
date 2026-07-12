package sg.mesha.goatos.push

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.util.concurrent.atomic.AtomicReference
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
 *
 * ### Once-only guarantee
 * [route] is a `StateFlow` so `GoatOsShell`'s `collectAsStateWithLifecycle()` can survive a
 * lifecycle STOP/START (e.g. a configuration change) and still see the pending route once it
 * resubscribes. But a `StateFlow`'s `.value` getter-then-setter is NOT atomic by itself — a
 * naive "read `.value`, then set `.value = null`" `consume()` has a TOCTOU window where a second
 * caller (a re-triggered `LaunchedEffect`, an overlapping collector, or a future consumer added
 * on another thread) can read the same non-null route before the first caller's `null` write
 * lands, and both would navigate. [pendingRoute] (an [AtomicReference], the actual source of
 * truth) closes that window: [AtomicReference.getAndSet] is a single atomic read-and-clear, so
 * at most ONE caller of [consume] ever receives a given non-null route — every other concurrent
 * or later caller gets `null`. [_route] is kept as a StateFlow *mirror* of [pendingRoute] purely
 * so the Compose side still gets reactive emission; it is never the source of truth for whether
 * a route has been handed out.
 */
@Singleton
class PendingNavigation @Inject constructor() {
    private val pendingRoute = AtomicReference<String?>(null)
    private val _route = MutableStateFlow<String?>(null)
    val route: StateFlow<String?> = _route.asStateFlow()

    fun set(route: String) {
        pendingRoute.set(route)
        _route.value = route
    }

    /** Atomically reads-and-clears the pending route ([AtomicReference.getAndSet]) so at most one
     *  caller ever receives a given route — a later recomposition/collection of [route], a
     *  config-change replay, or a second overlapping consumer can never re-navigate to the same
     *  tap twice. The [route] StateFlow is only nulled when THIS call is the one that actually
     *  won the atomic swap, keeping the reactive mirror consistent with [pendingRoute]. */
    fun consume(): String? {
        val current = pendingRoute.getAndSet(null)
        if (current != null) {
            _route.value = null
        }
        return current
    }
}
