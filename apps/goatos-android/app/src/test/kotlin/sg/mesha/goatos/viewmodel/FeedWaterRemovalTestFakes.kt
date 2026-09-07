package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto

/**
 * A [BootstrapRepository] that serves ONLY the farm's feed & water removal cutoff (maintainer
 * decision 2026-09-07: config, not code), for the two plan wizards. Defaults to the seeded
 * 20:00 so wizard tests read the same evening the backend fixtures do; pass `null` to model an
 * older cached bootstrap or an unconfigured farm.
 */
class FakeCutoffBootstrapRepository(private val cutoff: String? = "20:00") : BootstrapRepository {
    override suspend fun loadNavState(): NavState = NavState.Empty
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
    override suspend fun feedWaterRemovalCutoffTime(): String? = cutoff
}
