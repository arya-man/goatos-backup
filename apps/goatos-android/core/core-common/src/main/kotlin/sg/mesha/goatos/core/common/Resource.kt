package sg.mesha.goatos.core.common

/**
 * Stale-while-revalidate wrapper for an offline-first read
 * (docs/decisions/android-offline-first.md). Room is the UI's single source of truth: [data]
 * is populated as soon as the on-device cache has anything (null only on a cold cache),
 * [isRefreshing] and [error] describe the in-flight/last network attempt, and the cache
 * NEVER disappears just because a background refresh is running or failed — a screen must
 * never show a blank/loading wall when cached data exists.
 */
data class Resource<out T>(
    val data: T?,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val error: Throwable? = null,
) {
    val hasData: Boolean get() = data != null
}
