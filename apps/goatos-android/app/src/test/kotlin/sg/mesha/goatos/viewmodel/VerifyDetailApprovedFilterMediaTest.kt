package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
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
import org.junit.Before
import org.junit.Test
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationMediaItem
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationSourceRef
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto

/**
 * An ALREADY-APPROVED item must still show its video.
 *
 * Live repro: the verifier filtered the queue by "Approved", tapped a row, and got the whole-screen
 * empty state "No video attached to this item" — on an item whose media was intact at every layer
 * (the queue endpoint returns a full media array with a download url for every approved item, and
 * every media_ref resolves to a real proof artifact).
 *
 * Cause was entirely client-side. The nav route does not forward the queue's selected status, so
 * the detail screen re-queried with none — and the backend silently defaults a blank status to
 * `pending` (verification/app/service.go: `if params.Status == "" && !params.IncludeAllStatuses`).
 * The approved item was therefore never in the page it searched. Pending items only ever "worked"
 * because that accidental default happened to match them.
 *
 * The fake below reproduces exactly that backend behaviour: it returns the approved item ONLY for
 * the explicit no-filter status, and an empty page otherwise. A detail screen that inherits the
 * queue's ambient default fails this test.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyDetailApprovedFilterMediaTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun approvedItem() = VerificationQueueItem(
        itemId = "approved-item-1",
        category = "vaccination_proof",
        status = VerificationStatus.APPROVED,
        rowVersion = 3,
        subjectLabel = "Godel 1 · G-910001",
        shedLabel = "Godel 1",
        evidenceAvailable = true,
        source = VerificationSourceRef(refType = "vaccination_goat", submissionId = "submission-godel-1"),
        media = listOf(
            VerificationMediaItem(proofId = "proof-approved-1", downloadUrl = "/proof-approved-1.mp4", mimeType = "video/mp4"),
        ),
    )

    @Test
    fun `opening an approved item from the Approved filter still renders its video`() = runTest(dispatcher) {
        val repo = StatusSensitiveRepository(approvedItem())
        val vm = VerifyDetailViewModel(
            repo = repo,
            syncRepo = ApprovedFilterSyncRepository(),
            analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
            crashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
            // Matches the buggy nav route: NO "status" key at all.
            savedStateHandle = SavedStateHandle(
                mapOf("itemId" to "submission-godel-1", "category" to "vaccination_proof"),
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("the approved item must resolve", 1, state.entries.size)
        assertTrue(
            "its video must render instead of the no-media empty state",
            state.entries.single().media.isNotEmpty(),
        )
        assertEquals(
            "the detail screen must not inherit the queue's ambient pending default",
            listOf("all"),
            repo.observedStatuses,
        )
    }
}

/**
 * Mimics the real backend: a blank/absent status is silently treated as `pending`, so only the
 * explicit no-filter sentinel returns a non-pending item.
 */
private class StatusSensitiveRepository(private val item: VerificationQueueItem) : VerificationRepository {
    val observedStatuses = mutableListOf<String?>()

    private fun pageFor(status: String?): VerificationQueueResponseDto =
        if (status == "all") VerificationQueueResponseDto(items = listOf(item)) else VerificationQueueResponseDto(items = emptyList())

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto =
        pageFor(status)

    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> {
        observedStatuses += status
        return flowOf(Resource(data = pageFor(status)))
    }

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> =
        flowOf(Resource(data = pageFor("all")))

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
    override fun observeLeadershipVideos(category: String?, windowSize: Int) = flowOf(emptyList<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>())
    override fun observeLeadershipTitle(category: String?, windowSize: Int) = flowOf("")
    override suspend fun refreshLeadershipVideos(category: String?, windowSize: Int, reset: Boolean) =
        sg.mesha.goatos.core.common.AppResult.Ok(Unit)
}

/** Minimal SyncRepository: this test never issues a verdict, it only opens the screen. */
private class ApprovedFilterSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun findOutboxItem(itemId: String): AppResult<SyncQueueItem?> = AppResult.Ok(null)
    override suspend fun triggerDrain() = Unit
}
