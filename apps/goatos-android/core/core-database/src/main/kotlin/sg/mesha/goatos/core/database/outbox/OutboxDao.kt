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

    /** Observes ACTIVE rows only (QUEUED, IN_FLIGHT, and still-retryable non-conflict FAILED) —
     *  never includes SUCCEEDED, dead-letter (conflict) rows, OR attempt-exhausted FAILED rows
     *  (`attemptCount >= maxAttempts`). An exhausted row is terminal (it will never be re-claimed —
     *  see [eligibleForDrain]'s `attemptCount < maxAttempts` guard), so keeping it here would leave
     *  it in the active set forever, unbounded-accumulating in memory. This bounds memory and query
     *  time. Backs the sync-status overlay (see `SyncRepository.observeStatus`). */
    @Query(
        "SELECT * FROM outbox WHERE status IN ('QUEUED', 'IN_FLIGHT') " +
            "OR (status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts) " +
            "ORDER BY createdAt ASC",
    )
    fun observeActive(): Flow<List<OutboxEntity>>

    /**
     * Bounded active rows for one ordering group and a small caller-owned op-type set. Used by
     * offline-first read-model reconciliation so a network refresh cannot erase a command that is
     * still queued, in flight, or retryable locally. Terminal conflicts/exhausted rows are excluded
     * so authoritative backend rework is allowed through.
     */
    @Query(
        "SELECT * FROM outbox WHERE groupKey = :groupKey AND opType IN (:opTypes) AND (" +
            "status IN ('QUEUED', 'IN_FLIGHT') OR " +
            "(status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts)) " +
            "ORDER BY createdAt ASC LIMIT :limit",
    )
    suspend fun findActiveForGroup(
        groupKey: String,
        opTypes: List<String>,
        limit: Int,
    ): List<OutboxEntity>

    /** Batched variant for paged read-model reconciliation; avoids one outbox query per card. */
    @Query(
        "SELECT * FROM outbox WHERE groupKey IN (:groupKeys) AND opType IN (:opTypes) AND (" +
            "status IN ('QUEUED', 'IN_FLIGHT') OR " +
            "(status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts)) " +
            "ORDER BY createdAt ASC LIMIT :limit",
    )
    suspend fun findActiveForGroups(
        groupKeys: List<String>,
        opTypes: List<String>,
        limit: Int,
    ): List<OutboxEntity>

    /**
     * The single most recent row for one ordering group + op type, through EVERY status
     * including terminal SUCCEEDED — unlike [findActiveForGroup] / [findByIdempotencyKey],
     * this does NOT filter by status and does NOT key off the current idempotency epoch. A
     * caller asking "is there an outstanding/landed submission for this scope?" must find the
     * row by its stable identity (groupKey, opType), because [findByIdempotencyKey] re-derives
     * a key from the CURRENT epoch — which the SUCCEEDED row's own success already rotated
     * past (see `WeighingRepository.findPendingSubmit` / `SyncEngine.reconcileFeatureSuccess`),
     * so a key-based lookup misses the very row it is trying to find.
     */
    @Query(
        "SELECT * FROM outbox WHERE groupKey = :groupKey AND opType = :opType " +
            "ORDER BY createdAt DESC LIMIT 1",
    )
    suspend fun findLatestForGroupAndOpType(groupKey: String, opType: String): OutboxEntity?

    /** Observes ONE row by id through EVERY status, including terminal SUCCEEDED/conflict/
     *  attempt-exhausted (R50-030: leadership close must follow its own submission to a terminal
     *  state even when that row is older than the bounded recent-terminal window, which
     *  [observeRecentTerminals] would drop). Emits null if the row is absent/deleted. */
    @Query("SELECT * FROM outbox WHERE id = :id LIMIT 1")
    fun observeById(id: String): Flow<OutboxEntity?>

    /** Observes a bounded window of recent terminal rows — SUCCEEDED, dead-letter (conflict), AND
     *  attempt-exhausted FAILED (`attemptCount >= maxAttempts`) — for the UI to show recent-sync
     *  context without holding the entire history in memory. [recentLimit] bounds the number of
     *  rows. Attempt-exhausted rows appear here (as a terminal), not in [observeActive]. */
    @Query(
        "SELECT * FROM outbox WHERE status = 'SUCCEEDED' " +
            "OR (status = 'FAILED' AND (conflict = 1 OR attemptCount >= maxAttempts)) " +
            "ORDER BY updatedAt DESC LIMIT :recentLimit",
    )
    suspend fun observeRecentTerminals(recentLimit: Int): List<OutboxEntity>

    /** Prunes SUCCEEDED rows older than [retentionMs], keeping only recent successes for UI context.
     *  Never prunes FAILED, QUEUED, or IN_FLIGHT rows. Returns count of deleted rows. */
    @Query("DELETE FROM outbox WHERE status = 'SUCCEEDED' AND updatedAt < :cutoffTime")
    suspend fun pruneSucceeded(cutoffTime: Long): Int

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

    /** Deletes a specific outbox item by id (R50-028: cancelling unsynced operations like removing
     *  a proof that was never uploaded). Safe to call on any status except IN_FLIGHT, but a race
     *  with an in-flight drain is benign (the delete is idempotent). */
    @Query("DELETE FROM outbox WHERE id = :id")
    suspend fun delete(id: String)

    /** Deletes the item ONLY if it is still cancellable (QUEUED or terminal/backoff FAILED — NOT
     *  IN_FLIGHT). Returns rows affected: 1 = cancelled, 0 = the dispatcher already claimed it
     *  (IN_FLIGHT/SUCCEEDED) or it is gone. This single guarded statement is atomic against the
     *  dispatcher's status-guarded markInFlight, so there is no check-then-delete race where a proof
     *  file is pulled out from under an in-flight upload (R50-028 TOCTOU). */
    @Query("DELETE FROM outbox WHERE id = :id AND status IN ('QUEUED', 'FAILED')")
    suspend fun deleteIfCancellable(id: String): Int

    /** Wipes the entire outbox. Used ONLY by the logout full clean-slate wipe (C35-001): any
     *  operator-authored write not yet synced belongs to the departing user's session and must
     *  never survive to the next principal on this device (regardless of QUEUED/FAILED/
     *  IN_FLIGHT/SUCCEEDED status). Never called from the drain/retry path. */
    @Query("DELETE FROM outbox")
    suspend fun clearAll()
}
