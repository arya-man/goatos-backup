package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.DrawerValue
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.OwnedModule
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.leadership.LeadershipScreen

/**
 * Screenshot tests for the app's nav chrome PER ROLE TIER (item: role-based screenshot
 * coverage). [ScreenshotTest] covers every SCREEN once, but never renders the shell's own
 * chrome (drawer/module-switcher vs bottom-bar-only) for a given role — this class closes
 * that gap.
 *
 * TRD §14 dumb-renderer rule (NavContract.kt): [sg.mesha.goatos.core.model.nav.NavChrome]
 * is backend-computed truth — EXPANDED iff the principal owns >=2 visible modules, else
 * MINIMAL (bottom-bar only). Each test below constructs an explicit [NavState] fixture for
 * one role tier — mirroring the seeded HR departments in
 * `docs/decisions/user-module-ownership-and-nav-chrome.md` / `docs/hr/roster-rbac-design.md`
 * (`vaccination` = 1 module -> MINIMAL, `admin_data` = 3 modules -> EXPANDED, `leadership` = 4
 * modules -> EXPANDED) — and renders [GoatOsShellChrome] directly with that role's landing
 * screen as content.
 *
 * Renders [GoatOsShellChrome] rather than the full [GoatOsShell]/[AppNavHost] so no
 * NavHostController or Hilt-injected ViewModel is needed in a plain Paparazzi JVM test — only
 * the chrome plus the same mock-accurate sample*State() fixtures ScreenshotTest/ScreenSamples.kt
 * already use.
 *
 * Run: ./gradlew :app:recordPaparazziDevDebug   (first run / after an intentional UI change)
 *      ./gradlew :app:verifyPaparazziDevDebug   (CI — fails on any pixel diff from the goldens)
 *
 * Gallery: tools/android/build-screenshot-gallery.py assembles these goldens into the
 * "Role chrome coverage" section alongside the per-screen gallery.
 */
class RoleChromeScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    private fun shot(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        content()
                    }
                }
            }
        }
    }

    // Leadership-tier nav — Overview/Calendar/Alerts (+ the shell's always-present "You" tab).
    // Used by every EXPANDED-chrome role below (CEO, Director, Park Head).
    private fun leadershipNavItems() = listOf(
        NavItem(key = "overview", label = "Overview", href = Routes.LEADERSHIP),
        NavItem(key = "calendar", label = "Calendar", href = Routes.CALENDAR),
        NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
    )

    // Single-vertical operational nav — Drives/Calendar/Alerts (+ "You"). Used by every
    // MINIMAL-chrome, vaccination-department-only role below (Park Manager, Operator).
    private fun operationalNavItems() = listOf(
        NavItem(key = "vaccination", label = "Drives", href = Routes.VACCINATION),
        NavItem(key = "calendar", label = "Calendar", href = Routes.CALENDAR),
        NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
    )

    // The seeded `leadership` department: all 4 built modules (admin_data's 3 + vaccination's 1)
    // -> EXPANDED. See docs/decisions/user-module-ownership-and-nav-chrome.md.
    private val leadershipFourModules = listOf(
        OwnedModule("pc", "vaccination"),
        OwnedModule("admin", "config"),
        OwnedModule("admin", "sop"),
        OwnedModule("admin", "audit"),
    )

    // The seeded `vaccination` department: its 1 module -> MINIMAL.
    private val vaccinationOnlyModule = listOf(OwnedModule("pc", "vaccination"))

    // CEO / superuser (role `ceo_internal`, tenant-scoped, department `leadership`): owns all 4
    // built modules -> EXPANDED, landing Overview.
    @Test
    fun role_ceo() = shot("role_ceo") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
                ownedModules = leadershipFourModules,
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Same CEO/superuser fixture with the drawer forced OPEN, so the EXPANDED-only module
    // switcher (invisible in role_ceo's closed-drawer golden) is actually visible for review.
    @Test
    fun role_ceo_drawer() = shot("role_ceo_drawer") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
                ownedModules = leadershipFourModules,
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
            initialDrawerValue = DrawerValue.Open,
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Director (HR Designation grade, roster-rbac-design.md §0.2 axis (a)) — EXPANDED, owns
    // >=2 modules, landing Overview.
    //
    // OPEN QUESTION (flagged, not guessed past): only 3 departments are seeded today
    // (`vaccination`=1 module, `admin_data`=3, `leadership`=4 — see
    // docs/decisions/user-module-ownership-and-nav-chrome.md). There is no committed
    // "Director" department/module-ownership row, and roster-rbac-design.md keeps HR grade,
    // Operational Position, and Department ownership as three independent axes — a Director's
    // grade does not by itself imply which modules they own. This fixture's 2-module set
    // (Preventive Care execution + SOP policy) is an illustrative EXPANDED shape for chrome
    // coverage, not a backend-verified seed.
    @Test
    fun role_director() = shot("role_director") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
                ownedModules = listOf(OwnedModule("pc", "vaccination"), OwnedModule("admin", "sop")),
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Park Head (Operational Position, roster-rbac-design.md §0.2 axis (b); oversees a whole
    // park/center) — EXPANDED, owns >=2 modules scoped to that park, landing Overview.
    // Same illustrative-fixture caveat as role_director above — no committed Park-Head-specific
    // department row exists yet.
    @Test
    fun role_park_head() = shot("role_park_head") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
                ownedModules = listOf(OwnedModule("pc", "vaccination"), OwnedModule("admin", "sop")),
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Park Manager (Operational Position, Manager-tier per roster-rbac-design.md §0.1 — e.g.
    // "Preventive Care Manager") — the realistic single-vertical case: today's seeded model owns
    // only the `vaccination` department's 1 module -> MINIMAL bottom-bar, landing Calendar.
    //
    // This is intentionally IDENTICAL chrome to role_operator below: nav_chrome is computed from
    // department module-count, not HR/operational grade (TRD §14), so a Manager and an
    // Operator who both sit in the single-module `vaccination` department render the same
    // chrome today. Not judged to own >=2 modules under the current seed, so no additional
    // EXPANDED variant is added — see the OPEN QUESTION above re: Director/Park Head for the
    // related ambiguity this shares.
    @Test
    fun role_park_manager() = shot("role_park_manager") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = operationalNavItems(),
                ownedModules = vaccinationOnlyModule,
            ),
            currentRoute = Routes.CALENDAR,
            onNavigate = {},
        ) {
            CalendarScreen(state = sampleCalendarState())
        }
    }

    // Operator (single-vertical, Vaccination) — MINIMAL bottom-bar, items Drives/Calendar/
    // Alerts/You, landing Calendar. Matches the existing operator vs leadership split this
    // task closes the per-role gap for.
    @Test
    fun role_operator() = shot("role_operator") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = operationalNavItems(),
                ownedModules = vaccinationOnlyModule,
            ),
            currentRoute = Routes.CALENDAR,
            onNavigate = {},
        ) {
            CalendarScreen(state = sampleCalendarState())
        }
    }
}
