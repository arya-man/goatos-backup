package sg.mesha.goatos.core.data

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.network.BootstrapDto

/** Single-row cache of the last bootstrap (offline-first boot). */
@Entity(tableName = "bootstrap_cache")
data class BootstrapCacheEntity(
    @PrimaryKey val id: Int = 0,
    val dtoJson: String,
    val updatedAt: Long,
)

@Dao
interface BootstrapCacheDao {
    @Query("SELECT * FROM bootstrap_cache WHERE id = 0")
    suspend fun get(): BootstrapCacheEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: BootstrapCacheEntity)
}

/**
 * Persists the raw (already @Serializable) bootstrap DTO — the domain models stay
 * Android/serialization-free. Real cache honors ETag/contract revision next.
 */
class BootstrapCache(
    private val dao: BootstrapCacheDao,
    private val clock: () -> Long = { System.currentTimeMillis() },
) {
    private val json = Json { ignoreUnknownKeys = true }

    suspend fun save(dto: BootstrapDto) {
        dao.upsert(BootstrapCacheEntity(id = 0, dtoJson = json.encodeToString(dto), updatedAt = clock()))
    }

    suspend fun load(): BootstrapDto? =
        dao.get()?.let { runCatching { json.decodeFromString<BootstrapDto>(it.dtoJson) }.getOrNull() }
}
