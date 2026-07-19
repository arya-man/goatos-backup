package sg.mesha.goatos.core.database.outbox

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Regression coverage for two outbox bounded-set fixes:
 *  - EXHAUSTED-SYNC: an attempt-exhausted FAILED row (conflict=0, attemptCount>=maxAttempts) must
 *    LEAVE [OutboxDao.observeActive] (else it accumulates in the active set forever) and appear in
 *    the terminal window instead.
 *  - R50-030: [OutboxDao.observeById] must follow ONE row through to its terminal status, even after
 *    it leaves the active set — so a leadership close can observe its own submission to completion.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxExhaustedAndObserveByIdTest {

    private lateinit var db: OutboxDatabase
    private lateinit var dao: OutboxDao

    @Before
    fun setUp() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        db = Room.inMemoryDatabaseBuilder(context, OutboxDatabase::class.java)
            .allowMainThreadQueries()
            .build()
        dao = db.outboxDao()
    }

    @After
    fun tearDown() = db.close()

    private fun row(
        id: String,
        status: OutboxStatus,
        attemptCount: Int = 0,
        maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
        conflict: Boolean = false,
    ) = OutboxEntity(
        id = id,
        opType = "shed_submit",
        groupKey = "g",
        idempotencyKey = "idem-$id",
        payloadJson = "{}",
        status = status.name,
        attemptCount = attemptCount,
        maxAttempts = maxAttempts,
        conflict = conflict,
        createdAt = 1L,
        updatedAt = 1L,
    )

    @Test
    fun `attempt-exhausted FAILED row leaves active and becomes a terminal`() = runTest {
        dao.insert(row("queued", OutboxStatus.QUEUED))
        dao.insert(row("exhausted", OutboxStatus.FAILED, attemptCount = 8, maxAttempts = 8, conflict = false))
        dao.insert(row("retryable", OutboxStatus.FAILED, attemptCount = 2, maxAttempts = 8, conflict = false))

        val active = dao.observeActive().first().map { it.id }.toSet()
        assertTrue("queued stays active", "queued" in active)
        assertTrue("still-retryable failed stays active", "retryable" in active)
        assertTrue("attempt-exhausted must NOT stay active", "exhausted" !in active)

        val terminals = dao.observeRecentTerminals(50).map { it.id }.toSet()
        assertTrue("attempt-exhausted appears as a terminal", "exhausted" in terminals)
    }

    @Test
    fun `observeById follows one row through to its terminal SUCCEEDED status`() = runTest {
        dao.insert(row("close-me", OutboxStatus.QUEUED))
        assertEquals(OutboxStatus.QUEUED.name, dao.observeById("close-me").first()?.status)

        dao.markInFlight("close-me", now = 2L)
        dao.markSucceeded("close-me", resultJson = "{}", now = 3L)

        // observeActive drops SUCCEEDED; observeById must still see the terminal row.
        assertTrue("close-me" !in dao.observeActive().first().map { it.id })
        val terminal = dao.observeById("close-me").first()
        assertNotNull("observeById must keep emitting the row past terminal", terminal)
        assertEquals(OutboxStatus.SUCCEEDED.name, terminal?.status)
    }
}
