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
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.RosterRepository
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionDto
import sg.mesha.goatos.core.network.dto.EnrichedPositionListResponseDto
import sg.mesha.goatos.core.network.dto.MyCoverageResponseDto

/**
 * MOB-007: Timetable offline-first tests. Verify that:
 * 1. Timetable data persists in Room cache across process death (cached-first render)
 * 2. Failed refresh keeps cached data on screen with offline indicator
 * 3. A cold start with empty cache shows honest empty state, not error
 * 4. Network errors are distinct from "no center" errors
 *
 * The [FakeTimetableRepository] backs the timetable with a MutableStateFlow, mirroring
 * Room's DAO Flow: refresh upserts the flow and the ViewModel's collector re-emits.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class TimetableViewModelTest {

    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun position(code: String) = EnrichedPositionDto(positionId = "p1", positionCode = code)

    private fun listOf1(code: String) =
        EnrichedPositionListResponseDto(items = listOf(position(code)))

    /** Cold start with cache: screen renders cached data immediately (stale-while-revalidate). */
    @Test
    fun `cold start with cached data renders immediately`() = runTest {
        val repo = FakeTimetableRepository().apply { setCachedTimetable(listOf1("P1")) }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertTrue("cached rows on cold start", state.rows.isNotEmpty())
        assertEquals("P1", state.rows.first().positionLabel)
        assertNull(state.errorCode)
        assertFalse("cache is not offline; it is just cached", state.isOffline)
    }

    /** Cold start with no cache (first load): honest empty state, not an error. */
    @Test
    fun `cold start with no cache shows empty state`() = runTest {
        val repo = FakeTimetableRepository().apply { setCachedTimetable(null) }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertTrue("cache miss shows empty", state.rows.isEmpty())
        // The default fake refresh is a no-op success, so the empty cache stays honest.
        assertNull("not an error; just empty", state.errorCode)
    }

    /** No center assigned: explicit "no_center" error (not a network error). */
    @Test
    fun `operator with no center assigned shows no_center error`() = runTest {
        val repo = FakeTimetableRepository()
        val bootstrap = FakeBootstrapRepository(centerId = null)

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("no_center", state.errorCode)
        assertTrue(state.rows.isEmpty())
    }

    /** Refresh fails with cached data: keep data on screen, set isOffline=true. */
    @Test
    fun `failed refresh keeps cached data and sets offline flag`() = runTest {
        val repo = FakeTimetableRepository().apply {
            setCachedTimetable(listOf1("P1"))
            makeRefreshFail()
        }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertTrue("cached rows still visible", state.rows.isNotEmpty())
        assertEquals("P1", state.rows.first().positionLabel)
        assertTrue("offline flag set after failed refresh", state.isOffline)
        assertNull("not an error; data exists", state.errorCode)
    }

    /** Refresh fails with NO cached data: show load_failed error. */
    @Test
    fun `failed refresh with no cache shows load_failed error`() = runTest {
        val repo = FakeTimetableRepository().apply {
            setCachedTimetable(null)
            makeRefreshFail()
        }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("no cache and refresh failed", "load_failed", state.errorCode)
        assertTrue(state.rows.isEmpty())
    }

    /** Successful refresh updates cached data and clears offline flag (Room re-emit). */
    @Test
    fun `successful refresh updates cache and clears offline flag`() = runTest {
        val repo = FakeTimetableRepository().apply {
            setCachedTimetable(listOf1("OLD"))
            setRefreshData(listOf1("NEW"))
        }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("refreshed data re-emitted via Room flow", "NEW", state.rows.first().positionLabel)
        assertFalse(state.isOffline)
        assertNull(state.errorCode)
    }

    /**
     * Process death + re-entry: a fresh ViewModel over a warm cache renders the roster
     * immediately (the core MOB-007 fix — offline-first via Room, no blank wall).
     */
    @Test
    fun `process death and re-entry restores timetable from cache`() = runTest {
        val repo = FakeTimetableRepository().apply {
            setCachedTimetable(listOf1("P1"))
            makeRefreshFail() // simulate still-offline on re-entry
        }
        val bootstrap = FakeBootstrapRepository(centerId = "center1")

        val vm = TimetableViewModel(repo, bootstrap)
        advanceUntilIdle()

        val state = vm.state.value
        assertEquals("timetable restored from Room cache after process death", "P1", state.rows.first().positionLabel)
        assertTrue("offline surfaced distinctly from a load error", state.isOffline)
        assertNull(state.errorCode)
    }
}

/**
 * Fake repository backing timetable/coverage with MutableStateFlows, mirroring Room's DAO
 * Flow: a successful refresh upserts the flow and the ViewModel's collector re-emits.
 */
private class FakeTimetableRepository : RosterRepository {
    private val timetableFlow = MutableStateFlow<EnrichedPositionListResponseDto?>(null)
    private val coverageFlow = MutableStateFlow<MyCoverageResponseDto?>(null)
    private var shouldRefreshFail = false
    private var refreshData: EnrichedPositionListResponseDto? = null

    fun setCachedTimetable(data: EnrichedPositionListResponseDto?) { timetableFlow.value = data }
    fun makeRefreshFail() { shouldRefreshFail = true }
    fun setRefreshData(data: EnrichedPositionListResponseDto) { refreshData = data }

    override fun observeTimetable(centerId: String): Flow<EnrichedPositionListResponseDto?> = timetableFlow

    override fun observeCoverage(): Flow<MyCoverageResponseDto?> = coverageFlow

    override suspend fun refreshTimetable(centerId: String, limit: Int?): Boolean {
        if (shouldRefreshFail) return false // simulate a network failure: cache is kept
        refreshData?.let { timetableFlow.value = it }
        return true
    }

    override suspend fun refreshCoverage(): Boolean = true // unused in timetable tests
}

private class FakeBootstrapRepository(private val centerId: String?) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = NavState(NavChrome.MINIMAL, emptyList())
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
        BootstrapOperatorProfileDto(displayName = "Test User", primaryLocationId = centerId)
}
