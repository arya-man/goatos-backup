package sg.mesha.goatos.core.network

import org.junit.Assert.assertEquals
import org.junit.Test

class CountsNavigationMappingTest {
    @Test
    fun `counts is preserved from every backend bootstrap navigation surface`() {
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

        assertEquals(listOf("counts", "weighing"), state.modules.map { it.key })
        assertEquals(listOf("/counts"), state.modules.first().navItems.map { it.href })
        assertEquals(listOf("/counts"), state.items.map { it.href })
    }
}
