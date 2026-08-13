package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.DriveSummaryDto
import sg.mesha.goatos.core.network.dto.currentScheduleDate

class CalendarScheduleDateTest {

    @Test
    fun `calendar event item uses backend due time as schedule date`() {
        val item = CalendarEventDto(
            eventId = "event-1",
            title = "Vaccination",
            dueAt = "2026-08-01T08:00:00+05:30",
        ).toCalendarItem()

        assertEquals("2026-08-05", CalendarEventDto(
            dueAt = "2026-08-05",
        ).currentScheduleDate)
        assertEquals("2026-08-01", item.dateKey)
        assertEquals("08:00", item.timeLabel)
    }

    @Test
    fun `drive summary label uses backend due date`() {
        val summary = DriveSummaryDto(
            dueDate = "2026-08-29",
        ).toCalendarDriveSummary()

        assertEquals("Sat 29 Aug", summary.dueDateLabel)
    }
}
