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
import sg.mesha.goatos.core.network.dto.MilkFeedingPageDto

private const val MILK_FEEDING_CACHE_PREFIX = "__milk_feeding__"

interface MilkFeedingRepository {
    fun observe(feedingDate: String, parkId: String = "", sessionNo: Int? = null): Flow<Resource<MilkFeedingPageDto>>
    suspend fun refresh(feedingDate: String, parkId: String = "", sessionNo: Int? = null): Result<Unit>
}

class DefaultMilkFeedingRepository(
    private val api: AppApi,
    private val cache: CountsBreakdownMetaCacheDao,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = System::currentTimeMillis,
) : MilkFeedingRepository {
    override fun observe(feedingDate: String, parkId: String, sessionNo: Int?): Flow<Resource<MilkFeedingPageDto>> {
        val key = cacheKey(feedingDate, parkId, sessionNo)
        return cache.observe(key).map { entity ->
            val cached = readCachedJson<MilkFeedingPageDto>(json, key, entity?.dtoJson, entity?.updatedAt, clock()) { cache.delete(it) }
            Resource(data = cached.data, lastSyncedAt = cached.updatedAt)
        }.flowOn(Dispatchers.Default)
    }

    override suspend fun refresh(feedingDate: String, parkId: String, sessionNo: Int?): Result<Unit> = runCatching {
        val page = api.getMilkFeedingTasks(feedingDate = feedingDate, parkId = parkId.ifBlank { null }, sessionNo = sessionNo, limit = 20)
        cache.upsert(CountsBreakdownMetaCacheEntity(cacheKey(feedingDate, parkId, sessionNo), json.encodeToString(page), clock()))
        cache.enforceCacheBounds()
    }

    private fun cacheKey(feedingDate: String, parkId: String, sessionNo: Int?) =
        "$MILK_FEEDING_CACHE_PREFIX:${feedingDate.trim()}:${parkId.trim()}:${sessionNo ?: "all"}"
}
