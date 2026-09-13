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
 * Room tables behind the Animal purchases tab (maintainer decision 2026-09-13,
 * docs/decisions/animal-purchases.md). Offline-first per docs/decisions/android-offline-first.md:
 * Room is the UI's single source of truth, the network refresh upserts here, and pagination binds
 * BOTH layers (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * The Toxin trio shape, twice over, plus one blob cache:
 *  - `animal_purchase_load_items` / `animal_purchase_load_remote_keys` — the paged LOAD list rows
 *    (one Room row per load) and the next keyset cursor per scope.
 *  - `animal_purchase_animal_items` / `animal_purchase_animal_remote_keys` — the paged ANIMAL rows
 *    of one load (queryKey = the load id) and that load's next cursor.
 *  - `animal_purchase_blob_cache` — JSON blobs by key: the form options (`options`), one load's
 *    header (`load:<id>`), and the list's caller flag (`loads:meta`).
 *
 * The phone's stored copy is a placeholder the next server read overwrites; the CEO's decision
 * lives on the server and reaches these rows only through a refresh or a write reconcile.
 */

@Entity(
    tableName = "animal_purchase_load_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class AnimalPurchaseLoadItemEntity(
    val queryKey: String,
    /** The LOAD id — the row grain the list pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "animal_purchase_load_remote_keys")
data class AnimalPurchaseLoadRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface AnimalPurchaseLoadItemDao {
    @Query(
        "SELECT * FROM animal_purchase_load_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, AnimalPurchaseLoadItemEntity>

    /** Every cached copy of one load's row, across scopes — the reconcile target after a write. */
    @Query("SELECT * FROM animal_purchase_load_items WHERE grainKey = :loadId")
    suspend fun rowsForLoad(loadId: String): List<AnimalPurchaseLoadItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<AnimalPurchaseLoadItemEntity>)

    @Query("DELETE FROM animal_purchase_load_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    @Query("SELECT COUNT(*) FROM animal_purchase_load_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int
}

@Dao
interface AnimalPurchaseLoadRemoteKeyDao {
    @Query("SELECT * FROM animal_purchase_load_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): AnimalPurchaseLoadRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: AnimalPurchaseLoadRemoteKeyEntity)

    @Query("DELETE FROM animal_purchase_load_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)
}

@Entity(
    tableName = "animal_purchase_animal_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class AnimalPurchaseAnimalItemEntity(
    /** The LOAD id the animal belongs to — one cached window per load. */
    val queryKey: String,
    /** The CANDIDATE id — the row grain. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "animal_purchase_animal_remote_keys")
data class AnimalPurchaseAnimalRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface AnimalPurchaseAnimalItemDao {
    @Query(
        "SELECT * FROM animal_purchase_animal_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, AnimalPurchaseAnimalItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<AnimalPurchaseAnimalItemEntity>)

    @Query("DELETE FROM animal_purchase_animal_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    @Query("SELECT COUNT(*) FROM animal_purchase_animal_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    /** The highest sortIndex cached for one load — a freshly recorded animal appends after it. */
    @Query("SELECT COALESCE(MAX(sortIndex), -1) FROM animal_purchase_animal_items WHERE queryKey = :queryKey")
    suspend fun maxSortIndex(queryKey: String): Int

    @Query("SELECT * FROM animal_purchase_animal_items WHERE queryKey = :queryKey AND grainKey = :candidateId")
    suspend fun get(queryKey: String, candidateId: String): AnimalPurchaseAnimalItemEntity?

    /** Drops every load's animal window except the [keepLoads] most recently touched. */
    @Query(
        "DELETE FROM animal_purchase_animal_items WHERE queryKey IN " +
            "(SELECT queryKey FROM animal_purchase_animal_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepLoads)",
    )
    suspend fun deleteRowsOutsideNewestLoads(keepLoads: Int)
}

@Dao
interface AnimalPurchaseAnimalRemoteKeyDao {
    @Query("SELECT * FROM animal_purchase_animal_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): AnimalPurchaseAnimalRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: AnimalPurchaseAnimalRemoteKeyEntity)

    @Query("DELETE FROM animal_purchase_animal_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM animal_purchase_animal_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM animal_purchase_animal_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepLoads)",
    )
    suspend fun deleteOutsideNewestLoads(keepLoads: Int)
}

@Entity(tableName = "animal_purchase_blob_cache")
data class AnimalPurchaseBlobCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface AnimalPurchaseBlobCacheDao : JsonBlobCacheDao<AnimalPurchaseBlobCacheEntity> {
    @Query("SELECT * FROM animal_purchase_blob_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<AnimalPurchaseBlobCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: AnimalPurchaseBlobCacheEntity)

    @Query("DELETE FROM animal_purchase_blob_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM animal_purchase_blob_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM animal_purchase_blob_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM animal_purchase_blob_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM animal_purchase_blob_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
