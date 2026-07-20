package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TopLevelChromeTest {
    private val roots = listOf(
        Routes.LEADERSHIP,
        Routes.CALENDAR,
        Routes.VACCINATION,
        Routes.ALERTS,
        Routes.YOU,
    )

    @Test
    fun `exact root destinations show global navigation chrome`() {
        roots.forEach { route ->
            assertTrue(route, isTopLevelRoute(route, roots))
        }
    }

    @Test
    fun `calendar drill destinations never inherit root chrome`() {
        listOf(
            Routes.CALENDAR_DRIVE,
            Routes.SCAN,
            Routes.SUBMIT,
            Routes.RECORD,
        ).forEach { route ->
            assertFalse(route, isTopLevelRoute(route, roots))
        }
    }

    @Test
    fun `root path prefixes do not make a child top level`() {
        assertFalse(isTopLevelRoute("${Routes.VACCINATION}/drive", roots))
        assertFalse(isTopLevelRoute("${Routes.CALENDAR}/day", roots))
    }

    @Test
    fun `calendar drill fallback always opens a hosted child`() {
        assertEquals(Routes.CALENDAR_DRIVE, calendarTargetRoute(null))
        assertEquals(Routes.CALENDAR_DRIVE, calendarTargetRoute(""))
        assertEquals(
            Routes.CALENDAR_DRIVE,
            calendarTargetRoute("/vaccination/execution"),
        )
        assertEquals(
            Routes.CALENDAR_DRIVE,
            calendarTargetRoute("/vaccination/scan/shed-1"),
        )
        assertEquals(
            Routes.CALENDAR_DRIVE,
            calendarTargetRoute("/vaccination/sheds/shed-1"),
        )
        assertFalse(isTopLevelRoute(calendarTargetRoute(null), roots))
    }
}
