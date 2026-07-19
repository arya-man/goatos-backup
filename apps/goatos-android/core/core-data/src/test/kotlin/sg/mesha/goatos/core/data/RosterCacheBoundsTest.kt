package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.CacheGovernance
import sg.mesha.goatos.core.data.cache.RosterTimetableCacheEntity
import sg.mesha.goatos.core.data.cache.enforceCacheBounds

/**
 * Regression for the R50-008/010 memory-bound follow-up: the roster timetable/coverage caches
 * were the only JSON-blob cache DAOs that did NOT implement [JsonBlobCacheDao], so the
 * whole `EnrichedPositionListResponseDto` blob accumulated with no TTL/row/byte cap and the
 * android-bounded-memory guard could not see it. This proves the timetable DAO now honors the
 * shared governance contract: [enforceCacheBounds] evicts the oldest rows past the cap.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class RosterCacheBoundsTest {

    @Test
    fun `timetable cache enforces the shared row cap, evicting oldest first`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        try {
            val dao = db.rosterTimetableCacheDao()
            val overflow = 5
            val total = CacheGovernance.DEFAULT_MAX_ROWS + overflow
            // Ascending updatedAt so the first `overflow` keys are the oldest.
            repeat(total) { i ->
                dao.upsert(RosterTimetableCacheEntity(cacheKey = "center-$i", dtoJson = "{}", updatedAt = i.toLong()))
            }
            assertEquals(total, dao.count())

            dao.enforceCacheBounds()

            assertEquals(CacheGovernance.DEFAULT_MAX_ROWS, dao.count())
            // The oldest `overflow` rows are gone; the newest survive.
            repeat(overflow) { i ->
                assertNull("center-$i should have been evicted", dao.observe("center-$i").first())
            }
            assertNotNull("newest row must survive", dao.observe("center-${total - 1}").first())
        } finally {
            db.close()
        }
    }
}
