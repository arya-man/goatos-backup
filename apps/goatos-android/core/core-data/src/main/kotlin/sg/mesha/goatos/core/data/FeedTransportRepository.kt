package sg.mesha.goatos.core.data

import androidx.room.withTransaction
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.distinctUntilChanged
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

/**
 * The narrow surface [sg.mesha.goatos.viewmodel.FeedTransportCaptureViewModel] depends on for live
 * status gating — split out so tests can fake it directly rather than needing a real [GoatDatabase]
 * to construct a [FeedTransportRepository]. See [FeedRepository]'s own interface/impl split for the
 * same shape.
 */
interface FeedTransportStatusSource {
    /** LIVE status for one transport task, straight from the same Room table the task list renders
     *  from. `null` while Room has no cached row for this task yet. */
    fun observeTaskStatus(taskId: String): Flow<String?>

    /**
     * ONE-SHOT SERVER read of one transport task's status, bypassing Room entirely.
     * [observeTaskStatus] only changes when THIS phone's own list refresh writes a fresh cached row
     * — a teammate submitting the SAME task on another phone never touches this phone's Room cache
     * while the capture screen sits open. This is the periodic top-up that closes that gap. Reuses
     * the existing `GET /feed-transport/tasks` endpoint (no new backend route), narrowed to one
     * shed/day.
     *
     * Returns `null` on ANY failure (offline/timeout/5xx) OR when no matching task comes back —
     * callers MUST treat `null` as "unknown, keep current state", never as "not yet submitted".
     */
    suspend fun fetchTaskStatus(businessDate: String, shedId: String, taskId: String): String?
}

class FeedTransportRepository(
    private val api: AppApi,
    private val db: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : FeedTransportStatusSource {
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

    /**
     * LIVE status for one transport task, straight from the same Room table [observe] renders from.
     * `null` while Room has no cached row for this task yet — the caller should fall back to its
     * nav-arg hint in that case. Lets [sg.mesha.goatos.viewmodel.FeedTransportCaptureViewModel] flip
     * to read-only live if the task is verified/rejected elsewhere while the capture screen is open,
     * mirroring the packing/distribution completion screens' status gating.
     */
    override fun observeTaskStatus(taskId: String): Flow<String?> =
        db.feedTransportScopedItemDao().observeByTaskId(taskId)
            .map { entity -> entity?.let { json.decodeFromString<FeedTransportTaskDto>(it.dtoJson).status } }
            .distinctUntilChanged()
            .flowOn(Dispatchers.Default)

    override suspend fun fetchTaskStatus( // offline-first-guard:ignore: liveness beats staleness — this exists specifically to see a teammate's write Room has not cached yet.
        businessDate: String,
        shedId: String,
        taskId: String,
    ): String? = runCatching { // exception:exempt expected poll failure (offline/timeout/5xx); caller treats null as unknown, not an error to record
        api.getFeedTransportTasks(
            businessDate = businessDate,
            shedId = shedId.takeIf { it.isNotBlank() },
            limit = STATUS_POLL_LIMIT,
        ).items.firstOrNull { it.taskId == taskId }?.status
    }.getOrNull()?.takeIf { it.isNotBlank() }

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

    /** Persist server truth into every cached filter scope before the active outbox overlay leaves. */
    suspend fun persistTaskStatus(taskId: String, status: String) {
        if (taskId.isBlank() || status.isBlank()) return
        val rows = db.feedTransportScopedItemDao().rowsByTaskId(taskId)
        if (rows.isEmpty()) return
        val now = clock()
        db.feedTransportScopedItemDao().upsertAll(
            rows.map { row ->
                val dto = json.decodeFromString<FeedTransportTaskDto>(row.dtoJson).copy(status = status)
                row.copy(dtoJson = json.encodeToString(dto), updatedAt = now)
            },
        )
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

        /** Page size for [fetchTaskStatus]'s narrow poll — one shed/day filtered server-side, so a
         *  small page is always enough (one task per physical shed per day). */
        const val STATUS_POLL_LIMIT = 20
    }
}
