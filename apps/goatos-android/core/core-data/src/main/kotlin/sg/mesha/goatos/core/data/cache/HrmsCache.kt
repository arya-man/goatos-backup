package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * HRMS enquiries + violations JSON-blob cache (maintainer decisions 2026-09-30; offline-first per
 * docs/decisions/android-offline-first.md). ONE blob table for the park head's three read shapes:
 *
 *  - `enquiries:open` — the first page of the park head's open enquiries (the For me cards).
 *  - `enquiry:<id>` — one enquiry's detail (questions, types, the park's people, the report).
 *  - `violations` — the first page of this month's violations plus the record form's options.
 *
 * Principal-scoped keys are safe because the DB is wiped on logout. Bounded by the shared
 * [enforceCacheBounds] LRU governance like every other blob cache.
 */
@Entity(tableName = "hrms_blob_cache")
data class HrmsBlobCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface HrmsBlobCacheDao : JsonBlobCacheDao<HrmsBlobCacheEntity> {
    @Query("SELECT * FROM hrms_blob_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<HrmsBlobCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: HrmsBlobCacheEntity)

    @Query("DELETE FROM hrms_blob_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM hrms_blob_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM hrms_blob_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM hrms_blob_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM hrms_blob_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
