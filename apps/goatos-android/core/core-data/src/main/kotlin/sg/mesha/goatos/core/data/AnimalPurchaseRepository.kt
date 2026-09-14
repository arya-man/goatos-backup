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
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.AnimalPurchaseAnimalItemEntity
import sg.mesha.goatos.core.data.cache.AnimalPurchaseAnimalRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.AnimalPurchaseBlobCacheEntity
import sg.mesha.goatos.core.data.cache.AnimalPurchaseLoadItemEntity
import sg.mesha.goatos.core.data.cache.AnimalPurchaseLoadRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.data.sync.AnimalPurchaseAnimalCreatePayload
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDetailDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionsDto

/** One screen-page of loads or animals — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val ANIMAL_PURCHASE_PAGE_SIZE = 20

/** How many distinct loads keep their cached animal window. */
private const val ANIMAL_PURCHASE_CACHED_LOADS = 6

/** Bump whenever the cached row JSON changes shape incompatibly (see PACKING_CACHE_SHAPE's kdoc in
 *  FeedRepository.kt for why a stale-shape row must be orphaned, never leniently decoded). */
private const val ANIMAL_PURCHASE_CACHE_SHAPE = "ap-v2"

/** The list read's caller flag, cached beside the rows so the "Add load" action survives a restart. */
@Serializable
data class AnimalPurchaseLoadsMeta(
    @SerialName("can_record") val canRecord: Boolean = false,
)

/**
 * Animal purchases reads (maintainer decision 2026-09-13, docs/decisions/animal-purchases.md).
 * Offline-first per docs/decisions/android-offline-first.md: Room is the UI's single source of
 * truth — the load list, one load's header and its animal rows all render from Room, and network
 * refreshes upsert Room. WRITES do not live here: the load and animal creates ride the durable
 * outbox through `SyncRepository.enqueueAnimalPurchaseLoadCreate` / `enqueueAnimalPurchaseAnimalCreate`,
 * and the sync engine reconciles each successful write's returned row back through
 * [persistServerLoad] / [persistServerAnimal].
 *
 * The DECISION on an animal is SERVER-owned: a cached row renders instantly, but the chip it
 * carries is whatever the server last said — the load screen refreshes on open, on resume and on a
 * timer, and never derives a decision itself.
 */
interface AnimalPurchaseRepository {
    /** The paged load list (newest first), a Room PagingSource filled by a RemoteMediator. */
    fun loads(): Flow<PagingData<AnimalPurchaseLoadDto>>

    /** Whether THIS caller may add loads and animals, from the last list/detail refresh. */
    fun observeCanRecord(): Flow<Boolean>

    /** Drops the list's freshness marker so the next pager refetches instead of TTL-skipping.
     *  Cached rows keep serving until fresh rows land. */
    suspend fun invalidateLoads()

    /** Room-first form vocabulary and copy; null while nothing is cached yet. */
    fun observeOptions(): Flow<AnimalPurchaseOptionsDto?>

    /** Network -> Room options refresh. Non-blocking contract: a failure leaves the cache serving. */
    suspend fun refreshOptions()

    /** Room-first load header; null while nothing is cached yet (corrupt rows quarantine). */
    fun observeLoad(loadId: String): Flow<AnimalPurchaseLoadDto?>

    /** The paged animals of one load, in recording order, a Room PagingSource filled by a
     *  RemoteMediator whose first page is the load detail read itself. */
    fun animals(loadId: String): Flow<PagingData<AnimalPurchaseAnimalDto>>

    /** One recorded animal from the load's cached window (null until the window holds it). */
    fun observeAnimal(loadId: String, candidateId: String): Flow<AnimalPurchaseAnimalDto?>

    /** Drops one load's freshness marker so its next pager refetches (header included). */
    suspend fun invalidateAnimals(loadId: String)

    /** Network -> Room refresh of one load's header AND first animal page. Non-blocking contract. */
    suspend fun refreshLoad(loadId: String)

    /** Reconciles a successful load-create dispatch's RETURNED load into Room. Sync engine only. */
    suspend fun persistServerLoad(load: AnimalPurchaseLoadDto)

    /** Reconciles a successful animal-create dispatch's RETURNED animal into Room. Sync engine only. */
    suspend fun persistServerAnimal(animal: AnimalPurchaseAnimalDto)

    /**
     * Animals recorded on THIS phone for the load that have not reached the server yet -- the
     * outbox rows still queued, in flight, or retrying. A director recording on a vendor's farm
     * with no signal must see what they saved, or they will record the animal twice; the rows
     * disappear on their own the moment the server accepts them and the real row lands.
     */
    fun observeQueuedAnimals(loadId: String): Flow<List<QueuedAnimalPurchaseAnimal>>
}

/** One not-yet-sent animal, decoded from its outbox payload. */
data class QueuedAnimalPurchaseAnimal(
    val outboxItemId: String,
    val loadId: String,
    val request: AnimalPurchaseAnimalCreateRequestDto,
    val queuedAtMs: Long,
    /** Every capture upload row this animal waits on, slot order then capture order; the create
     *  cannot send until all of them succeed. */
    val proofOutboxItemIds: List<String>,
)

class DefaultAnimalPurchaseRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
    /** Active outbox rows of one op type (the Clock repository's shape): injected so this
     *  module needs no handle on the outbox database itself. */
    private val activeOutboxRows: (opType: String) -> Flow<List<OutboxEntity>> = { _ -> flowOf(emptyList()) },
) : AnimalPurchaseRepository {

    @OptIn(ExperimentalPagingApi::class)
    override fun loads(): Flow<PagingData<AnimalPurchaseLoadDto>> = Pager(
        config = pagingConfig(),
        remoteMediator = AnimalPurchaseLoadRemoteMediator(api, database, json, clock),
        pagingSourceFactory = { database.animalPurchaseLoadItemDao().pagingSource(loadsScopeKey()) },
    ).flow
        .map { page -> page.map { entity -> json.decodeFromString<AnimalPurchaseLoadDto>(entity.dtoJson) } }
        // Decode off Main: the JSON parse happens once, here, not on the UI thread.
        .flowOn(Dispatchers.Default)

    override fun observeCanRecord(): Flow<Boolean> =
        observeBlob<AnimalPurchaseLoadsMeta>(LOADS_META_KEY).map { it?.canRecord ?: false }

    override suspend fun invalidateLoads() {
        // Deleting the remote key makes the next mediator initialize() LAUNCH_INITIAL_REFRESH;
        // the item rows are left in place so the screen keeps rendering until fresh rows land.
        database.animalPurchaseLoadRemoteKeyDao().delete(loadsScopeKey())
    }

    override fun observeOptions(): Flow<AnimalPurchaseOptionsDto?> = observeBlob(OPTIONS_KEY)

    override suspend fun refreshOptions() {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cached vocabulary
        // keeps the form usable and the next successful open repairs it.
        runCatching { putBlob(OPTIONS_KEY, json.encodeToString(api.getAnimalPurchaseOptions())) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "animal_purchase_options_refresh_failed", it)
            }
    }

    override fun observeLoad(loadId: String): Flow<AnimalPurchaseLoadDto?> = observeBlob(LOAD_KEY_PREFIX + loadId)

    @OptIn(ExperimentalPagingApi::class)
    override fun animals(loadId: String): Flow<PagingData<AnimalPurchaseAnimalDto>> = Pager(
        config = pagingConfig(),
        remoteMediator = AnimalPurchaseAnimalRemoteMediator(loadId, api, database, json, clock, ::persistDetailPage),
        pagingSourceFactory = { database.animalPurchaseAnimalItemDao().pagingSource(loadId) },
    ).flow
        .map { page -> page.map { entity -> json.decodeFromString<AnimalPurchaseAnimalDto>(entity.dtoJson) } }
        .flowOn(Dispatchers.Default)

    override fun observeAnimal(loadId: String, candidateId: String): Flow<AnimalPurchaseAnimalDto?> =
        database.animalPurchaseAnimalItemDao().observe(loadId, candidateId).map { row ->
            row?.let {
                runCatching { json.decodeFromString<AnimalPurchaseAnimalDto>(it.dtoJson) }
                    .onFailure { err ->
                        if (err is CancellationException) throw err
                        android.util.Log.w(LOG_TAG, "animal_purchase_cached_animal_decode_failed load=$loadId candidate=$candidateId", err)
                    }
                    .getOrNull()
            }
        }

    override suspend fun invalidateAnimals(loadId: String) {
        database.animalPurchaseAnimalRemoteKeyDao().delete(loadId)
    }

    override suspend fun refreshLoad(loadId: String) {
        // exception:exempt expected refresh failure; the cached header and rows keep serving.
        runCatching { persistDetailPage(loadId, api.getAnimalPurchaseLoad(loadId), replaceWindow = true) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "animal_purchase_load_refresh_failed load=$loadId", it)
            }
    }

    override suspend fun persistServerLoad(load: AnimalPurchaseLoadDto) {
        if (load.loadId.isBlank()) return
        val now = clock()
        val rowJson = json.encodeToString(load)
        val itemDao = database.animalPurchaseLoadItemDao()
        database.withTransaction {
            database.animalPurchaseBlobCacheDao().upsert(AnimalPurchaseBlobCacheEntity(LOAD_KEY_PREFIX + load.loadId, rowJson, now))
            val existing = itemDao.rowsForLoad(load.loadId)
            if (existing.isNotEmpty()) {
                itemDao.upsertAll(existing.map { it.copy(dtoJson = rowJson, updatedAt = now) })
            }
            // A newly recorded load belongs at the TOP of the newest-first list. Its cached rows
            // are dropped and the freshness marker cleared, so the next list open re-reads from
            // the server in the server's order rather than showing the phone's placeholder order.
            database.animalPurchaseLoadRemoteKeyDao().delete(loadsScopeKey())
        }
        database.animalPurchaseBlobCacheDao().enforceCacheBounds()
    }

    override suspend fun persistServerAnimal(animal: AnimalPurchaseAnimalDto) {
        if (animal.candidateId.isBlank() || animal.loadId.isBlank()) return
        val now = clock()
        val rowJson = json.encodeToString(animal)
        val itemDao = database.animalPurchaseAnimalItemDao()
        database.withTransaction {
            val existing = itemDao.get(animal.loadId, animal.candidateId)
            itemDao.upsertAll(
                listOf(
                    AnimalPurchaseAnimalItemEntity(
                        queryKey = animal.loadId,
                        grainKey = animal.candidateId,
                        // Recording order: the server's seq_no is the list order, so a new row
                        // lands after every row already cached for this load.
                        sortIndex = existing?.sortIndex ?: (itemDao.maxSortIndex(animal.loadId) + 1),
                        dtoJson = rowJson,
                        updatedAt = now,
                    ),
                ),
            )
            // The header's whole-load counts moved server-side; clear the freshness marker so the
            // detail screen's next open/resume/timer read pulls the server's counts and chips.
            database.animalPurchaseAnimalRemoteKeyDao().delete(animal.loadId)
        }
    }

    override fun observeQueuedAnimals(loadId: String): Flow<List<QueuedAnimalPurchaseAnimal>> =
        activeOutboxRows(OutboxOpType.ANIMAL_PURCHASE_ANIMAL_CREATE.name).map { rows ->
            rows.mapNotNull { row ->
                val payload = runCatching { json.decodeFromString<AnimalPurchaseAnimalCreatePayload>(row.payloadJson) }
                    .onFailure { err ->
                        if (err is CancellationException) throw err
                        android.util.Log.w(LOG_TAG, "animal_purchase_queued_animal_payload_decode_failed item=${row.id}", err)
                    }
                    .getOrNull()
                    ?: return@mapNotNull null
                if (payload.loadId != loadId) return@mapNotNull null
                QueuedAnimalPurchaseAnimal(outboxItemId = row.id, loadId = loadId, request = payload.request, queuedAtMs = row.createdAt, proofOutboxItemIds = payload.allProofOutboxItemIds)
            }
        }

    /**
     * Writes one detail read — the load header, the caller flag and the FIRST animal page — into
     * Room in one transaction. [replaceWindow] drops the load's cached animal rows first (a refresh
     * re-reads from the top so a row the server no longer lists disappears); the mediator's REFRESH
     * passes true, and so does [refreshLoad].
     */
    private suspend fun persistDetailPage(loadId: String, detail: AnimalPurchaseLoadDetailDto, replaceWindow: Boolean) {
        val now = clock()
        val blobDao = database.animalPurchaseBlobCacheDao()
        val itemDao = database.animalPurchaseAnimalItemDao()
        val remoteKeyDao = database.animalPurchaseAnimalRemoteKeyDao()
        val loadJson = json.encodeToString(detail.load)
        val loadItemDao = database.animalPurchaseLoadItemDao()
        val nextCursor = detail.nextCursor.takeIf { it.isNotBlank() }
        database.withTransaction {
            blobDao.upsert(AnimalPurchaseBlobCacheEntity(LOAD_KEY_PREFIX + loadId, loadJson, now))
            blobDao.upsert(AnimalPurchaseBlobCacheEntity(LOADS_META_KEY, json.encodeToString(AnimalPurchaseLoadsMeta(detail.canRecord)), now))
            // The list row of this load adopts the fresh header (its counts/summary) too.
            loadItemDao.upsertAll(loadItemDao.rowsForLoad(loadId).map { it.copy(dtoJson = loadJson, updatedAt = now) })
            if (replaceWindow) {
                itemDao.deleteQuery(loadId)
                remoteKeyDao.delete(loadId)
            }
            val rowBase = itemDao.countForQuery(loadId)
            itemDao.upsertAll(
                detail.animals.mapIndexed { index, animal ->
                    AnimalPurchaseAnimalItemEntity(
                        queryKey = loadId,
                        grainKey = animal.candidateId,
                        sortIndex = rowBase + index,
                        dtoJson = json.encodeToString(animal),
                        updatedAt = now,
                    )
                },
            )
            remoteKeyDao.upsert(
                AnimalPurchaseAnimalRemoteKeyEntity(
                    queryKey = loadId,
                    nextCursor = nextCursor.orEmpty(),
                    endReached = nextCursor == null,
                    updatedAt = now,
                ),
            )
            itemDao.deleteRowsOutsideNewestLoads(ANIMAL_PURCHASE_CACHED_LOADS)
            remoteKeyDao.deleteOutsideNewestLoads(ANIMAL_PURCHASE_CACHED_LOADS)
        }
        blobDao.enforceCacheBounds()
    }

    private inline fun <reified T> observeBlob(key: String): Flow<T?> =
        database.animalPurchaseBlobCacheDao().observe(key)
            .map { entity ->
                readCachedJson<T>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.animalPurchaseBlobCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    private suspend fun putBlob(key: String, dtoJson: String) {
        database.animalPurchaseBlobCacheDao().upsert(AnimalPurchaseBlobCacheEntity(key, dtoJson, clock()))
        database.animalPurchaseBlobCacheDao().enforceCacheBounds()
    }

    private fun pagingConfig() = PagingConfig(
        pageSize = ANIMAL_PURCHASE_PAGE_SIZE,
        initialLoadSize = ANIMAL_PURCHASE_PAGE_SIZE,
        prefetchDistance = 3,
        enablePlaceholders = false,
        maxSize = ANIMAL_PURCHASE_PAGE_SIZE * 3,
    )

    private companion object {
        const val LOG_TAG = "GoatOsAnimalPurchase"
        const val OPTIONS_KEY = "options"
        const val LOAD_KEY_PREFIX = "load:"
    }
}

/** Blob key of the list read's caller flag, shared by the list mediator and the detail write. */
private const val LOADS_META_KEY = "loads:meta"

/** The ONE scope key of the load list (no filters today), built from the shape + page size. */
private fun loadsScopeKey(): String =
    cacheKey(ANIMAL_PURCHASE_CACHE_SHAPE, "animal-purchase-loads", ANIMAL_PURCHASE_PAGE_SIZE.toString())

/**
 * Fills Room from `GET /app/procurement/animal-purchases/loads` page-by-page over the endpoint's
 * opaque keyset cursor. Room stays the single source of truth: this never hands rows to the UI, it
 * only writes them, and the [androidx.paging.PagingSource] re-emits.
 *
 * ALWAYS refresh on open (docs/decisions/android-offline-first.md — refresh-on-open,
 * stale-while-revalidate): the whole-load counts on every row move as the CEO decides, and a
 * failed refresh leaves the cached rows on screen.
 */
@OptIn(ExperimentalPagingApi::class)
private class AnimalPurchaseLoadRemoteMediator(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
) : RemoteMediator<Int, AnimalPurchaseLoadItemEntity>() {
    private val queryKey = loadsScopeKey()

    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, AnimalPurchaseLoadItemEntity>,
    ): MediatorResult {
        val cursor = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> null
            LoadType.APPEND -> {
                val remoteKey = database.animalPurchaseLoadRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached || remoteKey.nextCursor.isBlank()) {
                    return MediatorResult.Success(endOfPaginationReached = true)
                }
                remoteKey.nextCursor
            }
        }
        return try {
            val response = api.getAnimalPurchaseLoads(limit = ANIMAL_PURCHASE_PAGE_SIZE, cursor = cursor)
            // An absent next_cursor is the contract's own end-of-pages signal; a cursor that did
            // not ADVANCE is also the end, or an echoing backend would spin this mediator forever
            // on one page (the non-terminating pagination loop the scale rules ban).
            val nextCursor = response.nextCursor.takeIf { it.isNotBlank() }
            val endReached = nextCursor == null || nextCursor == cursor
            val updatedAt = clock()
            // Page rows and their cursor commit TOGETHER, or a crash between them leaves the
            // cursor pointing past rows that were never stored.
            database.withTransaction {
                val itemDao = database.animalPurchaseLoadItemDao()
                val remoteKeyDao = database.animalPurchaseLoadRemoteKeyDao()
                if (loadType == LoadType.REFRESH) {
                    itemDao.deleteQuery(queryKey)
                    remoteKeyDao.delete(queryKey)
                    database.animalPurchaseBlobCacheDao().upsert(
                        AnimalPurchaseBlobCacheEntity(LOADS_META_KEY, json.encodeToString(AnimalPurchaseLoadsMeta(response.canRecord)), updatedAt),
                    )
                }
                val rowBase = itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.loads.mapIndexed { index, load ->
                        AnimalPurchaseLoadItemEntity(
                            queryKey = queryKey,
                            grainKey = load.loadId,
                            // Server order preserved by offsetting the page's own index, so a
                            // later page never sorts above an earlier one.
                            sortIndex = rowBase + index,
                            dtoJson = json.encodeToString(load),
                            updatedAt = updatedAt,
                        )
                    },
                )
                remoteKeyDao.upsert(
                    AnimalPurchaseLoadRemoteKeyEntity(
                        queryKey = queryKey,
                        nextCursor = nextCursor.orEmpty(),
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

/**
 * Fills one load's animal window. REFRESH reads the load DETAIL (header + first page + the caller
 * flag, one request); APPEND reads `GET .../loads/{id}/animals?cursor=` page by page. The header
 * blob and the first page commit together through [persistPage], so a refresh never shows a
 * fresh chip on a row beside stale counts in the header.
 */
@OptIn(ExperimentalPagingApi::class)
private class AnimalPurchaseAnimalRemoteMediator(
    private val loadId: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val persistPage: suspend (loadId: String, detail: AnimalPurchaseLoadDetailDto, replaceWindow: Boolean) -> Unit,
) : RemoteMediator<Int, AnimalPurchaseAnimalItemEntity>() {

    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(
        loadType: LoadType,
        state: PagingState<Int, AnimalPurchaseAnimalItemEntity>,
    ): MediatorResult {
        if (loadId.isBlank()) return MediatorResult.Success(endOfPaginationReached = true)
        return try {
            when (loadType) {
                LoadType.PREPEND -> MediatorResult.Success(endOfPaginationReached = true)
                LoadType.REFRESH -> {
                    val detail = api.getAnimalPurchaseLoad(loadId)
                    persistPage(loadId, detail, true)
                    MediatorResult.Success(endOfPaginationReached = detail.nextCursor.isBlank())
                }
                LoadType.APPEND -> {
                    val remoteKey = database.animalPurchaseAnimalRemoteKeyDao().get(loadId)
                        ?: return MediatorResult.Success(endOfPaginationReached = true)
                    if (remoteKey.endReached || remoteKey.nextCursor.isBlank()) {
                        return MediatorResult.Success(endOfPaginationReached = true)
                    }
                    val cursor = remoteKey.nextCursor
                    val response = api.getAnimalPurchaseAnimals(loadId, limit = ANIMAL_PURCHASE_PAGE_SIZE, cursor = cursor)
                    val nextCursor = response.nextCursor.takeIf { it.isNotBlank() }
                    val endReached = nextCursor == null || nextCursor == cursor
                    val updatedAt = clock()
                    database.withTransaction {
                        val itemDao = database.animalPurchaseAnimalItemDao()
                        val rowBase = itemDao.countForQuery(loadId)
                        itemDao.upsertAll(
                            response.animals.mapIndexed { index, animal ->
                                AnimalPurchaseAnimalItemEntity(
                                    queryKey = loadId,
                                    grainKey = animal.candidateId,
                                    sortIndex = rowBase + index,
                                    dtoJson = json.encodeToString(animal),
                                    updatedAt = updatedAt,
                                )
                            },
                        )
                        database.animalPurchaseAnimalRemoteKeyDao().upsert(
                            AnimalPurchaseAnimalRemoteKeyEntity(
                                queryKey = loadId,
                                nextCursor = nextCursor.orEmpty(),
                                endReached = endReached,
                                updatedAt = updatedAt,
                            ),
                        )
                    }
                    MediatorResult.Success(endOfPaginationReached = endReached)
                }
            }
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Exception) {
            MediatorResult.Error(error)
        }
    }
}
