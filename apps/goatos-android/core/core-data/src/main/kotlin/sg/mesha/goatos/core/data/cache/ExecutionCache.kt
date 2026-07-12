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
interface ExecutionRowsCacheDao {
    @Query("SELECT * FROM execution_rows_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<ExecutionRowsCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ExecutionRowsCacheEntity)
}

@Entity(tableName = "execution_shed_cache")
data class ExecutionShedCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ExecutionShedCacheDao {
    @Query("SELECT * FROM execution_shed_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<ExecutionShedCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ExecutionShedCacheEntity)
}

@Entity(tableName = "scan_roster_cache")
data class ScanRosterCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ScanRosterCacheDao {
    @Query("SELECT * FROM scan_roster_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<ScanRosterCacheEntity?>

    @Query("SELECT * FROM scan_roster_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): ScanRosterCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ScanRosterCacheEntity)
}
