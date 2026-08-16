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
    VERIFICATION_CLOSE_BATCH,

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
     * These are herd-changing writes: approving a birth creates the kid and generates its
     * vaccination obligations, approving a death exits the animal and cancels its open obligations,
     * and approving a shifting relocates the named animals only when operator completion already
     * exists. Otherwise it records the independent approval gate and completion performs the move
     * later. A double-applied decision is therefore not a cosmetic duplicate —
     * so, like the three writes above, their callers derive a STABLE `SavedStateHandle`-persisted
     * idempotency key and never a timestamp-suffixed one.
     */
    COUNTS_APPROVAL_APPROVE,
    COUNTS_APPROVAL_REJECT,

    /**
     * Shifting EXECUTION from the operator's Pending tab
     * (`POST /app/counts/shifting-events/{id}/{complete,cancel}`).
     *
     * `SHIFTING_COMPLETE` records the operator's mandatory live-camera video and completion gate.
     * If Park Head approval already exists, that same transaction relocates the animals; otherwise
     * the event stays pending and approval performs the move later. The transaction recording the
     * second gate flips the movement to applied, rewrites shed/stage, and publishes location/stage
     * events. Its caller derives a STABLE idempotency key from the movement id (never a timestamp-
     * suffixed one), so a server-committed-but-client-unrecorded retry cannot apply twice. The
     * SHIFTING EVENT ID is the outbox group key so two actions on the same movement cannot drain
     * concurrently or out of order. `SHIFTING_CANCEL` retires an un-applied movement and moves
     * nothing; same stable-key + group-key contract.
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

    /**
     * Feed direction completion (`POST /feed-direction/complete`): an operator marks one shed-session
     * as fed. The completion carries only the shed-session identity; the OPTIONAL video flows
     * separately through [PROOF_UPLOAD] (mirroring [SHIFTING_COMPLETE]). Its caller derives a STABLE
     * idempotency key so a server-committed-but-client-unrecorded retry returns the original
     * completion, and the shed-session natural key makes a second completion a backend no-op. The
     * shed-session key is the outbox group key so two completions of the same shed-session drain
     * strictly oldest-first.
     */
    FEED_DIRECTION_COMPLETE,

    /**
     * Feed DISTRIBUTION completion (`POST /feed-direction/distribution/complete`), the
     * verifier-GATED direction flow (docs/decisions/feed-distribution-verification.md). Distinct
     * from [FEED_DIRECTION_COMPLETE] (the untouched packing path): it carries a MANDATORY
     * feed-distribution video ref AND a MANDATORY water-distribution proof ref (photo or video),
     * both resolved from PROOF_UPLOAD rows enqueued on the SAME group that drain first (mirroring
     * [SHIFTING_COMPLETE]'s mandatory-video coupling). It flips the shed-session to
     * `pending_verification` — nothing is completed until a verifier approves the pair. Its caller
     * derives a STABLE idempotency key so a server-committed-but-client-unrecorded retry re-enqueues
     * the SAME verification item instead of completing twice. The shed-session key is the outbox
     * group key so two completions of the same shed-session drain strictly oldest-first.
     */
    FEED_DISTRIBUTION_COMPLETE,

    /**
     * Feed PACKING completion (`POST /feed-direction/packing/complete`), the verifier-GATED packing
     * flow. Distinct from [FEED_DIRECTION_COMPLETE] (the untouched instant packing/direction path)
     * and simpler than [FEED_DISTRIBUTION_COMPLETE]: it carries a SINGLE MANDATORY packing video ref,
     * resolved from a PROOF_UPLOAD row enqueued on the SAME group that drains first (mirroring
     * [SHIFTING_COMPLETE]'s mandatory-video coupling). It flips the shed-session to
     * `pending_verification` — nothing is completed until a verifier approves. Its caller derives a
     * STABLE idempotency key so a server-committed-but-client-unrecorded retry re-enqueues the SAME
     * verification item instead of completing twice. The shed-session key is the outbox group key so
     * two completions of the same shed-session drain strictly oldest-first.
     */
    FEED_PACKING_COMPLETE,
    /** Shed-day milk preparation; carries 2 or 5 proof-upload row references as one submission. */
    MILK_PREPARATION_SUBMIT,
    /** One shed-session Milk Feeding answer cascade plus two proof-upload references. */
    MILK_FEEDING_SUBMIT,
    FEED_TRANSPORT_SUBMIT,

    /**
     * Birth/Death follow-up workflow action writes (docs/decisions/birth-death-workflows.md):
     * `POST /app/workflows/{workflow_id}/actions/{action_id}/answer` (question / question_select)
     * and `…/complete` (action type; `proof_ref` MANDATORY when the action `requires_video`).
     *
     * Both are must-not-double-apply writes — the backend rejects a NEW key against an
     * already-completed action with 409 — so their callers derive a STABLE per-action idempotency
     * key (never a timestamp-suffixed one); an exact replay returns the original result with
     * `idempotent_replay=true`. The WORKFLOW ID is the outbox group key so two actions on the same
     * workflow drain strictly oldest-first (and a `requires_video` completion drains AFTER its
     * coupled PROOF_UPLOAD row on the same group, exactly like [SHIFTING_COMPLETE]).
     */
    WORKFLOW_ACTION_ANSWER,
    WORKFLOW_ACTION_COMPLETE,
    /** Opens one disease course for a goat with a stable SavedStateHandle-persisted key. */
    HEALTH_CASE_OPEN,
    /** One idempotent Health treatment-session completion. */
    HEALTH_TREATMENT_COMPLETE,
    WEIGHING_ANIMAL_OBSERVATION,
    WEIGHING_SHED_OBSERVATION,

    /**
     * Weighing scope SUBMIT transition (`POST /app/weighing/campaigns/{id}/sheds/{id}/submit`).
     * Previously a direct, non-durable HTTP call from `WeighingRepository.submitIndividualScope`:
     * a killed process or a dropped connection mid-call lost the write entirely, with no retry and
     * no record it was ever attempted — the operator's confirm tap vanished. Routing it through the
     * outbox like every other weighing/observation write gives it the same durability + backoff
     * retry as the rest of the module. [idempotencyKey] is the existing Room-backed transition-epoch
     * key (`WeighingRepository.transitionIdempotencyKey`), unchanged by this move: the epoch only
     * advances after the row reaches [sg.mesha.goatos.core.database.outbox.OutboxStatus.SUCCEEDED],
     * so a retried attempt still dedupes server-side. The CAMPAIGN_SHED id is the outbox group key,
     * so two submit attempts for the same shed drain strictly oldest-first.
     */
    WEIGHING_SCOPE_SUBMIT,
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
