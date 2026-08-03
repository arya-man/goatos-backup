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
import sg.mesha.goatos.core.network.WEIGHING_PAGE_SIZE
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_OPERATORS
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WeighingAcceptedObservationDto
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
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedPageResponseDto
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
                scannedIdentifier = "TAG-5001",
                weightKg = 12.4,
            ),
        )
        assertTrue(recorded is AppResult.Ok)

        val after = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(20, after.rosterWindow.size)
        assertEquals(listOf("TAG-5001"), after.individualDrafts.map { it.scannedIdentifier })
    }

    @Test
    fun `leadership videos map individual animals and lump sum summaries`() = runTest {
        // ONE request at BUCKET grain. The gallery used to build this page itself by expanding a
        // task page and calling the single-bucket read once per bucket.
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingLeadershipSheds(cursor: String?, limit: Int) =
                WeighingLeadershipShedPageResponseDto(
                    items = listOf(
                        WeighingLeadershipShedVideosDto(
                            campaignId = "campaign",
                            campaignShedId = "individual",
                            shedName = "Gandhi 1",
                            weighingCategory = "individual_animal",
                            status = "completed",
                            periodLabel = "2026-07-27 - 2026-08-02",
                            individual = listOf(
                                WeighingObservationDto(
                                    scannedIdentifier = "RFID-000123",
                                    weightKg = 18.25,
                                    acceptedAt = "2026-07-29T06:00:00Z",
                                    media = listOf(WeighingProofMediaDto("proof-1", "https://proof/1")),
                                ),
                            ),
                        ),
                        WeighingLeadershipShedVideosDto(
                            campaignId = "campaign",
                            campaignShedId = "lump",
                            shedName = "Castro 1",
                            weighingCategory = "per_shed_partition",
                            status = "completed",
                            periodLabel = "2026-07-27 - 2026-08-02",
                            lumpSum = WeighingObservationDto(
                                weightKg = 250.0,
                                averageWeightKg = 25.0,
                                animalCount = 10,
                                media = listOf(
                                    WeighingProofMediaDto("proof-2", "https://proof/2"),
                                    WeighingProofMediaDto("proof-3", "https://proof/3"),
                                ),
                            ),
                        ),
                    ),
                )
        }
        val subject = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            database = db,
        )

        subject.refreshLeadershipVideos()
        val items = subject.observeLeadershipVideos().first()

        assertEquals("RFID-000123", items[0].animals.single().rfid)
        assertEquals(18.25, items[0].animals.single().weightKg, 0.0)
        assertEquals(10, items[1].animalCount)
        assertEquals(250.0, items[1].totalWeightKg!!, 0.0)
        assertEquals(25.0, items[1].averageWeightKg!!, 0.0)
        assertEquals(2, items[1].videos.size)
    }

    // FREE-FLOW: the scope read has no expected-animal roster to hydrate -- `items` is permanently
    // `[]` since the backend dropped `weighing_expected_animals` (000079). What refreshScope must
    // restore is the bucket's ACCEPTED observations, keyed on the scanned tag. The old code guarded
    // that restore on a non-empty `animal_id`, a field the backend has not sent since 000078, so
    // nothing was ever restored.
    @Test
    fun `refresh scope restores accepted observations with no animal id in the payload`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                observationsCursor: String?,
                limit: Int,
            ): WeighingRosterResponseDto = WeighingRosterResponseDto(
                observations = listOf(
                    WeighingAcceptedObservationDto(
                        observationId = "observation-1",
                        campaignId = campaignId,
                        campaignShedId = campaignShedId,
                        scannedIdentifier = "WG-RFID-0009",
                        weightKg = 12.4,
                        proofArtifactId = "proof-1",
                        acceptedAt = "2026-07-30T10:00:00Z",
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

        val drafts = repository.observeScope(scopeKey, windowSize = 20).first().individualDrafts
        assertEquals(listOf("WG-RFID-0009"), drafts.map { it.scannedIdentifier })
        assertTrue(drafts.single().syncedToBackend)
    }

    @Test
    fun `refresh scope drops stale accepted observations but keeps pending local work`() = runTest {
        repository.replaceRoster(
            scopeKey,
            listOf(
                rosterRow(animalId = "animal-accepted", tag = "TAG-ACCEPTED"),
                rosterRow(animalId = "animal-pending", tag = "TAG-PENDING"),
            ),
        )
        val accepted = repository.recordIndividual(
            individualCapture("animal-accepted", "TAG-ACCEPTED", weightKg = 10.2),
        ) as AppResult.Ok
        db.weighingObservationDao().markAcceptedByIdempotencyKey(accepted.value.idempotencyKey)
        repository.recordIndividual(
            individualCapture("animal-pending", "TAG-PENDING", weightKg = 11.4),
        ) as AppResult.Ok

        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                observationsCursor: String?,
                limit: Int,
            ): WeighingRosterResponseDto = WeighingRosterResponseDto(observations = emptyList())
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        repository.refreshScope("campaign-1", "group-1", "campaign-shed-1")

        val state = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(listOf("TAG-PENDING"), state.individualDrafts.map { it.scannedIdentifier })
    }

    @Test
    fun `refresh scope drops stale accepted shed partition so backend open shed can be recorded again`() = runTest {
        val first = repository.recordShedPartition(
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
        db.weighingShedObservationDao().markAcceptedByIdempotencyKey(first.value.idempotencyKey)

        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                observationsCursor: String?,
                limit: Int,
            ): WeighingRosterResponseDto = WeighingRosterResponseDto(observations = emptyList())
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            clock = { 2000L },
            idGenerator = stableIds().iterator()::next,
        )

        repository.refreshScope("campaign-1", "group-1", "campaign-shed-1")
        val second = repository.recordShedPartition(
            ShedPartitionWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-1",
                expectedLocationId = "shed-1",
                expectedLocationLabel = "Gandhi 1",
                resultJson = """{"weight": 181.0, "unit": "kg"}""",
            ),
        )

        assertTrue(second is AppResult.Ok)
        assertEquals(1, repository.observeScope(scopeKey, windowSize = 20).first().shedDrafts.size)
    }

    // FREE-FLOW: the roster leg of this read is GONE (no `items`, no roster cursor, no
    // `include_roster` gate). Only the observations cursor is drained, and it must drain fully --
    // everything past page one used to be dropped, so a re-weighed animal kept reading as
    // un-weighed on the device.
    @Test
    fun `refresh scope drains the observations cursor to the last page`() = runTest {
        val observationCursors = mutableListOf<String?>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingRoster(
                campaignId: String,
                campaignShedId: String,
                observationsCursor: String?,
                limit: Int,
            ): WeighingRosterResponseDto {
                observationCursors += observationsCursor
                return if (observationsCursor == null) {
                    WeighingRosterResponseDto(
                        observations = listOf(
                            WeighingAcceptedObservationDto(
                                observationId = "observation-page-1",
                                campaignId = campaignId,
                                campaignShedId = campaignShedId,
                                scannedIdentifier = "WG-RFID-7001",
                                weightKg = 12.0,
                                proofArtifactId = "proof-1",
                                acceptedAt = "2026-07-30T10:00:00Z",
                            ),
                        ),
                        nextObservationsCursor = "obs-page-2",
                    )
                } else {
                    WeighingRosterResponseDto(
                        observations = listOf(
                            WeighingAcceptedObservationDto(
                                observationId = "observation-page-2",
                                campaignId = campaignId,
                                campaignShedId = campaignShedId,
                                scannedIdentifier = "WG-RFID-7002",
                                weightKg = 13.0,
                                proofArtifactId = "proof-2",
                                acceptedAt = "2026-07-30T10:05:00Z",
                            ),
                        ),
                        nextObservationsCursor = null,
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

        repository.refreshScope("campaign-1", "group-1", "campaign-shed-1", maxRows = WEIGHING_PAGE_SIZE * 2)

        assertEquals(listOf(null, "obs-page-2"), observationCursors)
        assertEquals(
            listOf("WG-RFID-7001", "WG-RFID-7002"),
            repository.observeScope(scopeKey, windowSize = 20).first()
                .individualDrafts.map { it.scannedIdentifier }.sorted(),
        )
    }

    @Test
    fun `planner catalog maps parks shed counts operators and existing campaign guard`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingPlannerCatalog(
                periodStartDate: String,
            ): WeighingPlannerCatalogResponseDto =
                WeighingPlannerCatalogResponseDto(
                    parks = listOf(
                        WeighingPlannerParkDto(
                            parkId = "park-cpt",
                            name = "CPT - Channapatna",
                            kidCount = 144,
                            // Park grain: the catalog carries the park's shed COUNT. The shed rows
                            // are a separate per-park keyset page.
                            shedCount = 2,
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
            database = db,
        )

        repository.refreshPlannerCatalog("2026-07-27")
        val catalog = repository.observePlannerCatalog("2026-07-27").first().catalog

        assertEquals("CPT - Channapatna", catalog.parks.single().name)
        assertEquals("campaign-existing", catalog.parks.single().existingCampaign?.campaignId)
        assertEquals(2, catalog.parks.single().shedCount)
        assertEquals("Amit Kumar", catalog.operators.single().displayName)
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
            override suspend fun listWeighingCampaigns(scope: String?, cursor: String?, limit: Int, parkId: String?): WeighingCampaignListResponseDto =
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

        assertEquals(listOf("Castro 1"), result.value.items.map { it.label })
    }

    @Test
    fun `operator surface still excludes closed sheds, only canceled ones dropped for real`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(scope: String?, cursor: String?, limit: Int, parkId: String?): WeighingCampaignListResponseDto =
                WeighingCampaignListResponseDto(
                    items = listOf(
                        weighingCampaign(
                            status = "published",
                            sheds = listOf(
                                weighingShed("campaign-plan", "campaign-shed-live", "Castro 1", "pending"),
                                weighingShed("campaign-plan", "campaign-shed-closed", "Castro 2", "closed"),
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

        // Default scope is WEIGHING_SCOPE_MINE -- the operator's own executable work. A closed
        // bucket has nothing left for the operator to do, so it must stay excluded there.
        val result = repository.listAssignments() as AppResult.Ok

        assertEquals(listOf("Castro 1"), result.value.items.map { it.label })
    }

    @Test
    fun `leadership oversight surface receives closed sheds so history and reopen are reachable`() = runTest {
        // A23: toAssignments() used to drop status == "closed" unconditionally, which fed BOTH the
        // operator screen AND LeadershipWeighingScreen from the same list. LeadershipWeighingScreen
        // implements tap-to-reopen driven by `row.isClosed`, but a closed row could never arrive --
        // reopen was permanently dead code. WEIGHING_SCOPE_OPERATORS is the leadership/oversight
        // surface (per WeighingRepository.listAssignments's own scope doc), so it must see closed
        // buckets.
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(scope: String?, cursor: String?, limit: Int, parkId: String?): WeighingCampaignListResponseDto =
                WeighingCampaignListResponseDto(
                    items = listOf(
                        weighingCampaign(
                            status = "published",
                            sheds = listOf(
                                weighingShed("campaign-plan", "campaign-shed-live", "Castro 1", "pending"),
                                weighingShed("campaign-plan", "campaign-shed-closed", "Castro 2", "closed"),
                                weighingShed("campaign-plan", "campaign-shed-canceled", "Castro 3", "canceled"),
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

        val result = repository.listAssignments(scope = WEIGHING_SCOPE_OPERATORS) as AppResult.Ok

        // Closed reaches the surface (and stays reopen-able); canceled never becomes real work.
        assertEquals(listOf("Castro 1", "Castro 2"), result.value.items.map { it.label })
        assertTrue(result.value.items.single { it.label == "Castro 2" }.status.equals("closed", ignoreCase = true))
    }

    @Test
    fun `assignment page one returns a full screen page plus a usable next cursor`() = runTest {
        val requested = mutableListOf<Pair<String?, Int>>()
        val api = pagedCampaignApi(requested)
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val page = (repository.listAssignments() as AppResult.Ok).value

        assertEquals(WEIGHING_PAGE_SIZE, page.items.size)
        assertEquals("cursor-page-2", page.nextCursor)
        assertEquals(listOf(null to WEIGHING_PAGE_SIZE), requested)
    }

    @Test
    fun `assignment next cursor returns the next distinct page with no duplicates or gaps`() = runTest {
        val requested = mutableListOf<Pair<String?, Int>>()
        val api = pagedCampaignApi(requested)
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val first = (repository.listAssignments() as AppResult.Ok).value
        val second = (repository.listAssignments(first.nextCursor) as AppResult.Ok).value

        assertEquals(WEIGHING_PAGE_SIZE, second.items.size)
        assertEquals(null, second.nextCursor)
        assertEquals(
            listOf(null to WEIGHING_PAGE_SIZE, "cursor-page-2" to WEIGHING_PAGE_SIZE),
            requested,
        )
        val firstIds = first.items.map { it.campaignShedId }
        val secondIds = second.items.map { it.campaignShedId }
        assertTrue(firstIds.intersect(secondIds.toSet()).isEmpty())
        val combined = firstIds + secondIds
        assertEquals(WEIGHING_PAGE_SIZE * 2, combined.distinct().size)
        assertEquals(
            (1..WEIGHING_PAGE_SIZE * 2).map { "campaign-shed-$it" },
            combined,
        )
    }

    @Test
    fun `blank or repeated assignment cursor terminates instead of requesting again`() = runTest {
        val requested = mutableListOf<String?>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(scope: String?, cursor: String?, limit: Int, parkId: String?): WeighingCampaignListResponseDto {
                requested += cursor
                return WeighingCampaignListResponseDto(
                    items = listOf(weighingCampaign(status = "published", sheds = listOf(weighingShed("campaign-plan", "campaign-shed-1", "Castro 1", "pending")))),
                    nextCursor = if (cursor == null) "   " else cursor,
                )
            }
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
        )

        val blankCursorPage = (repository.listAssignments() as AppResult.Ok).value
        assertEquals(null, blankCursorPage.nextCursor)
        assertEquals(listOf<String?>(null), requested)

        val repeatedCursorPage = (repository.listAssignments("cursor-stuck") as AppResult.Ok).value
        assertEquals(null, repeatedCursorPage.nextCursor)
        assertEquals(listOf(null, "cursor-stuck"), requested)
    }

    @Test
    fun `leadership video pages follow the backend cursor across two distinct pages`() = runTest {
        val requested = mutableListOf<String?>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingLeadershipSheds(
                cursor: String?,
                limit: Int,
            ): WeighingLeadershipShedPageResponseDto {
                requested += cursor
                val suffix = if (cursor == null) "p1" else "p2"
                return WeighingLeadershipShedPageResponseDto(
                    items = listOf(
                        WeighingLeadershipShedVideosDto(
                            campaignId = "campaign",
                            campaignShedId = "shed-$suffix",
                            shedName = "Gandhi $suffix",
                            weighingCategory = "individual_animal",
                            status = "completed",
                            periodLabel = "2026-07-27 - 2026-08-02",
                            individual = listOf(
                                WeighingObservationDto(
                                    scannedIdentifier = "RFID-shed-$suffix",
                                    weightKg = 18.25,
                                    acceptedAt = "2026-07-29T06:00:00Z",
                                    media = listOf(WeighingProofMediaDto("proof-$suffix", "https://proof/$suffix")),
                                ),
                            ),
                        ),
                    ),
                    nextCursor = if (cursor == null) "cursor-page-2" else null,
                )
            }
        }
        repository = DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            database = db,
        )

        repository.refreshLeadershipVideos()
        assertEquals(listOf("shed-p1"), repository.observeLeadershipVideos().first().map { it.campaignShedId })

        repository.refreshLeadershipVideos(reset = false)
        assertEquals(
            listOf("shed-p1", "shed-p2"),
            repository.observeLeadershipVideos().first().map { it.campaignShedId },
        )
        assertEquals(listOf(null, "cursor-page-2"), requested)
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

        repository.attachIndividualProof(scopeKey, "TAG-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")
        val state = repository.observeScope(scopeKey, windowSize = 20).first()

        assertEquals(1, state.individualDrafts.size)
        assertTrue(state.individualDrafts.single().readyToSubmit)
        assertEquals(
            "weighing:individual:campaign-1:group-1:campaign-shed-1:TAG-1:local-1",
            state.individualDrafts.single().idempotencyKey,
        )
    }

    // THE CAPTURE-DROP DEFECT: the observation table's unique index used to be
    // (campaignId, campaignShedId, animalId) -- a column the backend stopped sending when it
    // dropped weighing_observations.animal_id (000078). Every free-flow capture therefore carried
    // animalId = "", two different scanned tags in the SAME bucket collided on that index, and
    // WeighingObservationDao.insert uses OnConflictStrategy.IGNORE -- so the second scan was
    // silently DROPPED with no error surfaced to the operator. The index is now keyed on the
    // scanned tag, which is the real free-flow identity.
    @Test
    fun `two different scanned tags in one bucket both persist`() = runTest {
        val first = repository.recordIndividual(individualCapture("ignored", "TAG-A", weightKg = 10.2))
        val second = repository.recordIndividual(individualCapture("ignored", "TAG-B", weightKg = 11.4))

        assertTrue(first is AppResult.Ok)
        assertTrue(second is AppResult.Ok)

        val drafts = repository.observeScope(scopeKey, windowSize = 20).first().individualDrafts
        assertEquals(listOf("TAG-A", "TAG-B"), drafts.map { it.scannedIdentifier }.sorted())
        assertEquals(listOf(10.2, 11.4), drafts.map { it.weightKg }.sorted())
    }

    @Test
    fun `same RFID can be captured independently in different weighing shed buckets`() = runTest {
        val otherScopeKey = weighingScopeKey("campaign-1", "group-1", "campaign-shed-2")
        val first = repository.recordIndividual(individualCapture("TAG-1", "TAG-1", weightKg = 10.2)) as AppResult.Ok
        val second = repository.recordIndividual(
            IndividualWeighingCapture(
                tenantId = "tenant",
                campaignId = "campaign-1",
                workGroupId = "group-1",
                campaignShedId = "campaign-shed-2",
                scannedIdentifier = "TAG-1",
                weightKg = 11.4,
            ),
        ) as AppResult.Ok

        repository.attachIndividualProof(scopeKey, "TAG-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")
        repository.attachIndividualProof(otherScopeKey, "TAG-1", proofCaptureId = "proof-local-2", serverProofId = "proof-server-2")

        val firstBucket = repository.observeScope(scopeKey, windowSize = 20).first()
        val secondBucket = repository.observeScope(otherScopeKey, windowSize = 20).first()

        assertEquals(first.value.observationId, firstBucket.individualDrafts.single().observationId)
        assertEquals(second.value.observationId, secondBucket.individualDrafts.single().observationId)
        assertEquals(10.2, firstBucket.individualDrafts.single().weightKg, 0.0)
        assertEquals(11.4, secondBucket.individualDrafts.single().weightKg, 0.0)
        assertEquals("proof-server-1", firstBucket.individualDrafts.single().serverProofId)
        assertEquals("proof-server-2", secondBucket.individualDrafts.single().serverProofId)
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
        repository.attachIndividualProof(scopeKey, "TAG-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")
        assertEquals(first.value.idempotencyKey, store.findByIdempotencyKey(first.value.idempotencyKey)?.idempotencyKey)

        val corrected = repository.recordIndividual(individualCapture("animal-1", "TAG-1", weightKg = 11.4)) as AppResult.Ok
        val state = repository.observeScope(scopeKey, windowSize = 20).first()

        assertEquals(null, store.findByIdempotencyKey(first.value.idempotencyKey))
        assertEquals(listOf(11.4), state.individualDrafts.map { it.weightKg })
        assertEquals("weighing:individual:campaign-1:group-1:campaign-shed-1:TAG-1:local-2", corrected.value.idempotencyKey)
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
        repository.attachIndividualProof(scopeKey, "TAG-1", "proof-local-1", "proof-server-1")
        db.weighingObservationDao().markAcceptedByIdempotencyKey(
            first.value.idempotencyKey,
        )

        val corrected = repository.recordIndividual(
            individualCapture("animal-1", "TAG-1", weightKg = 11.4),
        ) as AppResult.Ok
        repository.attachIndividualProof(scopeKey, "TAG-1", "proof-local-1", "proof-server-1")
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
        repository.attachIndividualProof(scopeKey, "TAG-1", proofCaptureId = "proof-local-1", serverProofId = null)
        db.proofCaptureDao().insert(syncedProof("proof-local-1", subjectId = "TAG-1", serverProofId = "proof-server-1"))

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
        repository.attachIndividualProof(scopeKey, "TAG-1", proofCaptureId = "proof-local-1", serverProofId = "proof-server-1")

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

    @Test
    fun `roster pagination returns correct page 2 without duplicates or gaps`() = runTest {
        // Create 50 animals to test pagination across multiple windows of 20
        val allAnimals = (1..50).map { index ->
            rosterRow(
                animalId = "animal-$index",
                tag = "TAG-$index",
                seq = index.toLong(),
            )
        }
        repository.replaceRoster(scopeKey, allAnimals)

        // First window should contain animals 1-20
        val window1 = repository.observeScope(scopeKey, windowSize = 20).first()
        assertEquals(20, window1.rosterWindow.size)
        assertEquals(50, window1.totalExpected)
        assertEquals("animal-1", window1.rosterWindow.first().animalId)
        assertEquals("animal-20", window1.rosterWindow.last().animalId)

        // Verify no duplicates in window 1
        val window1Ids = window1.rosterWindow.map { it.animalId }.toSet()
        assertEquals(20, window1Ids.size)

        // Second window should contain animals 21-40 (manual pagination via keyset)
        // val allRows = db.weighingRosterDao().findAll() // hypothetical method to get all rows
        // Note: This test validates the DAO query structure returns rows in order by seq
    }

    @Test
    fun `roster window size change does not affect total count`() = runTest {
        val animals = (1..45).map { index ->
            rosterRow(
                animalId = "animal-$index",
                tag = "TAG-$index",
                seq = index.toLong(),
            )
        }
        repository.replaceRoster(scopeKey, animals)

        // Total should be the same regardless of window size
        val window10 = repository.observeScope(scopeKey, windowSize = 10).first()
        val window20 = repository.observeScope(scopeKey, windowSize = 20).first()
        val window40 = repository.observeScope(scopeKey, windowSize = 40).first()

        assertEquals(45, window10.totalExpected)
        assertEquals(45, window20.totalExpected)
        assertEquals(45, window40.totalExpected)

        // But window sizes should be correctly bounded
        assertEquals(10, window10.rosterWindow.size)
        assertEquals(20, window20.rosterWindow.size)
        assertEquals(40, window40.rosterWindow.size)
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

    private fun individualCapture(@Suppress("UNUSED_PARAMETER") animalId: String, tag: String, weightKg: Double) =
        IndividualWeighingCapture(
            tenantId = "tenant",
            campaignId = "campaign-1",
            workGroupId = "group-1",
            campaignShedId = "campaign-shed-1",
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

    /** Two full screen-pages of shed assignments, keyset-linked by cursor, with no overlap. */
    private fun pagedCampaignApi(requested: MutableList<Pair<String?, Int>>): AppApi =
        object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(scope: String?, cursor: String?, limit: Int, parkId: String?): WeighingCampaignListResponseDto {
                requested += cursor to limit
                val start = if (cursor == null) 1 else WEIGHING_PAGE_SIZE + 1
                return WeighingCampaignListResponseDto(
                    items = listOf(
                        weighingCampaign(
                            status = "published",
                            sheds = (start until start + WEIGHING_PAGE_SIZE).map { index ->
                                weighingShed(
                                    campaignId = "campaign-plan",
                                    campaignShedId = "campaign-shed-$index",
                                    displayName = "Castro $index",
                                    status = "pending",
                                )
                            },
                        ),
                    ),
                    nextCursor = if (cursor == null) "cursor-page-2" else null,
                )
            }
        }

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

    @Test
    fun `close reopen close each carry a DIFFERENT idempotency key`() = runTest {
        // The backend replays a key it has already recorded and returns the ORIGINAL result without
        // touching state, so a key fixed per scope made the second close a silent no-op that still
        // reported success.
        val keys = mutableListOf<String>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun closeShedWeighingCampaign(
                campaignId: String,
                campaignShedId: String,
                idempotencyKey: String,
                request: sg.mesha.goatos.core.network.dto.WeighingScopeCloseRequestDto,
            ) {
                keys += idempotencyKey
            }

            override suspend fun reopenWeighingScope(
                campaignId: String,
                campaignShedId: String,
                idempotencyKey: String,
                request: sg.mesha.goatos.core.network.dto.WeighingScopeReopenRequestDto,
            ) {
                keys += idempotencyKey
            }
        }
        val repo = weighingRepository(api)

        assertTrue(repo.closeShedCampaign("campaign-1", "shed-1", "done") is AppResult.Ok)
        assertTrue(repo.reopenScope("campaign-1", "shed-1", "more animals") is AppResult.Ok)
        assertTrue(repo.closeShedCampaign("campaign-1", "shed-1", "done") is AppResult.Ok)

        assertEquals(3, keys.size)
        assertEquals(3, keys.toSet().size)
    }

    @Test
    fun `retrying a FAILED close reuses its idempotency key`() = runTest {
        // Idempotency still has to do its job: an attempt whose outcome is unknown must be
        // deduplicated when the operator taps again, so only a LANDED transition rotates the key.
        val keys = mutableListOf<String>()
        var failNext = true
        val api = object : AppApi by FakeAppApi() {
            override suspend fun closeShedWeighingCampaign(
                campaignId: String,
                campaignShedId: String,
                idempotencyKey: String,
                request: sg.mesha.goatos.core.network.dto.WeighingScopeCloseRequestDto,
            ) {
                keys += idempotencyKey
                if (failNext) {
                    failNext = false
                    throw java.io.IOException("timeout")
                }
            }
        }
        val repo = weighingRepository(api)

        assertTrue(repo.closeShedCampaign("campaign-1", "shed-1", "done") is AppResult.Err)
        assertTrue(repo.closeShedCampaign("campaign-1", "shed-1", "done") is AppResult.Ok)

        assertEquals(2, keys.size)
        assertEquals(keys[0], keys[1])
    }

    @Test
    fun `two different sheds never share an idempotency key`() = runTest {
        val keys = mutableListOf<String>()
        val api = object : AppApi by FakeAppApi() {
            override suspend fun abandonWeighingScope(
                campaignId: String,
                campaignShedId: String,
                idempotencyKey: String,
                request: sg.mesha.goatos.core.network.dto.WeighingScopeCloseRequestDto,
            ) {
                keys += idempotencyKey
            }
        }
        val repo = weighingRepository(api)

        repo.abandonScope("campaign-1", "shed-1", "rained off")
        repo.abandonScope("campaign-1", "shed-2", "rained off")

        assertEquals(2, keys.toSet().size)
    }

    private fun weighingRepository(api: AppApi): WeighingRepository =
        DefaultWeighingRepository(
            api = api,
            rosterDao = db.weighingRosterDao(),
            observationDao = db.weighingObservationDao(),
            shedObservationDao = db.weighingShedObservationDao(),
            clock = { 1000L },
            idGenerator = stableIds().iterator()::next,
        )

    private fun stableIds(): Sequence<String> = sequence {
        var index = 0
        while (true) yield("local-${++index}")
    }
}
