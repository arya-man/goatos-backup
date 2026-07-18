package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.IOException
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.CalendarCacheDao
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto

/**
 * MOB-004 guardrail: Calendar page 2 must live in Room (the on-device SSOT), not in ViewModel
 * memory. Before the fix, only page 1 was persisted; page 2 was accumulated in plain ViewModel
 * fields and vanished on process death or offline re-entry. These tests drive the repository
 * exactly the way the ViewModels now do — [DefaultCalendarRepository.appendEvents] persists each
 * continuation page INTO the same first-page Room scope, and the observed flow re-emits the
 * merged, bounded keyset window.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CalendarRepositoryPaginationTest {
    private data class Request(val status: String?, val dateFrom: String?, val cursor: String?, val limit: Int?)

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `merge preserves first-page order, de-dups by event id, and advances the cursor`() {
        val first = CalendarEventListResponseDto(
            items = listOf(calendarEvent("evt-1"), calendarEvent("evt-2")),
            nextCursor = "cursor-1",
        )
        val second = CalendarEventListResponseDto(
            items = listOf(calendarEvent("evt-2"), calendarEvent("evt-3")),
            nextCursor = null,
        )

        val merged = mergeCalendarEventsPage(first, second)

        assertEquals(listOf("evt-1", "evt-2", "evt-3"), merged.items.map { it.eventId })
        assertNull(merged.nextCursor)
    }

    @Test
    fun `page 2 survives process death and is served offline with no direct-network dependency`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val backend = Backend()
            val dao = database.calendarCacheDao()

            // --- session 1: load two pages, both persisted into Room ---
            val repo1 = repository(database, dao, backend, requests)
            repo1.refreshEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).getOrThrow()
            repo1.appendEvents(cursor = "cursor-1", dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).getOrThrow()

            val afterTwoPages = repo1.observeEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE * 2, afterTwoPages.items.size)
            assertEquals("cursor-2", afterTwoPages.nextCursor)
            val requestsAfterLoad = requests.size
            assertEquals(listOf(null, "cursor-1"), requests.map { it.cursor })

            // --- process death + offline: a brand-new repository instance over the SAME Room,
            // with the network now hard-down. The observed read must return both ordered pages
            // from Room alone, issuing ZERO new network calls. ---
            backend.offline = true
            val repo2 = repository(database, dao, backend, requests)
            val restored = repo2.observeEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).first().data!!

            assertEquals("no network call may happen during the offline Room read", requestsAfterLoad, requests.size)
            assertEquals(PAGE_SIZE * 2, restored.items.size)
            assertEquals(
                (1..PAGE_SIZE * 2).map { "evt-$it" },
                restored.items.map { it.eventId },
            )
            assertEquals(
                "no duplicate rows across the persisted pages",
                restored.items.size,
                restored.items.map { it.eventId }.distinct().size,
            )
            assertEquals("cursor-2", restored.nextCursor)
            assertEquals(afterTwoPages.items.map { it.eventId }, restored.items.map { it.eventId })
        } finally {
            database.close()
        }
    }

    @Test
    fun `offline append leaves the Room window and cursor intact for retry`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val backend = Backend()
            val dao = database.calendarCacheDao()
            val repo = repository(database, dao, backend, requests)
            repo.refreshEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).getOrThrow()

            backend.offline = true
            assertTrue(repo.appendEvents(cursor = "cursor-1", dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).isFailure)

            val stillPageOne = repo.observeEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE, stillPageOne.items.size)
            assertEquals("cursor-1", stillPageOne.nextCursor)

            // recovery: same cursor now succeeds and the window grows.
            backend.offline = false
            repo.appendEvents(cursor = "cursor-1", dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).getOrThrow()
            val recovered = repo.observeEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE * 2, recovered.items.size)
            assertEquals("cursor-2", recovered.nextCursor)
        } finally {
            database.close()
        }
    }

    @Test
    fun `a stale cursor is rejected before the network and the cache stays intact`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val requests = mutableListOf<Request>()
            val backend = Backend()
            val dao = database.calendarCacheDao()
            val repo = repository(database, dao, backend, requests)
            repo.refreshEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).getOrThrow()
            val requestsAfterLoad = requests.size

            val result = repo.appendEvents(cursor = "wrong-cursor", dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE)

            assertTrue(result.exceptionOrNull() is CalendarEventsCursorException)
            assertEquals("stale cursor must be rejected before any network call", requestsAfterLoad, requests.size)
            val cached = repo.observeEvents(dateFrom = DAY, dateTo = DAY, limit = PAGE_SIZE).first().data!!
            assertEquals(PAGE_SIZE, cached.items.size)
            assertEquals("cursor-1", cached.nextCursor)
            assertFalse(cached.items.isEmpty())
        } finally {
            database.close()
        }
    }

    private fun repository(
        database: GoatDatabase,
        dao: CalendarCacheDao,
        backend: Backend,
        requests: MutableList<Request>,
    ): DefaultCalendarRepository {
        val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
            when (method.name) {
                "listCalendarVaccinationEvents" -> {
                    val request = Request(
                        status = args?.get(3) as String?,
                        dateFrom = args?.get(4) as String?,
                        cursor = args?.get(9) as String?,
                        limit = args?.get(10) as Int?,
                    )
                    requests += request
                    if (backend.offline) throw IOException("offline")
                    numberedPage(request.cursor)
                }
                "toString" -> "CalendarAppApiTestProxy"
                "hashCode" -> System.identityHashCode(proxy)
                "equals" -> proxy === args?.firstOrNull()
                else -> error("unexpected AppApi method ${method.name}")
            }
        } as AppApi
        return DefaultCalendarRepository(api, dao, database, json, clock = { 42L })
    }

    private class Backend {
        var offline: Boolean = false
    }

    private fun numberedPage(cursor: String?): CalendarEventListResponseDto {
        val pageIndex = when (cursor) {
            null -> 0
            else -> cursor.removePrefix("cursor-").toInt()
        }
        val start = pageIndex * PAGE_SIZE + 1
        val rows = (start..TOTAL_ROWS).take(PAGE_SIZE).map { index -> calendarEvent("evt-$index") }
        val next = if (start + rows.size - 1 < TOTAL_ROWS) "cursor-${pageIndex + 1}" else null
        return CalendarEventListResponseDto(items = rows, nextCursor = next)
    }

    private fun calendarEvent(eventId: String) = CalendarEventDto(
        eventId = eventId,
        title = eventId,
        status = "due",
        dueAt = "2026-07-13T09:00:00+05:30",
    )

    private companion object {
        const val DAY = "2026-07-13"
        const val PAGE_SIZE = 20
        const val TOTAL_ROWS = 45
    }
}
