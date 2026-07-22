package sg.mesha.goatos.core.data

import androidx.paging.ExperimentalPagingApi
import androidx.paging.LoadType
import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.PagingState
import androidx.paging.RemoteMediator
import androidx.paging.map
import androidx.room.withTransaction
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.ShiftingPendingItemEntity
import sg.mesha.goatos.core.data.cache.ShiftingPendingRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto

/**
 * One screen-page of the operator's Pending tab. The backend caps this endpoint at 20 server-side
 * because the queue is read from a phone; the client asks for the same bound so both layers page
 * identically (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
const val SHIFTING_PENDING_PAGE_SIZE = 20

/** How many filter scopes (farm/shed combinations) keep their cached rows. Bounds the table. */
private const val SHIFTING_PENDING_CACHED_QUERIES = 4

/**
 * The shifting PENDING-EXECUTION queue (`GET /app/counts/shifting-events/pending-execution`).
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of truth.
 * The screen observes a Room [androidx.paging.PagingSource]; a `RemoteMediator` fills Room from the
 * backend page-by-page. Re-entering the tab renders the cached queue immediately and refreshes
 * behind it — never a blank wall over cached rows.
 *
 * The EXECUTION side ("Mark done" / cancel) does NOT live here. Those are mutating writes and go
 * through the durable outbox (`sync/SyncRepository`) exactly like every other Counts write, so a
 * completion done in a shed with no signal is durable on the phone and replays under one stable
 * idempotency key instead of moving the herd twice.
 */
interface ShiftingPendingRepository {
    /**
     * The paged queue, scoped by the operator's farm -> shed filter. Both layers page identically at
     * [SHIFTING_PENDING_PAGE_SIZE] over the backend's own opaque keyset cursor; nothing ever holds
     * the whole backlog. A blank [parkId]/[shedId] means "every park/shed".
     */
    fun pending(
        parkId: String = "",
        shedId: String = "",
    ): Flow<PagingData<CountsShiftingPendingExecutionItemDto>>

    /**
     * Removes an executed/cancelled movement from the cached queue.
     *
     * Called as soon as a completion/cancel is durably queued, so the row leaves the Pending list at
     * the moment the operator acts rather than at the next refresh — and, more importantly, so the
     * same movement cannot be completed twice while its first completion is still draining.
     */
    suspend fun forgetExecuted(shiftingEventId: String)

    /**
     * The cached movement by id, or null if it is no longer in any cached scope (already completed
     * or cancelled). Backs the execute screen's offline-first open — no refetch, since the operator
     * taps a row already in Room.
     */
    suspend fun findCached(shiftingEventId: String): CountsShiftingPendingExecutionItemDto?
}

class DefaultShiftingPendingRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : ShiftingPendingRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun pending(parkId: String, shedId: String): Flow<PagingData<CountsShiftingPendingExecutionItemDto>> {
        val key = scopeKey(parkId, shedId)
        return Pager(
            config = PagingConfig(
                pageSize = SHIFTING_PENDING_PAGE_SIZE,
                initialLoadSize = SHIFTING_PENDING_PAGE_SIZE,
                // Prefetch the next page as the operator reaches ~item 17-18 of the current one.
                prefetchDistance = 3,
                enablePlaceholders = false,
                // Hard ceiling on rows retained in memory: three pages, then the far side is dropped
                // and re-read from Room on scroll back.
                maxSize = SHIFTING_PENDING_PAGE_SIZE * 3,
            ),
            remoteMediator = ShiftingPendingRemoteMediator(
                parkId = parkId,
                shedId = shedId,
                api = api,
                database = database,
                json = json,
                clock = clock,
            ),
            pagingSourceFactory = { database.shiftingPendingItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<CountsShiftingPendingExecutionItemDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun forgetExecuted(shiftingEventId: String) {
        database.shiftingPendingItemDao().deleteMovement(shiftingEventId)
    }

    override suspend fun findCached(shiftingEventId: String): CountsShiftingPendingExecutionItemDto? =
        database.shiftingPendingItemDao().findById(shiftingEventId)
            ?.let { json.decodeFromString<CountsShiftingPendingExecutionItemDto>(it.dtoJson) }

    private fun scopeKey(parkId: String, shedId: String): String =
        cacheKey("shifting-pending", parkId, shedId)
}

/**
 * Fills Room from the backend page-by-page over the endpoint's opaque keyset cursor. Room stays the
 * single source of truth: this never hands rows to the UI, it only writes them, and the
 * [androidx.paging.PagingSource] re-emits.
 *
 * Keyset, not offset, because the queue is drained by several operators at once — an offset page
 * would skip or repeat movements while one of them pages through it.
 */
@OptIn(ExperimentalPagingApi::class)
private class ShiftingPendingRemoteMediator(
    private val parkId: String,
    private val shedId: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, ShiftingPendingItemEntity>() {
    private val queryKey = cacheKey("shifting-pending", parkId, shedId)

    /**
     * ALWAYS refresh on open. This is a work queue answering "what is waiting on me RIGHT NOW":
     * skipping the fetch inside a TTL would show an operator a movement someone else already
     * completed, or hide one just approved. Paging renders the cached Room window immediately while
     * this refresh runs, and a failed refresh leaves those cached rows on screen behind a stale
     * banner (docs/decisions/android-offline-first.md — refresh-on-open, stale-while-revalidate).
     */
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, ShiftingPendingItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.shiftingPendingRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isNullOrBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.listCountsShiftingPendingExecution(
                parkId = parkId.ifBlank { null },
                shedId = shedId.ifBlank { null },
                pageSize = SHIFTING_PENDING_PAGE_SIZE,
                cursor = cursor,
            )
            // An absent next_cursor is the contract's own end-of-pages signal. A cursor that did not
            // ADVANCE is also treated as the end: without that check a backend echoing the same
            // cursor would spin this mediator forever on one page (the non-terminating pagination
            // loop the scale rules ban).
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows and their cursor commit TOGETHER. A crash between them would otherwise leave
            // the cursor pointing past rows that were never stored, silently losing a page of the
            // operator's work queue.
            database.withTransaction {
                val itemDao = database.shiftingPendingItemDao()
                val remoteKeyDao = database.shiftingPendingRemoteKeyDao()
                val startIndex = if (loadType == LoadType.REFRESH) {
                    // A refresh re-reads the queue from the top: drop the scope's rows so a movement
                    // that was completed elsewhere (another operator) disappears instead of lingering
                    // as an untappable ghost.
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                    0
                } else {
                    itemDao.countFor(queryKey)
                }
                itemDao.upsertAll(
                    response.items.mapIndexed { index, item ->
                        ShiftingPendingItemEntity(
                            queryKey = queryKey,
                            shiftingEventId = item.shiftingEventId,
                            // Server order preserved by offsetting the page's own index, so a later
                            // page never sorts above an earlier one.
                            sortIndex = startIndex + index,
                            raisedAt = item.raisedAt,
                            dtoJson = json.encodeToString(item),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    ShiftingPendingRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(SHIFTING_PENDING_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(SHIFTING_PENDING_CACHED_QUERIES)
                }
            }
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            // Room keeps whatever it already had: a failed page load surfaces as a Paging
            // LoadState.Error the screen renders next to the cached rows, never as a wipe.
            MediatorResult.Error(error)
        }
    }
}
