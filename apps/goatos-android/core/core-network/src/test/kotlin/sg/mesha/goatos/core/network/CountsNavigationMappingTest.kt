package sg.mesha.goatos.core.network

import org.junit.Assert.assertEquals
import org.junit.Test

class CountsNavigationMappingTest {
    @Test
    fun `counts is preserved from every backend bootstrap navigation surface`() {
        val state = BootstrapDto(
            navChrome = "expanded",
            visibleNavigation = listOf(
                NavItemDto(key = "birth", label = "Birth", href = "/counts/birth"),
            ),
            modules = listOf(
                BootstrapModuleDto(
                    key = "counts",
                    label = "Counts",
                    href = "/counts/birth",
                    status = "available",
                    navItems = listOf(NavItemDto(key = "birth", label = "Birth", href = "/counts/birth")),
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
        assertEquals(listOf("/counts/birth"), state.modules.first().navItems.map { it.href })
        assertEquals(listOf("/counts/birth"), state.items.map { it.href })
    }
}
