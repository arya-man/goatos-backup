package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.IndividualWeighingDraft
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedWeighingDraft
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingCsvExport
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedCache
import sg.mesha.goatos.core.data.weighing.WeighingPage
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalogCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingPlannerParkBucketsCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScanMatch
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskListCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskLookup
import sg.mesha.goatos.core.data.weighing.WeighingParkRef
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatBucket
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeed
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import sg.mesha.goatos.ui.Routes

/**
 * Regression coverage for the restored "Edit task" wizard opening on an EMPTY bucket step.
 *
 * Root cause: [WeighingPlanWizardViewModel]'s catalog refresh and park-bucket refresh both gated
 * their "one fetch at a time" dedupe on the SAME shared [sg.mesha.goatos.viewmodel.WizardRaw.loading]
 * flag. The edit wizard pre-hydrates its date, so `loadCatalog` fires the catalog refresh and --
 * off the very first (already-cached) catalog emission -- the FIRST park-bucket refresh, back to
 * back, before the catalog network call has returned. The bucket refresh saw the catalog refresh's
 * `loading = true` still up, bailed out through its own dedupe guard, and nothing ever retries it:
 * `observeBucketsJob` is already non-null, so the catalog observer's one-shot trigger never fires
 * again. The bucket step's Room-backed cache stays permanently empty, and saving from there would
 * strip every shed off the published campaign.
 *
 * This fake reproduces the exact race: the catalog stream starts with a cache already present (a
 * park list from a prior session, as on a real device) and its refresh is held open on a gate the
 * test controls; the bucket stream starts genuinely EMPTY and only gets sheds once
 * `refreshPlannerParkBuckets` is actually called and writes into it. Against the pre-fix
 * implementation this test fails with zero hydrated selections; it passes once the two refreshes
 * are deduped against their own independent in-flight flags.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class WeighingPlanWizardEditHydrationTest {

    // STANDARD, not Unconfined: launched coroutines must stay QUEUED until the test pumps the
    // scheduler, exactly like the real Main dispatcher defers a `viewModelScope.launch` to the
    // next tick. An unconfined dispatcher runs `loadCatalog`'s collector job eagerly and inline,
    // which reorders it AHEAD of `refreshCatalog`'s synchronous `loading = true` write and hides
    // the race this test exists to catch.
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `edit wizard hydrates its shed buckets even when the catalog refresh is still in flight`() =
        runTest(dispatcher) {
            val repository = RaceReproducingWeighingRepository()
            val seedStore = WeighingRepeatSeedStore()
            seedStore.stage(
                sourceCampaignId = "campaign-cbe",
                seed = WeighingRepeatSeed(
                    parkId = "park-cbe",
                    parkName = "CBE",
                    sourceDateLabel = "Wed 5 Aug",
                    buckets = listOf(
                        WeighingRepeatBucket("loc-godel-1", "individual_animal", "user-pramod"),
                        WeighingRepeatBucket("loc-yashoda-1", "individual_animal", "user-pramod"),
                        WeighingRepeatBucket("loc-gandhi-1", "individual_animal", "user-pramod"),
                        WeighingRepeatBucket("loc-gandhi-2", "individual_animal", "user-pramod"),
                    ),
                    editCampaignId = "campaign-cbe",
                    editWeighDate = "2026-08-05",
                ),
            )

            val vm = WeighingPlanWizardViewModel(
                repository = repository,
                repeatSeedStore = seedStore,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
                savedStateHandle = SavedStateHandle(
                    mapOf(Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
                ),
            )
            // WhileSubscribed(5_000): nothing recomputes unless someone is actually collecting,
            // exactly like the composable in production.
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()

            // The catalog refresh is still awaiting its gate here -- this is the exact window in
            // which the old shared-flag guard silently dropped the bucket refresh for good.
            assertTrue(
                "expected the catalog refresh to still be in flight for this race to be exercised",
                repository.catalogRefreshStarted && !repository.catalogRefreshGate.isCompleted,
            )

            val hydrated = vm.state.value
            assertEquals(
                "expected the campaign's 4 sheds to be hydrated as Added while the catalog " +
                    "refresh was still in flight, got: bucketRows=${hydrated.bucketRows}",
                4,
                hydrated.addedCount,
            )
            assertEquals(
                setOf("loc-godel-1", "loc-yashoda-1", "loc-gandhi-1", "loc-gandhi-2"),
                hydrated.bucketRows.filter { it.added }.map { it.locationId }.toSet(),
            )
            assertTrue(
                "expected the wizard to have actually called refreshPlannerParkBuckets, not just " +
                    "read pre-seeded data",
                repository.parkBucketRefreshCount >= 1,
            )
            assertEquals(0, hydrated.repeatDroppedCount)

            // Let the catalog refresh resolve too, so the test does not leak a suspended coroutine.
            repository.catalogRefreshGate.complete(Unit)
            advanceUntilIdle()
        }

    /**
     * DEFECT (2026-08-04). Removing a shed from the edit wizard's BUCKETS step and re-adding it
     * via "Add remaining N" (or a single re-tap) silently reset it to lump-sum with no operator,
     * even though the campaign's own seed ([WeighingRepeatSeed.buckets]) still carries it as
     * individual-mode assigned to Pramod. [WeighingPlanWizardViewModel.toggleBucket] hardcoded
     * `PER_SHED_PARTITION_CATEGORY` for every re-add instead of consulting the seed a second time,
     * so a live operator's capture mode flipped with no prompt.
     */
    @Test
    fun `removing then re-adding a seeded bucket restores its prior mode and operator`() =
        runTest(dispatcher) {
            val repository = RaceReproducingWeighingRepository()
            val seedStore = WeighingRepeatSeedStore()
            seedStore.stage(
                sourceCampaignId = "campaign-cbe",
                seed = WeighingRepeatSeed(
                    parkId = "park-cbe",
                    parkName = "CBE",
                    sourceDateLabel = "Wed 5 Aug",
                    buckets = listOf(
                        WeighingRepeatBucket("loc-godel-1", "individual_animal", "user-pramod"),
                        WeighingRepeatBucket("loc-yashoda-1", "individual_animal", "user-pramod"),
                        WeighingRepeatBucket("loc-gandhi-1", "individual_animal", "user-pramod"),
                        WeighingRepeatBucket("loc-gandhi-2", "individual_animal", "user-pramod"),
                    ),
                    editCampaignId = "campaign-cbe",
                    editWeighDate = "2026-08-05",
                ),
            )

            val vm = WeighingPlanWizardViewModel(
                repository = repository,
                repeatSeedStore = seedStore,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
                savedStateHandle = SavedStateHandle(
                    mapOf(Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
                ),
            )
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()
            repository.catalogRefreshGate.complete(Unit)
            advanceUntilIdle()

            // Sanity: the seed hydrated Godel 1 as individual-mode, assigned to Pramod.
            val before = vm.state.value.configRows.first { it.locationId == "loc-godel-1" }
            assertEquals("individual_animal", before.category)
            assertEquals("user-pramod", before.operatorUserId)

            // Remove it, then re-add it in the same edit session.
            vm.toggleBucket("loc-godel-1")
            vm.toggleBucket("loc-godel-1")

            val after = vm.state.value.configRows.first { it.locationId == "loc-godel-1" }
            assertEquals(
                "a re-added seeded bucket must keep its prior capture mode, not fall back to lump-sum",
                "individual_animal",
                after.category,
            )
            assertEquals(
                "a re-added seeded bucket must keep its prior operator",
                "user-pramod",
                after.operatorUserId,
            )
        }

    /**
     * DEFECT (2026-08-05). The edit wizard's carried seed ([WeighingRepeatSeed.buckets]) is a
     * SNAPSHOT taken once, when the wizard was staged from the task list's then-cached state. If
     * the campaign's live category/operator has since moved on -- another edit landed, or the
     * operator app reassigned it -- the seed no longer matches server truth, but the live park
     * bucket catalog DOES: [WeighingPlannerShed.scheduledCategory] and
     * [WeighingPlannerShed.scheduledOperatorUserId] carry this exact bucket's CURRENT
     * category/operator even while [WeighingPlannerShed.scheduled] itself reads false, because this
     * wizard's own campaign is excluded from the "already taken" check.
     *
     * This is also DEFECT 3 from the same day: Configure showing stale values after Review -> Back
     * was traced to the SAME root cause, not a separate snapshot -- the wizard hydrates its
     * `selections` map ONCE from the stale seed, and step navigation only changes which step is
     * rendered, never re-derives `selections`. Fixing the hydration source fixes both symptoms.
     */
    @Test
    fun `edit wizard hydrates from the LIVE catalog read, not the stale carried seed`() =
        runTest(dispatcher) {
            val repository = RaceReproducingWeighingRepository(
                // The seed says Individual + Pramod -- stale, captured when the wizard was staged.
                godel1Category = "individual_animal",
                godel1OperatorUserId = "user-pramod",
                // The LIVE catalog read says the campaign now actually holds Lump-sum + Dinakar --
                // e.g. another edit landed after this wizard was staged but before it was opened.
                godel1ScheduledCategory = "per_shed_partition",
                godel1ScheduledOperatorUserId = "user-dinakar",
            )
            val seedStore = WeighingRepeatSeedStore()
            seedStore.stage(
                sourceCampaignId = "campaign-cbe",
                seed = WeighingRepeatSeed(
                    parkId = "park-cbe",
                    parkName = "CBE",
                    sourceDateLabel = "Wed 5 Aug",
                    buckets = listOf(
                        WeighingRepeatBucket("loc-godel-1", "individual_animal", "user-pramod"),
                    ),
                    editCampaignId = "campaign-cbe",
                    editWeighDate = "2026-08-05",
                ),
            )

            val vm = WeighingPlanWizardViewModel(
                repository = repository,
                repeatSeedStore = seedStore,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
                savedStateHandle = SavedStateHandle(
                    mapOf(Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
                ),
            )
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()
            repository.catalogRefreshGate.complete(Unit)
            advanceUntilIdle()

            val godel1 = vm.state.value.configRows.first { it.locationId == "loc-godel-1" }
            assertEquals(
                "Configure must show the LIVE campaign state, never the wizard's own stale seed",
                "per_shed_partition",
                godel1.category,
            )
            assertEquals(
                "Configure must show the LIVE operator, never the wizard's own stale seed",
                "user-dinakar",
                godel1.operatorUserId,
            )

            // DEFECT 3: navigating forward to Review and back must not change what Configure
            // shows -- there is nothing left in this ViewModel that re-derives `selections` from
            // step movement, so a still-wrong answer here means the fix regressed to reading a
            // one-time snapshot again.
            // advanceUntilIdle after every navigation: `state` is a stateIn mapping over `raw`,
            // so the step change is not visible on state.value in the same turn it is made.
            // Reading it straight after next() sees the PREVIOUS step and looks like a refusal.
            vm.next()
            advanceUntilIdle()
            assertEquals(sg.mesha.goatos.feature.weighing.plan.WeighingWizardStep.CONFIGURE, vm.state.value.step)
            vm.next()
            advanceUntilIdle()
            assertEquals(sg.mesha.goatos.feature.weighing.plan.WeighingWizardStep.REVIEW, vm.state.value.step)
            vm.back()
            advanceUntilIdle()
            val afterBack = vm.state.value.configRows.first { it.locationId == "loc-godel-1" }
            assertEquals("per_shed_partition", afterBack.category)
            assertEquals("user-dinakar", afterBack.operatorUserId)
        }

    /**
     * Implements only the planner-catalog and park-bucket surface for real; everything else
     * returns an inert default because this test never exercises it.
     */
    private class RaceReproducingWeighingRepository(
        private val godel1Category: String = "individual_animal",
        private val godel1OperatorUserId: String = "user-pramod",
        private val godel1ScheduledCategory: String = "",
        private val godel1ScheduledOperatorUserId: String = "",
    ) : WeighingRepository {

        // Cursor-append stubs. These fakes exercise the READ path; Ok(0) means "no further
        // page", which leaves every existing assertion about page CONTENTS unchanged.
        override suspend fun appendTaskList(scope: String, parkId: String?): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendTaskBuckets(campaignId: String): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendLeadershipShed(campaignId: String, campaignShedId: String): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendLeadershipVideos(): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendPlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            excludeCampaignId: String?,
        ): AppResult<Int> = AppResult.Ok(0)


        private val catalog = WeighingPlannerCatalog(
            parks = listOf(
                WeighingPlannerPark(
                    parkId = "park-cbe",
                    name = "CBE",
                    kidCount = 0,
                    shedCount = 4,
                    existingCampaign = null,
                ),
            ),
            operators = listOf(
                WeighingPlannerOperator("user-pramod", "Pramod", "PRA"),
                WeighingPlannerOperator("user-dinakar", "Dinakar", "DIN"),
            ),
        )

        // Starts already populated -- exactly like a device that opened this same date's planner
        // before and still holds it in Room.
        private val catalogFlow = MutableStateFlow(
            WeighingPlannerCatalogCache(catalog = catalog, hasCache = true),
        )

        var catalogRefreshStarted = false
            private set
        val catalogRefreshGate = CompletableDeferred<Unit>()

        override fun observePlannerCatalog(periodStartDate: String): Flow<WeighingPlannerCatalogCache> =
            catalogFlow

        override suspend fun refreshPlannerCatalog(periodStartDate: String): AppResult<Int> {
            catalogRefreshStarted = true
            // Held open until the test releases it -- this is what keeps the shared `loading`
            // flag up across the window where the bucket refresh must still succeed.
            catalogRefreshGate.await()
            return AppResult.Ok(catalog.parks.size)
        }

        // Starts genuinely EMPTY: no cache, no sheds. Only `refreshPlannerParkBuckets` below
        // populates it, exactly like a real Room-backed keyset page before its first network write.
        private val bucketsFlow = MutableStateFlow(WeighingPlannerParkBucketsCache())

        var parkBucketRefreshCount = 0
            private set

        override fun observePlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            windowSize: Int,
            excludeCampaignId: String?,
        ): Flow<WeighingPlannerParkBucketsCache> = bucketsFlow

        override suspend fun refreshPlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            reset: Boolean,
            excludeCampaignId: String?,
        ): AppResult<Int> {
            parkBucketRefreshCount += 1
            val sheds = listOf(
                WeighingPlannerShed(
                    locationId = "loc-godel-1",
                    name = "Godel 1",
                    kidCount = 0,
                    category = godel1Category,
                    operatorUserId = godel1OperatorUserId,
                    scheduledCategory = godel1ScheduledCategory,
                    scheduledOperatorUserId = godel1ScheduledOperatorUserId,
                ),
                WeighingPlannerShed(
                    locationId = "loc-yashoda-1",
                    name = "Yashoda 1",
                    kidCount = 0,
                    category = "individual_animal",
                    operatorUserId = "user-pramod",
                ),
                WeighingPlannerShed(
                    locationId = "loc-gandhi-1",
                    name = "Gandhi 1",
                    kidCount = 0,
                    category = "individual_animal",
                    operatorUserId = "user-pramod",
                ),
                WeighingPlannerShed(
                    locationId = "loc-gandhi-2",
                    name = "Gandhi 2",
                    kidCount = 0,
                    category = "individual_animal",
                    operatorUserId = "user-pramod",
                ),
            )
            bucketsFlow.value = WeighingPlannerParkBucketsCache(
                parkId = parkId,
                sheds = sheds,
                canLoadMore = false,
                hasCache = true,
            )
            return AppResult.Ok(sheds.size)
        }

        override suspend fun refreshPlannerParkBucketAvailability(
            periodStartDate: String,
            parkId: String,
            pages: Int,
            excludeCampaignId: String?,
        ): AppResult<Int> = AppResult.Ok(0)

        // ---- everything below this line is untouched by this test ---------------------------

        override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
            MutableStateFlow(WeighingScopeState(emptyList(), emptyList(), emptyList(), 0))

        override suspend fun listAssignments(
            cursor: String?,
            scope: String,
            parkId: String?,
        ): AppResult<WeighingPage<WeighingAssignment>> =
            AppResult.Ok(WeighingPage(items = emptyList(), nextCursor = null))

        override fun observeTaskList(
            filter: String,
            parkId: String?,
            windowSize: Int,
        ): Flow<WeighingTaskListCache> = MutableStateFlow(WeighingTaskListCache())

        override suspend fun refreshTaskList(
            filter: String,
            parkId: String?,
            reset: Boolean,
        ): AppResult<Int> = AppResult.Ok(0)

        override suspend fun getTask(campaignId: String): AppResult<WeighingTaskLookup> =
            AppResult.Err("not configured in this fake")

        override suspend fun listParks(): AppResult<List<WeighingParkRef>> = AppResult.Ok(emptyList())

        override fun observeTaskBuckets(campaignId: String, windowSize: Int): Flow<WeighingTaskBucketCache> =
            MutableStateFlow(WeighingTaskBucketCache())

        override suspend fun refreshTaskBuckets(campaignId: String, reset: Boolean): AppResult<Int> =
            AppResult.Ok(0)

        override suspend fun exportCampaignCsv(campaignId: String): AppResult<WeighingCsvExport> =
            AppResult.Err("not configured in this fake")

        override fun observeLeadershipShed(
            campaignId: String,
            campaignShedId: String,
            windowSize: Int,
        ): Flow<WeighingLeadershipShedCache> = MutableStateFlow(WeighingLeadershipShedCache())

        override suspend fun refreshLeadershipShed(
            campaignId: String,
            campaignShedId: String,
            reset: Boolean,
        ): AppResult<Int> = AppResult.Ok(0)

        override fun observeLeadershipVideos(windowSize: Int): Flow<List<WeighingLeadershipShed>> =
            MutableStateFlow(emptyList())

        override suspend fun refreshLeadershipVideos(reset: Boolean): AppResult<Int> = AppResult.Ok(0)

        override suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?> =
            AppResult.Ok(null)

        override suspend fun createPlan(draft: WeighingPlanDraft, publish: Boolean): AppResult<String> =
            AppResult.Ok("campaign-new")

        override suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?> =
            AppResult.Ok(null)

        override suspend fun publishCampaign(campaignId: String): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun refreshScope(
            campaignId: String,
            workGroupId: String,
            campaignShedId: String,
            maxRows: Int,
        ): AppResult<Int> = AppResult.Ok(0)

        override suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>) = Unit

        override suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch =
            WeighingScanMatch(row = null, outcome = "unknown", expectedLocationLabel = null, actualLocationLabel = null)

        override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> =
            AppResult.Err("not configured in this fake")

        override suspend fun attachIndividualProof(
            scopeKey: String,
            scannedIdentifier: String,
            proofCaptureId: String,
            serverProofId: String?,
        ) = Unit

        override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
            AppResult.Err("not configured in this fake")

        override suspend fun attachShedPartitionProof(
            scopeKey: String,
            proofCaptureId: String,
            serverProofId: String?,
            serverProofIds: List<String>,
        ) = Unit

        override suspend fun discardEditableIndividual(scopeKey: String, scannedIdentifier: String) = Unit

        override suspend fun submitIndividualScope(
            campaignId: String,
            campaignShedId: String,
            scannedIdentifiers: List<String>,
        ): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun reopenScope(
            campaignId: String,
            campaignShedId: String,
            reason: String,
        ): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun closeShedCampaign(
            campaignId: String,
            campaignShedId: String,
            reason: String,
        ): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun closeCampaign(campaignId: String, reason: String): AppResult<Unit> =
            AppResult.Ok(Unit)

        override suspend fun fetchWeightHistory(
            parkId: String?,
            campaignShedId: String?,
        ): AppResult<sg.mesha.goatos.core.network.WeightHistoryResponseDto> =
            AppResult.Err("not configured in this fake")

        override suspend fun fetchGrowthSummary(
            parkId: String?,
            from: String?,
            to: String?,
        ): AppResult<sg.mesha.goatos.core.network.GrowthSummaryDto> =
            AppResult.Err("not configured in this fake")
    }
}
