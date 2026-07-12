package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * JSON-blob-by-scope cache row for [sg.mesha.goatos.core.data.VerificationRepository]'s
 * queue read (the standalone Verifier section's category-filtered media queue —
 * context/architecture/verifier-app-and-flow.md). Same shape/governance as
 * [ExecutionRowsCacheEntity] et al. (see [CacheGovernance]/[enforceCacheBounds]):
 * cacheKey scopes by category (`cacheKey("verify-queue", category)`), the JSON blob is the
 * whole [sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto] page window for that
 * scope, and the DAO is bounded (TTL + row/byte cap), never `SELECT *`/unbounded.
 */
@Entity(tableName = "verification_queue_cache")
data class VerificationQueueCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface VerificationQueueCacheDao : JsonBlobCacheDao<VerificationQueueCacheEntity> {
    @Query("SELECT * FROM verification_queue_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<VerificationQueueCacheEntity?>

    @Query("SELECT * FROM verification_queue_cache WHERE cacheKey = :cacheKey")
    suspend fun get(cacheKey: String): VerificationQueueCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: VerificationQueueCacheEntity)

    @Query("DELETE FROM verification_queue_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM verification_queue_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM verification_queue_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM verification_queue_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM verification_queue_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
