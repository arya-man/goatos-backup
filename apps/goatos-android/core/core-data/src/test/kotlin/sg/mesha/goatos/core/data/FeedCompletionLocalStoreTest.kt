package sg.mesha.goatos.core.data

import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.sync.OutboxWiper
import sg.mesha.goatos.core.data.sync.SyncJobsCanceller
import sg.mesha.goatos.core.datastore.FakeDeviceStore
import sg.mesha.goatos.core.datastore.FakeSessionStore
import sg.mesha.goatos.core.network.FakeAppApi

/**
 * BLOCKER 6 proof.
 *
 * [FeedCompletionLocalStore] is a PROCESS singleton holding one operator's optimistic feed
 * completions. Two ways that outlived its authority before this change, both reproduced below
 * and both failing against the old store (which had no `clear()` at all, and whose `key()`
 * carried no business date):
 *
 *  1. Across LOGOUT — the maintainer is about to test every role by switching users on one
 *     device. The departing operator's completions stayed in the process, so the next principal
 *     saw someone else's sheds badged done.
 *  2. Across the BUSINESS DAY — the same shed/session/workflow comes due again every day, so a
 *     day-blind key made yesterday's "done" resurface on today's due row.
 */
class FeedCompletionLocalStoreTest {

    private var fakeToday: String = "2026-08-01"

    init {
        FeedCompletionLocalStore.businessDate = { fakeToday }
    }

    @After
    fun restoreClock() {
        FeedCompletionLocalStore.businessDate = { java.time.LocalDate.now(java.time.ZoneId.of("Asia/Kolkata")).toString() }
    }

    @Test
    fun `clear drops the departing operator's optimistic completions`() {
        val store = FeedCompletionLocalStore()
        val key = FeedCompletionLocalStore.key("shed-1", null, 1, "feed_direction")
        store.markCompleted(key)
        assertTrue("precondition: the completion is visible", store.isCompleted(key))

        store.clear()

        assertFalse("no completion survives clear()", store.isCompleted(key))
        assertEquals("the observable overlay is empty too", emptySet<String>(), store.completedKeys.value)
    }

    @Test
    fun `logout clears the feed completion overlay so the next operator sees none of it`() = runTest {
        val store = FeedCompletionLocalStore()
        val key = FeedCompletionLocalStore.key("shed-1", null, 1, "feed_direction")
        store.markCompleted(key)

        val coordinator = LogoutCoordinator(
            api = FakeAppApi(),
            deviceStore = FakeDeviceStore().apply { setDeviceId("device-123") },
            sessionStore = FakeSessionStore().apply { setBearerToken("t") },
            screenCacheStore = ScreenCacheStore { },
            outboxWiper = OutboxWiper { },
            syncJobsCanceller = SyncJobsCanceller { },
            feedCompletionLocalStore = store,
        )

        coordinator.logout(signOutVendorAuth = {})

        assertFalse(
            "the previous operator's optimistic completion must not survive logout",
            store.isCompleted(key),
        )
        assertEquals(emptySet<String>(), store.completedKeys.value)
    }

    @Test
    fun `yesterday's completion cannot resurface on today's due row`() {
        val store = FeedCompletionLocalStore()
        fakeToday = "2026-07-31"
        val yesterdayKey = FeedCompletionLocalStore.key("shed-1", null, 1, "feed_direction")
        store.markCompleted(yesterdayKey)

        fakeToday = "2026-08-01"
        val todayKey = FeedCompletionLocalStore.key("shed-1", null, 1, "feed_direction")

        assertFalse("same shed/session/workflow is a NEW obligation today", store.isCompleted(todayKey))
        assertTrue("the key is day-scoped", yesterdayKey != todayKey)
    }

    @Test
    fun `marking today prunes stale earlier-day entries so the set stays bounded`() {
        val store = FeedCompletionLocalStore()
        fakeToday = "2026-07-31"
        store.markCompleted(FeedCompletionLocalStore.key("shed-1", null, 1, "feed_direction"))
        store.markCompleted(FeedCompletionLocalStore.key("shed-2", null, 1, "feed_direction"))

        fakeToday = "2026-08-01"
        val todayKey = FeedCompletionLocalStore.key("shed-3", null, 1, "feed_direction")
        store.markCompleted(todayKey)

        assertEquals(
            "only the current business day's entries are retained",
            setOf(todayKey),
            store.completedKeys.value,
        )
    }

    @Test
    fun `one stale parent completion does not badge sibling hidden location done`() {
        val store = FeedCompletionLocalStore()

        val pen1 = FeedCompletionLocalStore.key("castro", "1", 1, "feed_direction", "castro|legacy-partition|1")
        val pen2 = FeedCompletionLocalStore.key("castro", "2", 1, "feed_direction", "castro|legacy-partition|2")
        store.markCompleted(pen1)

        assertTrue("precondition: submitted pen is optimistically complete", store.isCompleted(pen1))
        assertFalse("sibling pen remains open until its own proof submits", store.isCompleted(pen2))
    }
}
