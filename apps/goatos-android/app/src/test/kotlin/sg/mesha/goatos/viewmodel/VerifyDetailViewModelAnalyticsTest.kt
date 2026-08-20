package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVerification
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
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VerifyMeasurementInput
import sg.mesha.goatos.feature.verify.VerifyDecisionUnavailableReason
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
    fun `historical approved detail reuses the exact queue cache scope`() = runTest(dispatcher) {
        val repo = FakeVerifyDetailRepository()
        val vm = VerifyDetailViewModel(
            repo = repo,
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(
                mapOf(
                    "itemId" to "item-1",
                    "category" to "birth_evidence",
                    "status" to "approved",
                    "businessDate" to "2026-07-29",
                    "missed" to false,
                ),
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(Triple("approved", "2026-07-29", false), repo.lastObservedScope)
        assertEquals(Triple("approved", "2026-07-29", false), repo.lastRefreshedScope)
    }

    // THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20).
    //
    // This replaces `weight correction refresh waits for exact outbox success`, which guarded the
    // standalone save. That save relabelled the item, bumping row_version, so the Approve pressed
    // straight afterwards was version-fenced out and silently did nothing. There is no separate
    // save any more: one press, one outbox row, value and verdict together.
    @Test
    fun `approve carries the typed number on the same verdict`() = runTest(dispatcher) {
        val repo = FakeVerifyDetailRepository()
        val syncRepository = FakeVerifyDetailSyncRepository()
        val vm = VerifyDetailViewModel(
            repo = repo,
            syncRepo = syncRepository,
            analytics = RecordingAnalytics(),
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "weighing")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.Approve(
                itemId = "item-1",
                measurement = VerifyMeasurementInput(value = 42.5, reason = "scale reads 42.5"),
            ),
        )
        advanceUntilIdle()

        val verdict = syncRepository.lastVerdict
        assertEquals("approved", verdict?.decision)
        assertEquals(42.5, verdict?.measurement?.value)
        assertEquals("scale reads 42.5", verdict?.measurement?.reason)
    }

    // A rejection sends the work back to be recorded again, so a number typed before she changed
    // her mind must never reach the record that is about to be redone.
    @Test
    fun `reject never carries a number`() = runTest(dispatcher) {
        val repo = FakeVerifyDetailRepository()
        val syncRepository = FakeVerifyDetailSyncRepository()
        val vm = VerifyDetailViewModel(
            repo = repo,
            syncRepo = syncRepository,
            analytics = RecordingAnalytics(),
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "weighing")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyDetailEvent.Reject(reason = "clip too dark to read", itemId = "item-1"))
        advanceUntilIdle()

        val verdict = syncRepository.lastVerdict
        assertEquals("rejected", verdict?.decision)
        assertNull(verdict?.measurement)
    }

    @Test
    fun `detail keeps opened weighing video when refresh page omits the group`() = runTest(dispatcher) {
        val repo = FakeVerifyDetailRepository()
        val vm = VerifyDetailViewModel(
            repo = repo,
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "weighing")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertProofUrls(vm, "/proof-1.mp4", "/proof-2.mp4")

        repo.emitQueueItems(
            VerificationQueueItem(
                itemId = "other-item",
                category = "weighing",
                status = VerificationStatus.PENDING,
                rowVersion = 8,
                evidenceAvailable = true,
                media = listOf(
                    VerificationMediaItem(
                        proofId = "other-proof",
                        label = "Other weighing",
                        downloadUrl = "/other-proof.mp4",
                        mimeType = "video/mp4",
                    ),
                ),
            ),
        )
        advanceUntilIdle()

        assertProofUrls(vm, "/proof-1.mp4", "/proof-2.mp4")
        assertTrue(vm.state.value.media.isNotEmpty())
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

        vm.onEvent(VerifyDetailEvent.Approve())
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
            vm.onEvent(VerifyDetailEvent.Approve())
            advanceUntilIdle()
        } catch (error: RuntimeException) {
            fail("analytics exceptions must never escape verifier playback/verdict flow: ${error.message}")
        }
    }

    @Test
    fun `approve success fires verify_verdict_succeeded with the decision`() = runTest(dispatcher) {
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

        vm.onEvent(VerifyDetailEvent.Approve())
        advanceUntilIdle()

        val succeeded = analytics.events.first { it.first == AnalyticsFunnels.Events.VERIFY_VERDICT_SUCCEEDED }
        assertEquals("item-1", succeeded.second[AnalyticsFunnels.Params.ITEM_ID])
        assertEquals(VerificationDecision.APPROVED, succeeded.second[AnalyticsFunnels.Params.DECISION])
    }

    @Test
    fun `a verdict enqueue failure fires verify_verdict_failed with the decision and a reason`() = runTest(dispatcher) {
        val analytics = RecordingAnalytics()
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FailingVerifyDetailSyncRepository("local storage error"),
            analytics = analytics,
            crashReporter = RecordingCrashReporter(),
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(VerifyDetailEvent.Reject("proof video will not play, please record it again"))
        advanceUntilIdle()

        val failed = analytics.events.first { it.first == AnalyticsFunnels.Events.VERIFY_VERDICT_FAILED }
        assertEquals("item-1", failed.second[AnalyticsFunnels.Params.ITEM_ID])
        assertEquals(VerificationDecision.REJECTED, failed.second[AnalyticsFunnels.Params.DECISION])
        assertEquals("local storage error", failed.second[AnalyticsFunnels.Params.REASON])
    }

    @Test
    fun `evidence going unwatchable fires verify_decision_unavailable exactly once`() = runTest(dispatcher) {
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
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = "source error",
            ),
        )
        advanceUntilIdle()
        advanceUntilIdle()

        val unavailable = analytics.events.filter { it.first == AnalyticsEventsVerification.VERIFY_DECISION_UNAVAILABLE }
        assertEquals(1, unavailable.size)
        assertEquals("item-1", unavailable.first().second[AnalyticsFunnels.Params.ITEM_ID])
        assertEquals("EVIDENCE_UNAVAILABLE", unavailable.first().second[AnalyticsEventsVerification.Params.REASON])
    }

    @Test
    fun `source playback error disables approve without recording a Crashlytics non-fatal`() = runTest(dispatcher) {
        val analytics = RecordingAnalytics()
        val crashReporter = RecordingCrashReporter()
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = analytics,
            crashReporter = crashReporter,
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "weighing")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = "Source error",
            ),
        )
        advanceUntilIdle()

        assertTrue(!vm.state.value.entries.first().isApproveEnabled)
        assertEquals(
            VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE,
            vm.state.value.entries.first().decisionUnavailableReason,
        )
        assertTrue(crashReporter.exceptions.isEmpty())
        assertTrue(
            analytics.events.any { (event, props) ->
                event == AnalyticsEvents.VERIFY_VIDEO_PLAYBACK_ERROR &&
                    props[AnalyticsFunnels.Params.PROOF_ID] == "proof-1"
            },
        )
    }

    @Test
    fun `playback error resolves the play watchdog instead of also logging dead control`() = runTest(dispatcher) {
        val crashReporter = RecordingCrashReporter()
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = crashReporter,
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "weighing")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAY_INTENT,
                playerState = "STATE_READY",
                armed = true,
                targetAction = "play",
            ),
        )
        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = "Source error",
            ),
        )

        advanceTimeBy(AnalyticsFunnels.VERIFY_VIDEO_PLAY_WATCHDOG_TIMEOUT_MS + 1)
        advanceUntilIdle()

        assertTrue(crashReporter.exceptions.isEmpty())
    }

    @Test
    fun `non-source playback error remains visible as Crashlytics non-fatal`() = runTest(dispatcher) {
        val crashReporter = RecordingCrashReporter()
        val vm = VerifyDetailViewModel(
            repo = FakeVerifyDetailRepository(),
            syncRepo = FakeVerifyDetailSyncRepository(),
            analytics = RecordingAnalytics(),
            crashReporter = crashReporter,
            savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "weighing")),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(
            VerifyDetailEvent.VideoPlayback(
                proofSubject = "proof-1",
                mimeType = "video/mp4",
                action = VideoPlaybackAction.PLAYBACK_ERROR,
                reason = "decoder crashed",
            ),
        )

        assertEquals(listOf("verification video playback failed" to "decoder crashed"), crashReporter.exceptions)
    }
}

private fun assertProofUrls(vm: VerifyDetailViewModel, vararg suffixes: String) {
    val apiBase = BuildConfig.API_BASE_URL.removeSuffix("/")
    assertEquals(suffixes.toList(), vm.state.value.media.map { it.signedUrl.substringAfterLast(apiBase) })
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
    var lastObservedScope: Triple<String?, String?, Boolean?>? = null
    var lastRefreshedScope: Triple<String?, String?, Boolean?>? = null
    var refreshCount: Int = 0
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
    private val queueFlow = MutableStateFlow(Resource(data = response))

    fun emitQueueItems(vararg items: VerificationQueueItem) {
        queueFlow.value = Resource(data = VerificationQueueResponseDto(items = items.toList()))
    }

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = response
    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> {
        lastObservedScope = Triple(status, businessDate, missed)
        return queueFlow
    }

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        refreshCount += 1
        lastRefreshedScope = Triple(status, businessDate, missed)
        return Result.success(Unit)
    }
    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        queueFlow

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
    override fun observeLeadershipVideos(category: String?, windowSize: Int) = flowOf(emptyList<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>())
    override fun observeLeadershipTitle(category: String?, windowSize: Int) = flowOf("")
    override suspend fun refreshLeadershipVideos(category: String?, windowSize: Int, reset: Boolean) =
        sg.mesha.goatos.core.common.AppResult.Ok(Unit)
}

/** One enqueued verdict, as the outbox saw it. */
private data class RecordedVerdict(
    val decision: String,
    val measurement: VerificationVerdictMeasurementDto?,
)

private class FakeVerifyDetailSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val correctionItem = MutableStateFlow<SyncQueueItem?>(null)

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = correctionItem
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    /** What actually reached the outbox, so a test can assert on what the verdict DROPPED as well
     *  as on what it carried. */
    var lastVerdict: RecordedVerdict? = null
        private set

    override suspend fun enqueueVerificationVerdict(
        itemId: String,
        decision: String,
        reason: String?,
        rowVersion: Int,
        measurement: VerificationVerdictMeasurementDto?,
    ): AppResult<String> {
        lastVerdict = RecordedVerdict(decision = decision, measurement = measurement)
        return AppResult.Ok("outbox-1")
    }

    override suspend fun enqueueWeighingWeightCorrection(
        observationId: String,
        refType: String,
        weightKg: Double,
        animalCount: Int?,
        reason: String?,
    ): AppResult<String> = error("the standalone correction is not called any more; the approve carries the number")

    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> =
        AppResult.Ok(
            SyncQueueItem(
                id = itemId,
                idempotencyKey = "test-idempotency-key",
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

/** Sync repo whose verdict enqueue always fails, for [VerifyDetailViewModelAnalyticsTest]'s
 *  `verify_verdict_failed` coverage. */
private class FailingVerifyDetailSyncRepository(private val message: String) : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> =
        AppResult.Err(message)

    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> = AppResult.Ok(null)
    override suspend fun triggerDrain() = Unit
}
