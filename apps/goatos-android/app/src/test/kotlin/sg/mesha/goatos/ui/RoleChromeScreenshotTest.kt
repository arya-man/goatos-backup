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
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.leadership.LeadershipScreen

/**
 * Screenshot tests for the app's nav chrome PER ROLE TIER (item: role-based screenshot
 * coverage). [ScreenshotTest] covers every SCREEN once, but never renders the shell's own
 * chrome (drawer/module-switcher vs bottom-bar-only) for a given role — this class closes
 * that gap.
 *
 * TRD §14 dumb-renderer rule (NavContract.kt): [sg.mesha.goatos.core.model.nav.NavChrome]
 * is backend-computed truth. Each test below constructs an explicit [NavState] fixture for
 * one role tier and renders [GoatOsShellChrome] directly with that role's landing screen as
 * content.
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

    // CEO / superuser (role `ceo_internal`, tenant-scoped): EXPANDED, landing Overview.
    @Test
    fun role_ceo() = shot("role_ceo") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Same CEO/superuser fixture with the drawer forced OPEN, so the EXPANDED chrome
    // switcher (invisible in role_ceo's closed-drawer golden) is actually visible for review.
    @Test
    fun role_ceo_drawer() = shot("role_ceo_drawer") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
            ),
            // Vaccination active so the golden shows the mock's active-module state (green rail +
            // check) alongside the visible modules, Soon rows, Settings, and Sign-out.
            currentRoute = Routes.VACCINATION,
            onNavigate = {},
            initialDrawerValue = DrawerValue.Open,
            drawerProfile = DrawerProfile(name = "Arun Kumar", role = "Health Asst Mgr · CBE", initials = "AK"),
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Director (HR Designation grade) — EXPANDED, landing Overview.
    @Test
    fun role_director() = shot("role_director") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Park Head (Operational Position; oversees a whole park/center) — EXPANDED, landing Overview.
    @Test
    fun role_park_head() = shot("role_park_head") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = leadershipNavItems(),
            ),
            currentRoute = Routes.LEADERSHIP,
            onNavigate = {},
        ) {
            LeadershipScreen(state = sampleLeadershipState())
        }
    }

    // Park Manager (Operational Position, Manager-tier per roster-rbac-design.md §0.1 — e.g.
    // "Preventive Care Manager") — MINIMAL bottom-bar, landing Calendar.
    @Test
    fun role_park_manager() = shot("role_park_manager") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = operationalNavItems(),
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
            ),
            currentRoute = Routes.CALENDAR,
            onNavigate = {},
        ) {
            CalendarScreen(state = sampleCalendarState())
        }
    }
}
