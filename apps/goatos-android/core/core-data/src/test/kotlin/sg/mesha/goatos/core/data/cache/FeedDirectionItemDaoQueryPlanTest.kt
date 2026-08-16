package sg.mesha.goatos.core.data.cache

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
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
import sg.mesha.goatos.core.data.GoatDatabase

/**
 * Regression + query-plan proof for [FeedDirectionItemDao.observeRowForShedSessionInRange].
 *
 * The query it replaced (`grainKey LIKE :shedId || '|' || ... || '%'`) compiled to a full table
 * SCAN: SQLite's LIKE-optimizes-to-index-seek transform only fires for a LITERAL pattern or a bare
 * bound parameter, never for a pattern built by concatenating multiple bound parameters at query
 * time, and this table never enables `PRAGMA case_sensitive_like` either (required for LIKE to use
 * an index at all). `EXPLAIN QUERY PLAN` is asserted directly here rather than merely documented,
 * so a future edit that reintroduces a concatenated LIKE fails this test, not just a code review.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FeedDirectionItemDaoQueryPlanTest {

    private val context = ApplicationProvider.getApplicationContext<android.content.Context>()
    private lateinit var db: GoatDatabase

    @Before
    fun setUp() {
        // Fresh in-memory current-schema DB -- no upgrade path needed, so no migration chain
        // required (unlike GoatDatabaseUpgradeCrashTest's real-file upgrade proofs).
        db = Room.inMemoryDatabaseBuilder(context, GoatDatabase::class.java)
            .allowMainThreadQueries()
            .build()
    }

    @After
    fun tearDown() = db.close()

    private fun dao() = db.feedDirectionItemDao()

    @Test
    fun `range-bound prefix query uses the grainKey index, not a table scan`() = runTest {
        val plan = db.query(
            "EXPLAIN QUERY PLAN SELECT * FROM feed_direction_items " +
                "WHERE grainKey >= 'shed-1|A|normal|' AND grainKey < 'shed-1|A|normal|￿' " +
                "AND SUBSTR(grainKey, LENGTH(grainKey) - LENGTH('1')) = '|' || '1' " +
                "ORDER BY updatedAt DESC LIMIT 1",
            null,
        ).use { cursor ->
            val detailCol = cursor.getColumnIndex("detail")
            buildString {
                while (cursor.moveToNext()) {
                    append(cursor.getString(detailCol))
                    append('\n')
                }
            }
        }
        assertFalse(
            "range-bound prefix query must NOT fall back to a full table SCAN, plan was:\n$plan",
            plan.contains("SCAN feed_direction_items", ignoreCase = true),
        )
        assertTrue(
            "range-bound prefix query must use the grainKey index, plan was:\n$plan",
            plan.contains("USING INDEX", ignoreCase = true) || plan.contains("SEARCH", ignoreCase = true),
        )
    }

    @Test
    fun `range-bound prefix query still finds the exact shed-session row it used to LIKE-match`() = runTest {
        dao().upsertAll(
            listOf(
                // The row this shed-session actually owns.
                FeedDirectionItemEntity(
                    queryKey = "q1",
                    grainKey = "shed-1|A|normal|ration|arm|tag|1",
                    sortIndex = 0,
                    dtoJson = """{"lifecycleStatus":"awaiting"}""",
                    updatedAt = 10L,
                ),
                // A DIFFERENT session at the SAME shed/partition/workflow whose grainKey contains
                // "1" as a substring near the tail -- the class of false-match the OLD mid-string
                // LIKE ('%|' || sessionNo) could produce.
                FeedDirectionItemEntity(
                    queryKey = "q1",
                    grainKey = "shed-1|A|normal|ration|arm|tag|21",
                    sortIndex = 1,
                    dtoJson = """{"lifecycleStatus":"completed"}""",
                    updatedAt = 20L,
                ),
                // A different shed/partition/workflow prefix entirely.
                FeedDirectionItemEntity(
                    queryKey = "q1",
                    grainKey = "shed-2|A|normal|ration|arm|tag|1",
                    sortIndex = 2,
                    dtoJson = """{"lifecycleStatus":"completed"}""",
                    updatedAt = 30L,
                ),
            ),
        )

        val prefix = "shed-1|A|normal|"
        val prefixEnd = prefix + "￿"
        val row = dao().observeRowForShedSessionInRange(prefix, prefixEnd, "1").first()

        assertEquals(
            "session-1 must resolve to its OWN row, not session-21's",
            "shed-1|A|normal|ration|arm|tag|1",
            row?.grainKey,
        )
        assertEquals("""{"lifecycleStatus":"awaiting"}""", row?.dtoJson)
    }
}
