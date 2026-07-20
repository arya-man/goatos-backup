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
 * Room tables behind the Counts APPROVAL queue (docs/decisions/android-offline-first.md). Room is
 * the UI's single source of truth: the screen renders from here, the network refresh upserts here,
 * and an approver re-entering the tab sees their cached queue immediately rather than a blank wall.
 *
 * Shaped like the Calendar schedule pair ([CalendarScheduleEntity] / its remote key), NOT like the
 * JSON-blob-by-scope rollups: the queue is a growable keyset-paginated list, so rows are stored
 * ONE ROOM ROW PER REQUEST and read back through a [PagingSource]. That is what binds pagination to
 * both layers (docs/decisions/mobile-data-fetch-anti-patterns.md) — caching whole page responses
 * and concatenating them on load-more is the banned growing-blob shape.
 */

/**
 * One pending decision.
 *
 * [approvalRequestId] is the row's stable business identity, so a refresh that re-serves the same
 * request REPLACEs it instead of duplicating it. [sortIndex] preserves the backend's own ordering
 * (the queue's server-side ORDER BY) as pages arrive, so the client never re-sorts and a later page
 * can never sort above an earlier one.
 *
 * [raisedAt] is stored as the raw wire string and only parsed for display: parsing it into a
 * comparison key on device would let a client-side timezone assumption reorder an approver's queue.
 */
@Entity(
    tableName = "counts_approval_items",
    primaryKeys = ["queryKey", "approvalRequestId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class CountsApprovalItemEntity(
    val queryKey: String,
    val approvalRequestId: String,
    val sortIndex: Int,
    val raisedAt: String,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The opaque backend keyset cursor for the next page of one queue scope (status filter). */
@Entity(tableName = "counts_approval_remote_keys")
data class CountsApprovalRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface CountsApprovalItemDao {
    /**
     * The bounded window the UI observes. Room's [PagingSource] applies its own LIMIT/OFFSET per
     * page, so this never materializes the whole cached queue into memory — the DB read is bounded
     * exactly like the network page.
     */
    @Query(
        "SELECT * FROM counts_approval_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, approvalRequestId ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, CountsApprovalItemEntity>

    /** Bounded peek used to report queue depth/emptiness without loading the table. */
    @Query("SELECT COUNT(*) FROM counts_approval_items WHERE queryKey = :queryKey")
    suspend fun countFor(queryKey: String): Int

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<CountsApprovalItemEntity>)

    @Query("DELETE FROM counts_approval_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /**
     * Drops one decided request from the cached queue.
     *
     * Called the moment an approve/reject is durably queued in the outbox, so the row leaves the
     * approver's pending list immediately instead of sitting there until the next refresh — and,
     * more importantly, so it cannot be tapped a second time while its decision is still draining.
     */
    @Query("DELETE FROM counts_approval_items WHERE approvalRequestId = :approvalRequestId")
    suspend fun deleteRequest(approvalRequestId: String)

    /**
     * Row-retention bound across SCOPES: keeps only the newest [keepQueries] queue scopes' rows so
     * an install that switches status filters over months cannot grow this table without limit.
     */
    @Query(
        "DELETE FROM counts_approval_items WHERE queryKey IN " +
            "(SELECT queryKey FROM counts_approval_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface CountsApprovalRemoteKeyDao {
    @Query("SELECT * FROM counts_approval_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): CountsApprovalRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: CountsApprovalRemoteKeyEntity)

    @Query("DELETE FROM counts_approval_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM counts_approval_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM counts_approval_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
