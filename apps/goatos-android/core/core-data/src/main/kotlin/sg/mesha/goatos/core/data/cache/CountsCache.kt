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
 * Room tables behind the Counts read screen (docs/decisions/android-offline-first.md). Room is the
 * UI's single source of truth: the screen renders from here, the network refresh upserts here.
 *
 * The breakdown is split across THREE tables on purpose, because it carries two different kinds of
 * data with two different bounding rules:
 *
 *  - [CountsBreakdownItemEntity] — the paged ROWS, stored ONE ROOM ROW PER GRAIN. Read back through
 *    a [PagingSource] so the observed query is a bounded ~20-row window, exactly like the network
 *    page (docs/decisions/mobile-data-fetch-anti-patterns.md: "pagination binds BOTH layers").
 *    Storing a whole page-response blob and concatenating into it on load-more is the banned
 *    growing-blob shape and is deliberately not used here.
 *  - [CountsBreakdownMetaCacheEntity] — the FIXED-SIZE envelope (`total_*`, `charts`, `facets`).
 *    These are rolled up by the backend over the FULL filtered set, so they must be cached as
 *    served and never re-derived by summing the ~20 rows currently in memory.
 *  - [CountsBreakdownRemoteKeyEntity] — the next page offset per filter scope, so `RemoteMediator`
 *    can resume paging after process death without re-walking from zero.
 */

// ---------------------------------------------------------------------------
// Herd register summary — fixed-size rollup, JSON blob by scope
// ---------------------------------------------------------------------------

/**
 * `GET /herd-register/summary` cached per filter scope. A fixed-size rollup (one row per scope
 * grain), so the JSON-blob-by-scope shape used by every other rollup cache applies unchanged —
 * this is not a growable list.
 */
@Entity(tableName = "herd_summary_cache")
data class HerdSummaryCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface HerdSummaryCacheDao : JsonBlobCacheDao<HerdSummaryCacheEntity> {
    @Query("SELECT * FROM herd_summary_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<HerdSummaryCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: HerdSummaryCacheEntity)

    @Query("DELETE FROM herd_summary_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM herd_summary_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM herd_summary_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM herd_summary_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM herd_summary_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

// ---------------------------------------------------------------------------
// Shifting destination catalog — fixed-size vocabulary, single JSON blob
// ---------------------------------------------------------------------------

/**
 * `GET /app/counts/shifting/destinations` cached whole. Backs the shifting screen's two cascading
 * destination dropdowns (park -> that park's sheds).
 *
 * The JSON-blob shape is deliberate rather than a normalized park/shed pair of tables. This is a
 * bounded picker VOCABULARY, not a growable screen list: the farm has order-of two parks and ~154
 * sheds, and the whole catalog changes only when physical infrastructure does. Both dropdowns need
 * the entire set in hand at once to cascade, so paging it would buy nothing and cost a second
 * source of truth. It carries a single [cacheKey] because there is only one catalog per caller
 * scope — the row cap and TTL from [CacheGovernance] still apply through the shared
 * `enforceCacheBounds`/`readCachedJson` path, so it is bounded and self-healing like every other
 * blob cache here.
 */
@Entity(tableName = "counts_shifting_destinations_cache")
data class CountsShiftingDestinationsCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface CountsShiftingDestinationsCacheDao : JsonBlobCacheDao<CountsShiftingDestinationsCacheEntity> {
    @Query("SELECT * FROM counts_shifting_destinations_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<CountsShiftingDestinationsCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: CountsShiftingDestinationsCacheEntity)

    @Query("DELETE FROM counts_shifting_destinations_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM counts_shifting_destinations_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM counts_shifting_destinations_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM counts_shifting_destinations_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM counts_shifting_destinations_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

// ---------------------------------------------------------------------------
// Counts breakdown — fixed-size envelope, JSON blob by scope
// ---------------------------------------------------------------------------

/**
 * The breakdown's whole-result envelope (`total_rows`, `total_count`, `total_kids`,
 * `total_adults`, `charts`, `facets`, `projected_at`) WITHOUT the paged rows. Fixed size no matter
 * how many grains exist, so a blob is correct here; the rows live in
 * [CountsBreakdownItemEntity].
 */
@Entity(tableName = "counts_breakdown_meta_cache")
data class CountsBreakdownMetaCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface CountsBreakdownMetaCacheDao : JsonBlobCacheDao<CountsBreakdownMetaCacheEntity> {
    @Query("SELECT * FROM counts_breakdown_meta_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<CountsBreakdownMetaCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: CountsBreakdownMetaCacheEntity)

    @Query("DELETE FROM counts_breakdown_meta_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM counts_breakdown_meta_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM counts_breakdown_meta_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM counts_breakdown_meta_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM counts_breakdown_meta_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

// ---------------------------------------------------------------------------
// Counts breakdown — per-grain rows, read as a bounded PagingSource window
// ---------------------------------------------------------------------------

/**
 * ONE Room row per aggregated grain. [sortIndex] preserves the backend's server-side ordering
 * (the aggregate query's own ORDER BY) without the client re-sorting — position within the
 * scope, assigned as pages arrive. [grainKey] is the row's stable business identity, so a
 * refresh that re-serves the same grain REPLACEs it instead of duplicating it.
 */
@Entity(
    tableName = "counts_breakdown_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class CountsBreakdownItemEntity(
    val queryKey: String,
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The next `offset` for one filter scope, plus whether the backend has more rows to give. */
@Entity(tableName = "counts_breakdown_remote_keys")
data class CountsBreakdownRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextOffset: Int,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface CountsBreakdownItemDao {
    /**
     * The bounded window the UI observes. Room's [PagingSource] adds its own LIMIT/OFFSET per
     * page, so this never materializes the whole cached scope into memory — the DB read is
     * bounded exactly like the network page.
     */
    @Query(
        "SELECT * FROM counts_breakdown_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, CountsBreakdownItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<CountsBreakdownItemEntity>)

    @Query("DELETE FROM counts_breakdown_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /**
     * Row-retention bound across SCOPES: keeps only the newest [keepQueries] filter scopes'
     * rows, so a long-lived install that cycles through many filter combinations cannot grow
     * this table without limit.
     */
    @Query(
        "DELETE FROM counts_breakdown_items WHERE queryKey IN " +
            "(SELECT queryKey FROM counts_breakdown_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface CountsBreakdownRemoteKeyDao {
    @Query("SELECT * FROM counts_breakdown_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): CountsBreakdownRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: CountsBreakdownRemoteKeyEntity)

    @Query("DELETE FROM counts_breakdown_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM counts_breakdown_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM counts_breakdown_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
