package sg.mesha.goatos.core.database.outbox

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Once an account that was parked on the no-access screen (session-events 403) gets its access
 * back, the writes the server refused with 403 meanwhile are re-queued -- and ONLY those. A
 * validation refusal, a conflict or an exhausted transport failure keeps its state.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxRequeueAccessDeniedTest {

    private lateinit var db: OutboxDatabase
    private lateinit var dao: OutboxDao

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        db = Room.inMemoryDatabaseBuilder(context, OutboxDatabase::class.java).allowMainThreadQueries().build()
        dao = db.outboxDao()
    }

    @After
    fun tearDown() = db.close()

    private suspend fun failed(id: String, httpStatus: Int?, conflict: Boolean) {
        dao.insert(
            OutboxEntity(
                id = id, opType = "shed_submit", groupKey = "g-$id", idempotencyKey = "idem-$id", payloadJson = "{}",
                status = OutboxStatus.IN_FLIGHT.name, createdAt = 1L, updatedAt = 1L,
            ),
        )
        dao.markFailed(id, 1, Long.MAX_VALUE, conflict, "refused", "code", null, httpStatus, 2L)
    }

    @Test
    fun `only rows refused with 403 are re-queued`() = runTest {
        failed("access", httpStatus = 403, conflict = true)
        failed("validation", httpStatus = 422, conflict = true)
        failed("conflict", httpStatus = 409, conflict = true)
        failed("transport", httpStatus = null, conflict = false)

        val requeued = dao.requeueAccessDenied(now = 10L)

        assertEquals(1, requeued)
        val access = dao.findById("access")!!
        assertEquals(OutboxStatus.QUEUED.name, access.status)
        assertEquals(0, access.attemptCount)
        assertEquals(false, access.conflict)
        assertNull(access.lastHttpStatus)
        for (id in listOf("validation", "conflict", "transport")) {
            val row = dao.findById(id)!!
            assertEquals("$id untouched", OutboxStatus.FAILED.name, row.status)
            assertEquals("$id keeps its error", "refused", row.lastError)
        }
    }
}
