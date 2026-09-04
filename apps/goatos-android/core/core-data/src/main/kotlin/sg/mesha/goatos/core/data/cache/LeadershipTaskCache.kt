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
 * Room tables behind the Leadership Tasks module (maintainer request 2026-09-04). Offline-first
 * per docs/decisions/android-offline-first.md: Room is the UI's single source of truth for every
 * READ, the network refresh upserts here, and pagination binds BOTH layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md). Writes (raise/edit/status/seen) are
 * online calls whose returned detail is reconciled back into these tables.
 *
 * Same shapes as the Toxin trio in [ToxinTaskItemDao] et al:
 *  - `leadership_task_items` — the paged list ROWS, one Room row per task per filter scope, read
 *    back through a [PagingSource] so the observed query is a bounded ~20-row window.
 *  - `leadership_task_remote_keys` — the next page cursor per filter scope.
 *  - `leadership_task_detail_cache` — the JSON-blob-by-taskId detail cache.
 */

@Entity(
    tableName = "leadership_task_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class LeadershipTaskItemEntity(
    val queryKey: String,
    /** The TASK id — the row grain the list pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "leadership_task_remote_keys")
data class LeadershipTaskRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface LeadershipTaskItemDao {
    @Query(
        "SELECT * FROM leadership_task_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, LeadershipTaskItemEntity>

    /** Every cached copy of one task's row, across scopes — the reconcile target after a write. */
    @Query("SELECT * FROM leadership_task_items WHERE grainKey = :taskId")
    suspend fun rowsForTask(taskId: String): List<LeadershipTaskItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<LeadershipTaskItemEntity>)

    @Query("DELETE FROM leadership_task_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex. */
    @Query("SELECT COUNT(*) FROM leadership_task_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM leadership_task_items WHERE queryKey IN " +
            "(SELECT queryKey FROM leadership_task_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface LeadershipTaskRemoteKeyDao {
    @Query("SELECT * FROM leadership_task_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): LeadershipTaskRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: LeadershipTaskRemoteKeyEntity)

    @Query("DELETE FROM leadership_task_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    /** Drops EVERY scope's freshness marker — after a write, every filter's list is stale. */
    @Query("DELETE FROM leadership_task_remote_keys")
    suspend fun deleteAll()

    @Query(
        "DELETE FROM leadership_task_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM leadership_task_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

@Entity(tableName = "leadership_task_detail_cache")
data class LeadershipTaskDetailCacheEntity(
    /** The task id. */
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface LeadershipTaskDetailCacheDao : JsonBlobCacheDao<LeadershipTaskDetailCacheEntity> {
    @Query("SELECT * FROM leadership_task_detail_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<LeadershipTaskDetailCacheEntity?>

    @Query("SELECT * FROM leadership_task_detail_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): LeadershipTaskDetailCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: LeadershipTaskDetailCacheEntity)

    @Query("DELETE FROM leadership_task_detail_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM leadership_task_detail_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM leadership_task_detail_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM leadership_task_detail_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM leadership_task_detail_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
