package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedWastageCompleteAnalyticsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Test
    fun `proof upload terminal state emits analytics with proof and outbox ids`() = runTest(dispatcher) {
        val sync = WastageTerminalSyncRepository()
        val proofs = FakeProofCaptureRepository()
        val drafts = InMemoryCaptureDraftRepository()
        val analytics = RecordingAnalytics()
        val proofRowId = "wastage-proof-local-1"
        val proofOutboxId = "wastage-proof-outbox-1"
        val groupKey = "feed-wastage:shed-1:a:0:experiment:2026-08-13"
        proofs.seedProofs(
            ProofCaptureRow(
                id = proofRowId,
                fieldKey = "feed_wastage_video",
                proofSubject = ProofSubject.SHED,
                subjectId = "shed-1",
                localUri = "/proof/wastage.mp4",
                mimeType = "video/mp4",
                caption = null,
                rfidTag = null,
                capturedAtMs = 1L,
                capturedStartMs = 1L,
                capturedEndMs = 2L,
                capturedByPrincipalId = null,
                syncStatus = CaptureSyncStatus.PENDING,
                serverProofId = null,
                outboxItemId = proofOutboxId,
                lastError = null,
                partitionKey = "whole",
            ),
        )
        drafts.putProof(CaptureFlow.FEED_WASTAGE, groupKey, "video", proofOutboxId)

        FeedWastageCompleteViewModel(
            syncRepository = sync,
            proofCaptureSource = FakeProofCaptureSource(),
            proofCaptureRepository = proofs,
            photoCaptureSource = sg.mesha.goatos.capture.FakePhotoCaptureSource(),
            appContext = androidx.test.core.app.ApplicationProvider.getApplicationContext(),
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            drafts = drafts,
            feedRepository = FakeFeedRepository(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    "shed_id" to "shed-1",
                    "target_date" to "2026-08-13",
                    "shed_label" to "Shed 1",
                    "park_label" to "Farm 1",
                    "partition_label" to "A",
                    "experiment_arm" to "Experiment",
                    "capture_allowed" to "true",
                    "lifecycle_status" to "open",
                ),
            ),
        )
        advanceUntilIdle()

        sync.markProofTerminal(proofOutboxId, SyncItemStatus.SUCCEEDED)
        advanceUntilIdle()

        val event = analytics.events.last { it.name == AnalyticsEvents.FEED_WASTAGE_PROOF_UPLOAD_SYNCED }
        assertEquals("proof_upload_sync", event.props[AnalyticsEvents.Params.ACTION])
        assertEquals("success", event.props[AnalyticsEvents.Params.OUTCOME])
        assertEquals(proofRowId, event.props["local_proof_row_id"])
        assertEquals(proofOutboxId, event.props[AnalyticsEvents.Params.PROOF_OUTBOX_ITEM_ID])
    }
}

private class WastageTerminalSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    fun markProofTerminal(itemId: String, itemStatus: SyncItemStatus, error: String? = null) {
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = itemId,
            opType = "test",
            idempotencyKey = "test-key",
            groupKey = "test-group",
            status = itemStatus,
            attemptCount = 0,
            maxAttempts = 3,
            conflict = false,
            createdAt = System.currentTimeMillis(),
            updatedAt = System.currentTimeMillis(),
            lastError = error,
        )
    }

    override fun observeStatus(): kotlinx.coroutines.flow.StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> =
        items.getOrPut(itemId) { MutableStateFlow(null) }

    override suspend fun enqueueFeedWastageComplete(
        groupKey: String,
        idempotencyKey: String,
        parkId: String?,
        shedId: String,
        partitionLabel: String?,
        targetDate: String,
        wastageProofOutboxItemId: String,
        slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>,
        answers: kotlinx.serialization.json.JsonObject
    ): AppResult<String> = error("unused")

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDistributionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, distributionProofOutboxItemId: String?, feedWeightProofOutboxItemId: String?, waterProofOutboxItemId: String?, feedWeightProofRef: String?, distributionProofRef: String?, waterProofRef: String?, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDirectionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, sessionNo: Int, targetDate: String, workflow: String): AppResult<String> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
