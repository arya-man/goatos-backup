package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState

class LeadershipWeighingPolicyTest {
    @Test
    fun `ceo and pc director use leadership weighing`() {
        assertTrue(isWeighingLeadershipRole("ceo_internal"))
        assertTrue(isWeighingLeadershipRole("PC Director"))
        assertTrue(isWeighingLeadershipRole("Director"))
        assertTrue(isWeighingLeadershipRole("CXO"))
    }

    @Test
    fun `operator does not use leadership weighing`() {
        assertFalse(isWeighingLeadershipRole("operator"))
        assertFalse(isWeighingLeadershipRole("verifier"))
    }

    @Test
    fun `leadership weighing has exact read only tab order`() {
        val weighing = NavModule(
            key = "weighing",
            label = "Weighing",
            href = Routes.WEIGHING,
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(
                NavItem("weighing", "Weighing", Routes.WEIGHING),
                NavItem("alerts", "Alerts", Routes.ALERTS),
                NavItem("you", "You", Routes.YOU),
            ),
        )
        val source = NavState(
            chrome = NavChrome.EXPANDED,
            items = weighing.navItems,
            modules = listOf(weighing),
        )

        val result = source.withLeadershipWeighingNavigation(enabled = true)

        assertEquals(
            listOf("Weighing", "Videos", "Alerts", "You"),
            result.modules.single().navItems.map { it.label },
        )
        assertEquals(
            listOf(Routes.WEIGHING, Routes.WEIGHING_VIDEOS, Routes.ALERTS, Routes.YOU),
            result.modules.single().navItems.map { it.href },
        )
    }

    @Test
    fun `operator navigation remains untouched`() {
        val source = NavState(NavChrome.MINIMAL, emptyList())
        assertSame(source, source.withLeadershipWeighingNavigation(enabled = false))
    }
}
