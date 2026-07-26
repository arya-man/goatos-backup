package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.CountsApprovalDecisionRequestDto
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationCloseRequestDto
import java.security.MessageDigest
import java.util.UUID

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
     *  verdicts on the SAME item never race out of order; different items drain concurrently. */
    suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
    ): AppResult<String>

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
     * ([distributionProofOutboxItemId] = feed-distribution video, [waterProofOutboxItemId] = water
     * photo/video): the dispatcher resolves each uploaded proof_id and sends the pair, exactly like
     * [enqueueShiftingComplete] resolves its single mandatory video. All three writes MUST share the
     * same [groupKey] (the shed-session) so the two proofs drain strictly before this completion.
     * [idempotencyKey] must be a STABLE caller-persisted key so a resend re-enqueues the SAME
     * verification item instead of completing twice. This is SEPARATE from
     * [enqueueFeedDirectionComplete] (the untouched packing path).
     */
    suspend fun enqueueFeedDistributionComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        distributionProofOutboxItemId: String,
        waterProofOutboxItemId: String,
    ): AppResult<String> = AppResult.Err("feed distribution completion sync is not configured")

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

    /** Deletes a terminal FAILED row by idempotency key so a corrected payload can be rebuilt
     *  after process recreation. Never removes QUEUED, IN_FLIGHT, or SUCCEEDED writes. */
    suspend fun deleteFailedOutboxItemByIdempotencyKey(idempotencyKey: String): AppResult<Unit> =
        AppResult.Err("Failed outbox recovery is not available.")

    /** Finds a previously queued write by its stable idempotency key so a recreated screen can
     *  resume QUEUED/FAILED/SUCCEEDED state even when Android did not restore SavedState. */
    suspend fun findOutboxItemByIdempotencyKey(idempotencyKey: String): AppResult<SyncQueueItem?> =
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
) : SyncRepository {

    private val onlineFlow = MutableStateFlow(connectivityGate.isOnline())
    private val _status = MutableStateFlow(SyncStatus.empty(online = onlineFlow.value))

    init {
        appScope.launch {
            // Observe active rows + fetch recent terminals on changes to update UI.
            // Combines online status with outbox state to produce SyncStatus.
            var activeRows = emptyList<OutboxEntity>()
            var recentTerminals = emptyList<OutboxEntity>()

            appScope.launch {
                store.observeActive().collect { rows ->
                    activeRows = rows
                    recentTerminals = store.observeRecentTerminals(recentTerminalLimit)
                    _status.value = toSyncStatus(activeRows, recentTerminals, onlineFlow.value)
                }
            }

            appScope.launch {
                onlineFlow.collect { online ->
                    // Emit status with updated online flag, using current row state.
                    _status.value = toSyncStatus(activeRows, recentTerminals, online)
                }
            }
        }

        // Background periodic prune of old SUCCEEDED rows (every 10 minutes).
        appScope.launch {
            while (true) {
                try {
                    kotlinx.coroutines.delay(10 * 60 * 1000L) // 10 minutes
                    store.pruneSucceeded(succeededRetentionMs, clock())
                } catch (e: Exception) {
                    // Log and continue — a prune failure should not crash the app
                    // (logging is deferred; in production, log via observability layer)
                }
            }
        }
    }

    override fun observeStatus(): StateFlow<SyncStatus> = _status.asStateFlow()

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
        request: ScanCaptureRequestDto,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SCAN_CAPTURE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(ScanCapturePayload(taskId = taskId, request = request)),
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
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.PROOF_UPLOAD,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ProofUploadPayload(request = request, localFilePath = localFilePath, durationMs = durationMs),
        ),
    )

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

    override suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
    ): AppResult<String> {
        val idempotencyKey = "$itemId-verdict-${System.currentTimeMillis()}"
        return enqueue(
            opType = OutboxOpType.VERIFICATION_VERDICT,
            groupKey = itemId,
            idempotencyKey = idempotencyKey,
            payloadJson = syncJson.encodeToString(
                VerificationVerdictPayload(
                    itemId = itemId,
                    request = VerificationVerdictRequestDto(decision = decision, reason = reason, rowVersion = rowVersion),
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

    override suspend fun enqueueShiftingComplete(
        groupKey: String,
        idempotencyKey: String,
        destinationTag: String?,
        proofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.SHIFTING_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ShiftingCompletePayload(
                shiftingEventId = groupKey,
                destinationTag = destinationTag?.trim()?.ifBlank { null },
                proofOutboxItemId = proofOutboxItemId,
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
        sessionNo: Int,
        targetDate: String,
        workflow: String,
        distributionProofOutboxItemId: String,
        waterProofOutboxItemId: String,
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.FEED_DISTRIBUTION_COMPLETE,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            FeedDistributionCompletePayload(
                parkId = parkId?.trim()?.ifBlank { null },
                shedId = shedId.trim(),
                sessionNo = sessionNo,
                targetDate = targetDate.trim(),
                workflow = workflow.trim(),
                distributionProofOutboxItemId = distributionProofOutboxItemId,
                waterProofOutboxItemId = waterProofOutboxItemId,
            ),
        ),
    )

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
    ): AppResult<String> = withContext(dispatchers.io) {
        try {
            val fingerprint = requestFingerprint(opType, groupKey, payloadJson)
            val id = insertOrExistingRow(opType, groupKey, idempotencyKey, payloadJson, fingerprint)
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
    ): String {
        store.findByIdempotencyKey(idempotencyKey)?.let { return existingReplayIdOrThrow(it, opType, groupKey, payloadJson, fingerprint) }
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
                    maxAttempts = DEFAULT_MAX_ATTEMPTS,
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
            // A concurrent enqueue of the same key won the unique-index race. Return its row only
            // for an exact replay; same-key/different-payload is still a conflict.
            return store.findByIdempotencyKey(idempotencyKey)?.let {
                existingReplayIdOrThrow(it, opType, groupKey, payloadJson, fingerprint)
            } ?: throw e
        }
        return id
    }

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
        throw IllegalStateException("Idempotency key already belongs to a different queued write.")
    }

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
    val envelope = "${opType.name}\u0000$groupKey\u0000$payloadJson"
    val bytes = MessageDigest.getInstance("SHA-256").digest(envelope.toByteArray(Charsets.UTF_8))
    return bytes.joinToString(separator = "") { "%02x".format(it.toInt() and 0xff) }
}
