package sg.mesha.goatos.core.data.weighing

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.sync.DefaultSyncRepository
import sg.mesha.goatos.core.data.sync.FakeOutboxStore
import sg.mesha.goatos.core.data.sync.SyncEngine
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.database.capture.CaptureSyncStatus
import sg.mesha.goatos.core.database.capture.ProofCaptureEntity
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WeighingCampaignDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignShedDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignSummaryDto
import sg.mesha.goatos.core.network.dto.WeighingCreateCampaignRequestDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerCatalogResponseDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerOperatorDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerParkDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerShedDto
import sg.mesha.goatos.core.network.dto.WeighingRosterResponseDto
import sg.mesha.goatos.core.network.dto.WeighingRosterRowDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosResponseDto
import sg.mesha.goatos.core.network.dto.WeighingObservationDto
import sg.mesha.goatos.core.network.dto.WeighingProofMediaDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WeighingRepositoryTest {
    private lateinit var db: GoatDatabase
    private lateinit var repository: WeighingRepository
    private lateinit var appScope: CoroutineScope
    private val scopeKey = weighingScopeKey("campaign-1", "group-1", "campaign-shed-1")

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        appScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
        db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        repository = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
    }

    @After
    fun tearDown() {
        appScope.cancel()
        db.close()
    }

    @Test
    fun `off-window RFID lookup updates state without loading all animals`() = runTest {
        repository.replaceRoster(scopeKey, (1..5_001).map { index ->
            rosterRow(
                animalId = "animal-$index",
                tag = "TAG-$index",
                seq = index.toLong(),
            )
        })

        val initial = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(20, initial.rosterWindow.size)
        assertEquals(5_001, initial.totalExpected)

        val match = repository.matchTag(scopeKey, "TAG-5001")
        assertEquals("animal-5001", match.row?.animalId)

        val recorded = repository.recordIndividual(
            IndividualWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-1",
                animalId = "animal-5001",
                scannedIdentifier = "TAG-5001",
                weightKg = 12.4,
            ),
        )
        assertTrue(recorded is AppResult.Ok)

        val after = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(20, after.rosterWindow.size)
        assertEquals(listOf("animal-5001"), after.individualDrafts.map { it.animalId })
    }

    @Test
    fun `leadership videos map individual animals and lump sum summaries`() = runTest {
        val campaigns = WeighingCampaignListResponseDto(
            items = listOf(
                WeighingCampaignDto(
                    campaignId = "campaign",
                    status = "completed",
                    periodStartDate = "2026-07-27",
                    periodEndDate = "2026-08-02",
                    sheds = listOf(
                        campaignShed("campaign", "individual", "Gandhi 1", "individual_animal"),
                        campaignShed("campaign", "lump", "Castro 1", "per_shed_partition"),
                    ),
                ),
            ),
        )
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns() = campaigns

            override suspend fun getWeighingLeadershipShedVideos(
                campaignId: String,
                campaignShedId: String,
            ) = WeighingLeadershipShedVideosResponseDto(
                shed = if (campaignShedId == "individual") {
                    WeighingLeadershipShedVideosDto(
                        campaignId = campaignId,
                        campaignShedId = campaignShedId,
                        shedName = "Gandhi 1",
                        weighingCategory = "individual_animal",
                        status = "completed",
                        individual = listOf(
                            WeighingObservationDto(
                                animalId = "RFID-000123",
                                weightKg = 18.25,
                                acceptedAt = "2026-07-29T06:00:00Z",
                                media = listOf(WeighingProofMediaDto("proof-1", "https://proof/1")),
                            ),
                        ),
                    )
                } else {
                    WeighingLeadershipShedVideosDto(
                        campaignId = campaignId,
                        campaignShedId = campaignShedId,
                        shedName = "Castro 1",
                        weighingCategory = "per_shed_partition",
                        status = "completed",
                        lumpSum = WeighingObservationDto(
                            weightKg = 250.0,
                            averageWeightKg = 25.0,
                            animalCount = 10,
                            media = listOf(
                                WeighingProofMediaDto("proof-2", "https://proof/2"),
                                WeighingProofMediaDto("proof-3", "https://proof/3"),
                            ),
                        ),
                    )
                },
            )
        }
        val subject = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val result = subject.listLeadershipVideos() as AppResult.Ok

        assertEquals("RFID-000123", result.value[0].animals.single().rfid)
        assertEquals(18.25, result.value[0].animals.single().weightKg, 0.0)
        assertEquals(10, result.value[1].animalCount)
        assertEquals(250.0, result.value[1].totalWeightKg!!, 0.0)
        assertEquals(25.0, result.value[1].averageWeightKg!!, 0.0)
        assertEquals(2, result.value[1].videos.size)
    }

    @Test
    fun `refresh scope hydrates Room roster from backend rows`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                cursor: String?,
                limit: Int,
            ): WeighingRosterResponseDto = WeighingRosterResponseDto(
                items = listOf(
                    WeighingRosterRowDto(
                        campaignId = campaignId,
                        campaignShedId = campaignShedId,
                        animalId = "animal-9",
                        displayAnimalId = "KID-A-009",
                        primaryIdentifier = "WG-RFID-0009",
                        expectedLocationId = "shed-a",
                        expectedLocationLabel = "Kid Shed A",
                        currentLocationId = "shed-b",
                        currentLocationLabel = "Kid Shed B",
                        status = "pending",
                        availabilityStatus = "moved_other_shed",
                        seq = 9,
                    ),
                ),
            )
        }
        repository = DefaultWeighingRepository(
            api = api,
            tenantId = "tenant-live",
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val refreshed = repository.refreshScope("campaign-1", "group-1", "campaign-shed-1")
        assertEquals(1, (refreshed as AppResult.Ok).value)

        val match = repository.matchTag(scopeKey, "WG-RFID-0009")
        assertEquals("animal-9", match.row?.animalId)
        assertEquals("tenant-live", match.row?.tenantId)
        assertEquals("wrong_shed", match.outcome)
    }

    @Test
    fun `refresh scope follows roster cursors and keeps off-page RFID matchable`() = runTest {
        val requested = mutableListOf<Pair<String?, Int>>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                cursor: String?,
                limit: Int,
            ): WeighingRosterResponseDto {
                requested += cursor to limit
                return if (cursor == null) {
                    WeighingRosterResponseDto(
                        items = listOf(rosterDto(campaignId, campaignShedId, "animal-page-1", "WG-RFID-0001", 1)),
                        nextCursor = "cursor-page-2",
                    )
                } else {
                    WeighingRosterResponseDto(
                        items = listOf(rosterDto(campaignId, campaignShedId, "animal-page-2", "WG-RFID-5001", 101)),
                        nextCursor = null,
                    )
                }
            }
        }
        repository = DefaultWeighingRepository(
            api = api,
            tenantId = "tenant-live",
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val refreshed = repository.refreshScope("campaign-1", "group-1", "campaign-shed-1")

        assertEquals(2, (refreshed as AppResult.Ok).value)
        assertEquals(listOf(null to 20, "cursor-page-2" to 20), requested)
        val match = repository.matchTag(scopeKey, "WG-RFID-5001")
        assertEquals("animal-page-2", match.row?.animalId)
    }

    @Test
    fun `planner catalog maps parks sheds operators and existing campaign guard`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingPlannerCatalog(periodStartDate: String): WeighingPlannerCatalogResponseDto =
                WeighingPlannerCatalogResponseDto(
                    parks = listOf(
                        WeighingPlannerParkDto(
                            parkId = "park-cpt",
                            name = "CPT - Channapatna",
                            kidCount = 144,
                            sheds = listOf(
                                WeighingPlannerShedDto(locationId = "shed-castro-1", name = "Castro 1", kidCount = 80),
                                WeighingPlannerShedDto(locationId = "shed-castro-2", name = "Castro 2", kidCount = 64),
                            ),
                            existingCampaign = WeighingCampaignSummaryDto(
                                campaignId = "campaign-existing",
                                status = "in_progress",
                                periodStartDate = periodStartDate,
                                periodEndDate = "2026-08-02",
                                startBusinessDate = periodStartDate,
                                operatorUserId = "operator-amit",
                                shedCount = 2,
                            ),
                        ),
                    ),
                    operators = listOf(WeighingPlannerOperatorDto(userId = "operator-amit", displayName = "Amit Kumar", displayCode = "AMIT")),
                )
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val result = repository.plannerCatalog("2026-07-27") as AppResult.Ok

        assertEquals("CPT - Channapatna", result.value.parks.single().name)
        assertEquals("campaign-existing", result.value.parks.single().existingCampaign?.campaignId)
        assertEquals(listOf("Castro 1", "Castro 2"), result.value.parks.single().sheds.map { it.name })
        assertEquals("Amit Kumar", result.value.operators.single().displayName)
    }

    @Test
    fun `create and publish plan sends shed categories and returns operator assignment`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            lateinit var createRequest: WeighingCreateCampaignRequestDto
            var publishedCampaignId: String? = null

            override suspend fun createWeighingCampaign(
                idempotencyKey: String,
                request: WeighingCreateCampaignRequestDto,
            ): WeighingCampaignResponseDto {
                createRequest = request
                return WeighingCampaignResponseDto(campaign = weighingCampaign(status = "draft"))
            }

            override suspend fun publishWeighingCampaign(campaignId: String, idempotencyKey: String): WeighingCampaignResponseDto {
                publishedCampaignId = campaignId
                return WeighingCampaignResponseDto(campaign = weighingCampaign(status = "published"))
            }
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val result = repository.createAndPublishPlan(planDraft()) as AppResult.Ok

        assertEquals("campaign-plan", api.publishedCampaignId)
        assertEquals(listOf("individual_animal", "per_shed_partition"), api.createRequest.sheds.map { it.weighingCategory })
        assertEquals("Castro 1", result.value?.label)
    }

    @Test
    fun `edit existing draft updates same campaign and publishes without duplicate create`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            var createCalls = 0
            var updateCampaignId: String? = null
            var publishCampaignId: String? = null

            override suspend fun createWeighingCampaign(
                idempotencyKey: String,
                request: WeighingCreateCampaignRequestDto,
            ): WeighingCampaignResponseDto {
                createCalls++
                return WeighingCampaignResponseDto(campaign = weighingCampaign(status = "draft"))
            }

            override suspend fun updateWeighingCampaign(
                campaignId: String,
                idempotencyKey: String,
                request: WeighingCreateCampaignRequestDto,
            ): WeighingCampaignResponseDto {
                updateCampaignId = campaignId
                return WeighingCampaignResponseDto(campaign = weighingCampaign(campaignId = campaignId, status = "draft"))
            }

            override suspend fun publishWeighingCampaign(campaignId: String, idempotencyKey: String): WeighingCampaignResponseDto {
                publishCampaignId = campaignId
                return WeighingCampaignResponseDto(campaign = weighingCampaign(campaignId = campaignId, status = "published"))
            }
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val result = repository.updatePlan("campaign-existing", planDraft()) as AppResult.Ok

        assertEquals(0, api.createCalls)
        assertEquals("campaign-existing", api.updateCampaignId)
        assertEquals("campaign-existing", api.publishCampaignId)
        assertEquals("Castro 1", result.value?.label)
    }

    @Test
    fun `operator assignments exclude canceled campaign sheds from backend spelling`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(): WeighingCampaignListResponseDto =
                WeighingCampaignListResponseDto(
                    items = listOf(
                        weighingCampaign(
                            status = "published",
                            sheds = listOf(
                                weighingShed("campaign-plan", "campaign-shed-live", "Castro 1", "pending"),
                                weighingShed("campaign-plan", "campaign-shed-canceled", "Castro 2", "canceled"),
                            ),
                        ),
                    ),
                )
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val result = repository.listAssignments() as AppResult.Ok

        assertEquals(listOf("Castro 1"), result.value.map { it.label })
    }

    @Test
    fun `wrong shed scan preserves expected and actual location labels`() = runTest {
        repository.replaceRoster(
            scopeKey,
            listOf(
                rosterRow(
                    animalId = "animal-1",
                    tag = "TAG-1",
                    expectedLocationId = "shed-expected",
                    expectedLocationLabel = "Gandhi 1",
                    actualLocationId = "shed-actual",
                    actualLocationLabel = "Mandela 2",
                ),
            ),
        )

        val match = repository.matchTag(scopeKey, "tag-1")

        assertEquals("wrong_shed", match.outcome)
        assertEquals("Gandhi 1", match.expectedLocationLabel)
        assertEquals("Mandela 2", match.actualLocationLabel)
    }

    @Test
    fun `individual animal observation is idempotent and proof gated`() = runTest {
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))

        val first = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2))
        val replay = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2))
        assertTrue(first is AppResult.Ok)
        assertTrue(replay is AppResult.Ok)
        assertEquals(
            (first as AppResult.Ok).value.observationId,
            (replay as AppResult.Ok).value.observationId,
        )
        assertFalse(first.value.readyToSubmit)

        repository.attachIndividualProof(scopeKey, "animal-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")
        val state = repository.observeScope(scopeKey, windowSize = 20).first()

        assertEquals(1, state.individualDrafts.size)
        assertTrue(state.individualDrafts.single().readyToSubmit)
        assertEquals(
            "weighing:individual:campaign-1:group-1:campaign-shed-1:animal-1:local-1",
            state.individualDrafts.single().idempotencyKey,
        )
    }

    @Test
    fun `correcting an editable individual draft creates a fresh idempotency key and cancels stale queued write`() = runTest {
        val store = FakeOutboxStore()
        repository = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            syncRepository = offlineSyncRepository(store),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))

        val first = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2)) as AppResult.Ok
        repository.attachIndividualProof(scopeKey, "animal-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")
        assertEquals(first.value.idempotencyKey, store.findByIdempotencyKey(first.value.idempotencyKey)?.idempotencyKey)

        val corrected = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 11.4)) as AppResult.Ok
        val state = repository.observeScope(scopeKey, windowSize = 20).first()

        assertEquals(null, store.findByIdempotencyKey(first.value.idempotencyKey))
        assertEquals(listOf(11.4), state.individualDrafts.map { it.weightKg })
        assertEquals("weighing:individual:campaign-1:group-1:campaign-shed-1:animal-1:local-2", corrected.value.idempotencyKey)
    }

    @Test
    fun `correcting an accepted individual keeps proof and queues weight revision`() = runTest {
        val store = FakeOutboxStore()
        repository = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            syncRepository = offlineSyncRepository(store),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))

        val first = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2)) as AppResult.Ok
        repository.attachIndividualProof(scopeKey, "animal-1", "proof-local-1", "proof-server-1")
        db.weighingObservationDao().markAcceptedByIdempotencyKey(
            first.value.idempotencyKey,
        )

        val corrected = repository.recordIndividual(
            individualCapture("animal-1", "TAG-1", weightKg = 11.4),
        ) as AppResult.Ok
        repository.attachIndividualProof(scopeKey, "animal-1", "proof-local-1", "proof-server-1")
        val state = repository.observeScope(scopeKey, windowSize = 20).first()
        val queued = store.findByIdempotencyKey(
            "${corrected.value.idempotencyKey}:proof:proof-server-1",
        )

        assertEquals(11.4, state.individualDrafts.single().weightKg, 0.0)
        assertEquals("proof-local-1", state.individualDrafts.single().proofCaptureId)
        assertTrue(queued?.payloadJson.orEmpty().contains("\"weight_kg\":11.4"))
        assertTrue(queued?.payloadJson.orEmpty().contains("\"proof_artifact_id\":\"proof-server-1\""))
    }

    @Test
    fun `synced proof after screen exit still enqueues individual observation`() = runTest {
        val store = FakeOutboxStore()
        val concrete = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            syncRepository = offlineSyncRepository(store),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
        repository = concrete
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))
        val draft = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2)) as AppResult.Ok
        repository.attachIndividualProof(scopeKey, "animal-1", proofCaptureId = "proof-local-1", serverProofId = null)
        db.proofCaptureDao().insert(syncedProof("proof-local-1", subjectId = "animal-1", serverProofId = "proof-server-1"))

        concrete.reconcileReadyProofsOnce()

        val queued = store.findByIdempotencyKey(draft.value.idempotencyKey)
        assertEquals(draft.value.idempotencyKey, queued?.idempotencyKey)
        assertTrue(queued?.payloadJson.orEmpty().contains("proof-server-1"))
    }

    @Test
    fun `synced proof after screen exit still enqueues shed partition observation`() = runTest {
        val store = FakeOutboxStore()
        val concrete = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            syncRepository = offlineSyncRepository(store),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
        repository = concrete
        val draft = repository.recordShedPartition(
            ShedPartitionWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-1",
                expectedLocationId = "shed-1",
                expectedLocationLabel = "Gandhi 1",
                resultJson = """{"weight": 180.5, "unit": "kg"}""",
            ),
        ) as AppResult.Ok
        repository.attachShedPartitionProof(scopeKey, proofCaptureId = "shed-proof-local", serverProofId = null)
        db.proofCaptureDao().insert(syncedProof("shed-proof-local", subjectId = "shed-1", serverProofId = "shed-proof-server"))

        concrete.reconcileReadyProofsOnce()

        val queued = store.findByIdempotencyKey(draft.value.idempotencyKey)
        assertEquals(draft.value.idempotencyKey, queued?.idempotencyKey)
        assertTrue(queued?.payloadJson.orEmpty().contains("shed-proof-server"))
        assertTrue(queued?.payloadJson.orEmpty().contains("180.5"))
    }

    @Test
    fun `successful weighing animal outbox dispatch marks local row accepted`() = runTest {
        val store = FakeOutboxStore()
        repository = DefaultWeighingRepository(
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            syncRepository = offlineSyncRepository(store),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))
        val draft = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 10.2)) as AppResult.Ok
        repository.attachIndividualProof(scopeKey, "animal-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")

        val engine = SyncEngine(
            store = store,
            api = FakeAppApi(),
            connectivityGate = { true },
            clock = { 2000L },
            weighingObservationDao = db.weighingObservationDao(),
            weighingShedObservationDao = db.weighingShedObservationDao(),
        )

        engine.drainOnce()

        val row = db.weighingObservationDao().findByIdempotencyKey(draft.value.idempotencyKey)
        assertEquals(WeighingSyncStatus.ACCEPTED.name, row?.syncStatus)
    }

    @Test
    fun `per shed partition observation does not create individual animal drafts`() = runTest {
        repository.replaceRoster(scopeKey, listOf(rosterRow(animalId = "animal-1", tag = "TAG-1")))

        val result = repository.recordShedPartition(
            ShedPartitionWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-1",
                expectedLocationId = "shed-1",
                expectedLocationLabel = "Gandhi 1",
                resultJson = """{"weight": 180.5, "unit": "kg"}""",
            ),
        )
        assertTrue(result is AppResult.Ok)
        repository.attachShedPartitionProof(scopeKey, proofCaptureId = "shed-proof-local", serverProofId = "shed-proof-server")

        val state = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(emptyList<IndividualWeighingDraft>(), state.individualDrafts)
        assertEquals(1, state.shedDrafts.size)
        assertTrue(state.shedDrafts.single().readyToSubmit)
        assertEquals("weighing:shed:campaign-1:group-1:campaign-shed-1:local-1", state.shedDrafts.single().idempotencyKey)
    }

    private fun offlineSyncRepository(store: FakeOutboxStore): SyncRepository {
        val engine = SyncEngine(store, FakeAppApi(), connectivityGate = { false }, clock = { 1000L })
        return DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = { false },
            appScope = appScope,
            clock = { 1000L },
        )
    }

    private fun individualCapture(animalId: String, tag: String, weightKg: Double) =
        IndividualWeighingCapture(
            tenantId = "tenant",
            campaignId = "campaign-1",
            workGroupId = "group-1",
            campaignShedId = "campaign-shed-1",
            animalId = animalId,
            scannedIdentifier = tag,
            weightKg = weightKg,
        )

    private fun planDraft() = WeighingPlanDraft(
        parkId = "park-cpt",
        periodStartDate = "2026-07-27",
        periodEndDate = "2026-08-02",
        startBusinessDate = "2026-07-27",
        plannedCapPerDay = 100,
        operatorUserId = "operator-amit",
        sheds = listOf(
            WeighingPlannerShed(locationId = "shed-castro-1", name = "Castro 1", kidCount = 80, category = "individual_animal"),
            WeighingPlannerShed(locationId = "shed-castro-2", name = "Castro 2", kidCount = 64, category = "per_shed_partition"),
        ),
    )

    private fun weighingCampaign(
        campaignId: String = "campaign-plan",
        status: String,
        sheds: List<WeighingCampaignShedDto> = listOf(
            weighingShed(campaignId, "campaign-shed-castro-1", "Castro 1", "pending", "individual_animal", 80),
            weighingShed(campaignId, "campaign-shed-castro-2", "Castro 2", "pending", "per_shed_partition", 1),
        ),
    ) = WeighingCampaignDto(
        campaignId = campaignId,
        tenantId = "tenant",
        parkId = "park-cpt",
        periodStartDate = "2026-07-27",
        periodEndDate = "2026-08-02",
        startBusinessDate = "2026-07-27",
        status = status,
        plannedCapPerDay = 100,
        operatorUserId = "operator-amit",
        createdBy = "ceo",
        sheds = sheds,
    )

    private fun weighingShed(
        campaignId: String,
        campaignShedId: String,
        displayName: String,
        status: String,
        category: String = "individual_animal",
        expectedCount: Int = 80,
    ) = WeighingCampaignShedDto(
        campaignShedId = campaignShedId,
        campaignId = campaignId,
        locationId = "shed-${displayName.lowercase().replace(" ", "-")}",
        locationType = "shed",
        displayName = displayName,
        expectedAnimalCount = expectedCount,
        weighingCategory = category,
        status = status,
    )

    private fun rosterRow(
        animalId: String,
        tag: String,
        seq: Long = 1L,
        expectedLocationId: String = "shed-1",
        expectedLocationLabel: String = "Gandhi 1",
        actualLocationId: String? = expectedLocationId,
        actualLocationLabel: String? = expectedLocationLabel,
    ) = WeighingRosterRowEntity(
        id = "row-$animalId",
        scopeKey = scopeKey,
        tenantId = "tenant",
        campaignId = "campaign-1",
        workGroupId = "group-1",
        campaignShedId = "campaign-shed-1",
        expectedLocationId = expectedLocationId,
        expectedLocationLabel = expectedLocationLabel,
        actualLocationId = actualLocationId,
        actualLocationLabel = actualLocationLabel,
        animalId = animalId,
        displayAnimalId = animalId,
        primaryTag = tag,
        secondaryTag = null,
        normalizedPrimaryTag = normalizeWeighingTag(tag),
        normalizedSecondaryTag = null,
        status = "pending",
        availabilityStatus = null,
        seq = seq,
        updatedAt = 1L,
    )

    private fun rosterDto(
        campaignId: String,
        campaignShedId: String,
        animalId: String,
        tag: String,
        seq: Long,
    ) = WeighingRosterRowDto(
        campaignId = campaignId,
        campaignShedId = campaignShedId,
        animalId = animalId,
        displayAnimalId = animalId,
        primaryIdentifier = tag,
        expectedLocationId = "shed-a",
        expectedLocationLabel = "Kid Shed A",
        currentLocationId = "shed-a",
        currentLocationLabel = "Kid Shed A",
        status = "pending",
        availabilityStatus = "expected_shed",
        seq = seq,
    )

    private fun campaignShed(
        campaignId: String,
        campaignShedId: String,
        displayName: String,
        category: String,
    ) = WeighingCampaignShedDto(
        campaignId = campaignId,
        campaignShedId = campaignShedId,
        locationId = "location-$campaignShedId",
        displayName = displayName,
        weighingCategory = category,
        status = "completed",
    )

    private fun syncedProof(id: String, subjectId: String, serverProofId: String) = ProofCaptureEntity(
        id = id,
        taskId = scopeKey,
        fieldKey = "weighing_individual_video",
        proofSubject = "goat",
        subjectId = subjectId,
        localUri = "/tmp/$id.mp4",
        mimeType = "video/mp4",
        capturedAtMs = 1000L,
        capturedStartMs = 1000L,
        capturedEndMs = 2000L,
        syncStatus = CaptureSyncStatus.SYNCED.name,
        idempotencyKey = "proof-key-$id",
        serverProofId = serverProofId,
    )

    private fun stableIds(): Sequence<String> = sequence {
        var index = 0
        while (true) yield("local-${++index}")
    }
}
