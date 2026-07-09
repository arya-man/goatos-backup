package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.toNavState

/**
 * Reads the backend-driven nav state from the app bootstrap, offline-first: fetch
 * fresh, cache it, and fall back to the cached bootstrap when the network fails.
 * (ETag/contract-revision revalidation + Proto DataStore session land next.)
 */
interface BootstrapRepository {
    suspend fun loadNavState(): NavState
}

class DefaultBootstrapRepository(
    private val api: AppApi,
    private val cache: BootstrapCache? = null,
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState =
        try {
            val dto = api.bootstrap()
            cache?.save(dto)
            dto.toNavState()
        } catch (t: Throwable) {
            cache?.load()?.toNavState() ?: throw t
        }
}
