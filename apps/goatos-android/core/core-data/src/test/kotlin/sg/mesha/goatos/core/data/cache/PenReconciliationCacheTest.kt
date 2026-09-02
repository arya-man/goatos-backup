package sg.mesha.goatos.core.data.cache

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.GoatDatabase

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PenReconciliationCacheTest {
    @Test
    fun `find by id returns the freshest cached status snapshot`() = runTest {
        val db = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            GoatDatabase::class.java,
        ).allowMainThreadQueries().build()
        try {
            val dao = db.penReconciliationItemDao()
            dao.upsertAll(
                listOf(
                    PenReconciliationItemEntity(
                        queryKey = cacheKey("pen-reconciliation", "open"),
                        cardId = "card-1",
                        sortIndex = 0,
                        raisedAt = "2026-09-02T10:00:00Z",
                        dtoJson = """{"card_id":"card-1","status":"open"}""",
                        updatedAt = 100,
                    ),
                    PenReconciliationItemEntity(
                        queryKey = cacheKey("pen-reconciliation", "rework"),
                        cardId = "card-1",
                        sortIndex = 0,
                        raisedAt = "2026-09-02T10:00:00Z",
                        dtoJson = """{"card_id":"card-1","status":"rework"}""",
                        updatedAt = 200,
                    ),
                ),
            )

            assertEquals(
                """{"card_id":"card-1","status":"rework"}""",
                dao.findById("card-1")?.dtoJson,
            )
        } finally {
            db.close()
        }
    }
}
