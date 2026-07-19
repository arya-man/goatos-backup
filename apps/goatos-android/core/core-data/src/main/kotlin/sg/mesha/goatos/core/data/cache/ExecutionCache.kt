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
}

@Entity(tableName = "scan_roster_cache")
data class ScanRosterCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ScanRosterCacheDao : JsonBlobCacheDao<ScanRosterCacheEntity> {
    @Query("SELECT * FROM scan_roster_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ScanRosterCacheEntity?>

    @Query("SELECT * FROM scan_roster_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): ScanRosterCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ScanRosterCacheEntity)

    @Query("DELETE FROM scan_roster_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM scan_roster_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM scan_roster_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM scan_roster_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM scan_roster_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

/** Individual scan roster row for tag-based lookup (R50-007: RFID tag matching against the full shed
 *  roster, not just the loaded page). Indexed on shed_id + primary_tag + secondary_tag for efficient
 *  keyset searches. Per offline-first rules, Room is the single source of truth for the roster. */
@Entity(
    tableName = "scan_roster_row",
    indices = [
        androidx.room.Index(value = ["scopeKey"]),
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
    val obligationId: String,
    val updatedAt: Long,
)

@Dao
interface ScanRosterRowDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ScanRosterRowEntity)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(entities: List<ScanRosterRowEntity>)

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
