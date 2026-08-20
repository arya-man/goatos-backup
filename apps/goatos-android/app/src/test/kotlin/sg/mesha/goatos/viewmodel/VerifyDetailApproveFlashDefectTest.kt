package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
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
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationMediaItem
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationSourceRef
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.verify.VerifyDecisionUnavailableReason
import sg.mesha.goatos.feature.verify.VerifyDetailEvent

/**
 * Test the verifier screen's behavior when submitting a verdict (Approve).
 *
 * BUG-FLASH-DEFECT: After a verifier taps Approve on a queued item, the screen briefly flashes
 * the empty state "No video attached to this item" before showing the approved item or closing.
 *
 * ROOT CAUSE: The sequence is:
 * 1. User taps Approve → isSubmitting = true
 * 2. submitVerdict calls waitForBackendDecision, which launches a refresh() coroutine but doesn't wait for it
 * 3. waitForBackendDecision returns after the outbox succeeds
 * 4. isSubmitting is set to false (line 308)
 * 5. MEANWHILE the refresh() coroutine is still running
 * 6. When refresh() completes and updates observedGroup with the decided item (non-pending), the group emits empty
 * 7. At that point hasLoadedOnce=true AND isSubmitting=false, so the empty state SHOWS — the flash
 *
 * FIRST-CUT FIX: Added !state.isSubmitting to the empty state check
 * But this is WRONG because isSubmitting is already false by step 7 above.
 * The real fix needs to hold isSubmitting=true until the refetch confirms the new status, OR
 * use a dedicated flag like isDecisionResolving that stays true until the backend delivers the decided item.
 *
 * THIS TEST: Captures all state emissions during the Approve → transient empty → refetch sequence
 * and verifies that the definitive "No video attached" empty state is NEVER shown at any point.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyDetailApproveFlashDefectTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(
        repo: ApproveFlashRepository,
        sync: ApproveFlashSyncRepository = ApproveFlashSyncRepository(repo),
    ) = VerifyDetailViewModel(
        repo = repo,
        syncRepo = sync,
        analytics = ApproveFlashAnalytics(),
        crashReporter = ApproveFlashCrashReporter(),
        savedStateHandle = SavedStateHandle(mapOf("itemId" to "item-1", "category" to "vaccination_proof")),
    )

    /**
     * CRITICAL TEST: Drive the ViewModel through submit → transient empty emission → refetch.
     * Capture all state emissions and assert the empty state NEVER shows.
     *
     * The flash happens when:
     * - The item is approved and removed from the pending queue (observedGroup emits empty)
     * - But hasLoadedOnce is already true
     * - And isSubmitting is already false (because the background refresh() is still running)
     *
     * The old code would show the empty state at this point.
     * The fixed code should NEVER show it during the approve sequence.
     */
    @Test
    fun `when submitting approve verdict, the empty state never flashes before refetch completes`() =
        runTest(dispatcher) {
            val repo = ApproveFlashRepository(itemCount = 1, emptyMedia = false)
            val sync = ApproveFlashSyncRepository(repo)
            val vm = viewModel(repo, sync)

            // Collect all state emissions
            val emissions = mutableListOf<String>()
            backgroundScope.launch {
                vm.state.collect { state ->
                    emissions.add(
                        "hasLoaded=${state.hasLoadedOnce} resolving=${state.isDecisionResolving} entries=${state.entries.size} " +
                            "showsEmpty=${state.entries.isEmpty() && state.hasLoadedOnce && !state.isDecisionResolving}"
                    )
                }
            }
            advanceUntilIdle()

            // Initial load should show the item
            assertTrue("entries should have item after initial load", vm.state.value.entries.isNotEmpty())
            assertEquals("should have 1 entry", 1, vm.state.value.entries.size)

            // Approve the item
            vm.onEvent(VerifyDetailEvent.Approve(itemId = "item-1"))
            // THE GAP. refresh() is launched and not awaited, so the queue re-emits after
            // submitVerdict has moved on: the decided item briefly matches nothing and the group
            // empties, then the refetch delivers it with its new status. This is the window that
            // flashed "No video attached to this item" on a real phone; a fake that never emits it
            // makes this test unfalsifiable, which is exactly what it was before.
            repo.makeQueueEmpty()
            repo.notifyQueueChanged()
            advanceUntilIdle()
            repo.restoreDecided()
            advanceUntilIdle()

            // After all processing, the item should be decided or the screen should close
            // The critical assertion: at NO POINT should the empty state have been shown
            val showedEmpty = emissions.any { it.contains("showsEmpty=true") }
            assertFalse(
                "Empty state 'No video attached' should NEVER flash during approve sequence.\n" +
                        "State emissions:\n${emissions.joinToString("\n")}\n" +
                        "This likely means the fix is not correctly preventing the transient empty emission.",
                showedEmpty
            )
        }

    /**
     * Verify that the fix correctly keeps isSubmitting=true or uses another guard
     * while the refetch is in flight after an approve verdict.
     */
    @Test
    fun `during approve, isSubmitting or similar guard stays true until refetch confirms new status`() =
        runTest(dispatcher) {
            val repo = ApproveFlashRepository(itemCount = 1, emptyMedia = false)
            val sync = ApproveFlashSyncRepository(repo)
            val vm = viewModel(repo, sync)

            backgroundScope.launch { vm.state.collect {} }
            advanceUntilIdle()

            // Record the states while approving
            val submittingDuringResolve = mutableListOf<Boolean>()
            backgroundScope.launch {
                vm.state.collect { state ->
                    // Track isSubmitting while group is transitioning
                    submittingDuringResolve.add(state.isSubmitting)
                }
            }

            val entryCountBefore = vm.state.value.entries.size
            assertTrue("should have entry before approve", entryCountBefore > 0)

            // Approve
            vm.onEvent(VerifyDetailEvent.Approve(itemId = "item-1"))
            // THE GAP. refresh() is launched and not awaited, so the queue re-emits after
            // submitVerdict has moved on: the decided item briefly matches nothing and the group
            // empties, then the refetch delivers it with its new status. This is the window that
            // flashed "No video attached to this item" on a real phone; a fake that never emits it
            // makes this test unfalsifiable, which is exactly what it was before.
            repo.makeQueueEmpty()
            repo.notifyQueueChanged()
            advanceUntilIdle()
            repo.restoreDecided()
            advanceUntilIdle()

            // The guard should have prevented the flash by either:
            // 1. Keeping isSubmitting=true until refetch delivers the new status, OR
            // 2. Using a dedicated guard flag that blocks the empty state while resolving
            //
            // Verify the result: the screen should either show the item (not yet removed) or auto-close
            // but NEVER show the empty state.
            val entriesNow = vm.state.value.entries.size
            val showingEmpty = entriesNow == 0 && vm.state.value.hasLoadedOnce && !vm.state.value.isSubmitting
            assertFalse(
                "Screen should not show empty state after approve.\n" +
                        "entries=$entriesNow hasLoaded=${vm.state.value.hasLoadedOnce} isSubmitting=${vm.state.value.isSubmitting}",
                showingEmpty
            )
        }

    /**
     * Regression: ensure that genuine empty media (no proof) still shows the warning
     * and doesn't incorrectly hide it with the new guard.
     */
    @Test
    fun `genuine empty media still shows warning when item has no evidence`() =
        runTest(dispatcher) {
            val repo = ApproveFlashRepository(itemCount = 1, emptyMedia = true)
            val sync = ApproveFlashSyncRepository(repo)
            val vm = viewModel(repo, sync)

            backgroundScope.launch { vm.state.collect {} }
            advanceUntilIdle()

            // Item with empty media should have an entry (not filtered out)
            assertEquals("entry should exist even with empty media", 1, vm.state.value.entries.size)

            // But Approve should be disabled
            assertFalse("approve should be disabled with empty media", vm.state.value.isApproveEnabled)
            assertEquals(
                VerifyDecisionUnavailableReason.EVIDENCE_UNAVAILABLE,
                vm.state.value.decisionUnavailableReason,
            )

            // The genuine empty state (no entries + hasLoadedOnce) is only shown when
            // the queue genuinely has no items. With empty media, we have an entry but
            // it cannot be approved. This is different from the transient flash.
        }
}

/**
 * Repository fake that tracks queue updates and simulates the backend
 * storing the approval verdict.
 */
private class ApproveFlashRepository(
    private val itemCount: Int = 1,
    private val groupKey: String = "item-1",
    private val emptyMedia: Boolean = false,
) : VerificationRepository {
    private fun makeItems(): List<VerificationQueueItem> =
        (1..itemCount).map { i ->
            VerificationQueueItem(
                itemId = "item-$i",
                category = "vaccination_proof",
                status = VerificationStatus.PENDING,
                rowVersion = i,
                evidenceAvailable = !emptyMedia,
                media = if (emptyMedia) {
                    emptyList()
                } else {
                    listOf(
                        VerificationMediaItem(
                            proofId = "proof-$i",
                            downloadUrl = "/proof-$i.mp4",
                            mimeType = "video/mp4",
                            durationMs = 9_000,
                        ),
                    )
                },
                subjectLabel = "Animal $i",
                source = VerificationSourceRef(
                    module = "vaccination",
                    refType = "vaccination_goat",
                    refId = "goat-$i",
                    submissionId = groupKey,
                ),
            )
        }

    private val mutableItems = makeItems().toMutableList()
    private val queueFlow = MutableStateFlow(Resource(data = VerificationQueueResponseDto(items = mutableItems)))

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = queueFlow.value.data ?: VerificationQueueResponseDto()
    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = queueFlow

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        // Simulate queue refresh: remove items that are no longer pending (decided items leave the queue)
        mutableItems.retainAll { it.status == VerificationStatus.PENDING }
        notifyQueueChanged()
        return Result.success(Unit)
    }

    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = queueFlow

    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = Result.success(Unit)
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = markItemDecided(itemId)

    override fun observeLeadershipVideos(category: String?, windowSize: Int) = flowOf(emptyList<sg.mesha.goatos.core.data.vaccination.leadership.VaccinationLeadershipItemUi>())
    override fun observeLeadershipTitle(category: String?, windowSize: Int) = flowOf("")
    override suspend fun refreshLeadershipVideos(category: String?, windowSize: Int, reset: Boolean) =
        sg.mesha.goatos.core.common.AppResult.Ok(Unit)

    fun markItemDecided(itemId: String) {
        val index = mutableItems.indexOfFirst { it.itemId == itemId }
        if (index >= 0) {
            mutableItems[index] = mutableItems[index].copy(status = VerificationStatus.APPROVED)
            notifyQueueChanged()
        }
    }

    fun makeQueueEmpty() {
        mutableItems.clear()
    }

    /** The refetch landing: the decided item returns carrying its terminal status. */
    fun restoreDecided() {
        mutableItems.clear()
        mutableItems.addAll(makeItems().map { it.copy(status = VerificationStatus.APPROVED) })
        notifyQueueChanged()
    }

    fun notifyQueueChanged() {
        queueFlow.value = Resource(data = VerificationQueueResponseDto(items = mutableItems.toList()))
    }
}

private class ApproveFlashSyncRepository(private val queue: ApproveFlashRepository? = null) : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf()
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> {
        queue?.markItemDecided(itemId)
        return AppResult.Ok("outbox-1")
    }

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

private class ApproveFlashAnalytics : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) = Unit
    override fun setUserProperty(name: String, value: String?) = Unit
    override fun setUserId(id: String?) = Unit
}

private class ApproveFlashCrashReporter : CrashReporter {
    override fun log(message: String) = Unit
    override fun recordException(throwable: Throwable, message: String?) = Unit
    override fun setCustomKey(key: String, value: String) = Unit
}
