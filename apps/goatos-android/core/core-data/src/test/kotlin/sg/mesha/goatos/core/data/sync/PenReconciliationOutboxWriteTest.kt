package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.DispatcherProvider
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.CountsPenReconciliationCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import java.io.IOException

/**
 * The must-not-double-SUBMIT contract for the pen reconciliation "Mark done"
 * (PEN_RECONCILIATION_COMPLETE) write — docs/decisions/pen-reconciliation.md.
 *
 * These tests pin, end to end through the REAL outbox (no stubbed repository), that:
 *  1. the stored idempotency key reaches the backend VERBATIM on every retry — never regenerated,
 *     which is what lets a server-committed-but-client-unrecorded attempt dedupe onto the
 *     original submission instead of queueing a second verification;
 *  2. re-enqueuing the same card under the same key is a no-op returning the ORIGINAL row;
 *  3. the completion resolves and sends the coupled video upload's proof_ref on every attempt.
 */
class PenReconciliationOutboxWriteTest {

    private val unconfinedDispatchers = object : DispatcherProvider {
        override val io = Dispatchers.Unconfined
        override val default = Dispatchers.Unconfined
        override val main = Dispatchers.Unconfined
    }

    /** Captures the header key each complete call carried, and can fail on demand. */
    private class PenReconciliationApi(delegate: AppApi = FakeAppApi()) : AppApi by delegate {
        val completeKeys = mutableListOf<String>()
        val completeCardIds = mutableListOf<String>()
        val completeProofRefs = mutableListOf<String>()
        var failuresRemaining = 0

        override suspend fun completeCountsPenReconciliationCard(
            cardId: String,
            idempotencyKey: String,
            proofRef: String,
        ): CountsPenReconciliationCompleteResponseDto {
            completeKeys += idempotencyKey
            completeCardIds += cardId
            completeProofRefs += proofRef
            if (failuresRemaining > 0) {
                failuresRemaining--
                throw IOException("network down")
            }
            return CountsPenReconciliationCompleteResponseDto(cardId = cardId, status = "pending_verification")
        }
    }

    // A cancellable app scope so a fire-and-forget drain launched by an enqueue never outlives the
    // test and surfaces as an UncaughtExceptionsBeforeTest in a LATER test.
    private val appScope = CoroutineScope(Dispatchers.Unconfined + Job())

    @After
    fun tearDown() = appScope.cancel()

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
            appScope = appScope,
            dispatchers = unconfinedDispatchers,
            clock = { 0L },
        )
    }

    /**
     * Enqueues the MANDATORY video's PROOF_UPLOAD on [group] and drains it to SUCCEEDED, returning
     * its outbox id. The completion couples to this id and the sync engine resolves the uploaded
     * proof_id from it. Same group => the proof drains before the completion.
     */
    private suspend fun DefaultSyncRepository.enqueueSyncedProof(group: String): String {
        val request = ProofUploadRequestDto(
            proofType = "video",
            mimeType = "video/mp4",
            scopeType = "shed",
            scopeId = "shed-1",
            subjectType = "shed",
            subjectId = "shed-1",
        )
        val result = enqueueProofUpload(
            groupKey = group,
            idempotencyKey = "counts-pen-reconciliation-proof:$group:return",
            request = request,
            localFilePath = "/tmp/$group.mp4",
            durationMs = 1000,
        )
        return (result as AppResult.Ok).value
    }

    @Test
    fun `completion payload round trips its proof coupling`() {
        val payload = PenReconciliationCompletePayload(
            cardId = "card-1",
            proofOutboxItemId = "proof-return",
        )

        val decoded = syncJson.decodeFromString<PenReconciliationCompletePayload>(syncJson.encodeToString(payload))

        assertEquals(payload, decoded)
    }

    @Test
    fun `completion reuses the same idempotency key across retries`() = runBlocking {
        val api = PenReconciliationApi()
        val repo = repository(api)
        // The mandatory video uploads first (same group), so the completion can resolve proof_id.
        val proofId = repo.enqueueSyncedProof("card-1")
        // First completion attempt fails at the transport layer; the row backs off and retries.
        api.failuresRemaining = 1

        val enqueued = repo.enqueuePenReconciliationComplete(
            groupKey = "card-1",
            idempotencyKey = "counts-pen-reconciliation-complete:card-1",
            proofOutboxItemId = proofId,
        )
        assertTrue(enqueued is AppResult.Ok)
        repo.retry((enqueued as AppResult.Ok).value)

        assertEquals(2, api.completeKeys.size)
        // The whole point: attempt 2 carries the SAME key, so the backend recognises it as a
        // replay of the first submission instead of queueing a second verification.
        assertEquals("counts-pen-reconciliation-complete:card-1", api.completeKeys[0])
        assertEquals(api.completeKeys[0], api.completeKeys[1])
        assertEquals(listOf("card-1", "card-1"), api.completeCardIds)
        // Every attempt carries the resolved video proof_id from the coupled upload.
        assertEquals(
            listOf(
                "fake-proof-counts-pen-reconciliation-proof:card-1:return",
                "fake-proof-counts-pen-reconciliation-proof:card-1:return",
            ),
            api.completeProofRefs,
        )
    }

    @Test
    fun `re-enqueuing the same completion returns the original row instead of duplicating`() = runBlocking {
        val api = PenReconciliationApi()
        // Offline: the row stays queued so a second enqueue hits the idempotency guard rather
        // than an already-drained row.
        val repo = repository(api, online = false)

        val first = repo.enqueuePenReconciliationComplete(
            "card-1",
            "counts-pen-reconciliation-complete:card-1",
            proofOutboxItemId = "proof-item-1",
        )
        val second = repo.enqueuePenReconciliationComplete(
            "card-1",
            "counts-pen-reconciliation-complete:card-1",
            proofOutboxItemId = "proof-item-1",
        )

        assertTrue(first is AppResult.Ok)
        assertTrue(second is AppResult.Ok)
        assertEquals((first as AppResult.Ok).value, (second as AppResult.Ok).value)
        assertEquals("exactly one queued completion", 1, repo.observeStatus().value.items.size)
    }
}
