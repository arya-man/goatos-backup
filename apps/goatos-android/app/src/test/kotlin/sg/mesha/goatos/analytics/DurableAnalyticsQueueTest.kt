package sg.mesha.goatos.analytics

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner

/**
 * P2 backend-analytics-durability: proves the queue itself is durable, ordered, and capped,
 * independent of [BackendAnalyticsAdapter]'s wiring (covered separately in
 * [BackendAnalyticsAdapterTest]).
 */
@RunWith(RobolectricTestRunner::class)
class DurableAnalyticsQueueTest {

    private lateinit var context: Context

    @Before
    fun setUp() {
        context = ApplicationProvider.getApplicationContext()
    }

    private fun event(id: String, name: String = "sync_write_dead") = QueuedAnalyticsEvent(
        clientEventId = id,
        eventName = name,
        properties = mapOf("reason" to "attempts_exhausted"),
        clientEventTimeMs = 1_000L,
        flavor = "dev",
        appVersionName = "1.0",
        appVersionCode = 1,
    )

    @Test
    fun `an event emitted while offline is persisted`() = runTest {
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())

        queue.enqueue(event("e1"))

        assertEquals(1, queue.size())
    }

    @Test
    fun `draining sends persisted entries once and removes them on success`() = runTest {
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        queue.enqueue(event("e1"))
        queue.enqueue(event("e2"))

        val sent = mutableListOf<String>()
        queue.drain { queued ->
            sent += queued.clientEventId
            true
        }

        assertEquals(listOf("e1", "e2"), sent)
        assertEquals(0, queue.size())

        // A second drain of an already-empty queue sends nothing more -- proves entries are not
        // resent once successfully drained.
        val secondDrainSent = mutableListOf<String>()
        queue.drain { queued ->
            secondDrainSent += queued.clientEventId
            true
        }
        assertTrue(secondDrainSent.isEmpty())
    }

    @Test
    fun `drain stops at the first failure and preserves order for the next attempt`() = runTest {
        val queue = DurableAnalyticsQueue(context, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())
        queue.enqueue(event("e1"))
        queue.enqueue(event("e2"))
        queue.enqueue(event("e3"))

        val attempted = mutableListOf<String>()
        queue.drain { queued ->
            attempted += queued.clientEventId
            // Only the first entry succeeds; the queue must stop there rather than skipping ahead.
            queued.clientEventId == "e1"
        }

        // e1 succeeds; e2 is ATTEMPTED and fails (you cannot know it fails without trying);
        // the drain stops there, never reaching e3.
        assertEquals(listOf("e1", "e2"), attempted)
        assertEquals(2, queue.size())

        val secondAttempt = mutableListOf<String>()
        queue.drain { queued ->
            secondAttempt += queued.clientEventId
            true
        }
        assertEquals(listOf("e2", "e3"), secondAttempt)
        assertEquals(0, queue.size())
    }

    @Test
    fun `queue is capped and drops the oldest entries`() = runTest {
        val queue = DurableAnalyticsQueue(context, maxEntries = 3, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())

        repeat(5) { i -> queue.enqueue(event("e$i")) }

        assertEquals(3, queue.size())

        val remaining = mutableListOf<String>()
        queue.drain { queued ->
            remaining += queued.clientEventId
            true
        }
        // e0/e1 were dropped as the oldest; e2/e3/e4 survive, in order.
        assertEquals(listOf("e2", "e3", "e4"), remaining)
    }

    @Test
    fun `queue with no context is a safe no-op`() = runTest {
        val queue = DurableAnalyticsQueue(context = null, ioDispatcher = kotlinx.coroutines.test.UnconfinedTestDispatcher())

        queue.enqueue(event("e1"))
        assertEquals(0, queue.size())

        val sent = mutableListOf<String>()
        queue.drain { queued -> sent += queued.clientEventId; true }
        assertTrue(sent.isEmpty())
    }
}
