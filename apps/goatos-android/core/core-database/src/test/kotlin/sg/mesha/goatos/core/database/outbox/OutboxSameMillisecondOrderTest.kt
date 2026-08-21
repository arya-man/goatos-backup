package sg.mesha.goatos.core.database.outbox

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Drain order when two rows share a millisecond — asserted against REAL SQLite.
 *
 * This cannot be proven with an in-memory fake: Kotlin's `sortedBy` is stable, so a fake
 * silently preserves insertion order and passes whether or not the query has a tie-break.
 * SQLite gives no such guarantee — `ORDER BY createdAt` alone leaves tied rows in an
 * unspecified order — so the only honest test is the query itself.
 *
 * What is at stake: an operator's last scan and their Submit can land in the same
 * millisecond. If the Submit wins, the shed closes with that animal unsent, and the shed
 * reads as complete so nobody goes looking.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class OutboxSameMillisecondOrderTest {

    private fun db(): OutboxDatabase =
        Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            OutboxDatabase::class.java,
        ).allowMainThreadQueries().build()

    private fun row(id: String, opType: String, group: String, createdAt: Long, status: String = "QUEUED", nextAttemptAt: Long = 0L, attempts: Int = 0) =
        OutboxEntity(
            id = id,
            opType = opType,
            groupKey = group,
            idempotencyKey = "key-$id",
            payloadJson = "{}",
            status = status,
            attemptCount = attempts,
            maxAttempts = 8,
            conflict = false,
            createdAt = createdAt,
            updatedAt = createdAt,
            nextAttemptAt = nextAttemptAt,
        )

    @Test
    fun `the scan inserted first drains first when timestamps collide`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        // Same lane, same millisecond, scan written first — as the operator worked.
        dao.insert(row("scan", "SCAN_CAPTURE", "task-77|whole", createdAt = 5L))
        dao.insert(row("submit", "SHED_SUBMIT", "task-77|whole", createdAt = 5L))

        val order = dao.eligibleForDrain(now = 100L, limit = 50).map { it.id }

        assertEquals(listOf("scan", "submit"), order)
        database.close()
    }

    @Test
    fun `a Submit is not eligible while a same-millisecond scan is backed off`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        // The scan failed and its retry is still in the future: what a later drain finds.
        dao.insert(row("scan", "SCAN_CAPTURE", "task-77|whole", createdAt = 5L, status = "FAILED", nextAttemptAt = 10_000L, attempts = 1))
        dao.insert(row("submit", "SHED_SUBMIT", "task-77|whole", createdAt = 5L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals("the Submit must not be eligible ahead of its own shed's unsent scan", emptyList<String>(), eligible)
        database.close()
    }

    @Test
    fun `a different lane is unaffected by the tie-break`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(row("scan-a", "SCAN_CAPTURE", "task-A|whole", createdAt = 5L, status = "FAILED", nextAttemptAt = 10_000L, attempts = 1))
        dao.insert(row("submit-b", "SHED_SUBMIT", "task-B|whole", createdAt = 5L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(listOf("submit-b"), eligible)
        database.close()
    }
}
