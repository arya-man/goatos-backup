package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CountsApprovalRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsApprovalCaptureDto
import sg.mesha.goatos.core.network.dto.CountsApprovalCaptureMediaDto
import sg.mesha.goatos.core.network.dto.CountsApprovalCaptureRowDto
import sg.mesha.goatos.core.network.dto.CountsApprovalListItemDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.counts.ApprovalCaptureMediaUi
import sg.mesha.goatos.feature.counts.ApprovalCaptureRowUi
import sg.mesha.goatos.feature.counts.ApprovalEvent

/**
 * The approver sees what the Add birth / Add death form captured (maintainer decision 4,
 * 2026-09-16): every answer in farm words grouped by section, the older-app note, the verifier's
 * verdict on the report proof, and every proof under its title -- a proof is loaded only when the
 * approver taps it (a signed URL is short-lived and never cached).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ApprovalViewModelCaptureRowsTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `capture rows, media, missing note and review state map verbatim`() {
        val row = CountsApprovalListItemDto(
            approvalRequestId = "req-1", requestType = "birth", status = "pending", raisedAt = "2026-09-16T03:30:00Z",
            capture = CountsApprovalCaptureDto(
                versionLabel = "v2",
                rows = listOf(CountsApprovalCaptureRowDto(label = "How was the delivery?", value = "Assisted", group = "At report")),
                media = listOf(CountsApprovalCaptureMediaDto(proofId = "proof-1", label = "Newborns with the mother", kind = "photo")),
                missingNote = "Pen clip",
            ),
            captureReviewStatus = "rework",
            captureReviewReason = "Mother's face not visible",
        ).toRowUi()
        assertEquals(listOf(ApprovalCaptureRowUi(label = "How was the delivery?", value = "Assisted", group = "At report")), row.captureRows)
        assertEquals(listOf(ApprovalCaptureMediaUi(proofId = "proof-1", label = "Newborns with the mother", isPhoto = true)), row.captureMedia)
        assertEquals("Pen clip", row.captureMissingNote)
        assertEquals("rework", row.captureReviewStatus)
        assertEquals("Mother's face not visible", row.captureReviewReason)

        val plain = CountsApprovalListItemDto(approvalRequestId = "req-2", requestType = "death", status = "pending", raisedAt = "x").toRowUi()
        assertTrue(plain.captureRows.isEmpty() && plain.captureMedia.isEmpty())
        assertEquals("", plain.captureMissingNote)
    }

    @Test
    fun `tapping a proof resolves its URL once and a failure is shown`() = runTest(dispatcher) {
        val repository = MediaApprovalRepository(urls = mapOf("proof-1" to "https://signed/proof-1"))
        val viewModel = ApprovalViewModel(repository, MediaSyncRepository(), NoopCaptureAnalytics(), NoopCaptureCrashReporter(), SavedStateHandle())

        viewModel.onEvent(ApprovalEvent.OpenCaptureMedia("proof-1"))
        advanceUntilIdle()
        assertEquals("https://signed/proof-1", viewModel.state.value.openedMediaUrls["proof-1"])
        viewModel.onEvent(ApprovalEvent.OpenCaptureMedia("proof-1"))
        advanceUntilIdle()
        assertEquals("an opened proof is not re-fetched", 1, repository.lookups)

        viewModel.onEvent(ApprovalEvent.OpenCaptureMedia("missing"))
        advanceUntilIdle()
        assertNull(viewModel.state.value.openedMediaUrls["missing"])
        assertEquals(setOf("missing"), viewModel.state.value.failedMediaIds)
    }
}

private class MediaApprovalRepository(private val urls: Map<String, String>) : CountsApprovalRepository {
    var lookups = 0
    override fun approvals(status: String): Flow<PagingData<CountsApprovalListItemDto>> = flowOf(PagingData.empty())
    override suspend fun forgetDecided(approvalRequestId: String) = Unit
    override suspend fun proofDownloadUrl(proofId: String): String? {
        lookups++
        return urls[proofId]
    }
}

private class MediaSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}

private class NoopCaptureAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class NoopCaptureCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun log(message: String) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}
