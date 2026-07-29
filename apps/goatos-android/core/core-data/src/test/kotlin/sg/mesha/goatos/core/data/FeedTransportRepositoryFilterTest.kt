package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionDto
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionsDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FeedTransportRepositoryFilterTest {

    private data class Request(
        val date: String,
        val parkId: String?,
        val shedId: String?,
        val status: String?,
        val limit: Int?,
    )

    @Test
    fun `date farm shed and status are server filtered and cached by exact scope`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val api = object : AppApi by FakeAppApi() {
                override suspend fun getFeedTransportTasks(
                    businessDate: String,
                    parkId: String?,
                    shedId: String?,
                    status: String?,
                    cursor: String?,
                    limit: Int?,
                ): FeedTransportTaskPageDto {
                    requests += Request(businessDate, parkId, shedId, status, limit)
                    val suffix = parkId ?: "all"
                    return FeedTransportTaskPageDto(
                        items = listOf(
                            FeedTransportTaskDto(
                                taskId = "task-$suffix",
                                parkId = parkId ?: "park-all",
                                parkLabel = "Farm $suffix",
                                shedId = shedId ?: "shed-all",
                                shedLabel = "Shed $suffix",
                                businessDate = businessDate,
                                status = status ?: "due",
                                scheduledAt = "2026-07-29T15:30:00+05:30",
                            ),
                        ),
                        filters = FeedTransportFilterOptionsDto(
                            parks = listOf(FeedTransportFilterOptionDto("park-1", "Farm 1")),
                            sheds = listOf(FeedTransportFilterOptionDto("shed-1", "Shed 1")),
                        ),
                    )
                }
            }
            val repository = FeedTransportRepository(api, database)
            val farmQuery = FeedTransportQuery("2026-07-29", parkId = "park-1", shedId = "shed-1", status = "completed")
            val allQuery = FeedTransportQuery("2026-07-29")

            assertTrue(repository.refresh(farmQuery).isSuccess)
            assertTrue(repository.refresh(allQuery).isSuccess)

            assertEquals(Request("2026-07-29", "park-1", "shed-1", "completed", 20), requests.first())
            assertEquals("task-park-1", repository.observe(farmQuery, 20).first().items.single().taskId)
            assertEquals("task-all", repository.observe(allQuery, 20).first().items.single().taskId)
            assertEquals("Farm 1", repository.observe(farmQuery, 20).first().filters.parks.single().label)
        } finally {
            database.close()
        }
    }
}
