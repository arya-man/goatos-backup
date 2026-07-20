package sg.mesha.goatos.core.data.cache

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto

/**
 * Coverage for the shared JSON-blob-by-scope cache layer (C35-017 TTL/cap/eviction,
 * C35-022 corrupt-blob quarantine). Exercised through [CalendarCacheDao] as a representative
 * table — every other JSON-blob cache table ([ControlTowerCacheDao], [ExecutionRowsCacheDao],
 * [ExecutionShedCacheDao], [AdherenceCacheDao], [InsightsGapsCacheDao],
 * [InsightsCoverageCacheDao]) implements the identical [JsonBlobCacheDao] contract and shares
 * this exact [readCachedJson] / [enforceCacheBounds] code path, so proving the policy once
 * here proves it everywhere it's wired.
 *
 * Robolectric + a real (in-memory) Room database is required — the eviction/TTL SQL
 * (`ORDER BY updatedAt ASC LIMIT`, `SUM(LENGTH(...))`) needs real SQLite, not a stub.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class JsonBlobCacheSupportTest {
    private val json = Json { ignoreUnknownKeys = true }

    private fun sampleDto(id: String = "evt-1") = CalendarEventListResponseDto(
        items = listOf(CalendarEventDto(eventId = id, title = "PPR booster · Gandhi 1", status = "due")),
    )

    private fun newDb(): GoatDatabase {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        return Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    // --- (a) corrupt blob is quarantined on read, not left to wedge the screen forever ---

    @Test
    fun `corrupt blob is deleted on read and reported as quarantined, not a clean miss`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val key = "park-1"
            dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = "{ not valid json !!", updatedAt = 100L))

            val entity = dao.observe(key).first()
            val result = readCachedJson<CalendarEventListResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = entity?.dtoJson,
                updatedAt = entity?.updatedAt,
                now = 200L,
                quarantine = { dao.delete(it) },
            )

            assertNull("corrupt row must never surface as decoded data", result.data)
            assertTrue(
                "a row that existed but failed to decode must be flagged, distinguishing it " +
                    "from a genuine cold-cache miss",
                result.wasQuarantined,
            )
            assertNull(
                "the corrupt row must be deleted so the next successful refresh can repopulate " +
                    "a clean row through the normal upsert path",
                dao.observe(key).first(),
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun `a genuine cold-cache miss is not reported as quarantined`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val result = readCachedJson<CalendarEventListResponseDto>(
                json = json,
                cacheKey = "never-synced",
                dtoJson = null,
                updatedAt = null,
                now = 100L,
                quarantine = { dao.delete(it) },
            )
            assertNull(result.data)
            assertFalse(
                "no row ever existed here — this is a normal cold start, not corruption",
                result.wasQuarantined,
            )
        } finally {
            db.close()
        }
    }

    @Test
    fun `clean row decodes normally and is left untouched`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val key = "park-1"
            val dto = sampleDto()
            dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = 100L))

            val entity = dao.observe(key).first()
            val result = readCachedJson<CalendarEventListResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = entity?.dtoJson,
                updatedAt = entity?.updatedAt,
                now = 100L,
                quarantine = { dao.delete(it) },
            )

            assertEquals(dto, result.data)
            assertFalse(result.wasQuarantined)
            assertEquals(100L, result.updatedAt)
            assertEquals("a clean row must survive the read untouched", entity, dao.observe(key).first())
        } finally {
            db.close()
        }
    }

    // --- (b) a hard-TTL-expired row is treated as a miss (and quarantined) ---

    @Test
    fun `row past the hard TTL is treated as a miss and quarantined`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val key = "park-1"
            val writtenAt = 1_000L
            dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = json.encodeToString(sampleDto()), updatedAt = writtenAt))

            val entity = dao.observe(key).first()
            val now = writtenAt + CacheGovernance.DEFAULT_TTL_MILLIS + 1
            val result = readCachedJson<CalendarEventListResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = entity?.dtoJson,
                updatedAt = entity?.updatedAt,
                now = now,
                quarantine = { dao.delete(it) },
            )

            assertNull("a row past the hard TTL must not be served, even though it decodes fine", result.data)
            assertTrue(result.wasQuarantined)
            assertNull("expired row must be deleted, not served stale forever", dao.observe(key).first())
        } finally {
            db.close()
        }
    }

    @Test
    fun `row inside the TTL window is served normally`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val key = "park-1"
            val dto = sampleDto()
            val writtenAt = 1_000L
            dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = writtenAt))

            val entity = dao.observe(key).first()
            val now = writtenAt + CacheGovernance.DEFAULT_TTL_MILLIS - 1
            val result = readCachedJson<CalendarEventListResponseDto>(
                json = json,
                cacheKey = key,
                dtoJson = entity?.dtoJson,
                updatedAt = entity?.updatedAt,
                now = now,
                quarantine = { dao.delete(it) },
            )

            assertEquals(dto, result.data)
            assertFalse(result.wasQuarantined)
        } finally {
            db.close()
        }
    }

    // --- (c) bounded row + byte cap evicts oldest-first ---

    @Test
    fun `row cap evicts the oldest entries beyond the limit`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val maxRows = 5
            (1..8).forEach { i ->
                dao.upsert(CalendarCacheEntity(cacheKey = "scope-$i", dtoJson = "{}", updatedAt = i.toLong()))
            }
            assertEquals(8, dao.count())

            dao.enforceCacheBounds(maxRows = maxRows, maxBytes = Long.MAX_VALUE)

            assertEquals("row cap must bring the table down to exactly the limit", maxRows, dao.count())
            (1..3).forEach { i ->
                assertNull("oldest scope-$i must have been evicted first (LRU)", dao.observe("scope-$i").first())
            }
            (4..8).forEach { i ->
                assertEquals("newest scope-$i must survive", "scope-$i", dao.observe("scope-$i").first()?.cacheKey)
            }
        } finally {
            db.close()
        }
    }

    @Test
    fun `byte cap evicts oldest entries when total payload exceeds the budget`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            val payload = "x".repeat(100)
            (1..5).forEach { i ->
                dao.upsert(CalendarCacheEntity(cacheKey = "scope-$i", dtoJson = payload, updatedAt = i.toLong()))
            }
            assertEquals(5, dao.count())
            assertEquals(500L, dao.totalBytes())

            // Budget only fits 2 rows worth of payload — the loop must evict down to it.
            dao.enforceCacheBounds(maxRows = Int.MAX_VALUE, maxBytes = 250L)

            assertEquals(2, dao.count())
            assertEquals(200L, dao.totalBytes())
            assertNull("oldest scope-1 must have been evicted first (LRU)", dao.observe("scope-1").first())
            assertNull(dao.observe("scope-2").first())
            assertNull(dao.observe("scope-3").first())
            assertEquals("scope-4", dao.observe("scope-4").first()?.cacheKey)
            assertEquals("scope-5", dao.observe("scope-5").first()?.cacheKey)
        } finally {
            db.close()
        }
    }

    @Test
    fun `enforceCacheBounds is a no-op when under both budgets`() = runTest {
        val db = newDb()
        try {
            val dao = db.calendarCacheDao()
            dao.upsert(CalendarCacheEntity(cacheKey = "scope-1", dtoJson = "{}", updatedAt = 1L))

            dao.enforceCacheBounds(maxRows = CacheGovernance.DEFAULT_MAX_ROWS, maxBytes = CacheGovernance.DEFAULT_MAX_BYTES)

            assertEquals(1, dao.count())
            assertEquals("scope-1", dao.observe("scope-1").first()?.cacheKey)
        } finally {
            db.close()
        }
    }
}
