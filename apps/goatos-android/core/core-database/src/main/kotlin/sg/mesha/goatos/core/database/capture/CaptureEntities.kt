package sg.mesha.goatos.core.database.capture

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
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
 * scanned list as transient state. [syncStatus] tracks this row's OWN durability, not a
 * network write (a scan itself never leaves the device individually; it rides inside the
 * shed-submit payload) — SYNCED here means "included in a submission the outbox has queued".
 */
@Entity(
    tableName = "scanned_goat_capture",
    indices = [
        // Dedup: the same tag scanned twice for the same task/field is one row, not two.
        Index(value = ["taskId", "fieldKey", "tag"], unique = true),
        Index(value = ["taskId", "fieldKey", "capturedAtMs"]),
    ],
)
data class ScannedGoatEntity(
    @PrimaryKey val id: String,
    val taskId: String,
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

@Dao
interface ScannedGoatDao {
    /** Insert-or-ignore: the unique (taskId, fieldKey, tag) index makes a repeat scan of the
     *  same tag a silent no-op — dedup happens at the DB layer, not just in memory. */
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(entity: ScannedGoatEntity): Long

    // Bounded (mobile-guard: unbounded-db-read) — a shed's scanned-goat count is naturally
    // capacity-bounded, but the query still carries an explicit LIMIT rather than relying on
    // that business fact alone.
    @Query(
        "SELECT * FROM scanned_goat_capture WHERE taskId = :taskId AND fieldKey = :fieldKey " +
            "ORDER BY capturedAtMs ASC LIMIT :limit",
    )
    fun observeForField(taskId: String, fieldKey: String, limit: Int = MAX_SCANNED_PER_FIELD): Flow<List<ScannedGoatEntity>>

    @Query("SELECT COUNT(*) FROM scanned_goat_capture WHERE taskId = :taskId AND fieldKey = :fieldKey")
    fun observeCountForField(taskId: String, fieldKey: String): Flow<Int>

    @Query("SELECT * FROM scanned_goat_capture WHERE taskId = :taskId ORDER BY capturedAtMs ASC LIMIT :limit")
    suspend fun listForTask(taskId: String, limit: Int = MAX_SCANNED_PER_FIELD): List<ScannedGoatEntity>

    @Query("SELECT * FROM scanned_goat_capture WHERE taskId = :taskId ORDER BY capturedAtMs ASC LIMIT :limit")
    fun observeForTask(taskId: String, limit: Int = MAX_SCANNED_PER_FIELD): Flow<List<ScannedGoatEntity>>

    @Query("UPDATE scanned_goat_capture SET syncStatus = :status WHERE taskId = :taskId")
    suspend fun markTaskStatus(taskId: String, status: String)

    @Query("DELETE FROM scanned_goat_capture WHERE taskId = :taskId")
    suspend fun clearForTask(taskId: String)

    @Query("DELETE FROM scanned_goat_capture")
    suspend fun clearAll()

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
        Index(value = ["taskId", "fieldKey"]),
        Index(value = ["taskId", "subjectId", "capturedAtMs"]),
    ],
)
data class ProofCaptureEntity(
    @PrimaryKey val id: String,
    val taskId: String,
    /** The `video_proof` [sg.mesha.goatos.core.data.forms.FormField.key] this capture answers
     *  (e.g. `shed_video`, `vial_lot_video`, `administration_video`, or an operator-added extra
     *  field key) — server-driven, never hardcoded by the client. */
    val fieldKey: String,
    /** `shed` / `vial_lot` / `administration` / `extra` — mirrors the SOP's documented proof
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
    val capturedAtMs: Long,
    /** Freshness/anti-fraud metadata (docs/mobile/proof-capture-sync-and-e2e.md "Camera-only
     *  capture"): device-clock record start/stop, so a verifier can see this was a live,
     *  in-app recording of a plausible duration — never an imported file (no picker path
     *  exists to produce one). */
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
)

@Dao
interface ProofCaptureDao {
    @Insert(onConflict = OnConflictStrategy.ABORT)
    suspend fun insert(entity: ProofCaptureEntity)

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :taskId " +
            "ORDER BY CASE WHEN syncStatus = 'FAILED' THEN 1 ELSE 0 END, capturedAtMs ASC LIMIT :limit",
    )
    fun observeForTask(taskId: String, limit: Int = MAX_PROOFS_PER_TASK): Flow<List<ProofCaptureEntity>>

    @Query(
        "SELECT * FROM proof_capture WHERE taskId = :taskId " +
            "ORDER BY CASE WHEN syncStatus = 'FAILED' THEN 1 ELSE 0 END, capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listForTask(taskId: String, limit: Int = MAX_PROOFS_PER_TASK): List<ProofCaptureEntity>

    @Query(
        "SELECT * FROM proof_capture WHERE serverProofId IS NULL AND syncStatus IN ('PENDING', 'IN_FLIGHT') " +
            "AND capturedAtMs <= :capturedBeforeMs " +
            "ORDER BY capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listRecoverableUploads(
        capturedBeforeMs: Long,
        limit: Int = MAX_PROOFS_PER_TASK,
    ): List<ProofCaptureEntity>

    @Query(
        "SELECT COUNT(*) FROM proof_capture WHERE taskId = :taskId AND subjectId = :subjectId " +
            "AND syncStatus != 'FAILED'",
    )
    suspend fun activeCountForSubject(taskId: String, subjectId: String): Int

    @Query("SELECT * FROM proof_capture WHERE id = :id LIMIT 1")
    suspend fun findById(id: String): ProofCaptureEntity?

    @Query("UPDATE proof_capture SET outboxItemId = :outboxItemId WHERE id = :id")
    suspend fun setOutboxItemId(id: String, outboxItemId: String)

    @Query(
        "UPDATE proof_capture SET syncStatus = :status, serverProofId = :serverProofId, " +
            "lastError = :lastError WHERE id = :id",
    )
    suspend fun updateStatus(id: String, status: String, serverProofId: String?, lastError: String?)

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
    }
}
