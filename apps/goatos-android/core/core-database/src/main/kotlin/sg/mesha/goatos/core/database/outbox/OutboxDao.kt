package sg.mesha.goatos.core.database.outbox

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query
import androidx.room.Update
import kotlinx.coroutines.flow.Flow

/**
 * Room DAO for the outbox. Kept deliberately thin (insert / update / a couple of finder
 * queries + the drain-eligibility query) — state transitions (mark in-flight / succeeded /
 * failed / retry-ready) are composed in `:core:core-data`'s `RoomOutboxStore` via
 * find-then-[update], not as bespoke `@Query` UPDATE statements per transition.
 */
@Dao
interface OutboxDao {
    /** Aborts (throws) on a duplicate [OutboxEntity.idempotencyKey] — the unique index is the
     *  enforcement point for "never a new key on retry, never a silent duplicate enqueue".
     *  Callers should check [findByIdempotencyKey] first for an idempotent-enqueue no-op. */
    @Insert
    suspend fun insert(entity: OutboxEntity)

    @Update
    suspend fun update(entity: OutboxEntity)

    @Query("SELECT * FROM outbox WHERE idempotencyKey = :key LIMIT 1")
    suspend fun findByIdempotencyKey(key: String): OutboxEntity?

    @Query("SELECT * FROM outbox WHERE id = :id")
    suspend fun findById(id: String): OutboxEntity?

    /**
     * Rows the sync engine should attempt next, oldest-first: freshly QUEUED rows, or
     * FAILED rows still inside their retry budget whose backoff window has elapsed. A
     * [OutboxEntity.conflict] row is NEVER auto-eligible (only an explicit manual retry
     * re-arms it) — a definitive rejection will not change by re-sending the same payload.
     */
    @Query(
        "SELECT * FROM outbox WHERE status = 'QUEUED' " +
            "OR (status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts AND nextAttemptAt <= :now) " +
            "ORDER BY createdAt ASC",
    )
    suspend fun eligibleForDrain(now: Long): List<OutboxEntity>

    /** Backs the sync-status overlay (see `SyncRepository.observeStatus`). */
    @Query("SELECT * FROM outbox ORDER BY createdAt ASC")
    fun observeAll(): Flow<List<OutboxEntity>>
}
