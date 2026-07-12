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
interface InsightsGapsCacheDao : JsonBlobCacheDao<InsightsGapsCacheEntity> {
    @Query("SELECT * FROM insights_gaps_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<InsightsGapsCacheEntity?>

    @Query("SELECT * FROM insights_gaps_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): InsightsGapsCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: InsightsGapsCacheEntity)

    @Query("DELETE FROM insights_gaps_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM insights_gaps_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM insights_gaps_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM insights_gaps_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM insights_gaps_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}

@Entity(tableName = "insights_coverage_cache")
data class InsightsCoverageCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface InsightsCoverageCacheDao : JsonBlobCacheDao<InsightsCoverageCacheEntity> {
    @Query("SELECT * FROM insights_coverage_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<InsightsCoverageCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: InsightsCoverageCacheEntity)

    @Query("DELETE FROM insights_coverage_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM insights_coverage_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM insights_coverage_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM insights_coverage_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM insights_coverage_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
