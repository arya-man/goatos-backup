package sg.mesha.goatos.core.data.cache

/**
 * Persists the app versionCode that the execution read-cache blobs were last written under.
 * Backed by a small key-value store in :app (SharedPreferences); a fake in tests.
 */
interface CacheVersionStore {
    suspend fun lastExecutionCacheVersion(): Int
    suspend fun setExecutionCacheVersion(version: Int)
}

/**
 * Invalidates the execution read caches (drive-row list + per-shed drilldown) on an app-version
 * change.
 *
 * WHY: the execution caches are JSON blobs keyed by query scope; a normal refresh replaces a blob
 * only on a successful fetch, so a blob written before a serving-shape change (e.g. a vaccination
 * drive-date move) can survive an in-place app update as a stale ghost — the update keeps app data,
 * and a failed/absent refresh never overwrites it. Wiping these read blobs once per version bump
 * forces a clean refetch after every update. It NEVER touches the outbox or any write-side table,
 * so unsynced operator writes are preserved.
 */
class ExecutionCacheVersionGate(
    private val rowsDao: ExecutionRowsCacheDao,
    private val shedDao: ExecutionShedCacheDao,
    private val store: CacheVersionStore,
) {
    suspend fun purgeIfVersionChanged(currentVersion: Int) {
        if (store.lastExecutionCacheVersion() == currentVersion) return
        rowsDao.clearAll()
        shedDao.clearAll()
        store.setExecutionCacheVersion(currentVersion)
    }
}
