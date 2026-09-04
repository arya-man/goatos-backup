package sg.mesha.goatos.boot

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow

/**
 * "The backend-composed navigation may have changed — read it again, quietly."
 *
 * A screen ViewModel cannot reach the Activity-scoped [BootstrapViewModel], and MUST not flip the
 * shell through Loading (that unmounts the NavHost and drops the back stack). So a screen asks
 * through this app-scoped signal and [BootstrapViewModel.refreshQuietly] re-reads the bootstrap
 * in place. First use: the Leadership Tasks badge (the module's `badge_count`) drops after the
 * list loads and after a task is marked seen.
 *
 * Coalescing on purpose: a burst of requests while one refresh is running yields one more, not N.
 */
@Singleton
class NavStateRefreshSignal @Inject constructor() {
    private val _requests = MutableSharedFlow<Unit>(
        replay = 0,
        extraBufferCapacity = 1,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    val requests: SharedFlow<Unit> = _requests

    fun request() {
        _requests.tryEmit(Unit)
    }
}
