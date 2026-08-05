package sg.mesha.goatos.core.data.weighing

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.WeighingCampaignDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignListResponseDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignShedDto
import sg.mesha.goatos.core.network.dto.WeighingCampaignShedPageResponseDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedPageResponseDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosDto
import sg.mesha.goatos.core.network.dto.WeighingLeadershipShedVideosResponseDto
import sg.mesha.goatos.core.network.dto.WeighingObservationDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerCatalogResponseDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerOperatorDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerParkDto
import sg.mesha.goatos.core.network.dto.WeighingPlannerShedDto

/**
 * The Weighing LEADERSHIP reads as an offline-first data layer
 * (docs/decisions/android-offline-first.md).
 *
 * What is under test is the CONTRACT, not the plumbing: the screen model comes out of Room, the
 * network refresh only writes into Room, a second page appends without disturbing the first, a
 * failed refresh leaves the cached answer visible, and the shed read carries its own park/date/
 * assignee so a cold deep link needs no route arguments.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class WeighingLeadershipCacheTest {
    private lateinit var db: GoatDatabase

    @Before
    fun setUp() {
        db = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            GoatDatabase::class.java,
        ).allowMainThreadQueries().build()
    }

    @After
    fun tearDown() = db.close()

    private fun repository(api: AppApi): WeighingRepository = DefaultWeighingRepository(
        api = api,
        rosterDao = db.weighingRosterDao(),
        observationDao = db.weighingObservationDao(),
        shedObservationDao = db.weighingShedObservationDao(),
        database = db,
        clock = { 1_000L },
    )

    // --- L0 task list -------------------------------------------------------------------

    @Test
    fun `task list renders from Room and appends the next keyset page in server order`() = runTest {
        var calls = 0
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(
                scope: String?,
                cursor: String?,
                limit: Int,
                parkId: String?,
            ): WeighingCampaignListResponseDto {
                calls += 1
                // The client must ask for ONE screen-page, never the whole list.
                assertEquals(WEIGHING_LEADERSHIP_PAGE_SIZE, limit)
                return if (cursor == null) {
                    WeighingCampaignListResponseDto(
                        items = listOf(campaign("task-1"), campaign("task-2")),
                        counts = sg.mesha.goatos.core.network.dto.WeighingCampaignCountsDto(active = 9, completed = 4),
                        nextCursor = "cursor-2",
                    )
                } else {
                    WeighingCampaignListResponseDto(items = listOf(campaign("task-3")), nextCursor = null)
                }
            }
        }
        val repository = repository(api)

        assertEquals(AppResult.Ok(2), repository.refreshTaskList())
        val firstPage = repository.observeTaskList().first()
        assertEquals(listOf("task-1", "task-2"), firstPage.items.map { it.campaignId })
        // Whole-scope tallies come from the server, never from the cached page.
        assertEquals(9, firstPage.activeCount)
        assertEquals(4, firstPage.completedCount)
        assertTrue(firstPage.canLoadMore)

        assertEquals(AppResult.Ok(1), repository.refreshTaskList(reset = false))
        val appended = repository.observeTaskList().first()
        assertEquals(listOf("task-1", "task-2", "task-3"), appended.items.map { it.campaignId })
        assertFalse("last page must stop the scroll", appended.canLoadMore)
        assertEquals(2, calls)
    }

    @Test
    fun `a failed task refresh leaves the cached list visible`() = runTest {
        var fail = false
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(
                scope: String?,
                cursor: String?,
                limit: Int,
                parkId: String?,
            ): WeighingCampaignListResponseDto {
                if (fail) throw IllegalStateException("offline")
                return WeighingCampaignListResponseDto(items = listOf(campaign("task-1")))
            }
        }
        val repository = repository(api)
        repository.refreshTaskList()

        fail = true
        val result = repository.refreshTaskList()

        assertTrue(result is AppResult.Err)
        // Room keeps what it had: the operator sees the cached task, not a blank wall.
        assertEquals(listOf("task-1"), repository.observeTaskList().first().items.map { it.campaignId })
    }

    @Test
    fun `two task filters are independent cache scopes`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaigns(
                scope: String?,
                cursor: String?,
                limit: Int,
                parkId: String?,
            ) = WeighingCampaignListResponseDto(items = listOf(campaign("task-${parkId ?: "all"}")))
        }
        val repository = repository(api)
        repository.refreshTaskList(parkId = "park-a")
        repository.refreshTaskList(parkId = "park-b")

        assertEquals(
            listOf("task-park-a"),
            repository.observeTaskList(parkId = "park-a").first().items.map { it.campaignId },
        )
        assertEquals(
            listOf("task-park-b"),
            repository.observeTaskList(parkId = "park-b").first().items.map { it.campaignId },
        )
    }

    // --- L1 task detail -----------------------------------------------------------------

    @Test
    fun `task buckets page from their own read and keep the whole-task total`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaignSheds(
                campaignId: String,
                cursor: String?,
                limit: Int,
            ) = if (cursor == null) {
                WeighingCampaignShedPageResponseDto(
                    campaignId = campaignId,
                    items = listOf(bucket("bucket-1"), bucket("bucket-2")),
                    nextCursor = "cursor-2",
                    totalCount = 76,
                )
            } else {
                WeighingCampaignShedPageResponseDto(
                    campaignId = campaignId,
                    items = listOf(bucket("bucket-3")),
                    totalCount = 76,
                )
            }
        }
        val repository = repository(api)
        repository.refreshTaskBuckets("task-1")

        val first = repository.observeTaskBuckets("task-1").first()
        assertEquals(listOf("bucket-1", "bucket-2"), first.items.map { it.campaignShedId })
        // The header counts the whole task, not the page — so it does not move while scrolling.
        assertEquals(76, first.totalCount)
        assertTrue(first.canLoadMore)

        repository.refreshTaskBuckets("task-1", reset = false)
        val second = repository.observeTaskBuckets("task-1").first()
        assertEquals(listOf("bucket-1", "bucket-2", "bucket-3"), second.items.map { it.campaignShedId })
        assertEquals(76, second.totalCount)
    }

    /**
     * DEFECT (2026-08-04). Removing a shed from a campaign leaves its row behind with
     * status="canceled" rather than deleting it -- it is dead weight the backend still reports in
     * [WeighingCampaignShedPageResponseDto.totalCount]. The task-detail HEADER read this raw total
     * as-is ("4 shed buckets"), while the task card and the close-task action line both derive from
     * [WeighingTask.sheds], which already drops canceled rows ("3 shed buckets" / "3 not submitted").
     * One screen must not carry two different bucket counts: the cache the header reads must drop
     * canceled buckets the same way the task-record path already does.
     */
    @Test
    fun `a canceled bucket does not count toward the task's whole-task total`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingCampaignSheds(
                campaignId: String,
                cursor: String?,
                limit: Int,
            ) = WeighingCampaignShedPageResponseDto(
                campaignId = campaignId,
                items = listOf(
                    bucket("bucket-1"),
                    bucket("bucket-2"),
                    bucket("bucket-3"),
                    bucket("bucket-canceled").copy(status = "canceled"),
                ),
                nextCursor = null,
                // The raw backend total still counts the canceled row -- 4, not 3.
                totalCount = 4,
            )
        }
        val repository = repository(api)
        repository.refreshTaskBuckets("task-1")

        val cached = repository.observeTaskBuckets("task-1").first()
        // Card and action line agree on 3; the header-feeding cache must say the same.
        assertEquals(3, cached.totalCount)
        assertTrue(
            "a canceled bucket must never render as a live shed row",
            cached.items.none { it.campaignShedId == "bucket-canceled" },
        )
    }

    // --- L2 shed detail -----------------------------------------------------------------

    @Test
    fun `a cold shed read caches its own park date and assignee`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingLeadershipShedVideos(
                campaignId: String,
                campaignShedId: String,
                cursor: String?,
                limit: Int,
            ) = WeighingLeadershipShedVideosResponseDto(
                shed = WeighingLeadershipShedVideosDto(
                    campaignId = campaignId,
                    campaignShedId = campaignShedId,
                    shedName = "Gandhi 1",
                    parkName = "Channapatna",
                    weighDate = "2026-07-31",
                    operatorUserId = "user-1",
                    operatorDisplayName = "Amit Kumar",
                    weighingCategory = "individual_animal",
                    status = "in_progress",
                    individual = if (cursor == null) {
                        listOf(observation("obs-1"), observation("obs-2"))
                    } else {
                        listOf(observation("obs-3"))
                    },
                    nextIndividualCursor = if (cursor == null) "obs-cursor" else null,
                ),
            )
        }
        val repository = repository(api)
        repository.refreshLeadershipShed("task-1", "bucket-1")

        val cached = repository.observeLeadershipShed("task-1", "bucket-1").first()
        val shed = assertNotNull("the shed must render from Room", cached.shed).let { cached.shed!! }
        // These three are what a deep link previously had to receive as route arguments.
        assertEquals("Channapatna", shed.parkName)
        assertEquals("2026-07-31", shed.weighDate)
        assertEquals("Amit Kumar", shed.operatorDisplayName)
        assertEquals(listOf("obs-1", "obs-2"), shed.animals.map { it.observationId })
        assertTrue(cached.canLoadMoreRecords)

        repository.refreshLeadershipShed("task-1", "bucket-1", reset = false)
        val appended = repository.observeLeadershipShed("task-1", "bucket-1").first()
        assertEquals(listOf("obs-1", "obs-2", "obs-3"), appended.shed?.animals?.map { it.observationId })
        assertFalse(appended.canLoadMoreRecords)
    }

    // --- videos gallery -----------------------------------------------------------------

    @Test
    fun `the videos gallery renders cached buckets with their own captured records`() = runTest {
        var calls = 0
        val api = object : AppApi by FakeAppApi() {
            override suspend fun listWeighingLeadershipSheds(
                cursor: String?,
                limit: Int,
            ): WeighingLeadershipShedPageResponseDto {
                calls += 1
                val id = if (cursor == null) "bucket-of-task-1" else "bucket-of-task-2"
                return WeighingLeadershipShedPageResponseDto(
                    items = listOf(
                        WeighingLeadershipShedVideosDto(
                            campaignId = if (cursor == null) "task-1" else "task-2",
                            campaignShedId = id,
                            shedName = "Shed $id",
                            weighingCategory = "individual_animal",
                            status = "completed",
                            individual = listOf(observation("obs-$id-1"), observation("obs-$id-2")),
                        ),
                    ),
                    nextCursor = if (cursor == null) "cursor-2" else null,
                )
            }
        }
        val repository = repository(api)
        repository.refreshLeadershipVideos()

        val first = repository.observeLeadershipVideos().first()
        assertEquals(listOf("bucket-of-task-1"), first.map { it.campaignShedId })
        // The gallery renders a bucket's captured animals, so the cached records must come with it
        // — bounded per shed, not as one unbounded read across the whole gallery page.
        assertEquals(
            listOf("obs-bucket-of-task-1-1", "obs-bucket-of-task-1-2"),
            first.single().animals.map { it.observationId },
        )

        repository.refreshLeadershipVideos(reset = false)
        val second = repository.observeLeadershipVideos().first()
        assertEquals(listOf("bucket-of-task-1", "bucket-of-task-2"), second.map { it.campaignShedId })
        assertEquals(
            listOf("obs-bucket-of-task-2-1", "obs-bucket-of-task-2-2"),
            second.last().animals.map { it.observationId },
        )
        // ONE request per page — never one per bucket.
        assertEquals(2, calls)
    }

    @Test
    fun `a gallery refresh keeps a bucket paged deep on its own screen and does not rewind it`() = runTest {
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingLeadershipShedVideos(
                campaignId: String,
                campaignShedId: String,
                cursor: String?,
                limit: Int,
            ) = WeighingLeadershipShedVideosResponseDto(
                shed = WeighingLeadershipShedVideosDto(
                    campaignId = campaignId,
                    campaignShedId = campaignShedId,
                    shedName = "Shed $campaignShedId",
                    weighingCategory = "individual_animal",
                    status = "completed",
                    individual = if (cursor == null) {
                        listOf(observation("obs-1"), observation("obs-2"))
                    } else {
                        listOf(observation("obs-3"))
                    },
                    nextIndividualCursor = if (cursor == null) "records-2" else null,
                ),
            )

            override suspend fun listWeighingLeadershipSheds(cursor: String?, limit: Int) =
                WeighingLeadershipShedPageResponseDto(
                    items = listOf(
                        WeighingLeadershipShedVideosDto(
                            campaignId = "task-1",
                            campaignShedId = "bucket-1",
                            shedName = "Shed bucket-1",
                            weighingCategory = "individual_animal",
                            status = "completed",
                            // The gallery always carries page 1 of each bucket.
                            individual = listOf(observation("obs-1"), observation("obs-2")),
                            nextIndividualCursor = "records-2",
                        ),
                    ),
                )
        }
        val repository = repository(api)
        // The reader pages this bucket deep on its own detail screen.
        repository.refreshLeadershipShed("task-1", "bucket-1")
        repository.refreshLeadershipShed("task-1", "bucket-1", reset = false)
        val deep = repository.observeLeadershipShed("task-1", "bucket-1").first()
        assertEquals(listOf("obs-1", "obs-2", "obs-3"), deep.shed?.animals?.map { it.observationId })
        assertFalse(deep.canLoadMoreRecords)

        // Opening the gallery must not truncate that bucket back to page 1 or rewind its cursor:
        // both surfaces share one record set.
        repository.refreshLeadershipVideos()
        val afterGallery = repository.observeLeadershipShed("task-1", "bucket-1").first()
        assertEquals(listOf("obs-1", "obs-2", "obs-3"), afterGallery.shed?.animals?.map { it.observationId })
        assertFalse(afterGallery.canLoadMoreRecords)
    }

    // --- planner catalog ----------------------------------------------------------------

    @Test
    fun `every park is offered no matter how many sheds the biggest park holds`() = runTest {
        // The defect this replaces: the catalog was ONE flattened keyset page over (park, shed)
        // pairs capped at 20, so a 76-shed park filled page one by itself and the park step offered
        // a single park. A picker cannot page -- it cannot offer what it has not reached.
        val api = object : AppApi by FakeAppApi() {
            override suspend fun getWeighingPlannerCatalog(periodStartDate: String) =
                WeighingPlannerCatalogResponseDto(
                    parks = listOf(
                        park("park-big", shedCount = 76),
                        park("park-cbe", shedCount = 4),
                        park("park-cpt", shedCount = 4),
                    ),
                    operators = listOf(WeighingPlannerOperatorDto(userId = "user-1", displayName = "Amit Kumar")),
                )
        }
        val repository = repository(api)
        repository.refreshPlannerCatalog("2026-08-03")

        val cached = repository.observePlannerCatalog("2026-08-03").first()
        assertEquals(listOf("park-big", "park-cbe", "park-cpt"), cached.catalog.parks.map { it.parkId })
        // The shed COUNT is the park's own total, never a count of rows that happened to be fetched.
        assertEquals(76, cached.catalog.parks.first().shedCount)
        assertEquals(listOf("user-1"), cached.catalog.operators.map { it.userId })
    }

    private fun campaign(id: String) = WeighingCampaignDto(
        campaignId = id,
        parkId = "park-1",
        parkName = "Channapatna",
        startBusinessDate = "2026-07-31",
        status = "published",
        sheds = listOf(bucket("bucket-of-$id")),
    )

    private fun bucket(id: String) = WeighingCampaignShedDto(
        campaignShedId = id,
        campaignId = "task-1",
        locationId = "loc-$id",
        displayName = "Shed $id",
        weighingCategory = "individual_animal",
        status = "in_progress",
    )

    private fun observation(id: String) = WeighingObservationDto(
        observationId = id,
        campaignId = "task-1",
        campaignShedId = "bucket-1",
        scannedIdentifier = "rfid-$id",
        weightKg = 12.5,
        acceptedAt = "2026-07-31T10:00:00Z",
    )

    private fun park(parkId: String, shedCount: Int) = WeighingPlannerParkDto(
        parkId = parkId,
        name = parkId,
        shedCount = shedCount,
    )
}
