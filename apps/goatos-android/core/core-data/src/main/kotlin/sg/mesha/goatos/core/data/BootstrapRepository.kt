package sg.mesha.goatos.core.data

import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto

/**
 * Reads the backend-driven nav state from the app bootstrap (offline-first). The
 * interface references only [NavState] + the bootstrap profile DTO so annotation
 * processors resolving these types never transitively touch the concrete
 * implementation's cross-module dependencies (see [DefaultBootstrapRepository]).
 */
interface BootstrapRepository {
    suspend fun loadNavState(): NavState

    /** The operator profile from the (cached or fresh) bootstrap; null when the backend
     *  surfaced none for this principal (e.g. a leadership user has no operator profile). */
    suspend fun operatorProfile(): BootstrapOperatorProfileDto?
}
