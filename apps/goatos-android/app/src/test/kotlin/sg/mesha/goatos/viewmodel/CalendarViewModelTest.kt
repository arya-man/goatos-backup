package sg.mesha.goatos.viewmodel

import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.CalendarRepository
import sg.mesha.goatos.core.data.CalendarScheduleQuery
import sg.mesha.goatos.core.network.dto.CalendarDateMarkerDto
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.DriveSummaryDto
import sg.mesha.goatos.feature.calendar.CalendarTone
import kotlinx.serialization.json.JsonPrimitive
import java.time.LocalDate

@OptIn(ExperimentalCoroutinesApi::class)
class CalendarViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `calendar windows stay bounded to current week and month`() {
        val today = LocalDate.of(2026, 7, 12)

        assertEquals(CalendarDateRange("2026-07-11", "2026-07-17"), calendarWeekRange(today))
        assertEquals(CalendarDateRange("2026-07-01", "2026-07-31"), calendarMonthRange(today))
    }

    @Test
    fun `month days render open and completed markers distinctly`() {
        val monthDays = buildMonthDays(
            markers = listOf(
                CalendarDateMarkerDto(date = "2026-07-02", openCount = 2, eventCount = 2),
                CalendarDateMarkerDto(date = "2026-07-05", completedCount = 3, eventCount = 3),
            ),
            today = LocalDate.of(2026, 7, 12),
        )

        val secondDay = monthDays.first { it.dateKey == "2026-07-02" }
        val fifthDay = monthDays.first { it.dateKey == "2026-07-05" }

        assertEquals(true, secondDay.hasWork)
        assertEquals(CalendarTone.Ok, secondDay.dotTone)
        assertEquals(false, fifthDay.hasWork)
        assertEquals(true, fifthDay.hasCompletedHistory)
        assertEquals(CalendarTone.Muted, fifthDay.dotTone)
    }

    @Test
    fun `week strip uses readable short weekday labels not one letter chips`() {
        val weekDays = buildWeekDays(
            markers = listOf(CalendarDateMarkerDto(date = "2026-07-07", openCount = 2, eventCount = 2)),
            selectedDay = LocalDate.of(2026, 7, 7),
            today = LocalDate.of(2026, 7, 7),
        )

        // Rolling 8-day strip anchored on today (2026-07-07): today-1 (07-06, Mon) .. today+6 (07-13, Mon).
        assertEquals(listOf("Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"), weekDays.map { it.dayName })
        assertEquals(listOf("2026-07-06", "2026-07-12"), listOf(weekDays.first().dateKey, weekDays.last().dateKey))
        assertEquals(true, weekDays.first { it.dateKey == "2026-07-07" }.hasWork)
        assertEquals(true, weekDays.first { it.dateKey == "2026-07-07" }.isToday)
        assertEquals(true, weekDays.first { it.dateKey == "2026-07-07" }.isSelected)
    }

    @Test
    fun `workflow calendar rows open shed scan target on mobile`() {
        val target = CalendarEventDto(
            eventId = "calendar:task",
            shedId = "shed-1",
            links = mapOf("workflow" to JsonPrimitive("/vaccination/workflows/calendar:task")),
        ).routeTarget()

        assertEquals("scan/shed-1?task_id=task", target)
    }

    @Test
    fun `completed history rows stay on shed record target`() {
        val target = CalendarEventDto(
            eventId = "calendar:history",
            shedId = "shed-1",
            status = COMPLETED_STATUS,
            links = mapOf("vaccination" to JsonPrimitive("/vaccination/execution/sheds/shed-1")),
        ).routeTarget()

        assertEquals("record/shed-1", target)
    }

    @Test
    fun `drive summary formats ISO due date to localized display label`() {
        val item = CalendarEventDto(
            eventId = "calendar:drive",
            aggregated = true,
            driveSummary = DriveSummaryDto(
                parkName = "CBE",
                dueDate = "2026-07-07",
                shedCount = 4,
                shedsCompleted = 1,
                vaccineLabels = listOf("FMD", "HS"),
                totalCount = 77,
                completedCount = 20,
                remainingCount = 57,
                dueCount = 40,
                overdueCount = 12,
                deferredCount = 3,
                ownerLabel = "Arun Kumar",
            ),
        ).toCalendarItem()

        val summary = requireNotNull(item.driveSummary)
        assertEquals("CBE", summary.parkName)
        assertEquals("Tue 7 Jul", summary.dueDateLabel)
        assertEquals(4, summary.shedCount)
        assertEquals(1, summary.shedsCompleted)
        assertEquals(listOf("FMD", "HS"), summary.vaccineLabels)
        assertEquals(77, summary.totalCount)
        assertEquals(20, summary.completedCount)
        assertEquals(57, summary.remainingCount)
        assertEquals(40, summary.dueCount)
        assertEquals(12, summary.overdueCount)
        assertEquals(3, summary.deferredCount)
        assertEquals("Arun Kumar", summary.ownerLabel)
    }

    @Test
    fun `drive summary handles malformed due date gracefully`() {
        val item = CalendarEventDto(
            eventId = "calendar:drive",
            aggregated = true,
            driveSummary = DriveSummaryDto(
                parkName = "CBE",
                dueDate = "invalid-date",
                shedCount = 4,
                shedsCompleted = 1,
                vaccineLabels = listOf("FMD", "HS"),
                totalCount = 77,
                completedCount = 20,
                remainingCount = 57,
                dueCount = 40,
                overdueCount = 12,
                deferredCount = 3,
                ownerLabel = "Arun Kumar",
            ),
        ).toCalendarItem()

        val summary = requireNotNull(item.driveSummary)
        // Malformed input falls back to raw string without crashing
        assertEquals("invalid-date", summary.dueDateLabel)
    }

    @Test
    fun `aggregated event with no drive summary maps to a null summary`() {
        val item = CalendarEventDto(eventId = "calendar:legacy", aggregated = true).toCalendarItem()

        assertNull(item.driveSummary)
    }

    @Test
    fun `cold cache with failed refresh surfaces an explicit error instead of a blank screen`() = runTest(dispatcher) {
        val repo = FailingColdCalendarRepository()
        val vm = CalendarViewModel(repo = repo, analytics = NoopAnalytics(), crashReporter = NoopCrashReporter())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // R50-009: every segment resource is a cold cache (data == null) AND the background
        // refresh failed — buildCalendarState must surface an explicit error, never a silent
        // blank/empty screen that looks like "nothing is due".
        assertEquals(
            "Calendar could not load. Check your connection and try again.",
            vm.state.value.errorMessage,
        )
    }

    @Test
    fun `cold cache with a successful refresh never sets an error message`() = runTest(dispatcher) {
        val repo = FailingColdCalendarRepository(shouldFail = false)
        val vm = CalendarViewModel(repo = repo, analytics = NoopAnalytics(), crashReporter = NoopCrashReporter())
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        assertNull(vm.state.value.errorMessage)
    }
}

/** Minimal [CalendarRepository] test double for the R50-009 cold-cache regression: every
 *  observed resource stays a cold cache (`data = null`, as a fresh install / cleared Room table
 *  would be) and [refreshEvents] either always fails or always succeeds, per [shouldFail]. */
private class FailingColdCalendarRepository(
    private val shouldFail: Boolean = true,
) : CalendarRepository {
    override suspend fun events(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): CalendarEventListResponseDto = error("unused")

    override fun observeEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): Flow<Resource<CalendarEventListResponseDto>> = flowOf(Resource(data = null))

    override suspend fun refreshEvents(
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        includeFilterOptions: Boolean,
        cursor: String?,
        limit: Int?,
    ): Result<Unit> = if (shouldFail) {
        Result.failure(RuntimeException("network unreachable"))
    } else {
        Result.success(Unit)
    }

    override suspend fun appendEvents(
        cursor: String,
        parkId: String?,
        shedId: String?,
        ownerKey: String?,
        status: String?,
        dateFrom: String?,
        dateTo: String?,
        includeDateMarkers: Boolean,
        vaccine: String?,
        limit: Int?,
    ): Result<Unit> = error("unused")

    override fun schedule(query: CalendarScheduleQuery): Flow<PagingData<CalendarEventDto>> =
        flowOf(PagingData.empty())

    override fun observeScheduleMetadata(query: CalendarScheduleQuery): Flow<Resource<CalendarEventListResponseDto>> =
        flowOf(Resource(data = null))
}
