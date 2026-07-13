package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.CalendarDateMarkerDto
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.DriveSummaryDto
import sg.mesha.goatos.feature.calendar.CalendarTone
import kotlinx.serialization.json.JsonPrimitive
import java.time.LocalDate

class CalendarViewModelTest {

    @Test
    fun `calendar windows stay bounded to current week month and forty five history days`() {
        val today = LocalDate.of(2026, 7, 12)

        assertEquals(CalendarDateRange("2026-07-06", "2026-07-12"), calendarWeekRange(today))
        assertEquals(CalendarDateRange("2026-07-01", "2026-07-31"), calendarMonthRange(today))
        assertEquals(CalendarDateRange("2026-05-29", "2026-07-12"), calendarHistoryRange(today))
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
    fun `workflow calendar rows open shed scan target on mobile`() {
        val target = CalendarEventDto(
            eventId = "calendar:task",
            shedId = "shed-1",
            links = mapOf("workflow" to JsonPrimitive("/vaccination/workflows/calendar:task")),
        ).routeTarget()

        assertEquals("scan/shed-1", target)
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
}
