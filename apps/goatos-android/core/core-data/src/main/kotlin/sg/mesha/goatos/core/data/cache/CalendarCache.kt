package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/** JSON-blob-by-scope cache row for [sg.mesha.goatos.core.data.CalendarRepository.events]. */
@Entity(tableName = "calendar_cache")
data class CalendarCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface CalendarCacheDao : JsonBlobCacheDao<CalendarCacheEntity> {
    @Query("SELECT * FROM calendar_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<CalendarCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: CalendarCacheEntity)

    @Query("DELETE FROM calendar_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM calendar_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM calendar_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM calendar_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM calendar_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
