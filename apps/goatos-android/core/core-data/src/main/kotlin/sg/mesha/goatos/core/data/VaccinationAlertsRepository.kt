package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.VaccinationAlertsCacheDao
import sg.mesha.goatos.core.data.cache.VaccinationAlertsCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.VaccinationAlertPageResponseDto

/**
 * The vaccination module's OWN lifecycle alerts feed: a proof approved, a proof sent back for
 * rework, a record closed -- each already routed by the backend to whoever owns the next action.
 *
 * This is NOT the control-tower process-integrity summary ([ControlTowerRepository]), which is
 * what the Alerts tab used to render. Control tower reports process GAPS and carries no module
 * dimension, so none of the vaccination lifecycle notifications could ever appear there: during
 * the 2026-08-08 QA run 41 of them sat unread while the tab showed nothing.
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of
 * truth. [observeAlerts] is cache-first and reactive; [refreshAlerts] is the network side of
 * stale-while-revalidate and leaves the cache untouched on failure, so a refresh that fails in a
 * shed keeps the last alerts on screen instead of blanking them.
 *
 * SCOPING IS SERVER-SIDE AND IS NOT NEGOTIABLE FROM HERE. There is deliberately no operator,
 * park, or member parameter: the backend derives the audience from the caller's own session
 * (member_id equality inside the query), because the feed is the read of routing that already
 * happened. A client-supplied identity would be a way to ask for somebody else's alerts.
 */
interface VaccinationAlertsRepository {
    /** Cache-first stream: emits immediately with whatever Room has (null data on a cold cache)
     *  and re-emits after every successful [refreshAlerts]. */
    fun observeAlerts(cursor: String? = null, limit: Int? = null): Flow<Resource<VaccinationAlertPageResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the cache
     *  untouched -- the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshAlerts(cursor: String? = null, limit: Int? = null): Result<Unit>
}

class DefaultVaccinationAlertsRepository(
    private val api: AppApi,
    private val dao: VaccinationAlertsCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : VaccinationAlertsRepository {

    override fun observeAlerts(cursor: String?, limit: Int?): Flow<Resource<VaccinationAlertPageResponseDto>> {
        val key = cacheKey(cursor, limit?.toString())
        return dao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default) // JSON decode + DTO->Resource off the Main collector
    }

    override suspend fun refreshAlerts(cursor: String?, limit: Int?): Result<Unit> = runCatching {
        val dto = api.listVaccinationAlerts(
            cursor,
            limit ?: sg.mesha.goatos.core.network.VACCINATION_ALERTS_PAGE_SIZE,
        )
        val key = cacheKey(cursor, limit?.toString())
        dao.upsert(
            VaccinationAlertsCacheEntity(
                cacheKey = key,
                dtoJson = json.encodeToString(dto),
                updatedAt = clock(),
            ),
        )
        dao.enforceCacheBounds()
    }

    private suspend fun VaccinationAlertsCacheEntity?.toResource(
        key: String,
    ): Resource<VaccinationAlertPageResponseDto> {
        val cached = readCachedJson<VaccinationAlertPageResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { dao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }
}
