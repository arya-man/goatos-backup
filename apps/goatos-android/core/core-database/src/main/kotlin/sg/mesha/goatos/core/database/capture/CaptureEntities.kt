package sg.mesha.goatos.core.database.capture

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.ForeignKey
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Transaction
import kotlinx.coroutines.flow.Flow

/**
 * Room-first sync lifecycle for a captured row (MOB-002,
 * docs/mobile/proof-capture-sync-and-e2e.md §3) — identical mental model to the Android
 * Photos / Google Drive "uploading / synced" indicators. Every scan and every captured video
 * is persisted with one of these BEFORE any network call.
 */
enum class CaptureSyncStatus { PENDING, IN_FLIGHT, SYNCED, FAILED }

/**
 * One de-duplicated RFID scan captured for a task's `goat_scan` recording-form field
 * (docs/mobile/proof-capture-sync-and-e2e.md §1). Room is the SSOT — the UI never owns the
 * scanned list as transient state. [syncStatus] tracks this row's own SCAN_CAPTURE outbox
 * durability. SYNCED means the backend has accepted this RFID evidence row; shed submit is a
 * separate gate.
 */
@Entity(
    tableName = "scanned_goat_capture",
    indices = [
        // Dedup is per operational partition and hidden due-item. The screen still shows one
        // animal, but if that animal has two vaccines due, both due-items must sync.
        Index(value = ["taskId", "partitionKey", "fieldKey", "tag", "obligationId"], unique = true),
        Index(value = ["taskId", "partitionKey", "fieldKey", "capturedAtMs"]),
    ],
)
data class ScannedGoatEntity(
    @PrimaryKey val id: String,
    val taskId: String,
    /** Normalized partition label; `whole` means the shed has no partition. */
    val partitionKey: String = "whole",
    /** The `goat_scan` [sg.mesha.goatos.core.data.forms.FormField.key] this scan belongs to —
     *  a task's form can in principle declare more than one scan field. */
    val fieldKey: String,
    val tag: String,
    val goatId: String?,
    val obligationId: String?,
    val capturedAtMs: Long,
    val syncStatus: String = CaptureSyncStatus.PENDING.name,
)

/**
 * Append-only audit of every physical RFID reader hit on a scan task. Unlike
 * [ScannedGoatEntity], this table is not a completion/counter source and is not deduped by tag:
 * a duplicate primary-tag read, a secondary-tag alias read, an unknown tag, and a not-due tag
 * are all operational evidence that the reader fired. Submit ignores this table; only accepted
 * [ScannedGoatEntity] rows drive the final `GOAT_SCAN` answer.
 */
@Entity(
    tableName = "rfid_scan_attempt",
    indices = [
        Index(value = ["idempotencyKey"], unique = true),
        Index(value = ["taskId", "capturedAtMs"]),
        Index(value = ["taskId", "goatId", "capturedAtMs"]),
    ],
)
data class RfidScanAttemptEntity(
    @PrimaryKey val id: String,
    val taskId: String,
    val fieldKey: String,
    val tag: String,
    val normalizedTag: String,
    val goatId: String?,
    val obligationId: String?,
    /** accepted / duplicate / not_due / unknown */
    val outcome: String,
    /** primary / secondary / unknown */
    val tagRole: String,
    val reason: String?,
    val capturedAtMs: Long,
    val syncStatus: String = CaptureSyncStatus.PENDING.name,
    val idempotencyKey: String,
)

/** Result of [ScannedGoatDao.upsertScan] — tells the repository whether a fresh outbox
 *  enqueue is warranted. A [DUPLICATE] result must NOT enqueue (same tag, same obligation
 *  cycle, already captured/capturing). [INSERTED] and [REPLACED] both represent genuinely new
 *  evidence that has not reached the server for THIS obligation cycle and must enqueue. */
enum class ScanUpsertResult { INSERTED, REPLACED, DUPLICATE }

@Dao
interface ScannedGoatDao {
    /** Insert-or-ignore: the unique (taskId, partitionKey, fieldKey, tag) index makes a repeat scan of the
     *  same tag a silent no-op — dedup happens at the DB layer, not just in memory. Superseded
     *  by [upsertScan] for the write path; kept for direct/test use where the caller has
     *  already established there is no obligation-reopen case to consider. */
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(entity: ScannedGoatEntity): Long

    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey " +
            "AND tag = :tag LIMIT 1",
    )
    suspend fun findByTaskFieldTag(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        tag: String,
    ): ScannedGoatEntity?

    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey AND tag = :tag " +
            "AND ((obligationId IS NULL AND :obligationId IS NULL) OR obligationId = :obligationId) LIMIT 1",
    )
    suspend fun findByTaskFieldTagObligation(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        tag: String,
        obligationId: String?,
    ): ScannedGoatEntity?

    @Query(
        "UPDATE scanned_goat_capture SET goatId = :goatId, obligationId = :obligationId, " +
            "capturedAtMs = :capturedAtMs, syncStatus = :syncStatus WHERE id = :id",
    )
    suspend fun replaceScan(id: String, goatId: String?, obligationId: String?, capturedAtMs: Long, syncStatus: String)

    /**
     * Writes [entity] as durable local evidence, distinguishing a TRUE repeat (same tag, same
     * obligation cycle — must stay deduped) from a tag re-scanned for a DIFFERENT/reopened
     * obligation (a verifier-rejected obligation reopened, or the same physical tag reassigned
     * to a new obligation) — which is fresh evidence and must be written through, never
     * silently absorbed by the unique (taskId, partitionKey, fieldKey, tag, obligationId) index.
     *
     * ROOT CAUSE this closes: the plain [insert] (OnConflictStrategy.IGNORE) treated ANY tag
     * collision as a duplicate — including a STALE, already-SYNCED row left over from a PRIOR
     * obligation cycle for the same tag (e.g. yesterday's synced capture, whose obligation the
     * server later reopened — a rejected/re-verified obligation typically keeps its ORIGINAL id,
     * it just flips status, so obligationId equality cannot distinguish this case). The caller
     * (ScanViewModel) only reaches [recordScan] on a roster row it has already judged PENDING/
     * due-again — a true still-DONE re-scan is routed to the "already scanned" duplicate feed
     * path before ever calling this. So the durable signal here is the STORED row's own
     * [ScannedGoatEntity.syncStatus]: while it is still PENDING/IN_FLIGHT/FAILED, the original
     * write has not even reached the server yet, so a repeat read is a true duplicate of
     * in-flight work and must stay deduped. Once it is SYNCED, the server has already
     * acknowledged this exact tag for this exact obligation cycle — a caller that scans it AGAIN
     * only does so because the domain invalidated that earlier evidence (obligation reopened).
     * Silently no-op'ing on the SYNCED stale row produced an ACCEPTED scan attempt with no
     * durable capture and no outbox enqueue: an accepted operator scan that never reached
     * `scan_captures` on the backend.
     *
     * Deliberately NOT `@Transaction`: this app has exactly one writer for a given
     * (taskId, partitionKey, fieldKey, tag, obligationId) at a time (one physical RFID reader stream, sequential
     * onTagRead handling), and the unique index on that composite key is the actual concurrency
     * safety net — a racing insert can still only ever leave one row. Wrapping this
     * read-then-write in a DAO-interface `@Transaction` default method was found (via a
     * captured `android.database.SQLException: connection is closed`, suppressed under an
     * unrelated later test) to leak an async connection-pool operation past the calling
     * coroutine's own completion under a bare `Dispatchers.Unconfined` test dispatcher —
     * i.e. the transaction's internal bookkeeping did not fully finish before the
     * suspend call returned to its caller. Since no cross-row atomicity is actually
     * required here, the safest fix is to not ask Room's transaction coroutine machinery
     * to do more than this call needs.
     */
    suspend fun upsertScan(entity: ScannedGoatEntity): ScanUpsertResult {
        val existing = if (entity.obligationId == null) {
            // SQLite unique indexes do not consider NULL equal to NULL, so the DAO must collapse
            // local/null-obligation scans before insert. Once a scan is tied to explicit vaccine
            // obligations the obligation-grain query below preserves one hidden sync row per due item.
            findByTaskFieldTag(entity.taskId, entity.partitionKey, entity.fieldKey, entity.tag)
        } else {
            findByTaskFieldTagObligation(
                entity.taskId,
                entity.partitionKey,
                entity.fieldKey,
                entity.tag,
                entity.obligationId,
            )
        }
        if (existing == null) {
            insert(entity)
            return ScanUpsertResult.INSERTED
        }
        if (existing.syncStatus != CaptureSyncStatus.SYNCED.name) {
            // Original write for this tag is still in flight (or failed and awaiting retry) —
            // a repeat read right now carries no new signal; stay deduped.
            return ScanUpsertResult.DUPLICATE
        }
        // Existing row is SYNCED: the earlier evidence already reached the server. A fresh scan
        // of the same tag only happens because the caller's own roster state judged this
        // obligation due again (reopen) — replace the stale row in place and reset it to PENDING
        // so the repository re-enqueues the outbox write for the new cycle.
        replaceScan(
            id = existing.id,
            goatId = entity.goatId,
            obligationId = entity.obligationId,
            capturedAtMs = entity.capturedAtMs,
            syncStatus = entity.syncStatus,
        )
        return ScanUpsertResult.REPLACED
    }

    // Bounded (mobile-guard: unbounded-db-read) — a shed's scanned-goat count is naturally
    // capacity-bounded, but the query still carries an explicit LIMIT rather than relying on
    // that business fact alone.
    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey " +
            "ORDER BY capturedAtMs ASC LIMIT :limit",
    )
    fun observeForField(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        limit: Int = MAX_SCANNED_PER_FIELD,
    ): Flow<List<ScannedGoatEntity>>

    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey " +
            "ORDER BY capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listForField(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        limit: Int = MAX_SCANNED_PER_FIELD,
    ): List<ScannedGoatEntity>

    @Query(
        "SELECT COUNT(*) FROM scanned_goat_capture WHERE taskId = :taskId " +
            "AND partitionKey = :partitionKey AND fieldKey = :fieldKey",
    )
    fun observeCountForField(taskId: String, partitionKey: String, fieldKey: String): Flow<Int>

    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "ORDER BY capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listForTask(
        taskId: String,
        partitionKey: String,
        limit: Int = MAX_SCANNED_PER_FIELD,
    ): List<ScannedGoatEntity>

    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "ORDER BY capturedAtMs ASC LIMIT :limit",
    )
    fun observeForTask(
        taskId: String,
        partitionKey: String,
        limit: Int = MAX_SCANNED_PER_FIELD,
    ): Flow<List<ScannedGoatEntity>>

    @Query("UPDATE scanned_goat_capture SET syncStatus = :status WHERE taskId = :taskId")
    suspend fun markTaskStatus(taskId: String, status: String)

    @Query(
        "UPDATE scanned_goat_capture SET syncStatus = :status " +
            "WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey AND tag = :tag " +
            "AND (:obligationId IS NULL OR obligationId = :obligationId)",
    )
    suspend fun markFieldTagStatus(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        tag: String,
        obligationId: String?,
        status: String,
    )

    @Query("DELETE FROM scanned_goat_capture WHERE taskId = :taskId")
    suspend fun clearForTask(taskId: String)

    @Query(
        "DELETE FROM scanned_goat_capture " +
            "WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey AND syncStatus = 'SYNCED'",
    )
    suspend fun deleteSyncedForField(taskId: String, partitionKey: String, fieldKey: String)

    @Query(
        "DELETE FROM scanned_goat_capture " +
            "WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey AND syncStatus = 'SYNCED' " +
            "AND (obligationId IS NULL OR obligationId NOT IN (:serverDoneObligationIds))",
    )
    suspend fun deleteSyncedForFieldExceptObligations(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        serverDoneObligationIds: List<String>,
    )

    @Transaction
    suspend fun pruneSyncedFieldToServerDone(
        taskId: String,
        partitionKey: String,
        fieldKey: String,
        serverDoneObligationIds: List<String>,
    ) {
        if (serverDoneObligationIds.isEmpty()) {
            deleteSyncedForField(taskId, partitionKey, fieldKey)
        } else {
            deleteSyncedForFieldExceptObligations(taskId, partitionKey, fieldKey, serverDoneObligationIds)
        }
    }

    @Query("DELETE FROM scanned_goat_capture")
    suspend fun clearAll()

    /**
     * Clears the local "already scanned" evidence for animals the server has sent back, so a
     * rejected animal stops rendering as done on the scan screen and can be redone.
     *
     * Scoped to the NAMED obligations only. An earlier form also deleted rows with
     * `obligationId IS NULL`, which is not a rejected animal at all — it is a capture that was
     * never linked to an obligation, and those belong to other sheds as often as this one. That
     * wiped unrelated sheds' scan evidence on every refresh.
     *
     * SYNCED only: a PENDING row is work that has not reached the server yet, and dropping it
     * would destroy the operator's unsynced capture.
     */
    @Query(
        "DELETE FROM scanned_goat_capture " +
            "WHERE partitionKey = :partitionKey AND fieldKey = :fieldKey AND syncStatus = 'SYNCED' " +
            "AND obligationId IN (:rejectedObligationIds)",
    )
    suspend fun deleteSyncedByRejectedObligations(
        partitionKey: String,
        fieldKey: String,
        rejectedObligationIds: List<String>,
    )

    @Transaction
    suspend fun pruneSyncedByRejectedObligations(
        partitionKey: String,
        fieldKey: String,
        rejectedObligationIds: List<String>,
    ) {
        if (rejectedObligationIds.isNotEmpty()) {
            deleteSyncedByRejectedObligations(partitionKey, fieldKey, rejectedObligationIds)
        }
    }

    companion object {
        /** Safety cap, not a real page size — a shed's roster is capacity-bounded, this just
         *  keeps a single Room read from ever materializing an unbounded table. */
        const val MAX_SCANNED_PER_FIELD = 2000
    }
}

@Dao
interface RfidScanAttemptDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(entity: RfidScanAttemptEntity): Long

    @Query("SELECT * FROM rfid_scan_attempt WHERE taskId = :taskId ORDER BY capturedAtMs ASC LIMIT :limit")
    fun observeForTask(taskId: String, limit: Int = MAX_ATTEMPTS_PER_TASK): Flow<List<RfidScanAttemptEntity>>

    @Query("SELECT * FROM rfid_scan_attempt WHERE taskId = :taskId ORDER BY capturedAtMs ASC LIMIT :limit")
    suspend fun listForTask(taskId: String, limit: Int = MAX_ATTEMPTS_PER_TASK): List<RfidScanAttemptEntity>

    @Query("UPDATE rfid_scan_attempt SET syncStatus = :status WHERE id = :id")
    suspend fun updateStatus(id: String, status: String)

    @Query("DELETE FROM rfid_scan_attempt WHERE taskId = :taskId")
    suspend fun clearForTask(taskId: String)

    @Query("DELETE FROM rfid_scan_attempt")
    suspend fun clearAll()

    companion object {
        /** Audit trail cap for a single task readback. The table remains append-only; this only
         *  prevents any one UI/test read from materializing an unbounded history. */
        const val MAX_ATTEMPTS_PER_TASK = 5000
    }
}

/**
 * One captured proof video for a task's `video_proof` recording-form field
 * (docs/mobile/proof-capture-sync-and-e2e.md §2/§3). Written to Room BEFORE any network call;
 * [syncStatus] tracks the real outbox `PROOF_UPLOAD` row this capture drives: register the
 * server proof, stream bytes to the returned signed URL, complete the proof, then persist the
 * returned server proof id.
 */
@Entity(
    tableName = "proof_capture",
    indices = [
        Index(value = ["idempotencyKey"], unique = true),
        Index(value = ["taskId", "partitionKey", "fieldKey"]),
        Index(value = ["taskId", "partitionKey", "subjectId", "capturedAtMs"]),
    ],
)
data class ProofCaptureEntity(
    @PrimaryKey val id: String,
    val taskId: String,
    /** Normalized partition label; `whole` means the shed has no partition. */
    val partitionKey: String = "whole",
    /** The `video_proof` [sg.mesha.goatos.core.data.forms.FormField.key] this capture answers
     *  (e.g. `shed_video`, `vial_lot_video`, `administration_video`, or an operator-added extra
     *  field key) — server-driven, never hardcoded by the client. */
    val fieldKey: String,
    /** `goat` / `shed` / `vial_lot` / `administration` / `extra` — mirrors the SOP's documented proof
     *  subjects (docs/mobile/proof-capture-sync-and-e2e.md §2 table). */
    val proofSubject: String,
    /** Goat id for row-level vaccination evidence. Several clips may cover the same handling. */
    val subjectId: String? = null,
    val localUri: String,
    val mimeType: String,
    /** Operator-entered description for an extra (beyond the named/required) video — the SOP's
     *  `description`/`help_text` covers the NAMED fields; this is the free-text caption for a
     *  field the operator added themselves. */
    val caption: String? = null,
    /** Human-readable RFID/tag to burn on individual-animal overlays. Never a goat UUID. */
    val rfidTag: String? = null,
    val capturedAtMs: Long,
    /** Freshness/attribution metadata (docs/mobile/proof-capture-sync-and-e2e.md
     *  "Capture-source rules"): device-clock capture/import start/stop, so a verifier can see
     *  when this proof was produced and by whom. */
    val capturedStartMs: Long,
    val capturedEndMs: Long,
    /** The signed-in operator who recorded this video — part of the same freshness proof. */
    val capturedByPrincipalId: String? = null,
    val syncStatus: String = CaptureSyncStatus.PENDING.name,
    /** Stable per-row key so a retried [sg.mesha.goatos.core.data.sync.SyncRepository.enqueueProofUpload]
     *  call never double-registers this capture. */
    val idempotencyKey: String,
    /** The outbox row id driving this capture's registration — lets a process-death recreation
     *  resume observing the SAME outbox item instead of re-enqueueing. */
    val outboxItemId: String? = null,
    /** [sg.mesha.goatos.core.network.dto.ProofReferenceDto.proofId] once the backend has
     *  registered this proof — null until [syncStatus] reaches SYNCED. */
    val serverProofId: String? = null,
    val lastError: String? = null,
    /** The `capture_source` metadata (e.g. `in_app_camera` or `gallery_picker`) persisted WITH the durable row so the
     *  startup-recovery re-registration path
     *  ([sg.mesha.goatos.core.data.capture.ProofCaptureRepository]'s recovery walk) re-sends the
     *  ORIGINAL source. Before this was persisted, recovery had no in-memory `ProofPolicy` and fell
     *  back to the hardcoded default — silently rewriting the metadata of any non-camera source. The
     *  row is the SSOT; no enqueue path re-derives this from a policy. Mirrors
     *  `sg.mesha.goatos.core.data.forms.ProofPolicy.Default.captureSource` (cross-module const cannot
     *  be shared, so both default to [DEFAULT_CAPTURE_SOURCE]). */
    val captureSource: String = DEFAULT_CAPTURE_SOURCE,
    val featureSurface: String? = null,
    val proofMode: String? = null,
    val slotIndex: Int? = null,
    val slotRequired: Boolean = false,
    val processingState: String = ProofProcessingState.CAPTURED_ORIGINAL.name,
    val processingAttempted: Boolean = false,
    val stateAttempt: Int = 0,
    val uploadOriginal: Boolean = false,
    val originalUri: String? = null,
    val processedUri: String? = null,
    val originalBytes: Long? = null,
    val processedBytes: Long? = null,
    val inputWidth: Int? = null,
    val inputHeight: Int? = null,
    val durationMs: Long? = null,
    val targetVideoBitrate: Int? = null,
    val targetAudioBitrate: Int? = null,
    val locationStatus: String? = null,
    val latitude: Double? = null,
    val longitude: Double? = null,
    val gpsAccuracyM: Double? = null,
    val geocoderStatus: String? = null,
    val geocodedAddress: String? = null,
    val lastErrorStage: String? = null,
    val lastErrorClass: String? = null,
    val lastErrorRetryable: Boolean? = null,
    val lastErrorMessageHash: String? = null,
    val uploadSessionId: String? = null,
    val objectGeneration: String? = null,
    val uploadedAtMs: Long? = null,
    val attachedAtMs: Long? = null,
    /** Android Gallery copy already written for this proof row. Prevents retry/recovery from
     *  flooding Gallery with duplicate final media. */
    val gallerySavedUri: String? = null,
    val updatedAtMs: Long = capturedAtMs,
)

enum class ProofProcessingState {
    CAPTURED_ORIGINAL,
    LOCATION_RESOLVING,
    PROCESSING_MEDIA,
    PROCESSED,
    REGISTERING_UPLOAD,
    UPLOADING,
    UPLOAD_CONFIRMED,
    ATTACHED_TO_SUBMISSION,
    PROCESSING_FAILED_ORIGINAL_UPLOAD_QUEUED,
    REGISTER_FAILED_RETRYING,
    UPLOAD_FAILED_RETRYING,
    UPLOAD_ORIGINAL_FAILED_RETRYING,
    DEAD_LETTER,
}

@Entity(
    tableName = "proof_capture_state_event",
    foreignKeys = [
        ForeignKey(
            entity = ProofCaptureEntity::class,
            parentColumns = ["id"],
            childColumns = ["proofId"],
            onDelete = ForeignKey.CASCADE,
        ),
    ],
    indices = [
        Index(value = ["proofId", "occurredAtMs"]),
        Index(value = ["stage", "occurredAtMs"]),
    ],
)
data class ProofCaptureStateEventEntity(
    @PrimaryKey val id: String,
    val proofId: String,
    val fromState: String?,
    val toState: String,
    val stage: String,
    val attempt: Int,
    val occurredAtMs: Long,
    val durationMs: Long? = null,
    val bytesIn: Long? = null,
    val bytesOut: Long? = null,
    val errorClass: String? = null,
    val retryable: Boolean? = null,
)

/** Default `capture_source` for live in-app recording. Kept in lockstep with
 *  `sg.mesha.goatos.core.data.forms.ProofPolicy.Default.captureSource`. */
const val DEFAULT_CAPTURE_SOURCE = "in_app_camera"

@Dao
interface ProofCaptureDao {
    @Insert(onConflict = OnConflictStrategy.ABORT)
    suspend fun insert(entity: ProofCaptureEntity)

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :workflowId " +
            "AND proofSubject = 'workflow_death_draft' " +
            "ORDER BY capturedAtMs ASC, id ASC LIMIT 2",
    )
    fun observeWorkflowDeathDrafts(workflowId: String): Flow<List<ProofCaptureEntity>>

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :workflowId AND fieldKey = :actionId " +
            "AND proofSubject = 'workflow_death_draft' LIMIT 1",
    )
    suspend fun findWorkflowDeathDraft(workflowId: String, actionId: String): ProofCaptureEntity?

    @Query(
        "DELETE FROM proof_capture WHERE taskId = :workflowId AND fieldKey = :actionId " +
            "AND proofSubject = 'workflow_death_draft'",
    )
    suspend fun deleteWorkflowDeathDraft(workflowId: String, actionId: String)

    @Transaction
    suspend fun replaceWorkflowDeathDraft(entity: ProofCaptureEntity): ProofCaptureEntity? {
        val previous = findWorkflowDeathDraft(entity.taskId, entity.fieldKey)
        deleteWorkflowDeathDraft(entity.taskId, entity.fieldKey)
        insert(entity)
        return previous
    }

    @Query(
        "DELETE FROM proof_capture WHERE taskId = :workflowId " +
            "AND proofSubject = 'workflow_death_draft'",
    )
    suspend fun clearWorkflowDeathDrafts(workflowId: String)

    @Query(
        "UPDATE proof_capture SET syncStatus = 'SUBMITTING' WHERE taskId = :workflowId " +
            "AND proofSubject = 'workflow_death_draft'",
    )
    suspend fun markWorkflowDeathDraftsSubmitting(workflowId: String)

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :taskId " +
            "ORDER BY CASE WHEN syncStatus = 'FAILED' THEN 1 ELSE 0 END, capturedAtMs ASC LIMIT :limit",
    )
    fun observeForTask(taskId: String, limit: Int = MAX_PROOFS_PER_TASK): Flow<List<ProofCaptureEntity>>

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "ORDER BY CASE WHEN syncStatus = 'FAILED' THEN 1 ELSE 0 END, capturedAtMs ASC LIMIT :limit",
    )
    fun observeForTaskPartition(
        taskId: String,
        partitionKey: String,
        limit: Int = MAX_PROOFS_PER_TASK,
    ): Flow<List<ProofCaptureEntity>>

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :taskId " +
            "ORDER BY CASE WHEN syncStatus = 'FAILED' THEN 1 ELSE 0 END, capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listForTask(taskId: String, limit: Int = MAX_PROOFS_PER_TASK): List<ProofCaptureEntity>

    /** Task-scoped, all-status cleanup walk. This is deliberately separate from
     *  [listRecoverableUploadsPage]: terminal SYNCED/FAILED rows still own local files that must
     *  be reclaimed, and paging globally before filtering by task can skip an interleaved task. */
    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :taskId " +
            "AND (capturedAtMs > :afterCapturedAtMs OR (capturedAtMs = :afterCapturedAtMs AND id > :afterId)) " +
            "ORDER BY capturedAtMs ASC, id ASC LIMIT :limit",
    )
    suspend fun listForTaskCleanupPage(
        taskId: String,
        afterCapturedAtMs: Long,
        afterId: String,
        limit: Int = TASK_CLEANUP_PAGE_SIZE,
    ): List<ProofCaptureEntity>

    /** R50-028: keyset-paged startup-recovery read — walks bounded ~20-row pages (row-value
     *  keyset on capturedAtMs+id, guaranteeing forward progress) instead of materializing up to
     *  [MAX_PROOFS_PER_TASK] rows in one unbounded read. [capturedBeforeMs] is STRICT (`<`, not
     *  `<=`) so a row captured in the exact same millisecond as the caller's cutoff snapshot is
     *  never matched — see [sg.mesha.goatos.core.data.capture.DefaultProofCaptureRepository]'s
     *  `startupRecoveryCutoffMs` kdoc for why this makes recovery race-free against a fresh
     *  capture on the same (just-constructed) repository instance. */
    @Query(
        "SELECT * FROM proof_capture WHERE serverProofId IS NULL AND syncStatus IN ('PENDING', 'IN_FLIGHT') " +
            "AND capturedAtMs < :capturedBeforeMs " +
            "AND (capturedAtMs > :afterCapturedAtMs OR (capturedAtMs = :afterCapturedAtMs AND id > :afterId)) " +
            "ORDER BY capturedAtMs ASC, id ASC LIMIT :limit",
    )
    suspend fun listRecoverableUploadsPage(
        capturedBeforeMs: Long,
        afterCapturedAtMs: Long = 0L,
        afterId: String = "",
        limit: Int = RECOVERABLE_UPLOADS_PAGE_SIZE,
    ): List<ProofCaptureEntity>

    @Query(
        "SELECT COUNT(*) FROM proof_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND subjectId = :subjectId " +
            "AND syncStatus != 'FAILED'",
    )
    suspend fun activeCountForSubject(taskId: String, partitionKey: String, subjectId: String): Int

    @Query(
        "SELECT COUNT(*) FROM proof_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND proofSubject = :proofSubject " +
            "AND subjectId IS NULL AND syncStatus != 'FAILED'",
    )
    suspend fun activeCountForSubjectType(taskId: String, partitionKey: String, proofSubject: String): Int

    /**
     * Active proofs held by ONE capture SLOT.
     *
     * A screen whose slots are distinct required steps — feed distribution's weight photo, feed
     * video and water video — must cap each slot on its own. Those three share the shed as their
     * subject, so a per-SUBJECT cap pools them: one shared budget of five for three slots, leaving
     * only two re-captures across the whole screen before every further capture is refused and
     * silently writes no row. See ProofPolicy.maximumCountPerField.
     */
    @Query(
        "SELECT COUNT(*) FROM proof_capture WHERE taskId = :taskId AND partitionKey = :partitionKey " +
            "AND fieldKey = :fieldKey AND syncStatus != 'FAILED'",
    )
    suspend fun activeCountForField(taskId: String, partitionKey: String, fieldKey: String): Int

    @Query("SELECT * FROM proof_capture WHERE id = :id LIMIT 1")
    suspend fun findById(id: String): ProofCaptureEntity?

    @Query("UPDATE proof_capture SET outboxItemId = :outboxItemId WHERE id = :id")
    suspend fun setOutboxItemId(id: String, outboxItemId: String?)

    @Query(
        "UPDATE proof_capture SET syncStatus = :status, serverProofId = :serverProofId, " +
            "lastError = :lastError WHERE id = :id",
    )
    suspend fun updateStatus(id: String, status: String, serverProofId: String?, lastError: String?)

    @Query(
        "UPDATE proof_capture SET processingState = :processingState, stateAttempt = :attempt, " +
            "processingAttempted = :processingAttempted, uploadOriginal = :uploadOriginal, " +
            "lastErrorStage = :lastErrorStage, lastErrorClass = :lastErrorClass, " +
            "lastErrorRetryable = :lastErrorRetryable, lastErrorMessageHash = :lastErrorMessageHash, " +
            "updatedAtMs = :updatedAtMs WHERE id = :id",
    )
    suspend fun updateProcessingState(
        id: String,
        processingState: String,
        attempt: Int,
        processingAttempted: Boolean,
        uploadOriginal: Boolean,
        lastErrorStage: String?,
        lastErrorClass: String?,
        lastErrorRetryable: Boolean?,
        lastErrorMessageHash: String?,
        updatedAtMs: Long,
    )

    @Query(
        "UPDATE proof_capture SET localUri = :localUri, mimeType = :mimeType, processingState = :processingState, " +
            "processingAttempted = :processingAttempted, uploadOriginal = :uploadOriginal, processedUri = :processedUri, " +
            "originalBytes = :originalBytes, processedBytes = :processedBytes, inputWidth = :inputWidth, " +
            "inputHeight = :inputHeight, targetVideoBitrate = :targetVideoBitrate, " +
            "targetAudioBitrate = :targetAudioBitrate, updatedAtMs = :updatedAtMs WHERE id = :id",
    )
    suspend fun updateProcessingArtifact(
        id: String,
        localUri: String,
        mimeType: String,
        processingState: String,
        processingAttempted: Boolean,
        uploadOriginal: Boolean,
        processedUri: String?,
        originalBytes: Long?,
        processedBytes: Long?,
        inputWidth: Int?,
        inputHeight: Int?,
        targetVideoBitrate: Int?,
        targetAudioBitrate: Int?,
        updatedAtMs: Long,
    )

    @Query(
        "UPDATE proof_capture SET gallerySavedUri = :gallerySavedUri, updatedAtMs = :updatedAtMs WHERE id = :id",
    )
    suspend fun markGallerySaved(id: String, gallerySavedUri: String, updatedAtMs: Long)

    @Insert(onConflict = OnConflictStrategy.ABORT)
    suspend fun insertStateEvent(entity: ProofCaptureStateEventEntity)

    @Query("SELECT COUNT(*) FROM proof_capture_state_event WHERE proofId = :proofId AND stage = :stage")
    suspend fun countStateEvents(proofId: String, stage: String): Int

    @Query("DELETE FROM proof_capture WHERE id = :id AND taskId = :taskId")
    suspend fun delete(id: String, taskId: String)

    @Query("UPDATE proof_capture SET caption = :caption WHERE id = :id AND taskId = :taskId")
    suspend fun updateCaption(id: String, taskId: String, caption: String)

    @Query("DELETE FROM proof_capture WHERE taskId = :taskId")
    suspend fun clearForTask(taskId: String)

    @Query("DELETE FROM proof_capture")
    suspend fun clearAll()

    companion object {
        /** Safety ceiling for a bounded task read; the write cap is five clips per goat. */
        const val MAX_PROOFS_PER_TASK = 10_000
        const val MAX_PROOFS_PER_GOAT = 5

        /** R50-028: bounded page size for [listRecoverableUploadsPage]'s startup-recovery walk. */
        const val RECOVERABLE_UPLOADS_PAGE_SIZE = 20

        /** Bounded page size for task-local file cleanup before deleting the task's Room rows. */
        const val TASK_CLEANUP_PAGE_SIZE = 20
    }
}
