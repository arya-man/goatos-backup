package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.network.dto.CalendarEventDto
import sg.mesha.goatos.core.network.dto.CalendarEventListResponseDto
import sg.mesha.goatos.core.network.dto.CalendarPresentationDto
import java.time.LocalDate

class CalendarViewModelTest {

    @Test
    fun `calendar windows stay bounded to current month and forty five history days`() {
        val today = LocalDate.of(2026, 7, 12)

        assertEquals(CalendarDateRange("2026-07-01", "2026-07-31"), calendarMonthRange(today))
        assertEquals(CalendarDateRange("2026-05-29", "2026-07-12"), calendarHistoryRange(today))
    }

    @Test
    fun `open work and explicit completed history merge without duplicate event ids`() {
        val open = CalendarEventDto(
            eventId = "drive:future",
            status = "due",
            dueAt = "2026-07-17T03:30:00Z",
        )
        val completed = CalendarEventDto(
            eventId = "obligation:history",
            status = COMPLETED_STATUS,
            dueAt = "2026-07-01T03:30:00Z",
        )
        val presentation = CalendarPresentationDto(pageTitle = "Calendar")

        val merged = mergeCalendarResources(
            Resource(
                data = CalendarEventListResponseDto(presentation = presentation, items = listOf(open)),
                lastSyncedAt = 100,
            ),
            Resource(
                data = CalendarEventListResponseDto(items = listOf(completed, open)),
                lastSyncedAt = 200,
            ),
        )

        assertNotNull(merged.data)
        assertEquals(listOf("obligation:history", "drive:future"), merged.data?.items?.map { it.eventId })
        assertEquals("Calendar", merged.data?.presentation?.pageTitle)
        assertEquals(200L, merged.lastSyncedAt)
    }
}
