package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto

@OptIn(ExperimentalCoroutinesApi::class)
class SyncStatusViewModelTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private class FakeSyncRepository(initial: SyncStatus) : SyncRepository {
        val status = MutableStateFlow(initial)
        val retried = mutableListOf<String>()
        var drainCount = 0

        override fun observeStatus(): StateFlow<SyncStatus> = status
        override fun observeItem(itemId: String): kotlinx.coroutines.flow.Flow<SyncQueueItem?> = kotlinx.coroutines.flow.flowOf(null)
        override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
        override suspend fun retry(itemId: String): AppResult<Unit> {
            retried += itemId
            return AppResult.Ok(Unit)
        }
        override suspend fun triggerDrain() { drainCount++ }
        override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
        override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
        override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
        override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
        override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
        override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    }

    private fun online() = SyncStatus.empty(online = true)
    private fun offline() = SyncStatus.empty(online = false)

    @Test
    fun `offline banner shows only after the grace period and hides immediately on reconnect`() = runTest {
        val repo = FakeSyncRepository(online())
        val vm = SyncStatusViewModel(repo)
        // A subscriber is required for the WhileSubscribed-backed StateFlow to run upstream.
        val job = launch { vm.showOfflineBanner.collect {} }
        runCurrent()

        assertFalse("online -> no banner", vm.showOfflineBanner.value)

        repo.status.value = offline()
        advanceTimeBy(1_000)
        runCurrent()
        assertFalse("within the 2.5s grace -> still hidden (blip suppression)", vm.showOfflineBanner.value)

        advanceTimeBy(2_000) // total 3s offline, past the grace window
        runCurrent()
        assertTrue("offline past grace -> banner shows", vm.showOfflineBanner.value)

        repo.status.value = online()
        runCurrent()
        assertFalse("reconnect -> hides immediately, no debounce", vm.showOfflineBanner.value)

        job.cancel()
    }

    @Test
    fun `a brief blip shorter than the grace window never shows the banner`() = runTest {
        val repo = FakeSyncRepository(online())
        val vm = SyncStatusViewModel(repo)
        val job = launch { vm.showOfflineBanner.collect {} }
        runCurrent()

        repo.status.value = offline()
        advanceTimeBy(1_500) // blip
        repo.status.value = online() // back before the 2.5s grace elapsed
        advanceTimeBy(5_000)
        runCurrent()

        assertFalse("a sub-grace blip must never flash the banner", vm.showOfflineBanner.value)
        job.cancel()
    }

    @Test
    fun `retryAll re-arms every failed row and triggers a drain`() = runTest {
        val repo = FakeSyncRepository(
            SyncStatus.empty(online = true).copy(
                items = listOf(
                    queueItem("a", SyncItemStatus.FAILED),
                    queueItem("b", SyncItemStatus.QUEUED),
                    queueItem("c", SyncItemStatus.FAILED),
                    queueItem("d", SyncItemStatus.SUCCEEDED),
                ),
            ),
        )
        val vm = SyncStatusViewModel(repo)

        vm.retryAll()
        advanceUntilIdle()

        assertEquals("only FAILED rows are retried", listOf("a", "c"), repo.retried)
        assertEquals("a drain is kicked once", 1, repo.drainCount)
    }

    private fun queueItem(id: String, status: SyncItemStatus) = SyncQueueItem(
        id = id,
        opType = "SHED_SUBMIT",
        groupKey = "shed-1",
        status = status,
        attemptCount = 0,
        maxAttempts = 8,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        lastError = null,
    )
}
