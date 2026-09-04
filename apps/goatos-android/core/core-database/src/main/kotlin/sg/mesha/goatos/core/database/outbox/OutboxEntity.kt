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
    /** Raw verifier journey audit rows (`POST /verification/review-events`). */
    VERIFICATION_REVIEW_EVENTS,

    /**
     * THE VERIFIER'S WEIGHT CORRECTION (maintainer decision 2026-08-17):
     * `POST /app/weighing/observations/{id}/weight-correction`. She watches a weighing proof video
     * and replaces the number the operator typed.
     *
     * It rides the outbox like every other write in this app, so a correction made in a shed with no
     * signal is durable rather than lost. Adding an op type needs NO Room migration: [OutboxEntity.opType]
     * is a plain TEXT column holding this enum's `name`.
     *
     * Its idempotency key is derived from the observation AND the corrected values, never a
     * timestamp: a retry of the SAME correction must replay for free, while correcting to 12 kg and
     * then to 13 kg are two different acts that must not collide on one key.
     */
    WEIGHING_WEIGHT_CORRECTION,

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
     * Pen Reconciliation completion from the Herd Operations "Reconcile" tab
     * (`POST /app/counts/pen-reconciliation/cards/{card_id}/complete`).
     *
     * Records that the operator physically returned a strayed animal to its registered pen, with
     * the MANDATORY live-camera video resolved from a PROOF_UPLOAD row enqueued on the SAME group
     * (the CARD ID) that drains first — mirroring [SHIFTING_COMPLETE]'s mandatory-video coupling.
     * The write never rewrites the herd register; it flips the card to `pending_verification` for
     * the tenant verifier (there is deliberately NO approver step). Its caller derives a STABLE
     * idempotency key from the card id (never a timestamp-suffixed one), so a
     * server-committed-but-client-unrecorded retry returns the original result instead of queueing
     * a second verification. The CARD ID is the outbox group key so two actions on the same card
     * cannot drain concurrently or out of order.
     */
    PEN_RECONCILIATION_COMPLETE,

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

    /**
     * Feed WASTAGE completion (`POST /feed-direction/wastage/complete`), the verifier-GATED
     * leftover-feed flow on EXPERIMENT pens (maintainer decision 2026-08-18). Grain is the PEN-DAY
     * — no session, no workflow (the server stamps `experiment`). Like [FEED_PACKING_COMPLETE] it
     * carries a SINGLE MANDATORY video ref, resolved from a PROOF_UPLOAD row enqueued on the SAME
     * group that drains first. It flips the pen-day to `pending_verification` — nothing is
     * completed until a verifier approves. A pen-day accepts exactly ONE video, so a DIFFERENT
     * video is a 409 that terminalizes the row (surfaced to the operator, never retried); a
     * genuine re-send of the SAME video replays for free under the stable idempotency key. The
     * pen-day key is the outbox group key.
     */
    FEED_WASTAGE_COMPLETE,

    /**
     * THE VERIFIER'S WASTAGE MEASUREMENT (maintainer decision 2026-08-18):
     * `POST /feed-direction/wastage/{completion_id}/measurement`. She watches the pen's wastage
     * video and records the leftover weight she reads off it — the second producer-owned
     * measurement route after [WEIGHING_WEIGHT_CORRECTION], and it rides the outbox the same way
     * so a measurement made in a shed with no signal is durable rather than lost.
     *
     * Its idempotency key is derived from the completion AND the value, never a timestamp: a
     * retry of the SAME measurement must replay for free, while recording 3 kg and then 3.5 kg
     * are two different acts that must not collide on one key. ZERO IS A VALID VALUE (an empty
     * trough). Adding an op type needs NO Room migration: [OutboxEntity.opType] is a plain TEXT
     * column holding this enum's `name`.
     */
    FEED_WASTAGE_MEASUREMENT,
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

    /**
     * The diagnosis engine's two writes. SEPARATE op types, not one, for the same
     * reason the backend gives them separate routes and separate permissions:
     * submitting an observation PROPOSES and opens nothing, while confirming is
     * the only thing that starts a treatment course. One combined item could do
     * both, which is exactly the collapse the engine exists to prevent.
     */
    HEALTH_OBSERVATION_SUBMIT,
    HEALTH_DIAGNOSIS_CONFIRM,
    /** One clinical case closure (recovered / referred / canceled), health.diagnose only. */
    HEALTH_CASE_CLOSE,
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

    /**
     * Feed & water removal submit (`POST /app/weighing/fasting/{id}/submit`, maintainer decision
     * 2026-09-03): the second operator's TWO mandatory live-camera videos — feed removal and
     * water removal — recorded the evening before a weigh date. Adding an op type needs NO Room
     * migration: [OutboxEntity.opType] is a plain TEXT column holding this enum's `name`.
     *
     * The two videos ride by REFERENCE to their PROOF_UPLOAD outbox rows, each on its OWN
     * per-slot upload group (independent field work — one backed-off clip must not strand the
     * other), while THIS row sits on the task-grain group key and resolves each upload's proof id
     * at dispatch (the [FEED_DISTRIBUTION_COMPLETE] shape). A not-yet-uploaded clip suspends the
     * dispatch on the proof-dependency lane without burning retry budget; a permanently-failed
     * upload is terminal with an operator-facing reason. The idempotency key is STABLE per
     * (task, proof pair): a retry replays for free, while a post-rework re-shoot names new proofs
     * and is a genuinely new act under a new key.
     */
    WEIGHING_FASTING_SUBMIT,

    /**
     * PC Care RFID scan (`POST /app/pc-care/tasks/{task_id}/animals`, module pc_care, maintainer
     * decision 2026-08-21): one tag scanned into a care task, stored VERBATIM server-side. Its
     * caller derives a STABLE per-(task, normalized-tag) idempotency key (never a timestamp), so a
     * double-tap or a server-committed-but-client-unrecorded retry replays for free. A `409
     * duplicate_scan` — another phone already scanned this tag — is TERMINAL: the dispatch marks
     * the durable Room animal row DUPLICATE and the row is never re-enqueued under a new key.
     * Scans drain on their own per-task group so they never queue behind a video upload.
     */
    PC_CARE_SCAN_ADD,

    /**
     * PC Care slot proof registration
     * (`PUT /app/pc-care/tasks/{task_id}/animals/{animal_row_id}/proofs/{slot}`): attaches one
     * slot's live-camera video to one scanned animal. The video rides by REFERENCE to its coupled
     * PROOF_UPLOAD row on the SAME task group, which drains first (mirroring
     * [FEED_PACKING_COMPLETE]'s completion-references-upload coupling); the dispatcher resolves
     * the uploaded server proof id. The server animal_row_id may be unknown at enqueue time (the
     * scan may still be syncing) and is re-resolved from the Room animal row at dispatch — still
     * blank means a plain retryable wait, never a terminal failure.
     */
    PC_CARE_SLOT_REGISTER,

    /**
     * PC Care task-level proof registration
     * (`PUT /app/pc-care/tasks/{task_id}/proofs/{slot}`): attaches one fridge-stock proof to an
     * inventory_vaccine task. The video/photo rides by REFERENCE to its coupled PROOF_UPLOAD row
     * on the SAME task group, so upload drains before registration and before submit.
     */
    PC_CARE_TASK_PROOF_REGISTER,

    /**
     * PC Care task submit (`POST /app/pc-care/tasks/{task_id}/submit`): submits the WHOLE task —
     * refused until every scanned animal carries its full slot set. It shares the task group with
     * the PROOF_UPLOAD and [PC_CARE_SLOT_REGISTER] rows, so every proof resolves server-side
     * before the submit drains. Its caller derives a STABLE per-(task, row_version) idempotency
     * key, so a retry re-enqueues the SAME verification item instead of submitting twice, while a
     * post-rework re-submit (bumped row_version) is a genuinely new act under a new key.
     */
    PC_CARE_TASK_SUBMIT,

    /**
     * Toxin step completion (`POST /app/toxin/tasks/{task_id}/steps/{step_no}/complete`, module
     * toxin, maintainer decision 2026-08-25): records one guided aflatoxin-test step's proof. The
     * proof rides by REFERENCE to its coupled PROOF_UPLOAD row on the SAME task group, which
     * drains first (mirroring [PC_CARE_SLOT_REGISTER]); the dispatcher resolves the uploaded
     * server proof id into `proof_ref`. The idempotency key is STABLE per (task, step, proof
     * row) — a retry replays for free while a re-shoot is a new act. The server clock is the
     * gate: `422 wait_not_elapsed` / `409 step_already_done` are terminal, carry backend farm
     * copy, and the reconcile refreshes the task detail so the screen re-renders server state.
     * Adding an op type needs NO Room migration: [OutboxEntity.opType] is a plain TEXT column.
     */
    TOXIN_STEP_COMPLETE,

    /**
     * Toxin reading submit (`POST /app/toxin/tasks/{task_id}/submit`): step 7's strip PHOTO +
     * outcome reading. Same task group as [TOXIN_STEP_COMPLETE] and the PROOF_UPLOAD rows, so
     * the strip photo's upload drains strictly before the submit that references it. STABLE
     * per-(task, outcome, photo row) idempotency key — never timestamp-suffixed.
     */
    TOXIN_SUBMIT,

    /**
     * Clock In / Clock Out punches (`POST /app/clock/in` / `/app/clock/out`, module clock,
     * maintainer decision 2026-08-27 — docs/features/clock-in-out/plan.md). Adding an op type
     * needs NO Room migration: [OutboxEntity.opType] is a plain TEXT column holding this enum's
     * `name`.
     *
     * One in/out pair exists per IST business day, so the idempotency key is STABLE and
     * day-scoped — `clock:<business_date>:<in|out>`, minted at tap time, never a timestamp — and
     * the groupKey is `clock:<business_date>` so a day's in and out drain strictly in order and
     * never concurrently. A 409 (`already_clocked_in` / `not_clocked_in` / `already_clocked_out`)
     * is a definitive server answer about a day that already has that punch: terminal by
     * `isTerminalAppApiError`, surfaced once, never retried. A 422 `mock_location_detected` is
     * likewise terminal — the server independently refuses a payload admitting a mock fix or an
     * installed fake-GPS app, and re-sending the same payload can never succeed.
     */
    CLOCK_IN,
    CLOCK_OUT,

    /**
     * Vendors module (maintainer decision 2026-09-03): a vendor recorded on the phone
     * (`POST /procurement/vendors`). Its optional voice note rides ahead of it as a PROOF_UPLOAD
     * on the same per-vendor group, so the upload drains first and the dispatcher resolves the
     * server proof id. The route has no idempotency header; the register's natural key refuses a
     * duplicate with `409 vendor_duplicate`, which a replay reads as "already there".
     */
    VENDOR_CREATE,

    /** A feed purchase recorded on the phone (`POST /procurement/feed-purchases`, Idempotency-Key). */
    FEED_PURCHASE_CREATE,

    /** A sale recorded on the phone (`POST /sales/deals`, Idempotency-Key; maintainer instruction 2026-09-04). */
    SALES_DEAL_CREATE,

    /**
     * EDITING a recorded sale (maintainer instruction 2026-09-04): a buyer receipt added, changed
     * or removed. ONE op type for the three verbs rather than three, because they share a payload,
     * a lane and a reconcile -- the verb rides in the payload's `op`. Every one returns the WHOLE
     * updated deal, so the sync pass writes the server's row into the ledger and detail caches.
     */
    SALES_DEAL_PAYMENT_WRITE,

    /** The deal's status word moved (`POST /sales/deals/{id}/status`); returns the whole deal. */
    SALES_DEAL_STATUS_SET,

    /**
     * Pipeline and evidence entry (`/sales/buyer-leads`, `/sales/fpo-leads`,
     * `/sales/market-benchmarks`, `/sales/sold-tags`, `/sales/weight-checks`): the five panels the
     * retired Sales DB sheet carried. ONE op type with the panel in the payload's `kind`, for the
     * SALES_DEAL_PAYMENT_WRITE reason -- one lane, one dispatch, one reconcile.
     */
    SALES_PIPELINE_WRITE,
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
