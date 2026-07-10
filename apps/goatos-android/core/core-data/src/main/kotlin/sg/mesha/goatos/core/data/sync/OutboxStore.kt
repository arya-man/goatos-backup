package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import sg.mesha.goatos.core.database.outbox.OutboxDao
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxStatus

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
    suspend fun eligibleForDrain(now: Long): List<OutboxEntity>
    fun observeAll(): Flow<List<OutboxEntity>>

    suspend fun markInFlight(id: String, now: Long)
    suspend fun markSucceeded(id: String, resultJson: String, now: Long)
    suspend fun markFailed(id: String, attemptCount: Int, nextAttemptAt: Long, conflict: Boolean, lastError: String, now: Long)

    /** Re-arms a row for another attempt: resets [OutboxEntity.attemptCount] to 0,
     *  [OutboxEntity.conflict] to false, and [OutboxEntity.status] back to QUEUED — the SAME
     *  [OutboxEntity.idempotencyKey] and [OutboxEntity.payloadJson] are preserved untouched. */
    suspend fun markRetryReady(id: String, now: Long)
}

class RoomOutboxStore(private val dao: OutboxDao) : OutboxStore {
    override suspend fun insert(entity: OutboxEntity) = dao.insert(entity)
    override suspend fun findById(id: String): OutboxEntity? = dao.findById(id)
    override suspend fun findByIdempotencyKey(key: String): OutboxEntity? = dao.findByIdempotencyKey(key)
    override suspend fun eligibleForDrain(now: Long): List<OutboxEntity> = dao.eligibleForDrain(now)
    override fun observeAll(): Flow<List<OutboxEntity>> = dao.observeAll()

    override suspend fun markInFlight(id: String, now: Long) {
        val current = dao.findById(id) ?: return
        dao.update(current.copy(status = OutboxStatus.IN_FLIGHT.name, updatedAt = now))
    }

    override suspend fun markSucceeded(id: String, resultJson: String, now: Long) {
        val current = dao.findById(id) ?: return
        dao.update(
            current.copy(
                status = OutboxStatus.SUCCEEDED.name,
                resultJson = resultJson,
                lastError = null,
                updatedAt = now,
            ),
        )
    }

    override suspend fun markFailed(
        id: String,
        attemptCount: Int,
        nextAttemptAt: Long,
        conflict: Boolean,
        lastError: String,
        now: Long,
    ) {
        val current = dao.findById(id) ?: return
        dao.update(
            current.copy(
                status = OutboxStatus.FAILED.name,
                attemptCount = attemptCount,
                nextAttemptAt = nextAttemptAt,
                conflict = conflict,
                lastError = lastError,
                updatedAt = now,
            ),
        )
    }

    override suspend fun markRetryReady(id: String, now: Long) {
        val current = dao.findById(id) ?: return
        dao.update(
            current.copy(
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                conflict = false,
                lastError = null,
                nextAttemptAt = now,
                updatedAt = now,
            ),
        )
    }
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
