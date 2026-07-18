package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.FlowCollector
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.capture.ScanCaptureRepository
import sg.mesha.goatos.core.data.capture.ScannedGoatRow
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskListResponseDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * MOB-010 guardrail: proves the three Room observers `load()`/`observeCaptureState()` wire
 * (task-detail via [TasksRepository.observeTaskDetail], scans via
 * [ScanCaptureRepository.observeAllForTask], proofs via [ProofCaptureRepository.observeProofs])
 * are collected ONLY while [SubmitViewModel.state] has an active subscriber, via the
 * `_state.subscriptionCount`-gated `collectLatest` added for MOB-010.
 *
 * The prior forever-`collect` around each inner `stateIn(WhileSubscribed(5_000))` was itself a
 * permanent subscriber, so the inner WhileSubscribed never saw zero subscribers and each Room
 * stream was collected forever, never released when the screen was backgrounded.
 *
 * Unlike [AlertsViewModelWhileSubscribedTest]/[ProfileViewModelWhileSubscribedTest] (which gate
 * on a `WhileSubscribed(5_000)` grace period on the EXPOSED StateFlow), this gate is a raw
 * `subscriptionCount > 0` check with no grace period — the smallest-risk fix for
 * [SubmitViewModel], which drives [SubmitViewModel.state] imperatively from many callbacks and
 * cannot safely be converted to a fully derived StateFlow. Collection stops the instant the last
 * subscriber leaves rather than ~5s later; `advanceTimeBy(6_000)` below still passes because
 * "eventually 0" holds regardless of grace period.
 *
 * Uses [UnconfinedTestDispatcher] so a launched collector subscribes eagerly/synchronously,
 * mirroring [AlertsViewModelWhileSubscribedTest].
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SubmitViewModelWhileSubscribedTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `task-detail, scan, and proof Room observers are collected only while state has subscribers`() = runTest(dispatcher) {
        val task = TaskSummaryDto(taskId = "task-1", sopVersionId = "sop-1", scopeId = "shed-1", title = "Shed 1", rowVersion = 1)
        val repository = CountingTasksRepository(task)
        val scanCaptureRepository = CountingScanCaptureRepository()
        val proofCaptureRepository = CountingProofCaptureRepository()

        val viewModel = SubmitViewModel(
            repo = repository,
            syncRepository = WhileSubNoopSyncRepository(),
            scanCaptureRepository = scanCaptureRepository,
            proofCaptureRepository = proofCaptureRepository,
            scanSource = FakeScanSource(),
            proofCaptureSource = FakeProofCaptureSource(),
            bootstrapRepository = FakeCaptureBootstrapRepository(),
            savedStateHandle = SavedStateHandle(mapOf("taskId" to "task-1")),
        )

        fun totalActive() =
            repository.activeDetailCollectors + scanCaptureRepository.activeCollectors + proofCaptureRepository.activeCollectors

        // No UI subscriber yet -> the gated observers stay cold.
        assertEquals(0, totalActive())

        // Subscribe (simulates the screen collecting state) — Unconfined subscribes synchronously.
        val job1 = launch { viewModel.state.collect {} }
        assertEquals(3, totalActive())

        // Unsubscribe (screen backgrounded). No grace period here (see class KDoc) — the gate
        // drops immediately, and stays at 0 well past any WhileSubscribed-style timeout.
        job1.cancel()
        advanceTimeBy(6_000)
        assertEquals(0, totalActive())

        // Returning to the screen restarts every observer.
        val job2 = launch { viewModel.state.collect {} }
        assertEquals(3, totalActive())

        job2.cancel()
    }
}

/** Counts active collectors of [observeTaskDetail]'s Flow — always has a valid cached task. */
private class CountingTasksRepository(task: TaskSummaryDto) : TasksRepository {
    private val flow = MutableStateFlow(Resource(data = TaskDetail(task = task, form = FormSpec.Empty)))
    var activeDetailCollectors = 0
        private set

    override suspend fun tasks(state: String?, limit: Int?): TaskListResponseDto = error("unused")

    override suspend fun taskDetail(taskId: String): TaskDetail = error("unused")

    override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> =
        object : Flow<Resource<TaskDetail>> {
            override suspend fun collect(collector: FlowCollector<Resource<TaskDetail>>) {
                activeDetailCollectors++
                try {
                    flow.collect(collector)
                } finally {
                    activeDetailCollectors--
                }
            }
        }

    override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = Result.success(Unit)
}

/** Counts active collectors of [observeAllForTask]'s Flow. */
private class CountingScanCaptureRepository : ScanCaptureRepository {
    private val flow = MutableStateFlow<List<ScannedGoatRow>>(emptyList())
    var activeCollectors = 0
        private set

    override fun observeScannedTags(taskId: String, fieldKey: String): Flow<List<ScannedGoatRow>> = error("unused")

    override fun observeScannedCount(taskId: String, fieldKey: String): Flow<Int> = error("unused")

    override fun observeAllForTask(taskId: String): Flow<List<ScannedGoatRow>> =
        object : Flow<List<ScannedGoatRow>> {
            override suspend fun collect(collector: FlowCollector<List<ScannedGoatRow>>) {
                activeCollectors++
                try {
                    flow.collect(collector)
                } finally {
                    activeCollectors--
                }
            }
        }

    override suspend fun recordScan(
        taskId: String,
        fieldKey: String,
        tag: String,
        goatId: String?,
        obligationId: String?,
    ) = Unit

    override suspend fun tagsForTask(taskId: String): List<String> = emptyList()

    override suspend fun clearForTask(taskId: String) = Unit
}

/** Counts active collectors of [observeProofs]'s Flow. */
private class CountingProofCaptureRepository : ProofCaptureRepository {
    private val flow = MutableStateFlow<List<ProofCaptureRow>>(emptyList())
    var activeCollectors = 0
        private set

    override fun observeProofs(taskId: String): Flow<List<ProofCaptureRow>> =
        object : Flow<List<ProofCaptureRow>> {
            override suspend fun collect(collector: FlowCollector<List<ProofCaptureRow>>) {
                activeCollectors++
                try {
                    flow.collect(collector)
                } finally {
                    activeCollectors--
                }
            }
        }

    override suspend fun capture(
        taskId: String,
        fieldKey: String,
        subject: ProofSubject,
        subjectId: String?,
        localUri: String,
        mimeType: String,
        caption: String?,
        scopeType: String,
        scopeId: String,
        capturedStartMs: Long,
        capturedEndMs: Long,
        capturedByPrincipalId: String?,
    ): AppResult<ProofCaptureRow> = error("unused")

    override suspend fun updateCaption(taskId: String, id: String, caption: String): AppResult<Unit> = error("unused")

    override suspend fun remove(taskId: String, id: String): AppResult<Unit> = error("unused")

    override suspend fun retryUpload(taskId: String, id: String): AppResult<Unit> = AppResult.Ok(Unit)

    override suspend fun clearForTask(taskId: String) = Unit
}

private class WhileSubNoopSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status

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

    override suspend fun enqueueProofUpload(
        groupKey: String,
        idempotencyKey: String,
        request: ProofUploadRequestDto,
        localFilePath: String,
        durationMs: Long?,
    ): AppResult<String> = error("unused")

    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")

    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> =
        error("unused")

    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")

    override suspend fun triggerDrain() = Unit
}
