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
        androidx.room.Index(value = ["shedId"]),
        androidx.room.Index(value = ["shedId", "primaryTag"]),
        androidx.room.Index(value = ["shedId", "secondaryTag"]),
    ],
)
data class ScanRosterRowEntity(
    @PrimaryKey val id: String, // "{shedId}#{obligationId}" or "{shedId}#{goatId}#{primaryTag}"
    val shedId: String,
    val goatId: String,
    val primaryTag: String,
    val secondaryTag: String?,
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

    /** Find a roster row by shed and normalized tag (primary or secondary). Returns the first match
     *  or null if no goat in this shed has the tag. Normalized tag comparison (alphanumeric lowercase). */
    @Query(
        "SELECT * FROM scan_roster_row WHERE shedId = :shedId AND " +
            "(LOWER(REPLACE(REPLACE(REPLACE(primaryTag, '-', ''), ' ', ''), '_', '')) = :normalizedTag OR " +
            "LOWER(REPLACE(REPLACE(REPLACE(secondaryTag, '-', ''), ' ', ''), '_', '')) = :normalizedTag) " +
            "LIMIT 1"
    )
    suspend fun findByTag(shedId: String, normalizedTag: String): ScanRosterRowEntity?

    /** Count rows by status for a shed. Used to derive total/done/pending counts without reloading
     *  the entire JSON blob. (R50-008: aggregates independent of loaded page size). */
    @Query("SELECT status, COUNT(*) as count FROM scan_roster_row WHERE shedId = :shedId GROUP BY status")
    suspend fun countByStatus(shedId: String): List<StatusCount>

    /** R50-008: Observable status aggregates for a shed — a bounded GROUP BY (max a handful of
     *  status rows), re-emitted whenever the roster rows change, so ring/tile counters stay
     *  page-independent. */
    @Query("SELECT status, COUNT(*) as count FROM scan_roster_row WHERE shedId = :shedId GROUP BY status")
    fun observeCountsByStatus(shedId: String): Flow<List<StatusCount>>

    /** R50-008: Status aggregates for a bounded id set (the session's local unsynced DONE overlay,
     *  at most a scan session's worth of ids). Lets the ViewModel add local edits to the full-roster
     *  counts without double-counting rows the backend already reports DONE. */
    @Query(
        "SELECT status, COUNT(*) as count FROM scan_roster_row " +
            "WHERE shedId = :shedId AND obligationId IN (:obligationIds) GROUP BY status"
    )
    suspend fun countByStatusForObligations(shedId: String, obligationIds: List<String>): List<StatusCount>

    @Query("DELETE FROM scan_roster_row WHERE shedId = :shedId")
    suspend fun deleteForShed(shedId: String)

    @Query("DELETE FROM scan_roster_row")
    suspend fun deleteAll()
}

/** Helper data class for status-based aggregation (R50-008). */
data class StatusCount(
    val status: String,
    val count: Int,
)
