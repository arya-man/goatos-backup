package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
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
                    NavItem("alerts", "Alerts", Routes.VACCINATION_ALERTS),
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

    @Test
    fun `leadership cold start lands in shared vaccination module`() {
        val vaccinationBar = listOf(
            NavItem("vaccination", "Vaccination", Routes.VACCINATION),
            NavItem("alerts", "Alerts", Routes.VACCINATION_ALERTS),
            NavItem("you", "You", Routes.YOU),
        )
        val state = NavState(
            chrome = NavChrome.MINIMAL,
            items = vaccinationBar,
            modules = listOf(
                NavModule(
                    key = "vaccination",
                    label = "Vaccination",
                    href = Routes.VACCINATION,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = vaccinationBar,
                ),
            ),
        )
        assertEquals(Routes.VACCINATION, startDestinationFor(state))
    }

    @Test
    fun `counts-only operator cold starts on its counts landing, not calendar`() {
        val countsBar = listOf(
            NavItem("birth", "Birth", Routes.COUNTS_BIRTH),
            NavItem("death", "Death", Routes.COUNTS_DEATH),
            NavItem("shifting", "Shifting", Routes.COUNTS_SHIFTING),
            NavItem("reconcile", "Reconcile", Routes.COUNTS_RECONCILE),
        )
        // The backend lands a capture operator on /counts/birth (the census /counts is
        // gated away). Calendar is NOT exposed to them, so the fallback must not fire.
        val state = NavState(
            chrome = NavChrome.MINIMAL,
            items = countsBar,
            modules = listOf(
                NavModule(
                    key = "counts",
                    label = "Counts",
                    href = Routes.COUNTS_BIRTH,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = countsBar,
                ),
            ),
        )
        assertEquals(Routes.COUNTS_BIRTH, startDestinationFor(state))
    }

    @Test
    fun `clock-only principal cold starts on My Clock, not a bar-less calendar`() {
        // E2E finding 2026-08-28: once the reminder banner cleared, a clock-only
        // person landing on Calendar had NO route into the module at all.
        val clockBar = listOf(NavItem("clock", "My Clock", Routes.CLOCK))
        val state = NavState(
            chrome = NavChrome.MINIMAL,
            items = clockBar,
            modules = listOf(
                NavModule(
                    key = "clock",
                    label = "Clock In / Out",
                    href = Routes.CLOCK,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = clockBar,
                ),
            ),
        )
        assertEquals(Routes.CLOCK, startDestinationFor(state))
    }

    @Test
    fun `drawer-top clock module never steals the landing from the active work bar`() {
        // Clock sits FIRST in the drawer (maintainer ask 2026-08-28), but the served
        // visible_navigation is the WORK module's bar and the day opens there.
        val vaccinationBar = listOf(
            NavItem("vaccination", "Drives", Routes.VACCINATION),
            NavItem("alerts", "Alerts", Routes.VACCINATION_ALERTS),
        )
        val state = NavState(
            chrome = NavChrome.EXPANDED,
            items = vaccinationBar,
            modules = listOf(
                NavModule(
                    key = "clock",
                    label = "Clock In / Out",
                    href = Routes.CLOCK,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = listOf(NavItem("clock", "My Clock", Routes.CLOCK)),
                ),
                NavModule(
                    key = "vaccination",
                    label = "Vaccination",
                    href = Routes.VACCINATION,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = vaccinationBar,
                ),
            ),
        )
        assertEquals(Routes.VACCINATION, startDestinationFor(state))
    }

    private fun navState(vararg items: NavItem) =
        NavState(chrome = NavChrome.MINIMAL, items = items.toList())
}
