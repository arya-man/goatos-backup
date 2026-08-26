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

    private fun row(
        id: String,
        opType: String,
        group: String,
        createdAt: Long,
        status: String = "QUEUED",
        nextAttemptAt: Long = 0L,
        attempts: Int = 0,
        conflict: Boolean = false,
    ) =
        OutboxEntity(
            id = id,
            opType = opType,
            groupKey = group,
            idempotencyKey = "key-$id",
            payloadJson = "{}",
            status = status,
            attemptCount = attempts,
            maxAttempts = 8,
            conflict = conflict,
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

    /**
     * A scan that has run out of retries is not "finished" -- it is unsent, permanently.
     *
     * The back-off case above was already covered, and it hid this one: the predicate only
     * held the lane while the scan was still retryable, so the moment it burned its last
     * attempt the Submit became eligible and the shed closed one animal short, reading as
     * complete to everyone. The exhausted scan is not even in the active set, so nothing on
     * screen would have said otherwise.
     */
    @Test
    fun `a Submit is not eligible while a same-lane scan has exhausted its retries`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "scan", "SCAN_CAPTURE", "task-77|whole", createdAt = 5L,
                status = "FAILED", nextAttemptAt = 10L, attempts = 8,
            ),
        )
        dao.insert(row("submit", "SHED_SUBMIT", "task-77|whole", createdAt = 5L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "the Submit must not close a shed whose scan will never be sent",
            emptyList<String>(),
            eligible,
        )
        database.close()
    }

    /** Same again for a scan dead-lettered as a conflict: also unsent, also permanent. */
    @Test
    fun `a Submit is not eligible while a same-lane scan is dead-lettered`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "scan", "SCAN_CAPTURE", "task-77|whole", createdAt = 5L,
                status = "FAILED", nextAttemptAt = 10L, attempts = 2, conflict = true,
            ),
        )
        dao.insert(row("submit", "SHED_SUBMIT", "task-77|whole", createdAt = 5L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "a dead-lettered scan still means the shed is incomplete",
            emptyList<String>(),
            eligible,
        )
        database.close()
    }

    /** A stuck scan holds ITS lane only. Another shed's session must keep draining. */
    @Test
    fun `an exhausted scan does not block a different shed`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "scan", "SCAN_CAPTURE", "task-77|whole", createdAt = 5L,
                status = "FAILED", nextAttemptAt = 10L, attempts = 8,
            ),
        )
        dao.insert(row("other-submit", "SHED_SUBMIT", "task-99|whole", createdAt = 5L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(listOf("other-submit"), eligible)
        database.close()
    }

    @Test
    fun `a terminal pc care submit does not block later proof repair rows`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "stale-submit",
                "PC_CARE_TASK_SUBMIT",
                "pc-care:task:task-77",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(row("proof-upload", "PROOF_UPLOAD", "pc-care:task:task-77", createdAt = 6L))
        dao.insert(row("proof-register", "PC_CARE_TASK_PROOF_REGISTER", "pc-care:task:task-77", createdAt = 7L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "a stale failed submit must not strand corrective proof uploads behind a loader",
            listOf("proof-upload", "proof-register"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `a terminal pc care submit still blocks a later non repair submit`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "stale-submit",
                "PC_CARE_TASK_SUBMIT",
                "pc-care:task:task-77",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(row("new-submit", "PC_CARE_TASK_SUBMIT", "pc-care:task:task-77", createdAt = 6L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(emptyList<String>(), eligible)
        database.close()
    }

    @Test
    fun `a terminal pc care proof register does not block later proof repair rows`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "stale-register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care:task:task-77",
                createdAt = 5L,
                status = "FAILED",
                conflict = true,
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 1,
            ),
        )
        dao.insert(row("replacement-upload", "PROOF_UPLOAD", "pc-care:task:task-77", createdAt = 6L))
        dao.insert(row("replacement-register", "PC_CARE_TASK_PROOF_REGISTER", "pc-care:task:task-77", createdAt = 7L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "a stale failed proof registration must not strand replacement proof uploads behind a loader",
            listOf("replacement-upload", "replacement-register"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `a terminal pc care proof register still blocks submit`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "stale-register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care:task:task-77",
                createdAt = 5L,
                status = "FAILED",
                conflict = true,
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 1,
            ),
        )
        dao.insert(row("submit", "PC_CARE_TASK_SUBMIT", "pc-care:task:task-77", createdAt = 6L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(emptyList<String>(), eligible)
        database.close()
    }

    @Test
    fun `a terminal proof upload does not block replacement proof repair rows`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "stale-upload",
                "PROOF_UPLOAD",
                "feed:packing:task-77",
                createdAt = 5L,
                status = "FAILED",
                conflict = true,
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 1,
            ),
        )
        dao.insert(row("replacement-upload", "PROOF_UPLOAD", "feed:packing:task-77", createdAt = 6L))
        dao.insert(row("replacement-submit", "FEED_PACKING_COMPLETE", "feed:packing:task-77", createdAt = 7L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "a stale failed proof upload must not strand replacement proof rows behind a loader",
            listOf("replacement-upload", "replacement-submit"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `a terminal proof upload does not block a later proof referenced write`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "stale-upload",
                "PROOF_UPLOAD",
                "pc-care:task:task-77",
                createdAt = 5L,
                status = "FAILED",
                conflict = true,
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 1,
            ),
        )
        dao.insert(row("submit", "PC_CARE_TASK_SUBMIT", "pc-care:task:task-77", createdAt = 6L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "the submit must drain and fail explicitly if it references the stale proof, instead of being stranded forever",
            listOf("submit"),
            eligible,
        )
        database.close()
    }
}
