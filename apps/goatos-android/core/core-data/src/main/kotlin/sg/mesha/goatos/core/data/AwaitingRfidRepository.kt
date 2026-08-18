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
import sg.mesha.goatos.core.data.cache.AwaitingRfidItemEntity
import sg.mesha.goatos.core.data.cache.AwaitingRfidRemoteKeyEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.TemporaryTaggedGoatDto

/**
 * One screen-page of the "Awaiting RFID" list. The backend caps this endpoint at 20 server-side
 * because the list is read from a phone; the client asks for the same bound so both layers page
 * identically (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
const val AWAITING_RFID_PAGE_SIZE = 20

/**
 * The operator "Awaiting RFID" list (`GET /app/counts/goats/temporary-tagged`).
 *
 * Offline-first (docs/decisions/android-offline-first.md): Room is the UI's single source of truth.
 * The screen observes a Room [androidx.paging.PagingSource]; a `RemoteMediator` fills Room from the
 * backend page-by-page. Re-entering the list renders the cached rows immediately and refreshes behind
 * them — never a blank wall over cached rows.
 *
 * The PROMOTE side (assigning the permanent RFID) does NOT live here. That is a mutating write and
 * goes through the durable outbox (`sync/SyncRepository`) exactly like every other Counts write, so a
 * promotion done in a shed with no signal is durable on the phone and replays under one stable
 * idempotency key instead of retagging twice.
 */
/**
 * The operator's optional location filter for the "Awaiting RFID" list (park -> shed cascade). A
 * null dimension means unfiltered. shed_id alone is sufficient (a shed belongs to one park), but the
 * cascade sends both once a shed is chosen.
 */
data class AwaitingRfidFilter(
    val parkId: String? = null,
    val shedId: String? = null,
)

interface AwaitingRfidRepository {
    /**
     * The paged list. Both layers page identically at [AWAITING_RFID_PAGE_SIZE] over the backend's own
     * keyset cursor (the last row's display_id); nothing ever holds the whole backlog.
     *
     * [filter] narrows the list to one park/shed server-side. A REFRESH clears and refills the whole
     * cache, so the Room table only ever holds the CURRENT filter's rows — the observed read needs no
     * filter of its own and no schema change. A filter change re-creates the Pager, which triggers a
     * REFRESH that swaps the cached page for the new filter's page.
     */
    fun awaiting(filter: AwaitingRfidFilter = AwaitingRfidFilter()): Flow<PagingData<TemporaryTaggedGoatDto>>

    /**
     * Removes a promoted goat from the cached list.
     *
     * Called only after the promote succeeds. A terminal rejection must leave the cached goat
     * available for correction instead of silently losing it from the awaiting-RFID queue.
     */
    suspend fun forgetPromoted(goatId: String)

    /**
     * The cached goat by id, or null if it is no longer on the list (already promoted). Backs the
     * promote screen's offline-first open — no refetch, since the operator taps a row already in Room.
     */
    suspend fun findCached(goatId: String): TemporaryTaggedGoatDto?
}

class DefaultAwaitingRfidRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : AwaitingRfidRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun awaiting(filter: AwaitingRfidFilter): Flow<PagingData<TemporaryTaggedGoatDto>> =
        Pager(
            config = PagingConfig(
                pageSize = AWAITING_RFID_PAGE_SIZE,
                initialLoadSize = AWAITING_RFID_PAGE_SIZE,
                // Prefetch the next page as the operator reaches ~item 17-18 of the current one.
                prefetchDistance = 3,
                enablePlaceholders = false,
                // Hard ceiling on rows retained in memory: three pages, then the far side is dropped
                // and re-read from Room on scroll back.
                maxSize = AWAITING_RFID_PAGE_SIZE * 3,
            ),
            remoteMediator = AwaitingRfidRemoteMediator(
                api = api,
                database = database,
                json = json,
                clock = clock,
                filter = filter,
            ),
            pagingSourceFactory = { database.awaitingRfidItemDao().pagingSource() },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<TemporaryTaggedGoatDto>(entity.dtoJson) } }
            // Decode off Main: the JSON parse happens once, here, not on the UI thread.
            .flowOn(Dispatchers.Default)

    override suspend fun forgetPromoted(goatId: String) {
        database.awaitingRfidItemDao().deleteGoat(goatId)
    }

    override suspend fun findCached(goatId: String): TemporaryTaggedGoatDto? =
        database.awaitingRfidItemDao().findById(goatId)
            ?.let { json.decodeFromString<TemporaryTaggedGoatDto>(it.dtoJson) }
}

/**
 * Fills Room from the backend page-by-page over the endpoint's keyset cursor. Room stays the single
 * source of truth: this never hands rows to the UI, it only writes them, and the
 * [androidx.paging.PagingSource] re-emits.
 *
 * Keyset, not offset, because the list may be drained by several operators retagging at once — an
 * offset page would skip or repeat goats while one of them pages through it.
 */
@OptIn(ExperimentalPagingApi::class)
private class AwaitingRfidRemoteMediator(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val filter: AwaitingRfidFilter,
) : RemoteMediator<Int, AwaitingRfidItemEntity>() {
    private val scope = AwaitingRfidRemoteKeyEntity.SCOPE

    /**
     * ALWAYS refresh on open. This is a work list answering "who needs a permanent RFID RIGHT NOW":
     * skipping the fetch inside a TTL would show an operator a goat someone else already promoted, or
     * hide one just tagged. Paging renders the cached Room window immediately while this refresh runs,
     * and a failed refresh leaves those cached rows on screen behind a stale banner
     * (docs/decisions/android-offline-first.md — refresh-on-open, stale-while-revalidate).
     */
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, AwaitingRfidItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.awaitingRfidRemoteKeyDao().get(scope)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isNullOrBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.listCountsTemporaryTaggedGoats(
                pageSize = AWAITING_RFID_PAGE_SIZE,
                cursor = cursor,
                parkId = filter.parkId?.takeIf { it.isNotBlank() },
                shedId = filter.shedId?.takeIf { it.isNotBlank() },
            )
            // An absent next_cursor is the contract's own end-of-pages signal. A cursor that did not
            // ADVANCE is also treated as the end: without that check a backend echoing the same cursor
            // would spin this mediator forever on one page (the non-terminating pagination loop the
            // scale rules ban).
            val nextCursor = response.nextCursor?.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows and their cursor commit TOGETHER. A crash between them would otherwise leave
            // the cursor pointing past rows that were never stored, silently losing a page of the
            // operator's work list.
            database.withTransaction {
                val itemDao = database.awaitingRfidItemDao()
                val remoteKeyDao = database.awaitingRfidRemoteKeyDao()
                val startIndex = if (loadType == LoadType.REFRESH) {
                    // A refresh re-reads the list from the top: drop the cached rows so a goat that was
                    // promoted elsewhere (another operator) disappears instead of lingering as an
                    // untappable ghost.
                    itemDao.deleteAll()
                    remoteKeyDao.delete(scope)
                    0
                } else {
                    itemDao.countAll()
                }
                itemDao.upsertAll(
                    response.items.mapIndexed { index, item ->
                        AwaitingRfidItemEntity(
                            goatId = item.goatId,
                            // Server order preserved by offsetting the page's own index, so a later
                            // page never sorts above an earlier one.
                            sortIndex = startIndex + index,
                            displayId = item.displayId,
                            dtoJson = json.encodeToString(item),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    AwaitingRfidRemoteKeyEntity(
                        id = scope,
                        nextCursor = nextCursor,
                        endReached = endReached,
                        updatedAt = updatedAt,
                    ),
                )
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
