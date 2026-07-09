package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.model.nav.NavState

/**
 * Reads the backend-driven nav state from the app bootstrap (offline-first). The
 * interface is kept in its own file — referencing only [NavState] — so annotation
 * processors resolving this type never transitively touch the concrete
 * implementation's cross-module dependencies (see [DefaultBootstrapRepository]).
 */
interface BootstrapRepository {
    suspend fun loadNavState(): NavState
}
