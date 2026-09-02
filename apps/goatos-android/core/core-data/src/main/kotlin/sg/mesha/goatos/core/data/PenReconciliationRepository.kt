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
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.PenReconciliationItemEntity
import sg.mesha.goatos.core.data.cache.PenReconciliationRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCardDto
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationStatusCountsDto

/** Whole-filter chip counts for the Reconcile tab — backend truth, never page-derived. */
data class PenReconciliationMeta(
    val status: String = "",
    val counts: CountsPenReconciliationStatusCountsDto = CountsPenReconciliationStatusCountsDto(),
)

/**
 * One screen-page of the Reconcile queue. The backend caps this endpoint at 20 server-side; the
 * client asks for the same bound so both layers page identically
 * (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
const val PEN_RECONCILIATION_PAGE_SIZE = 20

/** How many status scopes keep their cached rows. Bounds the table. */
private const val PEN_RECONCILIATION_CACHED_QUERIES = 4

/**
 * The Herd Operations RECONCILE queue (`GET /app/counts/pen-reconciliation/cards`) — "wrong pen"
 * cards raised by weighing submits (docs/decisions/pen-reconciliation.md).
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of
 * truth. The screen observes a Room [androidx.paging.PagingSource]; a `RemoteMediator` fills Room
 * from the backend page-by-page over the endpoint's opaque keyset cursor.
 *
 * The EXECUTION side ("Mark done") does NOT live here. That is a mutating write and goes through
 * the durable outbox (`sync/SyncRepository`) exactly like every other Counts write, so a
 * completion done in a shed with no signal is durable on the phone and replays under one stable
 * idempotency key instead of submitting twice.
 */
interface PenReconciliationRepository {
    val meta: StateFlow<PenReconciliationMeta>

    /** The paged Reconcile cards for one backend status bucket (all/open/…). */
    fun cards(status: String = "all"): Flow<PagingData<CountsPenReconciliationCardDto>>

    /**
     * Removes a submitted card from the cached queue the moment its completion is durably queued,
     * so it leaves the actionable list immediately and cannot be completed twice while the first
     * completion is still draining.
     */
    suspend fun forgetCompleted(cardId: String)

    /**
     * The cached card by id, or null if it is no longer in any cached scope. Backs the execute
     * screen's offline-first open — no refetch, since the operator taps a row already in Room.
     */
    suspend fun findCached(cardId: String): CountsPenReconciliationCardDto?
}

class DefaultPenReconciliationRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : PenReconciliationRepository {
    private val _meta = MutableStateFlow(PenReconciliationMeta())
    override val meta: StateFlow<PenReconciliationMeta> = _meta

    @OptIn(ExperimentalPagingApi::class)
    override fun cards(status: String): Flow<PagingData<CountsPenReconciliationCardDto>> {
        val key = scopeKey(status)
        return Pager(
            config = PagingConfig(
                pageSize = PEN_RECONCILIATION_PAGE_SIZE,
                initialLoadSize = PEN_RECONCILIATION_PAGE_SIZE,
                // Prefetch the next page as the operator reaches ~item 17-18 of the current one.
                prefetchDistance = 3,
                enablePlaceholders = false,
                // Hard ceiling on rows retained in memory: three pages, then the far side is
                // dropped and re-read from Room on scroll back.
                maxSize = PEN_RECONCILIATION_PAGE_SIZE * 3,
            ),
            remoteMediator = PenReconciliationRemoteMediator(
                status = status,
                api = api,
                database = database,
                json = json,
                clock = clock,
                onMeta = { counts -> _meta.value = PenReconciliationMeta(status, counts) },
            ),
            pagingSourceFactory = { database.penReconciliationItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<CountsPenReconciliationCardDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun forgetCompleted(cardId: String) {
        database.penReconciliationItemDao().deleteCard(cardId)
    }

    override suspend fun findCached(cardId: String): CountsPenReconciliationCardDto? =
        database.penReconciliationItemDao().findById(cardId)
            ?.let { json.decodeFromString<CountsPenReconciliationCardDto>(it.dtoJson) }

    private fun scopeKey(status: String): String = cacheKey("pen-reconciliation", status)
}

/**
 * Fills Room from the backend page-by-page over the endpoint's opaque keyset cursor. Room stays
 * the single source of truth: this never hands rows to the UI, it only writes them, and the
 * [androidx.paging.PagingSource] re-emits.
 */
@OptIn(ExperimentalPagingApi::class)
private class PenReconciliationRemoteMediator(
    private val status: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onMeta: (CountsPenReconciliationStatusCountsDto) -> Unit,
) : RemoteMediator<Int, PenReconciliationItemEntity>() {
    private val queryKey = cacheKey("pen-reconciliation", status)

    /**
     * ALWAYS refresh on open. This is a work queue answering "which animals are in the wrong pen
     * RIGHT NOW": skipping the fetch inside a TTL would show a card another operator already
     * cleared. Paging renders the cached Room window immediately while this refresh runs, and a
     * failed refresh leaves those cached rows on screen (stale-while-revalidate).
     */
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, PenReconciliationItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.penReconciliationRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isNullOrBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.listCountsPenReconciliationCards(
                status = status,
                pageSize = PEN_RECONCILIATION_PAGE_SIZE,
                cursor = cursor,
            )
            if (loadType == LoadType.REFRESH) onMeta(response.statusCounts)
            // An absent next_cursor is the contract's own end-of-pages signal. A cursor that did
            // not ADVANCE is also treated as the end — otherwise a backend echoing the same cursor
            // would spin this mediator forever on one page (the non-terminating pagination loop
            // the scale rules ban).
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            // Page rows and their cursor commit TOGETHER so a crash between them cannot leave the
            // cursor pointing past rows that were never stored.
            database.withTransaction {
                val itemDao = database.penReconciliationItemDao()
                val remoteKeyDao = database.penReconciliationRemoteKeyDao()
                // Wall-clock milliseconds can tie across quick status-chip refreshes. Store a
                // monotonic cache timestamp so detail lookup can choose the newest scope by
                // freshness alone, never by lexicographic queryKey fallback.
                val updatedAt = maxOf(clock(), (itemDao.maxUpdatedAt() ?: Long.MIN_VALUE) + 1)
                val startIndex = if (loadType == LoadType.REFRESH) {
                    // A refresh re-reads the queue from the top: drop the scope's rows so a card
                    // cleared elsewhere disappears instead of lingering as an untappable ghost.
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                    0
                } else {
                    itemDao.countFor(queryKey)
                }
                itemDao.upsertAll(
                    response.items.mapIndexed { index, item ->
                        PenReconciliationItemEntity(
                            queryKey = queryKey,
                            cardId = item.cardId,
                            // Server order preserved by offsetting the page's own index, so a
                            // later page never sorts above an earlier one.
                            sortIndex = startIndex + index,
                            raisedAt = item.raisedAt,
                            dtoJson = json.encodeToString(item),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    PenReconciliationRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(PEN_RECONCILIATION_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(PEN_RECONCILIATION_CACHED_QUERIES)
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
