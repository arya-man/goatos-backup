package sg.mesha.goatos.core.data.weighing

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Index
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

enum class WeighingCategory { INDIVIDUAL_ANIMAL, PER_SHED_PARTITION }
enum class WeighingSyncStatus { PENDING_LOCAL, PROOF_UPLOADING, READY_TO_SUBMIT, SYNC_FAILED, ACCEPTED }

@Entity(
    tableName = "weighing_roster_row",
    indices = [
        Index(value = ["scopeKey"]),
        Index(value = ["scopeKey", "seq"]),
        Index(value = ["scopeKey", "normalizedPrimaryTag"]),
        Index(value = ["scopeKey", "normalizedSecondaryTag"]),
        Index(value = ["campaignId", "animalId"], unique = true),
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
        Index(value = ["campaignId", "animalId"], unique = true),
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
    val animalId: String,
    val scannedIdentifier: String,
    val weightKg: Double,
    val proofCaptureId: String?,
    val serverProofId: String?,
    val syncStatus: String,
    val idempotencyKey: String,
    val capturedAtMs: Long,
    val lastError: String?,
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
    val animalId: String,
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

    @Query("SELECT * FROM weighing_roster_row WHERE scopeKey = :scopeKey AND animalId = :animalId LIMIT 1")
    suspend fun findByAnimal(scopeKey: String, animalId: String): WeighingRosterRowEntity?

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

    @Query("SELECT * FROM weighing_observation WHERE scopeKey = :scopeKey ORDER BY capturedAtMs ASC LIMIT :limit")
    fun observeForScope(scopeKey: String, limit: Int = MAX_OBSERVATIONS_PER_SCOPE): Flow<List<WeighingObservationEntity>>

    @Query("SELECT * FROM weighing_observation WHERE idempotencyKey = :idempotencyKey LIMIT 1")
    suspend fun findByIdempotencyKey(idempotencyKey: String): WeighingObservationEntity?

    @Query("SELECT * FROM weighing_observation WHERE scopeKey = :scopeKey AND animalId = :animalId LIMIT 1")
    suspend fun findByAnimal(scopeKey: String, animalId: String): WeighingObservationEntity?

    @Query(
        "SELECT o.scopeKey AS scopeKey, o.animalId AS animalId, o.proofCaptureId AS proofCaptureId, " +
            "p.serverProofId AS serverProofId FROM weighing_observation o " +
            "JOIN proof_capture p ON p.id = o.proofCaptureId " +
            "WHERE o.syncStatus = 'PROOF_UPLOADING' AND o.proofCaptureId IS NOT NULL " +
            "AND p.serverProofId IS NOT NULL AND p.serverProofId != '' " +
            "ORDER BY o.capturedAtMs ASC LIMIT :limit",
    )
    fun observeReadyProofs(limit: Int = READY_PROOF_RECONCILE_LIMIT): Flow<List<WeighingIndividualReadyProofRow>>

    @Query(
        "SELECT o.scopeKey AS scopeKey, o.animalId AS animalId, o.proofCaptureId AS proofCaptureId, " +
            "p.serverProofId AS serverProofId FROM weighing_observation o " +
            "JOIN proof_capture p ON p.id = o.proofCaptureId " +
            "WHERE o.syncStatus = 'PROOF_UPLOADING' AND o.proofCaptureId IS NOT NULL " +
            "AND p.serverProofId IS NOT NULL AND p.serverProofId != '' " +
            "ORDER BY o.capturedAtMs ASC LIMIT :limit",
    )
    suspend fun listReadyProofs(limit: Int = READY_PROOF_RECONCILE_LIMIT): List<WeighingIndividualReadyProofRow>

    @Query(
        "UPDATE weighing_observation SET proofCaptureId = :proofCaptureId, serverProofId = :serverProofId, " +
            "syncStatus = :syncStatus, lastError = NULL WHERE observationId = :observationId",
    )
    suspend fun attachProof(observationId: String, proofCaptureId: String, serverProofId: String?, syncStatus: String)

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
