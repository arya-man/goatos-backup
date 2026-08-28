package sg.mesha.goatos.core.database.outbox

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.Query
import kotlinx.coroutines.flow.Flow
import androidx.room.ColumnInfo

/** Aggregate counts of active outbox items without materializing rows. Used for sync status
 *  badges and health monitoring. Queued and inFlight are always ≥0; failed can be null if
 *  the aggregation query returns no rows. */
data class ActiveOutboxCounts(
    @ColumnInfo("queued")
    val queued: Int = 0,

    @ColumnInfo("inFlight")
    val inFlight: Int = 0,

    @ColumnInfo("failed")
    val failed: Int = 0,
) {
    val total: Int get() = (queued ?: 0) + (inFlight ?: 0) + (failed ?: 0)
}

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
            // A strict createdAt comparison lets two rows written in the SAME millisecond
            // ignore each other: a Submit enqueued in the same tick as a scan was neither
            // held back by it nor ordered after it. rowid breaks the tie in insertion order.
            //
            // outbox has a TEXT primary key, so this rowid is IMPLICIT -- and SQLite is
            // allowed to renumber implicit rowids during VACUUM. Nothing in this app runs
            // VACUUM, and nothing may: adding one would silently invert a same-millisecond
            // scan/Submit pair that is already queued. If a compaction task is ever wanted,
            // give this table an explicit monotonic sequence column first.
            "AND (older.createdAt < candidate.createdAt " +
            "OR (older.createdAt = candidate.createdAt AND older.rowid < candidate.rowid)) " +
            // A failed older row in this lane holds the lane -- including the two TERMINAL
            // failures, a scan that burned its retry budget and one dead-lettered as a
            // conflict, which used to fall straight through because they are no longer
            // eligible themselves.
            //
            // Those are the cases that matter most. Ordering exists here so a shed's Submit
            // cannot claim the session complete while one of its scans never reached the
            // server, and a permanently stuck scan is exactly that: letting the Submit past
            // closes the shed one animal short while every screen reads "complete". A held
            // lane is visible and recoverable; a silent under-count is neither.
            //
            // Rows still making progress (QUEUED, IN_FLIGHT) are deliberately NOT blockers:
            // they drain in this same pass, in order, and treating them as blockers would
            // hold back work that is about to succeed.
            "AND ((" +
            "older.status = 'FAILED' " +
            "AND NOT (candidate.opType = 'COUNTS_SHIFTING' AND older.opType = 'COUNTS_SHIFTING') " +
            "AND NOT (candidate.opType = 'PROOF_UPLOAD' AND older.opType = 'PROOF_UPLOAD') " +
            "AND NOT (candidate.opType = older.opType AND candidate.opType IN ('WEIGHING_ANIMAL_OBSERVATION', 'WEIGHING_SHED_OBSERVATION')) " +
            "AND NOT (older.opType IN ('PC_CARE_SLOT_REGISTER', 'PC_CARE_TASK_PROOF_REGISTER') " +
            "  AND candidate.opType IN ('PROOF_UPLOAD', 'PC_CARE_SLOT_REGISTER', 'PC_CARE_TASK_PROOF_REGISTER')) " +
            "AND NOT (older.opType IN ('PC_CARE_SLOT_REGISTER', 'PC_CARE_TASK_PROOF_REGISTER') " +
            "  AND candidate.opType = 'PC_CARE_TASK_SUBMIT' " +
            "  AND EXISTS (" +
            "    SELECT 1 FROM outbox AS newer_register " +
            "    WHERE newer_register.groupKey = candidate.groupKey " +
            "      AND newer_register.opType IN ('PC_CARE_SLOT_REGISTER', 'PC_CARE_TASK_PROOF_REGISTER') " +
            "      AND (newer_register.createdAt > older.createdAt " +
            "        OR (newer_register.createdAt = older.createdAt AND newer_register.rowid > older.rowid)) " +
            "      AND (newer_register.createdAt < candidate.createdAt " +
            "        OR (newer_register.createdAt = candidate.createdAt AND newer_register.rowid < candidate.rowid)) " +
            "      AND newer_register.status IN ('QUEUED', 'IN_FLIGHT', 'SUCCEEDED')" +
            "  )) " +
            "AND (older.conflict = 1 " +
            "  OR older.attemptCount >= older.maxAttempts " +
            "  OR older.nextAttemptAt > :now)) " +
            "OR (candidate.opType IN ('PC_CARE_SLOT_REGISTER', 'PC_CARE_TASK_PROOF_REGISTER') " +
            "  AND older.opType = 'PROOF_UPLOAD' " +
            "  AND candidate.payloadJson LIKE '%\"proof_outbox_item_id\":\"' || older.id || '\"%' " +
            "  AND (older.status IN ('QUEUED', 'IN_FLIGHT') " +
            "    OR (older.status = 'FAILED' AND older.conflict = 0 AND older.attemptCount < older.maxAttempts)))" +
            "OR (candidate.opType = 'PC_CARE_TASK_SUBMIT' " +
            "  AND older.opType = 'PROOF_UPLOAD' " +
            "  AND EXISTS (" +
            "    SELECT 1 FROM outbox AS pending_register " +
            "    WHERE pending_register.groupKey = candidate.groupKey " +
            "      AND pending_register.opType IN ('PC_CARE_SLOT_REGISTER', 'PC_CARE_TASK_PROOF_REGISTER') " +
            "      AND pending_register.payloadJson LIKE '%\"proof_outbox_item_id\":\"' || older.id || '\"%' " +
            "      AND (pending_register.createdAt < candidate.createdAt " +
            "        OR (pending_register.createdAt = candidate.createdAt AND pending_register.rowid < candidate.rowid)) " +
            "      AND (pending_register.status IN ('QUEUED', 'IN_FLIGHT') " +
            "        OR (pending_register.status = 'FAILED' AND pending_register.conflict = 0 AND pending_register.attemptCount < pending_register.maxAttempts))" +
            "  ) " +
            "  AND (older.status IN ('QUEUED', 'IN_FLIGHT') " +
            "    OR (older.status = 'FAILED' AND older.conflict = 0 AND older.attemptCount < older.maxAttempts)))" +
            ") " +
            ") " +
            "ORDER BY candidate.createdAt ASC, candidate.rowid ASC LIMIT :limit",
    )
    suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity>

    /** Observes ACTIVE rows only (QUEUED, IN_FLIGHT, and still-retryable non-conflict FAILED) —
     *  never includes SUCCEEDED, dead-letter (conflict) rows, OR attempt-exhausted FAILED rows
     *  (`attemptCount >= maxAttempts`). An exhausted row is terminal (it will never be re-claimed —
     *  see [eligibleForDrain]'s `attemptCount < maxAttempts` guard), so keeping it here would leave
     *  it in the active set forever, unbounded-accumulating in memory. This bounds memory and query
     *  time. Backs the sync-status overlay (see `SyncRepository.observeStatus`).
     *
     *  DEPRECATED: Use [observeActiveCounts] for counts only (most UI use case) or
     *  [observeActiveWindow] for a bounded list. Full materialization violates bounded-memory rules. */
    @Query(
        "SELECT * FROM outbox WHERE status IN ('QUEUED', 'IN_FLIGHT') " +
            "OR (status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts) " +
            "ORDER BY createdAt ASC",
    )
    fun observeActive(): Flow<List<OutboxEntity>>

    /** Observes ACTIVE counts (QUEUED, IN_FLIGHT, FAILED non-conflict retryable) without materializing
     *  rows. Returns a data class with pending, inFlight, failed counts. Used for sync-status badge
     *  and health monitoring without memory overhead. */
    @Query(
        "SELECT " +
            "CAST(SUM(CASE WHEN status = 'QUEUED' THEN 1 ELSE 0 END) AS INTEGER) as queued, " +
            "CAST(SUM(CASE WHEN status = 'IN_FLIGHT' THEN 1 ELSE 0 END) AS INTEGER) as inFlight, " +
            "CAST(SUM(CASE WHEN status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts THEN 1 ELSE 0 END) AS INTEGER) as failed " +
            "FROM outbox",
    )
    fun observeActiveCounts(): Flow<ActiveOutboxCounts>

    /** Observes a bounded window of ACTIVE rows (newest-first, limited by [limit]) without
     *  unbounded growth. Excludes SUCCEEDED, terminal FAILED, and dead-letter rows.
     *  Use this instead of [observeActive] when you need a subset for UI display. */
    @Query(
        "SELECT * FROM outbox WHERE status IN ('QUEUED', 'IN_FLIGHT') " +
            "OR (status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts) " +
            "ORDER BY createdAt DESC LIMIT :limit",
    )
    fun observeActiveWindow(limit: Int): Flow<List<OutboxEntity>>

    /** Every ACTIVE row of ONE op type. Bounded by nature (a single feature's pending writes),
     *  unlike the cross-feature window: reconciliation guards (e.g. pending health-case opens)
     *  need the COMPLETE set for their op type — a newest-N window silently drops the oldest
     *  pending command once other features queue enough rows after it. */
    @Query(
        "SELECT * FROM outbox WHERE opType = :opType AND (status IN ('QUEUED', 'IN_FLIGHT') " +
            "OR (status = 'FAILED' AND conflict = 0 AND attemptCount < maxAttempts)) ORDER BY createdAt ASC",
    )
    fun observeActiveByOpType(opType: String): Flow<List<OutboxEntity>>

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

    /**
     * Re-opens a row that reached a TERMINAL failure (dead-letter conflict OR attempt-exhausted)
     * for a brand-new user-initiated submit under the SAME [OutboxEntity.idempotencyKey] — the
     * unique index otherwise silently drops a fresh enqueue whose key already belongs to a dead
     * row (device-proven defect: a task with one permanently-FAILED submit could never be
     * resubmitted). Resets the row to QUEUED with a fresh attempt budget and replaces the
     * payload/fingerprint with the caller's latest request (a resubmit may carry corrected data),
     * while the id and idempotencyKey never change, so the server still sees the same logical
     * write and any earlier idempotent replay semantics keep working.
     *
     * Guarded to ONLY apply to a genuinely terminal row: `status='FAILED' AND (conflict=1 OR
     * attemptCount>=maxAttempts)`. A row still inside its backoff window (retryable FAILED) or
     * QUEUED/IN_FLIGHT/SUCCEEDED is untouched — this is not a general-purpose row overwrite, only
     * the escape hatch for a row the drain loop will otherwise never touch again. Returns rows
     * affected: 1 = reopened, 0 = the row was not terminal (a concurrent transition already moved
     * it on — the caller re-reads and falls back to normal replay/conflict handling).
     */
    @Query(
        "UPDATE outbox SET status = 'QUEUED', attemptCount = 0, conflict = 0, lastError = NULL, " +
            "nextAttemptAt = :now, updatedAt = :now, payloadJson = :payloadJson, requestFingerprint = :fingerprint " +
            "WHERE id = :id AND status = 'FAILED' AND (conflict = 1 OR attemptCount >= maxAttempts)",
    )
    suspend fun reopenTerminalForRetry(id: String, payloadJson: String, fingerprint: String, now: Long): Int

    /**
     * Proof uploads are file-backed and user-replaceable: a retry/recovery may need to send the same
     * proof idempotency key with a refreshed processed-file payload or ordering group. Keep this
     * deliberately FAILED-only so queued/in-flight/succeeded rows cannot be mutated under a drain.
     */
    @Query(
        "UPDATE outbox SET status = 'QUEUED', groupKey = :groupKey, attemptCount = 0, conflict = 0, " +
            "lastError = NULL, nextAttemptAt = :now, updatedAt = :now, payloadJson = :payloadJson, " +
            "requestFingerprint = :fingerprint WHERE id = :id AND status = 'FAILED' AND opType = 'PROOF_UPLOAD'",
    )
    suspend fun reopenFailedProofUploadForRetry(
        id: String,
        groupKey: String,
        payloadJson: String,
        fingerprint: String,
        now: Long,
    ): Int

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
