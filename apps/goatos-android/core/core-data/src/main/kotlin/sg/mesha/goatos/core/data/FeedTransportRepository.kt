package sg.mesha.goatos.core.data

import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.combine
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.FeedTransportScopedItemEntity
import sg.mesha.goatos.core.data.cache.FeedTransportScopedRemoteKeyEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionsDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto

/**
 * Transport is one task per PHYSICAL SHED per day, so there is no pen to filter by: a shed's whole
 * load leaves on one trip and is proved by one video. Pen grain belongs to packing and distribution.
 */
data class FeedTransportQuery(
    val businessDate: String,
    val parkId: String = "",
    val shedId: String = "",
    val status: String = "",
) {
    internal val scopeKey: String
        get() = listOf(businessDate, parkId, shedId, status).joinToString("|")
}

class FeedTransportRepository(
    private val api: AppApi,
    private val db: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) {
    /**
     * Cache-first page Flow. The per-row `dtoJson` decode is CPU work over the whole visible
     * window and Room emits on its query executor, so the mapping is moved off the collector's
     * dispatcher with [flowOn] — the collector is the UI, and decoding a window of rows on Main
     * on every Room emission drops frames. [flowOn] is upstream-only: it does not change where
     * the ViewModel/UI collects.
     */
    fun observe(query: FeedTransportQuery, limit: Int): Flow<FeedTransportTaskPageDto> = combine(
        db.feedTransportScopedItemDao().observe(query.scopeKey, limit),
        db.feedTransportScopedRemoteKeyDao().observe(query.scopeKey),
    ) { rows, key ->
        FeedTransportTaskPageDto(
            items = rows.map { json.decodeFromString<FeedTransportTaskDto>(it.dtoJson) },
            nextCursor = key?.nextCursor,
            filters = key?.filtersJson?.let { json.decodeFromString<FeedTransportFilterOptionsDto>(it) }
                ?: FeedTransportFilterOptionsDto(),
        )
    }.flowOn(Dispatchers.Default)

    suspend fun refresh(query: FeedTransportQuery): Result<Unit> = runCatching {
        val page = fetch(query, cursor = null)
        val now = clock()
        db.withTransaction {
            // One-shot sweep of rows cached under the retired pen-grain key shape; a no-op once
            // they are gone.
            db.feedTransportScopedItemDao().deleteLegacyPartitionScopes()
            db.feedTransportScopedRemoteKeyDao().deleteLegacyPartitionScopes()
            db.feedTransportScopedItemDao().deleteScope(query.scopeKey)
            db.feedTransportScopedItemDao().upsertAll(page.items.toEntities(query.scopeKey, 0, now))
            db.feedTransportScopedRemoteKeyDao().upsert(page.toRemoteKey(query.scopeKey, now))
        }
    }

    suspend fun loadMore(query: FeedTransportQuery): Result<Unit> = runCatching {
        val key = db.feedTransportScopedRemoteKeyDao().get(query.scopeKey) ?: return@runCatching
        val cursor = key.nextCursor ?: return@runCatching
        val page = fetch(query, cursor)
        val now = clock()
        db.withTransaction {
            val base = db.feedTransportScopedItemDao().count(query.scopeKey)
            db.feedTransportScopedItemDao().upsertAll(page.items.toEntities(query.scopeKey, base, now))
            db.feedTransportScopedRemoteKeyDao().upsert(page.toRemoteKey(query.scopeKey, now))
        }
    }

    private suspend fun fetch(query: FeedTransportQuery, cursor: String?): FeedTransportTaskPageDto =
        api.getFeedTransportTasks(
            businessDate = query.businessDate,
            parkId = query.parkId.takeIf { it.isNotBlank() },
            shedId = query.shedId.takeIf { it.isNotBlank() },
            status = query.status.takeIf { it.isNotBlank() },
            cursor = cursor,
            limit = PAGE_SIZE,
        )

    private fun List<FeedTransportTaskDto>.toEntities(scopeKey: String, base: Int, now: Long) =
        mapIndexed { index, task ->
            FeedTransportScopedItemEntity(
                scopeKey = scopeKey,
                taskId = task.taskId,
                sortIndex = base + index,
                dtoJson = json.encodeToString(task),
                updatedAt = now,
            )
        }

    private fun FeedTransportTaskPageDto.toRemoteKey(scopeKey: String, now: Long) =
        FeedTransportScopedRemoteKeyEntity(
            scopeKey = scopeKey,
            nextCursor = nextCursor,
            endReached = nextCursor == null,
            filtersJson = json.encodeToString(filters),
            updatedAt = now,
        )

    private companion object {
        const val PAGE_SIZE = 20
    }
}
