package sg.mesha.goatos.viewmodel

import app.cash.turbine.test
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.network.dto.EnrichedPositionDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.feature.timetable.TimetableEvent

/**
 * MOB-007: Timetable offline-first tests. Verify that:
 * 1. Timetable data persists in Room cache across process death
 * 2. Failed refresh keeps cached data on screen with offline indicator
 * 3. A cold start with empty cache shows honest empty state, not error
 * 4. Network errors are distinct from "no center" errors
 */
class TimetableViewModelTest {

    private lateinit var repo: FakeTimetableRepository
    private lateinit var bootstrap: FakeBootstrapRepository
    private lateinit var viewModel: TimetableViewModel

    @Before
    fun setup() {
        repo = FakeTimetableRepository()
        bootstrap = FakeBootstrapRepository()
    }

    /**
     * Cold start with cache: screen renders cached data immediately
     * (stale-while-revalidate pattern)
     */
    @Test
    fun `cold start with cached data renders immediately`() = runTest {
        // Arrange: bootstrap returns center, cache has data
        bootstrap.centerId = "center1"
        val cachedData = EnrichedPositionListResponseDto(
            items = listOf(
                EnrichedPositionDto(positionId = "p1", positionCode = "P1"),
            )
        )
        repo.setCachedTimetable("center1", cachedData)

        // Act: create ViewModel
        viewModel = TimetableViewModel(repo, bootstrap)

        // Assert: cached data renders immediately, not blank
        viewModel.state.test {
            val state = awaitItem()
            assert(state.rows.isNotEmpty()) { "Should have cached rows on cold start" }
            assert(state.rows.first().positionLabel == "P1")
            assert(state.errorCode == null)
            assert(state.isOffline == false) { "Cache is not offline; it's just cached" }
        }
    }

    /**
     * Cold start with no cache (first load): honest empty state, not error
     */
    @Test
    fun `cold start with no cache shows empty state`() = runTest {
        // Arrange: bootstrap returns center, cache is empty
        bootstrap.centerId = "center1"
        repo.setCachedTimetable("center1", null)

        // Act: create ViewModel
        viewModel = TimetableViewModel(repo, bootstrap)

        // Assert: empty state, no error code
        viewModel.state.test {
            val state = awaitItem()
            assert(state.rows.isEmpty()) { "Cache miss should show empty" }
            assert(state.errorCode == null) { "Not an error; just empty" }
        }
    }

    /**
     * No center assigned: explicit "no_center" error (not network error)
     */
    @Test
    fun `operator with no center assigned shows no_center error`() = runTest {
        // Arrange: bootstrap returns null center
        bootstrap.centerId = null

        // Act: create ViewModel
        viewModel = TimetableViewModel(repo, bootstrap)

        // Assert: explicit no_center error, not network error
        viewModel.state.test {
            val state = awaitItem()
            assert(state.errorCode == "no_center")
            assert(state.rows.isEmpty())
        }
    }

    /**
     * Refresh fails with cached data: keep data on screen, set isOffline=true
     */
    @Test
    fun `failed refresh keeps cached data and sets offline flag`() = runTest {
        // Arrange: cold start with cached data
        bootstrap.centerId = "center1"
        val cachedData = EnrichedPositionListResponseDto(
            items = listOf(
                EnrichedPositionDto(positionId = "p1", positionCode = "P1"),
            )
        )
        repo.setCachedTimetable("center1", cachedData)

        // Act: create ViewModel (render cached data)
        viewModel = TimetableViewModel(repo, bootstrap)

        // Simulate refresh failure (network error)
        repo.makeRefreshFail("center1")

        // Trigger refresh
        viewModel.onEvent(TimetableEvent.Refresh)

        // Assert: cached data remains, isOffline=true
        viewModel.state.test {
            val state = awaitItem()  // initial cached state
            assert(state.rows.isNotEmpty()) { "Cached rows still visible" }
            assert(state.rows.first().positionLabel == "P1")
            assert(state.isOffline == true) { "Offline flag set after failed refresh" }
            assert(state.errorCode == null) { "Not an error; data exists" }
        }
    }

    /**
     * Refresh fails with NO cached data: show load_failed error
     */
    @Test
    fun `failed refresh with no cache shows load_failed error`() = runTest {
        // Arrange: bootstrap returns center, cache is empty
        bootstrap.centerId = "center1"
        repo.setCachedTimetable("center1", null)

        // Act: create ViewModel
        viewModel = TimetableViewModel(repo, bootstrap)

        // Make refresh fail
        repo.makeRefreshFail("center1")

        // Trigger refresh
        viewModel.onEvent(TimetableEvent.Refresh)

        // Assert: load_failed error (not offline flag)
        viewModel.state.test {
            val state = awaitItem()  // empty state
            val nextState = awaitItem()  // after failed refresh
            assert(nextState.errorCode == "load_failed") { "No cache and refresh failed" }
            assert(nextState.rows.isEmpty())
        }
    }

    /**
     * Successful refresh updates cached data and clears offline flag
     */
    @Test
    fun `successful refresh updates cache and clears offline flag`() = runTest {
        // Arrange: bootstrap returns center, cache has OLD data
        bootstrap.centerId = "center1"
        val oldData = EnrichedPositionListResponseDto(
            items = listOf(
                EnrichedPositionDto(positionId = "p1", positionCode = "OLD"),
            )
        )
        repo.setCachedTimetable("center1", oldData)

        // Act: create ViewModel
        viewModel = TimetableViewModel(repo, bootstrap)

        // Provide new data for refresh
        val newData = EnrichedPositionListResponseDto(
            items = listOf(
                EnrichedPositionDto(positionId = "p1", positionCode = "NEW"),
            )
        )
        repo.setRefreshData("center1", newData)

        // Trigger refresh
        viewModel.onEvent(TimetableEvent.Refresh)

        // Assert: new data renders, offline flag cleared
        viewModel.state.test {
            val state = awaitItem()  // old cached data
            // After refresh, new data should flow through Room
            val updatedState = awaitItem()
            assert(updatedState.rows.first().positionLabel == "NEW")
            assert(updatedState.isOffline == false)
            assert(updatedState.errorCode == null)
        }
    }
}

/**
 * Fake repository for testing: mimics Room cache behavior
 */
private class FakeTimetableRepository : RosterRepository {
    private var cachedTimetable: EnrichedPositionListResponseDto? = null
    private var cachedCoverage: sg.mesha.goatos.core.network.dto.MyCoverageResponseDto? = null
    private var shouldRefreshFail: Boolean = false
    private var refreshData: EnrichedPositionListResponseDto? = null

    fun setCachedTimetable(centerId: String, data: EnrichedPositionListResponseDto?) {
        cachedTimetable = data
    }

    fun makeRefreshFail(centerId: String) {
        shouldRefreshFail = true
    }

    fun setRefreshData(centerId: String, data: EnrichedPositionListResponseDto) {
        refreshData = data
    }

    override fun observeTimetable(centerId: String) = flowOf(cachedTimetable)

    override fun observeCoverage() = flowOf(cachedCoverage)

    override suspend fun refreshTimetable(centerId: String, limit: Int?) {
        if (shouldRefreshFail) return  // simulate failure
        refreshData?.let { cachedTimetable = it }
    }

    override suspend fun refreshCoverage() {
        // no-op for this test
    }
}

private class FakeBootstrapRepository : BootstrapRepository {
    var centerId: String? = null

    override suspend fun operatorProfile() = sg.mesha.goatos.core.data.BootstrapProfile(
        userId = "u1",
        primaryLocationId = centerId,
        roles = emptyList(),
        department = null,
    )

    override suspend fun cache() = null
}
