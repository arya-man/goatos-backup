package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.lang.reflect.Proxy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto

/**
 * End-to-end proof for C35-022 through a real repository (not just the shared cache-layer
 * unit tests): a corrupt cached blob must never wedge [CalendarRepository.observeEvents] in a
 * false loading/empty-forever state. Before this fix the corrupt row was left in Room forever
 * — [DefaultCalendarRepository.refreshEvents] upserts the SAME cache key, so once the row was
 * quarantined (deleted) on read, the very next successful refresh repopulates it and the
 * screen recovers automatically.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class CalendarRepositoryCacheRecoveryTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `corrupt cached row surfaces as no-data then self-heals after a successful refresh`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val database = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val dao = database.calendarCacheDao()
            val key = cacheKey("park-1", null, null, null, null, null, "false", null, null)

            // Simulate a corrupted/partially-written row already sitting in Room.
            dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = "{ this is not valid json", updatedAt = 1L))

            val freshDto = CalendarEventListResponseDto(
                items = listOf(CalendarEventDto(eventId = "evt-1", title = "PPR booster · Gandhi 1", status = "due")),
            )
            val api = Proxy.newProxyInstance(AppApi::class.java.classLoader, arrayOf(AppApi::class.java)) { proxy, method, args ->
                when (method.name) {
                    "listCalendarVaccinationEvents" -> freshDto
                    "toString" -> "CalendarAppApiTestProxy"
                    "hashCode" -> System.identityHashCode(proxy)
                    "equals" -> proxy === args?.firstOrNull()
                    else -> error("unexpected AppApi method ${method.name}")
                }
            } as AppApi

            val repository = DefaultCalendarRepository(api, dao, json, clock = { 2L })

            // Before any refresh: the corrupt row must read as "no data yet", not crash, and
            // must have been quarantined (deleted) rather than left to block forever.
            val beforeRefresh = repository.observeEvents(parkId = "park-1").first()
            assertEquals(null, beforeRefresh.data)
            assertNull("corrupt row must be deleted on read", dao.observe(key).first())

            // A normal background refresh (exactly what the ADR's stale-while-revalidate flow
            // runs on screen open) must now be able to repopulate a clean row.
            repository.refreshEvents(parkId = "park-1").getOrThrow()

            val afterRefresh = repository.observeEvents(parkId = "park-1").first()
            assertEquals(freshDto, afterRefresh.data)
            assertTrue("refresh must repopulate Room so the screen recovers automatically", afterRefresh.data != null)
        } finally {
            database.close()
        }
    }
}
