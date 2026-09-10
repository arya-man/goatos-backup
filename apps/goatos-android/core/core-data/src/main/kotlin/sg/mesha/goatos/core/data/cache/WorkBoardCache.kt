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
 * Room tables behind the Work Board's My Work screen (maintainer decision 2026-09-10). Offline-first
 * per docs/decisions/android-offline-first.md: Room is the UI's single source of truth, the network
 * refresh upserts here, and pagination binds BOTH layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * The Feed three-table shape ([FeedPackingMetaCacheDao] et al), because the board has the same
 * two reads — a fixed-size WHOLE-FILTER summary and a paged row set:
 *  - `work_board_meta_cache` — the `/work-board/summary` envelope as served, one JSON blob per
 *    filter scope. NEVER re-derived by summing the ~20 rows currently in memory.
 *  - `work_board_items` — the paged ROWS, ONE ROOM ROW PER BOARD ROW (`row_key`), read back through
 *    a [PagingSource] so the observed query is a bounded ~20-row window.
 *  - `work_board_remote_keys` — the next page's STRING keyset cursor per scope (the backend pages by
 *    `module|source_type|source_id`, never by offset).
 */

@Entity(tableName = "work_board_meta_cache")
data class WorkBoardMetaCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface WorkBoardMetaCacheDao : JsonBlobCacheDao<WorkBoardMetaCacheEntity> {
    @Query("SELECT * FROM work_board_meta_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<WorkBoardMetaCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: WorkBoardMetaCacheEntity)

    @Query("DELETE FROM work_board_meta_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM work_board_meta_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM work_board_meta_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM work_board_meta_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM work_board_meta_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

@Entity(
    tableName = "work_board_items",
    primaryKeys = ["queryKey", "rowKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["rowKey"]),
    ],
)
data class WorkBoardItemEntity(
    val queryKey: String,
    /** The backend `row_key` (`module|source_type|source_id`) — the row grain the board pages by. */
    val rowKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "work_board_remote_keys")
data class WorkBoardRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    /** The backend's keyset cursor for the next page; null once the last page landed. */
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface WorkBoardItemDao {
    @Query(
        "SELECT * FROM work_board_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, rowKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, WorkBoardItemEntity>

    /**
     * The MOST RECENTLY cached copy of one board row, across ANY filter scope this app instance
     * has paged — the L1 detail's read, so opening a row never issues a second network call.
     */
    @Query("SELECT * FROM work_board_items WHERE rowKey = :rowKey ORDER BY updatedAt DESC LIMIT 1")
    fun observeRow(rowKey: String): Flow<WorkBoardItemEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<WorkBoardItemEntity>)

    @Query("DELETE FROM work_board_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex. */
    @Query("SELECT COUNT(*) FROM work_board_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM work_board_items WHERE queryKey IN " +
            "(SELECT queryKey FROM work_board_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface WorkBoardRemoteKeyDao {
    @Query("SELECT * FROM work_board_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): WorkBoardRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WorkBoardRemoteKeyEntity)

    @Query("DELETE FROM work_board_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM work_board_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM work_board_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
