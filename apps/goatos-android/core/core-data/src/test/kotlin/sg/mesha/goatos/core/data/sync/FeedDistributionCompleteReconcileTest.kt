package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.FeedDistributionCompleteResponseDto
import sg.mesha.goatos.core.network.dto.FeedPackingCompleteResponseDto

/**
 * Test that SyncEngine.reconcileFeatureSuccess updates the Room cache with the new session
 * status after a successful feed distribution/packing completion submission.
 *
 * RED test (previously failed because reconcileFeatureSuccess didn't handle FEED_DISTRIBUTION_COMPLETE):
 * After submit succeeds, the list's Room cache should show pending_verification, not the old pending status.
 *
 * GREEN after fix: Room cache is updated immediately after the outbox item succeeds.
 */
class FeedDistributionCompleteReconcileTest {

    private val updatedSessions = mutableMapOf<String, String>()

    private fun createFakeFeedRepository() = object : sg.mesha.goatos.core.data.FeedRepository {
        override suspend fun persistDirectionSessionStatus(
            shedId: String,
            partitionLabel: String,
            workflow: String,
            sessionNo: Int,
            lifecycleStatus: String,
        ) {
            val key = "$shedId|$partitionLabel|$workflow|$sessionNo"
            updatedSessions[key] = lifecycleStatus
        }

        override suspend fun persistPackingRowStatus(
            shedId: String,
            partitionLabel: String,
            workflow: String,
            sessionNo: Int,
            lifecycleStatus: String,
        ) {
            val key = "$shedId|$partitionLabel|$workflow|$sessionNo"
            updatedSessions[key] = lifecycleStatus
        }

        // Other methods not needed for this test
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

        override fun observeWastageTotals(query: sg.mesha.goatos.core.data.FeedWastageQuery) =
            kotlinx.coroutines.flow.flowOf(sg.mesha.goatos.core.common.Resource<sg.mesha.goatos.core.network.dto.FeedWastageWorklistPageDto>(data = null))

        override fun wastageRows(query: sg.mesha.goatos.core.data.FeedWastageQuery) =
            kotlinx.coroutines.flow.flowOf(androidx.paging.PagingData.empty<sg.mesha.goatos.core.network.dto.FeedWastageRowDto>())

        override fun observeWastageRowStatus(shedId: String, partitionLabel: String, workflow: String) =
            kotlinx.coroutines.flow.flowOf<String?>(null)

        override suspend fun fetchWastageRowStatus(parkId: String, shedId: String, partitionLabel: String, targetDate: String): String? = null

        override suspend fun persistWastageRowStatus(
            shedId: String,
            partitionLabel: String,
            workflow: String,
            lifecycleStatus: String,
        ) {
            val key = "$shedId|$partitionLabel|$workflow"
            updatedSessions[key] = lifecycleStatus
        }
    }

    @Test
    fun `reconcileFeatureSuccess updates Room cache when feed distribution completion succeeds`() = runBlocking {
        val feedRepository = createFakeFeedRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            feedRepository = feedRepository,
        )

        // Simulate a successful feed distribution completion
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
            status = "pending_verification", // This is what the backend returns
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

        // Drive the REAL production path: seed the store with an already-SUCCEEDED row and run a
        // drain pass. drainOnce()'s startup reconcile loop
        // (`store.observeRecentTerminals(...).filter{SUCCEEDED}.forEach{reconcileFeatureSuccess}`)
        // is exactly what runs after a process restart with a pending-reconcile row, and it is the
        // one path `private fun reconcileFeatureSuccess` cannot be invoked without going through
        // (a raw reflective call to a `suspend fun` needs a `Continuation` argument it never had
        // here — the original committed version of this test called
        // `getDeclaredMethod("reconcileFeatureSuccess", OutboxEntity::class.java)` with ONE
        // parameter, which does not exist for a compiled suspend fun and threw
        // NoSuchMethodException on every run; this test was never actually green).
        store.insert(item)
        engine.drainOnce()

        // Verify the Room cache was updated with the new status
        val key = "shed-1|1|distribution|2"
        assertEquals(
            "After successful feed distribution completion, Room cache should show pending_verification",
            "pending_verification",
            updatedSessions[key]
        )
    }

    @Test
    fun `reconcileFeatureSuccess updates Room cache when feed packing completion succeeds`() = runBlocking {
        val feedRepository = createFakeFeedRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            feedRepository = feedRepository,
        )

        val packingPayload = FeedPackingCompletePayload(
            parkId = "park-1",
            shedId = "shed-2",
            partitionLabel = "2",
            sessionNo = 3,
            targetDate = "2026-08-16",
            workflow = "packing",
            packingProofOutboxItemId = "proof-4",
        )

        val response = FeedPackingCompleteResponseDto(
            completionId = "completion-2",
            status = "pending_verification",
            newlyPending = true,
        )

        val item = OutboxEntity(
            id = "item-2",
            opType = OutboxOpType.FEED_PACKING_COMPLETE.name,
            groupKey = "shed-2",
            idempotencyKey = "key-2",
            payloadJson = syncJson.encodeToString(packingPayload),
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

        store.insert(item)
        engine.drainOnce()

        val key = "shed-2|2|packing|3"
        assertEquals(
            "After successful feed packing completion, Room cache should show pending_verification",
            "pending_verification",
            updatedSessions[key]
        )
    }

    /**
     * The LIVE dispatch path, not the restart-repair path the two tests above drive. Found
     * on-device 2026-08-19: an operator submitted a wastage video, the outbox row SUCCEEDED,
     * and the worklist card stayed "Pending" until the NEXT drain pass — because processItem
     * called `reconcileFeatureSuccess(item)` with the PRE-dispatch entity, whose `resultJson`
     * is still null, so the `item.resultJson?.let { ... }` arm silently skipped. The stored row
     * had the resultJson; the in-memory argument did not. Here the item starts QUEUED with no
     * resultJson — exactly the live shape — and ONE drain pass must reconcile the Room cache.
     */
    @Test
    fun `live drain pass reconciles a wastage completion in the same pass it succeeds`() = runBlocking {
        val feedRepository = createFakeFeedRepository()
        val store = FakeOutboxStore()
        val engine = SyncEngine(
            store = store,
            api = ScriptedAppApi(),
            connectivityGate = { true },
            feedRepository = feedRepository,
        )

        // The mandatory wastage video's PROOF_UPLOAD row, already uploaded (SUCCEEDED with a
        // proof id) so the completion dispatch can resolve its proof ref.
        val proofResult = sg.mesha.goatos.core.network.dto.ProofUploadResponseDto(
            proof = sg.mesha.goatos.core.network.dto.ProofReferenceDto(proofId = "proof-id-1"),
        )
        store.insert(
            OutboxEntity(
                id = "proof-1",
                opType = OutboxOpType.PROOF_UPLOAD.name,
                groupKey = "shed-1",
                idempotencyKey = "proof-key-1",
                payloadJson = "{}",
                status = OutboxStatus.SUCCEEDED.name,
                attemptCount = 1,
                maxAttempts = 3,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = Long.MAX_VALUE,
                lastError = null,
                resultJson = syncJson.encodeToString(proofResult),
            ),
        )
        // The wastage completion, QUEUED with resultJson = null — the live pre-dispatch shape.
        val payload = FeedWastageCompletePayload(
            parkId = "park-1",
            shedId = "shed-1",
            partitionLabel = "Part 3",
            targetDate = "2026-08-19",
            wastageProofOutboxItemId = "proof-1",
        )
        store.insert(
            OutboxEntity(
                id = "item-wastage",
                opType = OutboxOpType.FEED_WASTAGE_COMPLETE.name,
                groupKey = "shed-1",
                idempotencyKey = "key-wastage",
                payloadJson = syncJson.encodeToString(payload),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = 3,
                conflict = false,
                createdAt = 1L,
                updatedAt = 1L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )

        // ONE pass: dispatch succeeds (FakeAppApi returns pending_verification) and the SAME pass
        // must reconcile the Room mirror — the operator is looking at the worklist right now.
        engine.drainOnce()

        assertEquals(
            "The pass that syncs a wastage completion must reconcile the Room row in that same " +
                "pass — deferring to the next pass leaves the card on Pending until app restart",
            "pending_verification",
            updatedSessions["shed-1|Part 3|experiment"],
        )
    }
}
