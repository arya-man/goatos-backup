package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * JSON-blob-by-scope cache row for [sg.mesha.goatos.core.data.TasksRepository]'s task-detail
 * read (offline-first — docs/decisions/android-offline-first.md, MOB-001). Keyed by the exact
 * task id the operator opened via the Scan -> Submit drill, so a cold start, process death, or
 * offline reopen renders the last-fetched task + its SOP form instead of a blank/error wall —
 * a network-only `api.xxx()` pass-through with no Room persistence is banned for screen-facing
 * reads.
 */
@Entity(tableName = "task_detail_cache")
data class TaskDetailCacheEntity(
    @PrimaryKey val cacheKey: String,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface TaskDetailCacheDao : JsonBlobCacheDao<TaskDetailCacheEntity> {
    @Query("SELECT * FROM task_detail_cache WHERE cacheKey = :cacheKey")
    override fun observe(cacheKey: String): Flow<TaskDetailCacheEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    override suspend fun upsert(entity: TaskDetailCacheEntity)

    @Query("DELETE FROM task_detail_cache WHERE cacheKey = :cacheKey")
    override suspend fun delete(cacheKey: String)

    @Query("SELECT COUNT(*) FROM task_detail_cache")
    override suspend fun count(): Int

    @Query("SELECT COALESCE(SUM(LENGTH(dtoJson)), 0) FROM task_detail_cache")
    override suspend fun totalBytes(): Long

    @Query(
        "DELETE FROM task_detail_cache WHERE cacheKey IN " +
            "(SELECT cacheKey FROM task_detail_cache ORDER BY updatedAt ASC LIMIT :n)",
    )
    override suspend fun deleteOldest(n: Int)
}
