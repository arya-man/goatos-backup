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
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.cache.SalesDealItemEntity
import sg.mesha.goatos.core.data.cache.SalesDealRemoteKeyEntity
import sg.mesha.goatos.core.data.cache.VendorsBlobCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.data.cache.enforceCacheBounds
import sg.mesha.goatos.core.data.cache.readCachedJson
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidatePageDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SalePreviewDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.core.network.serverErrorText

/** Whole-filter totals of the deals ledger, refreshed with every page-one fetch. */
data class SalesDealTotals(val total: Int = 0)

/**
 * Sales on the phone (maintainer instruction 2026-09-04): the Procurement module's Sales tab.
 * Room is the single source of truth for the deals ledger and the vocabularies; the network
 * refresh runs behind it (stale-while-revalidate). Tagging animals to a sale is an ONLINE
 * conversation with the server (candidates, preview, confirm) because every step is judged
 * against the live herd -- a cached "sellable" would be a lie by the time it was confirmed.
 */
interface SalesRepository {
    fun deals(farm: String): Flow<PagingData<SalesDealDto>>
    val dealTotals: StateFlow<SalesDealTotals>
    suspend fun invalidateDeals(farm: String)

    fun observeDeal(dealId: String): Flow<SalesDealDto?>
    fun observeOptions(): Flow<SalesOptionsDto?>
    suspend fun refreshOptions()
    fun observeVendorOptions(): Flow<VendorOptionsDto?>
    suspend fun refreshVendorOptions()

    /** The server's returned row after a queued create landed. */
    suspend fun persistServerDeal(deal: SalesDealDto)

    // --- tagging animals (online) ---
    suspend fun saleLocations(): AppResult<SaleLocationsDto>
    suspend fun saleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, cursor: String?): AppResult<SaleCandidatePageDto>
    suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto>
    suspend fun previewAllocation(request: SaleAllocationRequestDto): AppResult<SalePreviewDto>
    suspend fun confirmAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): AppResult<SaleAllocationDto>
}

class DefaultSalesRepository(
    private val api: AppApi,
    private val database: GoatDatabase,
    private val json: Json = Json { ignoreUnknownKeys = true },
    private val clock: () -> Long = { System.currentTimeMillis() },
) : SalesRepository {

    private val _dealTotals = MutableStateFlow(SalesDealTotals())
    override val dealTotals: StateFlow<SalesDealTotals> = _dealTotals

    @OptIn(ExperimentalPagingApi::class)
    override fun deals(farm: String): Flow<PagingData<SalesDealDto>> {
        val key = dealScopeKey(farm)
        return Pager(
            config = PagingConfig(
                pageSize = VENDORS_PAGE_SIZE,
                initialLoadSize = VENDORS_PAGE_SIZE,
                prefetchDistance = 3,
                enablePlaceholders = false,
                maxSize = VENDORS_PAGE_SIZE * 3,
            ),
            remoteMediator = SalesDealRemoteMediator(farm, key, api, database, json, clock) { totals -> _dealTotals.value = totals },
            pagingSourceFactory = { database.salesDealItemDao().pagingSource(key) },
        ).flow
            .map { page -> page.map { entity -> json.decodeFromString<SalesDealDto>(entity.dtoJson) } }
            .flowOn(Dispatchers.Default)
    }

    override suspend fun invalidateDeals(farm: String) {
        database.salesDealRemoteKeyDao().delete(dealScopeKey(farm))
    }

    override fun observeDeal(dealId: String): Flow<SalesDealDto?> = observeBlob(DEAL_KEY_PREFIX + dealId)

    override fun observeOptions(): Flow<SalesOptionsDto?> = observeBlob(OPTIONS_KEY)

    override suspend fun refreshOptions() {
        // exception:exempt expected refresh failure; the cached vocabulary keeps the form usable.
        runCatching { putBlob(OPTIONS_KEY, json.encodeToString(api.getSalesOptions())) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "sales_options_refresh_failed", it)
            }
    }

    override fun observeVendorOptions(): Flow<VendorOptionsDto?> = observeBlob(VENDOR_OPTIONS_KEY)

    override suspend fun refreshVendorOptions() {
        // exception:exempt expected refresh failure; the cached picklist keeps the form usable.
        runCatching { putBlob(VENDOR_OPTIONS_KEY, json.encodeToString(api.getVendorOptions())) }
            .onFailure {
                if (it is CancellationException) throw it
                android.util.Log.w(LOG_TAG, "sales_vendor_options_refresh_failed", it)
            }
    }

    override suspend fun persistServerDeal(deal: SalesDealDto) {
        if (deal.dealId.isBlank()) return
        val now = clock()
        val rowJson = json.encodeToString(deal)
        val itemDao = database.salesDealItemDao()
        database.withTransaction {
            database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(DEAL_KEY_PREFIX + deal.dealId, rowJson, now))
            itemDao.upsertAll(itemDao.rowsForDeal(deal.dealId).map { it.copy(dtoJson = rowJson, updatedAt = now) })
            // A new deal must reach the ledger: drop every scope's freshness marker so the next
            // open refetches page one rather than TTL-skipping past it.
            database.salesDealRemoteKeyDao().deleteAll()
        }
        database.vendorsBlobCacheDao().enforceCacheBounds()
    }

    // offline-first-guard:ignore: tagging animals is judged against the LIVE herd on the server; a cached candidate list would confirm a stale "sellable"
    override suspend fun saleLocations(): AppResult<SaleLocationsDto> = call { api.getSaleLocations() }

    // offline-first-guard:ignore: same as saleLocations -- a live-herd read the server owns
    override suspend fun saleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, cursor: String?): AppResult<SaleCandidatePageDto> =
        call { api.getSaleCandidates(parkId, shedId?.ifBlank { null }, partitionLabels, query?.ifBlank { null }, VENDORS_PAGE_SIZE, cursor?.ifBlank { null }) }

    // offline-first-guard:ignore: what is tagged to a sale is read live beside the confirm it feeds
    override suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto> = call { api.getSaleAllocation(dealId) }

    override suspend fun previewAllocation(request: SaleAllocationRequestDto): AppResult<SalePreviewDto> = call { api.previewSaleAllocation(request) } // offline-first-guard:ignore: sale allocation preview is a live-herd server decision; a Room counterpart could approve stale sellable state.

    override suspend fun confirmAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): AppResult<SaleAllocationDto> = // offline-first-guard:ignore: sale allocation confirm is the server-owned herd mutation; cached confirmation would risk marking stale animals sold.
        call { api.confirmSaleAllocation(idempotencyKey, request) }

    private suspend fun <T> call(block: suspend () -> T): AppResult<T> = try {
        AppResult.Ok(block())
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        // exception:exempt every failure is returned to the screen as the server's own farm copy
        // when it sent one, else a generic retry line; nothing is swallowed.
        AppResult.Err(e.serverErrorText()?.message?.takeIf { it.isNotBlank() } ?: (e.message ?: "Could not reach the server"), e)
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

    private fun dealScopeKey(farm: String): String = cacheKey(SALES_CACHE_SHAPE, "sales-deals", farm, VENDORS_PAGE_SIZE.toString())

    private companion object {
        const val LOG_TAG = "GoatOsSales"
        const val SALES_CACHE_SHAPE = "sales-v1"
        const val SALES_CACHED_QUERIES = 4
        const val DEAL_KEY_PREFIX = "sale:"
        const val OPTIONS_KEY = "sales-options"
        const val VENDOR_OPTIONS_KEY = "sales-vendor-options"
    }

    /** Fills Room from `GET /sales/deals` page by page; the per-scope "cursor" is the next offset. */
    @OptIn(ExperimentalPagingApi::class)
    private class SalesDealRemoteMediator(
        private val farm: String,
        private val queryKey: String,
        private val api: AppApi,
        private val database: GoatDatabase,
        private val json: Json,
        private val clock: () -> Long,
        private val onTotals: (SalesDealTotals) -> Unit,
    ) : RemoteMediator<Int, SalesDealItemEntity>() {
        override suspend fun initialize(): InitializeAction = InitializeAction.LAUNCH_INITIAL_REFRESH

        override suspend fun load(loadType: LoadType, state: PagingState<Int, SalesDealItemEntity>): MediatorResult {
            val offset = when (loadType) {
                LoadType.PREPEND -> return MediatorResult.Success(endOfPaginationReached = true)
                LoadType.REFRESH -> 0
                LoadType.APPEND -> {
                    val remoteKey = database.salesDealRemoteKeyDao().get(queryKey)
                        ?: return MediatorResult.Success(endOfPaginationReached = true)
                    if (remoteKey.endReached) return MediatorResult.Success(endOfPaginationReached = true)
                    remoteKey.nextCursor.toIntOrNull() ?: return MediatorResult.Success(endOfPaginationReached = true)
                }
            }
            return try {
                val response = api.getSalesDeals(farm = farm.ifBlank { null }, limit = VENDORS_PAGE_SIZE, offset = offset)
                onTotals(SalesDealTotals(response.total))
                val now = clock()
                val nextOffset = offset + response.deals.size
                val endReached = response.deals.isEmpty() || nextOffset >= response.total
                database.withTransaction {
                    val itemDao = database.salesDealItemDao()
                    if (loadType == LoadType.REFRESH) itemDao.deleteQuery(queryKey)
                    val base = if (loadType == LoadType.REFRESH) 0 else itemDao.countForQuery(queryKey)
                    itemDao.upsertAll(
                        response.deals.mapIndexed { index, deal ->
                            SalesDealItemEntity(queryKey = queryKey, grainKey = deal.dealId, sortIndex = base + index, dtoJson = json.encodeToString(deal), updatedAt = now)
                        },
                    )
                    // Every row also lands in the detail blob so the deal screen opens from cache.
                    response.deals.forEach { deal ->
                        database.vendorsBlobCacheDao().upsert(VendorsBlobCacheEntity(DEAL_KEY_PREFIX + deal.dealId, json.encodeToString(deal), now))
                    }
                    database.salesDealRemoteKeyDao().upsert(SalesDealRemoteKeyEntity(queryKey, nextOffset.toString(), endReached, now))
                    database.salesDealRemoteKeyDao().deleteOutsideNewestQueries(SALES_CACHED_QUERIES)
                    itemDao.deleteRowsOutsideNewestQueries(SALES_CACHED_QUERIES)
                }
                database.vendorsBlobCacheDao().enforceCacheBounds()
                MediatorResult.Success(endOfPaginationReached = endReached)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // exception:exempt offline/timeout/5xx: cached rows keep serving; Paging reports it.
                MediatorResult.Error(e)
            }
        }
    }
}
