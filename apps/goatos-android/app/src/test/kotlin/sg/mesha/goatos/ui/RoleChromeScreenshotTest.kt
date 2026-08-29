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
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.feature.counts.WorkflowCardBucket
import sg.mesha.goatos.feature.counts.WorkflowCardUi
import sg.mesha.goatos.feature.counts.WorkflowChipUi
import sg.mesha.goatos.feature.counts.WorkflowListScreen
import sg.mesha.goatos.feature.counts.WorkflowListUiState
import sg.mesha.goatos.feature.counts.WorkflowModuleUi
import sg.mesha.goatos.feature.feed.FeedDropdownOption
import sg.mesha.goatos.feature.feed.FeedTransportFilterUi
import sg.mesha.goatos.feature.feed.FeedTransportRowUi
import sg.mesha.goatos.feature.feed.FeedTransportScreen
import sg.mesha.goatos.feature.feed.FeedTransportUiState
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.weighing.WeighingScreen

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
        NavItem(key = "alerts", label = "Alerts", href = Routes.VACCINATION_ALERTS),
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
        NavItem(key = "alerts", label = "Alerts", href = Routes.VACCINATION_ALERTS),
    )

    private fun vaccinationCloserNavItems() = listOf(
        NavItem(key = "vaccination", label = "Drives", href = Routes.VACCINATION),
        NavItem(key = "calendar", label = "Calendar", href = Routes.CALENDAR),
        NavItem(key = "videos", label = "Videos", href = Routes.VERIFY_ACTION),
        NavItem(key = "alerts", label = "Alerts", href = Routes.VACCINATION_ALERTS),
        NavItem(key = "you", label = "You", href = Routes.YOU),
    )

    private fun countsFieldNavItems() = listOf(
        NavItem(key = "birth", label = "Birth", href = Routes.COUNTS_BIRTH),
        NavItem(key = "death", label = "Death", href = Routes.COUNTS_DEATH),
        NavItem(key = "shifting", label = "Shifting", href = Routes.COUNTS_SHIFTING),
    )

    private fun feedFieldNavItems() = listOf(
        NavItem(key = "feed_direction", label = "Feed Direction", href = "/feed/direction"),
        NavItem(key = "feed_packing", label = "Feed Packing", href = "/feed/packing"),
        NavItem(key = "feed_transport", label = "Feed Transport", href = "/feed/transport"),
    )

    private fun weighingDirectorNavItems() = listOf(
        NavItem(key = "weighing", label = "My work", href = "/weighing"),
        NavItem(key = "operators", label = "Operators", href = "/weighing/operators"),
        NavItem(key = "weighing_alerts", label = "Alerts", href = "/weighing/alerts"),
        NavItem(key = "you", label = "You", href = Routes.YOU),
    )

    private fun countsModule() = NavModule(
        key = "counts",
        label = "Herd Operations",
        href = Routes.COUNTS_BIRTH,
        status = NavModuleStatus.AVAILABLE,
        navItems = countsFieldNavItems(),
    )

    private fun feedModule() = NavModule(
        key = "feed_direction",
        label = "Feed",
        href = "/feed/direction",
        status = NavModuleStatus.AVAILABLE,
        navItems = feedFieldNavItems(),
    )

    private fun weighingDirectorModule() = NavModule(
        key = "weighing",
        label = "Weighing",
        href = "/weighing",
        status = NavModuleStatus.AVAILABLE,
        navItems = weighingDirectorNavItems(),
    )

    /**
     * The field operator's REAL module set. A preventive-care department operator is granted
     * Vaccination, Herd Operations, Feed, Health and Milk (bootstrap_copy.go resolves these from
     * department_module_grants), so their chrome is EXPANDED with a drawer -- not the single
     * Vaccination module the old fixtures implied.
     */
    private fun operatorModules() = listOf(
        vaccinationModule(),
        countsModule(),
        feedModule(),
        NavModule(
            key = "aas_health",
            label = "Health",
            href = "/health/adults",
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(
                NavItem(key = "health_adults", label = "Adults", href = "/health/adults"),
                NavItem(key = "health_kids", label = "Kids", href = "/health/kids"),
            ),
        ),
        NavModule(
            key = "milk",
            label = "Milk",
            href = "/counts/milk-preparation",
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(
                NavItem(key = "milk_preparation", label = "Milk Prep", href = "/counts/milk-preparation"),
                NavItem(key = "milk_feeding", label = "Milk Feeding", href = "/counts/milk-feeding"),
                NavItem(key = "colostrum", label = "Colostrum", href = "/counts/colostrum"),
            ),
        ),
    )

    /**
     * The Birth TAB's real landing content. Routes.COUNTS_BIRTH renders WorkflowListDestination
     * (AppNavHost.kt) -- the per-goat outstanding-action work list -- and the recording FORM sits
     * behind the ＋ at Routes.COUNTS_BIRTH_ADD. Rendering the form here would put a screen under
     * the Birth tab that tapping Birth never actually reaches.
     */
    @androidx.compose.runtime.Composable
    private fun sampleBirthRows() = flowOf(
        PagingData.from(
            listOf(
                WorkflowCardUi(
                    workflowId = "b-1", displayId = "CPT-10234", roleLabel = "Kid",
                    metaLine = "Born 6 Aug 05:40 · Castro 2 · Boer",
                    actionsDone = 1, actionsTotal = 4,
                    nextKindLabel = "Next", nextTitle = "Weigh the kid",
                    dueLabel = "2h late", overdue = true, bucket = WorkflowCardBucket.OVERDUE,
                ),
                WorkflowCardUi(
                    workflowId = "b-2", displayId = "CPT-10235", roleLabel = "Kid",
                    metaLine = "Born 5 Aug 23:10 · Gandhi 1 · Sirohi",
                    actionsDone = 0, actionsTotal = 4,
                    nextKindLabel = "Next", nextTitle = "Fit the ear tag",
                    dueLabel = "15:00", overdue = false, bucket = WorkflowCardBucket.DUE,
                ),
            ),
        ),
    ).collectAsLazyPagingItems()

    private fun sampleBirthListState() = WorkflowListUiState(
        module = WorkflowModuleUi.BIRTH,
        subtitle = "2 births to finish",
        dateIso = "2026-08-06",
        dateLabel = "Today · 6 Aug",
        isToday = true,
        chips = listOf(
            WorkflowChipUi("all", "All", 2),
            WorkflowChipUi("overdue", "Overdue", 1),
            WorkflowChipUi("due", "Due", 1),
            WorkflowChipUi("completed", "Completed", 0),
        ),
        selectedFilter = "all",
    )

    private fun sampleFeedTransportState() = FeedTransportUiState(
        date = "2026-07-29",
        today = "2026-07-29",
        filters = FeedTransportFilterUi(
            parks = listOf(
                FeedDropdownOption("park-1", "Channapatna"),
                FeedDropdownOption("park-2", "Coimbatore"),
            ),
            selectedParkId = "park-1",
            selectedParkLabel = "Channapatna",
            sheds = listOf(
                FeedDropdownOption("shed-1", "Gandhi 1"),
                FeedDropdownOption("shed-2", "Gandhi 2"),
                FeedDropdownOption("shed-3", "Godel 1"),
            ),
        ),
        rows = listOf(
            FeedTransportRowUi(
                taskId = "task-1", parkId = "park-1", shedId = "shed-1",
                shedLabel = "Gandhi 1", parkLabel = "Channapatna",
                status = "due", reworkReason = null,
            ),
            FeedTransportRowUi(
                taskId = "task-2", parkId = "park-1", shedId = "shed-2",
                shedLabel = "Gandhi 2", parkLabel = "Channapatna",
                status = "verification_due", reworkReason = null,
            ),
            FeedTransportRowUi(
                taskId = "task-3", parkId = "park-1", shedId = "shed-3",
                shedLabel = "Godel 1", parkLabel = "Channapatna",
                status = "rework", reworkReason = "Transport path is not visible",
            ),
        ),
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
            href = "/counts/birth",
            status = NavModuleStatus.AVAILABLE,
            navItems = listOf(
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
    //
    // `modules` is REQUIRED even on a MINIMAL single-module fixture, and its absence was a live
    // defect in this file: GoatOsShellChrome derives its top-level route set from
    // navState.availableModules() (GoatOsShell.kt -> drawerTopLevelRoutes -> isTopLevelRoute).
    // With no modules that set is empty, NO route matches, the shell treats the screen as an
    // L1 drill, and it renders a BACK ARROW WITH NO BOTTOM BAR. The three goldens below were
    // recorded in exactly that state, so each one proved the opposite of what its name claims —
    // counts_operator in particular asserted "the three lifecycle icons in the bottom bar"
    // while its golden had no bottom bar at all.
    @Test
    fun role_park_manager() = shot("role_park_manager") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = vaccinationFieldNavItems(),
                modules = listOf(vaccinationModule()),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // Field operator, Vaccination lens — the Drives/Alerts/You bar over today's shed queue.
    @Test
    fun role_operator() = shot("role_operator") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = vaccinationFieldNavItems(),
                modules = listOf(vaccinationModule()),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    // Counts field lens: explicitly locks the three backend-composed lifecycle icons in the
    // bottom bar, including distinct arrival/exit glyphs for Birth and Death.
    @Test
    fun counts_operator() = shot("counts_operator") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = countsFieldNavItems(),
                modules = listOf(countsModule()),
            ),
            currentRoute = Routes.COUNTS_BIRTH,
            onNavigate = { true },
        ) {
            WorkflowListScreen(state = sampleBirthListState(), rows = sampleBirthRows())
        }
    }

    // A REAL field operator holds several modules (Vaccination, Herd Operations, Feed, Health,
    // Milk), so their chrome is EXPANDED with a module drawer — the single-module fixtures above
    // are the narrow case, not the common one. These three close that gap: the drawer itself,
    // and the two module bars an operator uses most beside Vaccination.
    @Test
    fun role_operator_drawer() = shot("role_operator_drawer") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = vaccinationFieldNavItems(),
                modules = operatorModules(),
            ),
            currentRoute = Routes.VACCINATION,
            onNavigate = { true },
            initialDrawerValue = DrawerValue.Open,
            drawerProfile = DrawerProfile(name = "Amit Kumar", role = "Operator · CPT", initials = "AK"),
        ) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    @Test
    fun operator_herd_operations() = shot("operator_herd_operations") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = countsFieldNavItems(),
                modules = operatorModules(),
            ),
            currentRoute = Routes.COUNTS_BIRTH,
            onNavigate = { true },
        ) {
            WorkflowListScreen(state = sampleBirthListState(), rows = sampleBirthRows())
        }
    }

    @Test
    fun operator_feed() = shot("operator_feed") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.EXPANDED,
                items = feedFieldNavItems(),
                modules = operatorModules(),
            ),
            currentRoute = "/feed/transport",
            onNavigate = { true },
        ) {
            FeedTransportScreen(state = sampleFeedTransportState(), onEvent = {})
        }
    }

    // Growth Director — Weighing and only Weighing, so MINIMAL chrome keeps You on the bar.
    @Test
    fun role_growth_director() = shot("role_growth_director") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = weighingDirectorNavItems(),
                modules = listOf(weighingDirectorModule()),
            ),
            currentRoute = "/weighing",
            onNavigate = { true },
        ) {
            WeighingScreen(state = sampleWeighingOperatorState())
        }
    }

    // Feed Director — the whole feed chain and nothing else.
    @Test
    fun role_feed_director() = shot("role_feed_director") {
        GoatOsShellChrome(
            navState = NavState(
                chrome = NavChrome.MINIMAL,
                items = feedFieldNavItems(),
                modules = listOf(feedModule()),
            ),
            currentRoute = "/feed/transport",
            onNavigate = { true },
        ) {
            FeedTransportScreen(state = sampleFeedTransportState(), onEvent = {})
        }
    }
}
