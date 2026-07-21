package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState

class AppStartDestinationTest {
    @Test
    fun `first backend visible root is landing even when calendar is also exposed`() {
        assertEquals(
            Routes.VACCINATION,
            startDestinationFor(
                navState(
                    NavItem("vaccination", "Drives", Routes.VACCINATION),
                    NavItem("calendar", "Calendar", Routes.CALENDAR),
                ),
            ),
        )
    }

    @Test
    fun `vaccination operator starts on shed queue when backend does not expose calendar`() {
        assertEquals(
            Routes.VACCINATION,
            startDestinationFor(
                navState(
                    NavItem("vaccination", "Drives", Routes.VACCINATION),
                    NavItem("alerts", "Alerts", Routes.ALERTS),
                    NavItem("you", "You", Routes.YOU),
                ),
            ),
        )
    }

    @Test
    fun `verifier starts on standalone verification root`() {
        assertEquals(
            Routes.VERIFY,
            startDestinationFor(navState(NavItem("verify", "Verify", Routes.VERIFY))),
        )
    }

    @Test
    fun `unknown or empty bootstrap root fails safely`() {
        assertEquals(Routes.CALENDAR, startDestinationFor(navState(NavItem("future", "Future", "/future"))))
        assertEquals(Routes.CALENDAR, startDestinationFor(NavState.Empty))
    }

    private fun navState(vararg items: NavItem) =
        NavState(chrome = NavChrome.MINIMAL, items = items.toList())
}
