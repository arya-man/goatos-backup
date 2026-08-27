package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheDao
import sg.mesha.goatos.core.data.cache.CountsBreakdownMetaCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.MilkPreparationPageDto

private const val MILK_PREPARATION_CACHE_PREFIX = "__milk_preparation__"

/**
 * Room-backed current-day Milk Preparation worklist. The response's [MilkPreparationPageDto.farmTasks]
 * is bounded by the tenant's physical farm count; herd-sized cohort detail remains paged by
 * the backend and is not accumulated on the phone.
 */
interface MilkPreparationRepository {
    fun observe(preparationDate: String): Flow<Resource<MilkPreparationPageDto>>
    suspend fun refresh(preparationDate: String): Result<Unit>
}

class DefaultMilkPreparationRepository(
    private val api: AppApi,
    private val cache: CountsBreakdownMetaCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : MilkPreparationRepository {
    override fun observe(preparationDate: String): Flow<Resource<MilkPreparationPageDto>> {
        val key = cacheKey(preparationDate)
        return cache.observe(key)
            .map { entity ->
                val cached = readCachedJson<MilkPreparationPageDto>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { cache.delete(it) },
                )
                Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
            }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun refresh(preparationDate: String): Result<Unit> = runCatching {
        // The selected date MUST reach the API: caching a dateless (today) response under the
        // selected date's key rendered today's numbers beneath a past-day label.
        val page = api.getMilkPreparation(preparationDate = preparationDate.trim(), limit = 20, offset = 0)
        cache.upsert(
            CountsBreakdownMetaCacheEntity(
                cacheKey = cacheKey(preparationDate),
                dtoJson = json.encodeToString(page),
                updatedAt = clock(),
            ),
        )
        cache.enforceCacheBounds()
    }

    private fun cacheKey(preparationDate: String): String =
        "$MILK_PREPARATION_CACHE_PREFIX:${preparationDate.trim()}"
}
