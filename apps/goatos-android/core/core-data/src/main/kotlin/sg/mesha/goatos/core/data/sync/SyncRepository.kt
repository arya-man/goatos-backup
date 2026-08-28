package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.common.OutboxTelemetryEvent
import sg.mesha.goatos.core.common.OutboxTelemetryReporter
import sg.mesha.goatos.core.common.OutboxWritePhase
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.ActiveOutboxCounts
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.ClockPunchRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.FeedWastageMeasurementRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationDecision
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto
import sg.mesha.goatos.core.network.dto.VerificationReviewEventBatchRequestDto
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingWeightCorrectionRequestDto
import java.security.MessageDigest
import java.util.UUID

/** enqueueClockPunch budget: rides out days of dead network at the server-capped backoff (decision D1). */
private const val CLOCK_PUNCH_MAX_ATTEMPTS = 1000

/**
 * ## Sync engine — public integration point
 *
 * [observeStatus] is the ONE thing a UI needs: a hot [StateFlow] of [SyncStatus]
 * (connectivity + pending/in-flight/failed/dead-letter counts + last-sync time + the
 * per-item list). It updates live as items are enqueued and drained — collect it with
 * `collectAsStateWithLifecycle()`, no polling. This is the integration point for the
 * sync-status overlay (`ui/Overlays.kt`, owned by a separate agent — currently rendering
 * fake data; wire it to `observeStatus()` here).
 *
 * The `enqueue*` methods are how a feature ViewModel writes WITHOUT calling
 * [sg.mesha.goatos.core.network.AppApi] inline: the call returns as soon as the write is
 * durably queued (optimistic UI), and the caller filters [SyncStatus.items] by the returned
 * id to follow that specific item's status afterward (see `SubmitViewModel` for the pattern
 * — enqueue, then collect `observeStatus()` filtered to the returned id).
 *
 * ### Idempotency-key strategy
 * The CALLER derives or generates the idempotency key ONCE for the logical write (for a shed
 * submit, `SubmitViewModel` derives it from the task id) and persists it alongside the draft
 * (not just in a local var that a ViewModel recreation would lose) so the SAME key is passed
 * to `enqueue*` on every resend.
 * This port never mints a new key internally:
 * - a repeat `enqueue*` call with a key that already has a row is an idempotent no-op ONLY
 *   when the operation, group, and payload fingerprint match — the EXISTING row's id is
 *   returned, never a duplicate insert (enforced by a unique index on `idempotencyKey`, see
 *   `core-database`'s `OutboxEntity`);
 * - a same-key/different-payload attempt is rejected before it can hide a changed write behind
 *   an older queued/synced row;
 * - [retry] re-arms an existing row for another attempt using its already-stored key and
 *   payload; it never takes or generates a new key;
 * - [SyncEngine] reuses the stored key on every automatic backoff retry too.
 *
 * If a write cannot even be queued (e.g. a storage error), `enqueue*` returns
 * [AppResult.Err] — it is never silently dropped.
 */
interface SyncRepository {
    fun observeStatus(): StateFlow<SyncStatus>

    /**
     * Active, locally durable Health reports that do not have backend-created treatment sessions
     * yet. Lightweight fakes default to an empty stream; production projects these directly from
     * Room's outbox so an offline report remains visible after navigation or process recreation.
     */
    fun observePendingHealthCaseOpens(): Flow<List<PendingHealthCaseOpen>> = flowOf(emptyList())

    /**
     * Grain keys of every submit still ALIVE in the outbox — the optimistic "In review" badge.
     *
     * Derived from the outbox instead of an in-memory set on purpose: a row that succeeds or dies
     * leaves the active set by itself, so there is nothing to clear, no second key to disagree with,
     * and nothing lost to process death. See [submittedGrainKeyOf].
     */
    fun observeSubmittedForReviewGrains(): Flow<Set<String>> = flowOf(emptySet())

    /** Observes a specific outbox item by id (R50-006: leadership close needs to observe items
     *  that may be older than the recent-terminal window). Returns a Flow that emits whenever
     *  the item's status changes, never emitting null (item not found = no emission). */
    fun observeItem(itemId: String): Flow<SyncQueueItem?>

    /** Enqueues a shed-submit write (`POST /app/tasks/{task_id}/submissions`). [groupKey]
     *  orders same-shed writes FIFO (TRD: outbox is "ordered per shed"); different groups
     *  may drain concurrently. Returns the outbox row id to follow via [observeStatus]. */
    suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String>

    /** Enqueues a draft RFID scan write (`POST /app/tasks/{task_id}/scan-captures`).
     *  One stable idempotency key per task/field/tag makes repeat scans a no-op locally and
     *  server-side. */
    suspend fun enqueueScanCapture(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        partitionKey: String = "whole",
        request: ScanCaptureRequestDto,
    ): AppResult<String> = AppResult.Err("scan capture sync is not configured")

    /** Enqueues an append-only RFID reader attempt audit event. This is separate from
     *  [enqueueScanCapture]: attempts include duplicate alias, not-due, and unknown scans and
     *  never drive Submit counters. */
    suspend fun enqueueScanAttempt(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): AppResult<String> = AppResult.Err("scan attempt sync is not configured")

    /** Enqueues a reschedule write (`POST /app/vaccination/obligations/{id}/reschedule`). */
    suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): AppResult<String>

    /** Enqueues a captured-proof registration write (`POST /app/proofs/uploads`), followed by
     *  the binary PUT of [localFilePath]'s bytes and the completion call
     *  (`POST /app/proofs/{proof_id}/complete`) — all three steps run as ONE outbox dispatch
     *  (see [SyncEngine.dispatchProofUpload]), so the row only reaches SYNCED once the video is
     *  actually durable server-side, not just registered. [localFilePath] is this app's own
     *  private-storage path for the captured video; [durationMs] is the capture's measured
     *  duration (freshness metadata, docs/mobile/proof-capture-sync-and-e2e.md "Camera-only
     *  capture"). */
    suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String>

    /** Enqueues a leadership verify action on a record task (C35-011). */
    suspend fun enqueueVerifyTask(
        taskId: String,
        reason: String,
        rowVersion: Int,
    ): AppResult<String>

    /** Enqueues a leadership rework action on a record task (C35-011). */
    suspend fun enqueueReworkTask(
        taskId: String,
        reason: String,
        rowVersion: Int,
    ): AppResult<String>

    /** Enqueues the standalone Verifier section's approve/reject + reason verdict
     *  (context/architecture/verifier-app-and-flow.md). [reason] is mandatory for
     *  `decision = "rejected"` — enforced by the caller (VerifyDetailViewModel) before this
     *  is ever called, mirrored server-side. [groupKey] is the verification item id so two
     *  verdicts on the SAME item never race out of order; different items drain concurrently.
     *
     *  [measurement] is THE NUMBER SHE READ OFF THE VIDEO (maintainer decision 2026-08-20),
     *  travelling WITH the approve instead of as a second write. It rides this one durable outbox
     *  row, so a shed with no signal queues one act rather than two that can drain apart — and the
     *  approve can no longer be fenced out by the version bump its own earlier save caused. Null on
     *  a reject and on the normal weighing approve, where blank keeps the operator's weight. */
    suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
        measurement: VerificationVerdictMeasurementDto? = null,
    ): AppResult<String>

    /**
     * THE VERIFIER'S WEIGHT CORRECTION (maintainer decision 2026-08-17): she replaces the weight the
     * operator typed, while she watches the proof video.
     *
     * [animalCount] is LUMP-SUM ONLY and null means "leave the recorded count alone" -- it is omitted
     * from the request rather than sent as 0, because the backend REFUSES a head count on an
     * individual capture instead of ignoring it. [observationId] is the outbox group key so two
     * corrections of the same record never drain out of order.
     */
    suspend fun enqueueWeighingWeightCorrection(
        observationId: String,
        refType: String,
        weightKg: Double,
        animalCount: Int?,
        reason: String?,
    ): AppResult<String> = AppResult.Err("Correcting a weight is not available.")

    /** Queues leadership closure after verifier approval; stable per item row version. */
    suspend fun enqueueVerificationClose(
        itemId: String,
        rowVersion: Int,
    ): AppResult<String> = AppResult.Err("Leadership closure is not available.")

    /** Queues one atomic leadership closure for a fully verifier-approved drive submission. */
    suspend fun enqueueVerificationSubmissionClose(
        submissionId: String,
    ): AppResult<String> = AppResult.Err("Leadership closure is not available.")

    /** Queues one atomic leadership closure for a fully reviewed vaccination batch/drive. */
    suspend fun enqueueVerificationBatchClose(
        batchId: String,
    ): AppResult<String> = AppResult.Err("Leadership closure is not available.")

    /** Enqueues verifier journey audit rows durably so offline/process-death does not drop them. */
    suspend fun enqueueVerificationReviewEvents(
        groupKey: String,
        idempotencyKey: String,
        request: VerificationReviewEventBatchRequestDto,
    ): AppResult<String> = AppResult.Err("verification review audit sync is not configured")

    /**
     * Enqueues an operator-reported shifting/movement write (`POST /app/counts/shifting-events`).
     *
     * [groupKey] is the DESTINATION SHED id: two movements into the same shed drain strictly
     * oldest-first, while movements into different sheds drain concurrently.
     *
     * [idempotencyKey] must be a STABLE key the caller derived once and persisted (SavedStateHandle),
     * never a timestamp-suffixed one — the backend derives the movement's logical key from it, so a
     * fresh key on resend would record a SECOND movement instead of collapsing onto the first.
     */
    suspend fun enqueueCountsShifting(
        groupKey: String,
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): AppResult<String> = AppResult.Err("counts shifting sync is not configured")

    /**
     * Enqueues a clock punch (`POST /app/clock/in` / `/app/clock/out`, module clock — maintainer
     * decision 2026-08-27). [idempotencyKey] is the STABLE day-scoped `clock:<business_date>:<in|out>`
     * key minted at tap; [groupKey] is `clock:<business_date>` so a day's in and out drain in
     * order and never concurrently. A 409 about a day that already holds that punch is terminal
     * and never retried.
     */
    suspend fun enqueueClockPunch(
        clockIn: Boolean,
        groupKey: String,
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): AppResult<String> = AppResult.Err("clock punch sync is not configured")

    /**
     * Enqueues a birth write (`POST /app/counts/birth-events`). [groupKey] is the newborn's
     * identity (its primary tag), so repeat writes about the same animal stay ordered.
     * [idempotencyKey] carries the same stable-key requirement as [enqueueCountsShifting]: a
     * duplicate here would invent a second animal.
     */
    suspend fun enqueueCountsBirth(
        groupKey: String,
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): AppResult<String> = AppResult.Err("counts birth sync is not configured")

    /**
     * Enqueues a death write (`POST /app/counts/death-events`) through identity's guardrailed
     * critical-death exit. [groupKey] is the goat id. Same stable-key requirement: the write also
     * carries a `row_version` optimistic-concurrency guard, so a replay under a NEW key would be
     * rejected as a stale-version conflict rather than deduplicated.
     */
    suspend fun enqueueCountsDeath(
        groupKey: String,
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): AppResult<String> = AppResult.Err("counts death sync is not configured")

    /**
     * Enqueues a Counts lifecycle APPROVAL decision
     * (`POST /app/counts/approvals/{request_id}/{approve,reject}`).
     *
     * [approve] selects the endpoint. [reason] is REQUIRED when rejecting (enforced by the caller
     * before this is reached, and again server-side and in the database) and optional when
     * approving.
     *
     * [groupKey] is the approval request id, so two decisions on the SAME request drain strictly
     * oldest-first and never race; decisions on different requests drain concurrently.
     *
     * [idempotencyKey] must be a STABLE key the caller derived once and persisted
     * (`SavedStateHandle`), never a timestamp-suffixed one. This is the write where that matters
     * most: approving APPLIES the effect, so a fresh key on resend would create a second kid, exit
     * an animal twice, or relocate a herd twice.
     */
    suspend fun enqueueCountsApprovalDecision(
        requestId: String,
        approve: Boolean,
        reason: String?,
        idempotencyKey: String,
    ): AppResult<String> = AppResult.Err("counts approval sync is not configured")

    suspend fun enqueueWeighingAnimalObservation(
        campaignId: String,
        groupKey: String,
        idempotencyKey: String,
        request: WeighingAnimalObservationRequestDto,
    ): AppResult<String> = AppResult.Err("weighing animal observation sync is not configured")

    suspend fun enqueueWeighingShedObservation(
        campaignId: String,
        groupKey: String,
        idempotencyKey: String,
        request: WeighingShedObservationRequestDto,
    ): AppResult<String> = AppResult.Err("weighing shed observation sync is not configured")

    /** Enqueues a weighing scope SUBMIT transition (`POST
     *  /app/weighing/campaigns/{id}/sheds/{id}/submit`), durable and retryable like every other
     *  outbox write instead of the direct, at-most-once HTTP call this replaced. [groupKey] is the
     *  campaign-shed id, so two submit attempts for the same shed drain strictly oldest-first.
     *  [idempotencyKey] MUST be the caller's existing Room-backed transition-epoch key
     *  (`WeighingRepository.transitionIdempotencyKey`) — never a fresh one per call — so a
     *  server-committed-but-client-unrecorded retry dedupes instead of double-submitting. */
    suspend fun enqueueWeighingScopeSubmit(
        campaignId: String,
        campaignShedId: String,
        groupKey: String,
        idempotencyKey: String,
        request: WeighingScopeSubmitRequestDto,
    ): AppResult<String> = AppResult.Err("weighing scope submit sync is not configured")

    /**
     * Enqueues a Shifting EXECUTION "Mark done" (`POST /app/counts/shifting-events/{id}/complete`) —
     * the write that RELOCATES the animals.
     *
     * [groupKey] is the shifting event id, so two actions on the SAME movement drain strictly
     * oldest-first and never race; different movements drain concurrently.
     *
     * [idempotencyKey] must be a STABLE key the caller derived once and persisted
     * (`SavedStateHandle`), never a timestamp-suffixed one. This is a must-not-double-apply write:
     * a fresh key on resend would relocate the herd twice. Under the stable key the backend returns
     * the original relocation with `idempotent_replay=true`.
     *
     * [destinationTag] is normally null (the server derives the destination cohort). The optional
     * video is NOT sent here — it goes through [enqueueProofUpload] against the destination shed.
     */
    suspend fun enqueueShiftingComplete(
        groupKey: String,
        idempotencyKey: String,
        destinationTag: String? = null,
        proofOutboxItemId: String,
        feedPackingProofOutboxItemId: String? = null,
        feedGivenProofOutboxItemId: String? = null,
        feedConfigFingerprint: String? = null,
    ): AppResult<String> = AppResult.Err("shifting completion sync is not configured")

    /**
     * Enqueue a feed-direction shed-session completion. [groupKey] is the shed-session key so two
     * completions of the same shed-session drain strictly oldest-first. The optional video is a
     * SEPARATE [enqueueProofUpload], not carried here.
     */
    suspend fun enqueueFeedDirectionComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
    ): AppResult<String> = AppResult.Err("feed completion sync is not configured")

    /**
     * Enqueue a verifier-GATED feed-DISTRIBUTION completion
     * (`POST /feed-direction/distribution/complete`, docs/decisions/feed-distribution-verification.md).
     * BOTH proofs are MANDATORY and passed by REFERENCE to their PROOF_UPLOAD outbox rows
     * ([feedWeightProofOutboxItemId] = feed-weight photo, [distributionProofOutboxItemId] =
     * feed-distribution video, [waterProofOutboxItemId] = water
     * photo/video): the dispatcher resolves each uploaded proof_id by outbox id and sends the pair,
     * exactly like [enqueueShiftingComplete] resolves its mandatory evidence set. The completion stays
     * on the shed-session [groupKey]; feed distribution proof uploads may use slot-specific groups so
     * one backed-off upload does not strand the other proof slots. If a referenced proof has not
     * uploaded yet, dispatch retries until that outbox row is `SUCCEEDED`.
     * [idempotencyKey] must be a STABLE caller-persisted key so a resend re-enqueues the SAME
     * verification item instead of completing twice. This is SEPARATE from
     * [enqueueFeedDirectionComplete] (the untouched packing path).
     */
    suspend fun enqueueFeedDistributionComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        distributionProofOutboxItemId: String?,
        feedWeightProofOutboxItemId: String?,
        waterProofOutboxItemId: String?,
        // SERVER proof ids for slots shot on ANOTHER operator's phone, which have no local outbox
        // row here. Null for a slot this phone recorded itself.
        feedWeightProofRef: String? = null,
        distributionProofRef: String? = null,
        waterProofRef: String? = null,
    ): AppResult<String> = AppResult.Err("feed distribution completion sync is not configured")

    /**
     * Enqueue a verifier-GATED feed-PACKING completion (`POST /feed-direction/packing/complete`).
     * Simpler than [enqueueFeedDistributionComplete]: a SINGLE proof is MANDATORY and passed by
     * REFERENCE to its PROOF_UPLOAD outbox row ([packingProofOutboxItemId]): the dispatcher resolves
     * the uploaded proof_id and sends it, exactly like [enqueueShiftingComplete] resolves its single
     * mandatory video. Both writes MUST share the same [groupKey] (the shed-session) so the proof
     * drains strictly before this completion. [idempotencyKey] must be a STABLE caller-persisted key
     * so a resend re-enqueues the SAME verification item instead of completing twice. This is
     * SEPARATE from both [enqueueFeedDirectionComplete] (the untouched instant packing path) and
     * [enqueueFeedDistributionComplete] (the two-proof distribution path).
     */
    suspend fun enqueueFeedPackingComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        packingProofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("feed packing completion sync is not configured")

    /**
     * Enqueue a verifier-GATED feed-WASTAGE completion (`POST /feed-direction/wastage/complete`,
     * maintainer decision 2026-08-18). Grain is the PEN-DAY on an EXPERIMENT pen — no session, no
     * workflow. ONE MANDATORY proof passed by REFERENCE to its PROOF_UPLOAD outbox row
     * ([wastageProofOutboxItemId]); both writes MUST share the same [groupKey] (the pen-day) so
     * the proof drains strictly before this completion. [idempotencyKey] must be a STABLE
     * caller-persisted key so a resend replays instead of completing twice; a DIFFERENT video for
     * a pen-day that already holds one is a 409 the drain surfaces as terminal, never retries.
     */
    suspend fun enqueueFeedWastageComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        targetDate: String,
        wastageProofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("feed wastage completion sync is not configured")

    /**
     * THE VERIFIER'S WASTAGE MEASUREMENT (maintainer decision 2026-08-18): she records the
     * leftover weight she reads off a pen's wastage video, in kg. ZERO IS VALID — an empty trough
     * is a real measurement. [completionId] comes from the verification item's own
     * `measurement_correction.observation_id` and is the outbox group key so two measurements of
     * the same pen-day never drain out of order — the last one she made must win.
     */
    suspend fun enqueueFeedWastageMeasurement(
        completionId: String,
        wastageKg: Double,
    ): AppResult<String> = AppResult.Err("Recording a wastage weight is not available.")

    /**
     * Enqueues one PC Care RFID scan (`POST /app/pc-care/tasks/{task_id}/animals`, module
     * pc_care). The idempotency key is [pcCareScanIdempotencyKey] — STABLE per (task, normalized
     * tag), so a resend replays for free — and the group is [pcCareScanGroupKey], deliberately
     * separate from the task's video-upload group so a scan never queues behind a clip.
     */
    suspend fun enqueuePcCareScanAdd(
        taskId: String,
        tagVerbatim: String,
        normalizedTag: String,
    ): AppResult<String> = AppResult.Err("pc care scan sync is not configured")

    /**
     * Enqueues one PC Care slot proof registration
     * (`PUT /app/pc-care/tasks/{task_id}/animals/{row}/proofs/{slot}`). The mandatory video is
     * passed by REFERENCE to its PROOF_UPLOAD outbox row ([proofOutboxItemId]); both writes MUST
     * share the task group ([pcCareTaskGroupKey]) so the upload drains first. [animalRowId] may
     * be blank while the scan is still syncing — the dispatcher re-resolves it from the Room
     * animal row.
     */
    suspend fun enqueuePcCareSlotRegister(
        taskId: String,
        animalRowId: String,
        normalizedTag: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("pc care slot proof sync is not configured")

    suspend fun enqueuePcCareTaskProofRegister(
        taskId: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("pc care task proof sync is not configured")

    /**
     * Enqueues the WHOLE-task PC Care submit (`POST /app/pc-care/tasks/{task_id}/submit`), on the
     * SAME task group as the slot registrations and uploads, so it drains last. The idempotency
     * key is [pcCareSubmitIdempotencyKey] — stable per (task, row version).
     */
    suspend fun enqueuePcCareTaskSubmit(
        taskId: String,
        rowVersion: Int,
    ): AppResult<String> = AppResult.Err("pc care submit sync is not configured")

    /**
     * Enqueues one Toxin step completion
     * (`POST /app/toxin/tasks/{task_id}/steps/{step_no}/complete`, module toxin). The mandatory
     * proof (video, or step 7's strip photo) is passed by REFERENCE to its PROOF_UPLOAD outbox
     * row ([proofOutboxItemId]); both writes MUST share the task group ([toxinTaskGroupKey]) so
     * the upload drains first. The idempotency key is [toxinStepIdempotencyKey] — STABLE per
     * (task, step, proof row), never a timestamp.
     */
    suspend fun enqueueToxinStepComplete(
        taskId: String,
        stepNo: Int,
        proofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("toxin step sync is not configured")

    /**
     * Enqueues the Toxin reading submit (`POST /app/toxin/tasks/{task_id}/submit`) — step 7's
     * strip photo + outcome, on the SAME task group as the uploads and step completions so it
     * drains last. [outcome] is one of the BACKEND-OWNED `outcome_options` values.
     */
    suspend fun enqueueToxinSubmit(
        taskId: String,
        outcome: String,
        stripPhotoOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("toxin submit sync is not configured")

    suspend fun enqueueMilkPreparationSubmit(
        groupKey: String,
        idempotencyKey: String,
        parkId: String,
        preparationDate: String,
        goatMilkUsed: Boolean,
        answers: MilkPreparationAnswersPayload,
        proofOutboxItemIds: Map<String, String>,
    ): AppResult<String> = AppResult.Err("milk preparation sync is not configured")

    suspend fun enqueueMilkFeedingSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        parkId: String,
        feedingDate: String,
        sessionNo: Int,
        answers: sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto,
        cleanBottlesProofOutboxItemId: String,
        mixingAndFillingProofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("milk feeding sync is not configured")

    suspend fun enqueueFeedTransportSubmit(groupKey:String,idempotencyKey:String,taskId:String,proofOutboxItemId:String):AppResult<String> = AppResult.Err("feed transport sync is not configured")

    /**
     * Enqueues a Counts identifier PROMOTE (`POST /app/counts/goats/{goat_id}/promote-identifier`).
     * Assigns [permanentIdentifier] to the temporary-tagged goat [groupKey], atomically retiring its
     * temp tag. The caller derives a STABLE [idempotencyKey] from the goat id (never a timestamp-
     * suffixed one) — a fresh key on resend would attempt a second retag. Under the stable key the
     * backend returns the original promotion with `idempotent_replay=true`. [rowVersion] is the goat's
     * optimistic-concurrency token from the awaiting-RFID row, so a stale in-hand record is rejected.
     */
    suspend fun enqueuePromoteIdentifier(
        groupKey: String,
        idempotencyKey: String,
        permanentIdentifier: String,
        rowVersion: Int,
        secondaryIdentifier: String? = null,
    ): AppResult<String> = AppResult.Err("identifier promotion sync is not configured")

    /**
     * Enqueues a Shifting EXECUTION cancel (`POST /app/counts/shifting-events/{id}/cancel`). Retires
     * an authorized movement; moves nothing. [reason] is REQUIRED server-side. Same group-key +
     * stable-key contract as [enqueueShiftingComplete].
     */
    suspend fun enqueueShiftingCancel(
        groupKey: String,
        idempotencyKey: String,
        reason: String,
    ): AppResult<String> = AppResult.Err("shifting cancel sync is not configured")

    /**
     * Enqueues a birth/death workflow-action ANSWER
     * (`POST /app/workflows/{workflow_id}/actions/{action_id}/answer`,
     * docs/decisions/birth-death-workflows.md). [groupKey] is the WORKFLOW id so two actions on
     * the same workflow drain strictly oldest-first. [idempotencyKey] must be a STABLE per-action
     * key the caller derived once and persisted — the backend rejects a NEW key against an
     * already-completed action, so a fresh key on resend would surface a spurious conflict instead
     * of collapsing onto the original answer.
     */
    suspend fun enqueueWorkflowActionAnswer(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        answerValue: String,
        proofOutboxItemId: String? = null,
    ): AppResult<String> = AppResult.Err("workflow action sync is not configured")

    /**
     * Enqueues a birth/death workflow-action COMPLETE
     * (`POST /app/workflows/{workflow_id}/actions/{action_id}/complete`). For a `requires_video`
     * action the MANDATORY video is passed by REFERENCE to its PROOF_UPLOAD outbox row
     * ([proofOutboxItemId], enqueued on the SAME [groupKey] so it drains first); the dispatcher
     * resolves the uploaded proof_id and sends it as `proof_ref`, exactly like
     * [enqueueShiftingComplete]. Null for non-video completions. Same stable-key contract as
     * [enqueueWorkflowActionAnswer].
     */
    suspend fun enqueueWorkflowActionComplete(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        proofOutboxItemId: String? = null,
    ): AppResult<String> = AppResult.Err("workflow action sync is not configured")

    /** Opens a Health disease course. The goat is the ordering group and the caller persists one
     * idempotency key for the draft so a retry cannot create a duplicate disease episode. */
    suspend fun enqueueHealthCaseOpen(
        goatId: String,
        diseaseKey: String,
        ageBand: String,
        startDate: String,
        idempotencyKey: String,
        goatDisplayId: String = "",
        diseaseName: String = "",
    ): AppResult<String> = AppResult.Err("health case sync is not configured")

    /** Enqueues one Health session completion. The session id is both the ordering group and the
     * stable idempotency identity, preventing duplicate medicine administration rows on retry. */
    suspend fun enqueueHealthTreatmentComplete(
        healthSessionId: String,
        idempotencyKey: String,
        proofRef: String = "",
    ): AppResult<String> = AppResult.Err("health treatment sync is not configured")

    /** Re-arms a FAILED (dead-letter or conflict) row for another attempt — the SAME
     *  idempotency key and payload, a fresh attempt budget. Backs the sync-status sheet's
     *  retry affordance. */
    suspend fun retry(itemId: String): AppResult<Unit>

    /** Deletes an outbox item by id. Used when cancelling unsynced operations (R50-028: removing
     *  a proof that was never uploaded should clean up its queued outbox entry). */
    suspend fun deleteOutboxItem(itemId: String): AppResult<Unit>

    /** Atomically cancels (deletes) an outbox item ONLY if it is still QUEUED/FAILED — NOT if the
     *  dispatcher has already claimed it (IN_FLIGHT). Returns Ok(true) if cancelled, Ok(false) if the
     *  dispatcher won the race / it is gone. Closes the R50-028 check-then-delete TOCTOU: the caller
     *  no longer reads the status and then deletes unconditionally. Default delegates to
     *  [deleteOutboxItem] for lightweight fakes; the production impl overrides with a guarded delete. */
    suspend fun cancelOutboxItemIfPending(itemId: String): AppResult<Boolean> =
        when (val r = deleteOutboxItem(itemId)) {
            is AppResult.Ok -> AppResult.Ok(true)
            is AppResult.Err -> r
        }

    /** Deletes a server-side proof that has already synced but has not been attached to a
     *  submitted record. Used by proof X/remove; callers should keep the local row visible if this
     *  returns Err so the app never hides backend media. */
    suspend fun deleteUploadedProof(proofId: String): AppResult<Unit> =
        AppResult.Err("Uploaded proof delete is not available.")

    /** Deletes a terminal FAILED row by idempotency key so a corrected payload can be rebuilt
     *  after process recreation. Never removes QUEUED, IN_FLIGHT, or SUCCEEDED writes. */
    suspend fun deleteFailedOutboxItemByIdempotencyKey(idempotencyKey: String): AppResult<Unit> =
        AppResult.Err("Failed outbox recovery is not available.")

    /** Finds a previously queued write by its stable idempotency key so a recreated screen can
     *  resume QUEUED/FAILED/SUCCEEDED state even when Android did not restore SavedState. */
    suspend fun findOutboxItemByIdempotencyKey(idempotencyKey: String): AppResult<SyncQueueItem?> =
        AppResult.Err("Outbox recovery is not available.")

    /** Finds the single most recent outbox row for (groupKey, opType) through EVERY status,
     *  including terminal SUCCEEDED — unlike [findOutboxItemByIdempotencyKey], this does not key
     *  off the current idempotency epoch, so it still finds a row whose success already rotated
     *  the epoch that would derive a different key today (weighing scope-submit). */
    suspend fun findLatestOutboxItem(groupKey: String, opType: String): AppResult<SyncQueueItem?> =
        AppResult.Err("Outbox recovery is not available.")

    /** Finds one outbox row by id, including terminal rows. Used by local feature stores to
     *  reconcile their Room SSOT after process/activity churn missed a live terminal emission. */
    suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> =
        AppResult.Err("Outbox item lookup is not available.")

    /** Forces an immediate drain pass (pull-to-refresh, a manual "sync now", or connectivity
     *  regained). `enqueue*` already triggers this automatically — call this directly only
     *  when nothing new was enqueued but a retry should still happen right away. */
    suspend fun triggerDrain()
}

private class IdempotencyKeyConflict : Exception("Idempotency key already belongs to a different queued write.")

/** Mirrors the exact terminal guard in [sg.mesha.goatos.core.database.outbox.OutboxDao.reopenTerminalForRetry]:
 *  a FAILED row the drain loop will never touch again on its own — either a definitive server
 *  rejection ([OutboxEntity.conflict]) or one that spent its whole retry budget. A FAILED row
 *  still inside its backoff window (drain-eligible later) is NOT terminal. */
private fun OutboxEntity.isTerminalOutboxFailure(): Boolean =
    status == OutboxStatus.FAILED.name && (conflict || attemptCount >= maxAttempts)

class DefaultSyncRepository(
    private val store: OutboxStore,
    private val engine: SyncEngine,
    private val connectivityGate: ConnectivityGate,
    private val appScope: CoroutineScope,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val clock: () -> Long = System::currentTimeMillis,
    /** Maximum number of recent terminal rows (SUCCEEDED + dead-letter FAILED) to keep in the UI. */
    private val recentTerminalLimit: Int = 20,
    /** Retention time for SUCCEEDED rows before pruning (default: 24 hours). */
    private val succeededRetentionMs: Long = 24 * 60 * 60 * 1000L,
    /** Drive/Photos-style background upload (MOB-002 §3): asked to ensure the foreground
     *  upload service is running whenever an [UploadSyncCoordinator.RELEVANT_OP_TYPES] write is
     *  enqueued, so the visible progress notification survives the app being backgrounded or
     *  closed mid-upload. [ForegroundSyncController.Noop] by default so every existing/test
     *  construction of this class keeps compiling unchanged. */
    private val foregroundSyncController: ForegroundSyncController = ForegroundSyncController.Noop,
    /** Queue-lifecycle visibility (see [OutboxTelemetryReporter]). This half covers the ONE
     *  transition [SyncEngine] cannot see — the moment a write becomes durable but has not yet
     *  been attempted, which is exactly the state a never-draining queue is stuck in.
     *  [OutboxTelemetryReporter.Noop] by default so existing/test constructions keep compiling. */
    private val telemetry: OutboxTelemetryReporter = OutboxTelemetryReporter.Noop,
) : SyncRepository {

    private val onlineFlow = MutableStateFlow(connectivityGate.isOnline())
    private val _status = MutableStateFlow(SyncStatus.empty(online = onlineFlow.value))
    private val activeWindowLimit: Int = 20 // Bounded window for memory safety in long offline periods

    init {
        appScope.launch {
            // Observe active rows (bounded window) + fetch recent terminals on changes to update UI.
            // Combines online status with outbox state to produce SyncStatus.
            var activeRows = emptyList<OutboxEntity>()
            var recentTerminals = emptyList<OutboxEntity>()

            var activeCounts: ActiveOutboxCounts? = null
            appScope.launch {
                store.observeActiveWindow(activeWindowLimit).collect { rows ->
                    activeRows = rows
                    recentTerminals = store.observeRecentTerminals(recentTerminalLimit)
                    _status.value = toSyncStatus(activeRows, recentTerminals, onlineFlow.value, activeCounts)
                }
            }
            appScope.launch {
                // TRUE totals via SQL aggregate — the windowed list undercounts past the window
                // size, and the badge must never lie about how much work is still pending.
                store.observeActiveCounts().collect { counts ->
                    activeCounts = counts
                    _status.value = toSyncStatus(activeRows, recentTerminals, onlineFlow.value, counts)
                }
            }

            appScope.launch {
                onlineFlow.collect { online ->
                    // Emit status with updated online flag, using current row state.
                    _status.value = toSyncStatus(activeRows, recentTerminals, online, activeCounts)
                }
            }
        }

        // Background periodic prune of old SUCCEEDED rows (every 10 minutes).
        appScope.launch {
            while (true) {
                try {
                    kotlinx.coroutines.delay(10 * 60 * 1000L) // 10 minutes
                    store.pruneSucceeded(succeededRetentionMs, clock())
                } catch (e: CancellationException) {
                    // Scope shutdown, not a failure: rethrow so the coroutine actually cancels
                    // instead of this loop spinning forever inside a dead scope.
                    throw e
                } catch (e: Exception) {
                    // A prune failure is survivable -- the rows stay and the next tick retries --
                    // but it is NOT nothing: an outbox that never prunes grows without bound and
                    // the first symptom is a slow app with no explanation. Recorded, never
                    // silenced; the marker this replaced kept the guard quiet and told nobody.
                    android.util.Log.w("GoatOsOutbox", "outbox_prune_failed retained=$succeededRetentionMs", e)
                }
            }
        }
    }

    override fun observeStatus(): StateFlow<SyncStatus> = _status.asStateFlow()

    override fun observePendingHealthCaseOpens(): Flow<List<PendingHealthCaseOpen>> =
        // FULL per-opType active set, never the cross-feature newest-N window: this reconciliation
        // guard exists so a network refresh cannot erase a still-pending command, and a windowed
        // read silently drops the oldest pending open once other features queue enough rows
        // after it (judge finding 2026-08-15).
        store.observeActiveByOpType(OutboxOpType.HEALTH_CASE_OPEN.name)
            .map { rows -> projectPendingHealthCaseOpens(rows, syncJson) }
            .distinctUntilChanged()

    override fun observeSubmittedForReviewGrains(): Flow<Set<String>> =
        // FULL active set, never a windowed read: a newest-N window silently drops the oldest
        // pending submit once other features queue enough rows after it (judge finding 2026-08-15,
        // see observePendingHealthCaseOpens above).
        store.observeActive()
            .map { rows -> projectSubmittedGrains(rows, syncJson) }
            .distinctUntilChanged()

    companion object {
        /**
         * ACTIVE outbox rows -> the grains whose badge should read "in review".
         *
         * Pure so the contract that matters — a terminal or succeeded row is simply ABSENT, which is
         * what retracts the badge — is testable without Room, flows or a clock.
         */
        internal fun projectSubmittedGrains(
            rows: List<sg.mesha.goatos.core.database.outbox.OutboxEntity>,
            json: kotlinx.serialization.json.Json,
        ): Set<String> = rows.mapNotNullTo(mutableSetOf()) {
            submittedGrainKeyOf(it.opType, it.payloadJson, json)
        }
    }

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        store.observeById(itemId)
            .map { entity -> entity?.toSyncQueueItem() }
            .distinctUntilChanged()

    /** DI-wiring-only hook (see AppModule's `provideConnectivitySyncTrigger`) — NOT part of
     *  the [SyncRepository] port; UI/ViewModel code never calls this directly. */
    fun notifyConnectivityChanged(online: Boolean) {
        onlineFlow.value = online
    }

    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SHED_SUBMIT,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(ShedSubmitPayload(taskId = taskId, request = request)),
    )

    override suspend fun enqueueScanCapture(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        partitionKey: String,
        request: ScanCaptureRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SCAN_CAPTURE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ScanCapturePayload(taskId = taskId, partitionKey = partitionKey, request = request),
        ),
    )

    override suspend fun enqueueScanAttempt(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: ScanAttemptRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SCAN_ATTEMPT,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(ScanAttemptPayload(taskId = taskId, request = request)),
    )

    override suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.RESCHEDULE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(ReschedulePayload(obligationId = obligationId, request = request)),
    )

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        val result = enqueue(
            opType = OutboxOpType.PROOF_UPLOAD,
            groupKey = groupKey,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                ProofUploadPayload(request = request, localFilePath = localFilePath, durationMs = durationMs),
            ),
        )
        return result
    }

    override suspend fun enqueueVerifyTask(
        taskId: String,
        reason: String,
        rowVersion: Int,
    ): AppResult<String> {
        val idempotencyKey = "$taskId-verify-${System.currentTimeMillis()}"
        return enqueue(
            opType = OutboxOpType.VERIFY_TASK,
            groupKey = taskId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(VerifyTaskPayload(taskId = taskId, request = ReviewTaskRequestDto(reason = reason, rowVersion = rowVersion))),
        )
    }

    override suspend fun enqueueReworkTask(
        taskId: String,
        reason: String,
        rowVersion: Int,
    ): AppResult<String> {
        val idempotencyKey = "$taskId-rework-${System.currentTimeMillis()}"
        return enqueue(
            opType = OutboxOpType.REWORK_TASK,
            groupKey = taskId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(ReworkTaskPayload(taskId = taskId, request = ReviewTaskRequestDto(reason = reason, rowVersion = rowVersion))),
        )
    }

    override suspend fun enqueueWeighingWeightCorrection(
        observationId: String,
        refType: String,
        weightKg: Double,
        animalCount: Int?,
        reason: String?,
    ): AppResult<String> {
        // STABLE, never timestamp-suffixed: a retry of the SAME correction must replay for free on
        // the server rather than write twice, while correcting to 12 kg and then to 13 kg are two
        // different acts that must not collide on one key -- so the corrected VALUES are in the key.
        val idempotencyKey = "$observationId-weight-correction-$weightKg-${animalCount ?: "keep"}"
        return enqueue(
            opType = OutboxOpType.WEIGHING_WEIGHT_CORRECTION,
            // The observation is the group key, so two corrections of the same record can never
            // drain concurrently or out of order -- the last one she made must be the one that wins.
            groupKey = observationId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                WeighingWeightCorrectionPayload(
                    observationId = observationId,
                    request = WeighingWeightCorrectionRequestDto(
                        refType = refType,
                        weightKg = weightKg,
                        animalCount = animalCount,
                        reason = reason,
                        idempotencyKey = idempotencyKey,
                    ),
                ),
            ),
        )
    }

    override suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
        measurement: VerificationVerdictMeasurementDto?,
    ): AppResult<String> {
        val idempotencyKey = "$itemId-verdict-${System.currentTimeMillis()}"
        return enqueue(
            opType = OutboxOpType.VERIFICATION_VERDICT,
            groupKey = itemId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                VerificationVerdictPayload(
                    itemId = itemId,
                    request = VerificationVerdictRequestDto(
                        decision = decision,
                        reason = reason,
                        rowVersion = rowVersion,
                        // Only on an approve. The backend drops it on a reject anyway; not sending
                        // it keeps the queued payload honest about what the act was.
                        measurement = measurement.takeIf { decision == VerificationDecision.APPROVED },
                    ),
                ),
            ),
        )
    }

    override suspend fun enqueueVerificationClose(
        itemId: String,
        rowVersion: Int,
    ): AppResult<String> {
        val idempotencyKey = "$itemId-close-$rowVersion"
        return enqueue(
            opType = OutboxOpType.VERIFICATION_CLOSE,
            groupKey = itemId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                VerificationClosePayload(
                    itemId = itemId,
                    request = VerificationCloseRequestDto(rowVersion = rowVersion),
                ),
            ),
        )
    }

    override suspend fun enqueueVerificationSubmissionClose(
        submissionId: String,
    ): AppResult<String> {
        val idempotencyKey = "$submissionId-drive-close"
        return enqueue(
            opType = OutboxOpType.VERIFICATION_CLOSE_SUBMISSION,
            groupKey = submissionId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                VerificationCloseSubmissionPayload(submissionId = submissionId),
            ),
        )
    }

    override suspend fun enqueueVerificationBatchClose(
        batchId: String,
    ): AppResult<String> {
        val idempotencyKey = "$batchId-drive-close"
        return enqueue(
            opType = OutboxOpType.VERIFICATION_CLOSE_BATCH,
            groupKey = batchId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                VerificationCloseBatchPayload(batchId = batchId),
            ),
        )
    }

    override suspend fun enqueueVerificationReviewEvents(
        groupKey: String,
        idempotencyKey: String,
        request: VerificationReviewEventBatchRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.VERIFICATION_REVIEW_EVENTS,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(VerificationReviewEventsPayload(request = request)),
    )

    override suspend fun enqueueCountsShifting(
        groupKey: String,
        idempotencyKey: String,
        request: CountsShiftingEventRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.COUNTS_SHIFTING,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(CountsShiftingPayload(request = request)),
    )

    override suspend fun enqueueClockPunch(
        clockIn: Boolean,
        groupKey: String,
        idempotencyKey: String,
        request: ClockPunchRequestDto,
    ): AppResult<String> = enqueue(
        // The op type selects the endpoint AND separates an in from an out in the request
        // fingerprint, so the two can never be mistaken for a replay of each other.
        opType = if (clockIn) OutboxOpType.CLOCK_IN else OutboxOpType.CLOCK_OUT,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(ClockPunchPayload(request = request)),
        // A punch must SURVIVE a long outage (decision D1 — offline punches are
        // the point of the module). The default 8-attempt budget killed a
        // queued clock-in in minutes when the server was unreachable (E2E
        // finding 2026-08-28); with the server-capped backoff this budget rides
        // out days of dead network rather than minutes. Terminal 409/422
        // conflicts still dead-letter immediately via isTerminalAppApiError.
        maxAttempts = CLOCK_PUNCH_MAX_ATTEMPTS,
    )

    override suspend fun enqueueCountsBirth(
        groupKey: String,
        idempotencyKey: String,
        request: CountsBirthEventRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.COUNTS_BIRTH,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(CountsBirthPayload(request = request)),
    )

    override suspend fun enqueueCountsDeath(
        groupKey: String,
        idempotencyKey: String,
        request: CountsDeathEventRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.COUNTS_DEATH,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(CountsDeathPayload(request = request)),
    )

    override suspend fun enqueueCountsApprovalDecision(
        requestId: String,
        approve: Boolean,
        reason: String?,
        idempotencyKey: String,
    ): AppResult<String> = enqueue(
        // The op type selects the endpoint AND separates an approve from a reject in the request
        // fingerprint, so the two can never be mistaken for a replay of each other.
        opType = if (approve) OutboxOpType.COUNTS_APPROVAL_APPROVE else OutboxOpType.COUNTS_APPROVAL_REJECT,
        groupKey = requestId,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            CountsApprovalDecisionPayload(
                requestId = requestId,
                request = CountsApprovalDecisionRequestDto(reason = reason?.trim()?.ifBlank { null }),
            ),
        ),
    )

    override suspend fun enqueueWeighingAnimalObservation(
        campaignId: String,
        groupKey: String,
        idempotencyKey: String,
        request: WeighingAnimalObservationRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.WEIGHING_ANIMAL_OBSERVATION,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(WeighingAnimalObservationPayload(campaignId = campaignId, request = request)),
    )

    override suspend fun enqueueWeighingShedObservation(
        campaignId: String,
        groupKey: String,
        idempotencyKey: String,
        request: WeighingShedObservationRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.WEIGHING_SHED_OBSERVATION,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(WeighingShedObservationPayload(campaignId = campaignId, request = request)),
    )

    override suspend fun enqueueWeighingScopeSubmit(
        campaignId: String,
        campaignShedId: String,
        groupKey: String,
        idempotencyKey: String,
        request: WeighingScopeSubmitRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.WEIGHING_SCOPE_SUBMIT,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            WeighingScopeSubmitPayload(campaignId = campaignId, campaignShedId = campaignShedId, request = request),
        ),
    )

    override suspend fun enqueueShiftingComplete(
        groupKey: String,
        idempotencyKey: String,
        destinationTag: String?,
        proofOutboxItemId: String,
        feedPackingProofOutboxItemId: String?,
        feedGivenProofOutboxItemId: String?,
        feedConfigFingerprint: String?,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SHIFTING_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ShiftingCompletePayload(
                shiftingEventId = groupKey,
                destinationTag = destinationTag?.trim()?.ifBlank { null },
                proofOutboxItemId = proofOutboxItemId,
                feedPackingProofOutboxItemId = feedPackingProofOutboxItemId,
                feedGivenProofOutboxItemId = feedGivenProofOutboxItemId,
                feedConfigFingerprint = feedConfigFingerprint,
            ),
        ),
    )

    override suspend fun enqueueShiftingCancel(
        groupKey: String,
        idempotencyKey: String,
        reason: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SHIFTING_CANCEL,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ShiftingCancelPayload(shiftingEventId = groupKey, reason = reason),
        ),
    )

    override suspend fun enqueueFeedDirectionComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.FEED_DIRECTION_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            FeedDirectionCompletePayload(
                parkId = parkId?.trim()?.ifBlank { null },
                shedId = shedId.trim(),
                sessionNo = sessionNo,
                targetDate = targetDate.trim(),
                workflow = workflow.trim(),
            ),
        ),
    )

    override suspend fun enqueueFeedDistributionComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        distributionProofOutboxItemId: String?,
        feedWeightProofOutboxItemId: String?,
        waterProofOutboxItemId: String?,
        feedWeightProofRef: String?,
        distributionProofRef: String?,
        waterProofRef: String?,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.FEED_DISTRIBUTION_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            FeedDistributionCompletePayload(
                parkId = parkId?.trim()?.ifBlank { null },
                shedId = shedId.trim(),
                partitionLabel = partitionLabel?.trim()?.ifBlank { null },
                sessionNo = sessionNo,
                targetDate = targetDate.trim(),
                workflow = workflow.trim(),
                distributionProofOutboxItemId = distributionProofOutboxItemId,
                feedWeightProofOutboxItemId = feedWeightProofOutboxItemId,
                waterProofOutboxItemId = waterProofOutboxItemId,
                feedWeightProofRef = feedWeightProofRef?.trim()?.ifBlank { null },
                distributionProofRef = distributionProofRef?.trim()?.ifBlank { null },
                waterProofRef = waterProofRef?.trim()?.ifBlank { null },
            ),
        ),
    )

    override suspend fun enqueueFeedPackingComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        packingProofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.FEED_PACKING_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            FeedPackingCompletePayload(
                parkId = parkId?.trim()?.ifBlank { null },
                shedId = shedId.trim(),
                partitionLabel = partitionLabel?.trim()?.ifBlank { null },
                sessionNo = sessionNo,
                targetDate = targetDate.trim(),
                workflow = workflow.trim(),
                packingProofOutboxItemId = packingProofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueueFeedWastageComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        targetDate: String,
        wastageProofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.FEED_WASTAGE_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            FeedWastageCompletePayload(
                parkId = parkId?.trim()?.ifBlank { null },
                shedId = shedId.trim(),
                partitionLabel = partitionLabel?.trim()?.ifBlank { null },
                targetDate = targetDate.trim(),
                wastageProofOutboxItemId = wastageProofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueueFeedWastageMeasurement(
        completionId: String,
        wastageKg: Double,
    ): AppResult<String> {
        // STABLE, never timestamp-suffixed, and the VALUE is in the key: a retry of the SAME
        // measurement replays for free on the server, while recording 3 kg and then 3.5 kg are two
        // different acts that must not collide on one key (same rule as the weighing correction).
        val idempotencyKey = "feed-wastage-measurement-$completionId-$wastageKg"
        return enqueue(
            opType = OutboxOpType.FEED_WASTAGE_MEASUREMENT,
            // The completion is the group key, so two measurements of the same pen-day can never
            // drain concurrently or out of order — the last one she made must be the one that wins.
            groupKey = completionId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                FeedWastageMeasurementPayload(
                    completionId = completionId,
                    request = FeedWastageMeasurementRequestDto(
                        wastageKg = wastageKg,
                        idempotencyKey = idempotencyKey,
                    ),
                ),
            ),
        )
    }

    override suspend fun enqueuePcCareScanAdd(
        taskId: String,
        tagVerbatim: String,
        normalizedTag: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.PC_CARE_SCAN_ADD,
        groupKey = pcCareScanGroupKey(taskId.trim()),
        idempotencyKey = pcCareScanIdempotencyKey(taskId.trim(), normalizedTag),
        payloadJson = syncJson.encodeToString(
            PcCareScanAddPayload(
                taskId = taskId.trim(),
                tagVerbatim = tagVerbatim,
                normalizedTag = normalizedTag,
            ),
        ),
    )

    override suspend fun enqueuePcCareSlotRegister(
        taskId: String,
        animalRowId: String,
        normalizedTag: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.PC_CARE_SLOT_REGISTER,
        groupKey = pcCareTaskGroupKey(taskId.trim()),
        idempotencyKey = pcCareSlotIdempotencyKey(taskId.trim(), normalizedTag, slotFieldKey, proofOutboxItemId),
        payloadJson = syncJson.encodeToString(
            PcCareSlotRegisterPayload(
                taskId = taskId.trim(),
                animalRowId = animalRowId.trim(),
                normalizedTag = normalizedTag,
                slotFieldKey = slotFieldKey,
                proofOutboxItemId = proofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueuePcCareTaskProofRegister(
        taskId: String,
        slotFieldKey: String,
        proofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.PC_CARE_TASK_PROOF_REGISTER,
        groupKey = pcCareTaskGroupKey(taskId.trim()),
        idempotencyKey = pcCareTaskProofIdempotencyKey(taskId.trim(), slotFieldKey, proofOutboxItemId),
        payloadJson = syncJson.encodeToString(
            PcCareTaskProofRegisterPayload(
                taskId = taskId.trim(),
                slotFieldKey = slotFieldKey,
                proofOutboxItemId = proofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueuePcCareTaskSubmit(
        taskId: String,
        rowVersion: Int,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.PC_CARE_TASK_SUBMIT,
        groupKey = pcCareTaskGroupKey(taskId.trim()),
        idempotencyKey = pcCareSubmitIdempotencyKey(taskId.trim(), rowVersion),
        payloadJson = syncJson.encodeToString(
            PcCareTaskSubmitPayload(taskId = taskId.trim(), rowVersion = rowVersion),
        ),
    )

    override suspend fun enqueueToxinStepComplete(
        taskId: String,
        stepNo: Int,
        proofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.TOXIN_STEP_COMPLETE,
        groupKey = toxinTaskGroupKey(taskId.trim()),
        idempotencyKey = toxinStepIdempotencyKey(taskId.trim(), stepNo, proofOutboxItemId),
        payloadJson = syncJson.encodeToString(
            ToxinStepCompletePayload(
                taskId = taskId.trim(),
                stepNo = stepNo,
                proofOutboxItemId = proofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueueToxinSubmit(
        taskId: String,
        outcome: String,
        stripPhotoOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.TOXIN_SUBMIT,
        groupKey = toxinTaskGroupKey(taskId.trim()),
        idempotencyKey = toxinSubmitIdempotencyKey(taskId.trim(), outcome.trim(), stripPhotoOutboxItemId),
        payloadJson = syncJson.encodeToString(
            ToxinSubmitPayload(
                taskId = taskId.trim(),
                outcome = outcome.trim(),
                stripPhotoOutboxItemId = stripPhotoOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueueMilkPreparationSubmit(
        groupKey: String,
        idempotencyKey: String,
        parkId: String,
        preparationDate: String,
        goatMilkUsed: Boolean,
        answers: MilkPreparationAnswersPayload,
        proofOutboxItemIds: Map<String, String>,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.MILK_PREPARATION_SUBMIT,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            MilkPreparationSubmitPayload(parkId.trim(), preparationDate.trim(), goatMilkUsed, answers, proofOutboxItemIds),
        ),
    )

    override suspend fun enqueueMilkFeedingSubmit(
        groupKey: String,
        idempotencyKey: String,
        taskId: String,
        parkId: String,
        feedingDate: String,
        sessionNo: Int,
        answers: sg.mesha.goatos.core.network.dto.MilkFeedingAnswersDto,
        cleanBottlesProofOutboxItemId: String,
        mixingAndFillingProofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.MILK_FEEDING_SUBMIT,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(MilkFeedingSubmitPayload(taskId, parkId, feedingDate, sessionNo, answers, cleanBottlesProofOutboxItemId, mixingAndFillingProofOutboxItemId)),
    )

    override suspend fun enqueueFeedTransportSubmit(groupKey:String,idempotencyKey:String,taskId:String,proofOutboxItemId:String):AppResult<String> = enqueue(opType=OutboxOpType.FEED_TRANSPORT_SUBMIT,groupKey=groupKey,idempotencyKey=idempotencyKey,payloadJson=syncJson.encodeToString(FeedTransportSubmitPayload(taskId,proofOutboxItemId)))

    override suspend fun enqueuePromoteIdentifier(
        groupKey: String,
        idempotencyKey: String,
        permanentIdentifier: String,
        rowVersion: Int,
        secondaryIdentifier: String?,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.COUNTS_PROMOTE_IDENTIFIER,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            PromoteIdentifierPayload(
                goatId = groupKey,
                permanentIdentifier = permanentIdentifier.trim(),
                animalIdentifier2 = secondaryIdentifier?.trim()?.ifBlank { null },
                rowVersion = rowVersion,
            ),
        ),
    )

    private suspend fun enqueue(
        opType: OutboxOpType,
        groupKey: String,
        idempotencyKey: String,
        payloadJson: String,
        maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
    ): AppResult<String> = withContext(dispatchers.io) {
        try {
            val fingerprint = requestFingerprint(opType, groupKey, payloadJson)
            val id = insertOrExistingRow(opType, groupKey, idempotencyKey, payloadJson, fingerprint, maxAttempts)
            triggerDrainAsync()
            // Background upload foreground service (MOB-002 §3): only for the op types that
            // carry proof/video-sized payloads worth a visible "uploading" notification — see
            // UploadSyncCoordinator.RELEVANT_OP_TYPES. A verify/rework/reschedule write still
            // drains via triggerDrainAsync() above; it just never shows the upload notification.
            if (opType in UploadSyncCoordinator.RELEVANT_OP_TYPES) {
                foregroundSyncController.ensureRunning()
            }
            AppResult.Ok(id)
        } catch (cancellation: CancellationException) {
            // Never swallow cancellation into an Err — that breaks structured concurrency
            // (a torn-down caller scope must see its own cancellation, not a fake failure).
            throw cancellation
        } catch (e: IdempotencyKeyConflict) {
            AppResult.Err(e.message ?: "Idempotency key already belongs to a different queued write.")
        } catch (e: Throwable) {
            AppResult.Err("Couldn't queue the write: ${e.message}", e)
        }
    }

    /** Idempotent-enqueue: returns the existing row's id if this exact request is already queued,
     *  else inserts a new row. Handles the concurrent-insert race — if two callers pass the unique
     *  key at once, the loser's unique-index violation is turned back into the winner's row id only
     *  after proving the winning row is the same semantic request. */
    private suspend fun insertOrExistingRow(
        opType: OutboxOpType,
        groupKey: String,
        idempotencyKey: String,
        payloadJson: String,
        fingerprint: String,
        maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
    ): String {
        store.findByIdempotencyKey(idempotencyKey)?.let {
            return reopenOrReplayExistingRow(it, opType, groupKey, payloadJson, fingerprint)
        }
        val now = clock()
        val id = UUID.randomUUID().toString()
        try {
            store.insert(
                OutboxEntity(
                    id = id,
                    opType = opType.name,
                    groupKey = groupKey,
                    idempotencyKey = idempotencyKey,
                    payloadJson = payloadJson,
                    requestFingerprint = fingerprint,
                    status = OutboxStatus.QUEUED.name,
                    attemptCount = 0,
                    maxAttempts = maxAttempts,
                    conflict = false,
                    createdAt = now,
                    updatedAt = now,
                    nextAttemptAt = now,
                    lastError = null,
                    resultJson = null,
                ),
            )
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            // A concurrent enqueue of the same key won the unique-index race. Route through the
            // same reopen-or-replay decision a pre-existing row would have taken (the racing
            // winner may itself already be a dead-letter row from a PRIOR attempt).
            return store.findByIdempotencyKey(idempotencyKey)?.let {
                reopenOrReplayExistingRow(it, opType, groupKey, payloadJson, fingerprint)
            } ?: throw e
        }
        // Only a genuinely NEW row is announced. An idempotent replay returns above without
        // reporting, so the enqueue count stays a count of distinct writes rather than of taps.
        // Telemetry is diagnostics, never control flow — a broken reporter cannot fail a write.
        runCatching {
            telemetry.onOutboxWrite(
                OutboxTelemetryEvent(
                    phase = OutboxWritePhase.ENQUEUED,
                    opType = opType.name,
                    itemId = id,
                    attempt = 0,
                    maxAttempts = maxAttempts,
                ),
            )
        }
        return id
    }

    /**
     * DEVICE-PROVEN DEFECT FIX: a submit whose only outbox row already reached a TERMINAL
     * failure (dead-letter conflict OR attempt-exhausted, [isTerminalOutboxFailure]) must not
     * be treated as an idempotent replay of a dead row — that silently dropped every future
     * submit under the same stable key ("milk-feeding-submit:<taskId>"-shaped), since the
     * unique index on [OutboxEntity.idempotencyKey] rejected a fresh insert and the old
     * [existingReplayIdOrThrow] path just handed back the SAME dead row's id (fingerprint
     * match) or threw [IdempotencyKeyConflict] (fingerprint mismatch) — either way the row
     * stayed FAILED/terminal forever and nothing was ever (re)sent.
     *
     * A terminal row is instead RE-OPENED in place via [OutboxStore.reopenTerminalForRetry]:
     * SAME id + SAME idempotencyKey (server-side replay semantics untouched), fresh attempt
     * budget, and the CALLER'S LATEST payload/fingerprint (a resubmit may carry corrected
     * data) — always, regardless of whether the fingerprint matches, because a terminal row
     * is dead and a brand-new user tap should never be rejected as a "conflict" against it.
     *
     * A non-terminal existing row (QUEUED, IN_FLIGHT, still-in-backoff FAILED, or SUCCEEDED)
     * keeps the original replay/conflict semantics untouched via [existingReplayIdOrThrow].
     */
    private suspend fun reopenOrReplayExistingRow(
        existing: OutboxEntity,
        opType: OutboxOpType,
        groupKey: String,
        payloadJson: String,
        fingerprint: String,
    ): String {
        if (
            existing.opType == OutboxOpType.PROOF_UPLOAD.name &&
            existing.status == OutboxStatus.FAILED.name
        ) {
            val reopened = store.reopenFailedProofUploadForRetry(
                id = existing.id,
                groupKey = groupKey,
                payloadJson = payloadJson,
                fingerprint = fingerprint,
                now = clock(),
            )
            if (reopened) {
                runCatching {
                    telemetry.onOutboxWrite(
                        OutboxTelemetryEvent(
                            phase = OutboxWritePhase.ENQUEUED,
                            opType = opType.name,
                            itemId = existing.id,
                            attempt = 0,
                            maxAttempts = existing.maxAttempts,
                        ),
                    )
                }
                return existing.id
            }
            val refreshed = store.findById(existing.id) ?: existing
            return reopenOrReplayExistingRowOnce(refreshed, opType, groupKey, payloadJson, fingerprint)
        }
        if (!existing.isTerminalOutboxFailure()) {
            return existingReplayIdOrThrow(existing, opType, groupKey, payloadJson, fingerprint)
        }
        val reopened = store.reopenTerminalForRetry(existing.id, payloadJson, fingerprint, clock())
        if (!reopened) {
            // Lost a race (e.g. a concurrent reopen/manual-retry already moved this row on) —
            // re-read the current state and fall back to the normal decision against it.
            val refreshed = store.findById(existing.id) ?: existing
            return reopenOrReplayExistingRowOnce(refreshed, opType, groupKey, payloadJson, fingerprint)
        }
        // A reopen is a fresh, durably-queued write in every observable sense a caller cares
        // about — the same signal a genuinely new row gets, so a UI/telemetry consumer cannot
        // tell "dead row came back to life" apart from "brand-new write queued" by watching this
        // seam, which is exactly the honesty this fix restores (previously: total silence).
        runCatching {
            telemetry.onOutboxWrite(
                OutboxTelemetryEvent(
                    phase = OutboxWritePhase.ENQUEUED,
                    opType = opType.name,
                    itemId = existing.id,
                    attempt = 0,
                    maxAttempts = existing.maxAttempts,
                ),
            )
        }
        return existing.id
    }

    /** One non-recursive fallback decision after losing the reopen race — never re-enters the
     *  reopen attempt a second time (a row that just lost that race is, by definition, no
     *  longer terminal-and-untouched, so re-trying would either loop or mask a real bug). */
    private suspend fun reopenOrReplayExistingRowOnce(
        existing: OutboxEntity,
        opType: OutboxOpType,
        groupKey: String,
        payloadJson: String,
        fingerprint: String,
    ): String = existingReplayIdOrThrow(existing, opType, groupKey, payloadJson, fingerprint)

    private fun existingReplayIdOrThrow(
        existing: OutboxEntity,
        opType: OutboxOpType,
        groupKey: String,
        payloadJson: String,
        fingerprint: String,
    ): String {
        val fingerprintMatches = existing.requestFingerprint.isNotBlank() && existing.requestFingerprint == fingerprint
        val legacyPayloadMatches = existing.requestFingerprint.isBlank() &&
            existing.opType == opType.name &&
            existing.groupKey == groupKey &&
            existing.payloadJson == payloadJson
        if (fingerprintMatches || legacyPayloadMatches) return existing.id
        throw IdempotencyKeyConflict()
    }

    override suspend fun enqueueWorkflowActionAnswer(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        answerValue: String,
        proofOutboxItemId: String?,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.WORKFLOW_ACTION_ANSWER,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            WorkflowActionAnswerPayload(
                workflowId = workflowId,
                actionId = actionId,
                answerValue = answerValue,
                proofOutboxItemId = proofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueueWorkflowActionComplete(
        groupKey: String,
        idempotencyKey: String,
        workflowId: String,
        actionId: String,
        proofOutboxItemId: String?,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.WORKFLOW_ACTION_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            WorkflowActionCompletePayload(
                workflowId = workflowId,
                actionId = actionId,
                proofOutboxItemId = proofOutboxItemId,
            ),
        ),
    )

    override suspend fun enqueueHealthTreatmentComplete(
        healthSessionId: String,
        idempotencyKey: String,
        proofRef: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.HEALTH_TREATMENT_COMPLETE,
        groupKey = healthSessionId,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            HealthTreatmentCompletePayload(healthSessionId = healthSessionId, proofRef = proofRef),
        ),
    )

    override suspend fun enqueueHealthCaseOpen(
        goatId: String,
        diseaseKey: String,
        ageBand: String,
        startDate: String,
        idempotencyKey: String,
        goatDisplayId: String,
        diseaseName: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.HEALTH_CASE_OPEN,
        groupKey = goatId,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            HealthCaseOpenPayload(
                goatId = goatId,
                diseaseKey = diseaseKey,
                ageBand = ageBand,
                startDate = startDate,
                goatDisplayId = goatDisplayId,
                diseaseName = diseaseName,
            ),
        ),
    )

    override suspend fun retry(itemId: String): AppResult<Unit> = withContext(dispatchers.io) {
        try {
            store.findById(itemId) ?: throw NoSuchElementException("Outbox item not found: $itemId")
            // markRetryReady only re-arms a terminal FAILED row; a no-op (row already SUCCEEDED
            // or a drain has it IN_FLIGHT) is fine — the live status flow reflects the real state.
            store.markRetryReady(itemId, clock())
            triggerDrainAsync()
            AppResult.Ok(Unit)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't retry: ${e.message}", e)
        }
    }

    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = withContext(dispatchers.io) {
        try {
            store.findById(itemId) ?: throw NoSuchElementException("Outbox item not found: $itemId")
            // R50-028: delete the outbox item — safe to delete unsynced items (PENDING/FAILED).
            // IN_FLIGHT items should not be deleted (in-progress dispatch), but a race is benign
            // (the delete is idempotent; the drain will see it's gone).
            store.delete(itemId)
            AppResult.Ok(Unit)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't delete outbox item: ${e.message}", e)
        }
    }

    override suspend fun cancelOutboxItemIfPending(itemId: String): AppResult<Boolean> = withContext(dispatchers.io) {
        try {
            AppResult.Ok(store.deleteIfCancellable(itemId))
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't cancel outbox item: ${e.message}", e)
        }
    }

    override suspend fun deleteUploadedProof(proofId: String): AppResult<Unit> = withContext(dispatchers.io) {
        try {
            engine.deleteProof(proofId)
            AppResult.Ok(Unit)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't delete uploaded proof: ${e.message}", e)
        }
    }

    override suspend fun deleteFailedOutboxItemByIdempotencyKey(
        idempotencyKey: String,
    ): AppResult<Unit> = withContext(dispatchers.io) {
        try {
            val existing = store.findByIdempotencyKey(idempotencyKey)
                ?: throw NoSuchElementException("Outbox item not found for idempotency key.")
            check(existing.status == OutboxStatus.FAILED.name) {
                "Only a FAILED outbox item can be replaced; current status is ${existing.status}."
            }
            store.delete(existing.id)
            AppResult.Ok(Unit)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't replace failed outbox item: ${e.message}", e)
        }
    }

    override suspend fun findOutboxItemByIdempotencyKey(
        idempotencyKey: String,
    ): AppResult<SyncQueueItem?> = withContext(dispatchers.io) {
        try {
            AppResult.Ok(store.findByIdempotencyKey(idempotencyKey)?.toSyncQueueItem())
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't recover outbox item: ${e.message}", e)
        }
    }

    override suspend fun findLatestOutboxItem(
        groupKey: String,
        opType: String,
    ): AppResult<SyncQueueItem?> = withContext(dispatchers.io) {
        try {
            AppResult.Ok(store.findLatestForGroupAndOpType(groupKey, opType)?.toSyncQueueItem())
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't recover outbox item: ${e.message}", e)
        }
    }

    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> = withContext(dispatchers.io) {
        try {
            AppResult.Ok(store.findById(itemId)?.toSyncQueueItem())
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (e: Throwable) {
            AppResult.Err("Couldn't recover outbox item: ${e.message}", e)
        }
    }

    override suspend fun triggerDrain() {
        triggerDrainAsync()
    }

    private fun triggerDrainAsync() {
        appScope.launch { engine.drainOnce() }
    }
}

private fun requestFingerprint(opType: OutboxOpType, groupKey: String, payloadJson: String): String {
    val fingerprintPayload = canonicalOutboxFingerprintPayload(opType, payloadJson, syncJson)
    val envelope = "${opType.name}\u0000$groupKey\u0000$fingerprintPayload"
    val bytes = MessageDigest.getInstance("SHA-256").digest(envelope.toByteArray(Charsets.UTF_8))
    return bytes.joinToString(separator = "") { "%02x".format(it.toInt() and 0xff) }
}

/** UI-only Health labels must not change the identity of the canonical case-open command. */
internal fun canonicalOutboxFingerprintPayload(
    opType: OutboxOpType,
    payloadJson: String,
    json: kotlinx.serialization.json.Json,
): String {
    if (opType != OutboxOpType.HEALTH_CASE_OPEN) return payloadJson
    return runCatching {
        val payload = json.decodeFromString<HealthCaseOpenPayload>(payloadJson)
        json.encodeToString(payload.copy(goatDisplayId = "", diseaseName = ""))
    }.getOrDefault(payloadJson)
}

/** Pure projection used by the production outbox-backed Health pending-report read path. */
internal fun projectPendingHealthCaseOpens(
    rows: List<OutboxEntity>,
    json: kotlinx.serialization.json.Json,
): List<PendingHealthCaseOpen> = rows.asSequence()
    .filter { it.opType == OutboxOpType.HEALTH_CASE_OPEN.name }
    .mapNotNull { row ->
        val payload = runCatching { json.decodeFromString<HealthCaseOpenPayload>(row.payloadJson) }
            .getOrNull() ?: return@mapNotNull null
        val status = runCatching { SyncItemStatus.valueOf(row.status) }
            .getOrNull() ?: return@mapNotNull null
        PendingHealthCaseOpen(
            outboxItemId = row.id,
            goatId = payload.goatId,
            goatDisplayId = payload.goatDisplayId.ifBlank { payload.goatId },
            diseaseKey = payload.diseaseKey,
            diseaseName = payload.diseaseName.ifBlank {
                payload.diseaseKey.replace('_', ' ').replaceFirstChar { it.uppercase() }
            },
            ageBand = payload.ageBand,
            startDate = payload.startDate,
            syncStatus = status,
            lastError = row.lastError,
        )
    }
    .toList()
