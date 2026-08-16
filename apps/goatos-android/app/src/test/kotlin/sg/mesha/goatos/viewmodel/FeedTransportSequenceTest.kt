package sg.mesha.goatos.viewmodel

import androidx.room.Room
import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import java.time.LocalDate
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.CaptureDraft
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.FeedTransportRepository
import sg.mesha.goatos.core.data.GoatDatabase
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionDto
import sg.mesha.goatos.core.network.dto.FeedTransportFilterOptionsDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskDto
import sg.mesha.goatos.core.network.dto.FeedTransportTaskPageDto
import sg.mesha.goatos.feature.feed.FeedDistributionProofStatus
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent
import sg.mesha.goatos.feature.feed.FeedTransportCaptureUiState
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportResultUi
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class FeedTransportSequenceTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `submit follows offline-first proof gating`() {
        assertFalse(FeedTransportCaptureUiState().submitEnabled)
        assertFalse(FeedTransportCaptureUiState(isCapturing = true, videoCaptured = true).submitEnabled)
        assertFalse(FeedTransportCaptureUiState(videoCaptured = true).submitEnabled)
        assertTrue(
            FeedTransportCaptureUiState(
                videoCaptured = true,
                videoStatus = FeedDistributionProofStatus.QUEUED,
                canSubmit = true,
            ).submitEnabled,
        )
        assertTrue(
            FeedTransportCaptureUiState(
                videoCaptured = true,
                videoStatus = FeedDistributionProofStatus.UPLOADING,
                canSubmit = true,
            ).submitEnabled,
        )
        assertFalse(
            FeedTransportCaptureUiState(
                videoCaptured = true,
                videoStatus = FeedDistributionProofStatus.FAILED,
                canSubmit = true,
            ).submitEnabled,
        )
        assertFalse(
            FeedTransportCaptureUiState(
                videoCaptured = true,
                videoStatus = FeedDistributionProofStatus.SYNCED,
                canSubmit = true,
                result = FeedTransportResultUi(FeedTransportSubmitStatus.QUEUED, "Submitted"),
            ).submitEnabled,
        )
    }

    @Test
    fun `duplicate transport filter selections do not emit duplicate analytics`() = runTest(dispatcher) {
        val database = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            GoatDatabase::class.java,
        ).allowMainThreadQueries().build()
        try {
            val api = object : AppApi by FakeAppApi() {
                override suspend fun getFeedTransportTasks(
                    businessDate: String,
                    parkId: String?,
                    shedId: String?,
                    status: String?,
                    cursor: String?,
                    limit: Int?,
                ): FeedTransportTaskPageDto = FeedTransportTaskPageDto(
                    items = listOf(
                        FeedTransportTaskDto(
                            taskId = "task-${parkId.orEmpty()}-${shedId.orEmpty()}-${status.orEmpty()}",
                            parkId = parkId ?: "park-1",
                            parkLabel = "Farm 1",
                            shedId = shedId ?: "shed-1",
                            shedLabel = "Shed 1",
                            partitionLabel = null,
                            operationalLocationDisplay = "Shed 1",
                            businessDate = businessDate,
                            status = status ?: "due",
                            scheduledAt = "2026-07-29T15:30:00+05:30",
                        ),
                    ),
                    filters = FeedTransportFilterOptionsDto(
                        parks = listOf(FeedTransportFilterOptionDto("park-1", "Farm 1")),
                        sheds = listOf(FeedTransportFilterOptionDto("shed-1", "Shed 1")),
                    ),
                )
            }
            val analytics = RecordingAnalytics()
            val viewModel = FeedTransportViewModel(
                repo = FeedTransportRepository(api, database),
                analytics = analytics,
                crashReporter = NoopCrashReporter(),
            )
            advanceUntilIdle()

            viewModel.onEvent(FeedTransportEvent.SelectShed("shed-1"))
            viewModel.onEvent(FeedTransportEvent.SelectShed("shed-1"))
            viewModel.onEvent(FeedTransportEvent.SelectStatus("completed"))
            viewModel.onEvent(FeedTransportEvent.SelectStatus("completed"))
            viewModel.onEvent(FeedTransportEvent.SelectPark("park-1"))
            viewModel.onEvent(FeedTransportEvent.SelectPark("park-1"))
            viewModel.onEvent(FeedTransportEvent.SelectDate(LocalDate.now().minusDays(1)))
            viewModel.onEvent(FeedTransportEvent.SelectDate(LocalDate.now().minusDays(1)))
            viewModel.onEvent(FeedTransportEvent.ClearFilters)
            viewModel.onEvent(FeedTransportEvent.ClearFilters)
            advanceUntilIdle()

            assertEquals(
                "re-selecting the already-active transport filter must not pollute the funnel",
                5,
                analytics.events.count { it.name == AnalyticsEvents.FEED_FILTER_APPLIED },
            )
        } finally {
            database.close()
        }
    }

    @Test
    fun `a cancelled transport re-capture keeps the existing proof`() = runTest(dispatcher) {
        val proofCaptureRepository = FakeProofCaptureRepository()
        val proofCaptureSource = FakeProofCaptureSource()
        val drafts = TransportDraftRepository()
        val sync = TransportSyncRepository()
        val viewModel = FeedTransportCaptureViewModel(
            sync = sync,
            capture = proofCaptureSource,
            proofCaptureRepository = proofCaptureRepository,
            drafts = drafts,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
            feedTransportRepository = FakeFeedTransportStatusSource(),
            saved = SavedStateHandle(
                mapOf(
                    FeedTransportCaptureViewModel.ARG_TASK_ID to "transport-task-1",
                    FeedTransportCaptureViewModel.ARG_SHED_ID to "shed-1",
                    FeedTransportCaptureViewModel.ARG_SHED_LABEL to "Shed 1",
                    FeedTransportCaptureViewModel.ARG_PARK_LABEL to "Farm 1",
                ),
            ),
        )
        advanceUntilIdle()

        proofCaptureSource.queue(CapturedVideo(localUri = "file://transport-old.mp4", startedAtMs = 1L, endedAtMs = 2L))
        viewModel.onEvent(FeedTransportCaptureEvent.RecordVideo)
        advanceUntilIdle()
        val originalProof = proofCaptureRepository.observeProofs("feed-transport:transport-task-1", null).first().single()

        proofCaptureSource.queue(null)
        viewModel.onEvent(FeedTransportCaptureEvent.ReRecordVideo)
        advanceUntilIdle()

        assertEquals(2, proofCaptureSource.captureCount)
        assertEquals(listOf(originalProof.id), proofCaptureRepository.observeProofs("feed-transport:transport-task-1", null).first().map { it.id })
        assertTrue(viewModel.state.value.videoCaptured)
    }
}

private class TransportDraftRepository : CaptureDraftRepository {
    private val rows = mutableMapOf<String, CaptureDraft>()
    private val progress = MutableStateFlow<Map<String, Int>>(emptyMap())

    override suspend fun find(flowKey: String, entityId: String): CaptureDraft = rows[entityId] ?: CaptureDraft()
    override fun observe(flowKey: String, entityId: String): Flow<CaptureDraft> = MutableStateFlow(rows[entityId] ?: CaptureDraft())
    override fun observeProgress(flowKey: String, limit: Int): Flow<Map<String, Int>> = progress

    override suspend fun putProof(flowKey: String, entityId: String, step: String, outboxItemId: String, fingerprint: String?) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(proofs = current.proofs + (step to outboxItemId), fingerprint = fingerprint ?: current.fingerprint)
        publish()
    }

    override suspend fun clearProof(flowKey: String, entityId: String, step: String) {
        val current = rows[entityId] ?: return
        rows[entityId] = current.copy(proofs = current.proofs - step)
        publish()
    }

    override suspend fun putSubmit(flowKey: String, entityId: String, idempotencyKey: String?, outboxItemId: String?) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(submitIdempotencyKey = idempotencyKey, submitOutboxItemId = outboxItemId)
        publish()
    }

    override suspend fun putAnswers(flowKey: String, entityId: String, answers: Map<String, String>) {
        val current = rows[entityId] ?: CaptureDraft()
        rows[entityId] = current.copy(answers = answers)
        publish()
    }

    override suspend fun clear(flowKey: String, entityId: String) {
        rows.remove(entityId)
        publish()
    }

    private fun publish() {
        progress.value = rows.mapValues { it.value.capturedCount }
    }
}

private class TransportSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    private var nextProof = 0

    override fun observeStatus(): MutableStateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = items.getOrPut(itemId) { MutableStateFlow(null) }

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> {
        val itemId = "transport-proof-${++nextProof}"
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = itemId,
            opType = "PROOF_UPLOAD",
            idempotencyKey = idempotencyKey,
            groupKey = groupKey,
            status = SyncItemStatus.QUEUED,
            attemptCount = 0,
            maxAttempts = 3,
            conflict = false,
            createdAt = 1L,
            updatedAt = 1L,
            lastError = null,
            localFilePath = localFilePath,
        )
        return AppResult.Ok(itemId)
    }

    override suspend fun enqueueShedSubmit(
        taskId: String,
        groupKey: String,
        idempotencyKey: String,
        request: SubmitTaskRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueReschedule(
        obligationId: String,
        groupKey: String,
        idempotencyKey: String,
        request: RescheduleObligationRequestDto,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun triggerDrain() = Unit
}
