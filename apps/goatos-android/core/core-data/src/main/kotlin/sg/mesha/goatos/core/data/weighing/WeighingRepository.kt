package sg.mesha.goatos.core.data.weighing

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignShedDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerCatalogResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRosterRowDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto
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

data class WeighingAssignment(
    val campaignId: String,
    val tenantId: String,
    val workGroupId: String,
    val campaignShedId: String,
    val expectedLocationId: String,
    val expectedLocationLabel: String,
    val label: String,
    val category: String,
    val status: String,
    val expectedCount: Int,
    val periodLabel: String,
)

data class WeighingPlannerCatalog(
    val parks: List<WeighingPlannerPark>,
    val operators: List<WeighingPlannerOperator>,
)

data class WeighingPlannerPark(
    val parkId: String,
    val name: String,
    val kidCount: Int,
    val sheds: List<WeighingPlannerShed>,
    val existingCampaign: WeighingCampaignSummary?,
)

data class WeighingPlannerShed(
    val locationId: String,
    val name: String,
    val kidCount: Int,
    val category: String = "per_shed_partition",
)

data class WeighingCampaignSummary(
    val campaignId: String,
    val status: String,
    val periodStartDate: String,
    val periodEndDate: String,
    val startBusinessDate: String,
    val operatorUserId: String,
    val shedCount: Int,
)

data class WeighingPlannerOperator(
    val userId: String,
    val displayName: String,
    val displayCode: String,
)

data class WeighingPlanDraft(
    val parkId: String,
    val periodStartDate: String,
    val periodEndDate: String,
    val startBusinessDate: String,
    val plannedCapPerDay: Int,
    val operatorUserId: String,
    val sheds: List<WeighingPlannerShed>,
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
    suspend fun listAssignments(): AppResult<List<WeighingAssignment>>
    suspend fun plannerCatalog(periodStartDate: String): AppResult<WeighingPlannerCatalog>
    suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?>
    suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?>
    suspend fun refreshScope(campaignId: String, workGroupId: String, campaignShedId: String, limit: Int = 5000): AppResult<Int>
    suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>)
    suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch
    suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft>
    suspend fun attachIndividualProof(scopeKey: String, animalId: String, proofCaptureId: String, serverProofId: String?)
    suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft>
    suspend fun attachShedPartitionProof(scopeKey: String, proofCaptureId: String, serverProofId: String?)
    suspend fun discardEditableIndividual(scopeKey: String, animalId: String)
}

class DefaultWeighingRepository(
    private val api: AppApi? = null,
    private val tenantId: String = "",
    private val rosterDao: WeighingRosterDao,
    private val observationDao: WeighingObservationDao,
    private val shedObservationDao: WeighingShedObservationDao,
    private val syncRepository: SyncRepository? = null,
    private val appScope: CoroutineScope? = null,
    private val clock: () -> Long = System::currentTimeMillis,
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) : WeighingRepository {
    init {
        startProofReadyReconciler()
    }

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

    override suspend fun listAssignments(): AppResult<List<WeighingAssignment>> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing assignments are not configured.")
        runCatching {
            val campaigns = client.listWeighingCampaigns().items
            AppResult.Ok(campaigns.flatMap { it.toAssignments() })
        }.getOrElse { AppResult.Err(it.message ?: "Could not load weighing assignments.") }
    }

    override suspend fun plannerCatalog(periodStartDate: String): AppResult<WeighingPlannerCatalog> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        runCatching {
            AppResult.Ok(client.getWeighingPlannerCatalog(periodStartDate).toPlannerCatalog())
        }.getOrElse { AppResult.Err(it.message ?: "Could not load weighing planner.") }
    }

    override suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (draft.sheds.isEmpty()) return@withContext AppResult.Err("Select at least one kid shed.")
        runCatching {
            val createIdem = "weighing:create:${draft.periodStartDate}:${draft.parkId}:${draft.sheds.joinToString(",") { it.locationId }}"
            val created = client.createWeighingCampaign(
                idempotencyKey = createIdem,
                request = draft.toCreateRequest(),
            ).campaign
            val publishIdem = "weighing:publish:${created.campaignId}"
            val published = client.publishWeighingCampaign(created.campaignId, publishIdem).campaign
            AppResult.Ok(published.toAssignments().firstOrNull())
        }.getOrElse { AppResult.Err(it.message ?: "Could not publish weighing plan.") }
    }

    override suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing planner is not configured.")
        if (campaignId.isBlank()) return@withContext AppResult.Err("Existing weighing task is missing.")
        if (draft.sheds.isEmpty()) return@withContext AppResult.Err("Select at least one kid shed.")
        runCatching {
            val updateIdem = "weighing:update:$campaignId:${draft.periodStartDate}:${draft.sheds.joinToString(",") { "${it.locationId}:${it.category}" }}"
            val updated = client.updateWeighingCampaign(campaignId, updateIdem, draft.toCreateRequest()).campaign
            val visible = if (updated.status == "draft") {
                val publishIdem = "weighing:publish:$campaignId"
                client.publishWeighingCampaign(campaignId, publishIdem).campaign
            } else {
                updated
            }
            AppResult.Ok(visible.toAssignments().firstOrNull())
        }.getOrElse { AppResult.Err(it.message ?: "Could not update weighing plan.") }
    }

    override suspend fun refreshScope(
        campaignId: String,
        workGroupId: String,
        campaignShedId: String,
        limit: Int,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing roster sync is not configured.")
        val key = weighingScopeKey(campaignId, workGroupId, campaignShedId)
        runCatching {
            val response = client.getWeighingRoster(campaignId, campaignShedId, limit.coerceIn(1, 5000))
            val rows = response.items.map { it.toEntity(scopeKey = key, workGroupId = workGroupId, tenantId = tenantId) }
            rosterDao.replaceScope(key, rows)
            AppResult.Ok(rows.size)
        }.getOrElse { AppResult.Err(it.message ?: "Could not refresh weighing roster.") }
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
            val existing = observationDao.findByAnimal(scopeKey, capture.animalId)
            if (existing?.syncStatus == WeighingSyncStatus.ACCEPTED.name) {
                return@withContext AppResult.Err("This animal's weighing has already synced.")
            }
            if (existing != null && existing.matchesDraft(capture) && existing.proofCaptureId.isNullOrBlank()) {
                return@withContext AppResult.Ok(existing.toDraft())
            }
            if (existing != null) {
                cancelCancellableOutbox(existing.idempotencyKey)
                observationDao.deleteEditable(existing.observationId)
            }
            val idempotencyKey = individualIdempotencyKey(
                campaignId = capture.campaignId,
                workGroupId = capture.workGroupId,
                campaignShedId = capture.campaignShedId,
                animalId = capture.animalId,
                observationId = observationId,
            )
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
        if (!serverProofId.isNullOrBlank()) {
            syncRepository?.enqueueWeighingAnimalObservation(
                campaignId = row.campaignId,
                groupKey = row.scopeKey,
                idempotencyKey = row.idempotencyKey,
                request = WeighingAnimalObservationRequestDto(
                    animalId = row.animalId,
                    weightKg = row.weightKg,
                    proofArtifactId = serverProofId,
                    actualLocationId = row.actualLocationId ?: row.expectedLocationId,
                ),
            )
        }
    }

    override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
        withContext(Dispatchers.IO) {
            if (!capture.resultJson.trim().startsWith("{")) {
                return@withContext AppResult.Err("Shed/partition weighing result must be structured JSON.")
            }
            val scopeKey = weighingScopeKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            val shedObservationId = idGenerator()
            val existing = shedObservationDao.findByScope(scopeKey)
            if (existing?.syncStatus == WeighingSyncStatus.ACCEPTED.name) {
                return@withContext AppResult.Err("This shed / partition result has already synced.")
            }
            if (existing != null && existing.resultJson == capture.resultJson && existing.proofCaptureId.isNullOrBlank()) {
                return@withContext AppResult.Ok(existing.toDraft())
            }
            if (existing != null) {
                cancelCancellableOutbox(existing.idempotencyKey)
                shedObservationDao.deleteEditable(existing.shedObservationId)
            }
            val idempotencyKey = shedIdempotencyKey(
                capture.campaignId,
                capture.workGroupId,
                capture.campaignShedId,
                shedObservationId,
            )
            val entity = WeighingShedObservationEntity(
                shedObservationId = shedObservationId,
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
        val resultWeightKg = weighingShedResultWeightKg(existing.resultJson)
        if (!serverProofId.isNullOrBlank() && resultWeightKg != null) {
            syncRepository?.enqueueWeighingShedObservation(
                campaignId = existing.campaignId,
                groupKey = existing.scopeKey,
                idempotencyKey = existing.idempotencyKey,
                request = WeighingShedObservationRequestDto(
                    campaignShedId = existing.campaignShedId,
                    weightKg = resultWeightKg,
                    proofArtifactId = serverProofId,
                ),
            )
        }
    }

    override suspend fun discardEditableIndividual(scopeKey: String, animalId: String) = withContext(Dispatchers.IO) {
        val row = observationDao.findByAnimal(scopeKey, animalId) ?: return@withContext
        cancelCancellableOutbox(row.idempotencyKey)
        observationDao.deleteEditable(row.observationId)
    }

    internal suspend fun reconcileReadyProofsOnce() {
        observationDao.listReadyProofs().forEach { ready ->
            attachIndividualProof(
                scopeKey = ready.scopeKey,
                animalId = ready.animalId,
                proofCaptureId = ready.proofCaptureId,
                serverProofId = ready.serverProofId,
            )
        }
        shedObservationDao.listReadyProofs().forEach { ready ->
            attachShedPartitionProof(
                scopeKey = ready.scopeKey,
                proofCaptureId = ready.proofCaptureId,
                serverProofId = ready.serverProofId,
            )
        }
    }

    private fun startProofReadyReconciler() {
        val scope = appScope ?: return
        if (syncRepository == null) return
        scope.launch(Dispatchers.IO) {
            reconcileReadyProofsOnce()
        }
        scope.launch(Dispatchers.IO) {
            observationDao.observeReadyProofs().collect { readyRows ->
                readyRows.forEach { ready ->
                    attachIndividualProof(
                        scopeKey = ready.scopeKey,
                        animalId = ready.animalId,
                        proofCaptureId = ready.proofCaptureId,
                        serverProofId = ready.serverProofId,
                    )
                }
            }
        }
        scope.launch(Dispatchers.IO) {
            shedObservationDao.observeReadyProofs().collect { readyRows ->
                readyRows.forEach { ready ->
                    attachShedPartitionProof(
                        scopeKey = ready.scopeKey,
                        proofCaptureId = ready.proofCaptureId,
                        serverProofId = ready.serverProofId,
                    )
                }
            }
        }
    }

    private suspend fun cancelCancellableOutbox(idempotencyKey: String) {
        val sync = syncRepository ?: return
        val existing = when (val found = sync.findOutboxItemByIdempotencyKey(idempotencyKey)) {
            is AppResult.Ok -> found.value
            is AppResult.Err -> null
        } ?: return
        if (existing.status == SyncItemStatus.QUEUED || existing.status == SyncItemStatus.FAILED) {
            sync.cancelOutboxItemIfPending(existing.id)
        }
    }
}

private fun WeighingObservationEntity.matchesDraft(capture: IndividualWeighingCapture): Boolean =
    tenantId == capture.tenantId &&
        campaignId == capture.campaignId &&
        workGroupId == capture.workGroupId &&
        campaignShedId == capture.campaignShedId &&
        animalId == capture.animalId &&
        scannedIdentifier == capture.scannedIdentifier &&
        weightKg == capture.weightKg

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

private fun WeighingRosterRowDto.toEntity(scopeKey: String, workGroupId: String, tenantId: String): WeighingRosterRowEntity =
    WeighingRosterRowEntity(
        id = listOf(campaignId, workGroupId, campaignShedId, animalId).joinToString(":"),
        scopeKey = scopeKey,
        tenantId = tenantId,
        campaignId = campaignId,
        workGroupId = workGroupId,
        campaignShedId = campaignShedId,
        expectedLocationId = expectedLocationId,
        expectedLocationLabel = expectedLocationLabel,
        actualLocationId = currentLocationId?.takeIf { it.isNotBlank() },
        actualLocationLabel = currentLocationLabel?.takeIf { it.isNotBlank() },
        animalId = animalId,
        displayAnimalId = displayAnimalId.ifBlank { animalId.takeLast(8) },
        primaryTag = primaryIdentifier,
        secondaryTag = secondaryIdentifier?.takeIf { it.isNotBlank() },
        normalizedPrimaryTag = normalizeWeighingTag(primaryIdentifier),
        normalizedSecondaryTag = secondaryIdentifier?.takeIf { it.isNotBlank() }?.let(::normalizeWeighingTag),
        status = status,
        availabilityStatus = availabilityStatus,
        seq = seq,
        updatedAt = System.currentTimeMillis(),
    )

private fun WeighingPlannerCatalogResponseDto.toPlannerCatalog(): WeighingPlannerCatalog =
    WeighingPlannerCatalog(
        parks = parks.map { park ->
            WeighingPlannerPark(
                parkId = park.parkId,
                name = park.name,
                kidCount = park.kidCount,
                sheds = park.sheds.map { shed ->
                    WeighingPlannerShed(
                        locationId = shed.locationId,
                        name = shed.name,
                        kidCount = shed.kidCount,
                    )
                },
                existingCampaign = park.existingCampaign?.let { existing ->
                    WeighingCampaignSummary(
                        campaignId = existing.campaignId,
                        status = existing.status,
                        periodStartDate = existing.periodStartDate,
                        periodEndDate = existing.periodEndDate,
                        startBusinessDate = existing.startBusinessDate,
                        operatorUserId = existing.operatorUserId,
                        shedCount = existing.shedCount,
                    )
                },
            )
        },
        operators = operators.map {
            WeighingPlannerOperator(
                userId = it.userId,
                displayName = it.displayName,
                displayCode = it.displayCode,
            )
        },
    )

private fun WeighingPlanDraft.toCreateRequest(): WeighingCreateCampaignRequestDto =
    WeighingCreateCampaignRequestDto(
        parkId = parkId,
        periodStartDate = periodStartDate,
        periodEndDate = periodEndDate,
        startBusinessDate = startBusinessDate,
        plannedCapPerDay = plannedCapPerDay,
        operatorUserId = operatorUserId,
        sheds = sheds.map {
            WeighingCreateCampaignShedDto(
                locationId = it.locationId,
                locationType = "shed",
                displayName = it.name,
                weighingCategory = it.category,
            )
        },
    )

private fun WeighingCampaignDto.toAssignments(): List<WeighingAssignment> =
    sheds
        .filter { shed -> status in setOf("published", "in_progress", "delayed") && shed.status != "cancelled" }
        .map { shed ->
            WeighingAssignment(
                campaignId = campaignId,
                tenantId = tenantId,
                workGroupId = shed.campaignShedId,
                campaignShedId = shed.campaignShedId,
                expectedLocationId = shed.locationId,
                expectedLocationLabel = shed.displayName,
                label = shed.displayName,
                category = shed.weighingCategory,
                status = shed.status,
                expectedCount = shed.expectedAnimalCount,
                periodLabel = listOf(periodStartDate, periodEndDate)
                    .filter { it.isNotBlank() }
                    .joinToString(" - "),
            )
        }

fun individualIdempotencyKey(
    campaignId: String,
    workGroupId: String,
    campaignShedId: String,
    animalId: String,
    observationId: String,
): String = "weighing:individual:$campaignId:$workGroupId:$campaignShedId:$animalId:$observationId"

fun shedIdempotencyKey(campaignId: String, workGroupId: String, campaignShedId: String, shedObservationId: String): String =
    "weighing:shed:$campaignId:$workGroupId:$campaignShedId:$shedObservationId"

fun weighingShedResult(weightKg: Double, unit: String = "kg"): JsonObject = buildJsonObject {
    put("weight", weightKg)
    put("unit", unit)
}

private fun weighingShedResultWeightKg(resultJson: String): Double? =
    runCatching {
        Json.parseToJsonElement(resultJson)
            .jsonObject["weight"]
            ?.jsonPrimitive
            ?.doubleOrNull
            ?.takeIf { it > 0.0 }
    }.getOrNull()
