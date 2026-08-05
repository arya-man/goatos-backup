package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
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
import sg.mesha.goatos.feature.verify.VerifyQueueEvent
import sg.mesha.goatos.feature.verify.VerifyQueueUiState

/**
 * State-SEQUENCE regressions for [VerifyQueueViewModel]. A screenshot test sees one frame and
 * cannot catch a bad TRANSITION -- every property below is a bug class that shipped for real and
 * was only caught by a human watching a phone (see the class-level comments on `_hasLoadedOnce`,
 * `loadedScopeKey`, and the `refresh()` try/finally in VerifyQueueViewModel.kt).
 *
 * Turbine is not on this module's classpath at the time this file was written (no `turbine` in
 * gradle/libs.versions.toml at the start of this session) -- emissions are collected manually via
 * `toList` on an Unconfined test dispatcher instead, per the existing convention in
 * VerifyQueueViewModelTest.kt / VerifyQueueShedGroupingTest.kt in this same package.
 *
 * Each property below was proven to bite by temporarily reintroducing the real bug in
 * VerifyQueueViewModel.kt, confirming RED, then reverting to confirm GREEN. See the session
 * report for the exact failure messages observed for each.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class VerifyQueueLoadSequenceTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    /**
     * FLASH: a screen published a confident answer before it had one -- "Queue clear" (the
     * EmptyState branch, which requires `hasLoadedOnce == true`) must never render before a
     * single row has actually been read. The very first combined state produced while the fake
     * repo's refresh is still suspended (nothing read yet) must be the SKELETON
     * (`rows.isEmpty() && !hasLoadedOnce`), never the confident-empty gate
     * (`rows.isEmpty() && hasLoadedOnce`) -- see VerifyQueueScreen.kt's `rows.isEmpty() &&
     * !state.hasLoadedOnce` skeleton branch.
     */
    @Test
    fun `no confident empty state renders before the first read completes`() = runTest(dispatcher) {
        val repo = SequenceFakeRepo(refreshDelayMs = 50)
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = SequenceFakeSyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
        )

        // Subscribing here observes whatever the combine has produced from the synchronous
        // portion of init{refresh()} that already ran (Unconfined dispatcher runs eagerly up to
        // the first suspension point, which is the fake's delay() inside refreshQueue) --
        // i.e. this is the in-flight snapshot, mid-read, captured before a single row was read.
        val seen = mutableListOf<VerifyQueueUiState>()
        val job = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { vm.state.toList(seen) }

        val midFlight = seen.first()
        assertTrue("mid-read snapshot must have no rows yet", midFlight.rows.isEmpty())
        assertFalse(
            "FLASH: hasLoadedOnce must not be true before the fake's refresh() has resolved -- " +
                "that is exactly 'Queue clear' rendering before a single row was read",
            midFlight.hasLoadedOnce,
        )

        advanceUntilIdle()
        assertTrue("refresh must eventually resolve", seen.last().hasLoadedOnce)
        job.cancel()
    }

    /**
     * WEDGE: a loading gate that never resolves. If the network call throws, `hasLoadedOnce`
     * must STILL become true (set in `finally`) -- otherwise the screen wedges on the skeleton
     * forever, because a marker set only after a successful result never runs when the call
     * threw first.
     *
     * Getting an actually-uncaught exception (no catch anywhere -- exactly the real,
     * unprotected `repo.refreshQueue(...)` call in `refresh()`) to run inside a JVM unit test
     * without the test HARNESS itself flagging that "uncaught exception" as the failure (instead
     * of asserting the state afterward) took three dead ends to rule out first:
     *  - throwing on the FIRST (constructor-triggered) refresh means `VerifyQueueViewModel(...)`
     *    itself never returns -- there is no `vm` reference left to inspect afterward.
     *  - kotlinx-coroutines-test registers a GLOBAL `CoroutineExceptionHandler` via
     *    `META-INF/services` (`kotlinx.coroutines.test.internal.ExceptionCollectorAsService`)
     *    that attributes ANY coroutine's uncaught exception to whichever `TestScope` is
     *    currently active in the JVM -- independent of Job hierarchy, independent of which
     *    scheduler/dispatcher the failing coroutine actually runs on. Neither a separate
     *    `UnconfinedTestDispatcher` for Main, nor `Thread.setUncaughtExceptionHandler`, nor a
     *    try/catch around the triggering call intercepts it while a `runTest { }` is still
     *    running -- it is collected and rethrown at that `runTest` call's own completion,
     *    on top of (not instead of) any exception this test body already caught.
     * The fix: finish `runTest { }` for the SETUP (build the VM against a working fake, let it
     * load once, `vm` now exists with `hasLoadedOnce == true`) and let it return normally --
     * once that TestScope has completed, the global collector has nothing live to attribute a
     * later exception to. THEN, back in plain (non-suspend) test code with `runTest` no longer
     * active, flip the fake to throw and force a scope change: zero suspension
     * (`refreshDelayMs = 0`) + the still-installed Unconfined Main dispatcher means that second
     * `refresh()` runs synchronously to completion inline on this call -- try, finally, then the
     * throw continuing to unwind, uncaught by anything, exactly like production -- so it
     * surfaces as a plain Kotlin exception straight out of `onEvent(...)`, which THIS test can
     * catch and then assert against.
     */
    @Test
    fun `hasLoadedOnce still resolves when refresh throws`() {
        val repo = SequenceFakeRepo(itemsToPublish = twoItems())
        lateinit var vm: VerifyQueueViewModel

        runTest(dispatcher) {
            vm = VerifyQueueViewModel(
                repo = repo,
                syncRepo = SequenceFakeSyncRepository(),
                analytics = NoopAnalytics(),
                savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
            )
            val job = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { vm.state.collect {} }
            advanceUntilIdle()
            assertTrue("precondition: scope A loaded normally", vm.state.value.hasLoadedOnce)
            job.cancel()
        }

        // runTest has returned; its TestScope is no longer the active one. Dispatchers.Main is
        // still the Unconfined dispatcher installed in setUp(), so this still runs the VM's
        // coroutine inline, synchronously, on this thread.
        repo.throwOnRefresh = true
        try {
            vm.onEvent(VerifyQueueEvent.SelectCategory("weighing_proof"))
        } catch (expected: RuntimeException) {
            // repo.refreshQueue() was configured to throw, and refresh() has no catch for it --
            // only try/finally -- so it propagates. This IS the WEDGE scenario under test; the
            // finally block already ran, setting hasLoadedOnce back to true, before this catch.
        }

        assertTrue(
            "WEDGE: a throwing refresh must still flip hasLoadedOnce true (via finally) or the " +
                "screen spins forever over a blank list",
            vm.state.value.hasLoadedOnce,
        )
    }

    /**
     * YANK: content already drawn must not be removed by a later refresh. Once rows are on
     * screen, pulling to refresh (same scope) must never produce an emission with an empty row
     * list while the refresh is in flight -- the gate must never fall back to bare
     * `isRefreshing` to decide what to show.
     */
    @Test
    fun `a second refresh on the same scope never empties already-drawn rows`() = runTest(dispatcher) {
        val repo = SequenceFakeRepo(refreshDelayMs = 30, itemsToPublish = twoItems())
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = SequenceFakeSyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
        )
        val seen = mutableListOf<VerifyQueueUiState>()
        val job = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { vm.state.toList(seen) }
        advanceUntilIdle()
        assertTrue("precondition: first read drew rows", vm.state.value.rows.isNotEmpty())

        seen.clear()
        vm.onEvent(VerifyQueueEvent.Refresh)
        advanceUntilIdle()

        assertTrue(
            "YANK: a same-scope refresh must never emit an empty row list while cached rows " +
                "were already drawn",
            seen.none { it.rows.isEmpty() },
        )
        job.cancel()
    }

    /**
     * STALE SCOPE: a global (unscoped) loaded-marker must not let the previous filter's answer
     * stand in for a newly selected filter. Switching category/park/shed/status/date must reset
     * the marker BEFORE the new scope's data arrives (so the old scope's rows/confident-empty
     * cannot masquerade as the new scope's answer); a refresh on the SAME scope must not reset it.
     */
    @Test
    fun `switching scope resets the loaded marker, same-scope refresh does not`() = runTest(dispatcher) {
        // Needs a suspension point: without it, a same-scope reintroduction of this bug -- or
        // the fix -- would run the entire refresh() to completion synchronously (Unconfined,
        // zero-delay fake) inside onEvent(), and the transient reset would never be observable
        // between the SelectCategory call returning and advanceUntilIdle() resolving it.
        val repo = SequenceFakeRepo(refreshDelayMs = 20, itemsToPublish = twoItems())
        val vm = VerifyQueueViewModel(
            repo = repo,
            syncRepo = SequenceFakeSyncRepository(),
            analytics = NoopAnalytics(),
            savedStateHandle = SavedStateHandle(mapOf("category" to "vaccination_proof")),
        )
        val seen = mutableListOf<VerifyQueueUiState>()
        val job = backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { vm.state.toList(seen) }
        advanceUntilIdle()
        assertTrue("precondition: scope A has loaded", vm.state.value.hasLoadedOnce)

        // Same-scope refresh (pull to refresh) must NOT reset the marker.
        vm.onEvent(VerifyQueueEvent.Refresh)
        assertTrue(
            "same-scope refresh must not reset hasLoadedOnce -- that would blank a legitimately " +
                "loaded queue while it silently re-reads",
            vm.state.value.hasLoadedOnce,
        )
        advanceUntilIdle()

        // Switching category is a brand-new scope, with nothing published for it yet.
        vm.onEvent(VerifyQueueEvent.SelectCategory("weighing_proof"))
        assertFalse(
            "STALE SCOPE: switching category must reset hasLoadedOnce immediately -- otherwise " +
                "scope A's answer (rows or confident-empty) stands in for scope B, which has not " +
                "been read yet",
            vm.state.value.hasLoadedOnce,
        )
        advanceUntilIdle()
        job.cancel()
    }
}

private fun twoItems(): List<VerificationQueueItem> = (1..2).map { n ->
    VerificationQueueItem(
        itemId = "seq-item-$n",
        category = "vaccination_proof",
        status = VerificationStatus.PENDING,
        rowVersion = n,
        subjectLabel = "Goat #$n",
        shedLabel = "Gandhi 1",
        source = VerificationSourceRef(refType = "vaccination_goat", submissionId = "seq-submission"),
        media = listOf(VerificationMediaItem(proofId = "seq-proof-$n", downloadUrl = "/seq-proof-$n.mp4")),
    )
}

private fun scopeStoreKey(
    category: String?,
    status: String?,
    businessDate: String?,
    missed: Boolean?,
    parkId: String?,
    shedId: String?,
): String = listOf(category, status, businessDate, missed, parkId, shedId).joinToString("|")

/**
 * Room-shaped fake: [observeQueue] returns a per-scope [MutableStateFlow] that only changes when
 * [refreshQueue] (for that same scope) actually pushes [itemsToPublish] into it -- exactly like
 * the real cache-first repository, where the observed Flow never mutates on its own between
 * successful refreshes.
 */
private class SequenceFakeRepo(
    private val refreshDelayMs: Long = 0,
    initialThrowOnRefresh: Boolean = false,
    private val itemsToPublish: List<VerificationQueueItem> = emptyList(),
) : VerificationRepository {
    private val states = mutableMapOf<String, MutableStateFlow<List<VerificationQueueItem>>>()
    var refreshCalls = 0
        private set

    /** `var` (not constructor-fixed) so a test can let the FIRST refresh succeed normally and
     *  only start throwing from a later one -- see the WEDGE test's sequencing rationale. */
    var throwOnRefresh: Boolean = initialThrowOnRefresh

    private fun stateFor(key: String) = states.getOrPut(key) { MutableStateFlow(emptyList()) }

    override suspend fun queue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?, cursor: String?): VerificationQueueResponseDto = error("unused")

    override fun observeQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> {
        val key = scopeStoreKey(category, status, businessDate, missed, parkId, shedId)
        return stateFor(key).map { items -> Resource(data = VerificationQueueResponseDto(items = items)) }
    }

    override suspend fun refreshQueue(category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> {
        refreshCalls++
        if (refreshDelayMs > 0) delay(refreshDelayMs)
        if (throwOnRefresh) throw RuntimeException("SequenceFakeRepo: simulated refresh failure")
        val key = scopeStoreKey(category, status, businessDate, missed, parkId, shedId)
        stateFor(key).value = itemsToPublish
        return Result.success(Unit)
    }

    override suspend fun appendQueue(cursor: String, category: String?, status: String?, businessDate: String?, missed: Boolean?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override fun observeActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Flow<Resource<VerificationQueueResponseDto>> = error("unused")
    override suspend fun refreshActionQueue(category: String?, parkId: String?, shedId: String?, limit: Int?): Result<Unit> = error("unused")
    override suspend fun markVaccinationBatchClosedLocally(batchId: String, category: String?, parkId: String?, shedId: String?, limit: Int?) = Unit
    override suspend fun markVerificationItemDecidedLocally(itemId: String) = Unit
}

private class SequenceFakeSyncRepository : SyncRepository {
    override fun observeStatus() = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationBatchClose(batchId: String): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
