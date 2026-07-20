package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import sg.mesha.goatos.core.database.outbox.OutboxDao
import sg.mesha.goatos.core.database.outbox.OutboxEntity

/**
 * Persistence port for the outbox. [SyncEngine] and [SyncRepository] talk to this, never to
 * Room directly — that keeps the drain/business logic unit-testable against an in-memory
 * fake (see core-data's `src/test` sources) without Robolectric or an instrumented/emulator
 * test path (neither is available: `androidx.room` in-memory Room needs a real `Context`,
 * and Robolectric has no version alias in `gradle/libs.versions.toml`).
 */
interface OutboxStore {
    suspend fun insert(entity: OutboxEntity)
    suspend fun findById(id: String): OutboxEntity?
    suspend fun findByIdempotencyKey(key: String): OutboxEntity?
    suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity>

    /** Observes ACTIVE rows only (QUEUED, IN_FLIGHT, non-conflict FAILED) — never includes
     *  SUCCEEDED or dead-letter rows. Bounded for memory/query performance. */
    fun observeActive(): Flow<List<OutboxEntity>>

    /** Observes ONE row by id through every status incl. terminal (R50-030): lets a caller follow
     *  a specific item to completion even when it is older than the recent-terminal window. */
    fun observeById(id: String): Flow<OutboxEntity?>

    /** Observes a bounded window of recent terminal rows (SUCCEEDED and conflict FAILED).
     *  Used to show recent-sync context to the UI without holding entire history. */
    suspend fun observeRecentTerminals(recentLimit: Int): List<OutboxEntity>

    /** Prunes SUCCEEDED rows older than [retentionMs]. Never prunes in-flight work.
     *  Returns count of deleted rows. */
    suspend fun pruneSucceeded(retentionMs: Long, now: Long): Int

    /** Legacy full-table query — use [observeActive] instead. */
    fun observeAll(): Flow<List<OutboxEntity>>

    /** All transitions are ATOMIC + status-guarded and return whether they were applied
     *  (`true`) or were a no-op because the row had already moved on (`false`) — so a manual
     *  retry and a concurrent drain can never silently clobber each other. */
    suspend fun markInFlight(id: String, now: Long): Boolean
    suspend fun markSucceeded(id: String, resultJson: String, now: Long): Boolean
    suspend fun markFailed(id: String, attemptCount: Int, nextAttemptAt: Long, conflict: Boolean, lastError: String, now: Long): Boolean

    /** Re-arms a terminal FAILED row for another attempt: resets [OutboxEntity.attemptCount] to
     *  0, [OutboxEntity.conflict] to false, [OutboxEntity.status] back to QUEUED — the SAME
     *  [OutboxEntity.idempotencyKey] and [OutboxEntity.payloadJson] are preserved untouched.
     *  Returns `false` (no-op) if the row is not FAILED (e.g. a drain has it IN_FLIGHT). */
    suspend fun markRetryReady(id: String, now: Long): Boolean

    /** Recovers rows stranded IN_FLIGHT by a prior crash/process-death mid-dispatch back to
     *  QUEUED. Returns the number reclaimed. Called at the top of every drain pass (safe under
     *  the drain mutex — no dispatch is concurrently in progress). */
    suspend fun reclaimInFlight(now: Long): Int

    /** Deletes an outbox item by id (R50-028: cancelling unsynced operations). Safe to call on
     *  any status, but IN_FLIGHT deletions should be rare (a race with in-flight dispatch). */
    suspend fun delete(id: String)

    /** Atomic status-guarded cancel: deletes the item only if still QUEUED/FAILED (not IN_FLIGHT).
     *  Returns true if it was cancelled, false if the dispatcher already claimed it or it is gone.
     *  Default (for lightweight fakes) is unconditional; RoomOutboxStore overrides with the guard. */
    suspend fun deleteIfCancellable(id: String): Boolean {
        delete(id)
        return true
    }
}

class RoomOutboxStore(private val dao: OutboxDao) : OutboxStore {
    override suspend fun insert(entity: OutboxEntity) = dao.insert(entity)
    override suspend fun findById(id: String): OutboxEntity? = dao.findById(id)
    override suspend fun findByIdempotencyKey(key: String): OutboxEntity? = dao.findByIdempotencyKey(key)
    override suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity> = dao.eligibleForDrain(now, limit)
    override fun observeActive(): Flow<List<OutboxEntity>> = dao.observeActive()
    override fun observeById(id: String): Flow<OutboxEntity?> = dao.observeById(id)
    override suspend fun observeRecentTerminals(recentLimit: Int): List<OutboxEntity> = dao.observeRecentTerminals(recentLimit)
    override suspend fun pruneSucceeded(retentionMs: Long, now: Long): Int = dao.pruneSucceeded(cutoffTime = now - retentionMs)
    override fun observeAll(): Flow<List<OutboxEntity>> = dao.observeActive() // Delegate to active for bounded query

    override suspend fun markInFlight(id: String, now: Long): Boolean = dao.markInFlight(id, now) > 0

    override suspend fun markSucceeded(id: String, resultJson: String, now: Long): Boolean =
        dao.markSucceeded(id, resultJson, now) > 0

    override suspend fun markFailed(
        id: String,
        attemptCount: Int,
        nextAttemptAt: Long,
        conflict: Boolean,
        lastError: String,
        now: Long,
    ): Boolean = dao.markFailed(id, attemptCount, nextAttemptAt, conflict, lastError, now) > 0

    override suspend fun markRetryReady(id: String, now: Long): Boolean = dao.markRetryReady(id, now) > 0

    override suspend fun reclaimInFlight(now: Long): Int = dao.reclaimInFlight(now)

    override suspend fun delete(id: String) = dao.delete(id)
    override suspend fun deleteIfCancellable(id: String): Boolean = dao.deleteIfCancellable(id) > 0
}

/** Room row -> UI/ViewModel-facing model (see [SyncQueueItem]). */
fun OutboxEntity.toSyncQueueItem(): SyncQueueItem = SyncQueueItem(
    id = id,
    opType = opType,
    groupKey = groupKey,
    status = SyncItemStatus.valueOf(status),
    attemptCount = attemptCount,
    maxAttempts = maxAttempts,
    conflict = conflict,
    createdAt = createdAt,
    updatedAt = updatedAt,
    lastError = lastError,
    resultJson = resultJson,
)

/** Active rows + bounded recent terminals -> the [SyncStatus] snapshot the UI renders.
 *  [activeRows] is QUEUED/IN_FLIGHT/non-conflict-FAILED (never SUCCEEDED).
 *  [recentTerminals] is a bounded window of SUCCEEDED and conflict-FAILED for UI context.
 *  [recentTerminals] MUST be pre-sorted by recency (descending updatedAt) from the DAO. */
fun toSyncStatus(activeRows: List<OutboxEntity>, recentTerminals: List<OutboxEntity>, online: Boolean): SyncStatus {
    val activeItems = activeRows.map { it.toSyncQueueItem() }
    val recentItems = recentTerminals.map { it.toSyncQueueItem() }
    val allItems = activeItems + recentItems

    return SyncStatus(
        online = online,
        pendingCount = activeItems.count { it.status == SyncItemStatus.QUEUED },
        inFlightCount = activeItems.count { it.status == SyncItemStatus.IN_FLIGHT },
        failedCount = activeItems.count { it.status == SyncItemStatus.FAILED },
        deadLetterCount = recentItems.count { it.isDeadLetter }, // Dead-letter is terminal, so only in recent
        lastSyncAt = recentItems.filter { it.status == SyncItemStatus.SUCCEEDED }.maxOfOrNull { it.updatedAt },
        items = allItems,
    )
}
