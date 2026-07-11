package sg.mesha.goatos.core.data

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.ControlTowerCacheDao
import sg.mesha.goatos.core.data.cache.ControlTowerCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.ControlTowerResponseDto

/**
 * Control Tower screen area: broken/at-risk vaccination process integrity
 * (summary + alerts). Offline-first (docs/decisions/android-offline-first.md): Room is the
 * UI's single source of truth. [observeSummary] is cache-first and reactive; [refreshSummary]
 * is the network side of stale-while-revalidate. [summary] is kept as the plain network call
 * [refreshSummary] wraps. DTO -> UiState mapping stays in :app.
 */
interface ControlTowerRepository {
    suspend fun summary(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): ControlTowerResponseDto

    /** Cache-first stream for this filter scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshSummary]. */
    fun observeSummary(
        parkId: String? = null,
        shedId: String? = null,
        workState: String? = null,
        severity: String? = null,
        dueBefore: String? = null,
        asOf: String? = null,
        cursor: String? = null,
        limit: Int? = null,
    ): Flow<Resource<ControlTowerResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the
     *  cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshSummary(
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

class DefaultControlTowerRepository(
    private val api: AppApi,
    private val dao: ControlTowerCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : ControlTowerRepository {
    override suspend fun summary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): ControlTowerResponseDto =
        api.getVaccinationControlTower(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)

    override fun observeSummary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): Flow<Resource<ControlTowerResponseDto>> =
        dao.observe(cacheKey(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit?.toString()))
            .map { it.toResource() }

    override suspend fun refreshSummary(
        parkId: String?,
        shedId: String?,
        workState: String?,
        severity: String?,
        dueBefore: String?,
        asOf: String?,
        cursor: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = summary(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit)
        val key = cacheKey(parkId, shedId, workState, severity, dueBefore, asOf, cursor, limit?.toString())
        dao.upsert(ControlTowerCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
    }

    private fun ControlTowerCacheEntity?.toResource(): Resource<ControlTowerResponseDto> =
        Resource(
            data = this?.let { runCatching { json.decodeFromString<ControlTowerResponseDto>(it.dtoJson) }.getOrNull() },
            lastSyncedAt = this?.updatedAt,
        )
}
