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
 * Room tables behind the Toxin module (aflatoxin strip test, maintainer decision 2026-08-25) —
 * one server-created 7-step test task per purchased feed load. Offline-first per
 * docs/decisions/android-offline-first.md: Room is the UI's single source of truth, the network
 * refresh upserts here, and pagination binds BOTH layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * Same shapes as the PC Care pair in [PcCareTaskItemDao] et al:
 *  - `toxin_task_items` — the paged task-list ROWS, one Room row per test round, read back through
 *    a [PagingSource] so the observed query is a bounded ~20-row window.
 *  - `toxin_task_remote_keys` — the next page cursor per status-filter scope.
 *  - `toxin_task_detail_cache` — the JSON-blob-by-taskId detail cache (a re-opened task renders
 *    its cached steps instantly; step STATES stay server-owned and refresh behind it).
 */

@Entity(
    tableName = "toxin_task_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class ToxinTaskItemEntity(
    val queryKey: String,
    /** The TASK id — the row grain the list pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "toxin_task_remote_keys")
data class ToxinTaskRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface ToxinTaskItemDao {
    @Query(
        "SELECT * FROM toxin_task_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, ToxinTaskItemEntity>

    /** Every cached copy of one task's row, across scopes — the reconcile target after a write. */
    @Query("SELECT * FROM toxin_task_items WHERE grainKey = :taskId")
    suspend fun rowsForTask(taskId: String): List<ToxinTaskItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<ToxinTaskItemEntity>)

    @Query("DELETE FROM toxin_task_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex. */
    @Query("SELECT COUNT(*) FROM toxin_task_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM toxin_task_items WHERE queryKey IN " +
            "(SELECT queryKey FROM toxin_task_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface ToxinTaskRemoteKeyDao {
    @Query("SELECT * FROM toxin_task_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): ToxinTaskRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: ToxinTaskRemoteKeyEntity)

    @Query("DELETE FROM toxin_task_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM toxin_task_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM toxin_task_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

@Entity(tableName = "toxin_task_detail_cache")
data class ToxinTaskDetailCacheEntity(
    /** The task id. */
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ToxinTaskDetailCacheDao : JsonBlobCacheDao<ToxinTaskDetailCacheEntity> {
    @Query("SELECT * FROM toxin_task_detail_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ToxinTaskDetailCacheEntity?>

    @Query("SELECT * FROM toxin_task_detail_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): ToxinTaskDetailCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ToxinTaskDetailCacheEntity)

    @Query("DELETE FROM toxin_task_detail_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM toxin_task_detail_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM toxin_task_detail_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM toxin_task_detail_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM toxin_task_detail_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
