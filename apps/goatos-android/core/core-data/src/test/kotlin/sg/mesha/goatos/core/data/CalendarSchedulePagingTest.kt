package sg.mesha.goatos.core.data

import androidx.paging.testing.asSnapshot
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.CalendarFilterOptionDto
import sg.mesha.goatos.core.network.dto.CalendarFilterOptionsDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CalendarSchedulePagingTest {

    @Test
    fun `month collection pages by 20 and requests options only on refresh`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val allRows = (1..45).map { index ->
                CalendarEventDto(
                    eventId = "event-$index",
                    dueAt = "2026-07-01T08:${(index - 1).toString().padStart(2, '0')}:00+05:30",
                    title = "Drive $index",
                    status = "scheduled",
                )
            }
            val api = Proxy.newProxyInstance(
                AppApi::class.java.classLoader,
                arrayOf(AppApi::class.java),
            ) { proxy, method, args ->
                when (method.name) {
                    "listCalendarVaccinationEvents" -> {
                        val cursor = args?.get(10) as String?
                        val page = cursor?.removePrefix("cursor-")?.toIntOrNull() ?: 0
                        val start = page * CALENDAR_SCHEDULE_PAGE_SIZE
                        val items = allRows.drop(start).take(CALENDAR_SCHEDULE_PAGE_SIZE)
                        val next = if (start + items.size < allRows.size) "cursor-${page + 1}" else null
                        requests += Request(
                            vaccine = args?.get(8) as String?,
                            includeFilterOptions = args?.get(9) as Boolean,
                            cursor = cursor,
                            limit = args.get(11) as Int?,
                        )
                        CalendarEventListResponseDto(
                            items = items,
                            nextCursor = next,
                            filterOptions = if (page == 0) {
                                CalendarFilterOptionsDto(
                                    vaccines = listOf(CalendarFilterOptionDto("FMD", "FMD")),
                                )
                            } else {
                                null
                            },
                        )
                    }
                    "toString" -> "CalendarScheduleApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi
            val repository = DefaultCalendarRepository(
                api = api,
                dao = database.calendarCacheDao(),
                database = database,
                clock = { 1_000L },
            )
            val query = CalendarScheduleQuery(
                vaccine = "FMD",
                dateFrom = "2026-07-01",
                dateTo = "2026-07-31",
            )

            val flow = repository.schedule(query)
            assertTrue("creating the Paging flow must not prefetch", requests.isEmpty())

            val snapshot = flow.asSnapshot { scrollTo(24) }

            assertTrue(snapshot.size >= 25)
            assertEquals(listOf(null, "cursor-1", "cursor-2"), requests.map { it.cursor })
            assertTrue(requests.first().includeFilterOptions)
            assertTrue(requests.drop(1).none { it.includeFilterOptions })
            assertTrue(requests.drop(1).all { request -> request.cursor != null })
            assertEquals(listOf(20, 20, 20), requests.map { it.limit })
            assertEquals(listOf("FMD", "FMD", "FMD"), requests.map { it.vaccine })
            val metadata = repository.observeScheduleMetadata(query).first().data
            assertEquals("FMD", metadata?.filterOptions?.vaccines?.single()?.value)
        } finally {
            database.close()
        }
    }

    private data class Request(
        val vaccine: String?,
        val includeFilterOptions: Boolean,
        val cursor: String?,
        val limit: Int?,
    )
}
