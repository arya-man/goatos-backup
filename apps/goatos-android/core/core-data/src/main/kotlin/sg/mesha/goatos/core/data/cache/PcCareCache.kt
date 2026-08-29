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
 * Room tables behind the PC Care module (module key pc_care, maintainer decision 2026-08-21) —
 * planner-assigned deworming / ticks removal / hoof trimming / hair trimming tasks with per-animal
 * live-camera video proof and a verifier gate. Offline-first per
 * docs/decisions/android-offline-first.md: Room is the UI's single source of truth, the network
 * refresh upserts here, and pagination binds BOTH layers
 * (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * Same shapes as the Feed Wastage trio in [FeedWastageItemDao] et al:
 *  - `pc_care_task_items` — the paged worklist ROWS, one Room row per task, read back through a
 *    [PagingSource] so the observed query is a bounded ~20-row window.
	 *  - `pc_care_task_remote_keys` — the next page cursor per filter scope, so `RemoteMediator` can
	 *    resume paging after process death without re-walking from zero.
 *  - `pc_care_task_detail_cache` — the JSON-blob-by-taskId detail cache (a re-opened task renders
 *    from here instead of a blank wall).
 *  - `pc_care_animal_rows` — one row per SCANNED ANIMAL in a task: the durable local model behind
 *    the scan screen (local duplicate check, sync status, and the server's per-slot proof state
 *    for peer visibility).
 */

// ---------------------------------------------------------------------------
// PC Care worklist — per-task rows, read as a bounded PagingSource window
// ---------------------------------------------------------------------------

@Entity(
    tableName = "pc_care_task_items",
    primaryKeys = ["queryKey", "grainKey"],
    indices = [
        Index(value = ["queryKey", "sortIndex"]),
        Index(value = ["grainKey"]),
    ],
)
data class PcCareTaskItemEntity(
    val queryKey: String,
    /** The TASK id — the row grain the worklist pages by. */
    val grainKey: String,
    val sortIndex: Int,
    val dtoJson: String,
    val updatedAt: Long,
)

@Entity(tableName = "pc_care_task_remote_keys")
data class PcCareTaskRemoteKeyEntity(
    @PrimaryKey val queryKey: String,
    val nextCursor: String,
    val endReached: Boolean,
    val updatedAt: Long,
)

@Dao
interface PcCareTaskItemDao {
    @Query(
        "SELECT * FROM pc_care_task_items WHERE queryKey = :queryKey " +
            "ORDER BY sortIndex ASC, grainKey ASC",
    )
    fun pagingSource(queryKey: String): PagingSource<Int, PcCareTaskItemEntity>

    /**
     * The MOST RECENTLY cached row for one TASK, across ANY filter scope this app instance has
     * paged. `grainKey` IS the task id, so any matching row is authoritative for the task's live
     * lifecycle status — the same shape as [FeedWastageItemDao.observeRowForPen]: a capture screen
     * left open across a status change (submitted / verified / rejected elsewhere) sees it without
     * a screen re-entry.
     */
    @Query(
        "SELECT * FROM pc_care_task_items WHERE grainKey = :taskId " +
            "ORDER BY updatedAt DESC LIMIT 1",
    )
    fun observeRowForTask(taskId: String): Flow<PcCareTaskItemEntity?>

    /** Every cached copy of one task's row, across scopes — the reconcile target after a submit. */
    @Query("SELECT * FROM pc_care_task_items WHERE grainKey = :taskId")
    suspend fun rowsForTask(taskId: String): List<PcCareTaskItemEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(items: List<PcCareTaskItemEntity>)

    @Query("DELETE FROM pc_care_task_items WHERE queryKey = :queryKey")
    suspend fun deleteQuery(queryKey: String)

    /** Rows already cached for this scope — the monotonic base for the next append page's sortIndex. */
    @Query("SELECT COUNT(*) FROM pc_care_task_items WHERE queryKey = :queryKey")
    suspend fun countForQuery(queryKey: String): Int

    @Query(
        "DELETE FROM pc_care_task_items WHERE queryKey IN " +
            "(SELECT queryKey FROM pc_care_task_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteRowsOutsideNewestQueries(keepQueries: Int)
}

@Dao
interface PcCareTaskRemoteKeyDao {
    @Query("SELECT * FROM pc_care_task_remote_keys WHERE queryKey = :queryKey")
    suspend fun get(queryKey: String): PcCareTaskRemoteKeyEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(key: PcCareTaskRemoteKeyEntity)

    @Query("DELETE FROM pc_care_task_remote_keys WHERE queryKey = :queryKey")
    suspend fun delete(queryKey: String)

    @Query(
        "DELETE FROM pc_care_task_remote_keys WHERE queryKey IN " +
            "(SELECT queryKey FROM pc_care_task_remote_keys ORDER BY updatedAt DESC LIMIT -1 OFFSET :keepQueries)",
    )
    suspend fun deleteOutsideNewestQueries(keepQueries: Int)
}

// ---------------------------------------------------------------------------
// PC Care task detail — JSON blob by task id
// ---------------------------------------------------------------------------

@Entity(tableName = "pc_care_task_detail_cache")
data class PcCareTaskDetailCacheEntity(
    /** The task id. */
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface PcCareTaskDetailCacheDao : JsonBlobCacheDao<PcCareTaskDetailCacheEntity> {
    @Query("SELECT * FROM pc_care_task_detail_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<PcCareTaskDetailCacheEntity?>

    @Query("SELECT * FROM pc_care_task_detail_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): PcCareTaskDetailCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: PcCareTaskDetailCacheEntity)

    @Query("DELETE FROM pc_care_task_detail_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM pc_care_task_detail_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM pc_care_task_detail_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM pc_care_task_detail_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM pc_care_task_detail_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

// ---------------------------------------------------------------------------
// PC Care scanned animals — one durable local row per (task, normalized tag)
// ---------------------------------------------------------------------------

/** Sync states a scanned-animal row can be in. Stored as TEXT (enum-free on disk, like the
 *  outbox's opType) so adding a state never needs a Room migration. */
object PcCareScanStatus {
    /** Queued locally; the PC_CARE_SCAN_ADD outbox row has not reached the server yet. */
    const val PENDING = "PENDING"

    /** The server accepted the scan; [PcCareAnimalRowEntity.animalRowId] carries its server id. */
    const val SYNCED = "SYNCED"

    /** The server refused with 409 duplicate_scan (another phone already scanned this tag). */
    const val DUPLICATE = "DUPLICATE"

    /** The scan write terminally failed for another reason (see the outbox row's lastError). */
    const val FAILED = "FAILED"
}

@Entity(
    tableName = "pc_care_animal_rows",
    primaryKeys = ["taskId", "normalizedTag"],
)
data class PcCareAnimalRowEntity(
    val taskId: String,
    /** trim()+lowercase() of the scanned tag — the LOCAL duplicate-check key. */
    val normalizedTag: String,
    /** The tag exactly as scanned — VERBATIM, never resolved against the herd. */
    val tagVerbatim: String,
    /** Server row id; blank until the scan syncs ([PcCareScanStatus.SYNCED]). */
    val animalRowId: String,
    /** Backend-owned attribution copy ("Scanned by X"); blank for this phone's own pending scan. */
    val scannedByName: String,
    /** One of [PcCareScanStatus]. */
    val scanSyncStatus: String,
    /** The server's per-slot proof state for this animal, as JSON-encoded
     *  `List<PcCareAnimalSlotDto>` — peer visibility ("Captured by X"), written by the captures
     *  poll. Empty string until the first poll. */
    val serverSlotsJson: String,
    val updatedAt: Long,
)

@Dao
interface PcCareAnimalRowDao {
    /**
     * One task's scanned animals, newest first. Bounded ([limit]) — never an unbounded
     * observeAll: a task's scan list is a work surface, and the repository passes a fixed cap.
     */
    @Query(
        "SELECT * FROM pc_care_animal_rows WHERE taskId = :taskId " +
            "ORDER BY updatedAt DESC, normalizedTag ASC LIMIT :limit",
    )
    fun observeAnimals(taskId: String, limit: Int): Flow<List<PcCareAnimalRowEntity>>

    @Query(
        "SELECT * FROM pc_care_animal_rows WHERE taskId = :taskId AND normalizedTag = :normalizedTag",
    )
    suspend fun getByTag(taskId: String, normalizedTag: String): PcCareAnimalRowEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<PcCareAnimalRowEntity>)

    /** Marks this phone's scan accepted: server row id in, status SYNCED. */
    @Query(
        "UPDATE pc_care_animal_rows SET animalRowId = :animalRowId, " +
            "scanSyncStatus = 'SYNCED', updatedAt = :updatedAt " +
            "WHERE taskId = :taskId AND normalizedTag = :normalizedTag",
    )
    suspend fun updateScanSynced(taskId: String, normalizedTag: String, animalRowId: String, updatedAt: Long)

    /** Marks the server's 409 duplicate_scan verdict on the local row (terminal, never retried). */
    @Query(
        "UPDATE pc_care_animal_rows SET scanSyncStatus = 'DUPLICATE', updatedAt = :updatedAt " +
            "WHERE taskId = :taskId AND normalizedTag = :normalizedTag",
    )
    suspend fun markScanDuplicate(taskId: String, normalizedTag: String, updatedAt: Long)

    /** Terminal outbox failure -> FAILED, but only while still PENDING so a DUPLICATE (or an
     *  already-SYNCED replay race) verdict is never overwritten by the generic failure arm. */
    @Query(
        "UPDATE pc_care_animal_rows SET scanSyncStatus = 'FAILED', updatedAt = :updatedAt " +
            "WHERE taskId = :taskId AND normalizedTag = :normalizedTag AND scanSyncStatus = 'PENDING'",
    )
    suspend fun markScanFailedIfPending(taskId: String, normalizedTag: String, updatedAt: Long)

    @Query("DELETE FROM pc_care_animal_rows WHERE taskId = :taskId")
    suspend fun deleteForTask(taskId: String)

    /**
     * Bounded eviction, keyed by TASK: keeps every row of the [keepTasks] most recently touched
     * tasks and drops the rest, so months of task churn cannot grow this table unboundedly —
     * the per-task twin of [PcCareTaskItemDao.deleteRowsOutsideNewestQueries].
     */
    @Query(
        "DELETE FROM pc_care_animal_rows WHERE taskId NOT IN " +
            "(SELECT taskId FROM pc_care_animal_rows GROUP BY taskId " +
            "ORDER BY MAX(updatedAt) DESC LIMIT :keepTasks)",
    )
    suspend fun deleteRowsOutsideNewestTasks(keepTasks: Int)
}
