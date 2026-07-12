package sg.mesha.goatos.core.data.cache

import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json

/**
 * Shared governance policy for every JSON-blob-by-scope cache table (Calendar / Control Tower /
 * Execution rows-shed-scanRoster / Adherence / Insights gaps-coverage / Bootstrap). Lives here
 * (not per-repo) so a policy change — TTL window, row/byte budget — is one edit, not eight
 * (C35-017: these caches previously had no TTL, no cap, and no eviction at all).
 */
object CacheGovernance {
    /**
     * Hard cutoff beyond which a row is treated as a cache MISS (and quarantined) even though
     * it still decodes cleanly. Stale-while-revalidate (docs/decisions/android-offline-first.md)
     * only covers the window inside this TTL — nothing is served indefinitely stale. A week is a
     * generous upper bound for PHC vaccination read models: every screen refreshes far more
     * often than this in normal use, so the TTL only bites when a device has been offline (or a
     * scope simply unvisited) for an unusually long stretch.
     */
    const val DEFAULT_TTL_MILLIS: Long = 7L * 24 * 60 * 60 * 1000

    /**
     * Bounded row cap per table. [cacheKey] notes a screen only ever visits "the handful of
     * filter combinations its UI actually offers" — 200 rows is generous headroom for that while
     * still bounding worst-case growth from cursor/filter churn across a long-lived install.
     */
    const val DEFAULT_MAX_ROWS: Int = 200

    /**
     * Soft per-table byte budget (approximate: SQLite `LENGTH()` on a TEXT column counts
     * characters, not raw UTF-8 bytes — close enough for a safety-valve budget, not exact
     * metering). Guards against a small number of unusually large payloads blowing past the row
     * cap's assumption that every row is roughly page-sized.
     */
    const val DEFAULT_MAX_BYTES: Long = 5L * 1024 * 1024
}

/** Outcome of reading one cached JSON blob through [readCachedJson]. */
data class CachedRead<out T>(
    val data: T?,
    val updatedAt: Long?,
    /**
     * A row EXISTED but was deleted (quarantined) because it failed to decode or was past its
     * hard TTL. Distinguishes "was corrupted/expired — will repopulate on the next successful
     * refresh" from a genuine cold-cache miss, so callers never mistake transient corruption for
     * "never synced".
     */
    val wasQuarantined: Boolean,
)

/**
 * Single shared read path for every JSON-blob-by-scope cache row.
 *
 * Two failure modes are handled the same way — DELETE the row instead of leaving it in place:
 *  - **corrupt** ([dtoJson] fails to decode as [T]: bit rot, cross-version schema drift after an
 *    app downgrade, a partial write). Before this fix ([AdherenceRepository], [CalendarRepository],
 *    [ControlTowerRepository], [ExecutionRepository], [VaccinationInsightsRepository], and
 *    `BootstrapCache` all shared this shape) the row was left untouched forever, so the screen
 *    stayed wedged showing `data = null` — indistinguishable from "never synced" — on every
 *    future read, even once the network was healthy again (C35-022).
 *  - **stale beyond [ttlMillis]** (C35-017): even a row that still decodes fine is not served
 *    past the hard TTL; stale-while-revalidate only covers the window inside it.
 *
 * Deleting the row (rather than just returning null data) means the NEXT successful refresh
 * repopulates a clean row through the exact same upsert path already in place — recovery is
 * automatic, never a permanent wedge.
 */
suspend inline fun <reified T> readCachedJson(
    json: Json,
    cacheKey: String,
    dtoJson: String?,
    updatedAt: Long?,
    now: Long,
    ttlMillis: Long = CacheGovernance.DEFAULT_TTL_MILLIS,
    quarantine: suspend (String) -> Unit,
): CachedRead<T> {
    if (dtoJson == null || updatedAt == null) {
        return CachedRead(data = null, updatedAt = null, wasQuarantined = false)
    }
    if (now - updatedAt > ttlMillis) {
        quarantine(cacheKey)
        return CachedRead(data = null, updatedAt = null, wasQuarantined = true)
    }
    val decoded = runCatching { json.decodeFromString<T>(dtoJson) }.getOrNull()
    if (decoded == null) {
        quarantine(cacheKey)
        return CachedRead(data = null, updatedAt = null, wasQuarantined = true)
    }
    return CachedRead(data = decoded, updatedAt = updatedAt, wasQuarantined = false)
}

/**
 * Common shape every JSON-blob-by-scope cache DAO exposes so [enforceCacheBounds] (and any
 * future shared cache-layer logic) can stay generic across tables instead of being re-written
 * per repository. Each concrete `@Dao` still owns its own `@Query` SQL (Room needs the literal
 * table name), it just also implements this interface.
 */
interface JsonBlobCacheDao<E> {
    fun observe(cacheKey: String): Flow<E?>

    suspend fun upsert(entity: E)

    /** Deletes a single row by scope key — used to quarantine a corrupt/expired row. */
    suspend fun delete(cacheKey: String)

    /** Total row count — used to decide whether [deleteOldest] eviction is needed. */
    suspend fun count(): Int

    /** Approximate total JSON payload size across all rows (see [CacheGovernance.DEFAULT_MAX_BYTES]). */
    suspend fun totalBytes(): Long

    /** Deletes the [n] oldest rows by `updatedAt` (LRU). */
    suspend fun deleteOldest(n: Int)
}

/**
 * LRU-by-`updatedAt` eviction (C35-017): call after every write so a table backed by an
 * unbounded set of filter/cursor scope combinations never grows past [maxRows] rows or
 * [maxBytes] of JSON payload. Both loops delete at least one row per iteration and are bounded
 * by the current row count, so they always terminate.
 */
suspend fun <E> JsonBlobCacheDao<E>.enforceCacheBounds(
    maxRows: Int = CacheGovernance.DEFAULT_MAX_ROWS,
    maxBytes: Long = CacheGovernance.DEFAULT_MAX_BYTES,
) {
    val rowOverflow = count() - maxRows
    if (rowOverflow > 0) {
        deleteOldest(rowOverflow)
    }

    var guard = count()
    while (totalBytes() > maxBytes && guard > 0) {
        deleteOldest(1)
        guard--
    }
}
