package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.DriveSummaryDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate

class CalendarScheduleDateTest {

    @Test
    fun `calendar event item uses effective schedule date before original due time`() {
        val item = CalendarEventDto(
            eventId = "event-1",
            title = "Vaccination",
            dueAt = "2026-08-01T08:00:00+05:30",
            effectiveScheduleDate = "2026-08-05",
        ).toCalendarItem()

        assertEquals("2026-08-05", CalendarEventDto(
            dueAt = "2026-08-01T08:00:00+05:30",
            effectiveScheduleDate = "2026-08-05",
        ).currentScheduleDate)
        assertEquals("2026-08-05", item.dateKey)
        assertEquals("", item.timeLabel)
    }

    @Test
    fun `drive summary label uses current assignment date before legacy due date`() {
        val summary = DriveSummaryDto(
            currentAssignmentDate = "2026-09-02",
            dueDate = "2026-08-29",
        ).toCalendarDriveSummary()

        assertEquals("Wed 2 Sep", summary.dueDateLabel)
    }
}
