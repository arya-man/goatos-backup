package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Clock module JSON-blob cache (module clock, maintainer decision 2026-08-27 —
 * docs/features/clock-in-out/plan.md, docs/decisions/android-offline-first.md). ONE blob table
 * for the module's three read shapes, keyed by scope:
 *
 *  - `status` — the caller's own `GET /app/clock/status` (drives the My Clock screen AND the
 *    shell-global not-clocked-in reminder banner). Principal-scoped singleton key, exactly like
 *    [RosterCoverageCacheEntity] (the DB is wiped on logout, so one row per install is one row
 *    per signed-in principal).
 *  - `presence:<filterKey>` — the FIRST page of the presence board for the currently viewed
 *    date/filter combination, the offline fallback so leadership re-entering the Team page sees
 *    their cached board instead of a blank wall.
 *  - `person:<memberId>:<date>` — one person-day detail blob.
 *
 * Bounded by the shared [enforceCacheBounds] LRU governance like every other blob cache here.
 */
@Entity(tableName = "clock_blob_cache")
data class ClockBlobCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface ClockBlobCacheDao : JsonBlobCacheDao<ClockBlobCacheEntity> {
    @Query("SELECT * FROM clock_blob_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<ClockBlobCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: ClockBlobCacheEntity)

    @Query("DELETE FROM clock_blob_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM clock_blob_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM clock_blob_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM clock_blob_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM clock_blob_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
