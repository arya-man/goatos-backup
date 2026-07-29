package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.availableModules
import sg.mesha.goatos.core.model.nav.barItems
import sg.mesha.goatos.core.model.nav.resolveModule

class TopLevelChromeTest {
    private val roots = listOf(
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
    fun `root route patterns with query args keep root chrome`() {
        val closerRoots = roots + Routes.VERIFY_ACTION

        assertTrue(isTopLevelRoute("${Routes.VERIFY_ACTION}?actionMode={actionMode}", closerRoots))
        assertFalse(isTopLevelRoute("${Routes.VERIFY_ACTION_DETAIL}?itemId={itemId}", closerRoots))
    }

    @Test
    fun `calendar drill fallback always opens a hosted child`() {
        assertEquals(Routes.calendarDriveRoute(null), calendarTargetRoute(null))
        assertEquals(Routes.calendarDriveRoute(null), calendarTargetRoute(""))
        assertEquals(
            Routes.calendarDriveRoute(null),
            calendarTargetRoute("/vaccination/execution"),
        )
        assertEquals(
            Routes.calendarDriveRoute(null),
            calendarTargetRoute("/vaccination/scan/shed-1"),
        )
        assertEquals(
            Routes.calendarDriveRoute(null),
            calendarTargetRoute("/vaccination/sheds/shed-1"),
        )
        assertEquals(
            Routes.calendarDriveRoute("2026-07-24"),
            calendarTargetRoute(null, "2026-07-24"),
        )
        assertEquals(
            Routes.calendarDriveRoute("2026-07-24"),
            calendarTargetRoute("/vaccination/execution", "2026-07-24"),
        )
        assertEquals(
            Routes.calendarDriveRoute("2026-07-24"),
            calendarTargetRoute("/vaccination/scan/shed-1", "2026-07-24"),
        )
        assertFalse(isTopLevelRoute(calendarTargetRoute(null), roots))
    }

    // -----------------------------------------------------------------------
    // Module-scoped L0 set. The bottom bar (and therefore the L0 root set) is the OPEN
    // module's own destinations, so switching modules must move which routes own chrome —
    // without ever letting a drill inherit it.
    // -----------------------------------------------------------------------

    private val vaccination = NavModule(
        key = "vaccination",
        label = "Vaccination",
        href = Routes.VACCINATION,
        status = NavModuleStatus.AVAILABLE,
        navItems = listOf(
            NavItem(key = "vaccination", label = "Drives", href = Routes.VACCINATION),
            NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
            NavItem(key = "you", label = "You", href = Routes.YOU),
        ),
    )

    private val counts = NavModule(
        key = "counts",
        label = "Counts",
        href = "/counts",
        status = NavModuleStatus.AVAILABLE,
        navItems = listOf(
            NavItem(key = "counts", label = "Counts", href = "/counts"),
            NavItem(key = "birth", label = "Birth", href = "/counts/birth"),
            NavItem(key = "death", label = "Death", href = "/counts/death"),
            NavItem(key = "shifting", label = "Shifting", href = "/counts/shifting"),
			NavItem(key = "milk_preparation", label = "Milk Prep", href = "/counts/milk-preparation"),
            // Approvals were removed from mobile (maintainer decision 2026-07-21): the Counts bar is
            // capture-only now, with no trailing Approval tab.
        ),
    )

    // The Feed module: two L0 read screens on its own bottom bar (Feed Direction + Feed Packing).
    private val feed = NavModule(
        key = "feed_direction",
        label = "Feed",
        href = Routes.FEED_DIRECTION,
        status = NavModuleStatus.AVAILABLE,
        navItems = listOf(
            NavItem(key = "feed_direction", label = "Feed Direction", href = Routes.FEED_DIRECTION),
            NavItem(key = "feed_packing", label = "Feed Packing", href = Routes.FEED_PACKING),
        ),
    )

    private val soon = NavModule(
        key = "breeding",
        label = "Breeding",
        href = "",
        status = NavModuleStatus.SOON,
        navItems = emptyList(),
    )

    private val twoModules = NavState(
        chrome = NavChrome.EXPANDED,
        items = vaccination.navItems,
        modules = listOf(vaccination, counts, feed, soon),
    )

    /**
     * The L0 set the shell computes: EXACTLY the open module's backend-composed items.
     *
     * Nothing is appended. "You" used to be added here (and in the shell) as client-static
     * chrome; it is now a backend nav contribution like any other, which is what let the Counts
     * module put its Approval queue in that trailing slot instead.
     */
    private fun rootsFor(state: NavState, selectedKey: String?, currentRoute: String?) =
        state.barItems(selectedKey, currentRoute).map { it.href }

    @Test
    fun `the open module owns the bottom bar and its routes are the L0 set`() {
        val vaccinationRoots = rootsFor(twoModules, "vaccination", Routes.VACCINATION)
        assertTrue(isTopLevelRoute(Routes.VACCINATION, vaccinationRoots))
        assertFalse(isTopLevelRoute(Routes.CALENDAR, vaccinationRoots))
        // Another module's landing route is NOT an L0 root while this module is open.
        assertFalse(isTopLevelRoute("/counts", vaccinationRoots))

        val countsRoots = rootsFor(twoModules, "counts", "/counts")
        assertTrue(isTopLevelRoute("/counts", countsRoots))
        assertTrue(isTopLevelRoute("/counts/birth", countsRoots))
        assertTrue(isTopLevelRoute("/counts/death", countsRoots))
        assertFalse(isTopLevelRoute(Routes.VACCINATION, countsRoots))
    }

    @Test
    fun `switching modules swaps the bar to that module's own items`() {
        assertEquals(
            listOf(Routes.VACCINATION, Routes.ALERTS, Routes.YOU),
            twoModules.barItems("vaccination", Routes.VACCINATION).map { it.href },
        )
        // The trailing tab DIFFERS by module: the vaccination bar ends in You, while Counts is
        // capture-only (no You, and no Approval since approvals were removed from mobile). This is
        // the assertion that would fail if the client ever went back to appending a fixed tab.
        assertEquals(
            listOf("/counts", "/counts/birth", "/counts/death", "/counts/shifting", "/counts/milk-preparation"),
            twoModules.barItems("counts", "/counts").map { it.href },
        )
    }

    @Test
    fun `a drill inside the open module never inherits chrome`() {
        val countsRoots = rootsFor(twoModules, "counts", "/counts")
        // "/counts/birth" IS a root; a deeper drill under it (/add, /workflows/{id}) is not.
        // Exact membership,
        // never prefix matching (docs/decisions/android-navigation-stack.md).
        assertFalse(isTopLevelRoute("/counts/birth/add", countsRoots))
        assertFalse(isTopLevelRoute("/counts/birth/workflows/wf-1", countsRoots))
        assertFalse(isTopLevelRoute("/counts/shifting/confirm", countsRoots))
        assertFalse(isTopLevelRoute(Routes.CALENDAR_DRIVE, countsRoots))
    }

    @Test
    fun `birth tag route carries backend kid context and is never a root`() {
        val route = Routes.promoteGoatRoute(
            goatId = "kid/1",
            displayId = "G-77",
            temporaryIdentifier = "CPT-00042",
            locationDisplay = "North Park / Shed A",
            rowVersion = 7,
        )

        assertEquals(
            "/counts/birth/tag/kid%2F1?display_id=G-77" +
                "&temporary_identifier=CPT-00042" +
                "&location_display=North%20Park%20%2F%20Shed%20A" +
                "&row_version=7",
            route,
        )
        assertFalse(isTopLevelRoute(route, rootsFor(twoModules, "counts", Routes.COUNTS_BIRTH)))
    }

    @Test
    fun `birth and death nav keys use distinct lifecycle icons`() {
        val birthIcon = MeshaIcons.forNavKey("birth")
        val deathIcon = MeshaIcons.forNavKey("death")

        assertSame(MeshaIcons.Birth, birthIcon)
        assertSame(MeshaIcons.Death, deathIcon)
        assertNotSame(birthIcon, deathIcon)
        assertSame(MeshaIcons.ArrowUpDown, MeshaIcons.forNavKey("birth_death"))
    }

    @Test
    fun `an unselected module falls back to the backend default module`() {
        // No explicit selection yet: the first available module wins, and the "soon" module
        // is never selectable.
        assertEquals(vaccination, twoModules.resolveModule(null, null))
        // The "soon" (breeding) module is never selectable, so it resolves to the default.
        assertEquals(vaccination, twoModules.resolveModule("breeding", null))
        assertEquals(listOf(vaccination, counts, feed), twoModules.availableModules())
    }

    @Test
    fun `the feed module owns a two-tab bar and both feed screens are L0 roots`() {
        val feedRoots = rootsFor(twoModules, "feed_direction", Routes.FEED_DIRECTION)
        assertTrue(isTopLevelRoute(Routes.FEED_DIRECTION, feedRoots))
        assertTrue(isTopLevelRoute(Routes.FEED_PACKING, feedRoots))
        // A drill under a feed root never inherits chrome (exact membership, not prefix).
        assertFalse(isTopLevelRoute("${Routes.FEED_DIRECTION}/detail", feedRoots))
        // Another module's route is not an L0 root while Feed is open.
        assertFalse(isTopLevelRoute(Routes.VACCINATION, feedRoots))
        assertEquals(
            listOf(Routes.FEED_DIRECTION, Routes.FEED_PACKING),
            twoModules.barItems("feed_direction", Routes.FEED_DIRECTION).map { it.href },
        )
    }

    @Test
    fun `a route in another module re-resolves the bar to that module`() {
        // Deep-link/push into Counts while Vaccination was the selected module: the bar must
        // follow the route, otherwise "/counts" would render with no chrome at all.
        val resolved = twoModules.resolveModule("vaccination", "/counts/shifting")
        assertEquals(counts, resolved)
        assertTrue(isTopLevelRoute("/counts/shifting", rootsFor(twoModules, "vaccination", "/counts/shifting")))
    }

    @Test
    fun `ceo videos root keeps its module chrome when compose reports the query route pattern`() {
        val ceoVaccination = vaccination.copy(
            navItems = listOf(
                NavItem(key = "vaccination", label = "Overview", href = Routes.VACCINATION),
                NavItem(key = "videos", label = "Videos", href = Routes.VERIFY_ACTION),
                NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
                NavItem(key = "you", label = "You", href = Routes.YOU),
            ),
        )
        val ceoState = NavState(
            chrome = NavChrome.EXPANDED,
            items = ceoVaccination.navItems,
            modules = listOf(ceoVaccination, counts, soon),
        )
        val composeRoute = "${Routes.VERIFY_ACTION}?${Routes.VERIFY_ACTION_ARG}={${Routes.VERIFY_ACTION_ARG}}"
        val roots = rootsFor(ceoState, "vaccination", composeRoute)

        assertEquals(ceoVaccination, ceoState.resolveModule("vaccination", composeRoute))
        assertTrue(isTopLevelRoute(composeRoute, roots))
        assertTrue(drawerAvailable(ceoState.chrome, composeRoute, roots))
    }

    @Test
    fun `operator and standalone verifier roles do not gain ceo videos drawer`() {
        val composeRoute = "${Routes.VERIFY_ACTION}?${Routes.VERIFY_ACTION_ARG}={${Routes.VERIFY_ACTION_ARG}}"

        val operatorState = NavState(
            chrome = NavChrome.MINIMAL,
            items = vaccination.navItems,
            modules = listOf(vaccination),
        )
        val operatorRoots = rootsFor(operatorState, "vaccination", Routes.VACCINATION)
        assertFalse(isTopLevelRoute(composeRoute, operatorRoots))
        assertFalse(drawerAvailable(operatorState.chrome, composeRoute, operatorRoots))

        val verifier = NavModule(
            key = "verify",
            label = "Verify",
            href = Routes.VERIFY,
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(NavItem(key = "verify", label = "Verify", href = Routes.VERIFY)),
        )
        val verifierState = NavState(
            chrome = NavChrome.MINIMAL,
            items = verifier.navItems,
            modules = listOf(verifier),
        )
        val verifierRoots = rootsFor(verifierState, "verify", Routes.VERIFY)
        assertFalse(isTopLevelRoute(composeRoute, verifierRoots))
        assertFalse(drawerAvailable(verifierState.chrome, composeRoute, verifierRoots))
    }

    @Test
    fun `a shared route keeps the selected module's bar`() {
        // Alerts is contributed by several modules. The explicitly selected module wins so
        // the bar does not silently flip while the operator is working inside one module.
        assertEquals(vaccination, twoModules.resolveModule("vaccination", Routes.ALERTS))
    }

    // -----------------------------------------------------------------------
    // Drawer availability. The defect this locks down: drawer access used to be opt-in per
    // screen (each one read LocalDrawerOpener and drew its own hamburger), so the Counts
    // module shipped with NO menu affordance at all — an operator who opened /counts could
    // not get back to Vaccination except by an undiscoverable edge swipe. Drawer access is
    // now derived from backend-composed L0 membership, so it must hold for EVERY root of
    // EVERY module without naming any of them.
    // -----------------------------------------------------------------------

    @Test
    fun `every L0 root of every module offers the drawer`() {
        twoModules.availableModules().forEach { module ->
            val roots = rootsFor(twoModules, module.key, module.href)
            // Every backend nav_item of the module — the whole L0 set, nothing appended.
            module.navItems.map { it.href }.forEach { route ->
                assertTrue(
                    "${module.key} root $route must offer the module drawer",
                    drawerAvailable(twoModules.chrome, route, roots),
                )
            }
        }
    }

    @Test
    fun `a drill never offers the drawer`() {
        val countsRoots = rootsFor(twoModules, "counts", "/counts")
        listOf(
            Routes.CALENDAR_DRIVE,
            Routes.SCAN,
            Routes.SUBMIT,
            Routes.RECORD,
            Routes.COUNTS_BIRTH_ADD,
            Routes.COUNTS_DEATH_ADD,
            Routes.COUNTS_SHIFTING_ADD,
        ).forEach { route ->
            assertFalse(route, drawerAvailable(twoModules.chrome, route, countsRoots))
        }
        // A root of ANOTHER module is not a root here either — it would be navigated to, not
        // rendered with this module's chrome.
        assertFalse(drawerAvailable(twoModules.chrome, Routes.VACCINATION, countsRoots))
    }

    @Test
    fun `single-module chrome has no drawer to offer on any route`() {
        // MINIMAL chrome = one module, so there is nothing to switch to: the bottom bar is the
        // whole nav. Roots still own the bar; they just have no drawer.
        val single = NavState(chrome = NavChrome.MINIMAL, items = vaccination.navItems, modules = listOf(vaccination))
        val roots = rootsFor(single, "vaccination", Routes.VACCINATION)
        assertTrue(isTopLevelRoute(Routes.VACCINATION, roots))
        assertFalse(drawerAvailable(single.chrome, Routes.VACCINATION, roots))
    }

    @Test
    fun `a route with no match offers nothing`() {
        val roots = rootsFor(twoModules, "counts", "/counts")
        assertFalse(drawerAvailable(twoModules.chrome, null, roots))
        assertFalse(drawerAvailable(twoModules.chrome, "/not-a-route", roots))
    }

    /**
     * The bottom bar is whatever the backend composed — the client appends nothing.
     *
     * This is the regression lock for the hardcoded trailing "You" tab. While the shell appended
     * it unconditionally, an operator's Counts module rendered THREE tabs (Birth/Death, Shifting,
     * You) no matter what the backend said, and the Counts module could not put its Approval
     * queue in that slot without an app release.
     */
    @Test
    fun `the bar is exactly the backend items with nothing appended`() {
        // An operator's Counts module: the backend gates Approval on the approval permissions and
        // sends neither it nor a You tab, so the bar is exactly the two capture tabs.
        val operatorCounts = NavModule(
            key = "counts",
            label = "Counts",
            href = "/counts/birth",
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(
                NavItem(key = "birth", label = "Birth", href = "/counts/birth"),
            NavItem(key = "death", label = "Death", href = "/counts/death"),
                NavItem(key = "shifting", label = "Shifting", href = "/counts/shifting"),
                NavItem(key = "milk_preparation", label = "Milk Prep", href = "/counts/milk-preparation"),
            ),
        )
        val operatorState = NavState(
            chrome = NavChrome.MINIMAL,
            items = operatorCounts.navItems,
            modules = listOf(operatorCounts),
        )
        assertEquals(
            listOf("/counts/birth", "/counts/death", "/counts/shifting", "/counts/milk-preparation"),
            operatorState.barItems("counts", "/counts/birth").map { it.href },
        )
        // ...and no You route sneaks into the operator's L0 set.
        val operatorRoots = rootsFor(operatorState, "counts", "/counts/birth")
        assertFalse(isTopLevelRoute(Routes.YOU, operatorRoots))

        // Approvals were removed from mobile (maintainer decision 2026-07-21): no Counts bar — for
        // any role — carries an approval route. The Counts L0 set is capture-only.
        val countsRoots = rootsFor(twoModules, "counts", "/counts")
        assertFalse(isTopLevelRoute("/counts/approvals", countsRoots))
    }

    @Test
    fun `a bootstrap with no modules still renders its visible_navigation bar`() {
        // Older backend or a cached pre-`modules` bootstrap: fall back to visible_navigation
        // rather than dropping the bottom bar entirely.
        val legacy = NavState(chrome = NavChrome.MINIMAL, items = vaccination.navItems)
        assertEquals(vaccination.navItems, legacy.barItems(null, Routes.VACCINATION))
        assertTrue(isTopLevelRoute(Routes.VACCINATION, rootsFor(legacy, null, Routes.VACCINATION)))
    }
}
