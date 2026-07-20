package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * JSON-blob-by-scope cache row for the vaccination shed-completion summary read
 * (offline-first — docs/decisions/android-offline-first.md). Keyed by the exact task id the
 * operator opened via Scan -> Submit, so a cold start, process death, or offline reopen renders
 * the last-fetched shed acknowledgement summary (shed name, expected/handled/proof-ready counts,
 * vaccine breakdown, submit gate) from Room instead of a blank wall. The submit-readiness summary
 * is genuinely viewable offline, so a network-only `api.xxx()` pass-through is banned here — this
 * is the Room SSOT counterpart the observe() Flow streams from.
 */
@Entity(tableName = "shed_completion_summary_cache")
data class ShedCompletionSummaryCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ShedCompletionSummaryCacheDao : JsonBlobCacheDao<ShedCompletionSummaryCacheEntity> {
    @Query("SELECT * FROM shed_completion_summary_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ShedCompletionSummaryCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ShedCompletionSummaryCacheEntity)

    @Query("DELETE FROM shed_completion_summary_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM shed_completion_summary_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM shed_completion_summary_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM shed_completion_summary_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM shed_completion_summary_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
