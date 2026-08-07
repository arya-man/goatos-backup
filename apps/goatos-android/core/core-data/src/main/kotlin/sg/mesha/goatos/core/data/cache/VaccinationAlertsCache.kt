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
 * [sg.mesha.goatos.core.data.VaccinationAlertsRepository.observeAlerts].
 *
 * The vaccination twin of [WeighingAlertsCacheEntity]. Room is the UI's single source of truth
 * for this feed like every other screen-facing read (docs/decisions/android-offline-first.md),
 * and it matters more here than most: an alert is the message telling an operator that a proof
 * came back for rework, and the shed where they read it is exactly where the network is worst.
 *
 * FIRST PAGE ONLY. This caches the newest page, which is what the screen opens on; paging deeper
 * is a live read and is not persisted. Alerts are recent-by-definition (the backend bounds the
 * feed to a rolling 30-day window), so there is no long tail worth storing on a phone.
 */
@Entity(tableName = "vaccination_alerts_cache")
data class VaccinationAlertsCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface VaccinationAlertsCacheDao : JsonBlobCacheDao<VaccinationAlertsCacheEntity> {
    @Query("SELECT * FROM vaccination_alerts_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<VaccinationAlertsCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: VaccinationAlertsCacheEntity)

    @Query("DELETE FROM vaccination_alerts_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM vaccination_alerts_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM vaccination_alerts_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM vaccination_alerts_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM vaccination_alerts_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
