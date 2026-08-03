package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.WeighingAlertsCacheDao
import sg.mesha.goatos.core.data.cache.WeighingAlertsCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.WeighingAlertPageResponseDto

/**
 * The weighing module's OWN lifecycle alerts feed: work assigned, a shed submitted for
 * verification, a proof sent back for rework, a shed reopened, work closed -- each already routed
 * by the backend to whoever owns the next action.
 *
 * This is NOT the vaccination process-integrity feed ([ControlTowerRepository]). It reads
 * `GET /app/weighing/alerts`, which is gated on weighing capabilities alone.
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of
 * truth. [observeAlerts] is cache-first and reactive; [refreshAlerts] is the network side of
 * stale-while-revalidate and leaves the cache untouched on failure, so a refresh that fails in a
 * shed keeps yesterday's alerts on screen instead of blanking them.
 *
 * SCOPING IS SERVER-SIDE AND IS NOT NEGOTIABLE FROM HERE. There is deliberately no operator,
 * park, or member parameter: the backend derives the audience from the caller's own session,
 * because the feed is the read of routing that already happened. A client-supplied identity
 * would be a way to ask for somebody else's alerts.
 */
interface WeighingAlertsRepository {
    /** Cache-first stream: emits immediately with whatever Room has (null data on a cold cache)
     *  and re-emits after every successful [refreshAlerts]. */
    fun observeAlerts(cursor: String? = null, limit: Int? = null): Flow<Resource<WeighingAlertPageResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the cache
     *  untouched -- the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshAlerts(cursor: String? = null, limit: Int? = null): Result<Unit>
}

class DefaultWeighingAlertsRepository(
    private val api: AppApi,
    private val dao: WeighingAlertsCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : WeighingAlertsRepository {

    override fun observeAlerts(cursor: String?, limit: Int?): Flow<Resource<WeighingAlertPageResponseDto>> {
        val key = cacheKey(cursor, limit?.toString())
        return dao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default) // JSON decode + DTO->Resource off the Main collector
    }

    override suspend fun refreshAlerts(cursor: String?, limit: Int?): Result<Unit> = runCatching {
        val dto = api.listWeighingAlerts(cursor, limit ?: sg.mesha.goatos.core.network.WEIGHING_ALERTS_PAGE_SIZE)
        val key = cacheKey(cursor, limit?.toString())
        dao.upsert(WeighingAlertsCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        dao.enforceCacheBounds()
    }

    private suspend fun WeighingAlertsCacheEntity?.toResource(key: String): Resource<WeighingAlertPageResponseDto> {
        val cached = readCachedJson<WeighingAlertPageResponseDto>(
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
