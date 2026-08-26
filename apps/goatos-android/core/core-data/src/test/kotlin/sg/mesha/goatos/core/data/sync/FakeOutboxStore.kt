package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.database.outbox.ActiveOutboxCounts

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

    /** Every row regardless of status — terminal ones included. Tests that assert "a write was
     *  re-enqueued" must count rows, not merely check that no error was returned. */
    fun snapshot(): List<OutboxEntity> = rows.value

    override suspend fun findById(id: String): OutboxEntity? = rows.value.firstOrNull { it.id == id }

    override suspend fun findByIdempotencyKey(key: String): OutboxEntity? =
        rows.value.firstOrNull { it.idempotencyKey == key }

    override suspend fun findLatestForGroupAndOpType(groupKey: String, opType: String): OutboxEntity? =
        rows.value
            .filter { it.groupKey == groupKey && it.opType == opType }
            .maxByOrNull { it.createdAt }

    override suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity> {
        val snapshot = rows.value
        return snapshot
            .filter { row ->
                row.isDrainCandidate(now) &&
                    snapshot.none { older ->
                        older.groupKey == row.groupKey &&
                            // Mirrors OutboxDao: insertion order breaks a createdAt tie, so
                            // same-millisecond rows still hold each other back.
                            (
                                older.createdAt < row.createdAt ||
                                    (older.createdAt == row.createdAt && snapshot.indexOf(older) < snapshot.indexOf(row))
                                ) &&
                            older.blocksLaterCandidate(now, row)
                    }
            }
            .sortedWith(compareBy({ it.createdAt }, { snapshot.indexOf(it) }))
            .take(limit)
    }

    override fun observeActive(): Flow<List<OutboxEntity>> =
        rows.asStateFlow().map { all ->
            all.filter {
                it.status == OutboxStatus.QUEUED.name ||
                    it.status == OutboxStatus.IN_FLIGHT.name ||
                    (it.status == OutboxStatus.FAILED.name && !it.conflict && it.attemptCount < it.maxAttempts)
            }
        }

    override fun observeActiveByOpType(opType: String): kotlinx.coroutines.flow.Flow<List<OutboxEntity>> {
        val base = observeActive()
        return kotlinx.coroutines.flow.flow {
            base.collect { rows -> emit(rows.filter { row -> row.opType == opType }) }
        }
    }
    override fun observeActiveCounts(): Flow<ActiveOutboxCounts> =
        rows.asStateFlow().map { all ->
            val active = all.filter {
                it.status == OutboxStatus.QUEUED.name ||
                    it.status == OutboxStatus.IN_FLIGHT.name ||
                    (it.status == OutboxStatus.FAILED.name && !it.conflict && it.attemptCount < it.maxAttempts)
            }
            ActiveOutboxCounts(
                queued = active.count { it.status == OutboxStatus.QUEUED.name },
                inFlight = active.count { it.status == OutboxStatus.IN_FLIGHT.name },
                failed = active.count { it.status == OutboxStatus.FAILED.name && !it.conflict && it.attemptCount < it.maxAttempts },
            )
        }

    override fun observeActiveWindow(limit: Int): Flow<List<OutboxEntity>> =
        rows.asStateFlow().map { all ->
            all.filter {
                it.status == OutboxStatus.QUEUED.name ||
                    it.status == OutboxStatus.IN_FLIGHT.name ||
                    (it.status == OutboxStatus.FAILED.name && !it.conflict && it.attemptCount < it.maxAttempts)
            }
                .sortedByDescending { it.createdAt }
                .take(limit)
        }

    override fun observeById(id: String): Flow<OutboxEntity?> =
        rows.asStateFlow().map { all -> all.firstOrNull { it.id == id } }

    override suspend fun observeRecentTerminals(recentLimit: Int): List<OutboxEntity> {
        return rows.value
            .filter {
                it.status == OutboxStatus.SUCCEEDED.name ||
                    (it.status == OutboxStatus.FAILED.name && it.conflict)
            }
            .sortedByDescending { it.updatedAt }
            .take(recentLimit)
    }

    override suspend fun pruneSucceeded(retentionMs: Long, now: Long): Int {
        val cutoff = now - retentionMs
        val beforeCount = rows.value.count { it.status == OutboxStatus.SUCCEEDED.name }
        rows.update { list ->
            list.filter { row ->
                !(row.status == OutboxStatus.SUCCEEDED.name && row.updatedAt < cutoff)
            }
        }
        val afterCount = rows.value.count { it.status == OutboxStatus.SUCCEEDED.name }
        return beforeCount - afterCount
    }

    override fun observeAll(): Flow<List<OutboxEntity>> =
        observeActive() // Delegate to observeActive for bounded query (legacy compatibility)

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

    override suspend fun reopenTerminalForRetry(id: String, payloadJson: String, fingerprint: String, now: Long): Boolean {
        val current = rows.value.firstOrNull { it.id == id } ?: return false
        val isTerminal = current.status == OutboxStatus.FAILED.name &&
            (current.conflict || current.attemptCount >= current.maxAttempts)
        if (!isTerminal) return false
        rows.update { list ->
            list.map {
                if (it.id == id) {
                    it.copy(
                        status = OutboxStatus.QUEUED.name,
                        attemptCount = 0,
                        conflict = false,
                        lastError = null,
                        nextAttemptAt = now,
                        updatedAt = now,
                        payloadJson = payloadJson,
                        requestFingerprint = fingerprint,
                    )
                } else {
                    it
                }
            }
        }
        return true
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

    override suspend fun delete(id: String) {
        rows.update { list -> list.filterNot { it.id == id } }
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

    private fun OutboxEntity.blocksLaterCandidate(now: Long, candidate: OutboxEntity): Boolean {
        if (
            candidate.opType in setOf("PC_CARE_SLOT_REGISTER", "PC_CARE_TASK_PROOF_REGISTER") &&
            opType == "PROOF_UPLOAD" &&
            candidate.referencesProofUpload(id) &&
            isActiveProofUpload(now)
        ) {
            return true
        }
        if (
            candidate.opType == "PC_CARE_TASK_SUBMIT" &&
            opType == "PROOF_UPLOAD" &&
            isActiveProofUpload(now) &&
            rows.value.any { register ->
                register.groupKey == candidate.groupKey &&
                    register.opType in setOf("PC_CARE_SLOT_REGISTER", "PC_CARE_TASK_PROOF_REGISTER") &&
                    register.referencesProofUpload(id) &&
                    register.isActiveRegister(now) &&
                    register.createdAt <= candidate.createdAt
            }
        ) {
            return true
        }
        if (status != OutboxStatus.FAILED.name || !(conflict || attemptCount >= maxAttempts || nextAttemptAt > now)) {
            return false
        }
        if (candidate.opType == "COUNTS_SHIFTING" && opType == "COUNTS_SHIFTING") return false
        if (candidate.opType == "PROOF_UPLOAD" && opType == "PROOF_UPLOAD") return false
        if (candidate.opType == opType && candidate.opType in setOf("WEIGHING_ANIMAL_OBSERVATION", "WEIGHING_SHED_OBSERVATION")) return false
        if (opType in setOf("PC_CARE_SLOT_REGISTER", "PC_CARE_TASK_PROOF_REGISTER") &&
            candidate.opType in setOf("PC_CARE_SLOT_REGISTER", "PC_CARE_TASK_PROOF_REGISTER")
        ) {
            return false
        }
        if (opType in setOf("PC_CARE_SLOT_REGISTER", "PC_CARE_TASK_PROOF_REGISTER") &&
            candidate.opType == "PC_CARE_TASK_SUBMIT" &&
            rows.value.any { register ->
                register.groupKey == candidate.groupKey &&
                    register.opType in setOf("PC_CARE_SLOT_REGISTER", "PC_CARE_TASK_PROOF_REGISTER") &&
                    register.createdAt > createdAt &&
                    register.createdAt <= candidate.createdAt &&
                    register.status in setOf(OutboxStatus.QUEUED.name, OutboxStatus.IN_FLIGHT.name, OutboxStatus.SUCCEEDED.name)
            }
        ) {
            return false
        }
        return true
    }

    private fun OutboxEntity.isActiveProofUpload(now: Long): Boolean =
        status in setOf(OutboxStatus.QUEUED.name, OutboxStatus.IN_FLIGHT.name) ||
            (status == OutboxStatus.FAILED.name && !conflict && attemptCount < maxAttempts && nextAttemptAt <= now)

    private fun OutboxEntity.isActiveRegister(now: Long): Boolean =
        status in setOf(OutboxStatus.QUEUED.name, OutboxStatus.IN_FLIGHT.name) ||
            (status == OutboxStatus.FAILED.name && !conflict && attemptCount < maxAttempts && nextAttemptAt <= now)

    private fun OutboxEntity.referencesProofUpload(proofOutboxItemId: String): Boolean =
        payloadJson.contains(""""proof_outbox_item_id":"$proofOutboxItemId"""")
}
