package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxStatus

/**
 * In-memory [OutboxStore] test double — no Room/Robolectric needed. [OutboxEntity] is a
 * plain data class (Room annotations are compile-time-only), so it can be constructed and
 * mutated directly here on the plain JVM unit-test runner; the eligibility filtering below
 * mirrors [sg.mesha.goatos.core.database.outbox.OutboxDao.eligibleForDrain] exactly, so
 * [SyncEngine]/[SyncRepository] exercise the SAME contract they'd see against
 * [RoomOutboxStore] in production.
 */
class FakeOutboxStore : OutboxStore {
    private val rows = MutableStateFlow<List<OutboxEntity>>(emptyList())

    override suspend fun insert(entity: OutboxEntity) {
        require(rows.value.none { it.idempotencyKey == entity.idempotencyKey }) {
            "duplicate idempotencyKey: ${entity.idempotencyKey}"
        }
        rows.update { it + entity }
    }

    override suspend fun findById(id: String): OutboxEntity? = rows.value.firstOrNull { it.id == id }

    override suspend fun findByIdempotencyKey(key: String): OutboxEntity? =
        rows.value.firstOrNull { it.idempotencyKey == key }

    override suspend fun eligibleForDrain(now: Long): List<OutboxEntity> = rows.value
        .filter { row ->
            row.status == OutboxStatus.QUEUED.name ||
                (
                    row.status == OutboxStatus.FAILED.name &&
                        !row.conflict &&
                        row.attemptCount < row.maxAttempts &&
                        row.nextAttemptAt <= now
                    )
        }
        .sortedBy { it.createdAt }

    override fun observeAll() = rows.asStateFlow()

    override suspend fun markInFlight(id: String, now: Long) =
        mutate(id) { it.copy(status = OutboxStatus.IN_FLIGHT.name, updatedAt = now) }

    override suspend fun markSucceeded(id: String, resultJson: String, now: Long) = mutate(id) {
        it.copy(status = OutboxStatus.SUCCEEDED.name, resultJson = resultJson, lastError = null, updatedAt = now)
    }

    override suspend fun markFailed(
        id: String,
        attemptCount: Int,
        nextAttemptAt: Long,
        conflict: Boolean,
        lastError: String,
        now: Long,
    ) = mutate(id) {
        it.copy(
            status = OutboxStatus.FAILED.name,
            attemptCount = attemptCount,
            nextAttemptAt = nextAttemptAt,
            conflict = conflict,
            lastError = lastError,
            updatedAt = now,
        )
    }

    override suspend fun markRetryReady(id: String, now: Long) = mutate(id) {
        it.copy(
            status = OutboxStatus.QUEUED.name,
            attemptCount = 0,
            conflict = false,
            lastError = null,
            nextAttemptAt = now,
            updatedAt = now,
        )
    }

    private fun mutate(id: String, transform: (OutboxEntity) -> OutboxEntity) {
        rows.update { list -> list.map { if (it.id == id) transform(it) else it } }
    }
}
