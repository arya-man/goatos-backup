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
        override suspend fun observeDirectionTotals(query: sg.mesha.goatos.core.data.FeedDirectionQuery) =
            kotlinx.coroutines.flow.flowOf(sg.mesha.goatos.core.common.Resource<Any>())

        override suspend fun directionRows(query: sg.mesha.goatos.core.data.FeedDirectionQuery) =
            kotlinx.coroutines.flow.flowOf(androidx.paging.PagingData.empty())

        override suspend fun observeDirectionSessionStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int) =
            kotlinx.coroutines.flow.flowOf(null)

        override suspend fun packingRows(query: sg.mesha.goatos.core.data.FeedPackingQuery) =
            kotlinx.coroutines.flow.flowOf(androidx.paging.PagingData.empty())

        override suspend fun observePackingRowStatus(shedId: String, partitionLabel: String, workflow: String, sessionNo: Int) =
            kotlinx.coroutines.flow.flowOf(null)

        override suspend fun observeTotals(query: sg.mesha.goatos.core.data.FeedPackingQuery) =
            kotlinx.coroutines.flow.flowOf(sg.mesha.goatos.core.common.Resource<Any>())

        override suspend fun penSessionCaptures(query: sg.mesha.goatos.core.data.FeedPenSessionCaptureQuery) = null

        override suspend fun fetchDirectionSessionStatus(parkId: String?, shedId: String, partitionLabel: String?, workflow: String, sessionNo: Int, targetDate: String): String? = null

        override suspend fun fetchProofDownloadUrl(proofId: String): String? = null

        override suspend fun probeDirectionSummary(query: sg.mesha.goatos.core.data.FeedDirectionQuery): Boolean = false
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

        // Manually call reconcileFeatureSuccess (normally called after outbox item succeeds)
        // This is the fix: before, reconcileFeatureSuccess didn't handle FEED_DISTRIBUTION_COMPLETE
        // and Room cache was never updated. Now it updates.
        val privateMethod = engine::class.java.getDeclaredMethod(
            "reconcileFeatureSuccess",
            OutboxEntity::class.java
        )
        privateMethod.isAccessible = true
        privateMethod.invoke(engine, item)

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

        val privateMethod = engine::class.java.getDeclaredMethod(
            "reconcileFeatureSuccess",
            OutboxEntity::class.java
        )
        privateMethod.isAccessible = true
        privateMethod.invoke(engine, item)

        val key = "shed-2|2|packing|3"
        assertEquals(
            "After successful feed packing completion, Room cache should show pending_verification",
            "pending_verification",
            updatedSessions[key]
        )
    }
}
