package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ScanCaptureDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureResponseDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.WeighingScopeSubmitRequestDto
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
        weighingTransitionEpochDao: sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochDao? = null,
    ): DefaultSyncRepository {
        val engine = SyncEngine(
            store = store,
            api = api,
            connectivityGate = { online },
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
            weighingTransitionEpochDao = weighingTransitionEpochDao,
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

    /** In-memory stand-in for the Room DAO, sufficient for asserting epoch-advance timing. */
    private class FakeWeighingTransitionEpochDao : sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochDao {
        private val rows = mutableMapOf<String, sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochEntity>()
        val upsertCalls = mutableListOf<sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochEntity>()

        override suspend fun get(scopeId: String): String? = rows[scopeId]?.epoch

        override suspend fun upsert(row: sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochEntity) {
            rows[row.scopeId] = row
            upsertCalls += row
        }

        override suspend fun insertIfAbsent(row: sg.mesha.goatos.core.data.weighing.WeighingTransitionEpochEntity) {
            rows.putIfAbsent(row.scopeId, row)
        }

        override suspend fun pruneOutsideNewest(keep: Int) = Unit
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

    /**
     * DEVICE-PROVEN DEFECT: a submit op whose only outbox row already reached a TERMINAL failure
     * (dead-letter conflict OR attempt-exhausted) permanently blocked ALL future submits of the
     * same task, because submit idempotency keys are stable per task ("milk-feeding-submit:
     * <taskId>"-shaped) and the unique index on [sg.mesha.goatos.core.database.outbox.OutboxEntity.idempotencyKey]
     * silently dropped a fresh enqueue under that key — no new row, no drain, no POST, no error
     * surfaced. A fresh user tap must re-open the SAME row (same idempotencyKey, so server-side
     * replay stays idempotent) back to PENDING with a reset attempt budget and the new payload, and
     * the drain loop must actually pick it up and send it.
     */
    @Test
    fun `re-enqueuing a key whose row is a terminal dead-letter reopens it and drains`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val repo = repository(store = store, api = api)
        val deadRow = OutboxEntity(
            id = "row-1",
            opType = OutboxOpType.SHED_SUBMIT.name,
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            payloadJson = "{}",
            requestFingerprint = "stale-fingerprint",
            status = OutboxStatus.FAILED.name,
            attemptCount = DEFAULT_MAX_ATTEMPTS,
            maxAttempts = DEFAULT_MAX_ATTEMPTS,
            conflict = false,
            createdAt = 0L,
            updatedAt = 0L,
            nextAttemptAt = Long.MAX_VALUE,
            lastError = "network unreachable",
        )
        store.insert(deadRow)

        val result = repo.enqueueShedSubmit(
            taskId = "task-1",
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            request = submitRequest("key-1"),
        )

        assertTrue("re-enqueue over a dead-letter row must succeed, not be silently dropped", result is AppResult.Ok)
        assertEquals("row-1", (result as AppResult.Ok).value)
        assertEquals("exactly one outbox row must exist — reopened in place, never a duplicate", 1, store.snapshot().size)
        assertTrue(
            "the actual network call must have fired: a reopened row must be drain-eligible",
            api.submitCalls.any { it.first == "task-1" },
        )
        val status = repo.observeStatus().value
        assertEquals(SyncItemStatus.SUCCEEDED, status.items.first().status)
    }

    @Test
    fun `re-enqueuing a dead-letter conflict row with a NEW payload reopens with the new payload`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val repo = repository(store = store, api = api)
        val deadRow = OutboxEntity(
            id = "row-1",
            opType = OutboxOpType.SHED_SUBMIT.name,
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            payloadJson = "{\"stale\":true}",
            requestFingerprint = "stale-fingerprint",
            status = OutboxStatus.FAILED.name,
            attemptCount = 1,
            maxAttempts = DEFAULT_MAX_ATTEMPTS,
            conflict = true, // definitive server rejection — never auto-retried, only a fresh submit re-arms it
            createdAt = 0L,
            updatedAt = 0L,
            nextAttemptAt = Long.MAX_VALUE,
            lastError = "422 validation failed",
        )
        store.insert(deadRow)

        val result = repo.enqueueShedSubmit(
            taskId = "task-1",
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            request = submitRequest("key-1-corrected"),
        )

        assertTrue("a corrected resubmit over a conflict row must succeed, never a stale IdempotencyKeyConflict", result is AppResult.Ok)
        val reopened = store.snapshot().single()
        assertEquals("key-1", reopened.idempotencyKey) // SAME key — server-side replay stays idempotent
        assertTrue("the corrected payload must replace the stale one", reopened.payloadJson.contains("key-1-corrected"))
        assertEquals(SyncItemStatus.SUCCEEDED, repo.observeStatus().value.items.first().status)
    }

    // ---------------------------------------------------------------------------------------
    // WEIGHING_SCOPE_SUBMIT outbox path (weighing-durable-state item 3): the same durable-
    // enqueue guarantees every other outbox op has already had proven above, plus the
    // submit-specific "epoch only advances on SUCCEEDED, never optimistically at enqueue" rule
    // that only this op type carries (see SyncEngine.reconcileFeatureSuccess).
    // ---------------------------------------------------------------------------------------

    private fun scopeSubmitRequest(vararg tags: String) = WeighingScopeSubmitRequestDto(tags.toList())

    @Test
    fun `enqueueWeighingScopeSubmit produces exactly one durable outbox row keyed by its idempotency key`() = runBlocking {
        val repo = repository()
        assertEquals(0, repo.observeStatus().value.items.size)

        val result = repo.enqueueWeighingScopeSubmit(
            campaignId = "campaign-1",
            campaignShedId = "shed-1",
            groupKey = "shed-1",
            idempotencyKey = "submit-key-1",
            request = scopeSubmitRequest("tag-1", "tag-2"),
        )

        assertTrue("enqueue must succeed", result is AppResult.Ok)
        val status = repo.observeStatus().value
        assertEquals("exactly one outbox row for one submit", 1, status.items.size)
        assertEquals("submit-key-1", status.items.single().idempotencyKey)
        assertEquals(
            "the fake api's default submitWeighingScope succeeds, so drain should terminalize it",
            SyncItemStatus.SUCCEEDED,
            status.items.single().status,
        )
    }

    @Test
    fun `re-invoking enqueueWeighingScopeSubmit with the SAME idempotency key never creates a duplicate outbox row`() = runBlocking {
        val store = FakeOutboxStore()
        val repo = repository(store = store)
        val request = scopeSubmitRequest("tag-1")

        val first = repo.enqueueWeighingScopeSubmit(
            campaignId = "campaign-1",
            campaignShedId = "shed-1",
            groupKey = "shed-1",
            idempotencyKey = "submit-key-1",
            request = request,
        )
        val second = repo.enqueueWeighingScopeSubmit(
            campaignId = "campaign-1",
            campaignShedId = "shed-1",
            groupKey = "shed-1",
            idempotencyKey = "submit-key-1",
            request = request,
        )

        val firstId = (first as AppResult.Ok).value
        val secondId = (second as AppResult.Ok).value
        assertEquals("re-enqueuing the same key must return the SAME outbox row id, not mint a new one", firstId, secondId)
        assertEquals(1, repo.observeStatus().value.items.size)
    }

    @Test
    fun `the local transition epoch advances ONLY after the submit reaches SUCCEEDED, never optimistically at enqueue`() = runBlocking {
        val epochDao = FakeWeighingTransitionEpochDao()
        val store = FakeOutboxStore()
        // start OFFLINE: enqueue must queue, not drain, so the "at enqueue" moment is observable
        // in isolation. repository()'s `online` is fixed at construction, so this test builds the
        // engine/repo pair directly instead, letting it flip online mid-run and drive an explicit
        // drain -- the same way SyncWorker triggers a real pass once connectivity returns.
        val online = booleanArrayOf(false)
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { online[0] },
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
            weighingTransitionEpochDao = epochDao,
        )
        val liveRepo = DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = { online[0] },
            appScope = CoroutineScope(Dispatchers.Unconfined),
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )

        liveRepo.enqueueWeighingScopeSubmit(
            campaignId = "campaign-1",
            campaignShedId = "shed-1",
            groupKey = "shed-1",
            idempotencyKey = "submit-key-1",
            request = scopeSubmitRequest("tag-1"),
        )

        assertEquals(
            "the row must be QUEUED, not yet drained, while offline",
            SyncItemStatus.QUEUED,
            liveRepo.observeStatus().value.items.single().status,
        )
        assertTrue(
            "enqueue itself must NOT advance the epoch -- only a SUCCEEDED drain may",
            epochDao.upsertCalls.isEmpty(),
        )

        online[0] = true
        engine.drainOnce()

        assertEquals(
            "after a successful drain the row is SUCCEEDED",
            SyncItemStatus.SUCCEEDED,
            liveRepo.observeStatus().value.items.single().status,
        )
        assertEquals(
            "the epoch must advance exactly once, only now that the submit is SUCCEEDED",
            1,
            epochDao.upsertCalls.size,
        )
        assertEquals("submit:campaign-1:shed-1", epochDao.upsertCalls.single().scopeId)
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
    fun `drive close is durable and grouped by vaccination batch`() = runBlocking {
        val api = ScriptedAppApi().apply {
            closeVaccinationBatchFn = { _, _ -> VerificationCloseSubmissionResponseDto() }
        }
        val repo = repository(api = api)

        val result = repo.enqueueVerificationBatchClose("batch-1")

        assertTrue(result is AppResult.Ok)
        val status = repo.observeStatus().value
        assertEquals(1, status.items.size)
        assertEquals(SyncItemStatus.SUCCEEDED, status.items.first().status)
        assertEquals(listOf("batch-1" to "batch-1-drive-close"), api.closeBatchCalls)
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
        assertEquals(null, (second as AppResult.Err).cause)
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
    fun `failed shed payload can be removed by stable key and rebuilt after process recreation`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("validation rejected") }
        }
        val repo = repository(store = store, api = api)

        val first = repo.enqueueShedSubmit("task-1", "shed-1", "key-1", submitRequest("key-1"))
        assertTrue(first is AppResult.Ok)
        assertEquals(SyncItemStatus.FAILED, repo.observeStatus().value.items.single().status)
		val recovered = repo.findOutboxItemByIdempotencyKey("key-1") as AppResult.Ok
		assertEquals((first as AppResult.Ok).value, recovered.value?.id)

        assertTrue(repo.deleteFailedOutboxItemByIdempotencyKey("key-1") is AppResult.Ok)

        api.submitAppTaskFn = { _, _, _ ->
            sg.mesha.goatos.core.network.dto.SubmissionResponseDto(
                submission = sg.mesha.goatos.core.network.dto.SubmissionSummaryDto(
                    validationReport = sg.mesha.goatos.core.network.dto.ValidationReportDto(valid = true),
                ),
            )
        }
        val corrected = SubmitTaskRequestDto(sopVersionId = "sop-2", idempotencyKey = "key-1")
        assertTrue(repo.enqueueShedSubmit("task-1", "shed-1", "key-1", corrected) is AppResult.Ok)
        assertEquals(SyncItemStatus.SUCCEEDED, repo.observeStatus().value.items.single().status)
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
    fun `local backend gate drains scan capture even when platform network is unvalidated`() = runBlocking {
        val store = FakeOutboxStore()
        val api = ScriptedAppApi()
        val calls = mutableListOf<Triple<String, String, ScanCaptureRequestDto>>()
        api.recordScanCaptureFn = { taskId, key, request ->
            calls += Triple(taskId, key, request)
            ScanCaptureResponseDto(
                capture = ScanCaptureDto(
                    captureId = "capture-1",
                    taskId = taskId,
                    fieldKey = request.fieldKey,
                    tag = request.tag,
                    goatId = request.goatId,
                    obligationId = request.obligationId,
                ),
            )
        }
        val gate = LocalBackendConnectivityGate(
            delegate = ConnectivityGate { false },
            apiBaseUrl = "http://localhost:8080/",
        )
        val engine = SyncEngine(store, api, connectivityGate = gate, dispatchers = unconfinedDispatchers, clock = { 0L })
        val repo = DefaultSyncRepository(
            store = store,
            engine = engine,
            connectivityGate = gate,
            appScope = CoroutineScope(Dispatchers.Unconfined),
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )

        val result = repo.enqueueScanCapture(
            taskId = "task-1",
            groupKey = "task-1",
            idempotencyKey = "scan:task-1:__scan_roster__:901007000504418",
            request = ScanCaptureRequestDto(
                fieldKey = "__scan_roster__",
                tag = "901007000504418",
                goatId = "goat-1",
                obligationId = "obligation-1",
                capturedAtMs = 42L,
            ),
        )

        assertTrue(result is AppResult.Ok)
        assertTrue(repo.observeStatus().value.online)
        assertEquals(SyncItemStatus.SUCCEEDED, repo.observeStatus().value.items.single().status)
        assertEquals(1, calls.size)
        assertEquals("task-1", calls.single().first)
        assertEquals("901007000504418", calls.single().third.tag)
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
