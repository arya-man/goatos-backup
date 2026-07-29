package sg.mesha.goatos.core.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class CountsNavigationDisabledTest {
    @Test
    fun `counts is removed from every bootstrap navigation surface`() {
        val state = BootstrapDto(
            navChrome = "expanded",
            visibleNavigation = listOf(
                NavItemDto(key = "counts", label = "Counts", href = "/counts"),
            ),
            modules = listOf(
                BootstrapModuleDto(
                    key = "counts",
                    label = "Counts",
                    href = "/counts",
                    status = "available",
                    navItems = listOf(NavItemDto(key = "counts", label = "Counts", href = "/counts")),
                ),
                BootstrapModuleDto(
                    key = "weighing",
                    label = "Weighing",
                    href = "/weighing",
                    status = "available",
                    navItems = listOf(NavItemDto(key = "weighing", label = "Weighing", href = "/weighing")),
                ),
            ),
        ).toNavState()

        assertFalse(state.modules.any { it.key == "counts" })
        assertFalse(state.items.any { it.href.startsWith("/counts") })
        assertEquals("/weighing", state.items.single().href)
    }
}
