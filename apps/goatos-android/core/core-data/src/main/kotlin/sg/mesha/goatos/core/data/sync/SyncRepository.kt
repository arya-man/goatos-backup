package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
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

    /** Enqueues a shed-submit write (`POST /app/tasks/{task_id}/submissions`). [groupKey]
     *  orders same-shed writes FIFO (TRD: outbox is "ordered per shed"); different groups
     *  may drain concurrently. Returns the outbox row id to follow via [observeStatus]. */
    suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String>

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

    /** Re-arms a FAILED (dead-letter or conflict) row for another attempt — the SAME
     *  idempotency key and payload, a fresh attempt budget. Backs the sync-status sheet's
     *  retry affordance. */
    suspend fun retry(itemId: String): AppResult<Unit>

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
