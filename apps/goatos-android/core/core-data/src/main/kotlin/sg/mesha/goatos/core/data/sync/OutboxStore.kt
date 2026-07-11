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
}

class RoomOutboxStore(private val dao: OutboxDao) : OutboxStore {
    override suspend fun insert(entity: OutboxEntity) = dao.insert(entity)
    override suspend fun findById(id: String): OutboxEntity? = dao.findById(id)
    override suspend fun findByIdempotencyKey(key: String): OutboxEntity? = dao.findByIdempotencyKey(key)
    override suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity> = dao.eligibleForDrain(now, limit)
    override fun observeAll(): Flow<List<OutboxEntity>> = dao.observeAll()

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
)

/** The full outbox table -> the [SyncStatus] snapshot the UI renders. */
fun List<OutboxEntity>.toSyncStatus(online: Boolean): SyncStatus {
    val items = map { it.toSyncQueueItem() }
    return SyncStatus(
        online = online,
        pendingCount = items.count { it.status == SyncItemStatus.QUEUED },
        inFlightCount = items.count { it.status == SyncItemStatus.IN_FLIGHT },
        failedCount = items.count { it.status == SyncItemStatus.FAILED },
        deadLetterCount = items.count { it.isDeadLetter },
        lastSyncAt = items.filter { it.status == SyncItemStatus.SUCCEEDED }.maxOfOrNull { it.updatedAt },
        items = items,
    )
}
