package sg.mesha.goatos.core.data.cache

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Room tables behind the Vendors module (maintainer decision 2026-09-03): the procurement vendor
 * register and the feed purchase ledger, view and add, on the phone. Offline-first per
 * docs/decisions/android-offline-first.md: Room is the UI's single source of truth, the network
 * refresh upserts here, and pagination binds BOTH layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * Same shapes as the Toxin trio in [ToxinTaskItemDao] et al:
 *  - `vendor_items` / `vendor_remote_keys` — the paged register rows per (search, status) scope
 *    plus the next-page cursor for that scope.
 *  - `feed_purchase_items` / `feed_purchase_remote_keys` — the same pair for the ledger, per
 *    (farm, delivery) scope.
 *  - `vendors_blob_cache` — JSON blobs by key: one vendor (`vendor:<id>`), one purchase
 *    (`purchase:<id>`), the vendor catalog (`catalog`) and the purchase form options (`options`).
 */

@Entity(
    tableName = "vendor_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class VendorItemEntity(
    val queryKey: String,
    /** The VENDOR id — the row grain the list pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "vendor_remote_keys")
data class VendorRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface VendorItemDao {
    @Query(
        "SELECT * FROM vendor_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, VendorItemEntity>

    /** Every cached copy of one vendor's row, across scopes — the reconcile target after a write. */
    @Query("SELECT * FROM vendor_items WHERE grainKey = :vendorId")
    suspend fun rowsForVendor(vendorId: String): List<VendorItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<VendorItemEntity>)

    @Query("DELETE FROM vendor_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    @Query("SELECT COUNT(*) FROM vendor_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM vendor_items WHERE queryKey IN " +
            "(SELECT queryKey FROM vendor_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface VendorRemoteKeyDao {
    @Query("SELECT * FROM vendor_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): VendorRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: VendorRemoteKeyEntity)

    @Query("DELETE FROM vendor_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query("DELETE FROM vendor_remote_keys")
    suspend fun deleteAll()

    @Query(
        "DELETE FROM vendor_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM vendor_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

@Entity(
    tableName = "feed_purchase_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class FeedPurchaseItemEntity(
    val queryKey: String,
    /** The PURCHASE id — the row grain the ledger pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "feed_purchase_remote_keys")
data class FeedPurchaseRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface FeedPurchaseItemDao {
    @Query(
        "SELECT * FROM feed_purchase_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, FeedPurchaseItemEntity>

    @Query("SELECT * FROM feed_purchase_items WHERE grainKey = :purchaseId")
    suspend fun rowsForPurchase(purchaseId: String): List<FeedPurchaseItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<FeedPurchaseItemEntity>)

    @Query("DELETE FROM feed_purchase_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    @Query("SELECT COUNT(*) FROM feed_purchase_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM feed_purchase_items WHERE queryKey IN " +
            "(SELECT queryKey FROM feed_purchase_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface FeedPurchaseRemoteKeyDao {
    @Query("SELECT * FROM feed_purchase_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): FeedPurchaseRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: FeedPurchaseRemoteKeyEntity)

    @Query("DELETE FROM feed_purchase_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query("DELETE FROM feed_purchase_remote_keys")
    suspend fun deleteAll()

    @Query(
        "DELETE FROM feed_purchase_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM feed_purchase_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

@Entity(tableName = "vendors_blob_cache")
data class VendorsBlobCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface VendorsBlobCacheDao : JsonBlobCacheDao<VendorsBlobCacheEntity> {
    @Query("SELECT * FROM vendors_blob_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<VendorsBlobCacheEntity?>

    @Query("SELECT * FROM vendors_blob_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): VendorsBlobCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: VendorsBlobCacheEntity)

    @Query("DELETE FROM vendors_blob_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM vendors_blob_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM vendors_blob_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM vendors_blob_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM vendors_blob_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
