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
        payloadJson: String = "{}",
    ) =
        OutboxEntity(
            id = id,
            opType = opType,
            groupKey = group,
            idempotencyKey = "key-$id",
            payloadJson = payloadJson,
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
    fun `a terminal failed shifting raise does not block the next raise into the same shed`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "old-shift",
                "COUNTS_SHIFTING",
                "destination-shed-1",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 1,
                conflict = true,
            ),
        )
        dao.insert(row("new-shift", "COUNTS_SHIFTING", "destination-shed-1", createdAt = 6L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "one rejected shifting raise must not poison every future move into that destination shed",
            listOf("new-shift"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `a terminal failed proof upload does not block a replacement proof upload`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "old-proof",
                "PROOF_UPLOAD",
                "proof-task-1",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(row("replacement-proof", "PROOF_UPLOAD", "proof-task-1", createdAt = 6L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "recording proof again must enqueue the fresh upload even when an older take is terminal",
            listOf("replacement-proof"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `a terminal failed pc care task proof registration does not block replacement registration or submit`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "old-register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care-task:task-1",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(row("replacement-register", "PC_CARE_TASK_PROOF_REGISTER", "pc-care-task:task-1", createdAt = 6L))
        dao.insert(row("submit", "PC_CARE_TASK_SUBMIT", "pc-care-task:task-1", createdAt = 7L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "recording stock proof again must not leave the task lane stuck behind an older terminal registration",
            listOf("replacement-register", "submit"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `pc care submit waits when current task proof registration is terminal and unreplaced`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(row("proof", "PROOF_UPLOAD", "pc-care-task:task-1", createdAt = 4L, status = "SUCCEEDED"))
        dao.insert(
            row(
                "current-register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care-task:task-1",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(row("submit", "PC_CARE_TASK_SUBMIT", "pc-care-task:task-1", createdAt = 6L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "submit must not skip straight past a terminal failed registration for the current proof",
            emptyList<String>(),
            eligible,
        )
        database.close()
    }

    @Test
    fun `pc care task proof registration waits behind its queued proof upload`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(row("fresh-proof", "PROOF_UPLOAD", "pc-care:task:task-1", createdAt = 6L))
        dao.insert(
            row(
                "register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care:task:task-1",
                createdAt = 7L,
                payloadJson = """{"proof_outbox_item_id":"fresh-proof"}""",
            ),
        )

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "the register must not consume attempts while its fresh proof upload is still queued",
            listOf("fresh-proof"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `pc care task proof registration ignores unrelated older active proof upload`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(row("stale-proof", "PROOF_UPLOAD", "pc-care:task:task-1", createdAt = 5L, status = "IN_FLIGHT"))
        dao.insert(row("fresh-proof", "PROOF_UPLOAD", "pc-care:task:task-1", createdAt = 6L, status = "SUCCEEDED"))
        dao.insert(
            row(
                "register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care:task:task-1",
                createdAt = 7L,
                payloadJson = """{"proof_outbox_item_id":"fresh-proof"}""",
            ),
        )

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "an unrelated stale upload in the same task group must not starve a fresh replacement register",
            listOf("register"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `pc care submit waits behind active proof upload even when older terminal register exists`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "old-register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care:task:task-1",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(row("fresh-proof", "PROOF_UPLOAD", "pc-care:task:task-1", createdAt = 6L))
        dao.insert(
            row(
                "fresh-register",
                "PC_CARE_TASK_PROOF_REGISTER",
                "pc-care:task:task-1",
                createdAt = 7L,
                payloadJson = """{"proof_outbox_item_id":"fresh-proof"}""",
            ),
        )
        dao.insert(row("submit", "PC_CARE_TASK_SUBMIT", "pc-care:task:task-1", createdAt = 8L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "old terminal registers should not poison the lane, but submit still waits for the fresh upload",
            listOf("fresh-proof"),
            eligible,
        )
        database.close()
    }

    @Test
    fun `a terminal failed pc care animal slot registration does not block replacement registration or submit`() = runBlocking {
        val database = db()
        val dao = database.outboxDao()
        dao.insert(
            row(
                "old-slot-register",
                "PC_CARE_SLOT_REGISTER",
                "pc-care-task:task-1",
                createdAt = 5L,
                status = "FAILED",
                nextAttemptAt = Long.MAX_VALUE,
                attempts = 8,
            ),
        )
        dao.insert(
            row(
                "replacement-slot-register",
                "PC_CARE_SLOT_REGISTER",
                "pc-care-task:task-1",
                createdAt = 6L,
                payloadJson = """{"proof_outbox_item_id":"replacement-proof"}""",
            ),
        )
        dao.insert(row("submit", "PC_CARE_TASK_SUBMIT", "pc-care-task:task-1", createdAt = 7L))

        val eligible = dao.eligibleForDrain(now = 1_000L, limit = 50).map { it.id }

        assertEquals(
            "recording animal proof again must not leave the task lane stuck behind an older terminal registration",
            listOf("replacement-slot-register", "submit"),
            eligible,
        )
        database.close()
    }
}
