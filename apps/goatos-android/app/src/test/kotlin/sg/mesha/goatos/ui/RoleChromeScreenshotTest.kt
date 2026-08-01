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
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.feature.sheds.ShedsScreen

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

    // Operator / preventive-care field lens: Drives/Alerts/You.
    private fun vaccinationFieldNavItems() = listOf(
        NavItem(key = "vaccination", label = "Drives", href = Routes.VACCINATION),
        NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
        // Backend emits the You tab (module contribution, priority 100) — the bar is 3 tabs.
        NavItem(key = "you", label = "You", href = Routes.YOU),
    )

    // CEO/CXO strategic lens inside the Vaccination module. The old standalone
    // Leadership module is gone; Overview is the /vaccination tab inside Vaccination.
    /**
     * The bottom bar as the backend actually composes it for an EXPANDED-chrome principal.
     *
     * No "you" item: the backend strips it whenever a drawer exists, because the drawer footer
     * already carries the account row above Sign out (workforce/app/service.go, NavChromeExpanded
     * branch). Keeping it in this fixture made the golden show You in BOTH places at once -- a
     * state that cannot ship, quietly baked into the screenshot everyone reviews against.
     * A principal with no drawer keeps You on the bar; see [vaccinationCloserNavItems].
     */
    private fun vaccinationCeoNavItems() = listOf(
        NavItem(key = "overview", label = "Overview", href = Routes.VACCINATION),
        NavItem(key = "calendar", label = "Calendar", href = Routes.CALENDAR),
        NavItem(key = "videos", label = "Videos", href = Routes.VERIFY_ACTION),
        NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
    )

    private fun vaccinationCloserNavItems() = listOf(
        NavItem(key = "vaccination", label = "Drives", href = Routes.VACCINATION),
        NavItem(key = "calendar", label = "Calendar", href = Routes.CALENDAR),
        NavItem(key = "videos", label = "Videos", href = Routes.VERIFY_ACTION),
        NavItem(key = "alerts", label = "Alerts", href = Routes.ALERTS),
        NavItem(key = "you", label = "You", href = Routes.YOU),
    )

    /**
     * Backend-composed drawer rows (`/app/bootstrap` `modules`), mirroring bootstrap_copy.go's
     * moduleNavRegistry: two built modules that each own a bar, plus the two roadmap rows the
     * backend advertises as "soon". The drawer renders EXACTLY this — the client no longer
     * holds a module list of its own — so these fixtures are what the EXPANDED goldens prove.
     * Labels are backend copy and render verbatim.
     */
    private fun vaccinationModule() = NavModule(
        key = "vaccination",
        label = "Vaccination",
        href = Routes.VACCINATION,
        status = NavModuleStatus.AVAILABLE,
        navItems = vaccinationFieldNavItems(),
    )

    private fun ceoVaccinationModule() = NavModule(
        key = "vaccination",
        label = "Vaccination",
        href = Routes.VACCINATION,
        status = NavModuleStatus.AVAILABLE,
        navItems = vaccinationCeoNavItems(),
    )

    // CEO/CXO drawer: Vaccination + Counts + the two roadmap "soon" rows.
    // A preventive-care leader (Director/Park Head) does NOT get this drawer -- see their
    // MINIMAL single-module fixtures below.
    private fun drawerModules() = listOf(
        ceoVaccinationModule(),
        NavModule(
            key = "counts",
            label = "Counts",
            href = "/counts",
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(
                NavItem(key = "counts", label = "Counts", href = "/counts"),
                NavItem(key = "birth", label = "Birth", href = "/counts/birth"),
                NavItem(key = "death", label = "Death", href = "/counts/death"),
                NavItem(key = "shifting", label = "Shifting", href = "/counts/shifting"),
            ),
        ),
        NavModule(key = "feed_direction", label = "Feed direction", href = "", status = NavModuleStatus.SOON, navItems = emptyList()),
        NavModule(key = "breeding", label = "Breeding", href = "", status = NavModuleStatus.SOON, navItems = emptyList()),
    )

    // CEO / superuser (role `ceo_internal`, tenant-scoped): EXPANDED, lands on Vaccination.
    @Test
    fun role_ceo() = shot("role_ceo") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = vaccinationCeoNavItems(),
                modules = drawerModules(),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // Same CEO/superuser fixture with the drawer forced OPEN, so the EXPANDED chrome
    // switcher (invisible in role_ceo's closed-drawer golden) is actually visible for review.
    @Test
    fun role_ceo_drawer() = shot("role_ceo_drawer") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = vaccinationCeoNavItems(),
                modules = drawerModules(),
            ),
            // Vaccination active on its landing so the golden shows the
            // active-module state (green rail + check) alongside the visible modules, Soon rows,
            // Settings, and Sign-out.
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
            initialDrawerValue = DrawerValue.Open,
            drawerProfile = DrawerProfile(name = "Arun Kumar", role = "Health Asst Mgr · CBE", initials = "AK"),
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // PC Director (preventive-care specialty) — MINIMAL bottom bar, Vaccination home only
    // (no Counts/Feed/Breeding), landing on Vaccination.
    @Test
    fun role_director() = shot("role_director") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = vaccinationCloserNavItems(),
                modules = listOf(vaccinationModule().copy(navItems = vaccinationCloserNavItems())),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // Park Head (oversees one park) — MINIMAL bottom bar, Vaccination home only, landing
    // on Vaccination. His single-park limit is grant data-scope, not nav.
    @Test
    fun role_park_head() = shot("role_park_head") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = vaccinationCloserNavItems(),
                modules = listOf(vaccinationModule().copy(navItems = vaccinationCloserNavItems())),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // Park Manager (Operational Position, Manager-tier per roster-rbac-design.md §0.1 — e.g.
    // "Preventive Care Manager") — MINIMAL bottom-bar, landing shed-first Vaccination queue.
    @Test
    fun role_park_manager() = shot("role_park_manager") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = vaccinationFieldNavItems(),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // Operator (single-vertical, Vaccination) — MINIMAL bottom-bar, items Drives/Alerts/You,
    // landing shed-first on today's work inside the 7-day queue.
    @Test
    fun role_operator() = shot("role_operator") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = vaccinationFieldNavItems(),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }
}
