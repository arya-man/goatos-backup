package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState

class AppStartDestinationTest {
    @Test
    fun `calendar is landing when backend exposes it`() {
        assertEquals(
            Routes.CALENDAR,
            startDestinationFor(
                navState(
                    NavItem("vaccination", "Drives", Routes.VACCINATION),
                    NavItem("calendar", "Calendar", Routes.CALENDAR),
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
