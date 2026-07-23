package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CountsBreakdownQuery
import sg.mesha.goatos.core.data.CountsRepository
import sg.mesha.goatos.core.network.dto.CountsBreakdownFacetsDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownResponseDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownRowDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownSeriesPointDto
import sg.mesha.goatos.core.network.dto.CountsBreakdownShedFacetDto
import sg.mesha.goatos.core.network.dto.CountsShiftingDestinationsResponseDto
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto
import sg.mesha.goatos.core.network.dto.HerdRegisterSummaryResponseDto
import sg.mesha.goatos.feature.counts.CountsEvent

/**
 * Counts census FILTER behaviour (park / shed / breed).
 *
 * These lock in the three things a filter bar can get quietly and expensively wrong:
 *
 *  1. **The backend does the filtering.** Every selection must re-query the repository with the
 *     matching params. The totals card is a whole-result rollup computed server-side, so a client
 *     that filtered the page it already held would print rows that disagree with the number above
 *     them.
 *  2. **Park -> shed cascades, and changing park RESETS the shed.** Shed names repeat across parks
 *     in the live herd ("Castro 1" exists in both, with different animals), so a shed id carried
 *     across a park change silently describes a different cohort than the one on screen.
 *  3. **Options are keyed by facet key, never by label**, and a shed with no park attribution is
 *     offered under NO park rather than the wrong one.
 *
 * The fake repository mirrors Room: `observeBreakdownTotals` returns a per-scope flow, and a scope
 * that has never been fetched emits `null` data — which is exactly the cold-cache case that the
 * facet carry-forward exists to survive.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class CountsViewModelFilterTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    // -----------------------------------------------------------------------
    // 1. Selecting a filter re-queries the backend with the matching params
    // -----------------------------------------------------------------------

    @Test
    fun `selecting a park re-queries the backend scoped to that park`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()

        assertEquals(
            "the park selection must reach the repository as park_id",
            PARK_CBE,
            repo.totalsQueries.last().parkId,
        )
        assertEquals(CBE_TOTAL, vm.state.value.totals.totalCount)
        job.cancelAndJoin()
    }

    @Test
    fun `filters combine into one scoped query`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        vm.onEvent(CountsEvent.SelectBreed(BREED))
        advanceUntilIdle()

        val query = repo.totalsQueries.last()
        assertEquals(PARK_CBE, query.parkId)
        assertEquals(BREED, query.breed)
        job.cancelAndJoin()
    }

    /**
     * A blank selection is "no filter", NOT an empty-string filter — an empty `breed` would filter
     * to animals whose breed is literally blank, a real and very different cohort.
     */
    @Test
    fun `clearing a dimension omits the parameter rather than sending a blank`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectBreed(BREED))
        advanceUntilIdle()
        vm.onEvent(CountsEvent.SelectBreed(""))
        advanceUntilIdle()

        assertNull("a cleared breed must be omitted, not sent blank", repo.totalsQueries.last().breed)
        job.cancelAndJoin()
    }

    // -----------------------------------------------------------------------
    // 2. The park -> shed cascade
    // -----------------------------------------------------------------------

    @Test
    fun `changing park resets the selected shed`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        vm.onEvent(CountsEvent.SelectShed(SHED_CBE_CASTRO1))
        advanceUntilIdle()
        assertEquals(SHED_CBE_CASTRO1, repo.totalsQueries.last().shedId)

        vm.onEvent(CountsEvent.SelectPark(PARK_CPT))
        advanceUntilIdle()

        assertNull(
            "a shed from the previous park must not survive a park change",
            repo.totalsQueries.last().shedId,
        )
        assertEquals("", vm.state.value.filters.selectedShedId)
        assertNull(vm.state.value.filters.selectedShedLabel)
        job.cancelAndJoin()
    }

    @Test
    fun `shed options are narrowed to the selected park`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()
        val cbeSheds = vm.state.value.filters.sheds
        assertEquals(listOf(SHED_CBE_CASTRO1), cbeSheds.map { it.key })
        assertEquals("Castro 1", cbeSheds.single().label)

        vm.onEvent(CountsEvent.SelectPark(PARK_CPT))
        advanceUntilIdle()
        val cptSheds = vm.state.value.filters.sheds

        // Same LABEL, different park, different shed id — the exact collision that makes a
        // label-keyed dropdown filter to the wrong cohort.
        assertEquals(listOf(SHED_CPT_CASTRO1), cptSheds.map { it.key })
        assertEquals("Castro 1", cptSheds.single().label)
        job.cancelAndJoin()
    }

    @Test
    fun `shed filter is disabled until a park is chosen`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(vm.state.value.filters.shedFilterSupported)
        assertFalse(
            "with no park chosen there is no unambiguous shed vocabulary to offer",
            vm.state.value.filters.isShedFilterEnabled,
        )

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()
        assertTrue(vm.state.value.filters.isShedFilterEnabled)
        job.cancelAndJoin()
    }

    /**
     * Graceful degradation: a backend that has not shipped the `sheds` facet must leave the shed
     * dropdown disabled, not empty-but-tappable, and must never crash the screen.
     */
    @Test
    fun `missing sheds facet degrades to an unsupported shed filter`() = runTest(dispatcher) {
        val repo = FakeCountsRepository(facets = FACETS.copy(sheds = emptyList()))
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()

        assertFalse(vm.state.value.filters.shedFilterSupported)
        assertFalse(vm.state.value.filters.isShedFilterEnabled)
        assertTrue(vm.state.value.filters.sheds.isEmpty())
        // The rest of the bar still works.
        assertEquals(2, vm.state.value.filters.parks.size)
        job.cancelAndJoin()
    }

    /** A shed the backend could not attribute to a park is offered under NO park, never the wrong one. */
    @Test
    fun `shed without park attribution is never offered`() = runTest(dispatcher) {
        val orphan = CountsBreakdownShedFacetDto(key = "orphan", label = "Orphan shed", count = 5, parkId = "")
        val repo = FakeCountsRepository(facets = FACETS.copy(sheds = FACETS.sheds + orphan))
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()
        assertFalse(vm.state.value.filters.sheds.any { it.key == "orphan" })

        vm.onEvent(CountsEvent.SelectPark(PARK_CPT))
        advanceUntilIdle()
        assertFalse(vm.state.value.filters.sheds.any { it.key == "orphan" })
        job.cancelAndJoin()
    }

    /**
     * Shed subtotals must render on the all-parks view even when the cascaded shed dropdown is empty.
     *
     * The cascaded [sheds] are narrowed to the selected park for correctness — an operator who picks
     * a park must see only sheds in that park. On the default all-parks view, [sheds] is empty
     * because no park is selected. But the [shedSubtotals] field carries the FULL shed list
     * regardless, so the screen can look up per-shed head counts for the subtotal divider.
     */
    @Test
    fun `shed subtotals are available on the all-parks view even when the cascaded sheds list is empty`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        // On the default view, no park is selected.
        assertEquals("", vm.state.value.filters.selectedParkId)
        // The cascaded shed dropdown is empty because there's no unambiguous shed vocabulary without a park.
        assertTrue(
            "cascaded sheds must be empty on all-parks view (no park selected)",
            vm.state.value.filters.sheds.isEmpty(),
        )
        // But the full subtotals list must still carry both sheds for display lookups.
        assertEquals(
            "shedSubtotals must contain the FULL list for subtotal display regardless of park selection",
            setOf(SHED_CBE_CASTRO1, SHED_CPT_CASTRO1),
            vm.state.value.filters.shedSubtotals.map { it.key }.toSet(),
        )

        // Verify the counts are correct.
        assertEquals(63, vm.state.value.filters.shedSubtotals.find { it.key == SHED_CBE_CASTRO1 }?.count)
        assertEquals(32, vm.state.value.filters.shedSubtotals.find { it.key == SHED_CPT_CASTRO1 }?.count)
        job.cancelAndJoin()
    }

    /** Shed subtotals stay complete even after selecting a park (whose cascaded sheds may be narrowed). */
    @Test
    fun `shed subtotals remain complete after selecting a park`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()

        // The cascaded sheds are now narrowed to CBE only.
        assertEquals(listOf(SHED_CBE_CASTRO1), vm.state.value.filters.sheds.map { it.key })
        // But the full subtotals list still contains both sheds, so the screen can render subtotals
        // for any shed that appears in the paged rows, even if that shed belongs to a different park.
        assertEquals(
            "shedSubtotals must stay complete even with a park selection",
            setOf(SHED_CBE_CASTRO1, SHED_CPT_CASTRO1),
            vm.state.value.filters.shedSubtotals.map { it.key }.toSet(),
        )
        job.cancelAndJoin()
    }

    // -----------------------------------------------------------------------
    // 3. Vocabulary survives a cold filtered scope
    // -----------------------------------------------------------------------

    /**
     * Selecting a filter for the first time hits a cold Room cache for that scope, so its envelope
     * arrives null. The filter bar must keep the vocabulary it already had — otherwise the operator
     * picks a park, the dropdowns empty themselves, and there is nothing left to pick a shed from.
     */
    @Test
    fun `facet vocabulary survives a cold filtered scope`() = runTest(dispatcher) {
        val repo = FakeCountsRepository(coldScopes = true)
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()
        assertEquals(2, vm.state.value.filters.parks.size)

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()

        assertEquals(
            "the park vocabulary must not vanish while the new scope is still loading",
            2,
            vm.state.value.filters.parks.size,
        )
        assertEquals(1, vm.state.value.filters.breeds.size)
        assertTrue(vm.state.value.filters.isShedFilterEnabled)
        job.cancelAndJoin()
    }

    // -----------------------------------------------------------------------
    // 3b. Lifecycle facet — the new Live/Sold/Culled/Dead/Transferred dimension
    // -----------------------------------------------------------------------

    @Test
    fun `selecting a lifecycle status re-queries the backend and does not reset park or shed`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        vm.onEvent(CountsEvent.SelectShed(SHED_CBE_CASTRO1))
        vm.onEvent(CountsEvent.SelectLifecycle(LIFECYCLE_SOLD))
        advanceUntilIdle()

        val query = repo.totalsQueries.last()
        assertEquals(LIFECYCLE_SOLD, query.lifecycleStatus)
        assertEquals("lifecycle is independent of park/shed, unlike the park->shed cascade", PARK_CBE, query.parkId)
        assertEquals(SHED_CBE_CASTRO1, query.shedId)
        job.cancelAndJoin()
    }

    @Test
    fun `lifecycle facet options and label resolve from the backend vocabulary`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(vm.state.value.filters.lifecycleFilterSupported)
        assertEquals(
            listOf("alive", "sold", "dead", "culled"),
            vm.state.value.filters.lifecycles.map { it.key },
        )

        vm.onEvent(CountsEvent.SelectLifecycle(LIFECYCLE_SOLD))
        advanceUntilIdle()
        assertEquals("Sold", vm.state.value.filters.selectedLifecycleLabel)
        job.cancelAndJoin()
    }

    /** Clearing lifecycle omits the parameter, returning to the backend's live-herd default. */
    @Test
    fun `clearing lifecycle omits the parameter`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectLifecycle(LIFECYCLE_SOLD))
        advanceUntilIdle()
        vm.onEvent(CountsEvent.SelectLifecycle(""))
        advanceUntilIdle()

        assertNull(repo.totalsQueries.last().lifecycleStatus)
        assertFalse(vm.state.value.filters.hasActiveFilter)
        job.cancelAndJoin()
    }

    /** A backend that has not shipped the `lifecycle` facet degrades to unsupported, never a crash. */
    @Test
    fun `missing lifecycle facet degrades to unsupported`() = runTest(dispatcher) {
        val repo = FakeCountsRepository(facets = FACETS.copy(lifecycle = emptyList()))
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        assertFalse(vm.state.value.filters.lifecycleFilterSupported)
        assertTrue(vm.state.value.filters.lifecycles.isEmpty())
        job.cancelAndJoin()
    }

    // -----------------------------------------------------------------------
    // 4. Clear
    // -----------------------------------------------------------------------

    @Test
    fun `clear drops every filter and returns to the unfiltered herd`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val vm = newViewModel(repo)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        vm.onEvent(CountsEvent.SelectShed(SHED_CBE_CASTRO1))
        vm.onEvent(CountsEvent.SelectBreed(BREED))
        advanceUntilIdle()
        assertTrue(vm.state.value.filters.hasActiveFilter)

        vm.onEvent(CountsEvent.ClearFilters)
        advanceUntilIdle()

        val query = repo.totalsQueries.last()
        assertNull(query.parkId)
        assertNull(query.shedId)
        assertNull(query.breed)
        assertFalse(vm.state.value.filters.hasActiveFilter)
        assertEquals(HERD_TOTAL, vm.state.value.totals.totalCount)
        job.cancelAndJoin()
    }

    // -----------------------------------------------------------------------
    // 5. Telemetry
    // -----------------------------------------------------------------------

    @Test
    fun `applying and clearing a filter emits the analytics event`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val analytics = RecordingAnalytics()
        val vm = newViewModel(repo, analytics)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        vm.onEvent(CountsEvent.SelectBreed(""))
        advanceUntilIdle()

        val filterEvents = analytics.events.filter { it.name == AnalyticsEvents.COUNTS_FILTER_APPLIED }
        assertEquals(1, filterEvents.size)
        assertEquals("park", filterEvents.single().props[AnalyticsEvents.Params.DIMENSION])
        assertEquals("set", filterEvents.single().props[AnalyticsEvents.Params.ACTION])

        vm.onEvent(CountsEvent.SelectPark(""))
        advanceUntilIdle()
        val cleared = analytics.events.last { it.name == AnalyticsEvents.COUNTS_FILTER_APPLIED }
        assertEquals("cleared", cleared.props[AnalyticsEvents.Params.ACTION])
        job.cancelAndJoin()
    }

    /** Re-picking the value already selected is not an interaction and must not be counted as one. */
    @Test
    fun `re-selecting the same value emits nothing`() = runTest(dispatcher) {
        val repo = FakeCountsRepository()
        val analytics = RecordingAnalytics()
        val vm = newViewModel(repo, analytics)
        val job = launch { vm.state.collect {} }
        advanceUntilIdle()

        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()
        val before = repo.totalsQueries.size
        vm.onEvent(CountsEvent.SelectPark(PARK_CBE))
        advanceUntilIdle()

        assertEquals(before, repo.totalsQueries.size)
        assertEquals(
            1,
            analytics.events.count { it.name == AnalyticsEvents.COUNTS_FILTER_APPLIED },
        )
        job.cancelAndJoin()
    }

    // -----------------------------------------------------------------------
    // Fixtures
    // -----------------------------------------------------------------------

    private fun newViewModel(
        repo: CountsRepository,
        analytics: RecordingAnalytics = RecordingAnalytics(),
    ) = CountsViewModel(repo, analytics, NoopCrashReporter())

    private companion object {
        const val PARK_CBE = "park-cbe"
        const val PARK_CPT = "park-cpt"

        /** Same LABEL in both parks, different ids — the live "Castro 1" collision. */
        const val SHED_CBE_CASTRO1 = "shed-cbe-castro1"
        const val SHED_CPT_CASTRO1 = "shed-cpt-castro1"

        const val BREED = "Beetal"
        const val HERD_TOTAL = 1682
        const val CBE_TOTAL = 951
        const val LIFECYCLE_SOLD = "sold"

        val FACETS = CountsBreakdownFacetsDto(
            lifecycle = listOf(
                CountsBreakdownSeriesPointDto(key = "alive", label = "Live", count = 1682),
                CountsBreakdownSeriesPointDto(key = LIFECYCLE_SOLD, label = "Sold", count = 40),
                CountsBreakdownSeriesPointDto(key = "dead", label = "Dead", count = 12),
                CountsBreakdownSeriesPointDto(key = "culled", label = "Culled", count = 6),
            ),
            parks = listOf(
                CountsBreakdownSeriesPointDto(key = PARK_CBE, label = "CBE", count = 951),
                CountsBreakdownSeriesPointDto(key = PARK_CPT, label = "CPT", count = 731),
            ),
            breeds = listOf(CountsBreakdownSeriesPointDto(key = BREED, label = BREED, count = 442)),
            sheds = listOf(
                CountsBreakdownShedFacetDto(SHED_CBE_CASTRO1, "Castro 1", 63, PARK_CBE),
                CountsBreakdownShedFacetDto(SHED_CPT_CASTRO1, "Castro 1", 32, PARK_CPT),
            ),
        )
    }

    /**
     * Room-shaped fake: one flow per filter scope, recording every scope it was asked for.
     *
     * [coldScopes] models the real first-selection case — a scope that has never been fetched emits
     * `Resource(data = null)`, exactly as `readCachedJson` does on a cold cache.
     */
    private class FakeCountsRepository(
        private val facets: CountsBreakdownFacetsDto = FACETS,
        private val coldScopes: Boolean = false,
    ) : CountsRepository {

        val totalsQueries = mutableListOf<CountsBreakdownQuery>()

        override fun observeHerdSummary(
            lifecycleStatus: String?,
            parkId: String?,
            breed: String?,
            sex: String?,
        ): Flow<Resource<HerdRegisterSummaryResponseDto>> =
            MutableStateFlow(Resource(data = HerdRegisterSummaryResponseDto()))

        override suspend fun refreshHerdSummary(
            lifecycleStatus: String?,
            parkId: String?,
            breed: String?,
            sex: String?,
        ): Result<Unit> = Result.success(Unit)

        override fun observeBreakdownTotals(
            query: CountsBreakdownQuery,
        ): Flow<Resource<CountsBreakdownResponseDto>> {
            totalsQueries += query
            val isBaseScope = query.parkId == null && query.shedId == null && query.breed == null
            if (coldScopes && !isBaseScope) return MutableStateFlow(Resource(data = null))
            return MutableStateFlow(
                Resource(
                    data = CountsBreakdownResponseDto(
                        totalCount = if (query.parkId == PARK_CBE) CBE_TOTAL else HERD_TOTAL,
                        facets = facets,
                    ),
                    lastSyncedAt = 1L,
                ),
            )
        }

        override fun breakdownRows(query: CountsBreakdownQuery): Flow<PagingData<CountsBreakdownRowDto>> =
            flowOf(PagingData.empty<CountsBreakdownRowDto>()).map { it }

        override fun observeShiftingDestinations(): Flow<Resource<CountsShiftingDestinationsResponseDto>> =
            MutableStateFlow(Resource(data = CountsShiftingDestinationsResponseDto()))

        override suspend fun refreshShiftingDestinations(): Result<Unit> = Result.success(Unit)

        override suspend fun lookupAnimals(
            query: String,
            parkId: String?,
            shedId: String?,
        ): Result<List<GoatSearchItemDto>> = Result.success(emptyList())
    }
}
