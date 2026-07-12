package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.AdherenceCacheDao
import sg.mesha.goatos.core.data.cache.AdherenceCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ProtocolAdherenceResponseDto

/**
 * Protocol Adherence screen area: expected-vs-actual vaccination adherence
 * (summary + rows). Offline-first (docs/decisions/android-offline-first.md): Room is the UI's
 * single source of truth. [observeAdherence] is cache-first and reactive; [refreshAdherence]
 * is the network side of stale-while-revalidate — it upserts Room on success (which re-emits
 * to every observer) and leaves the cache untouched on failure. [adherence] is kept as the
 * plain network call [refreshAdherence] wraps. DTO -> UiState mapping stays in :app.
 */
interface AdherenceRepository {
    suspend fun adherence(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ProtocolAdherenceResponseDto

    /** Cache-first stream for this filter scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshAdherence]. */
    fun observeAdherence(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Flow<Resource<ProtocolAdherenceResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the
     *  cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshAdherence(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Result<Unit>
}

class DefaultAdherenceRepository(
    private val api: AppApi,
    private val dao: AdherenceCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : AdherenceRepository {
    override suspend fun adherence(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ProtocolAdherenceResponseDto =
        api.getVaccinationAdherence(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)

    override fun observeAdherence(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): Flow<Resource<ProtocolAdherenceResponseDto>> {
        val key = cacheKey(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit?.toString())
        return dao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default) // JSON decode + DTO->Resource off the Main collector
    }

    override suspend fun refreshAdherence(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = adherence(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)
        val key = cacheKey(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit?.toString())
        dao.upsert(AdherenceCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        dao.enforceCacheBounds()
    }

    private suspend fun AdherenceCacheEntity?.toResource(key: String): Resource<ProtocolAdherenceResponseDto> {
        val cached = readCachedJson<ProtocolAdherenceResponseDto>(
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
