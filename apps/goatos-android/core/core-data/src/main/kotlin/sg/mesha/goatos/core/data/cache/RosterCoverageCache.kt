package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Principal-scoped cached coverage status (docs/decisions/android-offline-first.md).
 * Persists `GET /app/roster/my-coverage` response so the coverage banner survives
 * process death and network failures, and distinguishes "no coverage" from
 * "offline/unknown coverage".
 */
@Entity(tableName = "roster_coverage_cache")
data class RosterCoverageCacheEntity(
    @PrimaryKey val cacheKey: String = "coverage",  // singleton principal scope
    val dtoJson: String,                              // serialized MyCoverageResponseDto
    val updatedAt: Long,                              // when last synced
)

@Dao
interface RosterCoverageCacheDao {
    @Query("SELECT * FROM roster_coverage_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String = "coverage"): Flow<RosterCoverageCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: RosterCoverageCacheEntity)

    @Query("DELETE FROM roster_coverage_cache WHERE cacheKey = :cacheKey")
    suspend fun delete(cacheKey: String = "coverage")
}
