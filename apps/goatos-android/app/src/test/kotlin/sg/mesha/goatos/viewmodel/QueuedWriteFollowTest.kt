package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository

/**
 * The write banner must never tell an operator on a good connection that the record "will reach
 * the ledger when the phone is online": that wording is reserved for a row that is STILL unsent
 * after the grace period. Reported 2026-09-04 on the Procurement module.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class QueuedWriteFollowTest {

    private class RowRepo : SyncRepository by RecordingToxinSyncRepository() {
        val row = MutableStateFlow<SyncQueueItem?>(item(SyncItemStatus.QUEUED))
        override fun observeItem(itemId: String): Flow<SyncQueueItem?> = row
    }

    @Test
    fun `a write the server accepts inside the grace period reads saved and never offline`() = runTest {
        val repo = RowRepo()
        val seen = mutableListOf<QueuedWriteOutcome>()
        val job = launch { repo.followQueuedWrite("row", offlineAfterMs = 5_000).toList(seen) }
        advanceTimeBy(800)
        repo.row.value = item(SyncItemStatus.SUCCEEDED)
        advanceTimeBy(10_000)
        job.join()
        assertEquals(listOf<QueuedWriteOutcome>(QueuedWriteOutcome.Saved), seen)
    }

    @Test
    fun `a write still unsent after the grace period reads offline and then upgrades when it lands`() = runTest {
        val repo = RowRepo()
        val seen = mutableListOf<QueuedWriteOutcome>()
        val job = launch { repo.followQueuedWrite("row", offlineAfterMs = 5_000).toList(seen) }
        advanceTimeBy(5_001)
        assertEquals(listOf<QueuedWriteOutcome>(QueuedWriteOutcome.StillQueued), seen)
        repo.row.value = item(SyncItemStatus.SUCCEEDED)
        advanceTimeBy(1)
        job.join()
        assertEquals(listOf(QueuedWriteOutcome.StillQueued, QueuedWriteOutcome.Saved), seen)
    }

    @Test
    fun `a business rejection reads rejected with the server reason`() = runTest {
        val repo = RowRepo()
        val seen = mutableListOf<QueuedWriteOutcome>()
        val job = launch { repo.followQueuedWrite("row", offlineAfterMs = 5_000).toList(seen) }
        advanceTimeBy(100)
        repo.row.value = item(SyncItemStatus.FAILED, conflict = true, lastError = "buyer unknown")
        advanceTimeBy(10_000)
        job.join()
        assertEquals(listOf<QueuedWriteOutcome>(QueuedWriteOutcome.Rejected("buyer unknown")), seen)
    }

    @Test
    fun `a transport failure inside the retry budget is not a rejection`() = runTest {
        val repo = RowRepo()
        val seen = mutableListOf<QueuedWriteOutcome>()
        val job = launch { repo.followQueuedWrite("row", offlineAfterMs = 5_000).toList(seen) }
        repo.row.value = item(SyncItemStatus.FAILED, attemptCount = 1)
        advanceTimeBy(5_001)
        assertEquals(listOf<QueuedWriteOutcome>(QueuedWriteOutcome.StillQueued), seen)
        job.cancel()
    }

    private companion object {
        fun item(status: SyncItemStatus, conflict: Boolean = false, attemptCount: Int = 0, lastError: String? = null) = SyncQueueItem(
            id = "row", opType = "VENDOR_CREATE", idempotencyKey = "k", groupKey = "g", status = status,
            attemptCount = attemptCount, maxAttempts = 5, conflict = conflict, createdAt = 0L, updatedAt = 0L, lastError = lastError,
        )
    }
}
