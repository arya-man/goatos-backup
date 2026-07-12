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
interface ControlTowerCacheDao : JsonBlobCacheDao<ControlTowerCacheEntity> {
    @Query("SELECT * FROM control_tower_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ControlTowerCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ControlTowerCacheEntity)

    @Query("DELETE FROM control_tower_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM control_tower_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM control_tower_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM control_tower_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM control_tower_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
