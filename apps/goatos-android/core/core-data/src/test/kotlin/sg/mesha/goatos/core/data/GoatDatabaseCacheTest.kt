package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.sqlite.db.SupportSQLiteDatabase
import androidx.sqlite.db.SupportSQLiteOpenHelper
import androidx.sqlite.db.framework.FrameworkSQLiteOpenHelperFactory
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.CalendarCacheEntity
import sg.mesha.goatos.core.data.cache.cacheKey
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto

/**
 * Offline-first foundation coverage (docs/decisions/android-offline-first.md):
 *  1. [MIGRATION_1_2] actually creates every new v2 cache table on top of a real v1
 *     (bootstrap-cache-only) schema — purely additive, nothing destructive.
 *  2. A cache DAO round-trip (upsert -> observe) emits the exact DTO that was written,
 *     proving the JSON-blob-by-scope cache Room is built around actually works end to end.
 *
 * Room's Android database builder needs a real `Context` backed by a real (not stubbed)
 * `android.database.sqlite` — Robolectric provides both under a plain `testDebugUnitTest`
 * JVM run, without a device/emulator. Pinned to API 34 (@Config) regardless of this
 * module's compileSdk 36; nothing here depends on SDK-specific behavior.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GoatDatabaseCacheTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `migration 1 to 2 creates every new cache table`() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val factory = FrameworkSQLiteOpenHelperFactory()
        val configuration = SupportSQLiteOpenHelper.Configuration.builder(context)
            .name(null) // in-memory
            .callback(object : SupportSQLiteOpenHelper.Callback(1) {
                override fun onCreate(db: SupportSQLiteDatabase) {
                    // The real v1 schema: bootstrap_cache was the only table before this ADR.
                    db.execSQL(
                        "CREATE TABLE IF NOT EXISTS `bootstrap_cache` " +
                            "(`id` INTEGER NOT NULL, `dtoJson` TEXT NOT NULL, `updatedAt` INTEGER NOT NULL, " +
                            "PRIMARY KEY(`id`))",
                    )
                }

                override fun onUpgrade(db: SupportSQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit
            })
            .build()
        val helper = factory.create(configuration)

        try {
            val db = helper.writableDatabase // triggers onCreate at v1
            MIGRATION_1_2.migrate(db)

            val tables = mutableSetOf<String>()
            db.query("SELECT name FROM sqlite_master WHERE type = 'table'").use { cursor ->
                while (cursor.moveToNext()) {
                    tables += cursor.getString(0)
                }
            }

            listOf(
                "bootstrap_cache",
                "calendar_cache",
                "control_tower_cache",
                "execution_rows_cache",
                "execution_shed_cache",
                "scan_roster_cache",
                "adherence_cache",
                "insights_gaps_cache",
                "insights_coverage_cache",
            ).forEach { table ->
                assertTrue("expected table `$table` after MIGRATION_1_2", table in tables)
            }
        } finally {
            helper.close()
        }
    }

    @Test
    fun `calendar cache upsert then observe emits the decoded dto`() = runTest {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()

        try {
            val dao = db.calendarCacheDao()
            val dto = CalendarEventListResponseDto(
                items = listOf(
                    CalendarEventDto(eventId = "evt-1", title = "PPR booster · Gandhi 1", status = "due"),
                ),
            )
            val key = cacheKey("park-1", null, null, null, null, null, null, null)

            // Cold cache: nothing observed yet.
            assertEquals(null, dao.observe(key).first())

            dao.upsert(CalendarCacheEntity(cacheKey = key, dtoJson = json.encodeToString(dto), updatedAt = 42L))

            val cached = dao.observe(key).first()
            assertEquals(42L, cached?.updatedAt)
            val decoded = cached?.dtoJson?.let { json.decodeFromString<CalendarEventListResponseDto>(it) }
            assertEquals(dto, decoded)
        } finally {
            db.close()
        }
    }
}
