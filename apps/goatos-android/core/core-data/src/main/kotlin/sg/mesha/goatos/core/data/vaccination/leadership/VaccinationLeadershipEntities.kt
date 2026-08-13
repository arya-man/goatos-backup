package sg.mesha.goatos.core.data.vaccination.leadership

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Room tables behind the VACCINATION LEADERSHIP reads (the videos gallery showing full
 * evidence trail including pending/approved/rejected/closed items).
 *
 * Every one follows the same contract as the rest of the app
 * (docs/decisions/android-offline-first.md, docs/decisions/mobile-data-fetch-anti-patterns.md):
 *
 * - Room is the SSOT the screen renders from. The network refresh only writes here; the observed
 *   Flow re-emits. A failed refresh leaves the cached rows visible.
 * - The observed read is a BOUNDED window ordered by the SAME key the backend pages on, so Room
 *   never becomes the place the over-fetch moved to. It is never `SELECT *` over a scope.
 * - A refresh UPSERTS by the backend's own id, so re-reading page 1 updates rows instead of
 *   duplicating them.
 * - The table is bounded by a newest-N-items prune, so watching many verification items
 *   cannot grow the cache without limit.
 *
 * Row payloads are stored as the backend DTO's JSON. The mapping to the screen model stays in ONE
 * place (the repository) rather than being re-implemented as a column layout.
 */

/** One screen-page. Matches the backend's own default for these reads. */
const val VACCINATION_LEADERSHIP_PAGE_SIZE = 20

/** Hard ceiling on the observed window of the leadership gallery, in rows. */
const val VACCINATION_LEADERSHIP_MAX_WINDOW = VACCINATION_LEADERSHIP_PAGE_SIZE * 5

/**
 * One verification item in the leadership videos gallery.
 *
 * [queryKey] is the filter scope: always "vaccination_leadership". [sortIndex] preserves the
 * server's own order across pages, so page 2 can never sort above page 1. The gallery shows
 * items in their natural backend order without filtering.
 */
@Entity(
    tableName = "vaccination_leadership_item",
    primaryKeys = ["queryKey", "itemId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class VaccinationLeadershipItemEntity(
    val queryKey: String,
    val itemId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

/**
 * The cursor and metadata for the vaccination leadership gallery.
 *
 * Tracks pagination state across network fetches so keyset-paginated reads can
 * append correctly.
 */
@Entity(tableName = "vaccination_leadership_remote_key")
data class VaccinationLeadershipRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

/**
 * DAO for vaccination leadership gallery items.
 */
@Dao
interface VaccinationLeadershipItemDao {
    @Query(
        """
        SELECT * FROM vaccination_leadership_item
        WHERE queryKey = :queryKey
        ORDER BY sortIndex ASC
        LIMIT :limit
        """
    )
    fun observeWindow(queryKey: String, limit: Int): Flow<List<VaccinationLeadershipItemEntity>>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<VaccinationLeadershipItemEntity>)

    @Query("SELECT COUNT(*) FROM vaccination_leadership_item WHERE queryKey = :queryKey")
    suspend fun countByQuery(queryKey: String): Int

    @Query("SELECT MAX(sortIndex) FROM vaccination_leadership_item WHERE queryKey = :queryKey")
    suspend fun maxSortIndex(queryKey: String): Int?

    @Query("DELETE FROM vaccination_leadership_item WHERE queryKey = :queryKey")
    suspend fun clearByQuery(queryKey: String)

    @Query(
        """
        DELETE FROM vaccination_leadership_item
        WHERE queryKey = :queryKey
        AND sortIndex < (SELECT COALESCE(MAX(sortIndex), 0) FROM vaccination_leadership_item WHERE queryKey = :queryKey) - :keepCount
        """
    )
    suspend fun pruneOldest(queryKey: String, keepCount: Int)
}

/**
 * DAO for vaccination leadership remote pagination keys.
 */
@Dao
interface VaccinationLeadershipRemoteKeyDao {
    @Query("SELECT * FROM vaccination_leadership_remote_key WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): VaccinationLeadershipRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: VaccinationLeadershipRemoteKeyEntity)

    @Query("DELETE FROM vaccination_leadership_remote_key WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)
}
