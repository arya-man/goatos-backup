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
