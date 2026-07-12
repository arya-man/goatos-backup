package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Principal-scoped cached timetable for the operator's center
 * (docs/decisions/android-offline-first.md). Persists `GET /app/roster/timetable`
 * response so the timetable screen survives process death and network failures.
 */
@Entity(tableName = "roster_timetable_cache")
data class RosterTimetableCacheEntity(
    @PrimaryKey val cacheKey: String,  // centerId or "no_center"
    val dtoJson: String,                // serialized EnrichedPositionListResponseDto
    val updatedAt: Long,                // when last synced
)

@Dao
interface RosterTimetableCacheDao {
    @Query("SELECT * FROM roster_timetable_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<RosterTimetableCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: RosterTimetableCacheEntity)

    @Query("DELETE FROM roster_timetable_cache WHERE cacheKey = :cacheKey")
    suspend fun delete(cacheKey: String)
}
