package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * JSON-blob-by-scope cache row for
 * [sg.mesha.goatos.core.data.WeighingAlertsRepository.observeAlerts].
 *
 * Room is the UI's single source of truth for the weighing alerts feed, same as every other
 * screen-facing read (docs/decisions/android-offline-first.md). It matters more here than most:
 * an alert is the message telling an operator that work was assigned to them or that a proof
 * came back for rework, and the shed where they read it is exactly where the network is worst.
 * Cached alerts must survive process death and a dead network, so the operator opens the tab in
 * the shed and still sees what they were told.
 *
 * FIRST PAGE ONLY. This caches the newest page of the feed, which is what the screen opens on;
 * paging deeper is a live read and is not persisted. Alerts are recent-by-definition (the
 * backend bounds the feed to a rolling window), so there is no long tail worth storing on a
 * phone.
 */
@Entity(tableName = "weighing_alerts_cache")
data class WeighingAlertsCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface WeighingAlertsCacheDao : JsonBlobCacheDao<WeighingAlertsCacheEntity> {
    @Query("SELECT * FROM weighing_alerts_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<WeighingAlertsCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: WeighingAlertsCacheEntity)

    @Query("DELETE FROM weighing_alerts_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM weighing_alerts_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM weighing_alerts_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM weighing_alerts_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM weighing_alerts_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
