package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.CountsShiftingExecutionResponseDto
import java.io.IOException

/**
 * The must-not-double-RELOCATE contract for the shifting "Mark done" (SHIFTING_COMPLETE) write.
 *
 * Completing a movement is the write that physically relocates the animals, so a duplicate is not
 * cosmetic — it would move the herd twice. These tests pin, end to end through the REAL outbox (no
 * stubbed repository), that:
 *  1. the stored idempotency key reaches the backend VERBATIM on every retry — never regenerated,
 *     which is what lets a server-committed-but-client-unrecorded attempt dedupe onto the original
 *     relocation;
 *  2. re-enqueuing the same movement under the same key is a no-op that returns the ORIGINAL row;
 *  3. complete and cancel dispatch to their OWN routes so one can never be mistaken for the other.
 */
class ShiftingExecuteOutboxWriteTest {

    private val unconfinedDispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    /** Captures the header key each shifting-execution route was called with, and can fail on demand. */
    private class ShiftingApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        val completeKeys = mutableListOf<String>()
        val completeIds = mutableListOf<String>()
        val cancelKeys = mutableListOf<String>()
        var failuresRemaining = 0

        override suspend fun completeCountsShiftingEvent(
            shiftingEventId: String,
            idempotencyKey: String,
            destinationTag: String?,
        ): CountsShiftingExecutionResponseDto {
            completeKeys += idempotencyKey
            completeIds += shiftingEventId
            if (failuresRemaining > 0) {
                failuresRemaining--
                throw IOException("network down")
            }
            return CountsShiftingExecutionResponseDto(shiftingEventId = shiftingEventId, eventStatus = "applied")
        }

        override suspend fun cancelCountsShiftingEvent(
            shiftingEventId: String,
            idempotencyKey: String,
            reason: String,
        ): CountsShiftingExecutionResponseDto {
            cancelKeys += idempotencyKey
            return CountsShiftingExecutionResponseDto(shiftingEventId = shiftingEventId, eventStatus = "canceled")
        }
    }

    private fun repository(api: AppApi, online: Boolean = true): DefaultSyncRepository {
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = api,
            connectivityGate = { online },
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )
        return DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = { online },
            appScope = CoroutineScope(Dispatchers.Unconfined),
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )
    }

    @Test
    fun `completion reuses the same idempotency key across retries`() = runBlocking {
        val api = ShiftingApi()
        // First attempt fails at the transport layer; the row backs off and is retried.
        api.failuresRemaining = 1
        val repo = repository(api)

        val enqueued = repo.enqueueShiftingComplete(
            groupKey = "move-1",
            idempotencyKey = "counts-shifting-complete:move-1",
        )
        assertTrue(enqueued is AppResult.Ok)
        repo.retry((enqueued as AppResult.Ok).value)

        assertEquals(2, api.completeKeys.size)
        // The whole point: attempt 2 carries the SAME key, so the backend recognises it as a replay
        // of the first relocation instead of moving the herd a second time.
        assertEquals("counts-shifting-complete:move-1", api.completeKeys[0])
        assertEquals(api.completeKeys[0], api.completeKeys[1])
        assertEquals(listOf("move-1", "move-1"), api.completeIds)
    }

    @Test
    fun `re-enqueuing the same completion returns the original row instead of duplicating`() = runBlocking {
        val api = ShiftingApi()
        // Offline: the row stays queued so a second enqueue hits the idempotency guard rather than an
        // already-drained row.
        val repo = repository(api, online = false)

        val first = repo.enqueueShiftingComplete("move-1", "counts-shifting-complete:move-1")
        val second = repo.enqueueShiftingComplete("move-1", "counts-shifting-complete:move-1")

        assertTrue(first is AppResult.Ok)
        assertTrue(second is AppResult.Ok)
        assertEquals((first as AppResult.Ok).value, (second as AppResult.Ok).value)
        assertEquals("exactly one queued completion", 1, repo.observeStatus().value.items.size)
    }

    @Test
    fun `complete and cancel dispatch to their own routes`() = runBlocking {
        val api = ShiftingApi()
        val repo = repository(api)

        repo.enqueueShiftingComplete("move-1", "counts-shifting-complete:move-1")
        repo.enqueueShiftingCancel("move-2", "counts-shifting-cancel:move-2", reason = "shed flooded")

        assertEquals(listOf("counts-shifting-complete:move-1"), api.completeKeys)
        assertEquals(listOf("counts-shifting-cancel:move-2"), api.cancelKeys)
    }
}
