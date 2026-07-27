package sg.mesha.goatos.core.data.weighing

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import sg.mesha.goatos.core.common.AppResult
import java.util.UUID

data class WeighingScanMatch(
    val row: WeighingRosterRowEntity?,
    val outcome: String,
    val expectedLocationLabel: String?,
    val actualLocationLabel: String?,
)

data class IndividualWeighingDraft(
    val observationId: String,
    val animalId: String,
    val weightKg: Double,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
    val idempotencyKey: String,
)

data class ShedWeighingDraft(
    val shedObservationId: String,
    val resultJson: String,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
    val idempotencyKey: String,
)

data class WeighingScopeState(
    val rosterWindow: List<WeighingRosterRowEntity>,
    val individualDrafts: List<IndividualWeighingDraft>,
    val shedDrafts: List<ShedWeighingDraft>,
    val totalExpected: Int,
)

data class IndividualWeighingCapture(
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val animalId: String,
    val scannedIdentifier: String,
    val weightKg: Double,
    val capturedAtMs: Long? = null,
)

data class ShedPartitionWeighingCapture(
    val tenantId: String,
    val campaignId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val resultJson: String,
    val capturedAtMs: Long? = null,
)

interface WeighingRepository {
    fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState>
    suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>)
    suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch
    suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft>
    suspend fun attachIndividualProof(scopeKey: String, animalId: String, proofCaptureId: String, serverProofId: String?)
    suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft>
    suspend fun attachShedPartitionProof(scopeKey: String, proofCaptureId: String, serverProofId: String?)
    suspend fun discardEditableIndividual(scopeKey: String, animalId: String)
}

class DefaultWeighingRepository(
    private val rosterDao: WeighingRosterDao,
    private val observationDao: WeighingObservationDao,
    private val shedObservationDao: WeighingShedObservationDao,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : WeighingRepository {
    override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
        combine(
            rosterDao.observeWindow(scopeKey, windowSize.coerceIn(1, 250)),
            rosterDao.observeScopeTotal(scopeKey),
            observationDao.observeForScope(scopeKey),
            shedObservationDao.observeForScope(scopeKey),
        ) { roster, total, observations, shedObservations ->
            WeighingScopeState(
                rosterWindow = roster,
                totalExpected = total,
                individualDrafts = observations.map { it.toDraft() },
                shedDrafts = shedObservations.map { it.toDraft() },
            )
        }.flowOn(Dispatchers.Default)

    override suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>) {
        rosterDao.replaceScope(scopeKey, rows)
    }

    override suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch = withContext(Dispatchers.IO) {
        val normalized = normalizeWeighingTag(scannedTag)
        if (normalized.isBlank()) {
            return@withContext WeighingScanMatch(null, "unknown", null, null)
        }
        val row = rosterDao.findByTag(scopeKey, normalized)
        if (row == null) {
            WeighingScanMatch(null, "unknown", null, null)
        } else {
            val mismatch = !row.actualLocationId.isNullOrBlank() &&
                row.actualLocationId != row.expectedLocationId
            WeighingScanMatch(
                row = row,
                outcome = if (mismatch) "wrong_shed" else "expected",
                expectedLocationLabel = row.expectedLocationLabel,
                actualLocationLabel = row.actualLocationLabel ?: row.expectedLocationLabel,
            )
        }
    }

    override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> =
        withContext(Dispatchers.IO) {
            if (capture.weightKg <= 0.0) return@withContext AppResult.Err("Weight must be greater than 0 kg.")
            val scopeKey = weighingScopeKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            val rosterRow = rosterDao.findByAnimal(scopeKey, capture.animalId)
                ?: return@withContext AppResult.Err("Animal is not in this weighing scope.")
            val observationId = idGenerator()
            val idempotencyKey = individualIdempotencyKey(
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                animalId = capture.animalId,
            )
            val existing = observationDao.findByIdempotencyKey(idempotencyKey)
            if (existing != null) return@withContext AppResult.Ok(existing.toDraft())
            val entity = WeighingObservationEntity(
                observationId = observationId,
                scopeKey = scopeKey,
                tenantId = capture.tenantId,
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                expectedLocationId = rosterRow.expectedLocationId,
                expectedLocationLabel = rosterRow.expectedLocationLabel,
                actualLocationId = rosterRow.actualLocationId,
                actualLocationLabel = rosterRow.actualLocationLabel,
                animalId = capture.animalId,
                scannedIdentifier = capture.scannedIdentifier,
                weightKg = capture.weightKg,
                proofCaptureId = null,
                serverProofId = null,
                syncStatus = WeighingSyncStatus.PENDING_LOCAL.name,
                idempotencyKey = idempotencyKey,
                capturedAtMs = capture.capturedAtMs?.takeIf { it > 0L } ?: clock(),
                lastError = null,
            )
            observationDao.insert(entity)
            AppResult.Ok(entity.toDraft())
        }

    override suspend fun attachIndividualProof(
        scopeKey: String,
        animalId: String,
        proofCaptureId: String,
        serverProofId: String?,
    ) = withContext(Dispatchers.IO) {
        val row = observationDao.findByAnimal(scopeKey, animalId) ?: return@withContext
        observationDao.attachProof(
            observationId = row.observationId,
            proofCaptureId = proofCaptureId,
            serverProofId = serverProofId,
            syncStatus = if (serverProofId.isNullOrBlank()) {
                WeighingSyncStatus.PROOF_UPLOADING.name
            } else {
                WeighingSyncStatus.READY_TO_SUBMIT.name
            },
        )
    }

    override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
        withContext(Dispatchers.IO) {
            if (!capture.resultJson.trim().startsWith("{")) {
                return@withContext AppResult.Err("Shed/partition weighing result must be structured JSON.")
            }
            val scopeKey = weighingScopeKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            val idempotencyKey = shedIdempotencyKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            val existing = shedObservationDao.findByIdempotencyKey(idempotencyKey)
            if (existing != null) return@withContext AppResult.Ok(existing.toDraft())
            val entity = WeighingShedObservationEntity(
                shedObservationId = idGenerator(),
                scopeKey = scopeKey,
                tenantId = capture.tenantId,
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                expectedLocationId = capture.expectedLocationId,
                expectedLocationLabel = capture.expectedLocationLabel,
                resultJson = capture.resultJson,
                proofCaptureId = null,
                serverProofId = null,
                syncStatus = WeighingSyncStatus.PENDING_LOCAL.name,
                idempotencyKey = idempotencyKey,
                capturedAtMs = capture.capturedAtMs?.takeIf { it > 0L } ?: clock(),
                lastError = null,
            )
            shedObservationDao.insert(entity)
            AppResult.Ok(entity.toDraft())
        }

    override suspend fun attachShedPartitionProof(
        scopeKey: String,
        proofCaptureId: String,
        serverProofId: String?,
    ) = withContext(Dispatchers.IO) {
        val existing = shedObservationDao.findByScope(scopeKey) ?: return@withContext
        shedObservationDao.attachProof(
            shedObservationId = existing.shedObservationId,
            proofCaptureId = proofCaptureId,
            serverProofId = serverProofId,
            syncStatus = if (serverProofId.isNullOrBlank()) {
                WeighingSyncStatus.PROOF_UPLOADING.name
            } else {
                WeighingSyncStatus.READY_TO_SUBMIT.name
            },
        )
    }

    override suspend fun discardEditableIndividual(scopeKey: String, animalId: String) = withContext(Dispatchers.IO) {
        val row = observationDao.findByAnimal(scopeKey, animalId) ?: return@withContext
        observationDao.deleteEditable(row.observationId)
    }
}

private fun WeighingObservationEntity.toDraft(): IndividualWeighingDraft =
    IndividualWeighingDraft(
        observationId = observationId,
        animalId = animalId,
        weightKg = weightKg,
        proofReady = !serverProofId.isNullOrBlank(),
        readyToSubmit = syncStatus == WeighingSyncStatus.READY_TO_SUBMIT.name ||
            syncStatus == WeighingSyncStatus.ACCEPTED.name,
        idempotencyKey = idempotencyKey,
    )

private fun WeighingShedObservationEntity.toDraft(): ShedWeighingDraft =
    ShedWeighingDraft(
        shedObservationId = shedObservationId,
        resultJson = resultJson,
        proofReady = !serverProofId.isNullOrBlank(),
        readyToSubmit = syncStatus == WeighingSyncStatus.READY_TO_SUBMIT.name ||
            syncStatus == WeighingSyncStatus.ACCEPTED.name,
        idempotencyKey = idempotencyKey,
    )

fun individualIdempotencyKey(
    campaignId: String,
    workGroupId: String,
    campaignShedId: String,
    animalId: String,
): String = "weighing:individual:$campaignId:$workGroupId:$campaignShedId:$animalId"

fun shedIdempotencyKey(campaignId: String, workGroupId: String, campaignShedId: String): String =
    "weighing:shed:$campaignId:$workGroupId:$campaignShedId"

fun weighingShedResult(weightKg: Double, unit: String = "kg"): JsonObject = buildJsonObject {
    put("weight", weightKg)
    put("unit", unit)
}
