package sg.mesha.goatos.core.data.capture

import android.database.SQLException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.transformWhile
import kotlinx.coroutines.launch
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
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.syncJson
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import java.io.File
import java.net.URI
import java.util.UUID
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
            dao.insert(
                ScannedGoatEntity(
                    id = idGenerator(),
                    taskId = taskId,
                    partitionKey = executionPartitionKey(partitionLabel),
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
        val syncKey = scanCaptureIdempotencyKey(taskId, partitionKey, fieldKey, tag, obligationRowVersion)
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
        dao.listForTask(taskId, executionPartitionKey(partitionLabel)).map { it.tag }
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
 * threaded a row_version yet (pre-existing callers, [enqueuePendingScans] recovery), matching
 * every fresh row's baseline cycle so first-time enqueues are unaffected.
 */
private fun scanCaptureIdempotencyKey(
    taskId: String,
    partitionKey: String,
    fieldKey: String,
    tag: String,
    obligationRowVersion: Int = 0,
): String {
    val partitionSegment = if (partitionKey == "whole") "" else ":partition:$partitionKey"
    return "scan:$taskId$partitionSegment:$fieldKey:${tag.filter { it.isLetterOrDigit() }.lowercase()}:ov$obligationRowVersion"
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
     *  omit it and fall back to [ProofPolicy.Default] (the historical hardcoded values). */
    suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String? = null,
        localUri: String,
        mimeType: String,
        caption: String?,
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
    ): AppResult<ProofCaptureRow>

    suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit>

    suspend fun remove(taskId: String, id: String): AppResult<Unit>

    /** Re-arms a terminal FAILED proof upload row using the same proof idempotency key and
     *  payload. No-op for rows that are already queued/in-flight/synced. */
    suspend fun retryUpload(taskId: String, id: String): AppResult<Unit>

    suspend fun clearForTask(taskId: String)
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
    // Production always reconciles orphan uploads on construction. Tests set this false to drive
    // reconcileRecoverableUploadsNow() explicitly (awaited) instead of racing the fire-and-forget
    // init launch — Room's suspend @Query runs on Room's own executor, so a virtual-clock
    // advanceUntilIdle() cannot deterministically await the init launch.
    reconcileOnStartup: Boolean = true,
) : ProofCaptureRepository {

    // R50-029: reconciliation is keyset-based (monotonic row-value cursor), never wall-clock.
    // A device clock rollback cannot skip recovery of orphan rows. On each startup,
    // reconcileRecoverableUploadsNow() walks from a stored last-processed rowId/capturedAtMs,
    // applying idempotency at the outbox layer so re-enqueue is safe on replay.
    // No cutoff timestamp is stored or checked — every recoverable row is reached regardless
    // of absolute clock values.
    private var lastRecoveredAfterCapturedAtMs = 0L
    private var lastRecoveredAfterId = ""

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
                reconcileOutboxTerminalState(rows)
                rows.map { it.toRow() }
            }
            .flowOn(dispatchers.io)

    override suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
        proofPolicy: ProofPolicy,
        partitionLabel: String?,
        awaitUploadEnqueue: Boolean,
        uploadGroupKey: String?,
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
                ProofSubject.PARK -> "park"
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
            capturedAtMs = clock(),
            capturedStartMs = capturedStartMs,
            capturedEndMs = capturedEndMs,
            capturedByPrincipalId = capturedByPrincipalId,
            syncStatus = EntitySyncStatus.PENDING.name,
            idempotencyKey = idempotencyKey,
            // R50-027 SSOT: persist capture_source with the durable row so the startup-recovery
            // re-registration path re-sends the ORIGINAL source, not a Default fallback.
            captureSource = proofPolicy.captureSource,
            originalUri = localUri,
            durationMs = (capturedEndMs - capturedStartMs).coerceAtLeast(0),
            updatedAtMs = clock(),
        )
        // Room FIRST — the capture is durable before any network call is even attempted.
        dao.insert(entity)
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
        deleteLocalFile(entity.localUri)
        AppResult.Ok(Unit)
    }

    override suspend fun retryUpload(taskId: String, id: String): AppResult<Unit> = withContext(dispatchers.io) {
        val entity = dao.findById(id) ?: return@withContext AppResult.Err("Proof video not found.")
        if (entity.taskId != taskId) return@withContext AppResult.Err("Proof video does not belong to this task.")
        if (entity.syncStatus != EntitySyncStatus.FAILED.name) return@withContext AppResult.Ok(Unit)
        val outboxItemId = entity.outboxItemId
        if (outboxItemId.isNullOrBlank()) {
            dao.updateStatus(entity.id, EntitySyncStatus.PENDING.name, null, null)
            val (scopeType, scopeId) = recoveryScope(entity)
            enqueueRegistration(entity, scopeType, scopeId)
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
                deleteLocalFile(entity.localUri)
            }
            val last = page.last()
            afterCapturedAtMs = last.capturedAtMs
            afterId = last.id
            if (page.size < ProofCaptureDao.TASK_CLEANUP_PAGE_SIZE) break
        }
        dao.clearForTask(taskId)
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
        var afterCapturedAtMs = lastRecoveredAfterCapturedAtMs
        var afterId = lastRecoveredAfterId
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
                    enqueueRegistrationNow(entity, scopeType, scopeId)
                } else {
                    followOutboxItem(entity.id, outboxItemId)
                    syncRepository.triggerDrain()
                }
            }
            val last = page.last()
            lastRecoveredAfterCapturedAtMs = last.capturedAtMs
            lastRecoveredAfterId = last.id
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
        val request = ProofUploadRequestDto(
            proofType = proofTypeForMime(uploadEntity.mimeType),
            mimeType = uploadEntity.mimeType,
            scopeType = scopeType,
            scopeId = scopeId,
            subjectType = uploadEntity.proofSubject,
            subjectId = uploadEntity.subjectId,
            metadata = buildMap {
                put("field_key", JsonPrimitive(uploadEntity.fieldKey))
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
                proofOverlayRfidTag(uploadEntity)?.let { put("rfid_tag", JsonPrimitive(it)) }
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
        if (entity.processingAttempted) return entity
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
                    rfidTag = proofOverlayRfidTag(entity),
                    originalUri = entity.originalUri ?: entity.localUri,
                    mimeType = entity.mimeType,
                    capturedStartMs = entity.capturedStartMs,
                    capturedEndMs = entity.capturedEndMs,
                    capturedByPrincipalId = entity.capturedByPrincipalId,
                ),
            )
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
        } catch (error: Throwable) {
            val errorClass = error::class.java.simpleName.ifBlank { "Throwable" }
            dao.updateProcessingArtifact(
                id = entity.id,
                localUri = entity.originalUri ?: entity.localUri,
                mimeType = entity.mimeType,
                processingState = ProofProcessingState.PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED.name,
                processingAttempted = true,
                uploadOriginal = true,
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
                processingState = ProofProcessingState.PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED.name,
                attempt = attempt,
                processingAttempted = true,
                uploadOriginal = true,
                lastErrorStage = "processing",
                lastErrorClass = errorClass,
                lastErrorRetryable = false,
                lastErrorMessageHash = error.message?.hashCode()?.toString(),
                updatedAtMs = clock(),
            )
            recordProofEvent(
                entity,
                "processing_failed_original_upload_queued",
                ProofProcessingState.PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED.name,
                attempt,
                durationMs = clock() - startedAtMs,
                bytesIn = localFileBytes(entity.originalUri ?: entity.localUri),
                errorClass = errorClass,
                retryable = false,
            )
            telemetry.track(proofProcessingFailedEvent, proofAnalyticsProps(entity, attempt = attempt, uploadOriginal = true) + ("error_class" to errorClass))
            dao.findById(entity.id) ?: entity.copy(
                localUri = entity.originalUri ?: entity.localUri,
                processingState = ProofProcessingState.PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED.name,
                processingAttempted = true,
                stateAttempt = attempt,
                uploadOriginal = true,
            )
        }
    }

    private suspend fun saveFinalArtifactToGallery(entity: ProofCaptureEntity, request: ProofUploadRequestDto) {
        val startedAtMs = clock()
        telemetry.track(proofGallerySaveStartedEvent, proofAnalyticsProps(entity))
        try {
            galleryProofSaver.saveProofCopy(entity.localUri, request, entity.idempotencyKey)
            recordProofEvent(
                entity,
                "gallery_save_completed",
                entity.processingState,
                entity.stateAttempt,
                durationMs = clock() - startedAtMs,
                bytesIn = entity.processedBytes ?: entity.originalBytes ?: localFileBytes(entity.localUri),
            )
            telemetry.track(proofGallerySaveCompletedEvent, proofAnalyticsProps(entity) + ("duration_ms" to (clock() - startedAtMs).toString()))
        } catch (error: Throwable) {
            val errorClass = error::class.java.simpleName.ifBlank { "Throwable" }
            recordProofEvent(
                entity,
                "gallery_save_failed_upload_continues",
                entity.processingState,
                entity.stateAttempt,
                durationMs = clock() - startedAtMs,
                errorClass = errorClass,
                retryable = false,
            )
            telemetry.track(proofGallerySaveFailedEvent, proofAnalyticsProps(entity) + ("error_class" to errorClass))
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
                                dao.updateStatus(rowId, EntitySyncStatus.IN_FLIGHT.name, null, null)
                            item.status == SyncItemStatus.SUCCEEDED -> {
                                val proofId = decodeServerProofId(item.resultJson)
                                if (proofId.isNullOrBlank()) {
                                    dao.updateStatus(rowId, EntitySyncStatus.FAILED.name, null, corruptProofUploadResultMessage)
                                } else {
                                    dao.updateStatus(rowId, EntitySyncStatus.SYNCED.name, proofId, null)
                                    // R50-028: the video is durably server-side now — reclaim the
                                    // device-local copy so a long shift's captures cannot fill storage.
                                    dao.findById(rowId)?.let { deleteLocalFile(it.localUri) }
                                }
                            }
                            item.isDeadLetter || item.conflict ->
                                dao.updateStatus(rowId, EntitySyncStatus.FAILED.name, null, item.lastError)
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

    /** F1a: Derives the scope (scope_type and scope_id) from the persisted proof entity,
     *  matching the live capture path exactly. Shed-level proofs use "shed" scope;
     *  all others fall back to "task" scope. */
    private fun recoveryScope(entity: ProofCaptureEntity): Pair<String, String> {
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
    private suspend fun reconcileOutboxTerminalState(rows: List<ProofCaptureEntity>) {
        rows.asSequence()
            .filter { it.syncStatus != EntitySyncStatus.SYNCED.name }
            .mapNotNull { row -> row.outboxItemId?.takeIf(String::isNotBlank)?.let { row to it } }
            .forEach { (row, outboxItemId) ->
                when (val recovered = syncRepository.findOutboxItem(outboxItemId)) {
                    is AppResult.Err -> Unit
                    is AppResult.Ok -> {
                        val item = recovered.value ?: return@forEach
                        when {
                            item.status == SyncItemStatus.SUCCEEDED -> {
                                val proofId = decodeServerProofId(item.resultJson)
                                if (proofId.isNullOrBlank()) {
                                    val newStatus = EntitySyncStatus.FAILED.name
                                    if (row.syncStatus != newStatus || row.serverProofId != null || row.lastError != corruptProofUploadResultMessage) {
                                        dao.updateStatus(row.id, newStatus, null, corruptProofUploadResultMessage)
                                    }
                                } else {
                                    val newStatus = EntitySyncStatus.SYNCED.name
                                    if (row.syncStatus != newStatus || row.serverProofId != proofId || row.lastError != null) {
                                        dao.updateStatus(row.id, newStatus, proofId, null)
                                    }
                                }
                            }
                            item.status == SyncItemStatus.IN_FLIGHT && row.syncStatus != EntitySyncStatus.IN_FLIGHT.name -> {
                                val newStatus = EntitySyncStatus.IN_FLIGHT.name
                                if (row.syncStatus != newStatus || row.serverProofId != null || row.lastError != null) {
                                    dao.updateStatus(row.id, newStatus, null, null)
                                }
                            }
                            item.isDeadLetter || item.conflict -> {
                                val newStatus = EntitySyncStatus.FAILED.name
                                if (row.syncStatus != newStatus || row.serverProofId != null || row.lastError != item.lastError) {
                                    dao.updateStatus(row.id, newStatus, null, item.lastError)
                                }
                            }
                        }
                    }
                }
            }
    }
}

/** R50-028: best-effort local-file cleanup for a synced/removed/cleared proof. Deliberately
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
private const val proofGallerySaveStartedEvent = "proof_gallery_save_started"
private const val proofGallerySaveCompletedEvent = "proof_gallery_save_completed"
private const val proofGallerySaveFailedEvent = "proof_gallery_save_failed"
private const val proofUploadRegisteredEvent = "proof_upload_registered"

private fun proofAnalyticsProps(
    entity: ProofCaptureEntity,
    attempt: Int = entity.stateAttempt,
    uploadOriginal: Boolean = entity.uploadOriginal,
): Map<String, String> = buildMap {
    put("proof_id", entity.id)
    put("task_id", entity.taskId)
    put("partition_key", entity.partitionKey)
    put("field_key", entity.fieldKey)
    put("proof_subject", entity.proofSubject)
    entity.subjectId?.takeIf { it.isNotBlank() }?.let { put("subject_id", it) }
    proofOverlayRfidTag(entity)?.let { put("rfid_tag", it) }
    entity.featureSurface?.takeIf { it.isNotBlank() }?.let { put("feature_surface", it) }
    entity.proofMode?.takeIf { it.isNotBlank() }?.let { put("proof_mode", it) }
    entity.slotIndex?.let { put("slot_index", it.toString()) }
    put("slot_required", entity.slotRequired.toString())
    put("capture_source", entity.captureSource)
    entity.capturedByPrincipalId?.takeIf { it.isNotBlank() }?.let { put("operator_principal_id", it) }
    put("mime_type", entity.mimeType)
    put("processing_state", entity.processingState)
    put("processing_attempt", attempt.toString())
    put("upload_original", uploadOriginal.toString())
    put("duration_bucket", durationBucket(entity.durationMs ?: (entity.capturedEndMs - entity.capturedStartMs).coerceAtLeast(0)))
    entity.originalBytes?.let { put("original_size_bucket", byteBucket(it)) }
    entity.processedBytes?.let { put("processed_size_bucket", byteBucket(it)) }
    entity.inputWidth?.let { put("input_width", it.toString()) }
    entity.inputHeight?.let { put("input_height", it.toString()) }
    entity.targetVideoBitrate?.let { put("target_video_bitrate", it.toString()) }
    entity.targetAudioBitrate?.let { put("target_audio_bitrate", it.toString()) }
    entity.locationStatus?.takeIf { it.isNotBlank() }?.let { put("location_status", it) }
    entity.geocoderStatus?.takeIf { it.isNotBlank() }?.let { put("geocoder_status", it) }
}

private fun proofOverlayRfidTag(entity: ProofCaptureEntity): String? =
    entity.caption
        ?.takeIf { entity.fieldKey in rfidBurnOverlayFieldKeys }
        ?.takeIf { it.isNotBlank() }

private val rfidBurnOverlayFieldKeys = setOf(
    "vaccination_goat_proof",
    "weighing_individual_video",
)

private fun localFileBytes(localUri: String): Long? = runCatching {
    val file = if (localUri.startsWith("file:", ignoreCase = true)) File(URI(localUri)) else File(localUri)
    file.takeIf { it.exists() }?.length()
}.getOrNull()

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
    mimeType = mimeType,
    caption = caption,
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
