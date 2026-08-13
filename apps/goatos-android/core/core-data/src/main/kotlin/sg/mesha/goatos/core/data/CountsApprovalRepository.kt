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
import sg.mesha.goatos.core.data.cache.CountsApprovalItemEntity
import sg.mesha.goatos.core.data.cache.CountsApprovalRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto

/**
 * One screen-page of pending decisions. The backend caps this endpoint at 20 server-side because
 * the queue is read from a phone; the client asks for the same bound so both layers page
 * identically (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
const val COUNTS_APPROVAL_PAGE_SIZE = 20

/** How many queue scopes (status filters) keep their cached rows. Bounds the tables over months. */
private const val COUNTS_APPROVAL_CACHED_QUERIES = 6

/** The queue's default scope: what an approver is actually there to act on. */
const val COUNTS_APPROVAL_STATUS_PENDING = "pending"

/**
 * The Counts approver's queue (`GET /app/counts/approvals`).
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of truth.
 * The screen observes a Room [androidx.paging.PagingSource]; a `RemoteMediator` fills Room from the
 * backend page-by-page. Re-entering the tab renders the cached queue immediately and refreshes
 * behind it — never a blank wall over cached rows.
 *
 * The DECISION side (approve / reject) does NOT live here. Those are mutating writes and go through
 * the durable outbox (`sync/SyncRepository`) exactly like every other Counts write, so a decision
 * made in a shed with no signal is durable on the phone and replays under one stable idempotency
 * key instead of being applied twice.
 */
interface CountsApprovalRepository {
    /**
     * The paged queue. Both layers page identically at [COUNTS_APPROVAL_PAGE_SIZE] over the
     * backend's own opaque keyset cursor; nothing ever holds the whole backlog.
     */
    fun approvals(status: String = COUNTS_APPROVAL_STATUS_PENDING): Flow<PagingData<CountsApprovalListItemDto>>

    /**
     * Removes a decided request from the cached queue.
     *
     * Called as soon as a decision is durably queued, so the row leaves the pending list at the
     * moment the approver acts rather than at the next refresh — and, more importantly, so the same
     * request cannot be decided twice while its first decision is still draining from the outbox.
     */
    suspend fun forgetDecided(approvalRequestId: String)
}

class DefaultCountsApprovalRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : CountsApprovalRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun approvals(status: String): Flow<PagingData<CountsApprovalListItemDto>> {
        val key = scopeKey(status)
        return Pager(
            config = PagingConfig(
                pageSize = COUNTS_APPROVAL_PAGE_SIZE,
                initialLoadSize = COUNTS_APPROVAL_PAGE_SIZE,
                // Prefetch the next page as the approver reaches ~item 17-18 of the current one.
                prefetchDistance = 3,
                enablePlaceholders = false,
                // Hard ceiling on rows retained in memory: three pages, then the far side is
                // dropped and re-read from Room on scroll back.
                maxSize = COUNTS_APPROVAL_PAGE_SIZE * 3,
            ),
            remoteMediator = CountsRequestRemoteMediator(
                queryKey = key,
                database = database,
                clock = clock,
                fetchPage = { cursor ->
                    val response = api.listCountsApprovals(
                        status = status,
                        pageSize = COUNTS_APPROVAL_PAGE_SIZE,
                        cursor = cursor,
                    )
                    CountsRequestPage(response.items, response.nextCursor)
                },
                requestId = { it.approvalRequestId },
                raisedAt = { it.raisedAt },
                encode = { json.encodeToString(it) },
            ),
            pagingSourceFactory = { database.countsApprovalItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<CountsApprovalListItemDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)
    }

    override suspend fun forgetDecided(approvalRequestId: String) {
        database.countsApprovalItemDao().deleteRequest(approvalRequestId)
    }

    private fun scopeKey(status: String): String = cacheKey("counts-approvals", status)
}

/**
 * Fills Room from the backend page-by-page over the endpoint's opaque keyset cursor. Room stays the
 * single source of truth: this never hands rows to the UI, it only writes them, and the
 * [androidx.paging.PagingSource] re-emits.
 *
 * Keyset, not offset, because the queue is appended to continuously — an offset page would skip or
 * repeat rows as new requests arrive while an approver pages through it.
 */
private data class CountsRequestPage<T>(val items: List<T>, val nextCursor: String?)

@OptIn(ExperimentalPagingApi::class)
private class CountsRequestRemoteMediator<T>(
    private val queryKey: String,
    private val database: GoatDatabase,
    private val clock: () -> Long,
    private val fetchPage: suspend (String?) -> CountsRequestPage<T>,
    private val requestId: (T) -> String,
    private val raisedAt: (T) -> String,
    private val encode: (T) -> String,
) : RemoteMediator<Int, CountsApprovalItemEntity>() {

    /**
     * ALWAYS refresh on open — deliberately not the TTL-gated `SKIP_INITIAL_REFRESH` the
     * breakdown cache uses.
     *
     * This is a work queue, and the question it answers is "what is waiting on me RIGHT NOW".
     * Skipping the fetch inside a TTL means an approver who opens the tab is shown a queue that
     * is minutes stale: a request raised while they were on another screen is simply invisible,
     * and — worse — a request they just decided from another device still looks actionable. A
     * census rollup can be a few minutes old without misleading anyone; a pending-decision list
     * cannot.
     *
     * This costs nothing in blank-wall terms and stays inside the offline-first contract: Paging
     * renders the cached Room window immediately while this refresh runs in the background, and a
     * failed refresh leaves those cached rows on screen behind a stale banner
     * (docs/decisions/android-offline-first.md — refresh-on-open, stale-while-revalidate).
     */
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, CountsApprovalItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.countsApprovalRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isNullOrBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = fetchPage(cursor)
            // An absent next_cursor is the contract's own end-of-pages signal. A cursor that did
            // not ADVANCE is also treated as the end: without that check a backend echoing the
            // same cursor would spin this mediator forever on one page (the non-terminating
            // pagination loop the scale rules ban).
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows and their cursor commit TOGETHER. A crash between them would otherwise
            // leave the cursor pointing past rows that were never stored, silently losing a page
            // of pending decisions from the approver's queue.
            database.withTransaction {
                val itemDao = database.countsApprovalItemDao()
                val remoteKeyDao = database.countsApprovalRemoteKeyDao()
                val startIndex = if (loadType == LoadType.REFRESH) {
                    // A refresh re-reads the queue from the top: drop the scope's rows so a
                    // request that was decided elsewhere (admin web, another approver) disappears
                    // instead of lingering as an untappable ghost.
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                    0
                } else {
                    itemDao.countFor(queryKey)
                }
                itemDao.upsertAll(
                    response.items.mapIndexed { index, item ->
                        CountsApprovalItemEntity(
                            queryKey = queryKey,
                            approvalRequestId = requestId(item),
                            // Server order preserved by offsetting the page's own index, so a
                            // later page never sorts above an earlier one.
                            sortIndex = startIndex + index,
                            raisedAt = raisedAt(item),
                            dtoJson = encode(item),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    CountsApprovalRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteRowsOutsideNewestQueries(COUNTS_APPROVAL_CACHED_QUERIES)
                    remoteKeyDao.deleteOutsideNewestQueries(COUNTS_APPROVAL_CACHED_QUERIES)
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
