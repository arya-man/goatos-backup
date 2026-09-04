package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.TaskDetail
import sg.mesha.goatos.core.data.TasksRepository
import sg.mesha.goatos.core.data.forms.FormSpec
import sg.mesha.goatos.core.data.forms.ProofPolicy
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.ShedCompletionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TaskSummaryDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.submit.SubmitEvent
import sg.mesha.goatos.feature.submit.SubmitSnackbarMessage
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * Deduplication and snackbar feedback tests for the Submit shed-record screen.
 *
 * Proves that:
 * 1. Two rapid submit() invocations produce exactly ONE outbound submission (no duplicate queue entries)
 * 2. Success state emits SubmitSnackbarMessage.SUCCEEDED snackbar
 * 3. Failure/conflict state emits SubmitSnackbarMessage.CONFLICT snackbar
 * 4. Queued state emits SubmitSnackbarMessage.QUEUED snackbar
 * 5. Dead-letter state emits SubmitSnackbarMessage.DEAD_LETTER snackbar
 */
@OptIn(ExperimentalCoroutinesApi::class)
class SubmitViewModelSubmitDeduplicationTest {
    private val testDispatcher = StandardTestDispatcher()

    @Before
    fun setup() {
        Dispatchers.setMain(testDispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    private fun viewModel(
        repository: TasksRepository,
        sync: SyncRepository,
        taskId: String,
        shedId: String = "shed-1",
        sopVersionId: String = "sop-v1",
    ): SubmitViewModel = SubmitViewModel(
        repo = repository,
        syncRepository = sync,
        scanCaptureRepository = FakeScanCaptureRepository(),
        proofCaptureRepository = FakeProofCaptureRepository(),
        scanSource = FakeScanSource(),
        proofCaptureSource = FakeProofCaptureSource(),
        bootstrapRepository = FakeCaptureBootstrapRepository(),
        analytics = sg.mesha.goatos.core.analytics.NoopAnalytics(),
        crashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
        savedStateHandle = SavedStateHandle(
            buildMap {
                put("taskId", taskId)
                put("shedId", shedId)
                put("sopVersionId", sopVersionId)
            },
        ),
    )

    @Test
    fun `two rapid submits produce exactly one outbound submission`() = runTest(testDispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-1",
            sopVersionId = "sop-v1",
            scopeType = "shed",
            scopeId = "shed-1",
            state = "draft",
            rowVersion = 1,
        )
        val form = FormSpec.Empty
        val repository = DedupFakeTasksRepository(task, form)
        val sync = DeduplicationTestSyncRepository()
        val viewModel = viewModel(repository, sync, "task-1")

        // Keep state subscribed (MOB-010)
        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        // First submit
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        val firstEnqueueCount = sync.enqueueCalls.size
        assertEquals("Expected 1 enqueue after first submit", 1, firstEnqueueCount)

        // Second rapid submit (should be ignored by the submitInFlight guard)
        viewModel.onEvent(SubmitEvent.Submit)
        // Confirmation gate: answer it to reach the submit these assertions cover.
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        val secondEnqueueCount = sync.enqueueCalls.size
        assertEquals("Expected still 1 enqueue after second rapid submit — second should be ignored", 1, secondEnqueueCount)
    }

    @Test
    fun `enqueue failure clears in-flight guard so operator can retry submit`() = runTest(testDispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-1",
            sopVersionId = "sop-v1",
            scopeType = "shed",
            scopeId = "shed-1",
            state = "draft",
            rowVersion = 1,
        )
        val repository = DedupFakeTasksRepository(task, FormSpec.Empty)
        val sync = DeduplicationTestSyncRepository()
        sync.failNextEnqueue = true
        val viewModel = viewModel(repository, sync, "task-1")

        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals("First enqueue should fail before creating an outbox row", 0, sync.enqueueCalls.size)
        assertEquals(SyncState.DEAD_LETTER, viewModel.state.value.syncState)
        assertFalse(viewModel.state.value.showSubmitConfirmation)

        viewModel.onEvent(SubmitEvent.Submit)
        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals("Retry after queue failure must enqueue once", 1, sync.enqueueCalls.size)
        assertEquals(SyncState.QUEUED, viewModel.state.value.syncState)
    }

    @Test
    fun `submission queued state emits queued snackbar`() = runTest(testDispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-1",
            sopVersionId = "sop-v1",
            scopeType = "shed",
            scopeId = "shed-1",
            state = "draft",
            rowVersion = 1,
        )
        val form = FormSpec.Empty
        val repository = DedupFakeTasksRepository(task, form)
        val sync = DeduplicationTestSyncRepository()
        val viewModel = viewModel(repository, sync, "task-1")

        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        assertEquals(
            SubmitSnackbarMessage.QUEUED,
            viewModel.state.value.snackbarMessage
        )
        assertEquals(SyncState.QUEUED, viewModel.state.value.syncState)
    }

    @Test
    fun `submission success emits succeeded snackbar`() = runTest(testDispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-1",
            sopVersionId = "sop-v1",
            scopeType = "shed",
            scopeId = "shed-1",
            state = "draft",
            rowVersion = 1,
        )
        val form = FormSpec.Empty
        val repository = DedupFakeTasksRepository(task, form)
        val sync = DeduplicationTestSyncRepository()
        val viewModel = viewModel(repository, sync, "task-1")

        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        // Simulate outbox item reaching SUCCEEDED state
        val outboxItemId = sync.enqueueCalls.firstOrNull()?.itemId ?: return@runTest
        sync.markSucceeded(outboxItemId)
        advanceUntilIdle()

        assertEquals(
            SubmitSnackbarMessage.SUCCEEDED,
            viewModel.state.value.snackbarMessage
        )
        assertEquals(SyncState.ACKED, viewModel.state.value.syncState)
    }

    @Test
    fun `submission conflict emits conflict snackbar`() = runTest(testDispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-1",
            sopVersionId = "sop-v1",
            scopeType = "shed",
            scopeId = "shed-1",
            state = "draft",
            rowVersion = 1,
        )
        val form = FormSpec.Empty
        val repository = DedupFakeTasksRepository(task, form)
        val sync = DeduplicationTestSyncRepository()
        val viewModel = viewModel(repository, sync, "task-1")

        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        // Simulate outbox item reaching CONFLICT state
        val outboxItemId = sync.enqueueCalls.firstOrNull()?.itemId ?: return@runTest
        sync.markConflict(outboxItemId, "Missing required proof")
        advanceUntilIdle()

        assertEquals(
            SubmitSnackbarMessage.CONFLICT,
            viewModel.state.value.snackbarMessage
        )
        assertEquals(SyncState.CONFLICT, viewModel.state.value.syncState)
    }

    @Test
    fun `submission dead-letter emits dead-letter snackbar`() = runTest(testDispatcher) {
        val task = TaskSummaryDto(
            taskId = "task-1",
            sopVersionId = "sop-v1",
            scopeType = "shed",
            scopeId = "shed-1",
            state = "draft",
            rowVersion = 1,
        )
        val form = FormSpec.Empty
        val repository = DedupFakeTasksRepository(task, form)
        val sync = DeduplicationTestSyncRepository()
        val viewModel = viewModel(repository, sync, "task-1")

        backgroundScope.launch { viewModel.state.collect {} }
        advanceUntilIdle()

        viewModel.onEvent(SubmitEvent.Submit)

        // Confirmation gate: answer it to reach the submit these assertions cover.

        viewModel.onEvent(SubmitEvent.ConfirmSubmit)
        advanceUntilIdle()

        // Simulate outbox item reaching DEAD_LETTER state
        val outboxItemId = sync.enqueueCalls.firstOrNull()?.itemId ?: return@runTest
        sync.markDeadLetter(outboxItemId, "Network error")
        advanceUntilIdle()

        assertEquals(
            SubmitSnackbarMessage.DEAD_LETTER,
            viewModel.state.value.snackbarMessage
        )
        assertEquals(SyncState.DEAD_LETTER, viewModel.state.value.syncState)
    }

    // Fake implementations matching existing test patterns

    private class DedupFakeTasksRepository(
        private val task: TaskSummaryDto,
        private val form: FormSpec,
        private val proofPolicy: ProofPolicy = ProofPolicy(expectedSubjects = emptyList()),
        private val shedSummary: ShedCompletionSummaryDto? = null,
    ) : TasksRepository {
        private val flow = MutableStateFlow<Resource<TaskDetail>>(Resource(data = null))
        private val summaryFlow = MutableStateFlow<ShedCompletionSummaryDto?>(shedSummary)

        override suspend fun taskDetail(taskId: String): TaskDetail = TaskDetail(task = task, form = form, proofPolicy = proofPolicy)

        override fun observeTaskDetail(taskId: String): Flow<Resource<TaskDetail>> = flow

        override suspend fun refreshTaskDetail(taskId: String): Result<Unit> = runCatching {
            flow.value = Resource(data = TaskDetail(task = task, form = form, proofPolicy = proofPolicy), lastSyncedAt = 1L)
        }

        override fun observeShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): Flow<ShedCompletionSummaryDto?> = summaryFlow

        override suspend fun refreshShedCompletionSummary(taskId: String, shedId: String?, partitionLabel: String?): Result<Unit> = runCatching {
            summaryFlow.value = shedSummary
        }
    }

    private class DeduplicationTestSyncRepository : SyncRepository {
        data class EnqueueCall(val itemId: String, val request: SubmitTaskRequestDto)

        val enqueueCalls = mutableListOf<EnqueueCall>()
        var failNextEnqueue = false
        private val itemFlow = MutableStateFlow<SyncQueueItem?>(null)

        override fun observeStatus(): StateFlow<SyncStatus> = MutableStateFlow(SyncStatus.empty(online = true))

        override fun observeItem(itemId: String): Flow<SyncQueueItem?> = itemFlow

        override suspend fun enqueueShedSubmit(
            taskId: String,
            groupKey: String,
            idempotencyKey: String,
            request: SubmitTaskRequestDto,
        ): AppResult<String> {
            if (failNextEnqueue) {
                failNextEnqueue = false
                return AppResult.Err("enqueue failed")
            }
            val itemId = "outbox-${enqueueCalls.size + 1}"
            enqueueCalls.add(EnqueueCall(itemId, request))
            itemFlow.value = SyncQueueItem(
                id = itemId,
                idempotencyKey = "test-idempotency-key",
                opType = "shed_submit",
                groupKey = groupKey,
                status = SyncItemStatus.QUEUED,
                attemptCount = 0,
                maxAttempts = 5,
                conflict = false,
                createdAt = System.currentTimeMillis(),
                updatedAt = System.currentTimeMillis(),
                lastError = null,
            )
            return AppResult.Ok(itemId)
        }

        fun markSucceeded(itemId: String) {
            val current = itemFlow.value
            if (current?.id == itemId) {
                itemFlow.value = current.copy(
                    status = SyncItemStatus.SUCCEEDED,
                    updatedAt = System.currentTimeMillis(),
                )
            }
        }

        fun markConflict(itemId: String, error: String) {
            val current = itemFlow.value
            if (current?.id == itemId) {
                itemFlow.value = current.copy(
                    status = SyncItemStatus.FAILED,
                    conflict = true,
                    lastError = error,
                    updatedAt = System.currentTimeMillis(),
                )
            }
        }

        fun markDeadLetter(itemId: String, error: String) {
            val current = itemFlow.value
            if (current?.id == itemId) {
                itemFlow.value = current.copy(
                    status = SyncItemStatus.FAILED,
                    conflict = false,
                    lastError = error,
                    updatedAt = System.currentTimeMillis(),
                    attemptCount = 5,
                    maxAttempts = 5,
                )
            }
        }

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

        override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> =
            error("unused")

        override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> =
            error("unused")

        override suspend fun enqueueVerificationVerdict(
            itemId: String,
            decision: String,
            reason: String?,
            rowVersion: Int,
            measurement: VerificationVerdictMeasurementDto?,
        ): AppResult<String> = error("unused")

        override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")

        override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun deleteFailedOutboxItemByIdempotencyKey(key: String): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun findOutboxItemByIdempotencyKey(key: String): AppResult<SyncQueueItem?> =
            AppResult.Ok(null)

        override suspend fun triggerDrain() = Unit
    }
}
