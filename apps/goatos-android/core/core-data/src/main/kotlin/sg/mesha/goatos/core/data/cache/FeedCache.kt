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
 * Room tables behind the Feed module's two read screens — Feed Direction (the generated sheet) and
 * Feed Packing (the bag worklist) — per docs/decisions/android-offline-first.md. Room is the UI's
 * single source of truth: the screen renders from here, the network refresh upserts here.
 *
 * Each screen is split into the SAME three tables as the Counts breakdown, for the same reason
 * (docs/decisions/mobile-data-fetch-anti-patterns.md: "pagination binds BOTH layers"):
 *
 *  - `*_meta_cache` — the FIXED-SIZE whole-scope SUMMARY envelope (`total_kg_by_feed_item`, the
 *    blocked counts). Rolled up by the backend over the FULL filtered set, so it is cached as
 *    served and NEVER re-derived by summing the ~20 rows currently paged into memory.
 *  - `*_items` — the paged ROWS, ONE ROOM ROW PER GRAIN, read back through a [PagingSource] so the
 *    observed query is a bounded ~20-row window, exactly like the network page. A growing
 *    page-response blob is the banned shape and is deliberately not used.
 *  - `*_remote_keys` — the next page offset per filter scope, so `RemoteMediator` can resume paging
 *    after process death without re-walking from zero.
 */

// ---------------------------------------------------------------------------
// Feed Direction — fixed-size summary envelope, JSON blob by scope
// ---------------------------------------------------------------------------

@Entity(tableName = "feed_direction_meta_cache")
data class FeedDirectionMetaCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface FeedDirectionMetaCacheDao : JsonBlobCacheDao<FeedDirectionMetaCacheEntity> {
    @Query("SELECT * FROM feed_direction_meta_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<FeedDirectionMetaCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: FeedDirectionMetaCacheEntity)

    @Query("DELETE FROM feed_direction_meta_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM feed_direction_meta_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM feed_direction_meta_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM feed_direction_meta_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM feed_direction_meta_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

// ---------------------------------------------------------------------------
// Feed Direction — per-grain rows, read as a bounded PagingSource window
// ---------------------------------------------------------------------------

@Entity(
    tableName = "feed_direction_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class FeedDirectionItemEntity(
    val queryKey: String,
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "feed_direction_remote_keys")
data class FeedDirectionRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextOffset: Int,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface FeedDirectionItemDao {
    @Query(
        "SELECT * FROM feed_direction_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, FeedDirectionItemEntity>

    /**
     * The MOST RECENTLY cached row for a shed-session, across ANY filter scope this app instance
     * has paged. A shed-session's lifecycle bucket is shared by every ration-grain row of that
     * session (see [sg.mesha.goatos.core.network.dto.FeedDirectionRowDto.lifecycleStatus]'s kdoc),
     * so any one matching row is authoritative — there is no need to reconstruct the exact
     * `queryKey` the list screen happened to be filtered by when it cached the row. `grainKey` is
     * `shedId|partitionLabel|workflow|rationGroup|experimentArm|shedTag|sessionNo`; uses prefix
     * LIKE within a suffix constraint so the grainKey index is usable for the prefix scan.
     */
    @Query(
        "SELECT * FROM feed_direction_items WHERE grainKey LIKE " +
            ":shedId || '|' || :partitionLabel || '|' || :workflow || '|%' AND grainKey LIKE '%|' || :sessionNo " +
            "ORDER BY updatedAt DESC LIMIT 1",
    )
    fun observeRowForShedSession(
        shedId: String,
        partitionLabel: String,
        workflow: String,
        sessionNo: String,
    ): Flow<FeedDirectionItemEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<FeedDirectionItemEntity>)

    @Query("DELETE FROM feed_direction_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex.
     *  The backend pages by SHED (many rows per shed), so sortIndex must count ROWS, not shed offset. */
    @Query("SELECT COUNT(*) FROM feed_direction_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM feed_direction_items WHERE queryKey IN " +
            "(SELECT queryKey FROM feed_direction_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface FeedDirectionRemoteKeyDao {
    @Query("SELECT * FROM feed_direction_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): FeedDirectionRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: FeedDirectionRemoteKeyEntity)

    @Query("DELETE FROM feed_direction_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM feed_direction_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM feed_direction_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

// ---------------------------------------------------------------------------
// Feed Packing — fixed-size summary envelope, JSON blob by scope
// ---------------------------------------------------------------------------

@Entity(tableName = "feed_packing_meta_cache")
data class FeedPackingMetaCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface FeedPackingMetaCacheDao : JsonBlobCacheDao<FeedPackingMetaCacheEntity> {
    @Query("SELECT * FROM feed_packing_meta_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<FeedPackingMetaCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: FeedPackingMetaCacheEntity)

    @Query("DELETE FROM feed_packing_meta_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM feed_packing_meta_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM feed_packing_meta_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM feed_packing_meta_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM feed_packing_meta_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

// ---------------------------------------------------------------------------
// Feed Packing — per-line rows, read as a bounded PagingSource window
// ---------------------------------------------------------------------------

@Entity(
    tableName = "feed_packing_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class FeedPackingItemEntity(
    val queryKey: String,
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "feed_packing_remote_keys")
data class FeedPackingRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextOffset: Int,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface FeedPackingItemDao {
    @Query(
        "SELECT * FROM feed_packing_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, FeedPackingItemEntity>

    /**
     * The MOST RECENTLY cached row for one PEN-SESSION, across ANY filter scope this app instance
     * has paged (not just the exact `queryKey` the worklist happened to be filtered by). `grainKey`
     * is `shedId|partitionLabel|workflow|sessionNo` — see
     * [sg.mesha.goatos.core.network.dto.FeedPackingRowDto.grainKey]. Used to observe a session's
     * live `lifecycleStatus` from the same Room table the worklist renders from, so a completion
     * screen left open across a status change (verified/rejected elsewhere) sees it without a
     * screen re-entry.
     */
    @Query(
        "SELECT * FROM feed_packing_items WHERE grainKey = " +
            ":shedId || '|' || :partitionLabel || '|' || :workflow || '|' || :sessionNo " +
            "ORDER BY updatedAt DESC LIMIT 1",
    )
    fun observeRowForPenSession(
        shedId: String,
        partitionLabel: String,
        workflow: String,
        sessionNo: String,
    ): Flow<FeedPackingItemEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<FeedPackingItemEntity>)

    @Query("DELETE FROM feed_packing_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex.
     *  The backend pages by SHED (many rows per shed), so sortIndex must count ROWS, not shed offset. */
    @Query("SELECT COUNT(*) FROM feed_packing_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM feed_packing_items WHERE queryKey IN " +
            "(SELECT queryKey FROM feed_packing_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface FeedPackingRemoteKeyDao {
    @Query("SELECT * FROM feed_packing_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): FeedPackingRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: FeedPackingRemoteKeyEntity)

    @Query("DELETE FROM feed_packing_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM feed_packing_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM feed_packing_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
