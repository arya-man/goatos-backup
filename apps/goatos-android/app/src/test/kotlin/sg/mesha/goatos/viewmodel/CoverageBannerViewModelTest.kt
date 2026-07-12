package sg.mesha.goatos.viewmodel

import app.cash.turbine.test
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.dto.MyCoverageDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * MOB-007: Coverage banner offline-first tests. Verify that:
 * 1. Has coverage: banner renders from cache across process death
 * 2. No coverage (cached): banner hidden, state distinct from "offline"
 * 3. Network error on cold start (no cache): banner hidden, state is Unknown
 * 4. Failed refresh with cache: keeps showing prior state (either coverage or no coverage)
 * 5. Coverage state is explicitly distinguished: HasCoverage / NoCoverage / Unknown
 */
class CoverageBannerViewModelTest {

    private lateinit var repo: FakeCoverageRepository
    private lateinit var viewModel: CoverageBannerViewModel

    @Before
    fun setup() {
        repo = FakeCoverageRepository()
    }

    /**
     * Cold start with coverage cached: banner renders immediately
     */
    @Test
    fun `cold start with coverage cached renders banner`() = runTest {
        // Arrange: cache has has_coverage=true with banner text
        val cached = MyCoverageResponseDto(
            coverage = MyCoverageDto(
                hasCoverage = true,
                bannerText = "Covering PM1",
            )
        )
        repo.setCachedCoverage(cached)

        // Act: create ViewModel
        viewModel = CoverageBannerViewModel(repo)

        // Assert: banner renders
        viewModel.state.test {
            val state = awaitItem()
            assert(state != null) { "Banner should render" }
            assert(state?.text == "Covering PM1")
        }

        // Also verify internal state
        viewModel.coverageState.test {
            val state = awaitItem()
            assert(state is CoverageState.HasCoverage)
        }
    }

    /**
     * Cold start with no coverage cached: banner hidden, but not Unknown
     */
    @Test
    fun `cold start with no coverage cached hides banner`() = runTest {
        // Arrange: cache has has_coverage=false
        val cached = MyCoverageResponseDto(
            coverage = MyCoverageDto(hasCoverage = false)
        )
        repo.setCachedCoverage(cached)

        // Act: create ViewModel
        viewModel = CoverageBannerViewModel(repo)

        // Assert: banner hidden, but state is NoCoverage (not Unknown)
        viewModel.state.test {
            val state = awaitItem()
            assert(state == null) { "Banner should be hidden" }
        }

        viewModel.coverageState.test {
            val state = awaitItem()
            assert(state == CoverageState.NoCoverage) { "State is explicit NoCoverage, not Unknown" }
        }
    }

    /**
     * Cold start with no cache: banner hidden, state is Unknown
     * (We don't know yet if there's coverage; refresh will tell us)
     */
    @Test
    fun `cold start with no cache shows Unknown state`() = runTest {
        // Arrange: cache is empty
        repo.setCachedCoverage(null)

        // Act: create ViewModel
        viewModel = CoverageBannerViewModel(repo)

        // Assert: banner hidden, state is Unknown
        viewModel.state.test {
            val state = awaitItem()
            assert(state == null) { "Banner hidden on Unknown" }
        }

        viewModel.coverageState.test {
            val state = awaitItem()
            assert(state == CoverageState.Unknown) { "State is Unknown, awaiting first refresh" }
        }
    }

    /**
     * Refresh fails on cold start (no cache): stay Unknown, keep banner hidden
     */
    @Test
    fun `failed refresh on cold start keeps Unknown state`() = runTest {
        // Arrange: cache is empty, refresh will fail
        repo.setCachedCoverage(null)
        repo.makeRefreshFail()

        // Act: create ViewModel (triggers background refresh)
        viewModel = CoverageBannerViewModel(repo)

        // Assert: still Unknown (no data to fall back to)
        viewModel.coverageState.test {
            val state = awaitItem()
            assert(state == CoverageState.Unknown)
        }

        viewModel.state.test {
            val state = awaitItem()
            assert(state == null) { "Banner stays hidden on Unknown" }
        }
    }

    /**
     * Refresh fails with coverage cached: keep showing coverage
     * (offline, but don't blank to Unknown if cache exists)
     */
    @Test
    fun `failed refresh with coverage cached keeps showing coverage`() = runTest {
        // Arrange: cache has coverage
        val cached = MyCoverageResponseDto(
            coverage = MyCoverageDto(
                hasCoverage = true,
                bannerText = "Covering PM1",
            )
        )
        repo.setCachedCoverage(cached)
        repo.makeRefreshFail()

        // Act: create ViewModel (render cached, then refresh fails in background)
        viewModel = CoverageBannerViewModel(repo)

        // Assert: coverage still renders (stale but honest)
        viewModel.state.test {
            val state = awaitItem()
            assert(state?.text == "Covering PM1") { "Keep showing cached coverage" }
        }

        viewModel.coverageState.test {
            val state = awaitItem()
            assert(state is CoverageState.HasCoverage) { "Keep cached state" }
        }
    }

    /**
     * Refresh fails with no coverage cached: keep showing no coverage
     */
    @Test
    fun `failed refresh with no coverage cached keeps NoCoverage state`() = runTest {
        // Arrange: cache has no coverage
        val cached = MyCoverageResponseDto(
            coverage = MyCoverageDto(hasCoverage = false)
        )
        repo.setCachedCoverage(cached)
        repo.makeRefreshFail()

        // Act: create ViewModel
        viewModel = CoverageBannerViewModel(repo)

        // Assert: banner stays hidden, state stays NoCoverage
        viewModel.state.test {
            val state = awaitItem()
            assert(state == null) { "Banner stays hidden" }
        }

        viewModel.coverageState.test {
            val state = awaitItem()
            assert(state == CoverageState.NoCoverage) { "Keep cached no-coverage state" }
        }
    }

    /**
     * Successful refresh changes state from NoCoverage to HasCoverage
     */
    @Test
    fun `successful refresh updates from no coverage to has coverage`() = runTest {
        // Arrange: cache has no coverage
        val oldCached = MyCoverageResponseDto(
            coverage = MyCoverageDto(hasCoverage = false)
        )
        repo.setCachedCoverage(oldCached)

        // New data from refresh
        val newData = MyCoverageResponseDto(
            coverage = MyCoverageDto(
                hasCoverage = true,
                bannerText = "Now covering PM1",
            )
        )
        repo.setRefreshData(newData)

        // Act: create ViewModel
        viewModel = CoverageBannerViewModel(repo)

        // Trigger manual refresh
        viewModel.load()

        // Assert: coverage now renders
        viewModel.state.test {
            val state = awaitItem()  // initial: no coverage
            // After refresh, cache updated with new data
            val updatedState = awaitItem()
            assert(updatedState?.text == "Now covering PM1")
        }

        viewModel.coverageState.test {
            val state = awaitItem()  // initial: NoCoverage
            val updatedState = awaitItem()  // after refresh
            assert(updatedState is CoverageState.HasCoverage)
        }
    }

    /**
     * Process death + re-entry: cache survives, coverage renders without re-fetching
     * (This is the core MOB-007 fix: offline-first via Room)
     */
    @Test
    fun `process death and re-entry restores coverage from cache`() = runTest {
        // Arrange: simulate app had coverage cached
        val cached = MyCoverageResponseDto(
            coverage = MyCoverageDto(
                hasCoverage = true,
                bannerText = "Covering PM1",
            )
        )
        repo.setCachedCoverage(cached)

        // Act: Create ViewModel (simulating re-entry after process death)
        viewModel = CoverageBannerViewModel(repo)

        // Assert: coverage renders immediately from Room cache
        // (not a blank/loading state even though it's a fresh ViewModel)
        viewModel.state.test {
            val state = awaitItem()
            assert(state?.text == "Covering PM1") { "Coverage restored from Room cache after process death" }
        }
    }
}

private class FakeCoverageRepository : RosterRepository {
    private var cachedCoverage: MyCoverageResponseDto? = null
    private var shouldRefreshFail: Boolean = false
    private var refreshData: MyCoverageResponseDto? = null

    fun setCachedCoverage(data: MyCoverageResponseDto?) {
        cachedCoverage = data
    }

    fun makeRefreshFail() {
        shouldRefreshFail = true
    }

    fun setRefreshData(data: MyCoverageResponseDto) {
        refreshData = data
    }

    override fun observeTimetable(centerId: String) = flowOf(null)

    override fun observeCoverage() = flowOf(cachedCoverage)

    override suspend fun refreshTimetable(centerId: String, limit: Int?) {
        // no-op for this test
    }

    override suspend fun refreshCoverage() {
        if (shouldRefreshFail) return  // simulate failure
        refreshData?.let { cachedCoverage = it }
    }
}
