package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
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
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
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
 * The CALLER generates the idempotency key ONCE (e.g. `UUID.randomUUID()`) the first time
 * the user acts, and persists it alongside the draft (not just in a local var that a
 * ViewModel recreation would lose) so the SAME key is passed to `enqueue*` on every resend.
 * This port never mints a new key internally:
 * - a repeat `enqueue*` call with a key that already has a row is an idempotent no-op — the
 *   EXISTING row's id is returned, never a duplicate insert (enforced by a unique index on
 *   `idempotencyKey`, see `core-database`'s `OutboxEntity`);
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

    /** Enqueues a captured-proof registration write (`POST /app/proofs`). */
    suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
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
) : SyncRepository {

    private val onlineFlow = MutableStateFlow(connectivityGate.isOnline())
    private val _status = MutableStateFlow(SyncStatus.empty(online = onlineFlow.value))

    init {
        appScope.launch {
            combine(store.observeAll(), onlineFlow) { entities, online -> entities.toSyncStatus(online) }
                .collect { _status.value = it }
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
    ): AppResult<String> = enqueue(
        opType = OutboxOpType.PROOF_UPLOAD,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(ProofUploadPayload(request = request)),
    )

    private suspend fun enqueue(
        opType: OutboxOpType,
        groupKey: String,
        idempotencyKey: String,
        payloadJson: String,
    ): AppResult<String> = withContext(dispatchers.io) {
        runCatching {
            store.findByIdempotencyKey(idempotencyKey)?.let { existing -> return@runCatching existing.id }
            val now = clock()
            val id = UUID.randomUUID().toString()
            store.insert(
                OutboxEntity(
                    id = id,
                    opType = opType.name,
                    groupKey = groupKey,
                    idempotencyKey = idempotencyKey,
                    payloadJson = payloadJson,
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
            id
        }.onSuccess { triggerDrainAsync() }
            .fold(
                onSuccess = { AppResult.Ok(it) },
                onFailure = { e -> AppResult.Err("Couldn't queue the write: ${e.message}", e) },
            )
    }

    override suspend fun retry(itemId: String): AppResult<Unit> = withContext(dispatchers.io) {
        runCatching {
            store.findById(itemId) ?: throw NoSuchElementException("Outbox item not found: $itemId")
            store.markRetryReady(itemId, clock())
        }.onSuccess { triggerDrainAsync() }
            .fold(
                onSuccess = { AppResult.Ok(Unit) },
                onFailure = { e -> AppResult.Err("Couldn't retry: ${e.message}", e) },
            )
    }

    override suspend fun triggerDrain() {
        triggerDrainAsync()
    }

    private fun triggerDrainAsync() {
        appScope.launch { engine.drainOnce() }
    }
}
