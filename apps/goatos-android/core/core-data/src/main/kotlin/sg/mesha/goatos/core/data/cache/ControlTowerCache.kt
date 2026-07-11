package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/** JSON-blob-by-scope cache row for [sg.mesha.goatos.core.data.ControlTowerRepository.summary]. */
@Entity(tableName = "control_tower_cache")
data class ControlTowerCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ControlTowerCacheDao {
    @Query("SELECT * FROM control_tower_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<ControlTowerCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: ControlTowerCacheEntity)
}
