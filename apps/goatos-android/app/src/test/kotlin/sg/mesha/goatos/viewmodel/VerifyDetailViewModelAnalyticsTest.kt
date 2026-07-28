package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ReviewTaskRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationDecision
import sg.mesha.goatos.core.network.dto.VerificationMediaItem
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VideoPlaybackAction

@OptIn(ExperimentalCoroutinesApi::class)
class VerifyDetailViewModelAnalyticsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `backend task title and recorded answer are preserved for every verification video`() = runTest(dispatcher) {
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "birth_evidence")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf("Mother's Medicine", "ORS water"), vm.state.value.media.map { it.taskTitle })
        assertEquals(listOf("No", null), vm.state.value.media.map { it.answer })
    }

    @Test
    fun `detail open and playback events include bounded proof watch analytics`() = runTest(dispatcher) {
        val analytics = RecordingAnalytics()
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = analytics,
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAY_STARTED,
                durationMs = 10_000,
            ),
        )
        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.WATCH_SUMMARY,
                watchTimeMs = 4_200,
                durationMs = 10_000,
                positionMs = 5_000,
                percentWatched = 50,
                seekCount = 2,
                replayCount = 1,
                bufferingTimeMs = 300,
            ),
        )

        assertTrue(analytics.events.any { it.first == AnalyticsEvents.VERIFY_ITEM_OPENED })
        val start = analytics.events.single { it.first == AnalyticsEvents.VERIFY_VIDEO_PLAY_STARTED }
        assertEquals("item-1", start.second[AnalyticsFunnels.Params.ITEM_ID])
        assertEquals("proof-1", start.second[AnalyticsFunnels.Params.PROOF_ID])

        val summary = analytics.events.single { it.first == AnalyticsEvents.VERIFY_VIDEO_WATCH_SUMMARY }
        assertEquals("4200", summary.second[AnalyticsFunnels.Params.WATCH_TIME_MS])
        assertEquals("50", summary.second[AnalyticsFunnels.Params.PERCENT_WATCHED])
        assertEquals("2", summary.second[AnalyticsFunnels.Params.SEEK_COUNT])
        assertEquals("1", summary.second[AnalyticsFunnels.Params.REPLAY_COUNT])
        assertEquals("300", summary.second[AnalyticsFunnels.Params.BUFFERING_TIME_MS])
    }

    @Test
    fun `verdict attempt carries total watch time accumulated before decision`() = runTest(dispatcher) {
        val analytics = RecordingAnalytics()
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = analytics,
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.WATCH_SUMMARY,
                watchTimeMs = 3_000,
                durationMs = 10_000,
                positionMs = 3_000,
                percentWatched = 30,
            ),
        )
        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-2",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.WATCH_SUMMARY,
                watchTimeMs = 2_500,
                durationMs = 8_000,
                positionMs = 2_500,
                percentWatched = 31,
            ),
        )

        vm.onEvent(VerifyDetailEvent.Approve)
        advanceUntilIdle()

        val attempted = analytics.events.single { it.first == AnalyticsFunnels.Events.VERIFY_VERDICT_ATTEMPTED }
        assertEquals(VerificationDecision.APPROVED, attempted.second[AnalyticsFunnels.Params.DECISION])
        assertEquals("5500", attempted.second[AnalyticsFunnels.Params.TOTAL_WATCH_TIME_MS])
    }

    @Test
    fun `analytics failure never crashes playback or verdict flow`() = runTest(dispatcher) {
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = ThrowingAnalytics(),
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        try {
            vm.onEvent(
                VerifyDetailEvent.VideoPlayback(
                    proofSubject = "proof-1",
                    mimeType = "video/mp4",
                    action = VideoPlaybackAction.WATCH_SUMMARY,
                    watchTimeMs = 1_000,
                    durationMs = 10_000,
                    positionMs = 1_000,
                    percentWatched = 10,
                ),
            )
            vm.onEvent(VerifyDetailEvent.Approve)
            advanceUntilIdle()
        } catch (error: RuntimeException) {
            fail("analytics exceptions must never escape verifier playback/verdict flow: ${error.message}")
        }
    }
}

private class RecordingAnalytics : AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()

    override fun track(event: String, props: Map<String, String>) {
        events.add(event to props)
    }

    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class ThrowingAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {
        throw RuntimeException("analytics unavailable")
    }

    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class RecordingCrashReporter : CrashReporter {
    val exceptions = mutableListOf<Pair<String?, String>>()

    override fun recordException(throwable: Throwable, message: String?) {
        exceptions.add(message to throwable.message.orEmpty())
    }

    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}

private class FakeVerifyDetailRepository : VerificationRepository {
    private val item = VerificationQueueItem(
        itemId = "item-1",
        category = "vaccination_proof",
        status = VerificationStatus.PENDING,
        rowVersion = 7,
        evidenceAvailable = true,
        media = listOf(
            VerificationMediaItem(proofId = "proof-1", label = "Mother's Medicine", answer = "No", downloadUrl = "/proof-1.mp4", mimeType = "video/mp4", durationMs = 10_000),
            VerificationMediaItem(proofId = "proof-2", label = "ORS water", downloadUrl = "/proof-2.mp4", mimeType = "video/mp4", durationMs = 8_000),
        ),
    )
    private val response = VerificationQueueResponseDto(items = listOf(item))

    override suspend fun queue(category: String?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = response
    override fun observeQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = response))

    override suspend fun refreshQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun appendQueue(cursor: String, category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = response))

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
}

private class FakeVerifyDetailSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = AppResult.Ok("outbox-1")
    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> =
        AppResult.Ok(
            SyncQueueItem(
                id = itemId,
                opType = "verification_verdict",
                groupKey = "item-1",
                status = SyncItemStatus.SUCCEEDED,
                attemptCount = 1,
                maxAttempts = 3,
                conflict = false,
                createdAt = 1,
                updatedAt = 2,
                lastError = null,
            ),
        )

    override suspend fun triggerDrain() = Unit
}
