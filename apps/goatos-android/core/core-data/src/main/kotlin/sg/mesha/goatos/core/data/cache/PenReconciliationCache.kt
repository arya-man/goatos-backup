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
 * Room tables behind the Herd Operations RECONCILE tab — the operator's queue of "wrong pen" cards
 * raised by weighing submits (maintainer decision 2026-09-02;
 * docs/decisions/pen-reconciliation.md, docs/decisions/android-offline-first.md).
 *
 * Room is the UI's single source of truth: the screen renders from here, the network refresh
 * upserts here, and an operator re-entering the tab sees their cached queue immediately rather
 * than a blank wall. Shaped exactly like the shifting pending pair
 * ([ShiftingPendingItemEntity] / its remote key): a growable keyset-paginated list, stored ONE
 * ROOM ROW PER CARD and read back through a [PagingSource] — never a whole-page JSON blob.
 *
 * [queryKey] scopes rows by the selected status bucket, so switching the status chip shows a
 * different cached page rather than re-filtering one giant blob on device.
 */

/**
 * One Reconcile card. [cardId] is the row's stable business identity, so a refresh that re-serves
 * the same card REPLACEs it instead of duplicating it. [sortIndex] preserves the backend's own
 * ordering as pages arrive; the client never re-sorts.
 */
@Entity(
    tableName = "pen_reconciliation_items",
    primaryKeys = ["queryKey", "cardId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class PenReconciliationItemEntity(
    val queryKey: String,
    val cardId: String,
    val sortIndex: Int,
    val raisedAt: String,
    val dtoJson: String,
    val updatedAt: Long,
    val statusRank: Int,
)

/** The opaque backend keyset cursor for the next page of one status scope. */
@Entity(tableName = "pen_reconciliation_remote_keys")
data class PenReconciliationRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface PenReconciliationItemDao {
    /**
     * The bounded window the UI observes. Room's [PagingSource] applies its own LIMIT/OFFSET per
     * page, so this never materializes the whole cached queue into memory — the DB read is
     * bounded exactly like the network page.
     */
    @Query(
        "SELECT * FROM pen_reconciliation_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, cardId ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, PenReconciliationItemEntity>

    /** Bounded peek used to offset a freshly appended page without loading the table. */
    @Query("SELECT COUNT(*) FROM pen_reconciliation_items WHERE queryKey = :queryKey")
    suspend fun countFor(queryKey: String): Int

    /**
     * One cached card by id, from whichever status scope cached it. Backs the execute screen's
     * offline-first open: the operator taps a row already in Room, so the detail renders from
     * cache with no refetch. The same card may be cached under more than one status scope; prefer
     * the newest monotonic refresh snapshot so a freshly refreshed `completed`/`all` row cannot be
     * masked by an older `open`/`rework` copy from another chip. If legacy/concurrent rows still
     * tie on freshness, prefer the furthest-forward workflow state rather than lexicographic scope.
     */
    @Query(
        "SELECT * FROM pen_reconciliation_items WHERE cardId = :cardId " +
            "ORDER BY updatedAt DESC, statusRank DESC, queryKey ASC LIMIT 1",
    )
    suspend fun findById(cardId: String): PenReconciliationItemEntity?

    @Query("SELECT MAX(updatedAt) FROM pen_reconciliation_items")
    suspend fun maxUpdatedAt(): Long?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<PenReconciliationItemEntity>)

    @Query("DELETE FROM pen_reconciliation_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /**
     * Drops one submitted card from every cached scope the moment its completion is durably
     * queued, so the card leaves the operator's actionable list immediately instead of sitting
     * there until the next refresh — and so the same card cannot be completed a second time while
     * its first completion is still draining.
     */
    @Query("DELETE FROM pen_reconciliation_items WHERE cardId = :cardId")
    suspend fun deleteCard(cardId: String)

    /**
     * Row-retention bound across SCOPES: keeps only the newest [keepQueries] status scopes' rows
     * so this table cannot grow without limit as the operator switches chips over months.
     */
    @Query(
        "DELETE FROM pen_reconciliation_items WHERE queryKey IN " +
            "(SELECT queryKey FROM pen_reconciliation_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface PenReconciliationRemoteKeyDao {
    @Query("SELECT * FROM pen_reconciliation_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): PenReconciliationRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: PenReconciliationRemoteKeyEntity)

    @Query("DELETE FROM pen_reconciliation_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM pen_reconciliation_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM pen_reconciliation_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}
