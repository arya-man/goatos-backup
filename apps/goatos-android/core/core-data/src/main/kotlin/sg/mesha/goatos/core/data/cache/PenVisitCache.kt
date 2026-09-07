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
 * Room tables behind the pen-visit "For me" tab (maintainer decision 2026-09-07). Offline-first
 * per docs/decisions/android-offline-first.md: Room is the UI's single source of truth for every
 * READ, the network refresh upserts here, and pagination binds BOTH layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md). The submit rides the durable outbox and
 * the sync engine reconciles the server's returned task back into these tables.
 *
 * Same shapes as the Leadership Tasks trio in [LeadershipTaskItemDao] et al:
 *  - `pen_visit_items` — the paged list ROWS, one Room row per visit per filter scope, read back
 *    through a [PagingSource] so the observed query is a bounded ~20-row window.
 *  - `pen_visit_remote_keys` — the next page cursor per filter scope.
 *  - `pen_visit_detail_cache` — the JSON-blob-by-taskId detail cache.
 */

@Entity(
    tableName = "pen_visit_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class PenVisitItemEntity(
    val queryKey: String,
    /** The visit TASK id — the row grain the list pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "pen_visit_remote_keys")
data class PenVisitRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface PenVisitItemDao {
    @Query(
        "SELECT * FROM pen_visit_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, PenVisitItemEntity>

    /** Every cached copy of one visit's row, across scopes — the reconcile target after a write. */
    @Query("SELECT * FROM pen_visit_items WHERE grainKey = :taskId")
    suspend fun rowsForTask(taskId: String): List<PenVisitItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<PenVisitItemEntity>)

    @Query("DELETE FROM pen_visit_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex. */
    @Query("SELECT COUNT(*) FROM pen_visit_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM pen_visit_items WHERE queryKey IN " +
            "(SELECT queryKey FROM pen_visit_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface PenVisitRemoteKeyDao {
    @Query("SELECT * FROM pen_visit_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): PenVisitRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: PenVisitRemoteKeyEntity)

    @Query("DELETE FROM pen_visit_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    /** Drops EVERY scope's freshness marker — after a submit, every filter's list is stale. */
    @Query("DELETE FROM pen_visit_remote_keys")
    suspend fun deleteAll()

    @Query(
        "DELETE FROM pen_visit_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM pen_visit_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

@Entity(tableName = "pen_visit_detail_cache")
data class PenVisitDetailCacheEntity(
    /** The visit task id. */
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface PenVisitDetailCacheDao : JsonBlobCacheDao<PenVisitDetailCacheEntity> {
    @Query("SELECT * FROM pen_visit_detail_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<PenVisitDetailCacheEntity?>

    @Query("SELECT * FROM pen_visit_detail_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): PenVisitDetailCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: PenVisitDetailCacheEntity)

    @Query("DELETE FROM pen_visit_detail_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM pen_visit_detail_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM pen_visit_detail_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM pen_visit_detail_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM pen_visit_detail_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
