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
    fun `pc director starts on stock tab under vaccination`() {
        val vaccinationBar = listOf(
            NavItem("vaccination_stock", "Stock", Routes.VACCINATION_STOCK),
            NavItem("vaccination", "Drives", Routes.VACCINATION),
            NavItem("you", "You", Routes.YOU),
        )
        val state = NavState(
            chrome = NavChrome.EXPANDED,
            items = vaccinationBar,
            modules = listOf(
                NavModule(
                    key = "vaccination",
                    label = "Vaccination",
                    href = Routes.VACCINATION,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = vaccinationBar,
                ),
                NavModule(
                    key = "pc_care",
                    label = "Preventive Care",
                    href = Routes.PC_DEWORMING,
                    status = NavModuleStatus.AVAILABLE,
                    navItems = listOf(NavItem("pc_deworming", "Deworming", Routes.PC_DEWORMING)),
                ),
            ),
        )
        assertEquals(Routes.VACCINATION_STOCK, startDestinationFor(state))
    }

    @Test
    fun `counts-only operator cold starts on its counts landing, not calendar`() {
        val countsBar = listOf(
            NavItem("birth", "Birth", Routes.COUNTS_BIRTH),
            NavItem("death", "Death", Routes.COUNTS_DEATH),
            NavItem("shifting", "Shifting", Routes.COUNTS_SHIFTING),
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

    private fun navState(vararg items: NavItem) =
        NavState(chrome = NavChrome.MINIMAL, items = items.toList())
}
