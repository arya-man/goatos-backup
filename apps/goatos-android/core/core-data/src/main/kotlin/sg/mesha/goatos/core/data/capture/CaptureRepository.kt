package sg.mesha.goatos.core.data.capture

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
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.RfidScanAttemptDao
import sg.mesha.goatos.core.database.capture.RfidScanAttemptEntity
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
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
 * BEFORE it is reflected in the UI, deduped by (task, field, tag) at the DB layer — the UI
 * observes [observeScannedTags]/[observeScannedCount], it never owns the list as transient
 * ViewModel state.
 */
interface ScanCaptureRepository {
    fun observeScannedTags(taskId: String, fieldKey: String): Flow<List<ScannedGoatRow>>
    fun observeScannedCount(taskId: String, fieldKey: String): Flow<Int>

    /** Every scanned tag across every `goat_scan` field of [taskId] — the single Flow a
     *  ViewModel observes (one field or several); group by [ScannedGoatRow.fieldKey] for a
     *  per-field count/list. */
    fun observeAllForTask(taskId: String): Flow<List<ScannedGoatRow>>

    /** Persists one completed tag read to Room first; a repeat tag for the same field is a
     *  silent no-op (dedup). */
    suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String? = null,
        obligationId: String? = null,
        capturedAtMs: Long? = null,
    )

    /** All scanned tags across every `goat_scan` field of [taskId] — used to build the
     *  shed-submit answer payload. */
    suspend fun tagsForTask(taskId: String): List<String>

    suspend fun clearForTask(taskId: String)
}

class DefaultScanCaptureRepository(
    private val dao: ScannedGoatDao,
    private val syncRepository: SyncRepository? = null,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : ScanCaptureRepository {

    override fun observeScannedTags(taskId: String, fieldKey: String): Flow<List<ScannedGoatRow>> =
        dao.observeForField(taskId, fieldKey).map { rows -> rows.map { it.toRow() } }.flowOn(dispatchers.default)

    override fun observeScannedCount(taskId: String, fieldKey: String): Flow<Int> =
        dao.observeCountForField(taskId, fieldKey).flowOn(dispatchers.default)

    override fun observeAllForTask(taskId: String): Flow<List<ScannedGoatRow>> =
        dao.observeForTask(taskId).map { rows -> rows.map { it.toRow() } }.flowOn(dispatchers.default)

    override suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
        capturedAtMs: Long?,
    ) {
        val trimmed = tag.trim()
        if (trimmed.isEmpty()) return
        val durableCapturedAtMs = capturedAtMs?.takeIf { it > 0L } ?: clock()
        val inserted = withContext(dispatchers.io) {
            dao.insert(
                ScannedGoatEntity(
                    id = idGenerator(),
                    taskId = taskId,
                    fieldKey = fieldKey,
                    tag = trimmed,
                    goatId = goatId?.takeIf { it.isNotBlank() },
                    obligationId = obligationId?.takeIf { it.isNotBlank() },
                    capturedAtMs = durableCapturedAtMs,
                    syncStatus = EntitySyncStatus.PENDING.name,
                ),
            )
        }
        if (inserted <= 0L) return
        val syncKey = scanCaptureIdempotencyKey(taskId, fieldKey, trimmed)
        syncRepository?.enqueueScanCapture(
            taskId = taskId,
            groupKey = taskId,
            idempotencyKey = syncKey,
            request = ScanCaptureRequestDto(
                fieldKey = fieldKey,
                tag = trimmed,
                goatId = goatId?.takeIf { it.isNotBlank() },
                obligationId = obligationId?.takeIf { it.isNotBlank() },
                capturedAtMs = durableCapturedAtMs,
            ),
        )
    }

    override suspend fun tagsForTask(taskId: String): List<String> = withContext(dispatchers.io) {
        dao.listForTask(taskId).map { it.tag }
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
)

private fun scanCaptureIdempotencyKey(taskId: String, fieldKey: String, tag: String): String =
    "scan:$taskId:$fieldKey:${tag.filter { it.isLetterOrDigit() }.lowercase()}"

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
    )

    suspend fun attemptsForTask(taskId: String): List<RfidScanAttemptRow>

    suspend fun clearForTask(taskId: String)
}

class DefaultScanAttemptRepository(
    private val dao: RfidScanAttemptDao,
    private val syncRepository: SyncRepository? = null,
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
    ) {
        val trimmed = tag.trim()
        val normalized = normalizeTag(trimmed)
        if (trimmed.isEmpty() || normalized.isEmpty()) return
        val id = idGenerator()
        val capturedAtMs = clock()
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
            capturedAtMs = capturedAtMs,
            syncStatus = EntitySyncStatus.PENDING.name,
            idempotencyKey = idempotencyKey,
        )
        val inserted = withContext(dispatchers.io) { dao.insert(entity) }
        if (inserted <= 0L) return
        syncRepository?.enqueueScanAttempt(
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
                capturedAtMs = capturedAtMs,
            ),
        )
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
 */
interface ProofCaptureRepository {
    fun observeProofs(taskId: String): Flow<List<ProofCaptureRow>>

    /** Persists a captured video to Room first, then queues its metadata registration.
     *  Returns [AppResult.Err] (no Room write) if the per-task cap
     *  ([sg.mesha.goatos.core.database.capture.ProofCaptureDao.MAX_PROOFS_PER_TASK]) is
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

    override fun observeProofs(taskId: String): Flow<List<ProofCaptureRow>> =
        dao.observeForTask(taskId)
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
    ): AppResult<ProofCaptureRow> = withContext(dispatchers.io) {
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
                dao.activeCountForSubject(taskId, effectiveSubjectId)
            subject != ProofSubject.GOAT && effectiveSubjectId != null ->
                dao.activeCountForSubject(taskId, effectiveSubjectId)
            // R50-027: a generic (shed/vial/administration) capture has no per-goat subjectId, so
            // count active proofs of that subject TYPE for the task — otherwise the cap saw 0 and
            // never applied, leaving generic proofs unbounded.
            subject != ProofSubject.GOAT && effectiveSubjectId == null ->
                dao.activeCountForSubjectType(taskId, subject.wireValue)
            else -> 0
        }
        if (existing >= maxPerSubject) {
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
        val entity = ProofCaptureEntity(
            id = id,
            taskId = taskId,
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
        )
        // Room FIRST — the capture is durable before any network call is even attempted.
        dao.insert(entity)
        enqueueRegistration(entity, scopeType, scopeId)
        AppResult.Ok(entity.toRow())
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
            enqueueRegistration(entity, scopeType = "task", scopeId = taskId)
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
                    enqueueRegistrationNow(entity, scopeType = "task", scopeId = entity.taskId)
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
    ) {
        appScope.launch(dispatchers.io, start = CoroutineStart.UNDISPATCHED) {
            enqueueRegistrationNow(entity, scopeType, scopeId)
        }
    }

    private suspend fun enqueueRegistrationNow(
        entity: ProofCaptureEntity,
        scopeType: String,
        scopeId: String,
    ) {
        val request = ProofUploadRequestDto(
            proofType = "video",
            mimeType = entity.mimeType,
            scopeType = scopeType,
            scopeId = scopeId,
            subjectType = entity.proofSubject,
            subjectId = entity.subjectId,
            metadata = buildMap {
                put("field_key", JsonPrimitive(entity.fieldKey))
                // R50-027 SSOT: capture_source is read from the durable row, so the startup-recovery
                // path (which has no in-memory ProofPolicy) re-sends the ORIGINAL source instead of a
                // Default fallback that would silently rewrite a non-camera source.
                put("capture_source", JsonPrimitive(entity.captureSource))
                entity.caption?.takeIf { it.isNotBlank() }?.let { put("caption", JsonPrimitive(it)) }
                // Camera-only capture freshness proof (docs/mobile/proof-capture-sync-and-e2e.md
                // "Camera-only capture"): the verifier can see this was a live, timed,
                // attributable in-app recording, not an imported file.
                put("captured_start_ms", JsonPrimitive(entity.capturedStartMs))
                put("captured_end_ms", JsonPrimitive(entity.capturedEndMs))
                put("duration_ms", JsonPrimitive((entity.capturedEndMs - entity.capturedStartMs).coerceAtLeast(0)))
                entity.capturedByPrincipalId?.takeIf { it.isNotBlank() }
                    ?.let { put("captured_by_principal_id", JsonPrimitive(it)) }
            },
        )
        when (
            val result = syncRepository.enqueueProofUpload(
                groupKey = scopeId.ifBlank { entity.taskId },
                idempotencyKey = entity.idempotencyKey,
                request = request,
                localFilePath = entity.localUri,
                durationMs = (entity.capturedEndMs - entity.capturedStartMs).coerceAtLeast(0),
            )
        ) {
            is AppResult.Ok -> {
                dao.setOutboxItemId(entity.id, result.value)
                followOutboxItem(entity.id, result.value)
            }
            is AppResult.Err -> dao.updateStatus(entity.id, EntitySyncStatus.FAILED.name, null, result.message)
        }
    }

    /** R50-029: follows one outbox item to ITS terminal state via observeItem (by-id, window-independent),
     *  then stops — [transformWhile] ends the collection right after the terminal emission, so this
     *  coroutine (and its subscription to the per-item [SyncRepository.observeItem] flow) does not
     *  outlive the proof it was tracking. Before the fix (using observeStatus().map{}), high-volume
     *  terminal updates could drop the item from the bounded global window, leaving a proof stuck
     *  before reaching its terminal state. */
    private fun followOutboxItem(rowId: String, outboxItemId: String) {
        appScope.launch(dispatchers.io, start = CoroutineStart.UNDISPATCHED) {
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
        }
    }

    /** Reconciles proof rows from durable outbox state when lifecycle churn missed the live
     *  followOutboxItem() terminal emission. The UI remains Room-first: this only repairs
     *  proof_capture from the persisted outbox result before readiness is calculated. */
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
                                    dao.updateStatus(row.id, EntitySyncStatus.FAILED.name, null, corruptProofUploadResultMessage)
                                } else {
                                    dao.updateStatus(row.id, EntitySyncStatus.SYNCED.name, proofId, null)
                                }
                            }
                            item.status == SyncItemStatus.IN_FLIGHT && row.syncStatus != EntitySyncStatus.IN_FLIGHT.name ->
                                dao.updateStatus(row.id, EntitySyncStatus.IN_FLIGHT.name, null, null)
                            item.isDeadLetter || item.conflict ->
                                dao.updateStatus(row.id, EntitySyncStatus.FAILED.name, null, item.lastError)
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
        .getOrNull()
        ?.takeIf { it.isNotBlank() }
}

private const val corruptProofUploadResultMessage = "Proof upload finished without a server proof id. Record this video again."

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
    lastError = lastError,
)
