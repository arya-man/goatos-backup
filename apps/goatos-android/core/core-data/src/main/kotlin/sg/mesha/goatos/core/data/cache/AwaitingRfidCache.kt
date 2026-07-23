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
 * Room tables behind the operator "Awaiting RFID" list — goats that still carry an active temporary
 * tag and are waiting to be promoted to a permanent RFID (docs/decisions/android-offline-first.md).
 *
 * Room is the UI's single source of truth: the screen renders from here, the network refresh upserts
 * here, and an operator re-entering the list sees their cached rows immediately rather than a blank
 * wall. Shaped like the shifting PENDING-EXECUTION pair — ONE ROOM ROW PER GOAT read back through a
 * [PagingSource], NOT a JSON-blob-per-page — so pagination binds to both layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * Unlike the shifting queue there is no farm/shed filter scope: this is a single tenant-wide list, so
 * there is one remote-key row keyed by [AwaitingRfidRemoteKeyEntity.SCOPE] and the REFRESH clears the
 * whole table before re-inserting, which keeps it bounded to the (small) live awaiting-RFID set.
 */

/**
 * One goat awaiting a permanent RFID.
 *
 * [goatId] is the row's stable business identity, so a refresh that re-serves the same goat REPLACEs
 * it instead of duplicating it. [sortIndex] preserves the backend's own display_id ordering as pages
 * arrive, so the client never re-sorts and a later page can never sort above an earlier one.
 */
@Entity(
    tableName = "awaiting_rfid_items",
    indices = [Index(value = ["sortIndex"])],
)
data class AwaitingRfidItemEntity(
    @PrimaryKey val goatId: String,
    val sortIndex: Int,
    val displayId: String,
    val dtoJson: String,
    val updatedAt: Long,
)

/** The backend keyset cursor for the next page of the single awaiting-RFID list. */
@Entity(tableName = "awaiting_rfid_remote_keys")
data class AwaitingRfidRemoteKeyEntity(
    @PrimaryKey val id: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
) {
    companion object {
        /** The single scope id: this list is tenant-wide, not filtered. */
        const val SCOPE = "awaiting_rfid"
    }
}

@Dao
interface AwaitingRfidItemDao {
    /**
     * The bounded window the UI observes. Room's [PagingSource] applies its own LIMIT/OFFSET per page,
     * so this never materializes the whole cached list into memory — the DB read is bounded exactly
     * like the network page.
     */
    @Query("SELECT * FROM awaiting_rfid_items ORDER BY sortIndex ASC, goatId ASC") // mobile-guard:ignore: Room PagingSource — Room applies its own per-page LIMIT/OFFSET, so the observed read is a bounded ~20-row keyset window, never the whole table
    fun pagingSource(): PagingSource<Int, AwaitingRfidItemEntity>

    /** Bounded peek used to report list depth/emptiness without loading the table. */
    @Query("SELECT COUNT(*) FROM awaiting_rfid_items")
    suspend fun countAll(): Int

    /**
     * One cached goat by id. Backs the promote screen's offline-first open: the operator taps a row
     * already in Room, so the detail renders from cache with no refetch.
     */
    @Query("SELECT * FROM awaiting_rfid_items WHERE goatId = :goatId LIMIT 1")
    suspend fun findById(goatId: String): AwaitingRfidItemEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<AwaitingRfidItemEntity>)

    @Query("DELETE FROM awaiting_rfid_items")
    suspend fun deleteAll()

    /**
     * Drops one promoted goat from the list.
     *
     * Called the moment a promote is durably queued in the outbox, so the goat leaves the "Awaiting
     * RFID" list immediately instead of sitting there until the next refresh — and, more importantly,
     * so the same goat cannot be promoted a second time while its first promotion is still draining.
     */
    @Query("DELETE FROM awaiting_rfid_items WHERE goatId = :goatId")
    suspend fun deleteGoat(goatId: String)
}

@Dao
interface AwaitingRfidRemoteKeyDao {
    @Query("SELECT * FROM awaiting_rfid_remote_keys WHERE id = :id")
    suspend fun get(id: String): AwaitingRfidRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: AwaitingRfidRemoteKeyEntity)

    @Query("DELETE FROM awaiting_rfid_remote_keys WHERE id = :id")
    suspend fun delete(id: String)
}
