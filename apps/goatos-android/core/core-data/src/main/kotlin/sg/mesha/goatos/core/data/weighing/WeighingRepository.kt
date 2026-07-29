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
import sg.mesha.goatos.core.network.dto.WeighingAcceptedObservationDto
import sg.mesha.goatos.core.network.dto.WeighingAnimalObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignRequestDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignShedDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerCatalogResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRosterRowDto
import sg.mesha.goatos.core.network.dto.WeighingShedObservationRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto
import java.util.UUID
import java.time.Instant

data class WeighingScanMatch(
    val row: WeighingRosterRowEntity?,
    val outcome: String,
    val expectedLocationLabel: String?,
    val actualLocationLabel: String?,
)

data class IndividualWeighingDraft(
    val observationId: String,
    val animalId: String,
    val scannedIdentifier: String,
    val weightKg: Double,
    val capturedAtMs: Long,
    val proofCaptureId: String?,
    val proofReady: Boolean,
    val readyToSubmit: Boolean,
    val syncedToBackend: Boolean,
    val idempotencyKey: String,
    val serverProofId: String? = null,
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

data class WeighingLeadershipVideo(
    val proofId: String,
    val downloadUrl: String,
)

data class WeighingLeadershipAnimal(
    val rfid: String,
    val weightKg: Double,
    val acceptedAt: String,
    val videos: List<WeighingLeadershipVideo>,
)

data class WeighingLeadershipShed(
    val campaignId: String,
    val campaignShedId: String,
    val shedName: String,
    val category: String,
    val status: String,
    val periodLabel: String,
    val animals: List<WeighingLeadershipAnimal>,
    val animalCount: Int?,
    val totalWeightKg: Double?,
    val averageWeightKg: Double?,
    val videos: List<WeighingLeadershipVideo>,
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
    val proofArtifactIds: List<String> = emptyList(),
    val capturedAtMs: Long? = null,
)

interface WeighingRepository {
    fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState>
    suspend fun listAssignments(): AppResult<List<WeighingAssignment>>
    suspend fun listLeadershipVideos(): AppResult<List<WeighingLeadershipShed>>
    suspend fun plannerCatalog(periodStartDate: String): AppResult<WeighingPlannerCatalog>
    suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?>
    suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?>
    suspend fun refreshScope(campaignId: String, workGroupId: String, campaignShedId: String, maxRows: Int = 10_000): AppResult<Int>
    suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>)
    suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch
    suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft>
    suspend fun attachIndividualProof(scopeKey: String, animalId: String, proofCaptureId: String, serverProofId: String?)
    suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft>
    suspend fun attachShedPartitionProof(
        scopeKey: String,
        proofCaptureId: String,
        serverProofId: String?,
        serverProofIds: List<String> = emptyList(),
    )
    suspend fun discardEditableIndividual(scopeKey: String, animalId: String)
    suspend fun submitIndividualScope(
        campaignId: String,
        campaignShedId: String,
        scannedIdentifiers: List<String>,
    ): AppResult<Unit>
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

    override suspend fun listLeadershipVideos(): AppResult<List<WeighingLeadershipShed>> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing videos are not configured.")
        runCatching {
            val assignments = client.listWeighingCampaigns().items.flatMap { it.toAssignments() }
            val sheds = assignments.map { assignment ->
                val detail = client.getWeighingLeadershipShedVideos(
                    campaignId = assignment.campaignId,
                    campaignShedId = assignment.campaignShedId,
                ).shed
                val lump = detail.lumpSum
                WeighingLeadershipShed(
                    campaignId = detail.campaignId,
                    campaignShedId = detail.campaignShedId,
                    shedName = detail.shedName,
                    category = detail.weighingCategory,
                    status = detail.status,
                    periodLabel = assignment.periodLabel,
                    animals = detail.individual.map { observation ->
                        WeighingLeadershipAnimal(
                            rfid = observation.animalId.orEmpty(),
                            weightKg = observation.weightKg,
                            acceptedAt = observation.acceptedAt,
                            videos = observation.media.map { WeighingLeadershipVideo(it.proofId, it.downloadUrl) },
                        )
                    },
                    animalCount = lump?.animalCount,
                    totalWeightKg = lump?.weightKg,
                    averageWeightKg = lump?.averageWeightKg,
                    videos = lump?.media.orEmpty().map { WeighingLeadershipVideo(it.proofId, it.downloadUrl) },
                )
            }
            AppResult.Ok(sheds)
        }.getOrElse { AppResult.Err(it.message ?: "Could not load weighing videos.") }
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
        maxRows: Int,
    ): AppResult<Int> = withContext(Dispatchers.IO) {
        val client = api ?: return@withContext AppResult.Err("Weighing roster sync is not configured.")
        val key = weighingScopeKey(campaignId, workGroupId, campaignShedId)
        runCatching {
            val safetyLimit = maxRows.coerceIn(1, MAX_ROSTER_SYNC_ROWS)
            val rows = mutableListOf<WeighingRosterRowEntity>() // mobile-guard:ignore: bounded by safetyLimit and kept until successful atomic Room replace
            val accepted = linkedMapOf<String, WeighingAcceptedObservationDto>() // mobile-guard:ignore: bounded by safetyLimit/MAX_ROSTER_SYNC_ROWS within one refreshScope call, then discarded
            var cursor: String? = null
            do {
                val remaining = safetyLimit - rows.size
                val response = client.getWeighingRoster(
                    campaignId = campaignId,
                    campaignShedId = campaignShedId,
                    cursor = cursor,
                    limit = minOf(ROSTER_SYNC_PAGE_SIZE, remaining),
                )
                rows += response.items.map { it.toEntity(scopeKey = key, workGroupId = workGroupId, tenantId = tenantId) }
                response.observations.forEach { observation ->
                    accepted[observation.observationId] = observation
                }
                val next = response.nextCursor?.takeIf { it.isNotBlank() && it != cursor }
                cursor = next
            } while (cursor != null && rows.size < safetyLimit)
            rosterDao.replaceScope(key, rows)
            accepted.values.forEach { observation ->
                val animalId = observation.animalId.trim()
                val scannedIdentifier = observation.scannedIdentifier.trim().ifBlank { animalId }
                if (animalId.isNotEmpty() && observation.weightKg > 0.0 && observation.proofArtifactId.isNotBlank()) {
                    observationDao.restoreAccepted(
                        WeighingObservationEntity(
                            observationId = observation.observationId,
                            scopeKey = key,
                            tenantId = tenantId,
                            campaignId = campaignId,
                            workGroupId = workGroupId,
                            campaignShedId = campaignShedId,
                            expectedLocationId = observation.expectedLocationId,
                            expectedLocationLabel = "",
                            actualLocationId = null,
                            actualLocationLabel = null,
                            animalId = animalId,
                            scannedIdentifier = scannedIdentifier,
                            weightKg = observation.weightKg,
                            proofCaptureId = observation.proofArtifactId,
                            serverProofId = observation.proofArtifactId,
                            syncStatus = WeighingSyncStatus.ACCEPTED.name,
                            idempotencyKey = "weighing:server:${observation.observationId}",
                            capturedAtMs = observation.acceptedAt.toEpochMillisOrNow(),
                            lastError = null,
                        ),
                    )
                }
            }
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

    override suspend fun submitIndividualScope(
        campaignId: String,
        campaignShedId: String,
        scannedIdentifiers: List<String>,
    ): AppResult<Unit> =
        withContext(Dispatchers.IO) {
            val service = api ?: return@withContext AppResult.Err("Weighing service is unavailable.")
            try {
                val idempotencyKey = weighingSubmitIdempotencyKey(
                    campaignId,
                    campaignShedId,
                    scannedIdentifiers,
                )
                service.submitWeighingScope(
                    campaignId,
                    campaignShedId,
                    idempotencyKey,
                    WeighingScopeSubmitRequestDto(scannedIdentifiers),
                )
                AppResult.Ok(Unit)
            } catch (error: Throwable) {
                AppResult.Err(error.message ?: "Couldn't submit weighing shed.", error)
            }
        }

    override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> =
        withContext(Dispatchers.IO) {
            if (capture.weightKg <= 0.0) return@withContext AppResult.Err("Weight must be greater than 0 kg.")
            val scopeKey = weighingScopeKey(capture.campaignId, capture.workGroupId, capture.campaignShedId)
            val rosterRow = rosterDao.findByAnimal(scopeKey, capture.animalId)
            val observationId = idGenerator()
            val existing = observationDao.findByAnimal(scopeKey, capture.animalId)
            if (existing != null && existing.matchesDraft(capture) && existing.proofCaptureId.isNullOrBlank()) {
                return@withContext AppResult.Ok(existing.toDraft())
            }
            if (existing?.syncStatus == WeighingSyncStatus.ACCEPTED.name) {
                val revision = existing.copy(
                    scannedIdentifier = capture.scannedIdentifier,
                    weightKg = capture.weightKg,
                    syncStatus = WeighingSyncStatus.READY_TO_SUBMIT.name,
                    idempotencyKey = individualIdempotencyKey(
                        campaignId = capture.campaignId,
                        workGroupId = capture.workGroupId,
                        campaignShedId = capture.campaignShedId,
                        animalId = capture.animalId,
                        observationId = observationId,
                    ),
                    capturedAtMs = capture.capturedAtMs?.takeIf { it > 0L } ?: clock(),
                    lastError = null,
                )
                observationDao.update(revision)
                return@withContext AppResult.Ok(revision.toDraft())
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
                expectedLocationId = rosterRow?.expectedLocationId ?: capture.campaignShedId,
                expectedLocationLabel = rosterRow?.expectedLocationLabel ?: "Assigned shed",
                actualLocationId = rosterRow?.actualLocationId,
                actualLocationLabel = rosterRow?.actualLocationLabel,
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
        if (
            row.syncStatus == WeighingSyncStatus.ACCEPTED.name &&
            row.proofCaptureId == proofCaptureId &&
            row.serverProofId == serverProofId
        ) {
            return@withContext
        }
        val isProofRevision = !row.serverProofId.isNullOrBlank()
        val revisionIdempotencyKey = if (isProofRevision && !serverProofId.isNullOrBlank()) {
            "${row.idempotencyKey.substringBefore(":proof:")}:proof:$serverProofId"
        } else {
            row.idempotencyKey
        }
        observationDao.attachProof(
            observationId = row.observationId,
            proofCaptureId = proofCaptureId,
            serverProofId = serverProofId,
            idempotencyKey = revisionIdempotencyKey,
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
                idempotencyKey = revisionIdempotencyKey,
                request = WeighingAnimalObservationRequestDto(
                    animalId = row.animalId,
                    campaignShedId = row.campaignShedId,
                    scannedIdentifier = row.scannedIdentifier,
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
            val proofBundle = normalizedProofArtifactIds(null, capture.proofArtifactIds)
            if (proofBundle.isNotEmpty()) {
                val result = weighingShedResultValues(capture.resultJson)
                if (result != null) {
                    syncRepository?.enqueueWeighingShedObservation(
                        campaignId = capture.campaignId,
                        groupKey = scopeKey,
                        idempotencyKey = idempotencyKey,
                        request = WeighingShedObservationRequestDto(
                            campaignShedId = capture.campaignShedId,
                            weightKg = result.totalWeightKg,
                            animalCount = result.animalCount,
                            averageWeightKg = result.averageWeightKg,
                            proofArtifactId = proofBundle.first(),
                            proofArtifactIds = proofBundle,
                        ),
                    )
                }
            }
            AppResult.Ok(entity.toDraft())
        }

    override suspend fun attachShedPartitionProof(
        scopeKey: String,
        proofCaptureId: String,
        serverProofId: String?,
        serverProofIds: List<String>,
    ) = withContext(Dispatchers.IO) {
        val existing = shedObservationDao.findByScope(scopeKey) ?: return@withContext
        val proofBundle = normalizedProofArtifactIds(serverProofId, serverProofIds)
        if (
            existing.syncStatus == WeighingSyncStatus.ACCEPTED.name &&
            existing.proofCaptureId == proofCaptureId &&
            existing.serverProofId == serverProofId
        ) {
            return@withContext
        }
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
        val result = weighingShedResultValues(existing.resultJson)
        if (proofBundle.isNotEmpty() && result != null) {
            cancelCancellableOutbox(existing.idempotencyKey)
            syncRepository?.enqueueWeighingShedObservation(
                campaignId = existing.campaignId,
                groupKey = existing.scopeKey,
                idempotencyKey = existing.idempotencyKey,
                request = WeighingShedObservationRequestDto(
                    campaignShedId = existing.campaignShedId,
                    weightKg = result.totalWeightKg,
                    animalCount = result.animalCount,
                    averageWeightKg = result.averageWeightKg,
                    proofArtifactId = proofBundle.first(),
                    proofArtifactIds = proofBundle,
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

    private companion object {
        const val ROSTER_SYNC_PAGE_SIZE = 20
        const val MAX_ROSTER_SYNC_ROWS = 10_000
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
        scannedIdentifier = scannedIdentifier,
        weightKg = weightKg,
        capturedAtMs = capturedAtMs,
        proofCaptureId = proofCaptureId,
        proofReady = !proofCaptureId.isNullOrBlank(),
        readyToSubmit = syncStatus == WeighingSyncStatus.READY_TO_SUBMIT.name ||
            syncStatus == WeighingSyncStatus.ACCEPTED.name,
        syncedToBackend = syncStatus == WeighingSyncStatus.ACCEPTED.name,
        idempotencyKey = idempotencyKey,
        serverProofId = serverProofId,
    )

private fun WeighingShedObservationEntity.toDraft(): ShedWeighingDraft =
    ShedWeighingDraft(
        shedObservationId = shedObservationId,
        resultJson = resultJson,
        proofReady = !proofCaptureId.isNullOrBlank(),
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

private fun String.toEpochMillisOrNow(): Long =
    runCatching { Instant.parse(this).toEpochMilli() }.getOrDefault(System.currentTimeMillis())

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
        .filter { shed ->
            status in setOf("published", "in_progress", "delayed", "completed") &&
                shed.status.lowercase() !in setOf("canceled", "cancelled")
        }
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

private fun weighingSubmitIdempotencyKey(
    campaignId: String,
    campaignShedId: String,
    scannedIdentifiers: List<String>,
): String {
    val scopeHash = scannedIdentifiers
        .map { it.trim() }
        .filter { it.isNotEmpty() }
        .distinct()
        .sorted()
        .joinToString("|")
        .hashCode()
        .toUInt()
        .toString(16)
    return "weighing:submit:$campaignId:$campaignShedId:$scopeHash"
}

fun weighingShedResult(weightKg: Double, unit: String = "kg"): JsonObject = buildJsonObject {
    put("weight", weightKg)
    put("unit", unit)
}

private data class WeighingShedResultValues(
    val totalWeightKg: Double,
    val animalCount: Int,
    val averageWeightKg: Double,
)

private fun weighingShedResultValues(resultJson: String): WeighingShedResultValues? =
    runCatching {
        val result = Json.parseToJsonElement(resultJson).jsonObject
        val total = (result["total_weight_kg"] ?: result["weight"])?.jsonPrimitive?.doubleOrNull ?: return@runCatching null
        val count = result["animal_count"]?.jsonPrimitive?.content?.toIntOrNull() ?: 1
        if (total <= 0 || count <= 0) return@runCatching null
        WeighingShedResultValues(total, count, total / count)
    }.getOrNull()

private fun normalizedProofArtifactIds(primary: String?, ids: List<String>): List<String> =
    buildList {
        fun addProofId(id: String?) {
            val normalized = id?.trim().orEmpty()
            if (normalized.isNotEmpty() && normalized !in this) add(normalized)
        }
        addProofId(primary)
        ids.forEach(::addProofId)
    }.take(5)
