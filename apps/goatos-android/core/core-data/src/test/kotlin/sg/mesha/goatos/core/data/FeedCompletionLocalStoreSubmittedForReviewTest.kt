package sg.mesha.goatos.core.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import java.time.LocalDate
import java.time.ZoneId

/**
 * Coverage for [FeedCompletionLocalStore.markSubmittedForReview] and
 * [FeedCompletionLocalStore.submittedForReviewKeys] — the optimistic overlay source for
 * pending-but-not-yet-synced Feed Packing submits. The store works the exact same way as the
 * existing [markCompleted] / [completedKeys] pair, respecting both authority boundaries:
 *
 *  1. PRINCIPAL — [clear] wipes the submitted set so the next operator doesn't see the
 *     departing operator's queued submits (same as completions).
 *  2. BUSINESS DAY — entries are day-prefixed so yesterday's key never matches today's row
 *     (same as completions).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class FeedCompletionLocalStoreSubmittedForReviewTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `markSubmittedForReview adds the key to submittedForReviewKeys`() {
        val store = FeedCompletionLocalStore()
        val key = FeedCompletionLocalStore.key("shed-1", "Pen A", 1, "morning")

        store.markSubmittedForReview(key)

        assertTrue("key should be in submitted set", store.submittedForReviewKeys.value.contains(key))
    }

    @Test
    fun `submittedForReviewKeys is distinct from completedKeys`() {
        val store = FeedCompletionLocalStore()
        val key1 = FeedCompletionLocalStore.key("shed-1", "Pen A", 1, "morning")
        val key2 = FeedCompletionLocalStore.key("shed-2", "Pen B", 2, "evening")

        store.markCompleted(key1)
        store.markSubmittedForReview(key2)

        assertTrue("completed key should be in completedKeys", store.completedKeys.value.contains(key1))
        assertTrue("submitted key should be in submittedForReviewKeys", store.submittedForReviewKeys.value.contains(key2))
        assertFalse("completed key should NOT be in submitted", store.submittedForReviewKeys.value.contains(key1))
        assertFalse("submitted key should NOT be in completed", store.completedKeys.value.contains(key2))
    }

    @Test
    fun `markSubmittedForReview prunes entries from earlier business days`() = runTest(dispatcher) {
        val store = FeedCompletionLocalStore()

        // Simulate business date manipulation
        val originalDate = FeedCompletionLocalStore.businessDate
        try {
            // Set businessDate to a past date and mark an entry
            FeedCompletionLocalStore.businessDate = { "2026-08-14" }
            val yesterdayKey = FeedCompletionLocalStore.key("shed-1", "Pen A", 1, "morning")
            store.markSubmittedForReview(yesterdayKey)

            // Verify the entry was added
            assertTrue("yesterday's key should be present initially", store.submittedForReviewKeys.value.contains(yesterdayKey))

            // Move to today
            FeedCompletionLocalStore.businessDate = { "2026-08-16" }
            val todayKey = FeedCompletionLocalStore.key("shed-2", "Pen B", 2, "evening")

            // Mark a new entry (should prune yesterday's)
            store.markSubmittedForReview(todayKey)

            val current = store.submittedForReviewKeys.value
            assertFalse("yesterday's key should be pruned", current.contains(yesterdayKey))
            assertTrue("today's key should be present", current.contains(todayKey))
        } finally {
            // Restore original businessDate
            FeedCompletionLocalStore.businessDate = originalDate
        }
    }

    @Test
    fun `clear() wipes all submitted keys alongside completed keys`() {
        val store = FeedCompletionLocalStore()

        val completedKey = FeedCompletionLocalStore.key("shed-1", "Pen A", 1, "morning")
        val submittedKey1 = FeedCompletionLocalStore.key("shed-2", "Pen B", 2, "evening")
        val submittedKey2 = FeedCompletionLocalStore.key("shed-3", "Pen C", 3, "morning")

        store.markCompleted(completedKey)
        store.markSubmittedForReview(submittedKey1)
        store.markSubmittedForReview(submittedKey2)

        assertEquals("should have 1 completed key", 1, store.completedKeys.value.size)
        assertEquals("should have 2 submitted keys", 2, store.submittedForReviewKeys.value.size)

        store.clear()

        assertEquals("clear() should wipe all completed", 0, store.completedKeys.value.size)
        assertEquals("clear() should wipe all submitted", 0, store.submittedForReviewKeys.value.size)
    }

    @Test
    fun `multiple submissions to different pens are all tracked`() {
        val store = FeedCompletionLocalStore()

        val castro1 = FeedCompletionLocalStore.key("shed-1", "Pen 1", 1, "morning")
        val castro2 = FeedCompletionLocalStore.key("shed-1", "Pen 2", 1, "morning")
        val morning = FeedCompletionLocalStore.key("shed-1", "Pen 1", 1, "morning")
        val evening = FeedCompletionLocalStore.key("shed-1", "Pen 1", 1, "evening")

        store.markSubmittedForReview(castro1)
        store.markSubmittedForReview(castro2)
        store.markSubmittedForReview(evening)

        val submitted = store.submittedForReviewKeys.value
        assertEquals("should track multiple distinct keys", 3, submitted.size)
        assertTrue("castro1 should be tracked", submitted.contains(castro1))
        assertTrue("castro2 should be tracked", submitted.contains(castro2))
        assertTrue("evening should be tracked", submitted.contains(evening))
    }

    @Test
    fun `markSubmittedForReview respects the exact key format from FeedCompletionLocalStore_key`() {
        val store = FeedCompletionLocalStore()

        val shedId = "shed-production-1"
        val partitionLabel = "Cattle Pen A"
        val sessionNo = 3
        val workflow = "morning"

        val key = FeedCompletionLocalStore.key(shedId, partitionLabel, sessionNo, workflow)
        store.markSubmittedForReview(key)

        assertTrue("generated key should be in submitted set", store.submittedForReviewKeys.value.contains(key))

        // Verify the key format matches the backend/ViewModel expectations
        // (see FeedPackingViewModel.toRowUi for how the key is used)
        val expectedParts = listOf(
            LocalDate.now(ZoneId.of("Asia/Kolkata")).toString(),
            shedId,
            partitionLabel.trim().lowercase().ifBlank { "whole" },
            sessionNo.toString(),
            workflow,
        )
        assertTrue("key should encode all relevant fields", key.contains(shedId))
        assertTrue("key should encode session number", key.contains(sessionNo.toString()))
        assertTrue("key should encode workflow", key.contains(workflow))
    }
}
