package sg.mesha.goatos.core.database.outbox

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/**
 * Write kinds the outbox knows how to drain. One row per attempted operation; see
 * `SyncEngine.dispatch` (`:core:core-data`) for the per-type app-api call.
 */
enum class OutboxOpType {
    SHED_SUBMIT,
    SCAN_CAPTURE,
    SCAN_ATTEMPT,
    PROOF_UPLOAD,
    RESCHEDULE,
    VERIFY_TASK,
    REWORK_TASK,
    VERIFICATION_VERDICT,
    VERIFICATION_CLOSE,
    VERIFICATION_CLOSE_SUBMISSION,

    /**
     * Counts vertical writes (`POST /app/counts/…-events`). Adding an op type needs NO Room
     * migration: [OutboxEntity.opType] is a plain TEXT column holding this enum's `name`, and
     * `SyncEngine.dispatch` resolves it back with `OutboxOpType.valueOf`.
     *
     * All three are must-not-double-submit writes — a duplicated birth invents an animal, a
     * duplicated death exits one twice, a duplicated shifting double-counts a movement — so
     * their callers derive a STABLE idempotency key persisted in `SavedStateHandle`, never a
     * timestamp-suffixed one.
     */
    COUNTS_SHIFTING,
    COUNTS_BIRTH,
    COUNTS_DEATH,

    /**
     * Counts lifecycle APPROVAL decisions (`POST /app/counts/approvals/{id}/{approve,reject}`).
     *
     * These are the writes that actually change the herd: approving a birth creates the kid and
     * generates its vaccination obligations, approving a death exits the animal and cancels its
     * open obligations, and approving a shifting relocates the named animals and re-scopes their
     * shed-scoped obligations. A double-applied decision is therefore not a cosmetic duplicate —
     * so, like the three writes above, their callers derive a STABLE `SavedStateHandle`-persisted
     * idempotency key and never a timestamp-suffixed one.
     */
    COUNTS_APPROVAL_APPROVE,
    COUNTS_APPROVAL_REJECT,

    /**
     * Shifting EXECUTION from the operator's Pending tab
     * (`POST /app/counts/shifting-events/{id}/{complete,cancel}`).
     *
     * `SHIFTING_COMPLETE` is the "Mark done" that RELOCATES the animals — it flips the movement to
     * applied, rewrites their shed/stage, and publishes goat.location.changed / goat.stage_changed.
     * Completing twice would be a double relocation, so its caller derives a STABLE idempotency key
     * from the movement id (never a timestamp-suffixed one); under that key a
     * server-committed-but-client-unrecorded retry returns the original relocation instead of moving
     * the herd onward. The SHIFTING EVENT ID is the outbox group key so two actions on the same
     * movement can never drain concurrently or out of order. `SHIFTING_CANCEL` retires an authorized
     * movement and moves nothing; same stable-key + group-key contract.
     */
    SHIFTING_COMPLETE,
    SHIFTING_CANCEL,

    /**
     * Counts identifier PROMOTE from the operator's "Awaiting RFID" list
     * (`POST /app/counts/goats/{goat_id}/promote-identifier`).
     *
     * Assigns a permanent RFID to a temporary-tagged goat, atomically retiring the temp tag.
     * Promoting twice would attempt a second retag under a different RFID, so its caller derives a
     * STABLE idempotency key from the goat id (never a timestamp-suffixed one); under that key a
     * server-committed-but-client-unrecorded retry returns the original promotion instead of
     * retagging onward. The GOAT ID is the outbox group key so two promotes of the same goat can
     * never drain concurrently or out of order.
     */
    COUNTS_PROMOTE_IDENTIFIER,
}

/**
 * Outbox row lifecycle. Deliberately only these four states (the task's explicit ask) —
 * the finer-grained states in `docs/mobile/system-design.md`'s sync state machine
 * (UPLOADING_MEDIA/POSTING/BACKOFF/DEAD_LETTER/ACKED/CONFLICT) are derived at the
 * `:core:core-data` layer from these four plus [OutboxEntity.conflict]/[OutboxEntity.attemptCount]
 * (see `SyncQueueItem.isDeadLetter`), not stored as separate DB states.
 */
enum class OutboxStatus { QUEUED, IN_FLIGHT, SUCCEEDED, FAILED }

/** Default retry budget before a transport-failing row is treated as dead-letter. */
const val DEFAULT_MAX_ATTEMPTS = 8

/**
 * A single queued, at-least-once write to the backend app-api. Rows are never mutated
 * to point at a different [idempotencyKey], [requestFingerprint], or [payloadJson] after
 * insert — a retry (automatic backoff or manual [OutboxStatus.FAILED] retry) reuses this
 * exact row.
 *
 * [groupKey] is the ordering/concurrency partition (e.g. a shed id): the sync engine
 * drains items within the same [groupKey] strictly oldest-first by [createdAt] (TRD:
 * outbox is "ordered per shed"), while different groups may drain concurrently.
 */
@Entity(
    tableName = "outbox",
    indices = [
        Index(value = ["idempotencyKey"], unique = true),
        // Backs OutboxDao.eligibleForDrain's status/backoff-window scan so the drain query
        // stays an index range-scan, not a full-table scan, as outbox rows accumulate.
        Index(value = ["status", "nextAttemptAt"]),
    ],
)
data class OutboxEntity(
    @PrimaryKey val id: String,
    val opType: String,
    val groupKey: String,
    val idempotencyKey: String,
    val payloadJson: String,
    val status: String,
    val attemptCount: Int = 0,
    val maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
    /** true = the LAST attempt was a definitive server rejection (e.g. failed submission
     *  validation), not a transport/backoff failure. A conflict row is always terminal
     *  regardless of [attemptCount] and is never auto-retried by the drain query — only
     *  an explicit manual retry re-arms it. */
    val conflict: Boolean = false,
    val createdAt: Long,
    val updatedAt: Long,
    /** Epoch millis; the row is not eligible for drain again until this passes
     *  (exponential backoff). `Long.MAX_VALUE` marks a row terminal. */
    val nextAttemptAt: Long = 0L,
    val lastError: String? = null,
    /** Raw JSON of the last successful app-api response — lets the UI layer decode the
     *  original server result on an idempotent-replay read without a second network call. */
    val resultJson: String? = null,
    /** SHA-256 over the semantic request envelope (op type + group key + payload JSON).
     *  Exact same-key replays return this row; same-key/different-payload attempts are
     *  rejected before the old row can hide a changed write. */
    val requestFingerprint: String = "",
)
