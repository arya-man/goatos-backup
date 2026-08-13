package sg.mesha.goatos.core.data.cache

import androidx.paging.PagingSource
import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query

/**
 * Room tables behind the shifting PENDING-EXECUTION queue — the operator's "Pending" tab of
 * approved movements waiting to be physically walked (docs/decisions/android-offline-first.md).
 *
 * Room is the UI's single source of truth: the screen renders from here, the network refresh
 * upserts here, and an operator re-entering the tab sees their cached queue immediately rather than
 * a blank wall. Shaped exactly like the Counts APPROVAL queue pair ([CountsApprovalItemEntity] / its
 * remote key), NOT like the JSON-blob-by-scope rollups: this is a growable keyset-paginated list, so
 * rows are stored ONE ROOM ROW PER MOVEMENT and read back through a [PagingSource]. That is what
 * binds pagination to both layers (docs/decisions/mobile-data-fetch-anti-patterns.md) — caching
 * whole page responses and concatenating them on load-more is the banned growing-blob shape.
 *
 * [queryKey] scopes rows by the operator's farm -> shed filter (park|shed), so switching the filter
 * shows a different cached page rather than re-filtering one giant blob on device.
 */

/**
 * One authorized movement awaiting execution.
 *
 * [shiftingEventId] is the row's stable business identity, so a refresh that re-serves the same
 * movement REPLACEs it instead of duplicating it. [sortIndex] preserves the backend's own ordering
 * (authorized_at DESC) as pages arrive, so the client never re-sorts and a later page can never sort
 * above an earlier one. [raisedAt] is the raw wire string, parsed only for display — parsing it into
 * a comparison key on device would let a client-side timezone assumption reorder the queue.
 */
@Entity(
    tableName = "shifting_pending_items",
    primaryKeys = ["queryKey", "shiftingEventId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class ShiftingPendingItemEntity(
    val queryKey: String,
    val shiftingEventId: String,
    val sortIndex: Int,
    val raisedAt: String,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The opaque backend keyset cursor for the next page of one filter scope (park|shed). */
@Entity(tableName = "shifting_pending_remote_keys")
data class ShiftingPendingRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface ShiftingPendingItemDao {
    /**
     * The bounded window the UI observes. Room's [PagingSource] applies its own LIMIT/OFFSET per
     * page, so this never materializes the whole cached queue into memory — the DB read is bounded
     * exactly like the network page.
     */
    @Query(
        "SELECT * FROM shifting_pending_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, shiftingEventId ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, ShiftingPendingItemEntity>

    /** Bounded peek used to report queue depth/emptiness without loading the table. */
    @Query("SELECT COUNT(*) FROM shifting_pending_items WHERE queryKey = :queryKey")
    suspend fun countFor(queryKey: String): Int

    /**
     * One cached movement by id, from whichever filter scope cached it. Backs the execute screen's
     * offline-first open: the operator taps a row already in Room, so the detail renders from cache
     * with no refetch. LIMIT 1 — the same movement may be cached under more than one filter scope,
     * and its dtoJson is identical in each.
     */
    @Query("SELECT * FROM shifting_pending_items WHERE shiftingEventId = :shiftingEventId LIMIT 1")
    suspend fun findById(shiftingEventId: String): ShiftingPendingItemEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<ShiftingPendingItemEntity>)

    @Query("DELETE FROM shifting_pending_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /**
     * Drops one executed/cancelled movement from every cached scope.
     *
     * Called the moment a "Mark done" (or cancel) is durably queued in the outbox, so the row leaves
     * the operator's Pending list immediately instead of sitting there until the next refresh — and,
     * more importantly, so the same movement cannot be completed a second time while its first
     * completion is still draining. Scoped by shiftingEventId across ALL queryKeys because the same
     * movement can appear under more than one filter scope the operator visited.
     */
    @Query("DELETE FROM shifting_pending_items WHERE shiftingEventId = :shiftingEventId")
    suspend fun deleteMovement(shiftingEventId: String)

    /**
     * Row-retention bound across SCOPES: keeps only the newest [keepQueries] filter scopes' rows so
     * an install that switches farm/shed filters over months cannot grow this table without limit.
     */
    @Query(
        "DELETE FROM shifting_pending_items WHERE queryKey IN " +
            "(SELECT queryKey FROM shifting_pending_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface ShiftingPendingRemoteKeyDao {
    @Query("SELECT * FROM shifting_pending_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): ShiftingPendingRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: ShiftingPendingRemoteKeyEntity)

    @Query("DELETE FROM shifting_pending_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM shifting_pending_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM shifting_pending_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
