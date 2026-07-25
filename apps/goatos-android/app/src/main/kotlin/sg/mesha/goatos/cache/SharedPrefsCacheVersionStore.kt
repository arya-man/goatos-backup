package sg.mesha.goatos.cache

import android.content.Context
import androidx.core.content.edit
import sg.mesha.goatos.core.data.cache.CacheVersionStore

/**
 * SharedPreferences-backed [CacheVersionStore]. Persists the app versionCode the execution read
 * caches were last written under, so [sg.mesha.goatos.core.data.cache.ExecutionCacheVersionGate]
 * can wipe stale read blobs once after an in-place update. Holds no PII — a single Int version.
 */
class SharedPrefsCacheVersionStore(context: Context) : CacheVersionStore {
    private val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    override suspend fun lastExecutionCacheVersion(): Int = prefs.getInt(KEY_EXECUTION_CACHE_VERSION, 0)

    override suspend fun setExecutionCacheVersion(version: Int) {
        prefs.edit { putInt(KEY_EXECUTION_CACHE_VERSION, version) }
    }

    private companion object {
        const val PREFS_NAME = "goatos_cache_meta"
        const val KEY_EXECUTION_CACHE_VERSION = "execution_cache_version"
    }
}
