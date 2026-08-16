package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.common.OutboxTelemetryEvent
import sg.mesha.goatos.core.common.OutboxTelemetryReporter
import sg.mesha.goatos.core.common.OutboxWritePhase
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteResponseDto

/**
 * Guards the failure-handling half of the FEED_DISTRIBUTION_COMPLETE/FEED_PACKING_COMPLETE Room
 * cache reconcile: the outbox item is ALREADY SUCCEEDED on the server by the time
 * `reconcileFeatureSuccess` runs, so a local Room-write failure must never:
 *   1. propagate out of `reconcileFeatureSuccess` (it runs unguarded in `drainOnce`'s bulk
 *      reconcile loop for recently-terminal rows, and inside `processItem`'s try/catch where an
 *      escaping exception would incorrectly re-record an already-succeeded item as failed), and
 *   2. be swallowed without a trace (the repo's golden rule: never swallow silently).
 *
 * RED (pre-fix): the feedRepository call was not exception-guarded, so a persist failure threw
 * straight out of `reconcileFeatureSuccess`.
 * GREEN (post-fix): the failure is caught locally and reported via [OutboxTelemetryReporter].
 */
class FeedCompletionReconcileErrorHandlingTest {

    private class ThrowingFeedRepository : sg.mesha.goatos.core.data.FeedRepository {
        override suspend fun persistDirectionSessionStatus(
            shedId: String,
            partitionLabel: String,
            workflow: String,
            sessionNo: Int,
            lifecycleStatus: String,
        ): Unit = throw IllegalStateException("Room write failed")

        override suspend fun persistPackingRowStatus(
            shedId: String,
            partitionLabel: String,
            workflow: String,
            sessionNo: Int,
            lifecycleStatus: String,
        ): Unit = throw IllegalStateException("Room write failed")

        override fun observeDirectionTotals(query: sg.mesha.goatos.core.data.FeedDirectionQuery) =
            kotlinx.coroutines.flow.flowOf(sg.mesha.goatos.core.common.Resource<sg.mesha.goatos.core.network.dto.FeedDirectionPreviewPageDto>(data = null))

        override fun directionRows(query: sg.mesha.goatos.core.data.FeedDirectionQuery) =
            kotlinx.coroutines.flow.flowOf(androidx.paging.PagingData.empty<sg.mesha.goatos.core.network.dto.FeedDirectionRowDto>())

        override fun observePackingTotals(query: sg.mesha.goatos.core.data.FeedPackingQuery) =
            kotlinx.coroutines.flow.flowOf(sg.mesha.goatos.core.common.Resource<sg.mesha.goatos.core.network.dto.FeedPackingWorklistPageDto>(data = null))

        override fun observeDirectionSessionStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int) =
            kotlinx.coroutines.flow.flowOf<String?>(null)

        override fun packingRows(query: sg.mesha.goatos.core.data.FeedPackingQuery) =
            kotlinx.coroutines.flow.flowOf(androidx.paging.PagingData.empty<sg.mesha.goatos.core.network.dto.FeedPackingRowDto>())

        override fun observePackingRowStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int) =
            kotlinx.coroutines.flow.flowOf<String?>(null)

        override suspend fun penSessionCaptures(query: sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery) = null

        override suspend fun fetchDirectionSessionStatus(parkId: String, shedId: String, partitionLabel: String, workflow: String, sessionNo: Int, targetDate: String): String? = null

        override suspend fun fetchPackingRowStatus(parkId: String, shedId: String, partitionLabel: String, workflow: String, sessionNo: Int, targetDate: String): String? = null

        override suspend fun fetchProofDownloadUrl(proofId: String): String? = null

        override suspend fun probeDirectionSummary(query: sg.mesha.goatos.core.data.FeedDirectionQuery): Boolean = false
    }

    @Test
    fun `reconcileFeatureSuccess does not throw when Room persist fails, and reports it`() = runBlocking {
        val reportedEvents = mutableListOf<OutboxTelemetryEvent>()
        val telemetry = OutboxTelemetryReporter { event -> reportedEvents += event }
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            feedRepository = ThrowingFeedRepository(),
            telemetry = telemetry,
        )

        val completionPayload = FeedDistributionCompletePayload(
            parkId = "park-1",
            shedId = "shed-1",
            partitionLabel = "1",
            sessionNo = 2,
            targetDate = "2026-08-16",
            workflow = "distribution",
            feedWeightProofOutboxItemId = "proof-1",
            distributionProofOutboxItemId = "proof-2",
            waterProofOutboxItemId = "proof-3",
            feedWeightProofRef = null,
            distributionProofRef = null,
            waterProofRef = null,
        )
        val response = FeedDistributionCompleteResponseDto(
            completionId = "completion-1",
            status = "pending_verification",
            newlyPending = true,
        )
        val item = OutboxEntity(
            id = "item-1",
            opType = OutboxOpType.FEED_DISTRIBUTION_COMPLETE.name,
            groupKey = "shed-1",
            idempotencyKey = "key-1",
            payloadJson = syncJson.encodeToString(completionPayload),
            status = OutboxStatus.SUCCEEDED.name,
            attemptCount = 1,
            maxAttempts = 3,
            conflict = false,
            createdAt = 0L,
            updatedAt = 1000L,
            nextAttemptAt = Long.MAX_VALUE,
            lastError = null,
            resultJson = syncJson.encodeToString(response),
        )

        // Drive the REAL production path (see FeedDistributionCompleteReconcileTest for why raw
        // reflection on a `suspend fun` doesn't work): seed an already-SUCCEEDED row and drain.
        // A drainOnce() that returns/completes normally (vs. throwing, or leaving later groups
        // unprocessed) proves the Room-persist failure was caught locally, not propagated.
        store.insert(item)
        val completed = engine.drainOnce()
        assertTrue("drainOnce() must complete even when a post-success cache reconcile throws", completed)

        // Must not be silently swallowed: exactly one failure was reported, carrying this item's
        // id/opType/failure class.
        assertEquals(1, reportedEvents.size)
        val reported = reportedEvents.single()
        assertEquals(OutboxWritePhase.ATTEMPT_FAILED, reported.phase)
        assertEquals("item-1", reported.itemId)
        assertEquals(OutboxOpType.FEED_DISTRIBUTION_COMPLETE.name, reported.opType)
        assertTrue(reported.failureClass.orEmpty().contains("IllegalStateException"))
    }
}
