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
    /** `obligation_instances.row_version` for this row's obligation, echoed from
     *  [sg.mesha.goatos.core.network.dto.ScanRosterRowDto.obligationRowVersion]. See
     *  `sg.mesha.goatos.core.data.capture.scanCaptureIdempotencyKey` — this is the server-issued
     *  cycle discriminator folded into the scan-capture idempotency key so a genuinely-new scan
     *  after a verifier-rejection reopen is not deduped away as a replay of the prior cycle's
     *  already-synced capture. Defaults to 0 for rows written before this column existed. */
    val obligationRowVersion: Int = 0,
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

    /** Full-roster animal count for this scope — one animal may carry several vaccine obligations. */
    @Query("SELECT COUNT(DISTINCT goatId) FROM scan_roster_row WHERE scopeKey = :scopeKey")
    fun observeScopeTotal(scopeKey: String): Flow<Int>

    /** Distinct goat ids of every DONE/completed animal in the FULL roster (backend-persisted status).
     *  The submit proof gate unions this with the session's local-done overlay to require a synced
     *  proof for every vaccinated animal, page-independent. Bounded by one shed's animal count. */
    // An OUTSTANDING server status (rejected/due/pending/in_progress) vetoes the timestamp. A sent-back animal
    // keeps its scannedAtMs forever -- it really was scanned -- so OR-ing the timestamp in without
    // that veto kept a rejected animal in the done set, feeding the per-goat proof gate and letting
    // a reopened animal count as already proven.
    @Query(
        "SELECT goatId FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId != '' GROUP BY goatId HAVING " +
            "SUM(CASE WHEN LOWER(TRIM(status)) IN ('rejected', 'due', 'pending', 'in_progress') THEN 1 ELSE 0 END) = 0 AND " +
            "SUM(CASE WHEN scannedAtMs IS NOT NULL OR LOWER(status) LIKE '%done%' OR LOWER(status) LIKE '%complete%' THEN 1 ELSE 0 END) > 0"
    )
    fun observeDoneGoatIds(scopeKey: String): Flow<List<String>>

    /** Rows for a bounded goat-id set (the proof-incomplete animals the submit gate surfaces for
     *  retry/replace), so action-needed animals OUTSIDE the visible window can still be shown. The
     *  result is bounded by the passed id set; the caller sorts for display. */
    @Query("SELECT * FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId IN (:goatIds)")
    suspend fun rowsByGoatIds(scopeKey: String, goatIds: List<String>): List<ScanRosterRowEntity>

    /** Exact lookup over the canonical tag persisted at refresh time.
     *  ORDER BY: PENDING obligations first (outstanding status), then others.
     *  This ensures sibling rows (same goat, different obligations) favor the due/open obligation. */
    @Query(
        "SELECT * FROM scan_roster_row WHERE scopeKey = :scopeKey AND " +
            "(normalizedPrimaryTag = :normalizedTag OR normalizedSecondaryTag = :normalizedTag) " +
            "ORDER BY CASE WHEN status IN ('pending', 'due') THEN 0 ELSE 1 END, rowId ASC " +
            "LIMIT 1"
    )
    suspend fun findByTag(scopeKey: String, normalizedTag: String): ScanRosterRowEntity?

    /** Count one effective status per animal for a shed. A multi-vaccine animal can have several
     *  roster rows; any outstanding sibling obligation keeps the animal open unless the goat-level
     *  proof/completion has satisfied every sibling row. */
    @Query(
        "WITH per_goat AS (" +
            "SELECT goatId, " +
            "CASE " +
            "WHEN SUM(CASE WHEN LOWER(TRIM(status)) IN ('rejected', 'due', 'pending', 'in_progress') THEN 1 ELSE 0 END) > 0 THEN 'due' " +
            "WHEN SUM(CASE WHEN LOWER(status) LIKE '%skip%' THEN 1 ELSE 0 END) > 0 THEN 'skipped' " +
            "WHEN SUM(CASE WHEN scannedAtMs IS NOT NULL OR LOWER(status) LIKE '%done%' OR LOWER(status) LIKE '%complete%' THEN 1 ELSE 0 END) > 0 THEN 'done' " +
            "ELSE 'due' END AS effectiveStatus " +
            "FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId != '' GROUP BY goatId" +
            ") SELECT effectiveStatus AS status, COUNT(*) as count FROM per_goat GROUP BY effectiveStatus"
    )
    suspend fun countByStatus(scopeKey: String): List<StatusCount>

    /** R50-008: Observable animal-grain status aggregates for a shed — a bounded GROUP BY (max a
     *  handful of status rows), re-emitted whenever roster rows change, so ring/tile counters stay
     *  page-independent and multi-vaccine rows do not double-count a goat. */
    @Query(
        "WITH per_goat AS (" +
            "SELECT goatId, " +
            "CASE " +
            "WHEN SUM(CASE WHEN LOWER(TRIM(status)) IN ('rejected', 'due', 'pending', 'in_progress') THEN 1 ELSE 0 END) > 0 THEN 'due' " +
            "WHEN SUM(CASE WHEN LOWER(status) LIKE '%skip%' THEN 1 ELSE 0 END) > 0 THEN 'skipped' " +
            "WHEN SUM(CASE WHEN scannedAtMs IS NOT NULL OR LOWER(status) LIKE '%done%' OR LOWER(status) LIKE '%complete%' THEN 1 ELSE 0 END) > 0 THEN 'done' " +
            "ELSE 'due' END AS effectiveStatus " +
            "FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId != '' GROUP BY goatId" +
            ") SELECT effectiveStatus AS status, COUNT(*) as count FROM per_goat GROUP BY effectiveStatus"
    )
    fun observeCountsByStatus(scopeKey: String): Flow<List<StatusCount>>

    /** R50-008: Effective status aggregates for a bounded goat-id set (the session's local unsynced
     *  DONE overlay, at most a scan session's worth of ids). Lets the ViewModel add local edits to
     *  the full-roster counts without double-counting goats the backend already reports DONE. */
    @Query(
        "WITH per_goat AS (" +
            "SELECT goatId, " +
            "CASE " +
            "WHEN SUM(CASE WHEN LOWER(TRIM(status)) IN ('rejected', 'due', 'pending', 'in_progress') THEN 1 ELSE 0 END) > 0 THEN 'due' " +
            "WHEN SUM(CASE WHEN LOWER(status) LIKE '%skip%' THEN 1 ELSE 0 END) > 0 THEN 'skipped' " +
            "WHEN SUM(CASE WHEN scannedAtMs IS NOT NULL OR LOWER(status) LIKE '%done%' OR LOWER(status) LIKE '%complete%' THEN 1 ELSE 0 END) > 0 THEN 'done' " +
            "ELSE 'due' END AS effectiveStatus " +
            "FROM scan_roster_row WHERE scopeKey = :scopeKey AND goatId IN (:goatIds) GROUP BY goatId" +
            ") SELECT effectiveStatus AS status, COUNT(*) as count FROM per_goat GROUP BY effectiveStatus"
    )
    suspend fun countByStatusForGoats(scopeKey: String, goatIds: List<String>): List<StatusCount>

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
