package sg.mesha.goatos.core.data.weighing

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Update
import kotlinx.coroutines.flow.Flow

enum class WeighingCategory { INDIVIDUAL_ANIMAL, PER_SHED_PARTITION }
enum class WeighingSyncStatus { PENDING_LOCAL, PROOF_UPLOADING, READY_TO_SUBMIT, SYNC_FAILED, ACCEPTED }

/**
 * LOCAL SCAN STATE, not a server roster. Free-flow weighing has no expected-animal list — the
 * backend dropped `weighing_expected_animals` (000079) and the scope read's `items` array is
 * permanently empty — so nothing syncs into this table from the network any more. `animalId` here
 * is the local scan LABEL the ViewModel mints for a scanned tag, never a herd identity.
 */
@Entity(
    tableName = "weighing_roster_row",
    indices = [
        Index(value = ["scopeKey"]),
        Index(value = ["scopeKey", "seq"]),
        Index(value = ["scopeKey", "normalizedPrimaryTag"]),
        Index(value = ["scopeKey", "normalizedSecondaryTag"]),
        Index(value = ["campaignId", "campaignShedId", "animalId"], unique = true),
    ],
)
data class WeighingRosterRowEntity(
    @PrimaryKey val id: String,
    val scopeKey: String,
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val actualLocationId: String?,
    val actualLocationLabel: String?,
    val animalId: String,
    val displayAnimalId: String,
    val primaryTag: String,
    val secondaryTag: String?,
    val normalizedPrimaryTag: String,
    val normalizedSecondaryTag: String?,
    val status: String,
    val availabilityStatus: String?,
    val seq: Long,
    val updatedAt: Long,
)

@Entity(
    tableName = "weighing_observation",
    indices = [
        Index(value = ["idempotencyKey"], unique = true),
        Index(value = ["scopeKey", "capturedAtMs"]),
        // IDENTITY: the scanned tag, never an animal id. Weighing is free-flow -- the backend
        // dropped weighing_observations.animal_id (000078) and weighing_expected_animals (000079),
        // so a capture carries no herd identity at all and animal_id is never sent. Keying this
        // unique index on that column made every capture in one bucket collide on animalId = "",
        // and the DAO inserts with OnConflictStrategy.IGNORE -- so the SECOND scanned tag was
        // silently dropped. scanned_identifier is REQUIRED on the record contract, so it is the
        // only identity that can carry a uniqueness rule here.
        Index(value = ["campaignId", "campaignShedId", "scannedIdentifier"], unique = true),
        Index(value = ["campaignId", "workGroupId", "campaignShedId"]),
    ],
)
data class WeighingObservationEntity(
    @PrimaryKey val observationId: String,
    val scopeKey: String,
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val actualLocationId: String?,
    val actualLocationLabel: String?,
    /** The raw scanned tag. Free-flow weighing's ONLY identity; there is deliberately no animalId. */
    val scannedIdentifier: String,
    val weightKg: Double,
    val proofCaptureId: String?,
    val serverProofId: String?,
    val syncStatus: String,
    val idempotencyKey: String,
    val capturedAtMs: Long,
    val lastError: String?,
    // The verifier's verdict for THIS capture, cached with the capture it belongs to so a
    // sent-back animal still reads as sent-back offline. Without it the capture list renders a
    // rejected animal exactly like an accepted one -- green, "Video synced" -- and the operator
    // only discovers the rejection when Submit refuses the entire shed.
    val verificationStatus: String? = null,
    val reworkReason: String? = null,
)

@Entity(
    tableName = "weighing_shed_observation",
    indices = [
        Index(value = ["idempotencyKey"], unique = true),
        Index(value = ["campaignId", "campaignShedId"], unique = true),
        Index(value = ["scopeKey", "capturedAtMs"]),
    ],
)
data class WeighingShedObservationEntity(
    @PrimaryKey val shedObservationId: String,
    val scopeKey: String,
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val resultJson: String,
    val proofCaptureId: String?,
    val serverProofId: String?,
    val syncStatus: String,
    val idempotencyKey: String,
    val capturedAtMs: Long,
    val lastError: String?,
)

data class WeighingIndividualReadyProofRow(
    val scopeKey: String,
    val scannedIdentifier: String,
    val proofCaptureId: String,
    val serverProofId: String,
)

data class WeighingShedReadyProofRow(
    val scopeKey: String,
    val proofCaptureId: String,
    val serverProofId: String,
)

@Dao
interface WeighingRosterDao {
    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsertAll(rows: List<WeighingRosterRowEntity>)

    @Query("SELECT * FROM weighing_roster_row WHERE scopeKey = :scopeKey ORDER BY seq ASC, id ASC LIMIT :limit")
    fun observeWindow(scopeKey: String, limit: Int): Flow<List<WeighingRosterRowEntity>>

    @Query("SELECT COUNT(*) FROM weighing_roster_row WHERE scopeKey = :scopeKey")
    fun observeScopeTotal(scopeKey: String): Flow<Int>

    @Query(
        "SELECT * FROM weighing_roster_row WHERE scopeKey = :scopeKey AND " +
            "(normalizedPrimaryTag = :normalizedTag OR normalizedSecondaryTag = :normalizedTag) LIMIT 1",
    )
    suspend fun findByTag(scopeKey: String, normalizedTag: String): WeighingRosterRowEntity?

    @Query("DELETE FROM weighing_roster_row WHERE scopeKey = :scopeKey")
    suspend fun deleteForScope(scopeKey: String)

    @androidx.room.Transaction
    suspend fun replaceScope(scopeKey: String, rows: List<WeighingRosterRowEntity>) {
        deleteForScope(scopeKey)
        upsertAll(rows)
    }
}

@Dao
interface WeighingObservationDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(entity: WeighingObservationEntity): Long

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun restoreAccepted(entity: WeighingObservationEntity)

    @Query("DELETE FROM weighing_observation WHERE scopeKey = :scopeKey AND syncStatus = 'ACCEPTED'")
    suspend fun deleteAcceptedForScope(scopeKey: String)

    @Query("DELETE FROM weighing_observation WHERE scopeKey = :scopeKey AND syncStatus = 'ACCEPTED' AND observationId NOT IN (:activeObservationIds)")
    suspend fun deleteAcceptedNotIn(scopeKey: String, activeObservationIds: List<String>)

    @Update
    suspend fun update(entity: WeighingObservationEntity)

    @Query("SELECT * FROM weighing_observation WHERE scopeKey = :scopeKey ORDER BY capturedAtMs ASC LIMIT :limit")
    fun observeForScope(scopeKey: String, limit: Int = MAX_OBSERVATIONS_PER_SCOPE): Flow<List<WeighingObservationEntity>>

    @Query("SELECT * FROM weighing_observation WHERE idempotencyKey = :idempotencyKey LIMIT 1")
    suspend fun findByIdempotencyKey(idempotencyKey: String): WeighingObservationEntity?

    // Draft/duplicate detection is keyed on the SCANNED IDENTIFIER: free-flow weighing has no
    // expected-animal list and no animal identity at all, so the scanned tag is the only identity
    // a capture carries. Matches the (campaignId, campaignShedId, scannedIdentifier) unique index.
    @Query("SELECT * FROM weighing_observation WHERE scopeKey = :scopeKey AND scannedIdentifier = :scannedIdentifier LIMIT 1")
    suspend fun findByScannedIdentifier(scopeKey: String, scannedIdentifier: String): WeighingObservationEntity?

    @Query(
        "SELECT o.scopeKey AS scopeKey, o.scannedIdentifier AS scannedIdentifier, o.proofCaptureId AS proofCaptureId, " +
            "p.serverProofId AS serverProofId FROM weighing_observation o " +
            "JOIN proof_capture p ON p.id = o.proofCaptureId " +
            "WHERE o.syncStatus = 'PROOF_UPLOADING' AND o.proofCaptureId IS NOT NULL " +
            "AND p.serverProofId IS NOT NULL AND p.serverProofId != '' " +
            "ORDER BY o.capturedAtMs ASC LIMIT :limit",
    )
    fun observeReadyProofs(limit: Int = READY_PROOF_RECONCILE_LIMIT): Flow<List<WeighingIndividualReadyProofRow>>

    @Query(
        "SELECT o.scopeKey AS scopeKey, o.scannedIdentifier AS scannedIdentifier, o.proofCaptureId AS proofCaptureId, " +
            "p.serverProofId AS serverProofId FROM weighing_observation o " +
            "JOIN proof_capture p ON p.id = o.proofCaptureId " +
            "WHERE o.syncStatus = 'PROOF_UPLOADING' AND o.proofCaptureId IS NOT NULL " +
            "AND p.serverProofId IS NOT NULL AND p.serverProofId != '' " +
            "ORDER BY o.capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listReadyProofs(limit: Int = READY_PROOF_RECONCILE_LIMIT): List<WeighingIndividualReadyProofRow>

    // Clearing verificationStatus/reworkReason is part of attaching a NEW proof, not a separate
    // concern. A sent-back animal keeps verificationStatus='rework' and the verifier's reason on
    // its local row, and the capture screen renders the red "Sent back -- record this animal
    // again" strip from exactly those two fields. Leaving them set meant that after the operator
    // re-scanned and recorded the replacement video -- which reached the server correctly, with a
    // new proof and verification back to pending -- the phone still showed the rejection, telling
    // him to redo work he had just done. The rejection is history the moment new evidence exists.
    @Query(
        "UPDATE weighing_observation SET proofCaptureId = :proofCaptureId, serverProofId = :serverProofId, " +
            "idempotencyKey = :idempotencyKey, syncStatus = :syncStatus, lastError = NULL, " +
            "verificationStatus = NULL, reworkReason = NULL " +
            "WHERE observationId = :observationId",
    )
    suspend fun attachProof(
        observationId: String,
        proofCaptureId: String,
        serverProofId: String?,
        idempotencyKey: String,
        syncStatus: String,
    )

    @Query("DELETE FROM weighing_observation WHERE observationId = :observationId AND syncStatus != 'ACCEPTED'")
    suspend fun deleteEditable(observationId: String)

    @Query(
        "UPDATE weighing_observation SET syncStatus = 'ACCEPTED', lastError = NULL " +
            "WHERE idempotencyKey = :idempotencyKey",
    )
    suspend fun markAcceptedByIdempotencyKey(idempotencyKey: String): Int

    companion object {
        const val MAX_OBSERVATIONS_PER_SCOPE = 5_000
        const val READY_PROOF_RECONCILE_LIMIT = 20
    }
}

@Dao
interface WeighingShedObservationDao {
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(entity: WeighingShedObservationEntity): Long

    @Query("DELETE FROM weighing_shed_observation WHERE scopeKey = :scopeKey AND syncStatus = 'ACCEPTED'")
    suspend fun deleteAcceptedForScope(scopeKey: String)

    @Query("SELECT * FROM weighing_shed_observation WHERE scopeKey = :scopeKey ORDER BY capturedAtMs ASC LIMIT :limit")
    fun observeForScope(scopeKey: String, limit: Int = MAX_SHED_OBSERVATIONS_PER_SCOPE): Flow<List<WeighingShedObservationEntity>>

    @Query("SELECT * FROM weighing_shed_observation WHERE idempotencyKey = :idempotencyKey LIMIT 1")
    suspend fun findByIdempotencyKey(idempotencyKey: String): WeighingShedObservationEntity?

    @Query("SELECT * FROM weighing_shed_observation WHERE scopeKey = :scopeKey LIMIT 1")
    suspend fun findByScope(scopeKey: String): WeighingShedObservationEntity?

    @Query(
        "SELECT s.scopeKey AS scopeKey, s.proofCaptureId AS proofCaptureId, p.serverProofId AS serverProofId " +
            "FROM weighing_shed_observation s JOIN proof_capture p ON p.id = s.proofCaptureId " +
            "WHERE s.syncStatus = 'PROOF_UPLOADING' AND s.proofCaptureId IS NOT NULL " +
            "AND p.serverProofId IS NOT NULL AND p.serverProofId != '' " +
            "ORDER BY s.capturedAtMs ASC LIMIT :limit",
    )
    fun observeReadyProofs(limit: Int = READY_PROOF_RECONCILE_LIMIT): Flow<List<WeighingShedReadyProofRow>>

    @Query(
        "SELECT s.scopeKey AS scopeKey, s.proofCaptureId AS proofCaptureId, p.serverProofId AS serverProofId " +
            "FROM weighing_shed_observation s JOIN proof_capture p ON p.id = s.proofCaptureId " +
            "WHERE s.syncStatus = 'PROOF_UPLOADING' AND s.proofCaptureId IS NOT NULL " +
            "AND p.serverProofId IS NOT NULL AND p.serverProofId != '' " +
            "ORDER BY s.capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listReadyProofs(limit: Int = READY_PROOF_RECONCILE_LIMIT): List<WeighingShedReadyProofRow>

    @Query(
        "UPDATE weighing_shed_observation SET proofCaptureId = :proofCaptureId, serverProofId = :serverProofId, " +
            "syncStatus = :syncStatus, lastError = NULL WHERE shedObservationId = :shedObservationId",
    )
    suspend fun attachProof(shedObservationId: String, proofCaptureId: String, serverProofId: String?, syncStatus: String)

    @Query("DELETE FROM weighing_shed_observation WHERE shedObservationId = :shedObservationId AND syncStatus != 'ACCEPTED'")
    suspend fun deleteEditable(shedObservationId: String)

    @Query(
        "UPDATE weighing_shed_observation SET syncStatus = 'ACCEPTED', lastError = NULL " +
            "WHERE idempotencyKey = :idempotencyKey",
    )
    suspend fun markAcceptedByIdempotencyKey(idempotencyKey: String): Int

    companion object {
        const val MAX_SHED_OBSERVATIONS_PER_SCOPE = 100
        const val READY_PROOF_RECONCILE_LIMIT = 20
    }
}

fun weighingScopeKey(campaignId: String, workGroupId: String, campaignShedId: String): String =
    listOf(campaignId, workGroupId, campaignShedId).joinToString(":") { it.trim() }

fun normalizeWeighingTag(tag: String): String =
    tag.filter { it.isLetterOrDigit() }.lowercase()
