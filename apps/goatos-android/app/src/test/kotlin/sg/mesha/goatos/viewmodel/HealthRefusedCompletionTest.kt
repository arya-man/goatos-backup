package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem

/**
 * A SUBMIT THE SERVER REFUSED IS NOT A SUBMIT.
 *
 * The card marks itself completed the moment the write is queued, which is right -- the work was
 * done and the phone may be offline for hours. But when the server terminally refuses it, the
 * operator was left with "Sync will finish automatically" for a write that never will: the phone
 * showed the session closed while the farm still had it open. Found on a real device on
 * 2026-09-23, tapping Complete at 00:52 on a visit that opens at 08:00.
 */
class HealthRefusedCompletionTest {
    private fun item(
        status: SyncItemStatus,
        attemptCount: Int = 1,
        maxAttempts: Int = 5,
        conflict: Boolean = false,
        lastError: String? = null,
    ) = SyncQueueItem(
        id = "item-1",
        opType = "HEALTH_TREATMENT_COMPLETE",
        idempotencyKey = "health-complete:session-1:proof-1",
        groupKey = "session-1",
        status = status,
        attemptCount = attemptCount,
        maxAttempts = maxAttempts,
        conflict = conflict,
        createdAt = 0,
        updatedAt = 0,
        lastError = lastError,
    )

    @Test
    fun `a write still travelling says nothing`() {
        assertNull(refusedCompletionNotice(item(SyncItemStatus.QUEUED, attemptCount = 0)))
        assertNull(refusedCompletionNotice(item(SyncItemStatus.IN_FLIGHT)))
        // A transport failure that will retry is still travelling: telling the operator it failed
        // would send them to re-do work the phone is about to deliver.
        assertNull(refusedCompletionNotice(item(SyncItemStatus.FAILED, attemptCount = 1)))
    }

    @Test
    fun `a write that landed says nothing`() {
        assertNull(refusedCompletionNotice(item(SyncItemStatus.SUCCEEDED, attemptCount = 0)))
    }

    @Test
    fun `a refusal is reported in the server's own words`() {
        val notice = refusedCompletionNotice(
            item(SyncItemStatus.FAILED, conflict = true, lastError = "Morning work opens at 08:00."),
        )
        assertEquals("Morning work opens at 08:00.", notice)
    }

    @Test
    fun `a refusal that named no reason still says the work is not done`() {
        val notice = refusedCompletionNotice(item(SyncItemStatus.FAILED, conflict = true))
        assertEquals("This session could not be completed. Please try again.", notice)
    }

    @Test
    fun `a dead-lettered write is reported too`() {
        val notice = refusedCompletionNotice(
            item(SyncItemStatus.FAILED, attemptCount = 5, maxAttempts = 5, lastError = "Could not reach the server."),
        )
        assertEquals("Could not reach the server.", notice)
    }
}
