package sg.mesha.goatos.analytics

import android.content.Context
import android.util.Log
import kotlinx.coroutines.withContext
import kotlinx.coroutines.Dispatchers
import org.json.JSONArray
import org.json.JSONObject
import sg.mesha.goatos.core.analytics.AnalyticsAppContext
import java.io.File
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/** One persisted, not-yet-confirmed-delivered backend analytics event. */
data class QueuedAnalyticsEvent(
    val clientEventId: String,
    val eventName: String,
    val properties: Map<String, String>,
    val clientEventTimeMs: Long,
    val flavor: String,
    val appVersionName: String,
    val appVersionCode: Int,
)

/**
 * P2 backend-analytics-durability fix. [BackendAnalyticsAdapter] on its own is fire-and-forget: an
 * offline device silently drops exactly the forensic events (proof failures, dead writes, live
 * status flips) this backend mirror exists to catch. This is a MINIMAL durable queue for that
 * critical subset only -- not a general-purpose outbox and not a replacement for the existing
 * Room-backed write outbox elsewhere in the app:
 *
 * - Persistence is one JSON-array file under [Context.filesDir], written with a temp-file-then-
 *   rename swap so a process death mid-write cannot leave a truncated/corrupt queue behind.
 * - [enqueue] runs on [ioDispatcher] and is drop-oldest capped at [maxEntries] (default 500): a device offline for a long
 *   stretch loses its OLDEST unsent critical events rather than growing this file unbounded.
 * - [drain] sends entries in FIFO order and stops at the first failure, so a single offline/
 *   backend-down stretch cannot skip ahead and desync ordering; entries are only removed from the
 *   file once [drain]'s `send` callback reports success for them.
 * - No backoff/scheduling of its own: [BackendAnalyticsAdapter.track] calls [drain] opportunistically
 *   on every subsequent track() call (see its kdoc) since any live network attempt is itself
 *   evidence connectivity may be back. This deliberately avoids a second background trigger
 *   (WorkManager job / ConnectivityManager callback) that would need wiring into
 *   `di/AnalyticsModule.kt` or `GoatOsApplication.kt` -- both out of scope for this fix.
 * - [QueuedAnalyticsEvent.clientEventId] is carried as a normal `client_event_id` request
 *   property (see [BackendAnalyticsAdapter]) so the backend CAN dedupe a resend that races a
 *   local "did it actually persist that removal" crash, if `/app/analytics/events` chooses to;
 *   the DTO has no dedicated dedupe field today, so this adapter cannot guarantee true
 *   exactly-once, only at-most-once-per-successful-local-removal (rare duplicates possible, see
 *   [BackendAnalyticsAdapter]'s doc comment).
 *
 * [context] defaults to [AnalyticsAppContext.applicationContext], populated by
 * `AnalyticsAppContextInitProvider` before any other app component runs (zero-host-wiring
 * Context capture -- see that class's kdoc). If that Context is somehow still `null` (e.g. a bare
 * JVM unit test that never triggers Robolectric's ContentProvider init), every operation here is a
 * safe, silent no-op: the queue simply does not persist, degrading no worse than the pre-fix
 * fire-and-forget behavior.
 */
class DurableAnalyticsQueue(
    context: Context? = AnalyticsAppContext.applicationContext,
    private val maxEntries: Int = MAX_QUEUE_ENTRIES,
    /** Injectable so tests can run file I/O on the test dispatcher's virtual time. */
    private val ioDispatcher: kotlinx.coroutines.CoroutineDispatcher = Dispatchers.IO,
) {
    private val lock = ReentrantLock()
    private val queueFile: File? = context?.applicationContext?.let { File(it.filesDir, QUEUE_FILE_NAME) }

    /** Persists one event, applying the drop-oldest cap. Safe to call from any coroutine. */
    suspend fun enqueue(event: QueuedAnalyticsEvent) {
        val file = queueFile ?: return
        withContext(ioDispatcher) { lock.withLock {
            runCatching {
                val entries = readAllLocked(file) + event
                val bounded = if (entries.size > maxEntries) entries.takeLast(maxEntries) else entries
                writeAllLocked(file, bounded)
            }.onFailure { Log.w(TAG, "DurableAnalyticsQueue enqueue failed", it) }
        } }
    }

    /**
     * Attempts to send every persisted entry, in order, via [send]. Stops at the first entry
     * [send] reports as not-sent (returns `false` or throws) so later entries are never sent out
     * of order ahead of an earlier one still stuck. Entries [send] confirmed are removed from the
     * file before returning.
     */
    suspend fun drain(send: suspend (QueuedAnalyticsEvent) -> Boolean) {
        val file = queueFile ?: return
        val entries = withContext(ioDispatcher) {
            lock.withLock { runCatching { readAllLocked(file) }.getOrElse { emptyList() } }
        }
        if (entries.isEmpty()) return
        var sentCount = 0
        for (entry in entries) {
            // exception:exempt send failure IS the signal — drain stops and the entry stays queued for the next attempt
            val sent = runCatching { send(entry) }.getOrDefault(false)
            if (!sent) break
            sentCount++
        }
        if (sentCount > 0) {
            withContext(ioDispatcher) {
                lock.withLock {
                    val current = runCatching { readAllLocked(file) }.getOrElse { emptyList() }
                    val sentIds = entries.take(sentCount).map { it.clientEventId }.toSet()
                    val remaining = current.filterNot { it.clientEventId in sentIds }
                    runCatching { writeAllLocked(file, remaining) }
                        .onFailure { Log.w(TAG, "DurableAnalyticsQueue post-drain rewrite failed", it) }
                }
            }
        }
    }

    /** Current persisted entry count -- test/diagnostic hook, not on any hot path. */
    suspend fun size(): Int {
        val file = queueFile ?: return 0
        // exception:exempt diagnostic size probe; readAllLocked logs corrupt files itself
        return withContext(ioDispatcher) { lock.withLock { runCatching { readAllLocked(file) }.getOrDefault(emptyList()).size } }
    }

    private fun readAllLocked(file: File): List<QueuedAnalyticsEvent> {
        if (!file.exists()) return emptyList()
        val text = file.readText()
        if (text.isBlank()) return emptyList()
        val array = runCatching { JSONArray(text) }.getOrElse {
            // Corrupted queue file (e.g. process death mid-write): the forensic queue must never
            // die silently — log loudly, then start fresh rather than wedging every enqueue.
            Log.w(TAG, "DurableAnalyticsQueue file unreadable; dropping ${'$'}{text.length} bytes", it)
            return emptyList()
        }
        return buildList {
            for (i in 0 until array.length()) {
                val obj = array.optJSONObject(i) ?: continue
                val propsObj = obj.optJSONObject("properties")
                val props = buildMap {
                    propsObj?.keys()?.forEach { key -> put(key, propsObj.optString(key)) }
                }
                add(
                    QueuedAnalyticsEvent(
                        clientEventId = obj.optString("client_event_id"),
                        eventName = obj.optString("event_name"),
                        properties = props,
                        clientEventTimeMs = obj.optLong("client_event_time_ms"),
                        flavor = obj.optString("flavor"),
                        appVersionName = obj.optString("app_version_name"),
                        appVersionCode = obj.optInt("app_version_code"),
                    ),
                )
            }
        }
    }

    private fun writeAllLocked(file: File, entries: List<QueuedAnalyticsEvent>) {
        val array = JSONArray()
        for (entry in entries) {
            val obj = JSONObject()
            obj.put("client_event_id", entry.clientEventId)
            obj.put("event_name", entry.eventName)
            val propsObj = JSONObject()
            entry.properties.forEach { (k, v) -> propsObj.put(k, v) }
            obj.put("properties", propsObj)
            obj.put("client_event_time_ms", entry.clientEventTimeMs)
            obj.put("flavor", entry.flavor)
            obj.put("app_version_name", entry.appVersionName)
            obj.put("app_version_code", entry.appVersionCode)
            array.put(obj)
        }
        file.parentFile?.mkdirs()
        val tmp = File(file.parentFile, "${file.name}.tmp")
        tmp.writeText(array.toString())
        if (!tmp.renameTo(file)) {
            // Cross-filesystem or platform rename quirk fallback -- still correct, just not
            // atomic; the temp file is always cleaned up either way.
            file.writeText(array.toString())
            tmp.delete()
        }
    }

    private companion object {
        const val TAG = "GoatAnalyticsQueue"
        const val QUEUE_FILE_NAME = "durable_analytics_queue.json"
        const val MAX_QUEUE_ENTRIES = 500
    }
}
