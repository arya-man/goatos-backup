package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationDecision
import sg.mesha.goatos.core.network.dto.VerificationVerdictResponseDto
import sg.mesha.goatos.core.network.dto.VerificationCloseSubmissionResponseDto
import java.io.IOException

/**
 * [DefaultSyncRepository] tests run every dispatcher (including [appScope]) on
 * [Dispatchers.Unconfined]: with no real IO/delay in [FakeOutboxStore]/[ScriptedAppApi], an
 * Unconfined `launch`/`withContext` runs eagerly on the calling thread up to its next real
 * suspension point, so an enqueue's triggered drain observably completes before the
 * `suspend fun enqueue*` call returns — deterministic without `kotlinx-coroutines-test`
 * (not in `gradle/libs.versions.toml`; see [SyncEngine]'s KDoc for the broader missing-deps
 * note).
 */
class SyncRepositoryTest {

    private val unconfinedDispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    private fun repository(
        store: OutboxStore = FakeOutboxStore(),
        api: ScriptedAppApi = ScriptedAppApi(),
        online: Boolean = true,
    ): DefaultSyncRepository {
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

    private fun submitRequest(key: String) = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = key)

    @Test
    fun `enqueue is reflected in observeStatus immediately (and drains against the fake api)`() = runBlocking {
        val repo = repository()
        assertEquals(0, repo.observeStatus().value.items.size)

        val result = repo.enqueueShedSubmit(
            taskId = "task-1",
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            request = submitRequest("key-1"),
        )

        assertTrue(result is AppResult.Ok)
        val status = repo.observeStatus().value
        assertEquals(1, status.items.size)
        assertEquals(SyncItemStatus.SUCCEEDED, status.items.first().status)
        assertTrue(status.lastSyncAt != null)
    }

    @Test
    fun `re-enqueuing the SAME idempotency key returns the existing row, never a duplicate`() = runBlocking {
        val store = FakeOutboxStore()
        val repo = repository(store = store)
        val request = submitRequest("key-1")

        val first = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", request)
        val second = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", request)

        val firstId = (first as AppResult.Ok).value
        val secondId = (second as AppResult.Ok).value
        assertEquals(firstId, secondId)
        assertEquals(1, repo.observeStatus().value.items.size)
    }

    @Test
    fun `enqueueVerificationVerdict is durable and drains against the fake api`() = runBlocking {
        val api = ScriptedAppApi().apply { submitVerificationVerdictFn = { _, _, _ -> VerificationVerdictResponseDto() } }
        val repo = repository(api = api)

        val result = repo.enqueueVerificationVerdict(
            itemId = "item-1",
            decision = VerificationDecision.REJECTED,
            reason = "Operator not in frame",
            rowVersion = 1,
        )

        assertTrue(result is AppResult.Ok)
        val status = repo.observeStatus().value
        assertEquals(1, status.items.size)
        assertEquals(SyncItemStatus.SUCCEEDED, status.items.first().status)
        assertEquals(1, api.verdictCalls.size)
    }

    @Test
    fun `drive close is durable and grouped by submission`() = runBlocking {
        val api = ScriptedAppApi().apply {
            closeVerificationSubmissionFn = { _, _ -> VerificationCloseSubmissionResponseDto() }
        }
        val repo = repository(api = api)

        val result = repo.enqueueVerificationSubmissionClose("submission-1")

        assertTrue(result is AppResult.Ok)
        val status = repo.observeStatus().value
        assertEquals(1, status.items.size)
        assertEquals(SyncItemStatus.SUCCEEDED, status.items.first().status)
        assertEquals(listOf("submission-1" to "submission-1-drive-close"), api.closeSubmissionCalls)
    }

    @Test
    fun `re-enqueuing the SAME idempotency key with a different payload is rejected`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val repo = repository(store = store, api = api)

        val first = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", submitRequest("key-1"))
        val changedPayload = SubmitTaskRequestDto(sopVersionId = "sop-2", idempotencyKey = "key-1")
        val second = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", changedPayload)

        assertTrue(first is AppResult.Ok)
        assertTrue(second is AppResult.Err)
        assertEquals(1, repo.observeStatus().value.items.size)
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `retry re-arms a failed item and reuses the same idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        var callCount = 0
        api.submitAppTaskFn = { _, _, _ ->
            callCount++
            if (callCount == 1) throw IOException("down")
            sg.mesha.goatos.core.network.dto.SubmissionResponseDto(
                submission = sg.mesha.goatos.core.network.dto.SubmissionSummaryDto(
                    validationReport = sg.mesha.goatos.core.network.dto.ValidationReportDto(valid = true),
                ),
            )
        }
        val repo = repository(store = store, api = api)

        val enqueueResult = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", submitRequest("key-1"))
        val itemId = (enqueueResult as AppResult.Ok).value

        var item = repo.observeStatus().value.items.first { it.id == itemId }
        assertEquals(SyncItemStatus.FAILED, item.status)
        assertEquals(1, item.attemptCount)

        val retryResult = repo.retry(itemId)
        assertTrue(retryResult is AppResult.Ok)

        item = repo.observeStatus().value.items.first { it.id == itemId }
        assertEquals(SyncItemStatus.SUCCEEDED, item.status)
        assertEquals(listOf("key-1", "key-1"), api.submitCalls.map { it.second })
    }

    @Test
    fun `offline is reported in observeStatus and enqueue does not drain until reconnect`() = runBlocking {
        // Standalone setup (not the `repository()` helper): the engine's connectivity check
        // must be the SAME live cell the test flips to simulate reconnect — the helper fixes
        // `online` once at construction, which can't model a state change mid-test.
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        var isOnline = false
        val gate = ConnectivityGate { isOnline }
        val engine = SyncEngine(store, api, connectivityGate = gate, dispatchers = unconfinedDispatchers, clock = { 0L })
        val repo = DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = gate,
            appScope = CoroutineScope(Dispatchers.Unconfined),
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )

        val result = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", submitRequest("key-1"))
        assertTrue(result is AppResult.Ok)

        var status = repo.observeStatus().value
        assertTrue(!status.online)
        assertEquals(SyncItemStatus.QUEUED, status.items.first().status)
        assertEquals(0, api.submitCalls.size)

        // Simulate the OS network becoming available: flip the live gate the engine checks
        // AND the repo's display flag (exactly what ConnectivitySyncTrigger's callback does
        // in production), then re-trigger a drain.
        isOnline = true
        repo.notifyConnectivityChanged(true)
        repo.triggerDrain()

        status = repo.observeStatus().value
        assertTrue(status.online)
        assertEquals(SyncItemStatus.SUCCEEDED, status.items.first().status)
    }

    @Test
    fun `connectivity trigger forwards the validated transition exactly once`() {
        class FakeConnectivitySource : ConnectivitySource {
            var listener: ((Boolean) -> Unit)? = null
            var closed = false

            override fun start(onChange: (Boolean) -> Unit): AutoCloseable {
                listener = onChange
                return AutoCloseable {
                    closed = true
                    listener = null
                }
            }

            fun emit(online: Boolean) {
                listener?.invoke(online)
            }
        }

        val source = FakeConnectivitySource()
        val events = mutableListOf<Boolean>()
        val trigger = ConnectivitySyncTrigger(source) { events += it }

        trigger.start()
        source.emit(false)
        source.emit(false)
        source.emit(true)
        source.emit(true)

        assertEquals(listOf(false, true), events)

        trigger.stop()
        assertTrue(source.closed)
    }

    @Test
    fun `retrying an unknown item id is an error, never a silent no-op`() = runBlocking {
        val repo = repository()

        val result = repo.retry("does-not-exist")

        assertTrue(result is AppResult.Err)
    }
}
