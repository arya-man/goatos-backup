package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * JSON-blob-by-scope cache rows for [sg.mesha.goatos.core.data.ExecutionRepository]'s three
 * reads: the execution row list, the per-shed drilldown, and the per-animal scan roster.
 */
@Entity(tableName = "execution_rows_cache")
data class ExecutionRowsCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ExecutionRowsCacheDao : JsonBlobCacheDao<ExecutionRowsCacheEntity> {
    @Query("SELECT * FROM execution_rows_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ExecutionRowsCacheEntity?>

    @Query("SELECT * FROM execution_rows_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): ExecutionRowsCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ExecutionRowsCacheEntity)

    @Query("DELETE FROM execution_rows_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM execution_rows_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM execution_rows_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM execution_rows_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM execution_rows_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)

    /** Drops every cached execution-row blob. Used by the app-version cache gate so an in-place
     *  update discards read blobs written before a serving-shape change (e.g. a drive-date move),
     *  forcing a clean refetch. Never touches the outbox or any write-side table. */
    @Query("DELETE FROM execution_rows_cache")
    suspend fun clearAll()
}

@Entity(tableName = "execution_shed_cache")
data class ExecutionShedCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ExecutionShedCacheDao : JsonBlobCacheDao<ExecutionShedCacheEntity> {
    @Query("SELECT * FROM execution_shed_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ExecutionShedCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ExecutionShedCacheEntity)

    @Query("DELETE FROM execution_shed_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM execution_shed_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM execution_shed_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM execution_shed_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM execution_shed_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)

    /** Drops every cached shed-drilldown blob. See [ExecutionRowsCacheDao.clearAll]. */
    @Query("DELETE FROM execution_shed_cache")
    suspend fun clearAll()
}

/** Individual scan roster row — the per-animal SSOT for the shed scan screen. Room is the single
 *  source of truth for the whole roster (docs/decisions/android-offline-first.md +
 *  docs/decisions/mobile-data-fetch-anti-patterns.md "render list UIs from a bounded SSOT, never a
 *  whole-collection JSON blob"). Every consumer reads this table, never a serialized whole-roster
 *  blob:
 *   - the scan LIST renders a bounded keyset window ([observeRowsWindow]) advanced by scroll;
 *   - RFID tag validation matches the FULL roster via the indexed [findByTag] (R50-007);
 *   - ring/tile counters are full-roster GROUP BY aggregates ([observeCountsByStatus], R50-008);
 *   - the submit proof gate resolves the FULL set of DONE animals ([observeDoneGoatIds]) and the
 *     rows for any proof-incomplete animals ([rowsByGoatIds]), independent of the visible window.
 *  `seq` is the backend roster order captured at refresh time so the windowed read is stable and
 *  page N shows the same animals the backend would. Indexed on scopeKey (+ seq for the window,
 *  + normalized tags for lookup). */
@Entity(
    tableName = "scan_roster_row",
    indices = [
        androidx.room.Index(value = ["scopeKey"]),
        androidx.room.Index(value = ["scopeKey", "seq"]),
        androidx.room.Index(value = ["scopeKey", "normalizedPrimaryTag"]),
        androidx.room.Index(value = ["scopeKey", "normalizedSecondaryTag"]),
    ],
)
data class ScanRosterRowEntity(
    @PrimaryKey val id: String,
    val scopeKey: String,
    val shedId: String,
    val taskId: String,
    val goatId: String,
    val primaryTag: String,
    val secondaryTag: String?,
    val normalizedPrimaryTag: String,
    val normalizedSecondaryTag: String?,
    val vaccineLabel: String,
    val status: String,
    val scannedAtMs: Long? = null,
    val obligationId: String,
    /** Backend roster order captured at refresh, so the windowed UI read is stable and page-N
     *  matches backend order. Defaults to 0 for rows written before the ordering column existed. */
    val seq: Long = 0,
    val updatedAt: Long,
)

@Dao
interface ScanRosterRowDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ScanRosterRowEntity)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(entities: List<ScanRosterRowEntity>)

    /** The UI list read: a BOUNDED keyset window over the per-row SSOT ordered by backend roster
     *  position, advanced by scroll (never the whole collection). Re-emits on every roster upsert.
     *  This is the offline-first SSOT read that replaces the whole-roster JSON blob. */
    @Query("SELECT * FROM scan_roster_row WHERE scopeKey = :scopeKey ORDER BY seq ASC, id ASC LIMIT :limit")
    fun observeRowsWindow(scopeKey: String, limit: Int): Flow<List<ScanRosterRowEntity>>

    /** Full-roster row count for this scope — drives `hasMore` (window < total) without loading rows. */
    @Query("SELECT COUNT(*) FROM scan_roster_row WHERE scopeKey = :scopeKey")
    fun observeScopeTotal(scopeKey: String): Flow<Int>

    /** Distinct goat ids of every DONE/completed animal in the FULL roster (backend-persisted status).
     *  The submit proof gate unions this with the session's local-done overlay to require a synced
     *  proof for every vaccinated animal, page-independent. Bounded by one shed's animal count. */
    // An OUTSTANDING server status (rejected/due/pending) vetoes the timestamp. A sent-back animal
    // keeps its scannedAtMs forever -- it really was scanned -- so OR-ing the timestamp in without
    // that veto kept a rejected animal in the done set, feeding the per-goat proof gate and letting
    // a reopened animal count as already proven.
    @Query(
        "SELECT DISTINCT goatId FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId != '' AND " +
            "LOWER(TRIM(status)) NOT IN ('rejected', 'due', 'pending') AND " +
            "(scannedAtMs IS NOT NULL OR LOWER(status) LIKE '%done%' OR LOWER(status) LIKE '%complete%')"
    )
    fun observeDoneGoatIds(scopeKey: String): Flow<List<String>>

    /** Rows for a bounded goat-id set (the proof-incomplete animals the submit gate surfaces for
     *  retry/replace), so action-needed animals OUTSIDE the visible window can still be shown. The
     *  result is bounded by the passed id set; the caller sorts for display. */
    @Query("SELECT * FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId IN (:goatIds)")
    suspend fun rowsByGoatIds(scopeKey: String, goatIds: List<String>): List<ScanRosterRowEntity>

    /** Exact lookup over the canonical tag persisted at refresh time. */
    @Query(
        "SELECT * FROM scan_roster_row WHERE scopeKey = :scopeKey AND " +
            "(normalizedPrimaryTag = :normalizedTag OR normalizedSecondaryTag = :normalizedTag) " +
            "LIMIT 1"
    )
    suspend fun findByTag(scopeKey: String, normalizedTag: String): ScanRosterRowEntity?

    /** Count rows by status for a shed. Used to derive total/done/pending counts without reloading
     *  the entire JSON blob. (R50-008: aggregates independent of loaded page size). */
    @Query("SELECT status, COUNT(*) as count FROM scan_roster_row WHERE scopeKey = :scopeKey GROUP BY status")
    suspend fun countByStatus(scopeKey: String): List<StatusCount>

    /** R50-008: Observable status aggregates for a shed — a bounded GROUP BY (max a handful of
     *  status rows), re-emitted whenever the roster rows change, so ring/tile counters stay
     *  page-independent. */
    @Query("SELECT status, COUNT(*) as count FROM scan_roster_row WHERE scopeKey = :scopeKey GROUP BY status")
    fun observeCountsByStatus(scopeKey: String): Flow<List<StatusCount>>

    /** R50-008: Status aggregates for a bounded id set (the session's local unsynced DONE overlay,
     *  at most a scan session's worth of ids). Lets the ViewModel add local edits to the full-roster
     *  counts without double-counting rows the backend already reports DONE. */
    @Query(
        "SELECT status, COUNT(*) as count FROM scan_roster_row " +
            "WHERE scopeKey = :scopeKey AND obligationId IN (:obligationIds) GROUP BY status"
    )
    suspend fun countByStatusForObligations(scopeKey: String, obligationIds: List<String>): List<StatusCount>

    @Query("DELETE FROM scan_roster_row WHERE scopeKey = :scopeKey")
    suspend fun deleteForScope(scopeKey: String)

    @androidx.room.Transaction
    suspend fun replaceScope(scopeKey: String, entities: List<ScanRosterRowEntity>) {
        deleteForScope(scopeKey)
        upsertAll(entities)
    }

    @Query("DELETE FROM scan_roster_row")
    suspend fun deleteAll()
}

/** Helper data class for status-based aggregation (R50-008). */
data class StatusCount(
    val status: String,
    val count: Int,
)
