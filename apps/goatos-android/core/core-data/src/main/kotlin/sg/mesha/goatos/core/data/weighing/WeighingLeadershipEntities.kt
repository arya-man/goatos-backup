package sg.mesha.goatos.core.data.weighing

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Room tables behind the WEIGHING LEADERSHIP reads (task list, task detail, shed detail + its
 * captured records, the videos gallery, and the planner catalog).
 *
 * Every one of them follows the same contract as the rest of the app
 * (docs/decisions/android-offline-first.md, docs/decisions/mobile-data-fetch-anti-patterns.md):
 *
 * - Room is the SSOT the screen renders from. The network refresh only writes here; the observed
 *   Flow re-emits. A failed refresh leaves the cached rows visible.
 * - The observed read is a BOUNDED window ordered by the SAME key the backend pages on, so Room
 *   never becomes the place the over-fetch moved to. It is never `SELECT *` over a scope.
 * - A refresh UPSERTS by the backend's own id, so re-reading page 1 updates rows instead of
 *   duplicating them.
 * - Each table is bounded by a newest-N-scopes prune, so walking many tasks/sheds/dates cannot grow
 *   the cache without limit.
 *
 * Row payloads are stored as the backend DTO's JSON. The mapping to the screen model stays in ONE
 * place (the repository) rather than being re-implemented as a column layout that silently drops a
 * field the contract added.
 */

/** One screen-page. Matches the backend's own default and cap for these reads. */
const val WEIGHING_LEADERSHIP_PAGE_SIZE = 20

/** Hard ceiling on the observed window of any leadership list, in rows. */
const val WEIGHING_LEADERSHIP_MAX_WINDOW = WEIGHING_LEADERSHIP_PAGE_SIZE * 5

// ---------------------------------------------------------------------------------------------
// L0 — task list
// ---------------------------------------------------------------------------------------------

/**
 * One task (one park on one Asia/Kolkata business DATE) in one filter scope.
 *
 * [queryKey] is the filter the rows were read under (surface scope + park filter): two filters are
 * two independent keyset streams and must not interleave. [sortIndex] preserves the server's own
 * order across pages, so page 2 can never sort above page 1.
 */
@Entity(
    tableName = "weighing_task_row",
    primaryKeys = ["queryKey", "campaignId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class WeighingTaskRowEntity(
    val queryKey: String,
    val campaignId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

/**
 * The cursor and the WHOLE-SCOPE tallies for one task-list filter.
 *
 * The tab counts and the capability flags live here, not derived from the cached rows: counting the
 * cached page would make the tab numbers move every time a page lands.
 */
@Entity(tableName = "weighing_task_remote_key")
data class WeighingTaskRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val activeCount: Int,
    val completedCount: Int,
    val canPublish: Boolean,
    val canEnd: Boolean,
    val canReopen: Boolean,
    val updatedAt: Long,
)

// ---------------------------------------------------------------------------------------------
// L1 — task detail (the task's shed buckets)
// ---------------------------------------------------------------------------------------------

/** One bucket = one shed on one task. [campaignId] is the scope; [sortIndex] is server order. */
@Entity(
    tableName = "weighing_task_bucket_row",
    primaryKeys = ["campaignId", "campaignShedId"],
    indices = [Index(value = ["campaignId", "sortIndex"])],
)
data class WeighingTaskBucketRowEntity(
    val campaignId: String,
    val campaignShedId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

/**
 * Cursor + the WHOLE-TASK bucket count. [totalCount] ranges over the same key set the rows range
 * over, so the header does not change as the reader scrolls.
 */
@Entity(tableName = "weighing_task_bucket_remote_key")
data class WeighingTaskBucketRemoteKeyEntity(
    @PrimaryKey val campaignId: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val totalCount: Int,
    val updatedAt: Long,
)

// ---------------------------------------------------------------------------------------------
// L2 — shed detail + captured records (also the leadership videos gallery)
// ---------------------------------------------------------------------------------------------

/**
 * The shed bucket's own context, as the backend states it.
 *
 * Park name, weigh date and assignee name are COLUMNS here because they are the shed read's own
 * answer — a cold deep link renders the eyebrow and the assignee from Room without the parent list
 * having to hand them over as route arguments.
 *
 * [operatorDisplayName] blank WITH a non-blank [operatorUserId] is a roster gap, not "not
 * assigned"; only a blank [operatorUserId] means nobody is assigned yet.
 */
@Entity(
    tableName = "weighing_leadership_shed",
    indices = [
        Index(value = ["campaignId"]),
        Index(value = ["galleryQueryKey", "gallerySortIndex"]),
    ],
)
data class WeighingLeadershipShedEntity(
    /** `campaignId:campaignShedId` — the bucket's identity across both surfaces that read it. */
    @PrimaryKey val shedKey: String,
    val campaignId: String,
    val campaignShedId: String,
    val shedName: String,
    val parkName: String,
    /** Asia/Kolkata business DATE. Never a timestamp. */
    val weighDate: String,
    val operatorUserId: String,
    val operatorDisplayName: String,
    val category: String,
    val status: String,
    val periodLabel: String,
    val estimatedAnimalCount: Int,
    val maxShedVideos: Int,
    /** The single latest lump-sum observation as JSON, or null for an individual-mode bucket. */
    val lumpSumJson: String?,
    /**
     * Set only when this bucket also appears in the leadership VIDEOS gallery, which walks the same
     * shed read as a list. Null for a bucket reached by drilling into one shed. Keeping the gallery
     * order here — rather than in a second copy of the shed table — is what makes the gallery and
     * the shed detail read the SAME row, so opening one after the other cannot show two answers.
     */
    val galleryQueryKey: String?,
    val gallerySortIndex: Int?,
    val updatedAt: Long,
)

/** One captured animal weight on one shed bucket. Paged; never a denominator. */
@Entity(
    tableName = "weighing_leadership_record",
    primaryKeys = ["shedKey", "observationId"],
    indices = [Index(value = ["shedKey", "sortIndex"])],
)
data class WeighingLeadershipRecordEntity(
    val shedKey: String,
    val observationId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "weighing_leadership_record_remote_key")
data class WeighingLeadershipRecordRemoteKeyEntity(
    @PrimaryKey val shedKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

/**
 * The gallery's OWN cursor.
 *
 * It used to be stored as a task-list remote key under an invented query key, in the same table the
 * L0 task list prunes by newest-N-filters: browsing task filters evicted the gallery's cursor while
 * its rows were still on screen, and the gallery row consumed a filter slot, evicting a real
 * filter's tallies. A surface's cursor belongs to that surface's own table.
 */
@Entity(tableName = "weighing_leadership_gallery_remote_key")
data class WeighingLeadershipGalleryRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

// ---------------------------------------------------------------------------------------------
// Planner catalog (wizard)
// ---------------------------------------------------------------------------------------------

/**
 * One selectable PARK on the planner's park step, for ONE weigh date.
 *
 * Parks and sheds are two different grains and are cached in two different tables. Parks are few
 * and the step must offer them ALL, so this table holds no cursor: the whole park read replaces it
 * in one transaction. [shedCount] is the backend's own park-grain total, never a count of the shed
 * rows this device happens to hold — those live in [WeighingPlannerShedRowEntity], ~20 at a time.
 */
@Entity(
    tableName = "weighing_planner_park_row",
    primaryKeys = ["queryKey", "parkId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class WeighingPlannerParkRowEntity(
    /** The weigh DATE this park list was answered for. */
    val queryKey: String,
    val parkId: String,
    val sortIndex: Int,
    val name: String,
    val kidCount: Int,
    /** The park's own active-shed total, as the backend counted it. */
    val shedCount: Int,
    /** The park's existing task on this date, as JSON, when the backend reported one. */
    val existingCampaignJson: String?,
    val updatedAt: Long,
)

/**
 * One selectable shed on the bucket step, carrying its park's identity inline.
 *
 * Rows are keyed by `<weigh date>|<park id>` — the bucket page is per PARK, so one park's keyset
 * stream can never interleave with another's the way a flattened all-parks page could.
 */
@Entity(
    tableName = "weighing_planner_shed_row",
    primaryKeys = ["queryKey", "locationId", "partitionKey"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class WeighingPlannerShedRowEntity(
    /** `<weigh date>|<park id>`. Availability is date-scoped and the page is park-scoped. */
    val queryKey: String,
    val locationId: String,
    val partitionKey: String = "",
    val parkId: String,
    val parkName: String,
    val sortIndex: Int,
    val shedJson: String,
    /** The park's existing task on this date, as JSON, when the backend reported one. */
    val existingCampaignJson: String?,
    val updatedAt: Long,
)

/** The operator picker vocabulary. The backend sends it with the FIRST catalog page only. */
@Entity(
    tableName = "weighing_planner_operator_row",
    primaryKeys = ["queryKey", "userId"],
    indices = [Index(value = ["queryKey", "sortIndex"])],
)
data class WeighingPlannerOperatorRowEntity(
    val queryKey: String,
    val userId: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "weighing_planner_remote_key")
data class WeighingPlannerRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String?,
    val endReached: Boolean,
    val updatedAt: Long,
)

// ---------------------------------------------------------------------------------------------
// DAOs
// ---------------------------------------------------------------------------------------------

@Dao
interface WeighingTaskDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<WeighingTaskRowEntity>)

    /**
     * The BOUNDED observed window: ordered by the same key the backend pages on, capped by [limit].
     * Never an unbounded scope read — the cache is not allowed to be where the over-fetch moved to.
     */
    @Query("SELECT * FROM weighing_task_row WHERE queryKey = :queryKey ORDER BY sortIndex ASC LIMIT :limit")
    fun observeWindow(queryKey: String, limit: Int): Flow<List<WeighingTaskRowEntity>>

    /**
     * The next append position. Derived from MAX(sortIndex), never from COUNT(*): every row DAO
     * upserts by the backend's own id, so a row re-sent on a later page REPLACES instead of adding
     * and leaves the count behind the highest stored index — which would restart the next page
     * below indices already in the table and let the bounded window drop or reorder rows.
     */
    @Query("SELECT COALESCE(MAX(sortIndex), -1) + 1 FROM weighing_task_row WHERE queryKey = :queryKey")
    suspend fun nextSortIndex(queryKey: String): Int

    @Query("DELETE FROM weighing_task_row WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Row-count bound: keeps only the newest [keep] filter scopes. */
    @Query(
        "DELETE FROM weighing_task_row WHERE queryKey NOT IN (" +
            "SELECT queryKey FROM weighing_task_row GROUP BY queryKey ORDER BY MAX(updatedAt) DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)
}

@Dao
interface WeighingTaskRemoteKeyDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WeighingTaskRemoteKeyEntity)

    @Query("SELECT * FROM weighing_task_remote_key WHERE queryKey = :queryKey")
    fun observe(queryKey: String): Flow<WeighingTaskRemoteKeyEntity?>

    @Query("SELECT * FROM weighing_task_remote_key WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): WeighingTaskRemoteKeyEntity?

    @Query("DELETE FROM weighing_task_remote_key WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM weighing_task_remote_key WHERE queryKey NOT IN (" +
            "SELECT queryKey FROM weighing_task_remote_key ORDER BY updatedAt DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)
}

@Dao
interface WeighingTaskBucketDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<WeighingTaskBucketRowEntity>)

    @Query("SELECT * FROM weighing_task_bucket_row WHERE campaignId = :campaignId ORDER BY sortIndex ASC LIMIT :limit")
    fun observeWindow(campaignId: String, limit: Int): Flow<List<WeighingTaskBucketRowEntity>>

    /** Next append position — MAX(sortIndex)+1, never COUNT(*). See [WeighingTaskDao.nextSortIndex]. */
    @Query("SELECT COALESCE(MAX(sortIndex), -1) + 1 FROM weighing_task_bucket_row WHERE campaignId = :campaignId")
    suspend fun nextSortIndex(campaignId: String): Int

    @Query("DELETE FROM weighing_task_bucket_row WHERE campaignId = :campaignId")
    suspend fun deleteQuery(campaignId: String)

    @Query(
        "DELETE FROM weighing_task_bucket_row WHERE campaignId NOT IN (" +
            "SELECT campaignId FROM weighing_task_bucket_row GROUP BY campaignId ORDER BY MAX(updatedAt) DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)
}

@Dao
interface WeighingTaskBucketRemoteKeyDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WeighingTaskBucketRemoteKeyEntity)

    @Query("SELECT * FROM weighing_task_bucket_remote_key WHERE campaignId = :campaignId")
    fun observe(campaignId: String): Flow<WeighingTaskBucketRemoteKeyEntity?>

    @Query("SELECT * FROM weighing_task_bucket_remote_key WHERE campaignId = :campaignId")
    suspend fun get(campaignId: String): WeighingTaskBucketRemoteKeyEntity?

    @Query("DELETE FROM weighing_task_bucket_remote_key WHERE campaignId = :campaignId")
    suspend fun delete(campaignId: String)

    @Query(
        "DELETE FROM weighing_task_bucket_remote_key WHERE campaignId NOT IN (" +
            "SELECT campaignId FROM weighing_task_bucket_remote_key ORDER BY updatedAt DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)
}

@Dao
interface WeighingLeadershipShedDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(row: WeighingLeadershipShedEntity)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<WeighingLeadershipShedEntity>)

    @Query("SELECT * FROM weighing_leadership_shed WHERE shedKey = :shedKey")
    fun observe(shedKey: String): Flow<WeighingLeadershipShedEntity?>

    @Query("SELECT * FROM weighing_leadership_shed WHERE shedKey = :shedKey")
    suspend fun get(shedKey: String): WeighingLeadershipShedEntity?

    /** The gallery's BOUNDED window, in the server's own page order. */
    @Query(
        "SELECT * FROM weighing_leadership_shed WHERE galleryQueryKey = :queryKey " +
            "ORDER BY gallerySortIndex ASC LIMIT :limit",
    )
    fun observeGalleryWindow(queryKey: String, limit: Int): Flow<List<WeighingLeadershipShedEntity>>

    /** Next gallery append position — MAX(gallerySortIndex)+1, never COUNT(*). */
    @Query(
        "SELECT COALESCE(MAX(gallerySortIndex), -1) + 1 FROM weighing_leadership_shed " +
            "WHERE galleryQueryKey = :queryKey",
    )
    suspend fun nextGallerySortIndex(queryKey: String): Int

    /**
     * Clears the gallery membership WITHOUT deleting the bucket rows: a bucket that has also been
     * opened on its own detail screen keeps its cached context when the gallery re-reads page 1.
     */
    @Query(
        "UPDATE weighing_leadership_shed SET galleryQueryKey = NULL, gallerySortIndex = NULL " +
            "WHERE galleryQueryKey = :queryKey",
    )
    suspend fun clearGallery(queryKey: String)

    /** Row-count bound: only the newest [keep] visited buckets keep their cached context. */
    @Query(
        "DELETE FROM weighing_leadership_shed WHERE shedKey NOT IN (" +
            "SELECT shedKey FROM weighing_leadership_shed ORDER BY updatedAt DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewest(keep: Int)
}

@Dao
interface WeighingLeadershipRecordDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<WeighingLeadershipRecordEntity>)

    @Query("SELECT * FROM weighing_leadership_record WHERE shedKey = :shedKey ORDER BY sortIndex ASC LIMIT :limit")
    fun observeWindow(shedKey: String, limit: Int): Flow<List<WeighingLeadershipRecordEntity>>

    /**
     * The gallery's records: a window bounded PER SHED, not one unbounded read across every shed in
     * the gallery page.
     *
     * The correlated count is the per-partition bound (SQLite window functions are not available on
     * every supported API level). [shedKeys] is the gallery's own bounded shed window, so the read
     * is bounded on both axes: at most `shedKeys.size * perShedLimit` rows.
     */
    @Query(
        "SELECT r.* FROM weighing_leadership_record r WHERE r.shedKey IN (:shedKeys) AND (" +
            "SELECT COUNT(*) FROM weighing_leadership_record x " +
            "WHERE x.shedKey = r.shedKey AND x.sortIndex < r.sortIndex" +
            ") < :perShedLimit ORDER BY r.shedKey ASC, r.sortIndex ASC",
    )
    fun observeWindowForSheds(shedKeys: List<String>, perShedLimit: Int): Flow<List<WeighingLeadershipRecordEntity>>

    /** Next append position — MAX(sortIndex)+1, never COUNT(*). See [WeighingTaskDao.nextSortIndex]. */
    @Query("SELECT COALESCE(MAX(sortIndex), -1) + 1 FROM weighing_leadership_record WHERE shedKey = :shedKey")
    suspend fun nextSortIndex(shedKey: String): Int

    /**
     * Records whose bucket is no longer cached. The bucket table is bounded by a newest-N prune;
     * without this the records of every evicted bucket stay behind forever, so a gallery refresh
     * that writes records for hundreds of buckets reclaims none of them.
     */
    @Query("DELETE FROM weighing_leadership_record WHERE shedKey NOT IN (SELECT shedKey FROM weighing_leadership_shed)")
    suspend fun pruneOrphans()

    @Query("DELETE FROM weighing_leadership_record WHERE shedKey = :shedKey")
    suspend fun deleteQuery(shedKey: String)

    @Query(
        "DELETE FROM weighing_leadership_record WHERE shedKey NOT IN (" +
            "SELECT shedKey FROM weighing_leadership_record GROUP BY shedKey ORDER BY MAX(updatedAt) DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)
}

@Dao
interface WeighingLeadershipRecordRemoteKeyDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WeighingLeadershipRecordRemoteKeyEntity)

    @Query("SELECT * FROM weighing_leadership_record_remote_key WHERE shedKey = :shedKey")
    fun observe(shedKey: String): Flow<WeighingLeadershipRecordRemoteKeyEntity?>

    @Query("SELECT * FROM weighing_leadership_record_remote_key WHERE shedKey = :shedKey")
    suspend fun get(shedKey: String): WeighingLeadershipRecordRemoteKeyEntity?

    @Query("DELETE FROM weighing_leadership_record_remote_key WHERE shedKey = :shedKey")
    suspend fun delete(shedKey: String)

    @Query(
        "DELETE FROM weighing_leadership_record_remote_key WHERE shedKey NOT IN (" +
            "SELECT shedKey FROM weighing_leadership_record_remote_key ORDER BY updatedAt DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)

    /** Cursors whose bucket is no longer cached. See [WeighingLeadershipRecordDao.pruneOrphans]. */
    @Query(
        "DELETE FROM weighing_leadership_record_remote_key WHERE shedKey NOT IN (" +
            "SELECT shedKey FROM weighing_leadership_shed)",
    )
    suspend fun pruneOrphans()
}

@Dao
interface WeighingLeadershipGalleryRemoteKeyDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WeighingLeadershipGalleryRemoteKeyEntity)

    @Query("SELECT * FROM weighing_leadership_gallery_remote_key WHERE queryKey = :queryKey")
    fun observe(queryKey: String): Flow<WeighingLeadershipGalleryRemoteKeyEntity?>

    @Query("SELECT * FROM weighing_leadership_gallery_remote_key WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): WeighingLeadershipGalleryRemoteKeyEntity?
}

@Dao
interface WeighingPlannerCatalogDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertParks(rows: List<WeighingPlannerParkRowEntity>)

    /**
     * The park picker's BOUNDED read. Parks are few (a handful in the real data) and the step must
     * offer them ALL, so the limit is a sanity ceiling matching the backend's own park cap — not a
     * page size. There is no park cursor.
     */
    @Query("SELECT * FROM weighing_planner_park_row WHERE queryKey = :queryKey ORDER BY sortIndex ASC LIMIT :limit")
    fun observeParks(queryKey: String, limit: Int): Flow<List<WeighingPlannerParkRowEntity>>

    @Query("DELETE FROM weighing_planner_park_row WHERE queryKey = :queryKey")
    suspend fun deleteParks(queryKey: String)

    @Query(
        "DELETE FROM weighing_planner_park_row WHERE queryKey NOT IN (" +
            "SELECT queryKey FROM weighing_planner_park_row GROUP BY queryKey ORDER BY MAX(updatedAt) DESC LIMIT :keep)",
    )
    suspend fun pruneParksOutsideNewestQueries(keep: Int)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertSheds(rows: List<WeighingPlannerShedRowEntity>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertOperators(rows: List<WeighingPlannerOperatorRowEntity>)

    @Query("SELECT * FROM weighing_planner_shed_row WHERE queryKey = :queryKey ORDER BY sortIndex ASC LIMIT :limit")
    fun observeShedWindow(queryKey: String, limit: Int): Flow<List<WeighingPlannerShedRowEntity>>

    @Query("SELECT * FROM weighing_planner_operator_row WHERE queryKey = :queryKey ORDER BY sortIndex ASC LIMIT :limit")
    fun observeOperators(queryKey: String, limit: Int): Flow<List<WeighingPlannerOperatorRowEntity>>

    /**
     * Next append position — MAX(sortIndex)+1, never COUNT(*). The planner catalog EXPECTS
     * duplicates: a park whose sheds straddle a page boundary is repeated on the next page, so a
     * re-sent shed replaces its row and a COUNT-derived start index would collide.
     */
    @Query("SELECT COALESCE(MAX(sortIndex), -1) + 1 FROM weighing_planner_shed_row WHERE queryKey = :queryKey")
    suspend fun nextShedSortIndex(queryKey: String): Int

    @Query("SELECT COUNT(*) FROM weighing_planner_operator_row WHERE queryKey = :queryKey")
    suspend fun countOperators(queryKey: String): Int

    @Query("DELETE FROM weighing_planner_shed_row WHERE queryKey = :queryKey")
    suspend fun deleteSheds(queryKey: String)

    @Query("DELETE FROM weighing_planner_operator_row WHERE queryKey = :queryKey")
    suspend fun deleteOperators(queryKey: String)

    @Query(
        "DELETE FROM weighing_planner_shed_row WHERE queryKey NOT IN (" +
            "SELECT queryKey FROM weighing_planner_shed_row GROUP BY queryKey ORDER BY MAX(updatedAt) DESC LIMIT :keep)",
    )
    suspend fun pruneShedsOutsideNewestQueries(keep: Int)

    @Query(
        "DELETE FROM weighing_planner_operator_row WHERE queryKey NOT IN (" +
            "SELECT queryKey FROM weighing_planner_operator_row GROUP BY queryKey ORDER BY MAX(updatedAt) DESC LIMIT :keep)",
    )
    suspend fun pruneOperatorsOutsideNewestQueries(keep: Int)
}

@Dao
interface WeighingPlannerRemoteKeyDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: WeighingPlannerRemoteKeyEntity)

    @Query("SELECT * FROM weighing_planner_remote_key WHERE queryKey = :queryKey")
    fun observe(queryKey: String): Flow<WeighingPlannerRemoteKeyEntity?>

    @Query("SELECT * FROM weighing_planner_remote_key WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): WeighingPlannerRemoteKeyEntity?

    @Query("DELETE FROM weighing_planner_remote_key WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM weighing_planner_remote_key WHERE queryKey NOT IN (" +
            "SELECT queryKey FROM weighing_planner_remote_key ORDER BY updatedAt DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewestQueries(keep: Int)
}

/** The bucket identity shared by the shed-detail screen and the videos gallery. */
fun weighingShedKey(campaignId: String, campaignShedId: String): String =
    "${campaignId.trim()}:${campaignShedId.trim()}"

/**
 * The idempotency EPOCH for ONE weighing scope's state transitions, ON DISK.
 *
 * It lives in Room rather than in the process because the exact failure it protects against
 * OUTLIVES the process: a Close whose request reached the server but whose response was lost, on a
 * phone that is then killed. An in-heap epoch is gone by the retry, the retry mints a fresh key,
 * and the backend applies a SECOND close instead of replaying the first.
 *
 * [scopeId] is the transition's subject -- "campaignId:campaignShedId" for a bucket, the campaign
 * id for a whole task -- and is deliberately NOT per-transition-verb: reopen must rotate the epoch
 * the following close will use, or close -> reopen -> close replays the first close's snapshot.
 */
@Entity(tableName = "weighing_transition_epoch")
data class WeighingTransitionEpochEntity(
    @PrimaryKey val scopeId: String,
    val epoch: String,
    val updatedAt: Long,
)

@Dao
interface WeighingTransitionEpochDao {
    @Query("SELECT epoch FROM weighing_transition_epoch WHERE scopeId = :scopeId")
    suspend fun get(scopeId: String): String?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(row: WeighingTransitionEpochEntity)

    /**
     * Claims the epoch for [scopeId], writing [fresh] only when none is stored.
     *
     * IGNORE, not REPLACE: two concurrent attempts on the same scope must agree on ONE key, which
     * is the whole point of the retry being deduplicated. The read afterwards returns the winner.
     */
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertIfAbsent(row: WeighingTransitionEpochEntity)

    /** Bounded on disk: only the most recently used scopes are worth a replay window. */
    @Query(
        "DELETE FROM weighing_transition_epoch WHERE scopeId NOT IN (" +
            "SELECT scopeId FROM weighing_transition_epoch ORDER BY updatedAt DESC LIMIT :keep)",
    )
    suspend fun pruneOutsideNewest(keep: Int)
}
