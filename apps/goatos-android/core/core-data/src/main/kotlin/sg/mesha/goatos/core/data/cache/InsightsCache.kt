package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * JSON-blob-by-scope cache rows for [sg.mesha.goatos.core.data.VaccinationInsightsRepository]'s
 * two leadership drill overlays: data gaps and per-vaccine coverage.
 */
@Entity(tableName = "insights_gaps_cache")
data class InsightsGapsCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface InsightsGapsCacheDao {
    @Query("SELECT * FROM insights_gaps_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<InsightsGapsCacheEntity?>

    @Query("SELECT * FROM insights_gaps_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): InsightsGapsCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: InsightsGapsCacheEntity)
}

@Entity(tableName = "insights_coverage_cache")
data class InsightsCoverageCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface InsightsCoverageCacheDao {
    @Query("SELECT * FROM insights_coverage_cache WHERE cacheKey = :cacheKey")
    fun observe(cacheKey: String): Flow<InsightsCoverageCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: InsightsCoverageCacheEntity)
}
