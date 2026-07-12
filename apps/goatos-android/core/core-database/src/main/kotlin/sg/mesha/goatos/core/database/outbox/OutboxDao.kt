package sg.mesha.goatos.core.database.outbox

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * Room DAO for the outbox. State transitions are ATOMIC conditional `UPDATE`s guarded by the
 * expected current status (`WHERE id=:id AND status=<expected>`), NOT find-then-copy-then-
 * `@Update`: the old read-modify-write let a manual `retry()` and a concurrent drain clobber
 * each other (last-write-wins). Each transition returns the affected-row count so a caller can
 * tell whether it won the race (`1`) or was a no-op because the row had already moved on (`0`).
 */
@Dao
interface OutboxDao {
    /** Aborts (throws) on a duplicate [OutboxEntity.idempotencyKey] — the unique index is the
     *  enforcement point for "never a new key on retry, never a silent duplicate enqueue".
     *  Callers should check [findByIdempotencyKey] first for an idempotent-enqueue no-op. */
    @Insert
    suspend fun insert(entity: OutboxEntity)

    @Query("SELECT * FROM outbox WHERE idempotencyKey = :key LIMIT 1")
    suspend fun findByIdempotencyKey(key: String): OutboxEntity?

    @Query("SELECT * FROM outbox WHERE id = :id")
    suspend fun findById(id: String): OutboxEntity?

    /**
     * Rows the sync engine should attempt next, oldest-first: freshly QUEUED rows, or
     * FAILED rows still inside their retry budget whose backoff window has elapsed. A
     * [OutboxEntity.conflict] row is NEVER auto-eligible (only an explicit manual retry
     * re-arms it) — a definitive rejection will not change by re-sending the same payload.
     *
     * [limit] bounds the batch so a long offline period (thousands of queued writes) never
     * materializes the whole table into memory at once; the drain loops batch-by-batch (see
     * [SyncEngine.drainOnce]).
     */
    @Query(
        "SELECT candidate.* FROM outbox AS candidate " +
            "WHERE (candidate.status = 'QUEUED' " +
            "OR (candidate.status = 'FAILED' AND candidate.conflict = 0 " +
            "AND candidate.attemptCount < candidate.maxAttempts AND candidate.nextAttemptAt <= :now)) " +
            "AND NOT EXISTS (" +
            "SELECT 1 FROM outbox AS older " +
            "WHERE older.groupKey = candidate.groupKey " +
            "AND older.createdAt < candidate.createdAt " +
            "AND older.status = 'FAILED' " +
            "AND older.conflict = 0 " +
            "AND older.attemptCount < older.maxAttempts " +
            "AND older.nextAttemptAt > :now" +
            ") " +
            "ORDER BY candidate.createdAt ASC LIMIT :limit",
    )
    suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity>

    /** Backs the sync-status overlay (see `SyncRepository.observeStatus`). */
    @Query("SELECT * FROM outbox ORDER BY createdAt ASC")
    fun observeAll(): Flow<List<OutboxEntity>>

    // --- Atomic state transitions (return rows affected: 1 = applied, 0 = lost the race) ----

    @Query("UPDATE outbox SET status = 'IN_FLIGHT', updatedAt = :now WHERE id = :id AND status IN ('QUEUED', 'FAILED')")
    suspend fun markInFlight(id: String, now: Long): Int

    @Query(
        "UPDATE outbox SET status = 'SUCCEEDED', resultJson = :resultJson, lastError = NULL, updatedAt = :now " +
            "WHERE id = :id AND status = 'IN_FLIGHT'",
    )
    suspend fun markSucceeded(id: String, resultJson: String, now: Long): Int

    @Query(
        "UPDATE outbox SET status = 'FAILED', attemptCount = :attemptCount, nextAttemptAt = :nextAttemptAt, " +
            "conflict = :conflict, lastError = :lastError, updatedAt = :now WHERE id = :id AND status = 'IN_FLIGHT'",
    )
    suspend fun markFailed(
        id: String,
        attemptCount: Int,
        nextAttemptAt: Long,
        conflict: Boolean,
        lastError: String,
        now: Long,
    ): Int

    /** Manual retry only re-arms a terminal FAILED row — guarded so it can never clobber a
     *  row a drain is actively dispatching (IN_FLIGHT) or one that already SUCCEEDED. */
    @Query(
        "UPDATE outbox SET status = 'QUEUED', attemptCount = 0, conflict = 0, lastError = NULL, " +
            "nextAttemptAt = :now, updatedAt = :now WHERE id = :id AND status = 'FAILED'",
    )
    suspend fun markRetryReady(id: String, now: Long): Int

    /** Recovers rows orphaned IN_FLIGHT by a process death / crash mid-dispatch back to QUEUED.
     *  Safe to run at the top of a drain pass: the drain mutex guarantees no other dispatch is
     *  in progress, so any IN_FLIGHT row is necessarily stranded, not actively being sent. */
    @Query("UPDATE outbox SET status = 'QUEUED', updatedAt = :now WHERE status = 'IN_FLIGHT'")
    suspend fun reclaimInFlight(now: Long): Int

    /** Wipes the entire outbox. Used ONLY by the logout full clean-slate wipe (C35-001): any
     *  operator-authored write not yet synced belongs to the departing user's session and must
     *  never survive to the next principal on this device (regardless of QUEUED/FAILED/
     *  IN_FLIGHT/SUCCEEDED status). Never called from the drain/retry path. */
    @Query("DELETE FROM outbox")
    suspend fun clearAll()
}
