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

    override suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity> {
        val snapshot = rows.value
        return snapshot
            .filter { row ->
                row.isDrainCandidate(now) &&
                    snapshot.none { older ->
                        older.groupKey == row.groupKey &&
                            older.createdAt < row.createdAt &&
                            older.isBackedOff(now)
                    }
            }
            .sortedBy { it.createdAt }
            .take(limit)
    }

    override fun observeAll() = rows.asStateFlow()

    // Status-guarded conditional transitions mirroring OutboxDao's atomic UPDATE ... WHERE
    // status=<expected> queries exactly, so the fake enforces the SAME race semantics.
    override suspend fun markInFlight(id: String, now: Long): Boolean =
        mutateIf(id, expected = setOf(OutboxStatus.QUEUED.name, OutboxStatus.FAILED.name)) {
            it.copy(status = OutboxStatus.IN_FLIGHT.name, updatedAt = now)
        }

    override suspend fun markSucceeded(id: String, resultJson: String, now: Long): Boolean =
        mutateIf(id, expected = setOf(OutboxStatus.IN_FLIGHT.name)) {
            it.copy(status = OutboxStatus.SUCCEEDED.name, resultJson = resultJson, lastError = null, updatedAt = now)
        }

    override suspend fun markFailed(
        id: String,
        attemptCount: Int,
        nextAttemptAt: Long,
        conflict: Boolean,
        lastError: String,
        now: Long,
    ): Boolean = mutateIf(id, expected = setOf(OutboxStatus.IN_FLIGHT.name)) {
        it.copy(
            status = OutboxStatus.FAILED.name,
            attemptCount = attemptCount,
            nextAttemptAt = nextAttemptAt,
            conflict = conflict,
            lastError = lastError,
            updatedAt = now,
        )
    }

    override suspend fun markRetryReady(id: String, now: Long): Boolean =
        mutateIf(id, expected = setOf(OutboxStatus.FAILED.name)) {
            it.copy(
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                conflict = false,
                lastError = null,
                nextAttemptAt = now,
                updatedAt = now,
            )
        }

    override suspend fun reclaimInFlight(now: Long): Int {
        val stranded = rows.value.count { it.status == OutboxStatus.IN_FLIGHT.name }
        if (stranded > 0) {
            rows.update { list ->
                list.map {
                    if (it.status == OutboxStatus.IN_FLIGHT.name) {
                        it.copy(status = OutboxStatus.QUEUED.name, updatedAt = now)
                    } else {
                        it
                    }
                }
            }
        }
        return stranded
    }

    private fun mutateIf(id: String, expected: Set<String>, transform: (OutboxEntity) -> OutboxEntity): Boolean {
        val current = rows.value.firstOrNull { it.id == id } ?: return false
        if (current.status !in expected) return false
        rows.update { list -> list.map { if (it.id == id) transform(it) else it } }
        return true
    }

    private fun OutboxEntity.isDrainCandidate(now: Long): Boolean =
        status == OutboxStatus.QUEUED.name ||
            (
                status == OutboxStatus.FAILED.name &&
                    !conflict &&
                    attemptCount < maxAttempts &&
                    nextAttemptAt <= now
                )

    private fun OutboxEntity.isBackedOff(now: Long): Boolean =
        status == OutboxStatus.FAILED.name &&
            !conflict &&
            attemptCount < maxAttempts &&
            nextAttemptAt > now
}
