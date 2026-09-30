package sg.mesha.goatos.ui

import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.ComposeNavigator
import androidx.navigation.compose.composable
import androidx.navigation.createGraph
import androidx.navigation.navArgument
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.barItems
import sg.mesha.goatos.core.model.nav.resolveModule

/**
 * A web-authored phone tab (maintainer instruction 2026-10-01) is ONE parameterized L0 root,
 * `/pen-routines/tab/{tab_key}`, while the backend composes CONCRETE bar hrefs
 * (`/pen-routines/tab/fumigation`). These pin that the concrete href is hosted, is a granted root,
 * selects its own bar item, never collides with the task drill, and that the shell's
 * findNode/popBackStack resolve the concrete href against the pattern (navigation 2.9 matches a
 * route by pattern OR by `matchRoute`, and popBackStack by `hasRoute(route, arguments)`).
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class PenRoutineTabRouteTest {

    private val fumigation = "/pen-routines/tab/fumigation"

    private val pcCare = NavModule(
        key = "pc_care",
        label = "PC Care",
        href = "/pc-care/deworming",
        status = NavModuleStatus.AVAILABLE,
        navItems = listOf(
            NavItem(key = "pc_deworming", label = "Deworming", href = "/pc-care/deworming"),
            NavItem(key = "routine_tab_fumigation", label = "Fumigation", href = fumigation, icon = "fumigation"),
            NavItem(key = "you", label = "You", href = Routes.YOU),
        ),
    )
    private val routines = NavModule(
        key = "pen_routines",
        label = "Routines",
        href = Routes.PEN_ROUTINES,
        status = NavModuleStatus.AVAILABLE,
        navItems = listOf(NavItem(key = "pen_routines", label = "Routines", href = Routes.PEN_ROUTINES)),
    )
    private val navState = NavState(chrome = NavChrome.EXPANDED, items = pcCare.navItems, modules = listOf(pcCare, routines))

    @Test
    fun `the concrete tab href names its key and nothing else does`() {
        assertEquals("fumigation", Routes.penRoutineTabKeyFromHref(fumigation))
        assertEquals("fumigation", Routes.penRoutineTabKeyFromHref("$fumigation?x=1"))
        assertEquals(fumigation, Routes.penRoutineTabRoute("fumigation"))
        assertNull(Routes.penRoutineTabKeyFromHref(Routes.PEN_ROUTINES))
        assertNull(Routes.penRoutineTabKeyFromHref("/pen-routines/tab/"))
        assertNull(Routes.penRoutineTabKeyFromHref("/pen-routines/tab/Bad-Key"))
        assertNull(Routes.penRoutineTabKeyFromHref("/pen-routines/tab/a/b"))
        // The two-segment task drill never reads the tab path as a task id.
        assertNull(Routes.penRoutineIdFromHref(fumigation))
        assertEquals("task-1", Routes.penRoutineIdFromHref("/pen-routines/task-1"))
    }

    @Test
    fun `a granted tab is a root, selects its own bar item and keeps its module`() {
        assertTrue(isRootDestination(fumigation))
        assertTrue(navState.grantsRootDestination(fumigation))
        assertFalse(navState.grantsRootDestination("/pen-routines/tab/other"))
        // The shell reasons about the CONCRETE href, never the pattern.
        val shellRoute = shellRouteOf(Routes.PEN_ROUTINE_TAB, "fumigation")
        assertEquals(fumigation, shellRoute)
        assertEquals(Routes.PEN_ROUTINES, shellRouteOf(Routes.PEN_ROUTINES, null))
        assertEquals(Routes.PEN_ROUTINE, shellRouteOf(Routes.PEN_ROUTINE, null))
        assertTrue(isTopLevelRoute(shellRoute, pcCare.navItems.map { it.href }))
        assertEquals("pc_care", navState.resolveModule(selectedKey = null, currentRoute = shellRoute)?.key)
        assertEquals(pcCare.navItems, navState.barItems(selectedKey = "pen_routines", currentRoute = shellRoute))
        // The task drill stays a drill.
        assertFalse(isTopLevelRoute(Routes.PEN_ROUTINE, pcCare.navItems.map { it.href }))
        assertFalse(isRootDestination(Routes.penRoutineRoute("task-1")))
        assertEquals("Fumigation", navState.labelForHref(fumigation))
    }

    @Test
    fun `a push naming a tab opens that tab, and a tab is never the start destination`() {
        assertEquals(fumigation, pushTargetRoute(fumigation))
        assertEquals(Routes.penRoutineRoute("task-1"), pushTargetRoute("/pen-routines/task-1"))
        val tabFirst = NavState(
            chrome = NavChrome.MINIMAL,
            items = listOf(NavItem(key = "routine_tab_fumigation", label = "Fumigation", href = fumigation)) + routines.navItems,
            modules = listOf(routines),
        )
        assertEquals(Routes.PEN_ROUTINES, startDestinationFor(tabFirst))
    }

    @Test
    fun `findNode and popBackStack resolve a concrete tab href against the pattern`() {
        val nav = NavHostController(RuntimeEnvironment.getApplication()).apply {
            navigatorProvider.addNavigator(ComposeNavigator())
        }
        nav.graph = nav.createGraph(startDestination = Routes.PEN_ROUTINES) {
            composable(Routes.PEN_ROUTINES) {}
            composable(
                Routes.PEN_ROUTINE,
                arguments = listOf(navArgument(Routes.PEN_ROUTINE_ID_ARG) { type = NavType.StringType }),
            ) {}
            composable(
                Routes.PEN_ROUTINE_TAB,
                arguments = listOf(navArgument(Routes.PEN_ROUTINE_TAB_KEY_ARG) { type = NavType.StringType }),
            ) {}
        }

        val tabNode = nav.graph.findNode(fumigation)
        assertNotNull("the concrete tab href is hosted", tabNode)
        assertEquals(Routes.PEN_ROUTINE_TAB, tabNode?.route)
        assertEquals(Routes.PEN_ROUTINE, nav.graph.findNode("/pen-routines/task-1")?.route)
        assertNull("an unknown href is still refused", nav.graph.findNode("/nowhere/at/all"))

        nav.navigate(fumigation)
        assertEquals("fumigation", nav.currentBackStackEntry?.arguments?.getString(Routes.PEN_ROUTINE_TAB_KEY_ARG))
        nav.navigate(Routes.penRoutineRoute("task-1"))
        // Tapping the tab again pops back to ITS entry...
        assertTrue(nav.popBackStack(fumigation, inclusive = false))
        assertEquals(Routes.PEN_ROUTINE_TAB, nav.currentBackStackEntry?.destination?.route)
        // ...and a DIFFERENT tab's href never pops to it.
        assertFalse(nav.popBackStack("/pen-routines/tab/cleaning", inclusive = false))
    }
}
