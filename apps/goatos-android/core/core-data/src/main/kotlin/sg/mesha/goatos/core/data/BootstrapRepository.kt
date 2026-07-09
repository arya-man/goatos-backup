package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.toNavState

/**
 * Reads the backend-driven nav state from the app bootstrap. The real impl caches
 * to Room/DataStore keyed by ETag/contract revision + refreshes on FCM config-ping
 * (backend-driven-config.md); the skeleton reads straight through the [AppApi].
 */
interface BootstrapRepository {
    suspend fun loadNavState(): NavState
}

class DefaultBootstrapRepository(
    private val api: AppApi,
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = api.bootstrap().toNavState()
}
