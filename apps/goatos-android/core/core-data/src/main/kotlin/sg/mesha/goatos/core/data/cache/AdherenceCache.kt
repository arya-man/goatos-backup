package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/** JSON-blob-by-scope cache row for [sg.mesha.goatos.core.data.AdherenceRepository.adherence]. */
@Entity(tableName = "adherence_cache")
data class AdherenceCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface AdherenceCacheDao {
    @Query("SELECT * FROM adherence_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<AdherenceCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: AdherenceCacheEntity)
}
