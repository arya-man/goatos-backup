package sg.mesha.goatos.core.data.capture

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DefaultDispatchers
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.capture.ProofCaptureDao
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.syncJson
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
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
    suspend fun recordScan(taskId: String, fieldKey: String, tag: String)

    /** All scanned tags across every `goat_scan` field of [taskId] — used to build the
     *  shed-submit answer payload. */
    suspend fun tagsForTask(taskId: String): List<String>

    suspend fun clearForTask(taskId: String)
}

class DefaultScanCaptureRepository(
    private val dao: ScannedGoatDao,
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

    override suspend fun recordScan(taskId: String, fieldKey: String, tag: String) {
        val trimmed = tag.trim()
        if (trimmed.isEmpty()) return
        withContext(dispatchers.io) {
            dao.insert(
                ScannedGoatEntity(
                    id = idGenerator(),
                    taskId = taskId,
                    fieldKey = fieldKey,
                    tag = trimmed,
                    capturedAtMs = clock(),
                    syncStatus = EntitySyncStatus.PENDING.name,
                ),
            )
        }
    }

    override suspend fun tagsForTask(taskId: String): List<String> = withContext(dispatchers.io) {
        dao.listForTask(taskId).map { it.tag }
    }

    override suspend fun clearForTask(taskId: String) = withContext(dispatchers.io) {
        dao.clearForTask(taskId)
    }
}

private fun ScannedGoatEntity.toRow() = ScannedGoatRow(fieldKey = fieldKey, tag = tag, capturedAtMs = capturedAtMs)

/**
 * Room-first SSOT for a task's `video_proof` recording-form fields
 * (docs/mobile/proof-capture-sync-and-e2e.md §2/§3). Every captured video is persisted to
 * Room BEFORE any network call, then a metadata registration write
 * ([SyncRepository.enqueueProofUpload]) is queued through the SAME durable outbox the
 * shed-submit write uses — background, survives process death, exactly-once via the row's
 * own idempotency key. [observeProofs] status transitions PENDING -> IN_FLIGHT -> SYNCED/FAILED
 * mirror the outbox item this capture drives (Photos/Drive "uploading -> synced" model).
 *
 * NOTE (documented boundary, not new to this build — see `core-network`'s
 * `ProofUploadRequestDto` kdoc): [SyncRepository.enqueueProofUpload] registers proof METADATA
 * and gets back a signed upload URL; the actual binary PUT of the video bytes to that URL is a
 * separate pass this repository does not perform. A row therefore reaches [CaptureSyncStatus.SYNCED]
 * once metadata registration succeeds, not once the video bytes are actually durable server-side.
 */
interface ProofCaptureRepository {
    fun observeProofs(taskId: String): Flow<List<ProofCaptureRow>>

    /** Persists a captured video to Room first, then queues its metadata registration.
     *  Returns [AppResult.Err] (no Room write) if the per-task cap
     *  ([sg.mesha.goatos.core.database.capture.ProofCaptureDao.MAX_PROOFS_PER_TASK]) is
     *  already reached. */
    suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
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
    ): AppResult<ProofCaptureRow>

    suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit>

    suspend fun remove(taskId: String, id: String): AppResult<Unit>

    suspend fun clearForTask(taskId: String)
}

class DefaultProofCaptureRepository(
    private val dao: ProofCaptureDao,
    private val syncRepository: SyncRepository,
    private val appScope: CoroutineScope,
    private val dispatchers: DispatcherProvider = DefaultDispatchers,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : ProofCaptureRepository {

    override fun observeProofs(taskId: String): Flow<List<ProofCaptureRow>> =
        dao.observeForTask(taskId).map { rows -> rows.map { it.toRow() } }.flowOn(dispatchers.default)

    override suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        localUri: String,
        mimeType: String,
        caption: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
    ): AppResult<ProofCaptureRow> = withContext(dispatchers.io) {
        val existing = dao.countForTask(taskId)
        if (existing >= ProofCaptureDao.MAX_PROOFS_PER_TASK) {
            return@withContext AppResult.Err(
                "Maximum ${ProofCaptureDao.MAX_PROOFS_PER_TASK} proof videos reached for this drive.",
            )
        }
        val id = idGenerator()
        val idempotencyKey = "proof-upload:$taskId:$id"
        val entity = ProofCaptureEntity(
            id = id,
            taskId = taskId,
            fieldKey = fieldKey,
            proofSubject = subject.wireValue,
            localUri = localUri,
            mimeType = mimeType,
            caption = caption,
            capturedAtMs = clock(),
            capturedStartMs = capturedStartMs,
            capturedEndMs = capturedEndMs,
            capturedByPrincipalId = capturedByPrincipalId,
            syncStatus = EntitySyncStatus.PENDING.name,
            idempotencyKey = idempotencyKey,
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
        dao.delete(id, taskId)
        AppResult.Ok(Unit)
    }

    override suspend fun clearForTask(taskId: String) = withContext(dispatchers.io) {
        dao.clearForTask(taskId)
    }

    /** Queues the metadata-registration write on the app-lifetime scope (never blocks the
     *  caller's [capture] — the Room write above already made the capture durable) and follows
     *  its outbox item to reflect PENDING -> IN_FLIGHT -> SYNCED/FAILED back onto the row. */
    private fun enqueueRegistration(entity: ProofCaptureEntity, scopeType: String, scopeId: String) {
        appScope.launch(dispatchers.io) {
            val request = ProofUploadRequestDto(
                proofType = "video",
                mimeType = entity.mimeType,
                scopeType = scopeType,
                scopeId = scopeId,
                subjectType = entity.proofSubject,
                subjectId = null,
                metadata = buildMap {
                    put("field_key", JsonPrimitive(entity.fieldKey))
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
                )
            ) {
                is AppResult.Ok -> {
                    dao.setOutboxItemId(entity.id, result.value)
                    followOutboxItem(entity.id, result.value)
                }
                is AppResult.Err -> dao.updateStatus(entity.id, EntitySyncStatus.FAILED.name, null, result.message)
            }
        }
    }

    private fun followOutboxItem(rowId: String, outboxItemId: String) {
        appScope.launch(dispatchers.io) {
            syncRepository.observeStatus()
                .map { status -> status.items.firstOrNull { it.id == outboxItemId } }
                .filterNotNull()
                .distinctUntilChanged()
                .collect { item ->
                    when {
                        item.status == SyncItemStatus.IN_FLIGHT ->
                            dao.updateStatus(rowId, EntitySyncStatus.IN_FLIGHT.name, null, null)
                        item.status == SyncItemStatus.SUCCEEDED ->
                            dao.updateStatus(rowId, EntitySyncStatus.SYNCED.name, decodeServerProofId(item.resultJson), null)
                        item.isDeadLetter || item.conflict ->
                            dao.updateStatus(rowId, EntitySyncStatus.FAILED.name, null, item.lastError)
                        else -> Unit // QUEUED / still-retrying FAILED — leave PENDING, another emission follows.
                    }
                }
        }
    }
}

private fun decodeServerProofId(resultJson: String?): String? {
    if (resultJson.isNullOrBlank()) return null
    return runCatching { syncJson.decodeFromString<ProofUploadResponseDto>(resultJson).proof.proofId }
        .getOrNull()
        ?.takeIf { it.isNotBlank() }
}

private fun ProofCaptureEntity.toRow() = ProofCaptureRow(
    id = id,
    fieldKey = fieldKey,
    proofSubject = ProofSubject.from(proofSubject),
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
