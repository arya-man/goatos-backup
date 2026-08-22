package sg.mesha.goatos.core.data.capture

import android.database.SQLException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.transformWhile
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.data.executionPartitionKey
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.ProofCaptureStateEventEntity
import sg.mesha.goatos.core.database.capture.RfidScanAttemptDao
import sg.mesha.goatos.core.database.capture.RfidScanAttemptEntity
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import sg.mesha.goatos.core.database.capture.ScanUpsertResult
import sg.mesha.goatos.core.database.capture.ProofProcessingState
import sg.mesha.goatos.core.data.sync.GalleryProofSaver
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.syncJson
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import java.io.File
import java.net.URI
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import sg.mesha.goatos.core.database.capture.CaptureSyncStatus as EntitySyncStatus

/**
 * Room-first SSOT for a task's `goat_scan` recording-form field
 * (docs/mobile/proof-capture-sync-and-e2e.md §1). Every completed tag is written to Room
 * BEFORE it is reflected in the UI, deduped by (task, partition, field, tag) at the DB layer — the UI
 * observes [observeScannedTags]/[observeScannedCount], it never owns the list as transient
 * ViewModel state.
 *
 * `partitionLabel` is part of the operational identity for every execution read and write.
 * Callers opening a partition must pass that route label consistently; `null` means the shed is
 * unpartitioned and is normalized to `whole`, never "all partitions".
 */
interface ScanCaptureRepository {
    fun observeScannedTags(taskId: String, fieldKey: String, partitionLabel: String? = null): Flow<List<ScannedGoatRow>>
    fun observeScannedCount(taskId: String, fieldKey: String, partitionLabel: String? = null): Flow<Int>

    /** Every scanned tag across every `goat_scan` field of [taskId] in [partitionLabel] — the single Flow a
     *  ViewModel observes (one field or several); group by [ScannedGoatRow.fieldKey] for a
     *  per-field count/list. */
    fun observeAllForTask(taskId: String, partitionLabel: String? = null): Flow<List<ScannedGoatRow>>

    /** Persists one completed tag read to Room first; a repeat tag for the same field is a
     *  silent no-op (dedup).
     *
     *  [obligationRowVersion] is `obligation_instances.row_version` for [obligationId] — the
     *  server-issued capture-CYCLE discriminator folded into the outbox idempotency key (see
     *  [scanCaptureIdempotencyKey]). A verifier rejection bumps this on reopen, so a genuinely
     *  new scan after a reopen builds a NEW key and reaches the server, while a plain network
     *  retry of the SAME scan (same row_version) stays on the SAME key and dedupes as before. */
    suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String? = null,
        obligationId: String? = null,
        obligationRowVersion: Int = 0,
        capturedAtMs: Long? = null,
        partitionLabel: String? = null,
    )

    /** Persists a free-flow scan locally without creating a vaccination scan outbox item.
     * Returns false when the same normalized tag already exists for this task/partition/field. */
    suspend fun recordLocalScanIfAbsent(
        taskId: String,
        fieldKey: String,
        tag: String,
        capturedAtMs: Long? = null,
        partitionLabel: String? = null,
    ): Boolean

    /** Re-enqueues already-durable Room scan rows as backend draft captures. This is idempotent
     *  and exists for app/process re-entry after a prior build or crash left local evidence without
     *  a matching outbox row. */
    suspend fun enqueuePendingScans(taskId: String, fieldKey: String, partitionLabel: String? = null)

    suspend fun markLocalScanSynced(taskId: String, fieldKey: String, tag: String, partitionLabel: String? = null)

    /** All scanned tags across every `goat_scan` field of [taskId] in [partitionLabel] — used to build the
     *  shed-submit answer payload. */
    suspend fun tagsForTask(taskId: String, partitionLabel: String? = null): List<String>

    /** Full task-retirement cleanup. Deliberately removes every partition; execution screens must
     *  never use this to clear one submitted partition. */
    suspend fun clearForTask(taskId: String)
}

class DefaultScanCaptureRepository(
    private val dao: ScannedGoatDao,
    private val syncRepository: SyncRepository? = null,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : ScanCaptureRepository {

    override fun observeScannedTags(taskId: String, fieldKey: String, partitionLabel: String?): Flow<List<ScannedGoatRow>> =
        dao.observeForField(taskId, executionPartitionKey(partitionLabel), fieldKey)
            .map { rows -> rows.map { it.toRow() } }
            .flowOn(dispatchers.default)

    override fun observeScannedCount(taskId: String, fieldKey: String, partitionLabel: String?): Flow<Int> =
        dao.observeCountForField(taskId, executionPartitionKey(partitionLabel), fieldKey).flowOn(dispatchers.default)

    override fun observeAllForTask(taskId: String, partitionLabel: String?): Flow<List<ScannedGoatRow>> =
        dao.observeForTask(taskId, executionPartitionKey(partitionLabel))
            .map { rows -> rows.map { it.toRow() } }
            .flowOn(dispatchers.default)

    override suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        obligationRowVersion: Int,
        capturedAtMs: Long?,
        partitionLabel: String?,
    ) {
        val trimmed = tag.trim()
        if (trimmed.isEmpty()) return
        val durableCapturedAtMs = capturedAtMs?.takeIf { it > 0L } ?: clock()
        // upsertScan (not a plain insert-or-ignore): a tag collision against a STALE row from a
        // prior/reopened obligation cycle is fresh evidence, not a duplicate, and must still
        // write through to Room + the outbox. See ScanUpsertResult's kdoc for the incident this
        // closes (accepted scan attempt, zero durable capture, zero scan-captures POST).
        val result = withContext(dispatchers.io) {
            dao.upsertScan(
                ScannedGoatEntity(
                    id = idGenerator(),
                    taskId = taskId,
                    partitionKey = executionPartitionKey(partitionLabel),
                    fieldKey = fieldKey,
                    tag = trimmed,
                    goatId = goatId?.takeIf { it.isNotBlank() },
                    obligationId = obligationId?.takeIf { it.isNotBlank() },
                    capturedAtMs = durableCapturedAtMs,
                    syncStatus = EntitySyncStatus.PENDING.name,
                    obligationRowVersion = obligationRowVersion,
                ),
            )
        }
        if (result == ScanUpsertResult.DUPLICATE) return
        enqueueScanCapture(
            taskId = taskId,
            fieldKey = fieldKey,
            tag = trimmed,
            goatId = goatId,
            obligationId = obligationId,
            obligationRowVersion = obligationRowVersion,
            capturedAtMs = durableCapturedAtMs,
            partitionKey = executionPartitionKey(partitionLabel),
        )
    }

    override suspend fun recordLocalScanIfAbsent(
        taskId: String,
        fieldKey: String,
        tag: String,
        capturedAtMs: Long?,
        partitionLabel: String?,
    ): Boolean {
        val normalized = tag.filter { it.isLetterOrDigit() }.lowercase()
        if (normalized.isBlank()) return false
        val durableCapturedAtMs = capturedAtMs?.takeIf { it > 0L } ?: clock()
        return withContext(dispatchers.io) {
            val partitionKey = executionPartitionKey(partitionLabel)
            val existing = dao.findByTaskFieldTag(
                taskId = taskId,
                partitionKey = partitionKey,
                fieldKey = fieldKey,
                tag = normalized,
            )
            if (existing != null) return@withContext false
            dao.insert(
                ScannedGoatEntity(
                    id = idGenerator(),
                    taskId = taskId,
                    partitionKey = partitionKey,
                    fieldKey = fieldKey,
                    tag = normalized,
                    goatId = null,
                    obligationId = null,
                    capturedAtMs = durableCapturedAtMs,
                    syncStatus = EntitySyncStatus.PENDING.name,
                ),
            ) > 0L
        }
    }

    override suspend fun enqueuePendingScans(taskId: String, fieldKey: String, partitionLabel: String?) {
        if (syncRepository == null) return
        val partitionKey = executionPartitionKey(partitionLabel)
        val rows = withContext(dispatchers.io) {
            dao.listForField(taskId, partitionKey, fieldKey)
        }
        rows.forEach { row ->
            enqueueScanCapture(
                taskId = row.taskId,
                fieldKey = row.fieldKey,
                tag = row.tag,
                goatId = row.goatId,
                obligationId = row.obligationId,
                // P1 fix: recovery must replay the row's OWN obligationRowVersion, not the
                // enqueueScanCapture default of 0. Otherwise a reopened/rejected obligation's
                // durable row (row_version > 0) rebuilds its idempotency key with ov0 — the OLD
                // cycle's key — and a genuinely new post-reopen capture silently dedupes against
                // (or gets shadowed by) the prior cycle instead of reaching the server as fresh
                // evidence. See scanCaptureIdempotencyKey's kdoc.
                obligationRowVersion = row.obligationRowVersion,
                capturedAtMs = row.capturedAtMs,
                partitionKey = row.partitionKey,
            )
        }
    }

    override suspend fun markLocalScanSynced(taskId: String, fieldKey: String, tag: String, partitionLabel: String?) = withContext(dispatchers.io) {
        dao.markFieldTagStatus(
            taskId = taskId,
            partitionKey = executionPartitionKey(partitionLabel),
            fieldKey = fieldKey,
            tag = tag.filter { it.isLetterOrDigit() }.lowercase(),
            obligationId = null,
            status = EntitySyncStatus.SYNCED.name,
        )
    }

    private suspend fun enqueueScanCapture(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        obligationRowVersion: Int = 0,
        capturedAtMs: Long,
        partitionKey: String,
    ) {
        val syncKey = scanCaptureIdempotencyKey(taskId, partitionKey, fieldKey, tag, obligationId, obligationRowVersion)
        when (val result = syncRepository?.enqueueScanCapture(
            taskId = taskId,
            groupKey = "$taskId|$partitionKey",
            idempotencyKey = syncKey,
            partitionKey = partitionKey,
            request = ScanCaptureRequestDto(
                fieldKey = fieldKey,
                tag = tag,
                goatId = goatId?.takeIf { it.isNotBlank() },
                obligationId = obligationId?.takeIf { it.isNotBlank() },
                capturedAtMs = capturedAtMs,
            ),
        )) {
            is AppResult.Ok -> retryFailedScanCaptureIfNeeded(result.value)
            is AppResult.Err -> Unit
            null -> Unit
        }
    }

    private suspend fun retryFailedScanCaptureIfNeeded(outboxItemId: String) {
        val repo = syncRepository ?: return
        val item = when (val result = repo.findOutboxItem(outboxItemId)) {
            is AppResult.Ok -> result.value
            is AppResult.Err -> null
        } ?: return
        if (item.status != SyncItemStatus.FAILED) return
        repo.retry(outboxItemId)
    }

    override suspend fun tagsForTask(taskId: String, partitionLabel: String?): List<String> = withContext(dispatchers.io) {
        dao.listForTask(taskId, executionPartitionKey(partitionLabel)).map { it.tag }.distinct()
    }

    override suspend fun clearForTask(taskId: String) = withContext(dispatchers.io) {
        dao.clearForTask(taskId)
    }
}

private fun ScannedGoatEntity.toRow() = ScannedGoatRow(
    fieldKey = fieldKey,
    tag = tag,
    goatId = goatId,
    obligationId = obligationId,
    capturedAtMs = capturedAtMs,
    // Explicit mapping rather than valueOf-in-runCatching: an unrecognised string must fall to
    // PENDING (treat the capture as NOT yet seen by the backend, which is the safe side -- it
    // keeps showing locally and is never used to overrule a server status), and doing that with
    // a `when` means there is no exception to swallow in the first place.
    syncStatus = when (syncStatus.trim().uppercase()) {
        "SYNCED" -> CaptureSyncStatus.SYNCED
        "IN_FLIGHT" -> CaptureSyncStatus.IN_FLIGHT
        "FAILED" -> CaptureSyncStatus.FAILED
        else -> CaptureSyncStatus.PENDING
    },
    partitionKey = partitionKey,
    obligationRowVersion = obligationRowVersion,
)

/**
 * Idempotency key for a scan-capture outbox write, keyed by [obligationRowVersion] — the
 * server-issued `obligation_instances.row_version` capture-CYCLE discriminator (see
 * [ScanCaptureRepository.recordScan]'s kdoc) — not a device timestamp, deliberately: a
 * timestamp would build a NEW key on every network retry and duplicate the capture server-side,
 * while [obligationRowVersion] stays fixed across retries of the SAME scan (so a retry still
 * dedupes) and only changes when the DOMAIN reopens the obligation (a verifier rejection), which
 * is exactly when a fresh capture must reach the server rather than being silently absorbed as a
 * replay of the prior cycle's already-synced capture. Defaults to 0 for a caller that has not
 * threaded a row_version yet, matching every fresh row's baseline cycle so first-time enqueues
 * are unaffected. [enqueuePendingScans] recovery threads the row's OWN persisted
 * [ScannedGoatEntity.obligationRowVersion] (P1 fix) rather than relying on this default — a
 * durable row from a reopened/rejected obligation cycle must rebuild the SAME (non-zero) key its
 * original enqueue would have built, not silently fall back to the stale ov0 key.
 */
private fun scanCaptureIdempotencyKey(
    taskId: String,
    partitionKey: String,
    fieldKey: String,
    tag: String,
    obligationId: String?,
    obligationRowVersion: Int = 0,
): String {
    val partitionSegment = if (partitionKey == "whole") "" else ":partition:$partitionKey"
    val obligationSegment = obligationId?.takeIf { it.isNotBlank() }?.let { ":obligation:$it" }.orEmpty()
    return "scan:$taskId$partitionSegment:$fieldKey:${tag.filter { it.isLetterOrDigit() }.lowercase()}$obligationSegment:ov$obligationRowVersion"
}

interface ScanAttemptRepository {
    fun observeAttempts(taskId: String): Flow<List<RfidScanAttemptRow>>

    suspend fun recordAttempt(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        outcome: RfidScanAttemptOutcome,
        tagRole: RfidScanTagRole,
        reason: String?,
        capturedAtMs: Long? = null,
    )

    suspend fun attemptsForTask(taskId: String): List<RfidScanAttemptRow>

    suspend fun clearForTask(taskId: String)
}

class DefaultScanAttemptRepository(
    private val dao: RfidScanAttemptDao,
    private val syncRepository: SyncRepository? = null,
    private val appScope: CoroutineScope = CoroutineScope(DefaultDispatchers.io),
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : ScanAttemptRepository {
    override fun observeAttempts(taskId: String): Flow<List<RfidScanAttemptRow>> =
        dao.observeForTask(taskId).map { rows -> rows.map { it.toAttemptRow() } }.flowOn(dispatchers.default)

    override suspend fun recordAttempt(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        outcome: RfidScanAttemptOutcome,
        tagRole: RfidScanTagRole,
        reason: String?,
        capturedAtMs: Long?,
    ) {
        val trimmed = tag.trim()
        val normalized = normalizeTag(trimmed)
        if (trimmed.isEmpty() || normalized.isEmpty()) return
        val id = idGenerator()
        val durableCapturedAtMs = capturedAtMs?.takeIf { it > 0L } ?: clock()
        val idempotencyKey = "scan-attempt:$taskId:$id"
        val entity = RfidScanAttemptEntity(
            id = id,
            taskId = taskId,
            fieldKey = fieldKey,
            tag = trimmed,
            normalizedTag = normalized,
            goatId = goatId?.takeIf { it.isNotBlank() },
            obligationId = obligationId?.takeIf { it.isNotBlank() },
            outcome = outcome.wireValue,
            tagRole = tagRole.wireValue,
            reason = reason?.takeIf { it.isNotBlank() },
            capturedAtMs = durableCapturedAtMs,
            syncStatus = EntitySyncStatus.PENDING.name,
            idempotencyKey = idempotencyKey,
        )
        val inserted = withContext(dispatchers.io) { dao.insert(entity) }
        if (inserted <= 0L) return
        when (val result = syncRepository?.enqueueScanAttempt(
            taskId = taskId,
            groupKey = taskId,
            idempotencyKey = idempotencyKey,
            request = ScanAttemptRequestDto(
                fieldKey = fieldKey,
                tag = trimmed,
                normalizedTag = normalized,
                goatId = goatId?.takeIf { it.isNotBlank() },
                obligationId = obligationId?.takeIf { it.isNotBlank() },
                outcome = outcome.wireValue,
                tagRole = tagRole.wireValue,
                reason = reason?.takeIf { it.isNotBlank() },
                capturedAtMs = durableCapturedAtMs,
            ),
        )) {
            is AppResult.Ok -> followAttemptOutboxItem(id, result.value)
            is AppResult.Err -> dao.updateStatus(id, EntitySyncStatus.FAILED.name)
            null -> Unit
        }
    }

    private fun followAttemptOutboxItem(rowId: String, outboxItemId: String) {
        val repo = syncRepository ?: return
        appScope.launch(dispatchers.io, start = CoroutineStart.UNDISPATCHED) {
            try {
                repo.observeItem(outboxItemId)
                    .filterNotNull()
                    .distinctUntilChanged()
                    .transformWhile { item ->
                        emit(item)
                        item.status != SyncItemStatus.SUCCEEDED && !item.isDeadLetter && !item.conflict
                    }
                    .collect { item ->
                        when {
                            item.status == SyncItemStatus.IN_FLIGHT ->
                                dao.updateStatus(rowId, EntitySyncStatus.IN_FLIGHT.name)
                            item.status == SyncItemStatus.SUCCEEDED ->
                                dao.updateStatus(rowId, EntitySyncStatus.SYNCED.name)
                            item.isDeadLetter || item.conflict ->
                                dao.updateStatus(rowId, EntitySyncStatus.FAILED.name)
                            else -> Unit
                        }
                    }
            } catch (error: CancellationException) {
                throw error
            } catch (error: SQLException) {
                if (!error.message.orEmpty().contains("connection is closed", ignoreCase = true)) throw error
            }
        }
    }

    override suspend fun attemptsForTask(taskId: String): List<RfidScanAttemptRow> = withContext(dispatchers.io) {
        dao.listForTask(taskId).map { it.toAttemptRow() }
    }

    override suspend fun clearForTask(taskId: String) = withContext(dispatchers.io) {
        dao.clearForTask(taskId)
    }
}

private fun RfidScanAttemptEntity.toAttemptRow() = RfidScanAttemptRow(
    id = id,
    taskId = taskId,
    fieldKey = fieldKey,
    tag = tag,
    normalizedTag = normalizedTag,
    goatId = goatId,
    obligationId = obligationId,
    outcome = RfidScanAttemptOutcome.entries.firstOrNull { it.wireValue == outcome } ?: RfidScanAttemptOutcome.UNKNOWN,
    tagRole = RfidScanTagRole.entries.firstOrNull { it.wireValue == tagRole } ?: RfidScanTagRole.UNKNOWN,
    reason = reason,
    capturedAtMs = capturedAtMs,
)

private fun normalizeTag(tag: String): String = tag.filter { it.isLetterOrDigit() }.lowercase()

/**
 * Room-first SSOT for a task's `video_proof` recording-form fields
 * (docs/mobile/proof-capture-sync-and-e2e.md §2/§3). Every captured video is persisted to
 * Room BEFORE any network call, then a signed-upload write
 * ([SyncRepository.enqueueProofUpload]) is queued through the SAME durable outbox the
 * shed-submit write uses — background, survives process death, exactly-once via the row's
 * own idempotency key. [observeProofs] status transitions PENDING -> IN_FLIGHT -> SYNCED/FAILED
 * mirror the outbox item this capture drives (Photos/Drive "uploading -> synced" model).
 *
 * [SyncRepository.enqueueProofUpload]'s single outbox dispatch now runs the FULL signed-upload
 * flow (register metadata -> stream the video bytes to the signed URL -> call the completion
 * endpoint — see [sg.mesha.goatos.core.data.sync.SyncEngine.dispatchProofUpload]), so a row only
 * reaches [CaptureSyncStatus.SYNCED] once the video is actually durable server-side.
 *
 * `partitionLabel` is part of the operational identity for execution evidence. `null` means the
 * unpartitioned `whole` scope, not a task-wide wildcard.
 */
interface ProofCaptureRepository {
    fun observeProofs(taskId: String, partitionLabel: String? = null): Flow<List<ProofCaptureRow>>

    /** Persists a captured video to Room first, then queues its metadata registration.
     *  Returns [AppResult.Err] (no Room write) if the proof policy's per-partition subject cap is
     *  already reached.
     *
     *  [proofPolicy] (R50-027) drives the per-subject cap and the `capture_source` metadata sent
     *  with the registration; callers that have not loaded the task's SOP proof policy yet may
     *  omit it and fall back to [ProofPolicy.Default] (the historical hardcoded values).
     *
     *  [allowReplacementOverCap] permits captureReplacingLatest to bypass per-field cap temporarily
     *  for the new capture (the transient second row during replace). Manohar ordering: new proof
     *  succeeds first, old removed after. */
    suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String? = null,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String? = null,
        scopeType: String,
        scopeId: String,
        /** Device-clock record start/stop (Camera-only capture freshness metadata — see
         *  `ProofCaptureEntity`'s kdoc). */
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy = ProofPolicy.Default,
        partitionLabel: String? = null,
        awaitUploadEnqueue: Boolean = false,
        uploadGroupKey: String? = null,
        allowReplacementOverCap: Boolean = false,
    ): AppResult<ProofCaptureRow>

    suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit>

    suspend fun remove(taskId: String, id: String): AppResult<Unit>

    /** Re-arms a terminal FAILED proof upload row using the same proof idempotency key and
     *  payload. No-op for rows that are already queued/in-flight/synced. */
    suspend fun retryUpload(taskId: String, id: String): AppResult<Unit>

    suspend fun clearForTask(taskId: String)

    /** Latest active (non-FAILED) row held by [slot], or null if the slot is empty. Built on
     *  [observeProofs] so every implementer (Room-backed and fakes) shares one selection rule:
     *  most-recent [ProofCaptureRow.capturedAtMs] wins, matching [captureReplacingLatest]'s
     *  "replace the newest occupant" semantics. Default method — no per-implementer duplication
     *  of the slot-selection rule. */
    fun observeLatest(slot: EvidenceSlot): Flow<ProofCaptureRow?> =
        observeProofs(slot.identity.taskId, slot.identity.partitionKey.takeUnless { it == "whole" })
            .map { rows ->
                rows.filter { it.fieldKey == slot.fieldKey && it.syncStatus != CaptureSyncStatus.FAILED }
                    .maxByOrNull { it.capturedAtMs }
            }

    /** Count of active (non-FAILED, not-yet-delivered) rows held by [slot]. Mirrors
     *  [ProofCaptureDao.activeCountForField]'s "ACTIVE means in flight" rule so a delivered
     *  (serverProofId set) row does not hold the slot — see that query's kdoc for why a delivered
     *  proof must stay re-shootable across a reopened pen. */
    suspend fun activeCount(slot: EvidenceSlot): Int

    /**
     * Captures a new proof for [slot], then discards the slot's previous active occupants —
     * ordering per Manohar's rule: **capture the new evidence first, only discard old ones
     * after the new capture succeeds.** A failed new capture must never destroy evidence that was
     * already proving the slot; old rows are only removed once [capture] returns
     * [AppResult.Ok].
     *
     * Concurrent-replace safety: after successful capture, re-read ALL active rows for the slot
     * and remove any non-new occupants (not just a single `previous`). Two concurrent replaces
     * both read the same set of previous rows, both capture successfully, but the second remove() must still
     * eliminate the first's new row so exactly one active row remains. Keep Manohar ordering:
     * never remove anything unless the new capture returned Ok.
     *
     * CRITICAL: Implementations MUST re-read ALL active rows (not just the newest via observeLatest),
     * then remove all non-new ones by id. Per-slot serialization (via Mutex or equivalent) is required
     * to ensure concurrent replaces converge to exactly one active row.
     *
     * No default implementation — each implementer must provide full logic to safely re-read all
     * active rows and clean up old occupants while preserving Manohar ordering.
     */
    suspend fun captureReplacingLatest(
        slot: EvidenceSlot,
        subject: ProofSubject,
        subjectId: String? = null,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String? = null,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy = ProofPolicy.Default,
        awaitUploadEnqueue: Boolean = false,
        uploadGroupKey: String? = null,
    ): AppResult<ProofCaptureRow>
}

class DefaultProofCaptureRepository(
    private val dao: ProofCaptureDao,
    private val syncRepository: SyncRepository,
    private val appScope: CoroutineScope,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val mediaProcessor: ProofMediaProcessor = ProofMediaProcessor.Noop,
    private val galleryProofSaver: GalleryProofSaver = GalleryProofSaver.Noop,
    private val telemetry: ProofCaptureTelemetry = ProofCaptureTelemetry.Noop,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
    private val locationProvider: ProofLocationProvider = ProofLocationProvider.Unavailable,
    private val proofArtifactValidator: ProofArtifactValidator = NoopProofArtifactValidator,
    // Production always reconciles orphan uploads on construction. Tests set this false to drive
    // reconcileRecoverableUploadsNow() explicitly (awaited) instead of racing the fire-and-forget
    // init launch — Room's suspend @Query runs on Room's own executor, so a virtual-clock
    // advanceUntilIdle() cannot deterministically await the init launch.
    reconcileOnStartup: Boolean = true,
) : ProofCaptureRepository {
    private val gallerySaveLocks = ConcurrentHashMap<String, Mutex>()
    private val slotReplaceLocks = ConcurrentHashMap<String, Mutex>()

    /** P1 fix: retirement of a captureReplacingLatest slot's superseded occupant(s), keyed by the
     *  NEW row's id, fired synchronously from inside the SAME already-running status-transition
     *  handler that lands the row on SYNCED — [followOutboxItem], [reconcileOutboxTerminalState],
     *  and [recoverMissingProofUploadDriver]'s already-synced branch. Deliberately NOT a second,
     *  independently-launched Flow collector: a detached collector watching [observeProofs] would
     *  keep running for the lifetime of the repository even when the row never reaches a terminal
     *  state (e.g. a test that never drives its outbox item to completion), leaking a Room Flow
     *  subscription per replace and throwing once the underlying DB closes. Hooking the SAME
     *  per-row status pipeline every registration already runs makes retirement a one-shot,
     *  self-cleaning action with no independent lifecycle to leak. */
    private val pendingSlotRetirement = ConcurrentHashMap<String, suspend () -> Set<String>>()

    /** Fires and removes [rowId]'s pending slot-retirement action, if [captureReplacingLatest]
     *  registered one, returning the ids it actually removed (empty when there was no pending
     *  action, or it removed nothing). Safe to call for any row id — a plain registration (not a
     *  replace) simply has no entry and this is a no-op map lookup.
     *
     *  Callers that build an [observeProofs] emission from a `rows` snapshot taken BEFORE this
     *  call MUST subtract the returned ids from that snapshot: the retirement's `dao.delete` is a
     *  side effect on rows already fetched, and Room's invalidation-triggered re-emission (which
     *  would otherwise pick up the delete) arrives on a LATER emission — a `.first()` caller only
     *  sees the FIRST one and would otherwise get a stale row back in the very call that removed it. */
    private suspend fun fireSlotRetirementIfPending(rowId: String): Set<String> =
        pendingSlotRetirement.remove(rowId)?.invoke() ?: emptySet()

    init {
        // R50-028: never block DI construction on a Room read — launch on the app-lifetime scope
        // so a slow/large recovery walk cannot delay first frame or any other DI consumer of this
        // repository. reconcileRecoverableUploadsNow() itself walks bounded ~20-row keyset pages
        // instead of reading up to MAX_PROOFS_PER_TASK rows in one shot.
        if (reconcileOnStartup) {
            appScope.launch(dispatchers.io) {
                reconcileRecoverableUploadsNow()
            }
        }
    }

    override fun observeProofs(taskId: String, partitionLabel: String?): Flow<List<ProofCaptureRow>> =
        dao.observeForTaskPartition(taskId, executionPartitionKey(partitionLabel))
            .map { rows ->
                val retiredByReconcile = reconcileOutboxTerminalState(rows)
                // P1 fix: reconcileOutboxTerminalState's own SUCCEEDED branch only fires slot
                // retirement for a row it JUST transitioned to SYNCED — its outer filter skips a
                // row that was already SYNCED by the time this query ran (e.g. followOutboxItem's
                // detached collector wrote SYNCED first). Catch that case here too, so retirement
                // is guaranteed to fire the first time ANY caller observes a synced row with a
                // still-pending action, regardless of which path landed the SYNCED write.
                val retiredAlreadySynced = fireAnyPendingRetirementsFor(rows)
                // A row retirement just removed may still be sitting in THIS `rows` snapshot (it
                // was fetched before the removal) — Room's own re-emission for the delete lands on
                // a LATER collection, which a `.first()` caller never sees. Filter it out here so
                // this emission is never stale-by-one-delete.
                val retired = retiredByReconcile + retiredAlreadySynced
                rows.filter { it.id !in retired }.map { it.toRow() }
            }
            .flowOn(dispatchers.io)

    /** Fires [fireSlotRetirementIfPending] for every already-[CaptureSyncStatus.SYNCED] row in
     *  [rows] that still has a pending action registered, returning every id removed. Cheap no-op
     *  when [pendingSlotRetirement] is empty (the common case: most rows are never part of a
     *  captureReplacingLatest replace). */
    private suspend fun fireAnyPendingRetirementsFor(rows: List<ProofCaptureEntity>): Set<String> {
        val retired = mutableSetOf<String>() // mobile-guard:ignore: function-local accumulator, returned and GC-ed per call
        rows.forEach { row ->
            if (row.syncStatus == EntitySyncStatus.SYNCED.name && !row.serverProofId.isNullOrBlank()) {
                if (pendingSlotRetirement.isNotEmpty()) retired += fireSlotRetirementIfPending(row.id)
                // P1 fix (CRITICAL follow-up): the durable marker path — re-derives retirement
                // even with an EMPTY pendingSlotRetirement map (a fresh repository instance after
                // process death has no in-memory ticket at all).
                retired += retireSupersededRowIfAny(row)
            }
        }
        return retired
    }

    /** P1 fix (CRITICAL follow-up): durable counterpart to the in-memory
     *  [pendingSlotRetirement] ticket. If [row] is SYNCED with a serverProofId and carries a
     *  [ProofCaptureEntity.supersedesRowId], retires the row it names — re-derivable from durable
     *  state alone, so a process death between a successful captureReplacingLatest and the new
     *  row reaching SYNCED can never lose the retirement intent. Idempotent: once the superseded
     *  row is gone, [ProofCaptureDao.findById] returns null and this is a no-op on every later
     *  pass. */
    private suspend fun retireSupersededRowIfAny(row: ProofCaptureEntity): Set<String> {
        // The marker means "this row REPLACED its slot", not "it replaced exactly that one row":
        // the live path can find MULTIPLE old active occupants (a prior replacement that itself
        // died mid-retirement leaves two), and persisting a single id let the extras survive a
        // process death (external review 2026-08-16). Retire EVERY other active same-slot,
        // same-subject row older than the replacement — the named id is just the trigger.
        row.supersedesRowId?.takeIf { it.isNotBlank() } ?: return emptySet()
        val retired = mutableSetOf<String>() // mobile-guard:ignore: function-local accumulator, returned and GC-ed per call
        dao.listForTask(row.taskId)
            .filter {
                it.id != row.id &&
                    it.partitionKey == row.partitionKey &&
                    it.fieldKey == row.fieldKey &&
                    it.subjectId == row.subjectId &&
                    it.capturedAtMs <= row.capturedAtMs &&
                    it.syncStatus != EntitySyncStatus.FAILED.name
            }
            .forEach { superseded ->
                if (remove(superseded.taskId, superseded.id) is AppResult.Ok) retired += superseded.id
            }
        return retired
    }

    override suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        partitionLabel: String?,
        awaitUploadEnqueue: Boolean,
        uploadGroupKey: String?,
        allowReplacementOverCap: Boolean,
    ): AppResult<ProofCaptureRow> = captureInternal(
        taskId = taskId,
        fieldKey = fieldKey,
        subject = subject,
        subjectId = subjectId,
        localUri = localUri,
        mimeType = mimeType,
        caption = caption,
        rfidTag = rfidTag,
        scopeType = scopeType,
        scopeId = scopeId,
        capturedStartMs = capturedStartMs,
        capturedEndMs = capturedEndMs,
        capturedByPrincipalId = capturedByPrincipalId,
        proofPolicy = proofPolicy,
        partitionLabel = partitionLabel,
        awaitUploadEnqueue = awaitUploadEnqueue,
        uploadGroupKey = uploadGroupKey,
        allowReplacementOverCap = allowReplacementOverCap,
        supersedesRowId = null,
    )

    /** The real capture implementation. Deliberately NOT part of [ProofCaptureRepository]'s
     *  public interface — [supersedesRowId] is an internal-only detail of
     *  [captureReplacingLatest]'s durable-supersession fix (P1 CRITICAL follow-up); adding it to
     *  the public [capture] signature would force every test double implementing
     *  [ProofCaptureRepository] (several, across modules this fix does not own) to redeclare a
     *  parameter they have no reason to know about. [capture] is a thin public wrapper below that
     *  always passes null. */
    private suspend fun captureInternal(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        partitionLabel: String?,
        awaitUploadEnqueue: Boolean,
        uploadGroupKey: String?,
        allowReplacementOverCap: Boolean,
        supersedesRowId: String?,
    ): AppResult<ProofCaptureRow> = withContext(dispatchers.io) {
        val partitionKey = executionPartitionKey(partitionLabel)
        val effectiveSubjectId = subjectId?.takeIf { it.isNotBlank() }
        if (subject == ProofSubject.GOAT && effectiveSubjectId == null) {
            return@withContext AppResult.Err("Select a scanned goat before recording proof.")
        }
        // R50-027: caps are policy-driven. Per-goat mode uses the per-subject cap; shed-level
        // mode uses the SOP's shed total cap (1 required, up to 5 videos) because the whole shed
        // is the proof subject.
        val maxPerSubject = if (proofPolicy.isShedLevelVideo && subject == ProofSubject.SHED) {
            proofPolicy.maximumCount
        } else {
            proofPolicy.maximumCountPerSubject
        }
        // A policy that caps PER SLOT is counted per slot. Screens whose slots are distinct required
        // steps (feed distribution's weight photo / feed video / water video) share one subject —
        // the shed — so the per-subject count below pools all three into one budget, and the screen
        // locks itself out long before any single slot is over-filled.
        val perFieldCap = proofPolicy.maximumCountPerField
        if (perFieldCap != null && !allowReplacementOverCap) {
            val existingForField = dao.activeCountForField(taskId, partitionKey, fieldKey)
            if (existingForField >= perFieldCap) {
                return@withContext AppResult.Err(
                    "This proof is already recorded. Use re-capture to replace it.",
                )
            }
        }
        val existing = when {
            subject == ProofSubject.GOAT && effectiveSubjectId != null ->
                dao.activeCountForSubject(taskId, partitionKey, effectiveSubjectId)
            subject != ProofSubject.GOAT && effectiveSubjectId != null ->
                dao.activeCountForSubject(taskId, partitionKey, effectiveSubjectId)
            // R50-027: a generic (shed/vial/administration) capture has no per-goat subjectId, so
            // count active proofs of that subject TYPE for this operational partition — otherwise the cap saw 0 and
            // never applied, leaving generic proofs unbounded.
            subject != ProofSubject.GOAT && effectiveSubjectId == null ->
                dao.activeCountForSubjectType(taskId, partitionKey, subject.wireValue)
            else -> 0
        }
        val bypassHistoricalGoatProofCap = subject == ProofSubject.GOAT && effectiveSubjectId != null
        if (!bypassHistoricalGoatProofCap && existing >= maxPerSubject) {
            val subjectLabel = when (subject) {
                ProofSubject.GOAT -> "goat"
                ProofSubject.SHED -> "shed"
                ProofSubject.VIAL_LOT -> "vial"
                ProofSubject.ADMINISTRATION -> "administration"
                else -> "subject"
            }
            return@withContext AppResult.Err(
                "Maximum $maxPerSubject proof videos reached for this $subjectLabel.",
            )
        }
        val id = idGenerator()
        val idempotencyKey = "proof-upload:$taskId:$id"
        val location = runCatching { locationProvider.snapshot() }
            .getOrElse { ProofLocationSnapshot(locationStatus = "failed", geocoderStatus = "failed") }
        val entity = ProofCaptureEntity(
            id = id,
            taskId = taskId,
            partitionKey = partitionKey,
            fieldKey = fieldKey,
            proofSubject = subject.wireValue,
            subjectId = effectiveSubjectId,
            localUri = localUri,
            mimeType = mimeType,
            caption = caption,
            rfidTag = rfidTag?.takeIf { it.isNotBlank() },
            capturedAtMs = clock(),
            capturedStartMs = capturedStartMs,
            capturedEndMs = capturedEndMs,
            capturedByPrincipalId = capturedByPrincipalId,
            syncStatus = EntitySyncStatus.PENDING.name,
            idempotencyKey = idempotencyKey,
            // R50-027 SSOT: persist capture_source with the durable row so the startup-recovery
            // re-registration path re-sends the ORIGINAL source, not a Default fallback.
            captureSource = proofPolicy.captureSource,
            // R50-060: Persist original scopeType and scopeId for correct recovery on app restart.
            // Critical for weighing free-flow proofs (subject_type="other") where scope cannot
            // be re-derived from subjectId (null). Backend validation fails if scope differs
            // between initial attempt and recovery.
            scopeType = scopeType,
            scopeId = scopeId,
            originalUri = localUri,
            durationMs = (capturedEndMs - capturedStartMs).coerceAtLeast(0),
            locationStatus = location.locationStatus,
            latitude = location.latitude,
            longitude = location.longitude,
            gpsAccuracyM = location.gpsAccuracyM,
            geocoderStatus = location.geocoderStatus,
            geocodedAddress = location.address,
            updatedAtMs = clock(),
            // P1 fix (CRITICAL follow-up): durable supersession marker, part of this SAME insert.
            supersedesRowId = supersedesRowId?.takeIf { it.isNotBlank() },
            // Codex blocker 3: persist uploadGroupKey for ordering/grouping recovery. This is the
            // exact group key passed at capture time; startup recovery re-enqueues with this verbatim
            // to preserve proof ordering (feed, milk, packing flows) across process death.
            uploadGroupKey = uploadGroupKey?.takeIf { it.isNotBlank() },
            // Derive clientTaskKey from uploadGroupKey when present; falls back to taskId for legacy.
            // This is the application-level session/context id (e.g. feed workflow id, milk batch id).
            // Recovery uses this to preserve grouping semantics across process death.
            clientTaskKey = uploadGroupKey?.takeIf { it.isNotBlank() } ?: taskId,
        )
        // Gate 3: Backstop validation — file must exist && length > 0 before Room insert.
        // Mime-aware: a JPEG must never be judged by the video duration probe (OEMs that report
        // duration=0 for images would reject every valid photo at this gate).
        val validationResult = if (mimeType.startsWith("image/")) {
            proofArtifactValidator.validateImageFile(localUri)
        } else {
            proofArtifactValidator.validateVideoFile(localUri)
        }
        if (!validationResult.isValid) {
            return@withContext AppResult.Err(validationResult.reason ?: "Proof file is invalid. Please re-record.")
        }
        // Room FIRST — the capture is durable before any network call is even attempted.
        dao.insert(entity)
        telemetry.track(proofCaptureCompletedEvent, proofAnalyticsProps(entity))
        if (awaitUploadEnqueue) {
            enqueueRegistrationNow(entity, scopeType, scopeId, uploadGroupKey)
            AppResult.Ok((dao.findById(id) ?: entity).toRow())
        } else {
            enqueueRegistration(entity, scopeType, scopeId, uploadGroupKey)
            AppResult.Ok(entity.toRow())
        }
    }

    override suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit> =
        withContext(dispatchers.io) {
            dao.updateCaption(id, taskId, caption)
            AppResult.Ok(Unit)
        }

    override suspend fun remove(taskId: String, id: String): AppResult<Unit> = withContext(dispatchers.io) {
        // R50-028: fetch the row BEFORE processing so its local video file can be reclaimed too —
        // otherwise every removed proof leaks its recorded clip on device storage forever.
        // BUG#8 FIX: cancel the outbox upload FIRST (and guard IN_FLIGHT), only delete row/file if success
        val entity = dao.findById(id)?.takeIf { it.taskId == taskId }
            ?: return@withContext AppResult.Ok(Unit)

        val outboxItemId = entity.outboxItemId?.takeIf { it.isNotBlank() }
        if (!outboxItemId.isNullOrBlank()) {
            // R50-028 (TOCTOU fix): atomically cancel the outbox upload — a single status-guarded
            // DELETE that only removes the item if it is still QUEUED/FAILED. There is NO separate
            // "check IN_FLIGHT then delete" window for the dispatcher to claim the upload in between.
            when (val cancel = syncRepository.cancelOutboxItemIfPending(outboxItemId)) {
                is AppResult.Err -> return@withContext cancel
                is AppResult.Ok -> if (!cancel.value) {
                    // The guarded delete removed 0 rows: the dispatcher claimed the item (IN_FLIGHT)
                    // at that instant. Only delete the local row+file if the outbox row is now GONE
                    // or SUCCEEDED (the server has the proof). If ANY non-success row still exists —
                    // still IN_FLIGHT, or it raced to a RETRYABLE FAILED/QUEUED between our guarded
                    // delete and the dispatcher's markFailed — REFUSE: deleting the file would strand
                    // a retryable upload of a now-missing file (orphan). The user retries remove()
                    // once it settles (then the guarded delete succeeds on the QUEUED/FAILED row).
                    val current = syncRepository.observeItem(outboxItemId).first()
                    if (current != null && current.status != SyncItemStatus.SUCCEEDED) {
                        return@withContext AppResult.Err(
                            "Cannot delete proof while its upload is in progress. Wait for it to finish or fail, then retry.",
                        )
                    }
                    // else: gone (null) or SUCCEEDED — deleting the local row + file leaves no orphan.
                }
            }
        }

        val serverProofId = entity.serverProofId?.takeIf { it.isNotBlank() }
        if (serverProofId != null) {
            when (val deleted = syncRepository.deleteUploadedProof(serverProofId)) {
                is AppResult.Err -> return@withContext deleted
                is AppResult.Ok -> Unit
            }
        }

        // Row + file are removed only after the outbox item is cancelled or proven terminal.
        dao.delete(id, taskId)
        deleteLocalFiles(entity)
        AppResult.Ok(Unit)
    }

    override suspend fun retryUpload(taskId: String, id: String): AppResult<Unit> = withContext(dispatchers.io) {
        val entity = dao.findById(id) ?: return@withContext AppResult.Err("Proof video not found.")
        if (entity.taskId != taskId) return@withContext AppResult.Err("Proof video does not belong to this task.")
        // P1 fix: a row stuck AWAITING_RETRY never got a syncStatus=FAILED (registration/upload
        // never ran), so the FAILED-only guard below would silently no-op it forever. This is the
        // operator's explicit "retry processing" action: reset processingAttempted so
        // prepareFinalArtifact re-invokes the media processor (a fresh attempt, not the crash-mid-
        // PROCESSING_MEDIA recovery branch), then drive registration through the normal path.
        if (entity.processingState == ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name) {
            dao.updateProcessingState(
                id = entity.id,
                processingState = ProofProcessingState.CAPTURED_ORIGINAL.name,
                attempt = entity.stateAttempt,
                processingAttempted = false,
                uploadOriginal = false,
                lastErrorStage = null,
                lastErrorClass = null,
                lastErrorRetryable = null,
                lastErrorMessageHash = null,
                updatedAtMs = clock(),
            )
            val recovered = dao.findById(entity.id) ?: entity.copy(
                processingState = ProofProcessingState.CAPTURED_ORIGINAL.name,
                processingAttempted = false,
                uploadOriginal = false,
            )
            val (scopeType, scopeId) = recoveryScope(recovered)
            enqueueRegistrationNow(recovered, scopeType, scopeId)
            return@withContext AppResult.Ok(Unit)
        }
        if (entity.syncStatus != EntitySyncStatus.FAILED.name) return@withContext AppResult.Ok(Unit)
        val outboxItemId = entity.outboxItemId
        if (outboxItemId.isNullOrBlank()) {
            dao.updateStatus(entity.id, EntitySyncStatus.PENDING.name, null, null)
            val recovered = dao.findById(entity.id)
                ?: entity.copy(syncStatus = EntitySyncStatus.PENDING.name, outboxItemId = null, lastError = null)
            val (scopeType, scopeId) = recoveryScope(recovered)
            enqueueRegistrationNow(recovered, scopeType, scopeId)
            return@withContext AppResult.Ok(Unit)
        }
        when (val retry = syncRepository.retry(outboxItemId)) {
            is AppResult.Ok -> {
                dao.updateStatus(entity.id, EntitySyncStatus.PENDING.name, null, null)
                followOutboxItem(entity.id, outboxItemId)
                AppResult.Ok(Unit)
            }
            is AppResult.Err -> retry
        }
    }

    override suspend fun clearForTask(taskId: String) = withContext(dispatchers.io) {
        // R50-029: reclaim EVERY row's local video file using keyset pagination (matching the
        // row-delete set) — no read/delete-count mismatch even if task has >MAX_PROOFS_PER_TASK proofs.
        // The cleanup read is task-scoped and includes terminal rows. Reusing the global
        // recoverable-upload query here skipped SYNCED/FAILED files and could stop on a page made
        // entirely of another task's rows after post-filtering.
        var afterCapturedAtMs = Long.MIN_VALUE
        var afterId = ""
        while (true) {
            val page = dao.listForTaskCleanupPage(
                taskId = taskId,
                afterCapturedAtMs = afterCapturedAtMs,
                afterId = afterId,
            )
            if (page.isEmpty()) break
            page.forEach { entity ->
                deleteLocalFiles(entity)
            }
            val last = page.last()
            afterCapturedAtMs = last.capturedAtMs
            afterId = last.id
            if (page.size < ProofCaptureDao.TASK_CLEANUP_PAGE_SIZE) break
        }
        dao.clearForTask(taskId)
    }

    override suspend fun activeCount(slot: EvidenceSlot): Int = withContext(dispatchers.io) {
        dao.activeCountForField(slot.identity.taskId, executionPartitionKey(slot.identity.partitionKey), slot.fieldKey)
    }

    override suspend fun captureReplacingLatest(
        slot: EvidenceSlot,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        rfidTag: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        awaitUploadEnqueue: Boolean,
        uploadGroupKey: String?,
    ): AppResult<ProofCaptureRow> {
        // ITEM 7: Use Mutex per slot+subject grain to serialize concurrent replaces, ensuring
        // they converge to one active row per subject. Key must include subject grain
        // (subjectId when the policy is per-subject) to prevent cross-subject collisions.
        // Matching activeCountForSubject scoping: subjectId null/shed → shed-level, else per-subject.
        val effectiveSubjectId = subjectId?.takeIf { it.isNotBlank() }
        val slotKey = "${slot.identity.taskId}|${slot.fieldKey}|${effectiveSubjectId ?: "shed"}"
        val mutex = slotReplaceLocks.getOrPut(slotKey) { Mutex() }

        return mutex.withLock {
            val taskId = slot.identity.taskId
            val partitionLabel = slot.identity.partitionKey.takeUnless { it == "whole" }
            val partitionKey = executionPartitionKey(partitionLabel)
            // P1 fix (CRITICAL follow-up): capture the CURRENT single active occupant's id BEFORE
            // the new capture, so the new row's insert can durably record what it supersedes.
            // Same selection rule retireSlotAction uses (most-recent active row for this exact
            // subject grain) — "the previous occupant" this replace is standing in for.
            val previousOccupantId = dao.listForTask(taskId)
                .filter {
                    it.partitionKey == partitionKey &&
                        it.fieldKey == slot.fieldKey &&
                        it.syncStatus != EntitySyncStatus.FAILED.name &&
                        it.subjectId == effectiveSubjectId
                }
                .maxByOrNull { it.capturedAtMs }
                ?.id
            // Capture with allowReplacementOverCap=true to bypass per-field cap during replace (Manohar ordering).
            val result = captureInternal(
                taskId = taskId,
                fieldKey = slot.fieldKey,
                subject = subject,
                subjectId = subjectId,
                localUri = localUri,
                mimeType = mimeType,
                caption = caption,
                rfidTag = rfidTag,
                scopeType = scopeType,
                scopeId = scopeId,
                capturedStartMs = capturedStartMs,
                capturedEndMs = capturedEndMs,
                capturedByPrincipalId = capturedByPrincipalId,
                proofPolicy = proofPolicy,
                partitionLabel = partitionLabel,
                awaitUploadEnqueue = awaitUploadEnqueue,
                uploadGroupKey = uploadGroupKey,
                allowReplacementOverCap = true,  // Allow transient second row during replace
                supersedesRowId = previousOccupantId,
            )
            if (result is AppResult.Ok) {
                val newId = result.value.id
                val newSubjectId = result.value.subjectId
                // P1 fix: do NOT retire the old occupant(s) synchronously here. The old row may
                // already be server-SYNCED valid evidence; if the NEW upload later dead-letters or
                // its processed artifact turns out bad, an immediate remove() would have destroyed
                // the only proof the slot had. Register a one-shot retirement action, keyed by the
                // NEW row's id, that [fireSlotRetirementIfPending] invokes from inside the SAME
                // status-transition handler ([followOutboxItem] / [reconcileOutboxTerminalState] /
                // [recoverMissingProofUploadDriver]) the instant this row actually reaches SYNCED
                // with a serverProofId — never a separately-launched, independently-lived collector.
                // A terminal FAILED new row instead drops the action (see those call sites), so old
                // rows stay untouched.
                pendingSlotRetirement[newId] = retireSlotAction(taskId, partitionLabel, slot.fieldKey, newId, newSubjectId)
                // Safety for an outbox/dispatcher fast enough to reach SYNCED before the line above
                // ran (e.g. a synchronous test double): fire immediately rather than waiting for a
                // status transition that already happened.
                val current = dao.findById(newId)
                if (current != null &&
                    current.syncStatus == EntitySyncStatus.SYNCED.name &&
                    !current.serverProofId.isNullOrBlank()
                ) {
                    fireSlotRetirementIfPending(newId)
                }
            }
            result
        }
    }

    /** Builds the one-shot action [pendingSlotRetirement] holds for [newId]: re-reads ALL active
     *  rows for the slot (not just a stored `previous`) and removes every other active row of
     *  [newSubjectId] held by [fieldKey] — same ITEM 7 reasoning as before (two concurrent
     *  replaces for the same subject must still converge to exactly one active row). */
    private fun retireSlotAction(
        taskId: String,
        partitionLabel: String?,
        fieldKey: String,
        newId: String,
        newSubjectId: String?,
    ): suspend () -> Set<String> = {
        // Deliberately a plain suspend DAO read, NOT observeProofs(...).first(): this action can
        // fire from inside reconcileOutboxTerminalState, which itself runs inside observeProofs()'s
        // OWN map operator — re-entering that same Flow's query here raced/stalled against the
        // outer collection in practice. A direct DAO read has no such reentrancy.
        val partitionKey = executionPartitionKey(partitionLabel)
        val allActive = dao.listForTask(taskId)
            .filter { it.partitionKey == partitionKey && it.fieldKey == fieldKey && it.syncStatus != EntitySyncStatus.FAILED.name }
        val removedIds = mutableSetOf<String>() // mobile-guard:ignore: function-local accumulator, returned and GC-ed per call
        allActive.forEach { r ->
            if (r.id != newId && r.subjectId == newSubjectId) {
                if (remove(taskId, r.id) is AppResult.Ok) removedIds += r.id
            }
        }
        removedIds
    }

    /** Startup/process-recreation recovery for the two proof/outbox crash gaps:
     *  1. proof row exists but process died before [enqueueRegistration] wrote [ProofCaptureEntity.outboxItemId];
     *  2. proof row already has an outbox id but the old in-memory status collector died.
     *
     * Re-enqueue uses the row's stable [ProofCaptureEntity.idempotencyKey], so if the old process
     * did create the outbox row but died before recording its id, [SyncRepository.enqueueProofUpload]
     * returns that existing row instead of inserting a duplicate.
     *
     * R50-029: walks [ProofCaptureDao.listRecoverableUploadsPage] in bounded ~20-row keyset pages
     * (row-value keyset on capturedAtMs+id) using monotonic cursor, NOT a wall-clock cutoff.
     * Device clock rollback cannot skip recovery. Runs on the caller's dispatcher (the `init`
     * block launches it on [appScope] so it never blocks repository construction). */
    internal suspend fun reconcileRecoverableUploadsNow() {
        var afterCapturedAtMs = 0L
        var afterId = ""
        while (true) {
            val page = dao.listRecoverableUploadsPage(
                capturedBeforeMs = Long.MAX_VALUE, // R50-029: no cutoff, read ALL from cursor
                afterCapturedAtMs = afterCapturedAtMs,
                afterId = afterId,
            )
            if (page.isEmpty()) break
            page.forEach { entity ->
                val outboxItemId = entity.outboxItemId
                if (outboxItemId.isNullOrBlank()) {
                    val (scopeType, scopeId) = recoveryScope(entity)
                    // Use persisted uploadGroupKey (and clientTaskKey) to preserve proof ordering across
                    // process death. Legacy null falls back to current derivation.
                    enqueueRegistrationNow(
                        entity,
                        scopeType,
                        scopeId,
                        uploadGroupKey = entity.uploadGroupKey?.takeIf { it.isNotBlank() },
                    )
                } else {
                    when (val recovered = syncRepository.findOutboxItem(outboxItemId)) {
                        is AppResult.Err -> Unit
                        is AppResult.Ok -> {
                            if (recovered.value == null) {
                                recoverMissingProofUploadDriver(entity)
                            } else {
                                followOutboxItem(entity.id, outboxItemId)
                                syncRepository.triggerDrain()
                            }
                        }
                    }
                }
            }
            val last = page.last()
            afterCapturedAtMs = last.capturedAtMs
            afterId = last.id
            if (page.size < ProofCaptureDao.RECOVERABLE_UPLOADS_PAGE_SIZE) break
        }
    }

    /** Queues the metadata-registration write on the app-lifetime scope (never blocks the
     *  caller's [capture] — the Room write above already made the capture durable) and follows
     *  its outbox item to reflect PENDING -> IN_FLIGHT -> SYNCED/FAILED back onto the row. */
    private fun enqueueRegistration(
        entity: ProofCaptureEntity,
        scopeType: String,
        scopeId: String,
        uploadGroupKey: String? = null,
    ) {
        appScope.launch(dispatchers.io, start = CoroutineStart.UNDISPATCHED) {
            enqueueRegistrationNow(entity, scopeType, scopeId, uploadGroupKey)
        }
    }

    private suspend fun enqueueRegistrationNow(
        entity: ProofCaptureEntity,
        scopeType: String,
        scopeId: String,
        uploadGroupKey: String? = null,
    ) {
        val uploadEntity = prepareFinalArtifact(entity)
        // P1 fix: a processing failure that left the row AWAITING_RETRY must never proceed to
        // registration/upload — that would ship the raw original as completed proof for a
        // required-overlay flow. The row's DB state already reflects "awaiting operator action"
        // (see prepareFinalArtifact's failure catch blocks); nothing more to do here until an
        // explicit retry (retryUpload) resets processingAttempted and re-invokes the processor.
        if (uploadEntity.processingState == ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name) {
            return
        }
        val request = ProofUploadRequestDto(
            proofType = proofTypeForMime(uploadEntity.mimeType),
            mimeType = uploadEntity.mimeType,
            scopeType = scopeType,
            scopeId = scopeId,
            subjectType = uploadEntity.proofSubject,
            subjectId = uploadEntity.subjectId,
            metadata = buildMap {
                put("field_key", JsonPrimitive(uploadEntity.fieldKey))
                // Use persisted clientTaskKey if available; this is the application-level session/context
                // id (e.g. feed workflow id, milk batch id). Recovery re-sends the ORIGINAL key to preserve
                // grouping across process death. Legacy null falls back to uploadGroupKey (if present),
                // then taskId.
                put("client_task_key", JsonPrimitive(
                    uploadEntity.clientTaskKey?.takeIf { it.isNotBlank() }
                        ?: uploadGroupKey?.takeIf { it.isNotBlank() }
                        ?: uploadEntity.taskId
                ))
                // R50-027 SSOT: capture_source is read from the durable row, so the startup-recovery
                // path (which has no in-memory ProofPolicy) re-sends the ORIGINAL source instead of a
                // Default fallback that would silently rewrite a non-camera source.
                put("capture_source", JsonPrimitive(uploadEntity.captureSource))
                put("upload_original", JsonPrimitive(uploadEntity.uploadOriginal))
                uploadEntity.caption?.takeIf { it.isNotBlank() }?.let { put("caption", JsonPrimitive(it)) }
                // Camera-only capture freshness proof (docs/mobile/proof-capture-sync-and-e2e.md
                // "Camera-only capture"): the verifier can see this was a live, timed,
                // attributable in-app recording, not an imported file.
                put("captured_start_ms", JsonPrimitive(uploadEntity.capturedStartMs))
                put("captured_end_ms", JsonPrimitive(uploadEntity.capturedEndMs))
                put("duration_ms", JsonPrimitive((uploadEntity.capturedEndMs - uploadEntity.capturedStartMs).coerceAtLeast(0)))
                uploadEntity.locationStatus?.takeIf { it.isNotBlank() }
                    ?.let { put("location_status", JsonPrimitive(it)) }
                uploadEntity.latitude?.let { put("latitude", JsonPrimitive(it)) }
                uploadEntity.longitude?.let { put("longitude", JsonPrimitive(it)) }
                uploadEntity.gpsAccuracyM?.let { put("gps_accuracy_m", JsonPrimitive(it)) }
                uploadEntity.geocoderStatus?.takeIf { it.isNotBlank() }
                    ?.let { put("geocoder_status", JsonPrimitive(it)) }
                uploadEntity.geocodedAddress?.takeIf { it.isNotBlank() }
                    ?.let { put("geocoded_address", JsonPrimitive(it)) }
                humanRfidTag(uploadEntity)?.let { put("rfid_tag", JsonPrimitive(it)) }
                uploadEntity.capturedByPrincipalId?.takeIf { it.isNotBlank() }
                    ?.let { put("captured_by_principal_id", JsonPrimitive(it)) }
            },
        )
        saveFinalArtifactToGallery(uploadEntity, request)
        when (
            val result = syncRepository.enqueueProofUpload(
                groupKey = uploadGroupKey?.takeIf { it.isNotBlank() } ?: proofUploadGroupKey(uploadEntity, scopeId),
                idempotencyKey = uploadEntity.idempotencyKey,
                request = request,
                localFilePath = uploadEntity.localUri,
                durationMs = (uploadEntity.capturedEndMs - uploadEntity.capturedStartMs).coerceAtLeast(0),
            )
        ) {
            is AppResult.Ok -> {
                dao.updateProcessingState(
                    id = uploadEntity.id,
                    processingState = ProofProcessingState.REGISTERING_UPLOAD.name,
                    attempt = uploadEntity.stateAttempt,
                    processingAttempted = uploadEntity.processingAttempted,
                    uploadOriginal = uploadEntity.uploadOriginal,
                    lastErrorStage = null,
                    lastErrorClass = null,
                    lastErrorRetryable = null,
                    lastErrorMessageHash = null,
                    updatedAtMs = clock(),
                )
                recordProofEvent(uploadEntity, "upload_enqueued", uploadEntity.processingState, uploadEntity.stateAttempt)
                telemetry.track(proofUploadRegisteredEvent, proofAnalyticsProps(uploadEntity))
                dao.setOutboxItemId(uploadEntity.id, result.value)
                followOutboxItem(uploadEntity.id, result.value)
            }
            is AppResult.Err -> dao.updateStatus(uploadEntity.id, EntitySyncStatus.FAILED.name, null, result.message)
        }
    }

    private suspend fun prepareFinalArtifact(entity: ProofCaptureEntity): ProofCaptureEntity {
        if (entity.processingAttempted) {
            // Recovery: if row is in PROCESSING_MEDIA state with no final artifact,
            // the media processor crashed mid-transform. Re-invoke processing.
            if (entity.processingState == ProofProcessingState.PROCESSING_MEDIA.name &&
                entity.processedUri.isNullOrBlank()
            ) {
                val attempt = entity.stateAttempt + 1
                return try {
                    val processed = mediaProcessor.process(
                        ProofMediaProcessingRequest(
                            proofId = entity.id,
                            taskId = entity.taskId,
                            fieldKey = entity.fieldKey,
                            subjectType = entity.proofSubject,
                            subjectId = entity.subjectId,
                            rfidTag = humanRfidTag(entity),
                            originalUri = entity.originalUri ?: entity.localUri,
                            mimeType = entity.mimeType,
                            capturedStartMs = entity.capturedStartMs,
                            capturedEndMs = entity.capturedEndMs,
                            capturedByPrincipalId = entity.capturedByPrincipalId,
                            locationAddress = entity.geocodedAddress,
                            latitude = entity.latitude,
                            longitude = entity.longitude,
                            gpsAccuracyM = entity.gpsAccuracyM,
                            caption = entity.caption,
                        ),
                    )
                    validateProcessedArtifact(entity, processed)
                    dao.updateProcessingArtifact(
                        id = entity.id,
                        localUri = processed.outputUri,
                        mimeType = processed.outputMimeType,
                        processingState = ProofProcessingState.PROCESSED.name,
                        processingAttempted = true,
                        uploadOriginal = false,
                        processedUri = processed.outputUri,
                        originalBytes = processed.originalBytes,
                        processedBytes = processed.processedBytes,
                        inputWidth = processed.inputWidth,
                        inputHeight = processed.inputHeight,
                        targetVideoBitrate = processed.targetVideoBitrate,
                        targetAudioBitrate = processed.targetAudioBitrate,
                        updatedAtMs = clock(),
                    )
                    dao.findById(entity.id) ?: entity.copy(
                        localUri = processed.outputUri,
                        mimeType = processed.outputMimeType,
                        processingState = ProofProcessingState.PROCESSED.name,
                        processingAttempted = true,
                        stateAttempt = attempt,
                        uploadOriginal = false,
                        processedUri = processed.outputUri,
                        originalBytes = processed.originalBytes,
                        processedBytes = processed.processedBytes,
                    )
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Throwable) {
                    // P1 fix: processing failed again on retry. Original stays on disk (safety
                    // preserved) but is NOT queued for upload — required-overlay flows must never
                    // let an overlay-free capture satisfy a compliance proof gate. See
                    // PROCESSING_FAILED_AWAITING_RETRY's kdoc.
                    val errorClass = error::class.java.simpleName.ifBlank { "Throwable" }
                    val failureProps = proofProcessingFailureProps(error)
                    dao.updateProcessingArtifact(
                        id = entity.id,
                        localUri = entity.originalUri ?: entity.localUri,
                        mimeType = entity.mimeType,
                        processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                        processingAttempted = true,
                        uploadOriginal = false,
                        processedUri = null,
                        originalBytes = localFileBytes(entity.originalUri ?: entity.localUri),
                        processedBytes = null,
                        inputWidth = entity.inputWidth,
                        inputHeight = entity.inputHeight,
                        targetVideoBitrate = entity.targetVideoBitrate,
                        targetAudioBitrate = entity.targetAudioBitrate,
                        updatedAtMs = clock(),
                    )
                    dao.updateProcessingState(
                        id = entity.id,
                        processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                        attempt = attempt,
                        processingAttempted = true,
                        uploadOriginal = false,
                        lastErrorStage = "processing",
                        lastErrorClass = errorClass,
                        lastErrorRetryable = true,
                        lastErrorMessageHash = error.message?.hashCode()?.toString(),
                        updatedAtMs = clock(),
                    )
                    recordProofEvent(
                        entity,
                        "processing_failed_awaiting_retry",
                        ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                        attempt,
                        bytesIn = localFileBytes(entity.originalUri ?: entity.localUri),
                        errorClass = errorClass,
                        retryable = true,
                    )
                    telemetry.track(
                        proofProcessingFailedEvent,
                        proofAnalyticsProps(entity, attempt = attempt, uploadOriginal = false) + failureProps,
                    )
                    dao.findById(entity.id) ?: entity.copy(
                        processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                        uploadOriginal = false,
                    )
                }
            }
            return awaitFinalArtifact(entity)
        }
        val startedAtMs = clock()
        val attempt = entity.stateAttempt + 1
        dao.updateProcessingState(
            id = entity.id,
            processingState = ProofProcessingState.PROCESSING_MEDIA.name,
            attempt = attempt,
            processingAttempted = true,
            uploadOriginal = false,
            lastErrorStage = null,
            lastErrorClass = null,
            lastErrorRetryable = null,
            lastErrorMessageHash = null,
            updatedAtMs = startedAtMs,
        )
        recordProofEvent(entity, "processing_started", ProofProcessingState.PROCESSING_MEDIA.name, attempt)
        telemetry.track(proofProcessingStartedEvent, proofAnalyticsProps(entity, attempt = attempt))

        return try {
            val processed = mediaProcessor.process(
                ProofMediaProcessingRequest(
                    proofId = entity.id,
                    taskId = entity.taskId,
                    fieldKey = entity.fieldKey,
                    subjectType = entity.proofSubject,
                    subjectId = entity.subjectId,
                    rfidTag = humanRfidTag(entity),
                    originalUri = entity.originalUri ?: entity.localUri,
                    mimeType = entity.mimeType,
                    capturedStartMs = entity.capturedStartMs,
                    capturedEndMs = entity.capturedEndMs,
                    capturedByPrincipalId = entity.capturedByPrincipalId,
                    locationAddress = entity.geocodedAddress,
                    latitude = entity.latitude,
                    longitude = entity.longitude,
                    gpsAccuracyM = entity.gpsAccuracyM,
                    caption = entity.caption,
                ),
            )
            validateProcessedArtifact(entity, processed)
            dao.updateProcessingArtifact(
                id = entity.id,
                localUri = processed.outputUri,
                mimeType = processed.outputMimeType,
                processingState = ProofProcessingState.PROCESSED.name,
                processingAttempted = true,
                uploadOriginal = false,
                processedUri = processed.outputUri,
                originalBytes = processed.originalBytes,
                processedBytes = processed.processedBytes,
                inputWidth = processed.inputWidth,
                inputHeight = processed.inputHeight,
                targetVideoBitrate = processed.targetVideoBitrate,
                targetAudioBitrate = processed.targetAudioBitrate,
                updatedAtMs = clock(),
            )
            recordProofEvent(
                entity,
                "processing_completed",
                ProofProcessingState.PROCESSED.name,
                attempt,
                durationMs = clock() - startedAtMs,
                bytesIn = processed.originalBytes,
                bytesOut = processed.processedBytes,
            )
            telemetry.track(proofProcessingCompletedEvent, proofAnalyticsProps(entity, attempt = attempt, uploadOriginal = false))
            dao.findById(entity.id) ?: entity.copy(
                localUri = processed.outputUri,
                mimeType = processed.outputMimeType,
                processingState = ProofProcessingState.PROCESSED.name,
                processingAttempted = true,
                stateAttempt = attempt,
                uploadOriginal = false,
                processedUri = processed.outputUri,
                originalBytes = processed.originalBytes,
                processedBytes = processed.processedBytes,
            )
        } catch (error: CancellationException) {
            throw error
        } catch (error: Throwable) {
            // P1 fix: a required-overlay flow (every flow — there is no proof_policy opt-in for
            // originals today) must never let a processing failure silently ship an overlay-free
            // original as completed proof. The original stays on disk (safety preserved, same as
            // before) but the row is left AWAITING_RETRY and is NOT enqueued for upload —
            // enqueueRegistrationNow short-circuits on this state. An operator must explicitly
            // retry (re-record, or retryUpload() once the processor recovers).
            val errorClass = error::class.java.simpleName.ifBlank { "Throwable" }
            val failureProps = proofProcessingFailureProps(error)
            dao.updateProcessingArtifact(
                id = entity.id,
                localUri = entity.originalUri ?: entity.localUri,
                mimeType = entity.mimeType,
                processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                processingAttempted = true,
                uploadOriginal = false,
                processedUri = null,
                originalBytes = localFileBytes(entity.originalUri ?: entity.localUri),
                processedBytes = null,
                inputWidth = entity.inputWidth,
                inputHeight = entity.inputHeight,
                targetVideoBitrate = entity.targetVideoBitrate,
                targetAudioBitrate = entity.targetAudioBitrate,
                updatedAtMs = clock(),
            )
            dao.updateProcessingState(
                id = entity.id,
                processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                attempt = attempt,
                processingAttempted = true,
                uploadOriginal = false,
                lastErrorStage = "processing",
                lastErrorClass = errorClass,
                lastErrorRetryable = true,
                lastErrorMessageHash = error.message?.hashCode()?.toString(),
                updatedAtMs = clock(),
            )
            recordProofEvent(
                entity,
                "processing_failed_awaiting_retry",
                ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                attempt,
                durationMs = clock() - startedAtMs,
                bytesIn = localFileBytes(entity.originalUri ?: entity.localUri),
                errorClass = errorClass,
                retryable = true,
            )
            telemetry.track(
                proofProcessingFailedEvent,
                proofAnalyticsProps(entity, attempt = attempt, uploadOriginal = false) + failureProps,
            )
            dao.findById(entity.id) ?: entity.copy(
                localUri = entity.originalUri ?: entity.localUri,
                processingState = ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                processingAttempted = true,
                stateAttempt = attempt,
                uploadOriginal = false,
            )
        }
    }

    /**
     * ITEM 6: Validate PROCESSED artifact with full metadata probe (duration/dimensions).
     * For processed files we control the encoder on, metadata-probe failure is DEFINITIVE
     * rejection, not plausible-accept (unlike original camera files with OEM quirks).
     * Throws if validation fails (causing prepareFinalArtifact to catch + fall back to original).
     */
    private fun validateProcessedArtifact(
        entity: ProofCaptureEntity,
        processed: ProofMediaProcessingResult,
    ) {
        val originalUri = entity.originalUri ?: entity.localUri
        require(processed.outputUri.isNotBlank()) { "processed artifact path is blank" }
        require(processed.outputUri != originalUri) { "processed artifact reused original source" }
        processed.processedBytes?.let { bytes ->
            require(bytes > 0L) { "processed artifact is empty" }
        }
        // ITEM 6: Run full metadata validation (duration/dimensions) on PROCESSED output.
        // Strict mode: no plausible-accept for processed files.
        val validation = proofArtifactValidator.validateProcessedArtifact(processed.outputUri, processed.outputMimeType)
        if (!validation.isValid) {
            throw ProcessedArtifactValidationException(
                reason = validation.reason ?: "unknown error",
                failureKind = validation.failureKind ?: "processed_artifact_validation_failed",
                containerDurationMs = validation.containerDurationMs,
                videoTrackDurationMs = validation.videoTrackDurationMs,
            )
        }
    }

    private suspend fun awaitFinalArtifact(entity: ProofCaptureEntity): ProofCaptureEntity {
        repeat(PROOF_PROCESSING_WAIT_POLLS) {
            val current = dao.findById(entity.id) ?: entity
            if (current.syncStatus == "SYNCED" && !current.serverProofId.isNullOrBlank()) {
                return current
            }
            when (current.processingState) {
                ProofProcessingState.PROCESSED.name,
                ProofProcessingState.PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED.name,
                // P1 fix: this is now a terminal (operator-actionable) state, not a transient stop
                // on the way to an auto-queued original upload — stop polling and return it as-is.
                ProofProcessingState.PROCESSING_FAILED_AWAITING_RETRY.name,
                ProofProcessingState.REGISTERING_UPLOAD.name,
                ProofProcessingState.UPLOADING.name,
                ProofProcessingState.UPLOAD_CONFIRMED.name -> return current
            }
            delay(PROOF_PROCESSING_WAIT_MS)
        }
        error("proof processing did not reach a final artifact before upload")
    }

    private suspend fun saveFinalArtifactToGallery(entity: ProofCaptureEntity, request: ProofUploadRequestDto) {
        gallerySaveLocks.getOrPut(entity.id) { Mutex() }.withLock {
            val current = dao.findById(entity.id) ?: entity
            if (!current.gallerySavedUri.isNullOrBlank()) return
            if (dao.countStateEvents(current.id, gallerySaveCompletedStage) > 0) {
                dao.markGallerySaved(current.id, current.localUri, clock())
                return
            }
            check(
                current.processingState == ProofProcessingState.PROCESSED.name ||
                    current.processingState == ProofProcessingState.PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED.name,
            ) {
                "refusing to save non-final proof artifact to Gallery: ${current.processingState}"
            }
            val startedAtMs = clock()
            telemetry.track(proofGallerySaveStartedEvent, proofAnalyticsProps(current))
            try {
                galleryProofSaver.saveProofCopy(current.localUri, request, current.idempotencyKey)
                dao.markGallerySaved(current.id, current.localUri, clock())
                recordProofEvent(
                    current,
                    gallerySaveCompletedStage,
                    current.processingState,
                    current.stateAttempt,
                    durationMs = clock() - startedAtMs,
                    bytesIn = current.processedBytes ?: current.originalBytes ?: localFileBytes(current.localUri),
                )
                telemetry.track(proofGallerySaveCompletedEvent, proofAnalyticsProps(current) + ("duration_ms" to (clock() - startedAtMs).toString()))
            } catch (error: Throwable) {
                val errorClass = error::class.java.simpleName.ifBlank { "Throwable" }
                recordProofEvent(
                    current,
                    "gallery_save_failed_upload_continues",
                    current.processingState,
                    current.stateAttempt,
                    durationMs = clock() - startedAtMs,
                    errorClass = errorClass,
                    retryable = false,
                )
                telemetry.track(proofGallerySaveFailedEvent, proofAnalyticsProps(current) + ("error_class" to errorClass))
            }
        }
    }

    private suspend fun recordProofEvent(
        entity: ProofCaptureEntity,
        stage: String,
        toState: String,
        attempt: Int,
        durationMs: Long? = null,
        bytesIn: Long? = null,
        bytesOut: Long? = null,
        errorClass: String? = null,
        retryable: Boolean? = null,
    ) {
        dao.insertStateEvent(
            ProofCaptureStateEventEntity(
                id = idGenerator(),
                proofId = entity.id,
                fromState = entity.processingState,
                toState = toState,
                stage = stage,
                attempt = attempt,
                occurredAtMs = clock(),
                durationMs = durationMs,
                bytesIn = bytesIn,
                bytesOut = bytesOut,
                errorClass = errorClass,
                retryable = retryable,
            ),
        )
    }

    private fun proofUploadGroupKey(entity: ProofCaptureEntity, scopeId: String): String =
        when {
            entity.proofSubject.equals(ProofSubject.GOAT.wireValue, ignoreCase = true) &&
                !entity.subjectId.isNullOrBlank() -> entity.subjectId.orEmpty()
            scopeId.isNotBlank() -> scopeId
            else -> entity.taskId
        }

    /** R50-029: follows one outbox item to ITS terminal state via observeItem (by-id, window-independent),
     *  then stops — [transformWhile] ends the collection right after the terminal emission, so this
     *  coroutine (and its subscription to the per-item [SyncRepository.observeItem] flow) does not
     *  outlive the proof it was tracking. Before the fix (using observeStatus().map{}), high-volume
     *  terminal updates could drop the item from the bounded global window, leaving a proof stuck
     *  before reaching its terminal state. */
    private fun followOutboxItem(rowId: String, outboxItemId: String) {
        appScope.launch(dispatchers.io, start = CoroutineStart.UNDISPATCHED) {
            try {
                syncRepository.observeItem(outboxItemId)
                    .filterNotNull()
                    .distinctUntilChanged()
                    .transformWhile { item ->
                        emit(item)
                        item.status != SyncItemStatus.SUCCEEDED && !item.isDeadLetter && !item.conflict
                    }
                    .collect { item ->
                        when {
                            item.status == SyncItemStatus.IN_FLIGHT ->
                                if (dao.findById(rowId)?.syncStatus != EntitySyncStatus.IN_FLIGHT.name) {
                                    dao.updateStatus(rowId, EntitySyncStatus.IN_FLIGHT.name, null, null)
                                    dao.findById(rowId)?.let {
                                        recordProofEvent(it, "upload_started", EntitySyncStatus.IN_FLIGHT.name, it.stateAttempt)
                                        telemetry.track(proofUploadStartedEvent, proofAnalyticsProps(it, proofUploadStatus = "in_flight"))
                                    }
                                }
                            item.status == SyncItemStatus.SUCCEEDED -> {
                                val proofId = decodeServerProofId(item.resultJson)
                                if (proofId.isNullOrBlank()) {
                                    dao.updateStatus(rowId, EntitySyncStatus.FAILED.name, null, corruptProofUploadResultMessage)
                                } else {
                                    // Keep the app-private proof files while the Room row lives so
                                    // open tasks can still render previews after the backend upload
                                    // succeeds. Explicit remove()/clearForTask() reclaim them; the
                                    // separate Android Gallery artifact is never touched here.
                                    dao.updateStatus(rowId, EntitySyncStatus.SYNCED.name, proofId, null)
                                    // P1 fix: deliberately NOT firing slot retirement from here. This
                                    // collector runs detached on [appScope] — a caller has no handle
                                    // to await it, so any occupant it removes races the NEXT reader of
                                    // [observeProofs] instead of being ordered before it (proven by a
                                    // Room-executor-hop race: Room's suspend DAO calls dispatch onto
                                    // Room's OWN query executor, not this collector's dispatcher, so
                                    // "detached collector already wrote SYNCED" does not mean "detached
                                    // collector already finished retiring the old row" by the time a
                                    // caller's subsequent observeProofs().first() runs). Retirement
                                    // instead fires from [reconcileOutboxTerminalState], which runs
                                    // SYNCHRONOUSLY inside observeProofs()'s own map operator — so ANY
                                    // caller that awaits observeProofs() (a live UI collector, or a test
                                    // driving sync to completion then reading) deterministically
                                    // observes retirement as part of that SAME awaited call, in Room's
                                    // actual write order, with nothing left to race.
                                    dao.findById(rowId)?.let {
                                        recordProofEvent(it, "upload_completed", EntitySyncStatus.SYNCED.name, it.stateAttempt)
                                        telemetry.track(proofUploadCompletedEvent, proofAnalyticsProps(it, proofUploadStatus = "synced"))
                                    }
                                }
                            }
                            item.isDeadLetter || item.conflict -> {
                                dao.updateStatus(rowId, EntitySyncStatus.FAILED.name, null, item.lastError)
                                dao.findById(rowId)?.let {
                                    recordProofEvent(it, "upload_failed", EntitySyncStatus.FAILED.name, it.stateAttempt, errorClass = item.proofUploadFailureReason(), retryable = false)
                                    telemetry.track(
                                        proofUploadFailedEvent,
                                        proofAnalyticsProps(it, proofUploadStatus = "failed") + ("reason" to item.proofUploadFailureReason()),
                                    )
                                }
                                // P1 fix: a terminal FAILED new row must leave the old occupant(s)
                                // untouched — drop the pending action rather than ever firing it.
                                pendingSlotRetirement.remove(rowId)
                            }
                            else -> Unit // QUEUED / still-retrying FAILED — leave PENDING, another emission follows.
                        }
                    }
            } catch (error: CancellationException) {
                throw error
            } catch (error: SQLException) {
                if (!error.message.orEmpty().contains("connection is closed", ignoreCase = true)) throw error
            }
        }
    }

    private suspend fun recoverMissingProofUploadDriver(entity: ProofCaptureEntity): Set<String> {
        if (!entity.serverProofId.isNullOrBlank()) {
            if (entity.syncStatus != EntitySyncStatus.SYNCED.name || entity.lastError != null) {
                dao.updateStatus(entity.id, EntitySyncStatus.SYNCED.name, entity.serverProofId, null)
            }
            // P1 fix (CRITICAL follow-up): fire BOTH the in-memory ticket (same process, still
            // live) and the durable-marker path (survives process death — this recovery walk is
            // exactly the "fresh repository instance after process death" case).
            return fireSlotRetirementIfPending(entity.id) + retireSupersededRowIfAny(entity)
        }
        if (!entity.isRecoverableUploadState()) return emptySet()
        dao.setOutboxItemId(entity.id, null)
        dao.updateStatus(entity.id, EntitySyncStatus.PENDING.name, null, null)
        val recovered = (dao.findById(entity.id) ?: entity.copy(outboxItemId = null, syncStatus = EntitySyncStatus.PENDING.name))
        val (scopeType, scopeId) = recoveryScope(recovered)
        enqueueRegistrationNow(recovered, scopeType, scopeId)
        return emptySet()
    }

    /** F1a: Uses the PERSISTED scope (scope_type and scope_id) from the entity for correct
     *  re-registration on app restart. R50-060: Free-flow weighing proofs (subject_type="other")
     *  have scope_id=null and cannot re-derive scope from subjectId. The persisted scope_type and
     *  scope_id MUST match the original capture, or backend /app/proofs/uploads validateCreate
     *  will reject the re-registration with invalid_proof.
     *
     *  Fallback derivation is LEGACY and only used for rows migrated before R50-060 (scope fields
     *  were not yet persisted). New captures always persist scope.
     */
    private fun recoveryScope(entity: ProofCaptureEntity): Pair<String, String> {
        // R50-060: Use persisted scope. Fallback only for legacy entities.
        if (entity.scopeType.isNotBlank() && entity.scopeId.isNotBlank()) {
            return entity.scopeType to entity.scopeId
        }
        // LEGACY FALLBACK: For rows migrated from v44. Shed-level proofs can re-derive.
        val shedId = entity.subjectId
        return if (entity.proofSubject.equals("shed", ignoreCase = true) && !shedId.isNullOrBlank()) {
            "shed" to shedId
        } else {
            "task" to entity.taskId
        }
    }

    /** Reconciles proof rows from durable outbox state when lifecycle churn missed the live
     *  followOutboxItem() terminal emission. The UI remains Room-first: this only repairs
     *  proof_capture from the persisted outbox result before readiness is calculated.
     *  F4: Guard each updateStatus call so it only fires when values actually differ,
     *  preventing redundant re-emission churn. */
    private suspend fun reconcileOutboxTerminalState(rows: List<ProofCaptureEntity>): Set<String> {
        val retired = mutableSetOf<String>() // mobile-guard:ignore: function-local accumulator, returned and GC-ed per call
        rows.asSequence()
            .filter { it.syncStatus != EntitySyncStatus.SYNCED.name }
            .forEach { row ->
                val outboxItemId = row.outboxItemId?.takeIf(String::isNotBlank)
                if (outboxItemId == null) {
                    if (row.isRecoverableUploadState()) retired += recoverMissingProofUploadDriver(row)
                    return@forEach
                }
                when (val recovered = syncRepository.findOutboxItem(outboxItemId)) {
                    is AppResult.Err -> Unit
                    is AppResult.Ok -> {
                        val item = recovered.value
                        if (item == null) {
                            retired += recoverMissingProofUploadDriver(row)
                            return@forEach
                        }
                        when {
                            item.status == SyncItemStatus.SUCCEEDED -> {
                                val proofId = decodeServerProofId(item.resultJson)
                                if (proofId.isNullOrBlank()) {
                                    val newStatus = EntitySyncStatus.FAILED.name
                                    if (row.syncStatus != newStatus || row.serverProofId != null || row.lastError != corruptProofUploadResultMessage) {
                                        dao.updateStatus(row.id, newStatus, null, corruptProofUploadResultMessage)
                                        dao.findById(row.id)?.let {
                                            telemetry.track(proofUploadFailedEvent, proofAnalyticsProps(it, proofUploadStatus = "failed") + ("reason" to "missing_server_proof_id"))
                                        }
                                    }
                                } else {
                                    val newStatus = EntitySyncStatus.SYNCED.name
                                    if (row.syncStatus != newStatus || row.serverProofId != proofId || row.lastError != null) {
                                        dao.updateStatus(row.id, newStatus, proofId, null)
                                        dao.findById(row.id)?.let {
                                            telemetry.track(proofUploadCompletedEvent, proofAnalyticsProps(it, proofUploadStatus = "synced"))
                                        }
                                    }
                                    retired += fireSlotRetirementIfPending(row.id)
                                    // P1 fix (CRITICAL follow-up): durable-marker path, alongside
                                    // the in-memory ticket — see retireSupersededRowIfAny's kdoc.
                                    // Only reads row.supersedesRowId, so the pre-update `row` (not
                                    // yet reflecting the just-written SYNCED status) is fine here.
                                    retired += retireSupersededRowIfAny(row)
                                }
                            }
                            item.status == SyncItemStatus.IN_FLIGHT && row.syncStatus != EntitySyncStatus.IN_FLIGHT.name -> {
                                val newStatus = EntitySyncStatus.IN_FLIGHT.name
                                if (row.syncStatus != newStatus || row.serverProofId != null || row.lastError != null) {
                                    dao.updateStatus(row.id, newStatus, null, null)
                                    dao.findById(row.id)?.let {
                                        telemetry.track(proofUploadStartedEvent, proofAnalyticsProps(it, proofUploadStatus = "in_flight"))
                                    }
                                }
                            }
                            item.isDeadLetter || item.conflict -> {
                                val newStatus = EntitySyncStatus.FAILED.name
                                if (row.syncStatus != newStatus || row.serverProofId != null || row.lastError != item.lastError) {
                                    dao.updateStatus(row.id, newStatus, null, item.lastError)
                                    dao.findById(row.id)?.let {
                                        telemetry.track(
                                            proofUploadFailedEvent,
                                            proofAnalyticsProps(it, proofUploadStatus = "failed") + ("reason" to item.proofUploadFailureReason()),
                                        )
                                    }
                                }
                                pendingSlotRetirement.remove(row.id)
                            }
                        }
                    }
                }
            }
        return retired
    }
}

private fun ProofCaptureEntity.isRecoverableUploadState(): Boolean =
    syncStatus == EntitySyncStatus.PENDING.name || syncStatus == EntitySyncStatus.IN_FLIGHT.name

/** R50-028: best-effort local-file cleanup for a removed/cleared proof. Synced rows retain
 *  app-private files until the proof row is explicitly removed or the task is cleared, so open
 *  screens can preview the exact uploaded artifact. Deliberately
 *  silent on failure (a stale on-disk clip the OS will eventually reclaim under storage pressure
 *  is not a correctness issue, unlike a swallowed business-logic error) — accepts both the
 *  `file:` URI form [InAppVideoRecorder] writes and a plain path, mirroring
 *  `ProofBlobUploader.resolveLocalFile`. */
private fun deleteLocalFile(localUri: String) {
    if (localUri.isBlank()) return
    runCatching {
        val file = if (localUri.startsWith("file:", ignoreCase = true)) {
            runCatching { File(URI(localUri)) }.getOrElse { File(localUri) }
        } else {
            File(localUri)
        }
        if (file.exists()) file.delete()
    }
}

private fun deleteLocalFiles(entity: ProofCaptureEntity) {
    listOf(entity.localUri, entity.originalUri, entity.processedUri)
        .filterNotNull()
        .distinct()
        .forEach(::deleteLocalFile)
}

private fun decodeServerProofId(resultJson: String?): String? {
    if (resultJson.isNullOrBlank()) return null
    return runCatching { syncJson.decodeFromString<ProofUploadResponseDto>(resultJson).proof.proofId }
        .onFailure { android.util.Log.w("CaptureRepository", "decodeServerProofId: deserialize proof upload response failed", it) }
        .getOrNull()
        ?.takeIf { it.isNotBlank() }
}

private const val corruptProofUploadResultMessage = "Proof upload finished without a server proof id. Record this video again."

private const val proofProcessingStartedEvent = "proof_processing_started"
private const val proofProcessingCompletedEvent = "proof_processing_completed"
private const val proofProcessingFailedEvent = "proof_processing_failed"
private const val proofCaptureCompletedEvent = "proof_capture_completed"
private const val proofGallerySaveStartedEvent = "proof_gallery_save_started"
private const val proofGallerySaveCompletedEvent = "proof_gallery_save_completed"
private const val proofGallerySaveFailedEvent = "proof_gallery_save_failed"
private const val gallerySaveCompletedStage = "gallery_save_completed"
private const val PROOF_PROCESSING_WAIT_POLLS = 240
private const val PROOF_PROCESSING_WAIT_MS = 500L
private const val proofUploadRegisteredEvent = "proof_upload_registered"
private const val proofUploadStartedEvent = "proof_upload_started"
private const val proofUploadCompletedEvent = "proof_upload_completed"
private const val proofUploadFailedEvent = "proof_upload_failed"

private fun proofAnalyticsProps(
    entity: ProofCaptureEntity,
    attempt: Int = entity.stateAttempt,
    uploadOriginal: Boolean = entity.uploadOriginal,
    proofUploadStatus: String = entity.syncStatus.lowercase(),
): Map<String, String> = buildMap {
    put("proof_id", entity.id)
    put("task_id", entity.taskId)
    put("partition_key", entity.partitionKey)
    put("field_key", entity.fieldKey)
    put("proof_subject", entity.proofSubject)
    entity.subjectId?.takeIf { it.isNotBlank() }?.let { put("subject_id", it) }
    humanRfidTag(entity)?.let { put("rfid_tag", it) }
    entity.featureSurface?.takeIf { it.isNotBlank() }?.let { put("feature_surface", it) }
    entity.proofMode?.takeIf { it.isNotBlank() }?.let { put("proof_mode", it) }
    entity.slotIndex?.let { put("slot_index", it.toString()) }
    put("slot_required", entity.slotRequired.toString())
    put("capture_source", entity.captureSource)
    entity.capturedByPrincipalId?.takeIf { it.isNotBlank() }?.let { put("operator_principal_id", it) }
    put("mime_type", entity.mimeType)
    put("processing_state", entity.processingState)
    put("processing_attempt", attempt.toString())
    put("proof_upload_status", proofUploadStatus)
    put("upload_original", uploadOriginal.toString())
    put("duration_bucket", durationBucket(entity.durationMs ?: (entity.capturedEndMs - entity.capturedStartMs).coerceAtLeast(0)))
    entity.originalBytes?.let { put("original_size_bucket", byteBucket(it)) }
    entity.processedBytes?.let { put("processed_size_bucket", byteBucket(it)) }
    entity.inputWidth?.let { put("input_width", it.toString()) }
    entity.inputHeight?.let { put("input_height", it.toString()) }
    entity.targetVideoBitrate?.let { put("target_video_bitrate", it.toString()) }
    entity.targetAudioBitrate?.let { put("target_audio_bitrate", it.toString()) }
    entity.locationStatus?.takeIf { it.isNotBlank() }?.let { put("location_status", it) }
    entity.latitude?.let { put("latitude", it.toString()) }
    entity.longitude?.let { put("longitude", it.toString()) }
    entity.gpsAccuracyM?.let { put("gps_accuracy_m", it.toString()) }
    entity.geocoderStatus?.takeIf { it.isNotBlank() }?.let { put("geocoder_status", it) }
    entity.geocodedAddress?.takeIf { it.isNotBlank() }?.let { put("geocoded_address", it) }
}

private fun humanRfidTag(entity: ProofCaptureEntity): String? =
    entity.rfidTag
        ?.takeIf { entity.fieldKey in rfidBurnOverlayFieldKeys }
        ?.takeIf { it.isNotBlank() }

private fun SyncQueueItem.proofUploadFailureReason(): String = when {
    conflict -> "conflict"
    isDeadLetter -> "attempts_exhausted"
    else -> "retryable_failure"
}

private class ProcessedArtifactValidationException(
    val reason: String,
    val failureKind: String,
    val containerDurationMs: Long?,
    val videoTrackDurationMs: Long?,
) : IllegalStateException("Processed artifact validation failed: $reason")

private fun proofProcessingFailureProps(error: Throwable): Map<String, String> = buildMap {
    val errorClass = error::class.java.simpleName.ifBlank { "Throwable" }
    put("error_class", errorClass)
    if (error is ProcessedArtifactValidationException) {
        put("failure_kind", error.failureKind)
        put("reason", error.failureKind)
        put("validation_reason", error.reason)
        error.containerDurationMs?.let { put("container_duration_ms", it.toString()) }
        error.videoTrackDurationMs?.let { put("video_track_duration_ms", it.toString()) }
        if (error.containerDurationMs != null && error.containerDurationMs > 0 && error.videoTrackDurationMs != null) {
            put("video_track_duration_ratio_bps", ((error.videoTrackDurationMs * 10_000) / error.containerDurationMs).toString())
        }
    } else {
        put("failure_kind", "processing_exception")
        put("reason", errorClass)
    }
}

private val rfidBurnOverlayFieldKeys = setOf(
    "vaccination_goat_proof",
    "weighing_individual_video",
)

private fun localFileBytes(localUri: String): Long? =
    try {
        val file = if (localUri.startsWith("file:", ignoreCase = true)) File(URI(localUri)) else File(localUri)
        file.takeIf { it.exists() }?.length()
    } catch (_: Exception) {
        null
    }

private fun proofTypeForMime(mimeType: String): String =
    if (mimeType.startsWith("image/", ignoreCase = true)) "photo" else "video"

private fun byteBucket(bytes: Long): String = when {
    bytes < 1_000_000 -> "lt_1mb"
    bytes < 5_000_000 -> "1_5mb"
    bytes < 20_000_000 -> "5_20mb"
    bytes < 100_000_000 -> "20_100mb"
    else -> "gte_100mb"
}

private fun durationBucket(durationMs: Long): String = when {
    durationMs < 15_000 -> "lt_15s"
    durationMs < 60_000 -> "15_60s"
    durationMs < 180_000 -> "1_3m"
    else -> "gte_3m"
}

private fun ProofCaptureEntity.toRow() = ProofCaptureRow(
    id = id,
    fieldKey = fieldKey,
    proofSubject = ProofSubject.from(proofSubject),
    subjectId = subjectId,
    localUri = localUri,
    processedUri = processedUri,
    mimeType = mimeType,
    caption = caption,
    rfidTag = rfidTag,
    capturedAtMs = capturedAtMs,
    capturedStartMs = capturedStartMs,
    capturedEndMs = capturedEndMs,
    capturedByPrincipalId = capturedByPrincipalId,
    syncStatus = CaptureSyncStatus.valueOf(syncStatus),
    serverProofId = serverProofId,
    outboxItemId = outboxItemId,
    lastError = lastError,
    partitionKey = partitionKey,
    featureSurface = featureSurface,
    proofMode = proofMode,
    slotIndex = slotIndex,
    slotRequired = slotRequired,
    processingState = processingState,
    processingAttempted = processingAttempted,
    stateAttempt = stateAttempt,
    uploadOriginal = uploadOriginal,
    originalBytes = originalBytes,
    processedBytes = processedBytes,
    lastErrorStage = lastErrorStage,
    lastErrorClass = lastErrorClass,
)

// --- Testable Slot Builders (Blocker 9: Proof Flow Canonicalization) -----

/** Canonical slot builder for milk-preparation step captures.
 *  Testable function so both ViewModels and tests can assert byte-for-byte compatibility. */
fun buildMilkPreparationEvidenceSlot(
    parkId: String,
    preparationDate: String,
    stepCode: String,
): EvidenceSlot = EvidenceSlot(
    identity = ProofIdentity(
        flow = ProofFlow.MILK_PREPARATION,
        taskId = "milk-preparation:$parkId:$preparationDate",
        partitionKey = "whole",
        subjectKey = parkId,
    ),
    fieldKey = "milk_preparation_$stepCode",
)

/** Canonical slot builder for milk-feeding proof captures.
 *  Testable function so both ViewModels and tests can assert byte-for-byte compatibility. */
fun buildMilkFeedingEvidenceSlot(
    parkId: String,
    feedingDate: String,
    sessionNo: Int,
    taskId: String,
    code: String,
): EvidenceSlot = EvidenceSlot(
    identity = ProofIdentity(
        flow = ProofFlow.MILK_FEEDING,
        taskId = "milk-feeding:$parkId:$feedingDate:$sessionNo",
        partitionKey = "whole",
        subjectKey = taskId,
    ),
    fieldKey = "milk_feeding_$code",
)

/** Canonical slot builder for workflow action-video captures.
 *  Testable function so both ViewModels and tests can assert byte-for-byte compatibility. */
fun buildWorkflowEvidenceSlot(
    workflowId: String,
    goatId: String,
    actionId: String,
): EvidenceSlot = EvidenceSlot(
    identity = ProofIdentity(
        flow = ProofFlow.WORKFLOW_DETAIL,
        taskId = workflowId,
        partitionKey = "whole",
        subjectKey = goatId,
    ),
    fieldKey = "workflow_${actionId}_video",
)
