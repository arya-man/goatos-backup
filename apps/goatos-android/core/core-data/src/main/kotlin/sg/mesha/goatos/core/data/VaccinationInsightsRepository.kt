package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheDao
import sg.mesha.goatos.core.data.cache.InsightsCoverageCacheEntity
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheDao
import sg.mesha.goatos.core.data.cache.InsightsGapsCacheEntity
import sg.mesha.goatos.core.data.cache.CacheGovernance
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.VaccinationCoverageResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationGapRowDto
import sg.mesha.goatos.core.network.dto.VaccinationGapsResponseDto

/**
 * Leadership drill overlays: live vaccination data gaps and per-vaccine coverage.
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of
 * truth for both reads, each backed by its own cache table ([InsightsGapsCacheDao] /
 * [InsightsCoverageCacheDao]). observeX is cache-first and reactive; refreshX is the network
 * side of stale-while-revalidate — it upserts Room on success and leaves the cache untouched
 * on failure. gaps/coverage are kept as the plain network calls refreshX wraps. DTO -> overlay
 * rows stays in :app.
 */
interface VaccinationInsightsRepository {
    suspend fun gaps(
        parkId: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): VaccinationGapsResponseDto

    /** Cache-first stream for this filter scope: emits immediately with whatever Room has
     *  (null data on a cold cache) and re-emits after every successful [refreshGaps]. */
    fun observeGaps(
        parkId: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): Flow<Resource<VaccinationGapsResponseDto>>

    /** Fetches and upserts Room on success; on failure returns the failure and leaves the
     *  cache untouched — the caller surfaces stale/offline, never a blank screen. */
    suspend fun refreshGaps(
        parkId: String? = null,
        limit: Int? = null,
        cursor: String? = null,
    ): Result<Unit>

    /** Fetches the next keyset page and merges it into the observed first-page Room row. */
    suspend fun appendGaps(
        cursor: String,
        parkId: String? = null,
        limit: Int? = null,
    ): Result<Unit>

    suspend fun coverage(
        parkId: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): VaccinationCoverageResponseDto

    /** Cache-first stream for this filter scope. */
    fun observeCoverage(
        parkId: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Flow<Resource<VaccinationCoverageResponseDto>>

    /** Fetches and upserts Room on success; leaves the cache untouched on failure. */
    suspend fun refreshCoverage(
        parkId: String? = null,
        asOf: String? = null,
        dueBefore: String? = null,
        limit: Int? = null,
    ): Result<Unit>
}

class DefaultVaccinationInsightsRepository(
    private val api: AppApi,
    private val gapsDao: InsightsGapsCacheDao,
    private val coverageDao: InsightsCoverageCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : VaccinationInsightsRepository {
    private val gapsAppendMutex = Mutex()

    override suspend fun gaps(
        parkId: String?,
        limit: Int?,
        cursor: String?,
    ): VaccinationGapsResponseDto = api.getVaccinationGaps(parkId, limit, cursor)

    override fun observeGaps(
        parkId: String?,
        limit: Int?,
        cursor: String?,
    ): Flow<Resource<VaccinationGapsResponseDto>> {
        val key = cacheKey(parkId, limit?.toString(), cursor)
        return gapsDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default) // JSON decode + DTO->Resource off the Main collector
    }

    override suspend fun refreshGaps(
        parkId: String?,
        limit: Int?,
        cursor: String?,
    ): Result<Unit> = runCatching {
        val dto = gaps(parkId, limit, cursor)
        val key = cacheKey(parkId, limit?.toString(), cursor)
        gapsDao.upsert(InsightsGapsCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        gapsDao.enforceCacheBounds()
    }

    override suspend fun appendGaps(
        cursor: String,
        parkId: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        gapsAppendMutex.withLock {
            val key = cacheKey(parkId, limit?.toString(), null)
            val currentEntity = gapsDao.get(key)
            val current = readCachedJson<VaccinationGapsResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = currentEntity?.dtoJson,
                updatedAt = currentEntity?.updatedAt,
                now = clock(),
                quarantine = { gapsDao.delete(it) },
            ).data ?: throw VaccinationGapsCursorException("data-gap continuation has no cached first page")
            if (current.nextCursor != cursor) {
                throw VaccinationGapsCursorException("data-gap cursor is stale or belongs to another scope")
            }

            val page = gaps(parkId = parkId, limit = limit, cursor = cursor)
            if (page.nextCursor == cursor) {
                throw VaccinationGapsCursorException("data-gap backend returned a non-advancing cursor")
            }
            gapsDao.upsert(
                InsightsGapsCacheEntity(
                    cacheKey = key,
                    dtoJson = json.encodeToString(mergeVaccinationGapsPage(current, page)),
                    updatedAt = clock(),
                ),
            )
        }
    }

    override suspend fun coverage(
        parkId: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): VaccinationCoverageResponseDto = api.getVaccinationCoverage(parkId, asOf, dueBefore, limit)

    override fun observeCoverage(
        parkId: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Flow<Resource<VaccinationCoverageResponseDto>> {
        val key = cacheKey(parkId, asOf, dueBefore, limit?.toString())
        return coverageDao.observe(key)
            .map { entity -> entity.toResource(key) }
            .flowOn(Dispatchers.Default) // JSON decode + DTO->Resource off the Main collector
    }

    override suspend fun refreshCoverage(
        parkId: String?,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
    ): Result<Unit> = runCatching {
        val dto = coverage(parkId, asOf, dueBefore, limit)
        val key = cacheKey(parkId, asOf, dueBefore, limit?.toString())
        coverageDao.upsert(InsightsCoverageCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = clock()))
        coverageDao.enforceCacheBounds()
    }

    private suspend fun InsightsGapsCacheEntity?.toResource(key: String): Resource<VaccinationGapsResponseDto> {
        val cached = readCachedJson<VaccinationGapsResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { gapsDao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }

    private suspend fun InsightsCoverageCacheEntity?.toResource(key: String): Resource<VaccinationCoverageResponseDto> {
        val cached = readCachedJson<VaccinationCoverageResponseDto>(
            json = json,
            cacheKey = key,
            dtoJson = this?.dtoJson,
            updatedAt = this?.updatedAt,
            now = clock(),
            quarantine = { coverageDao.delete(it) },
        )
        return Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
    }
}

class VaccinationGapsCursorException(message: String) : IllegalStateException(message)

internal fun mergeVaccinationGapsPage(
    current: VaccinationGapsResponseDto,
    page: VaccinationGapsResponseDto,
): VaccinationGapsResponseDto {
    val merged = (current.rows + page.rows).distinctBy { it.stableGapIdentity() }
    val capped = merged.take(CacheGovernance.DEFAULT_MAX_ROWS) // mobile-guard:ignore: explicit 200-row cache-governance ceiling across user-requested pages
    return page.copy(
        parkId = page.parkId ?: current.parkId,
        rows = capped,
        nextCursor = page.nextCursor?.takeIf { merged.size <= CacheGovernance.DEFAULT_MAX_ROWS },
    )
}

private fun VaccinationGapRowDto.stableGapIdentity(): String =
    goatId.ifBlank { displayId.ifBlank { listOf(parkId, shedId, reasonCode).joinToString("|") } }
