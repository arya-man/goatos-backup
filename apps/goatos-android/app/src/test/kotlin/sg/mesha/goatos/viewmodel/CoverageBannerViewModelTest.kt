package sg.mesha.goatos.viewmodel

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * MOB-007: Coverage banner offline-first tests. Verify that:
 * 1. Has coverage: banner renders from cache across process death
 * 2. No coverage (cached): banner hidden, state distinct from "offline/unknown"
 * 3. Network error on cold start (no cache): banner hidden, state is Unknown
 * 4. Failed refresh with cache: keeps prior state (coverage or no-coverage)
 * 5. Coverage state is explicitly distinguished: HasCoverage / NoCoverage / Unknown
 *
 * The [FakeCoverageRepository] backs coverage with a MutableStateFlow, mirroring Room's
 * DAO Flow: refresh upserts the flow and the ViewModel's collector re-emits.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class CoverageBannerViewModelTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun coverage(has: Boolean, banner: String? = null) =
        MyCoverageResponseDto(coverage = MyCoverageDto(hasCoverage = has, bannerText = banner))

    /** Cold start with coverage cached: banner renders immediately. */
    @Test
    fun `cold start with coverage cached renders banner`() = runTest {
        val repo = FakeCoverageRepository().apply { setCachedCoverage(coverage(true, "Covering PM1")) }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertEquals("Covering PM1", vm.state.value?.text)
        assertTrue(vm.coverageState.value is CoverageState.HasCoverage)
    }

    /** Cold start with no coverage cached: banner hidden, state is NoCoverage (not Unknown). */
    @Test
    fun `cold start with no coverage cached hides banner`() = runTest {
        val repo = FakeCoverageRepository().apply { setCachedCoverage(coverage(false)) }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertNull("banner hidden", vm.state.value)
        assertEquals(
            "state is explicit NoCoverage, not Unknown",
            CoverageState.NoCoverage,
            vm.coverageState.value,
        )
    }

    /** Cold start with no cache: banner hidden, state is Unknown (awaiting first refresh). */
    @Test
    fun `cold start with no cache shows Unknown state`() = runTest {
        val repo = FakeCoverageRepository().apply {
            setCachedCoverage(null)
            makeRefreshFail() // no data to resolve Unknown into a real state
        }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertNull("banner hidden on Unknown", vm.state.value)
        assertEquals(
            "no cache + failed refresh stays Unknown",
            CoverageState.Unknown,
            vm.coverageState.value,
        )
    }

    /**
     * Failed refresh with coverage cached: keep showing coverage (stale but honest),
     * never blank to Unknown when cache exists.
     */
    @Test
    fun `failed refresh with coverage cached keeps showing coverage`() = runTest {
        val repo = FakeCoverageRepository().apply {
            setCachedCoverage(coverage(true, "Covering PM1"))
            makeRefreshFail()
        }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertEquals("keep showing cached coverage", "Covering PM1", vm.state.value?.text)
        assertTrue(vm.coverageState.value is CoverageState.HasCoverage)
    }

    /** Failed refresh with no-coverage cached: keep NoCoverage (distinct from Unknown/offline). */
    @Test
    fun `failed refresh with no coverage cached keeps NoCoverage state`() = runTest {
        val repo = FakeCoverageRepository().apply {
            setCachedCoverage(coverage(false))
            makeRefreshFail()
        }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertNull("banner stays hidden", vm.state.value)
        assertEquals(
            "keep cached no-coverage state, not Unknown",
            CoverageState.NoCoverage,
            vm.coverageState.value,
        )
    }

    /** Successful refresh moves state from NoCoverage to HasCoverage (Room re-emit). */
    @Test
    fun `successful refresh updates from no coverage to has coverage`() = runTest {
        val repo = FakeCoverageRepository().apply {
            setCachedCoverage(coverage(false))
            setRefreshData(coverage(true, "Now covering PM1"))
        }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertEquals("Now covering PM1", vm.state.value?.text)
        assertTrue(vm.coverageState.value is CoverageState.HasCoverage)
    }

    /**
     * Process death + re-entry: a fresh ViewModel over a warm cache restores the banner
     * immediately (the core MOB-007 fix — offline-first via Room, no re-fetch needed).
     */
    @Test
    fun `process death and re-entry restores coverage from cache`() = runTest {
        val repo = FakeCoverageRepository().apply {
            setCachedCoverage(coverage(true, "Covering PM1"))
            makeRefreshFail() // simulate still-offline on re-entry
        }

        val vm = CoverageBannerViewModel(repo)
        advanceUntilIdle()

        assertEquals(
            "coverage restored from Room cache after process death",
            "Covering PM1",
            vm.state.value?.text,
        )
        assertTrue(vm.coverageState.value is CoverageState.HasCoverage)
    }
}

/**
 * Fake repository backing coverage with a MutableStateFlow, mirroring Room's DAO Flow: a
 * successful refresh upserts the flow and the ViewModel's collector re-emits.
 */
private class FakeCoverageRepository : RosterRepository {
    private val timetableFlow = MutableStateFlow<EnrichedPositionListResponseDto?>(null)
    private val coverageFlow = MutableStateFlow<MyCoverageResponseDto?>(null)
    private var shouldRefreshFail = false
    private var refreshData: MyCoverageResponseDto? = null

    fun setCachedCoverage(data: MyCoverageResponseDto?) { coverageFlow.value = data }
    fun makeRefreshFail() { shouldRefreshFail = true }
    fun setRefreshData(data: MyCoverageResponseDto) { refreshData = data }

    override fun observeTimetable(centerId: String): Flow<EnrichedPositionListResponseDto?> = timetableFlow

    override fun observeCoverage(): Flow<MyCoverageResponseDto?> = coverageFlow

    override suspend fun refreshTimetable(centerId: String, limit: Int?): Boolean = true // unused in coverage tests

    override suspend fun refreshCoverage(): Boolean {
        if (shouldRefreshFail) return false // simulate a network failure: cache is kept
        refreshData?.let { coverageFlow.value = it }
        return true
    }
}
