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
 * Room tables behind the Birth/Death follow-up workflow module
 * (docs/decisions/birth-death-workflows.md; docs/decisions/android-offline-first.md).
 *
 * Room is the UI's single source of truth: the two work-list screens (`/counts/birth`,
 * `/counts/death`) render from here, the network refresh upserts here, and an operator re-entering
 * a module sees their cached cards immediately rather than a blank wall. Four tables:
 *
 *  - [WorkflowCardEntity] + [WorkflowRemoteKeyEntity] — the keyset-paginated card list, shaped
 *    exactly like the shifting Pending queue pair ([ShiftingPendingItemEntity] / its remote key):
 *    one Room row per card, read back through a [PagingSource] so pagination binds BOTH layers
 *    (docs/decisions/mobile-data-fetch-anti-patterns.md). [WorkflowCardEntity.queryKey] scopes rows
 *    by (module | date | filter), so switching the day or chip shows a different cached page.
 *  - [WorkflowChipsCacheEntity] — the day's chip counts, a JSON-blob-by-scope rollup keyed
 *    (module | date). The counts are backend-computed; the client never re-derives them from the
 *    fetched page.
 *  - [WorkflowDetailCacheEntity] — the drill-in detail (card + facts + bounded ≤18-row action
 *    list), a JSON blob keyed by workflow id, mirroring [TaskDetailCacheEntity]. Bounded via
 *    [JsonBlobCacheDao] + `enforceCacheBounds`.
 */

/**
 * One work-list card. [workflowId] is the row's stable business identity, so a refresh that
 * re-serves the same workflow REPLACEs it instead of duplicating it. [sortIndex] preserves the
 * backend's own keyset ordering (next_due_at, workflow_id) as pages arrive.
 */
@Entity(
    tableName = "workflow_cards",
    primaryKeys = ["queryKey", "workflowId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class WorkflowCardEntity(
    val queryKey: String,
    val workflowId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The opaque backend keyset cursor for the next page of one (module | date | filter) scope. */
@Entity(tableName = "workflow_remote_keys")
data class WorkflowRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface WorkflowCardDao {
    /** The bounded window the UI observes; Room's [PagingSource] applies its own LIMIT per page. */
    @Query(
        "SELECT * FROM workflow_cards WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, workflowId ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, WorkflowCardEntity>

    @Query("SELECT COUNT(*) FROM workflow_cards WHERE queryKey = :queryKey")
    suspend fun countFor(queryKey: String): Int

    /**
     * One cached card by id, from whichever scope cached it. Backs the drill-in's offline-first
     * open: the operator taps a row already in Room, so the header renders from cache while the
     * detail fetch runs. LIMIT 1 — the same workflow may be cached under several scopes with
     * identical dtoJson.
     */
    @Query("SELECT * FROM workflow_cards WHERE workflowId = :workflowId LIMIT 1")
    suspend fun findById(workflowId: String): WorkflowCardEntity?

    /** Every cached scope containing one workflow. The same card can exist under `all` and a
     * status chip; the hard limit is the repository's retained-scope bound. */
    @Query(
        "SELECT * FROM workflow_cards WHERE workflowId = :workflowId " +
            "ORDER BY updatedAt DESC LIMIT :limit",
    )
    suspend fun findAllById(workflowId: String, limit: Int): List<WorkflowCardEntity>

    /** Bounded snapshot used to reconcile a network refresh with locally queued commands. */
    @Query(
        "SELECT * FROM workflow_cards WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, workflowId ASC LIMIT :limit",
    )
    suspend fun findForQuery(queryKey: String, limit: Int): List<WorkflowCardEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<WorkflowCardEntity>)

    @Query("DELETE FROM workflow_cards WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /**
     * Row-retention bound across SCOPES: keeps only the newest [keepQueries] (module|date|filter)
     * scopes' rows so day/chip churn over months cannot grow this table without limit.
     */
    @Query(
        "DELETE FROM workflow_cards WHERE queryKey IN " +
            "(SELECT queryKey FROM workflow_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface WorkflowRemoteKeyDao {
    @Query("SELECT * FROM workflow_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): WorkflowRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WorkflowRemoteKeyEntity)

    @Query("DELETE FROM workflow_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM workflow_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM workflow_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

/** The day's chip counts for one (module | date) scope — backend-computed, JSON-blob cached. */
@Entity(tableName = "workflow_chips_cache")
data class WorkflowChipsCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface WorkflowChipsCacheDao : JsonBlobCacheDao<WorkflowChipsCacheEntity> {
    @Query("SELECT * FROM workflow_chips_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<WorkflowChipsCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: WorkflowChipsCacheEntity)

    @Query("DELETE FROM workflow_chips_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM workflow_chips_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM workflow_chips_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM workflow_chips_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM workflow_chips_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

/** The drill-in detail (card + facts + bounded action list) keyed by workflow id. */
@Entity(tableName = "workflow_detail_cache")
data class WorkflowDetailCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface WorkflowDetailCacheDao : JsonBlobCacheDao<WorkflowDetailCacheEntity> {
    @Query("SELECT * FROM workflow_detail_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<WorkflowDetailCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: WorkflowDetailCacheEntity)

    @Query("DELETE FROM workflow_detail_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM workflow_detail_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM workflow_detail_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM workflow_detail_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM workflow_detail_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
