package sg.mesha.goatos.core.data

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import sg.mesha.goatos.core.data.cache.readCachedJson
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

    /** Quarantines the singleton row — used when it fails to decode or is past its TTL
     *  (C35-022 / C35-017); the next successful [BootstrapCache.save] repopulates it. */
    @Query("DELETE FROM bootstrap_cache WHERE id = 0")
    suspend fun delete()
}

/**
 * Persists the raw (already @Serializable) bootstrap DTO — the domain models stay
 * Android/serialization-free. Real cache honors ETag/contract revision next.
 *
 * There is only ever one row (`id = 0`), so unlike the per-scope caches in
 * `core/data/cache/` there is no row/byte cap to enforce — only the shared TTL +
 * corrupt-quarantine policy from [readCachedJson] applies.
 */
class BootstrapCache(
    private val dao: BootstrapCacheDao,
    private val clock: () -> Long = { System.currentTimeMillis() },
) {
    private val json = Json { ignoreUnknownKeys = true }

    suspend fun save(dto: BootstrapDto) {
        dao.upsert(BootstrapCacheEntity(id = 0, dtoJson = json.encodeToString(dto), updatedAt = clock()))
    }

    suspend fun load(): BootstrapDto? {
        val entity = dao.get()
        return readCachedJson<BootstrapDto>(
            json = json,
            cacheKey = BOOTSTRAP_CACHE_KEY,
            dtoJson = entity?.dtoJson,
            updatedAt = entity?.updatedAt,
            now = clock(),
            quarantine = { dao.delete() },
        ).data
    }

    private companion object {
        /** Not a real Room key (the row is keyed by `id = 0`) — only used for the
         *  [readCachedJson] log/identity parameter. */
        const val BOOTSTRAP_CACHE_KEY = "bootstrap"
    }
}
