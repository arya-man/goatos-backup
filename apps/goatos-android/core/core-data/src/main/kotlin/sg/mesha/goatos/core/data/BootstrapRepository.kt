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

    /** The authenticated principal's tenant id from the (cached or fresh) bootstrap's
     *  `actor` slice — used to stamp a stable, non-PII tenant identifier on analytics
     *  identity (BootstrapViewModel.applyAnalyticsIdentity). Defaulted to null so existing
     *  fakes/implementers (tests, previews) compile unchanged; only [DefaultBootstrapRepository]
     *  overrides it with the real cache/network read. */
    suspend fun actorTenantId(): String? = null

    /** The farm's feed & water removal cutoff ("HH:MM" IST) from the (cached or fresh)
     *  bootstrap (maintainer decision 2026-09-07: config, not code), read by the weighing and
     *  PC Care plan wizards to mirror the backend's date-picker rule. Null when the bootstrap
     *  carries none (older cache, unconfigured farm) — the wizards then offer only the rule's
     *  invariant floor and the server's own refusal decides. Defaulted so existing fakes and
     *  implementers compile unchanged; only [DefaultBootstrapRepository] overrides it. */
    suspend fun feedWaterRemovalCutoffTime(): String? = null
}
