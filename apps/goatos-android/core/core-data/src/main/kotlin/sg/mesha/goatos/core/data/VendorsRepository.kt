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
import sg.mesha.goatos.core.data.cache.FeedPurchaseItemEntity
import sg.mesha.goatos.core.data.cache.FeedPurchaseRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.VendorItemEntity
import sg.mesha.goatos.core.data.cache.VendorRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.VendorsBlobCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseOptionsDto
import sg.mesha.goatos.core.network.dto.VendorCatalogDto
import sg.mesha.goatos.core.network.dto.VendorDto

/** One screen-page of vendors or purchases — bounds BOTH the network request and the Room window
 *  (docs/decisions/mobile-data-fetch-anti-patterns.md). */
const val VENDORS_PAGE_SIZE = 20

/** How many distinct list scopes keep their cached rows. */
private const val VENDORS_CACHED_QUERIES = 6

/** Bump whenever the cached row JSON changes shape incompatibly (see PACKING_CACHE_SHAPE's kdoc). */
private const val VENDORS_CACHE_SHAPE = "vendors-v1"

/** Blob-cache key prefix of one feed purchase's detail row (written by the ledger page and by a landed create). */
private const val PURCHASE_KEY_PREFIX = "purchase:"

/** Whole-filter totals from the last ledger refresh, never page-local sums. */
data class FeedPurchaseTotals(val total: Int = 0, val quantityKg: Double = 0.0, val spendRupees: Double = 0.0)

/**
 * Vendors module reads (maintainer decision 2026-09-03): the vendor register and the feed purchase
 * ledger. Offline-first per docs/decisions/android-offline-first.md: Room is the UI's single
 * source of truth — both lists and every detail render from Room, and network refreshes upsert
 * Room. WRITES do not live here: a recorded vendor or purchase rides the durable outbox through
 * `SyncRepository.enqueueVendorCreate` / `enqueueFeedPurchaseCreate`, and the sync engine
 * reconciles each successful write's RETURNED row back through [persistServerVendor] /
 * [persistServerFeedPurchase].
 */
interface VendorsRepository {
    /** The paged register for one (search, status) scope, a Room PagingSource filled by a RemoteMediator. */
    fun vendors(search: String, status: String): Flow<PagingData<VendorDto>>

    /** Whole-filter vendor count from the LAST refresh of the scope on screen. */
    val vendorTotal: StateFlow<Int>

    /** Drops one scope's freshness marker so the next pager refetches instead of TTL-skipping. */
    suspend fun invalidateVendors(search: String, status: String)

    /** Room-first vendor detail; null while nothing is cached yet. */
    fun observeVendor(vendorId: String): Flow<VendorDto?>

    /** Network -> Room detail refresh. Non-blocking: a failure leaves the cache serving. */
    suspend fun refreshVendor(vendorId: String)

    /** Room-first catalog (record types, states, statuses, units, frequencies). */
    fun observeCatalog(): Flow<VendorCatalogDto?>

    suspend fun refreshCatalog()

    /** Reconciles a successful VENDOR_CREATE's returned row into Room. Called by the sync engine. */
    suspend fun persistServerVendor(vendor: VendorDto)

    /**
     * The short-lived signed download URL of a vendor's voice note, or null when the proof cannot
     * be served right now. Deliberately NOT cached in Room: the URL expires.
     */
    suspend fun resolveVoiceNoteUrl(proofRef: String): String?

    /** The paged ledger for one (farm, delivery) scope. */
    fun feedPurchases(farm: String, delivery: String): Flow<PagingData<FeedPurchaseDto>>

    val feedPurchaseTotals: StateFlow<FeedPurchaseTotals>

    suspend fun invalidateFeedPurchases(farm: String, delivery: String)

    fun observeFeedPurchase(purchaseId: String): Flow<FeedPurchaseDto?>

    /** Room-first record form vocabulary (farms, feeds, payment words, vendors seen). */
    fun observeFeedPurchaseOptions(): Flow<FeedPurchaseOptionsDto?>

    suspend fun refreshFeedPurchaseOptions()

    /** Reconciles a successful FEED_PURCHASE_CREATE's returned row into Room. Called by the sync engine. */
    suspend fun persistServerFeedPurchase(purchase: FeedPurchaseDto)
}

class DefaultVendorsRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : VendorsRepository {

    private val _vendorTotal = MutableStateFlow(0)
    override val vendorTotal: StateFlow<Int> = _vendorTotal

    private val _feedPurchaseTotals = MutableStateFlow(FeedPurchaseTotals())
    override val feedPurchaseTotals: StateFlow<FeedPurchaseTotals> = _feedPurchaseTotals

    @OptIn(ExperimentalPagingApi::class)
    override fun vendors(search: String, status: String): Flow<PagingData<VendorDto>> {
        val key = vendorScopeKey(search, status)
        return Pager(
            config = pagingConfig(),
            remoteMediator = VendorRemoteMediator(search, status, key, api, database, json, clock) { total ->
                _vendorTotal.value = total
            },
            pagingSourceFactory = { database.vendorItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<VendorDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateVendors(search: String, status: String) {
        database.vendorRemoteKeyDao().delete(vendorScopeKey(search, status))
    }

    override fun observeVendor(vendorId: String): Flow<VendorDto?> =
        database.vendorsBlobCacheDao().observe(VENDOR_KEY_PREFIX + vendorId)
            .map { entity ->
                readCachedJson<VendorDto>(
                    json = json,
                    cacheKey = VENDOR_KEY_PREFIX + vendorId,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.vendorsBlobCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    override suspend fun refreshVendor(vendorId: String) {
        // exception:exempt expected refresh failure (offline/timeout/5xx); the cache keeps serving.
        runCatching { persistServerVendor(api.getProcurementVendor(vendorId)) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "vendor_detail_refresh_failed vendor=$vendorId", it)
            }
    }

    override fun observeCatalog(): Flow<VendorCatalogDto?> = observeBlob(CATALOG_KEY)

    override suspend fun refreshCatalog() {
        // exception:exempt expected refresh failure; the cached vocabulary keeps the form usable.
        runCatching { putBlob(CATALOG_KEY, json.encodeToString(api.getProcurementVendorCatalog())) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "vendor_catalog_refresh_failed", it)
            }
    }

    override suspend fun persistServerVendor(vendor: VendorDto) {
        if (vendor.vendorId.isBlank()) return
        val now = clock()
        val rowJson = json.encodeToString(vendor)
        val itemDao = database.vendorItemDao()
        database.withTransaction {
            database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(VENDOR_KEY_PREFIX + vendor.vendorId, rowJson, now))
            // Every cached list-row copy adopts the server's fresh row, so a re-entered list shows
            // the new vendor's details without a refetch; a NEW vendor has no rows yet and shows
            // up on the next refresh-on-open of the list.
            itemDao.upsertAll(itemDao.rowsForVendor(vendor.vendorId).map { it.copy(dtoJson = rowJson, updatedAt = now) })
            // A new row must reach the list: drop the register's freshness markers so the next
            // open refetches page one rather than TTL-skipping past it.
            database.vendorRemoteKeyDao().deleteAll()
        }
        database.vendorsBlobCacheDao().enforceCacheBounds()
    }

    // offline-first-guard:ignore: signed proof URL is short-lived; Room caches the vendor row, not an expiring download URL
    override suspend fun resolveVoiceNoteUrl(proofRef: String): String? {
        if (proofRef.isBlank()) return null
        // exception:exempt an unreachable proof degrades to "cannot play right now" on the screen.
        return runCatching { api.getProofDownloadUrl(proofRef).takeIf { it.isNotBlank() } }
            .getOrElse {
                if (it is CancellationException) throw it
                null
            }
    }

    @OptIn(ExperimentalPagingApi::class)
    override fun feedPurchases(farm: String, delivery: String): Flow<PagingData<FeedPurchaseDto>> {
        val key = purchaseScopeKey(farm, delivery)
        return Pager(
            config = pagingConfig(),
            remoteMediator = FeedPurchaseRemoteMediator(farm, delivery, key, api, database, json, clock) { totals ->
                _feedPurchaseTotals.value = totals
            },
            pagingSourceFactory = { database.feedPurchaseItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<FeedPurchaseDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateFeedPurchases(farm: String, delivery: String) {
        database.feedPurchaseRemoteKeyDao().delete(purchaseScopeKey(farm, delivery))
    }

    override fun observeFeedPurchase(purchaseId: String): Flow<FeedPurchaseDto?> = observeBlob(PURCHASE_KEY_PREFIX + purchaseId)

    override fun observeFeedPurchaseOptions(): Flow<FeedPurchaseOptionsDto?> = observeBlob(OPTIONS_KEY)

    override suspend fun refreshFeedPurchaseOptions() {
        // exception:exempt expected refresh failure; the cached vocabulary keeps the form usable.
        runCatching { putBlob(OPTIONS_KEY, json.encodeToString(api.getFeedPurchaseOptions())) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "feed_purchase_options_refresh_failed", it)
            }
    }

    override suspend fun persistServerFeedPurchase(purchase: FeedPurchaseDto) {
        if (purchase.feedPurchaseId.isBlank()) return
        val now = clock()
        val rowJson = json.encodeToString(purchase)
        val itemDao = database.feedPurchaseItemDao()
        database.withTransaction {
            database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(PURCHASE_KEY_PREFIX + purchase.feedPurchaseId, rowJson, now))
            itemDao.upsertAll(itemDao.rowsForPurchase(purchase.feedPurchaseId).map { it.copy(dtoJson = rowJson, updatedAt = now) })
            database.feedPurchaseRemoteKeyDao().deleteAll()
        }
        database.vendorsBlobCacheDao().enforceCacheBounds()
    }

    private inline fun <reified T> observeBlob(key: String): Flow<T?> =
        database.vendorsBlobCacheDao().observe(key)
            .map { entity ->
                readCachedJson<T>(
                    json = json,
                    cacheKey = key,
                    dtoJson = entity?.dtoJson,
                    updatedAt = entity?.updatedAt,
                    now = clock(),
                    quarantine = { database.vendorsBlobCacheDao().delete(it) },
                ).data
            }
            .flowOn(Dispatchers.Default)

    private suspend fun putBlob(key: String, dtoJson: String) {
        database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(key, dtoJson, clock()))
        database.vendorsBlobCacheDao().enforceCacheBounds()
    }

    private fun pagingConfig() = PagingConfig(
        pageSize = VENDORS_PAGE_SIZE,
        initialLoadSize = VENDORS_PAGE_SIZE,
        prefetchDistance = 3,
        enablePlaceholders = false,
        maxSize = VENDORS_PAGE_SIZE * 3,
    )

    private fun vendorScopeKey(search: String, status: String): String =
        cacheKey(VENDORS_CACHE_SHAPE, "vendors", search.trim().lowercase(), status, VENDORS_PAGE_SIZE.toString())

    private fun purchaseScopeKey(farm: String, delivery: String): String =
        cacheKey(VENDORS_CACHE_SHAPE, "feed-purchases", farm, delivery, VENDORS_PAGE_SIZE.toString())

    private companion object {
        const val LOG_TAG = "GoatOsVendors"
        const val VENDOR_KEY_PREFIX = "vendor:"
        const val CATALOG_KEY = "catalog"
        const val OPTIONS_KEY = "options"
    }
}

/**
 * Fills Room from `GET /procurement/vendors` page by page. The backend pages by OFFSET (a bounded
 * register of a few hundred rows, never herd-sized), so the "cursor" kept per scope is the next
 * offset. Room stays the single source of truth: this never hands rows to the UI.
 *
 * ALWAYS refresh on open (stale-while-revalidate): the register is edited on the web too.
 */
@OptIn(ExperimentalPagingApi::class)
private class VendorRemoteMediator(
    private val search: String,
    private val status: String,
    private val queryKey: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onTotal: (Int) -> Unit,
) : RemoteMediator<Int, VendorItemEntity>() {
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(loadType: LoadType, state: PagingState<Int, VendorItemEntity>): MediatorResult {
        val offset = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> 0
            LoadType.APPEND -> {
                val remoteKey = database.vendorRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached) return MediatorResult.Success(endOfPaginationReached = true)
                remoteKey.nextCursor.toIntOrNull() ?: return MediatorResult.Success(endOfPaginationReached = true)
            }
        }
        return try {
            val response = api.getProcurementVendors(
                search = search.trim().ifBlank { null },
                status = status.ifBlank { null },
                limit = VENDORS_PAGE_SIZE,
                offset = offset,
            )
            onTotal(response.total)
            val now = clock()
            val nextOffset = offset + response.vendors.size
            val endReached = response.vendors.isEmpty() || nextOffset >= response.total
            database.withTransaction {
                val itemDao = database.vendorItemDao()
                if (loadType == LoadType.REFRESH) itemDao.deleteQuery(queryKey)
                val base = if (loadType == LoadType.REFRESH) 0 else itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.vendors.mapIndexed { index, vendor ->
                        VendorItemEntity(
                            queryKey = queryKey,
                            grainKey = vendor.vendorId,
                            sortIndex = base + index,
                            dtoJson = json.encodeToString(vendor),
                            updatedAt = now,
                        )
                    },
                )
                database.vendorRemoteKeyDao().upsert(
                    VendorRemoteKeyEntity(queryKey, nextOffset.toString(), endReached, now),
                )
                database.vendorRemoteKeyDao().deleteOutsideNewestQueries(VENDORS_CACHED_QUERIES)
                itemDao.deleteRowsOutsideNewestQueries(VENDORS_CACHED_QUERIES)
            }
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // exception:exempt offline/timeout/5xx: the cached rows keep serving; Paging surfaces
            // the error on loadState for the screen to report.
            MediatorResult.Error(e)
        }
    }
}

/** The ledger's twin of [VendorRemoteMediator], over `GET /procurement/feed-purchases`. */
@OptIn(ExperimentalPagingApi::class)
private class FeedPurchaseRemoteMediator(
    private val farm: String,
    private val delivery: String,
    private val queryKey: String,
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json,
    private val clock: () -> Long,
    private val onTotals: (FeedPurchaseTotals) -> Unit,
) : RemoteMediator<Int, FeedPurchaseItemEntity>() {
    override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

    override suspend fun load(loadType: LoadType, state: PagingState<Int, FeedPurchaseItemEntity>): MediatorResult {
        val offset = when (loadType) {
            LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
            LoadType.REFRESH -> 0
            LoadType.APPEND -> {
                val remoteKey = database.feedPurchaseRemoteKeyDao().get(queryKey)
                    ?: return MediatorResult.Success(endOfPaginationReached = true)
                if (remoteKey.endReached) return MediatorResult.Success(endOfPaginationReached = true)
                remoteKey.nextCursor.toIntOrNull() ?: return MediatorResult.Success(endOfPaginationReached = true)
            }
        }
        return try {
            val response = api.getFeedPurchases(
                farm = farm.ifBlank { null },
                delivery = delivery.ifBlank { null },
                limit = VENDORS_PAGE_SIZE,
                offset = offset,
            )
            onTotals(FeedPurchaseTotals(response.total, response.quantityKg, response.spendRupees))
            val now = clock()
            val nextOffset = offset + response.purchases.size
            val endReached = response.purchases.isEmpty() || nextOffset >= response.total
            database.withTransaction {
                val itemDao = database.feedPurchaseItemDao()
                if (loadType == LoadType.REFRESH) itemDao.deleteQuery(queryKey)
                val base = if (loadType == LoadType.REFRESH) 0 else itemDao.countForQuery(queryKey)
                itemDao.upsertAll(
                    response.purchases.mapIndexed { index, purchase ->
                        FeedPurchaseItemEntity(
                            queryKey = queryKey,
                            grainKey = purchase.feedPurchaseId,
                            sortIndex = base + index,
                            dtoJson = json.encodeToString(purchase),
                            updatedAt = now,
                        )
                    },
                )
                // Every row also lands in the detail blob: the backend has no per-purchase read, so
                // the purchase screen opens from the row the ledger page carried.
                response.purchases.forEach { purchase ->
                    database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(PURCHASE_KEY_PREFIX + purchase.feedPurchaseId, json.encodeToString(purchase), now))
                }
                database.feedPurchaseRemoteKeyDao().upsert(
                    FeedPurchaseRemoteKeyEntity(queryKey, nextOffset.toString(), endReached, now),
                )
                database.feedPurchaseRemoteKeyDao().deleteOutsideNewestQueries(VENDORS_CACHED_QUERIES)
                itemDao.deleteRowsOutsideNewestQueries(VENDORS_CACHED_QUERIES)
            }
            MediatorResult.Success(endOfPaginationReached = endReached)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // exception:exempt offline/timeout/5xx: cached rows keep serving; Paging reports it.
            MediatorResult.Error(e)
        }
    }
}
