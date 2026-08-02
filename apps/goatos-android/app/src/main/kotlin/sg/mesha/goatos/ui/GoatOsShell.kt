package sg.mesha.goatos.ui

import android.util.Log
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.StateFlow
import javax.inject.Inject
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.nav.LocalDrawerOpener
import sg.mesha.goatos.core.designsystem.nav.LocalIsTopLevelRoot
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.feature.auth.RoleBasedPermissionGate
import sg.mesha.goatos.core.designsystem.R as DesignSystemR
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavModule
import sg.mesha.goatos.core.model.nav.NavModuleStatus
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.availableModules
import sg.mesha.goatos.core.model.nav.barItems
import sg.mesha.goatos.core.model.nav.resolveModule
import sg.mesha.goatos.push.DevicePushStateViewModel
import sg.mesha.goatos.push.PushNavigationViewModel
import sg.mesha.goatos.viewmodel.ProfileViewModel
import sg.mesha.goatos.viewmodel.SyncStatusViewModel

private const val TAG_SHELL = "GoatOsShell"

/** Drawer-header identity (mock `.dp`): avatar initials + name + "role · location" sub. */
data class DrawerProfile(val name: String, val role: String, val initials: String)

/**
 * Holds WHICH backend module the drawer currently has open.
 *
 * This is the one piece of nav state the client legitimately owns (the golden frontend rule
 * gives the frontend local selected state; the backend still owns the module list, labels,
 * routes, and each module's bar). It lives in [SavedStateHandle] rather than `rememberSaveable`
 * so the open module survives BOTH a configuration change and process death — an operator who
 * gets a call mid-drive returns to the module they were working in, not the default one.
 *
 * A null key means "not yet chosen"; [NavState.resolveModule] then falls back to the route's
 * owning module, then to the backend's default module.
 */
@HiltViewModel
class ShellModuleViewModel @Inject constructor(
    private val savedState: SavedStateHandle,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    val selectedModuleKey: StateFlow<String?> = savedState.getStateFlow(KEY_SELECTED_MODULE, null)

    /**
     * Records that the OS notification prompt was shown, and what the person answered.
     *
     * Worth measuring precisely because the absence of it hid a total failure: POST_NOTIFICATIONS
     * was never requested, so FCM accepted every push, reported it delivered, and Android dropped
     * it. "Delivered" counted a notification nobody could see. A denial rate is now visible
     * instead of inferred.
     */
    fun recordNotificationPrompt() {
        analytics.track(AnalyticsEvents.NOTIFICATION_PERMISSION_PROMPTED, emptyMap())
    }

    fun recordNotificationPermissionAnswer(granted: Boolean) {
        analytics.track(
            AnalyticsEvents.NOTIFICATION_PERMISSION_RESULT,
            mapOf(AnalyticsEvents.Params.REASON to if (granted) "granted" else "denied"),
        )
    }

    /** Records the operator switching modules. No-ops when the module is already open. */
    fun select(module: NavModule) {
        if (savedState.get<String?>(KEY_SELECTED_MODULE) == module.key) return
        savedState[KEY_SELECTED_MODULE] = module.key
        analytics.track(
            AnalyticsEvents.MODULE_SWITCHED,
            mapOf(AnalyticsEvents.Params.MODULE_KEY to module.key),
        )
    }

    private companion object {
        const val KEY_SELECTED_MODULE = "shell_selected_module_key"
    }
}

/**
 * The role-aware app shell — Material 3 (Expressive) chrome on the mock's dark palette.
 * TRD §14 dumb-renderer: renders the backend-computed [NavState] and never counts
 * modules or checks role.
 *  - EXPANDED chrome → a module-switcher drawer listing [NavState.modules] (backend-composed
 *    from the person's module grants); MINIMAL → bottom bar only.
 *  - The OPEN module's [NavModule.navItems] → M3 [NavigationBar] destinations, each with its
 *    mock icon and the M3 active-indicator pill; a trailing global "You" tab is always present.
 *    Switching modules in the drawer swaps the bar.
 */
@Composable
fun GoatOsShell(navState: NavState) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()

    // Cold-start / pre-auth notification-tap deep-link (docs: FCM push slice). A tap can arrive
    // before this NavHost even exists (MainActivity writes into PendingNavigation as soon as the
    // intent is read, well before sign-in + bootstrap resolve) — this is the first point a real
    // NavHostController exists to consume it. Fires exactly once: PendingNavigation.consume()
    // atomically nulls the held route, so a later recomposition of this same LaunchedEffect
    // (e.g. a config change) never re-navigates to the same tap twice.
    val pushNavVm: PushNavigationViewModel = hiltViewModel()
    val pendingPushRoute by pushNavVm.pendingRoute.collectAsStateWithLifecycle()
    // Set when a tapped alert points at something this person cannot open, so the screen can say
    // so in one line instead of leaving them on a blank or unexpected page.
    var showUnavailableAlertNotice by remember { mutableStateOf(false) }

    // Re-reports this device's push state (token + whether the phone will actually show what we
    // send) when the alerts gate below sees access come back on.
    val pushStateVm: DevicePushStateViewModel = hiltViewModel()

    // Shell-level connectivity/outbox status. Feeds the passive offline banner (debounced) and
    // the on-demand sync sheet — both read the one live SyncRepository flow, no polling.
    val syncVm: SyncStatusViewModel = hiltViewModel()
    val showOffline by syncVm.showOfflineBanner.collectAsStateWithLifecycle()
    val syncStatus by syncVm.status.collectAsStateWithLifecycle()
    var showSyncSheet by remember { mutableStateOf(false) }

    // Drawer identity + settings actions (mock `ovl-drawer`). ProfileViewModel already resolves
    // name/role/initials from the bootstrap cache and owns sign-out + language persistence.
    val profileVm: ProfileViewModel = hiltViewModel()
    val profile by profileVm.state.collectAsStateWithLifecycle()
    var showLanguage by remember { mutableStateOf(false) }

    // Bottom-nav roots are true role roots, not "return me to whatever child screen was last
    // under this tab" shortcuts. We used to save/restore tab state here; after camera/permission
    // interruptions that could resurrect a hosted child route as the operator landing page, which
    // put a Back affordance on a root and hid the bottom bar. Selecting a backend-composed L0 item
    // now clears any stale child stack and lands on the exact href the backend granted.
    //
    // Navigation is BACKEND-COMPOSED and CACHED OFFLINE, but the nav graph is fixed at build time,
    // so the two can legitimately disagree: a cached bar that predates a route rename, or a backend
    // newer than the installed APK, both hand us an href this build does not host. Passing that
    // straight to navController.navigate() throws IllegalArgumentException and kills the app — it
    // shipped twice (an old RFID-list route, then /counts/birth-death after the birth/death split). An
    // unknown href is a stale-contract condition to survive, never a crash: skip it and let the next
    // successful bootstrap refresh heal the cache.
    // Returns whether the href was actually hosted, so a caller holding several candidates (the
    // drawer) can fall through to the next one instead of leaving the tap dead.
    val navigate: (String) -> Boolean = { href ->
        if (navController.graph.findNode(href) == null) {
            Log.w(TAG_SHELL, "nav_href_not_hosted route=$href — stale nav cache or newer backend")
            false
        } else if (navController.popBackStack(href, inclusive = false)) {
            // The tapped root is ALREADY on the back stack (the common case: leaving a sibling tab
            // and coming back). Pop back to it instead of navigating onto it.
            //
            // navigate() with popUpTo(start) reuses that same entry via launchSingleTop, but leaves
            // its lifecycle parked below STARTED, so every collectAsStateWithLifecycle on the screen
            // stops collecting while the last rendered frame stays on screen. The screen then looks
            // alive -- taps still run click handlers, so a row CTA still navigates -- but nothing
            // driven by ViewModel state updates again. That is what made the weighing park chips go
            // dead after a trip through another tab: the chip callback fired and set the selection,
            // and the list never re-rendered.
            //
            // popBackStack resumes the existing entry, and still clears any child routes above it,
            // so the roots-are-roots behaviour documented above is unchanged.
            true
        } else {
            navController.navigate(href) {
                popUpTo(navController.graph.findStartDestination().id) { saveState = false }
                launchSingleTop = true
                restoreState = false
            }
            true
        }
    }

    // Which module the drawer has open — local UI selection, persisted across config change
    // and process death (see ShellModuleViewModel).
    val moduleVm: ShellModuleViewModel = hiltViewModel()
    val selectedModuleKey by moduleVm.selectedModuleKey.collectAsStateWithLifecycle()
    val visibleNavState = navState
    val canExecuteVaccination = visibleNavState.featureFlags["vaccination_execute"] == true
    // Backend-owned, never inferred. `weighing_execute` is compiled by the backend from
    // the real grant + module truth (workforce/app/bootstrap_copy.go canExecuteWeighing:
    // WeighingExecute permission AND the weighing module granted).
    //
    // This used to fall back to parsing the DISPLAY label -- roleLabel.substringBefore("·")
    // == "operator" -- which is exactly the role-name-string inference AGENTS.md bans: it
    // makes an authorization decision out of copy that exists to be shown to a human, so a
    // label tweak or a translation silently grants or revokes execution.
    val canExecuteWeighing = visibleNavState.featureFlags["weighing_execute"] == true
    val verificationVideoControlsEnabled = visibleNavState.featureFlags["verification_video_controls"] == true

    // Cold-start / pre-auth notification-tap deep-link. A tap can arrive before this NavHost even
    // exists (MainActivity writes into PendingNavigation as soon as the intent is read, well before
    // sign-in + bootstrap resolve), so this is the first point a real NavHostController AND the
    // person's own navigation both exist. Fires exactly once: PendingNavigation.consume() atomically
    // nulls the held route, so a later recomposition (e.g. a config change) never re-navigates to
    // the same tap twice.
    //
    // Two ways a tap can point nowhere usable, and NEITHER may leave a blank screen: the route is
    // not hosted by this build (older APK, retired link), or it is a module landing this person was
    // not granted (an alert forwarded to the wrong recipient, or access changed since it was sent).
    // Both land on this person's OWN home screen with a one-line notice.
    LaunchedEffect(pendingPushRoute, visibleNavState) {
        val route = pendingPushRoute ?: return@LaunchedEffect
        // Hold the tap until this person's own navigation has resolved. Judging it against an
        // empty pre-bootstrap state would reject every alert on a cold start.
        if (visibleNavState.items.isEmpty() && visibleNavState.modules.isEmpty()) return@LaunchedEffect
        val hosted = navController.graph.findNode(route) != null
        val permitted = !isRootDestination(route) || visibleNavState.grantsRootDestination(route)
        when {
            !hosted -> {
                Log.w(TAG_SHELL, "push_route_not_hosted route=$route — landing on the default screen")
                showUnavailableAlertNotice = true
                navigate(startDestinationFor(visibleNavState))
            }
            !permitted -> {
                Log.w(TAG_SHELL, "push_route_not_granted route=$route — landing on the default screen")
                showUnavailableAlertNotice = true
                navigate(startDestinationFor(visibleNavState))
            }
            else -> navController.navigate(route) { launchSingleTop = true }
        }
        pushNavVm.consume()
    }

    LaunchedEffect(visibleNavState, selectedModuleKey, backStackEntry?.destination?.route) {
        val selected = visibleNavState.availableModules().firstOrNull { it.key == selectedModuleKey }
        val currentBaseRoute = backStackEntry?.destination?.route?.routeBase()
        val allTopLevelRoutes = visibleNavState.availableModules().flatMap { module -> module.navItems.map { it.href } }
        if (
            selected != null &&
            selected.href.isNotBlank() &&
            currentBaseRoute in allTopLevelRoutes &&
            currentBaseRoute !in selected.navItems.map { it.href }
        ) {
            navigate(selected.href)
        }
    }

    GoatOsShellChrome(
        navState = visibleNavState,
        currentRoute = backStackEntry?.destination?.route,
        onNavigate = navigate,
        drawerProfile = DrawerProfile(profile.name, profile.roleLabel, profile.initials),
        languageLabel = languageLabel(AppLocaleState.tag),
        onOpenLanguage = { showLanguage = true },
        onSignOut = profileVm::signOut,
        selectedModuleKey = selectedModuleKey,
        onSelectModule = moduleVm::select,
    ) {
        // Pinned above screen content on every route; non-blocking, auto-hides on reconnect.
        OfflineBanner(visible = showOffline, onOpenDetails = { showSyncSheet = true })

        // Mandatory role-based permission gate — NON-DISMISSIBLE dialog shown after bootstrap.
        // Blocks the app until all required permissions (based on role) are granted.
        //
        // Operators require: camera (proof capture), BLE (RFID reader), notifications (alerts)
        // All other roles require: notifications (alerts) only
        //
        // Derives requirements from the backend-composed module list (featureFlags indicate
        // vaccination_execute, weighing_execute, etc.) rather than hardcoding role strings,
        // following the nav-composition guard pattern (AGENTS.md: do NOT hardcode per-role
        // arrays). If OS stops showing prompts ("Don't ask again"), redirects to app settings.
        // Re-checks on resume and auto-dismisses when all required permissions are granted.
        RoleBasedPermissionGate(
            navState = visibleNavState,
            onAllPermissionsGranted = { pushStateVm.reportNow() },
            onPermissionPrompted = moduleVm::recordNotificationPrompt,
            onPermissionAnswered = { _, granted ->
                if (granted) moduleVm.recordNotificationPermissionAnswer(true)
            },
        )
        UnavailableAlertNotice(
            visible = showUnavailableAlertNotice,
            onDismiss = { showUnavailableAlertNotice = false },
        )
        val navState = visibleNavState
        AppNavHost(
            navController = navController,
            startDestination = startDestinationFor(navState),
            showProtocolAdherenceCard = navState.featureFlags["protocol_adherence_card"] == true,
            canExecuteVaccination = canExecuteVaccination,
            canExecuteWeighing = canExecuteWeighing,
            verificationVideoControlsEnabled = verificationVideoControlsEnabled,
        )
    }

    if (showSyncSheet) {
        SyncSheet(
            isOnline = syncStatus.online,
            syncingCount = syncStatus.inFlightCount,
            queuedCount = syncStatus.pendingCount,
            failedCount = syncStatus.failedCount,
            queue = syncStatus.items,
            onRetryAll = syncVm::retryAll,
            onDismiss = { showSyncSheet = false },
        )
    }
    if (showLanguage) {
        LanguageSheet(
            current = AppLocaleState.tag,
            onSelect = { code -> profileVm.setLanguage(code); showLanguage = false },
            onDismiss = { showLanguage = false },
        )
    }
}

/**
 * The shell's chrome (drawer/module-switcher + bottom bar + Scaffold) around an arbitrary
 * [content] slot, factored out of [GoatOsShell] so it can be driven by a static [NavState]
 * fixture and a static landing composable — no [androidx.navigation.NavHostController] or Hilt
 * ViewModel required. [GoatOsShell] is the real app entry (content = [AppNavHost]);
 * `RoleChromeScreenshotTest` is the other caller (content = one role's landing screen), so every
 * role's nav chrome renders through the exact same code path a device would use. Drawer identity
 * + settings actions flow in as params (defaulted) so the chrome stays Hilt-free for that test.
 */
@Composable
fun GoatOsShellChrome(
    navState: NavState,
    currentRoute: String?,
    onNavigate: (String) -> Boolean,
    initialDrawerValue: DrawerValue = DrawerValue.Closed,
    drawerProfile: DrawerProfile? = null,
    languageLabel: String = "English",
    onOpenLanguage: () -> Unit = {},
    onSignOut: () -> Unit = {},
    selectedModuleKey: String? = null,
    onSelectModule: (NavModule) -> Unit = {},
    content: @Composable () -> Unit,
) {
    val hasDrawer = navState.chrome == NavChrome.EXPANDED
    val drawerState = rememberDrawerState(initialDrawerValue)
    val scope = rememberCoroutineScope()

    // The bottom bar is MODULE-SCOPED: it shows the OPEN module's own destinations, so
    // switching modules in the drawer swaps the bar. Resolution lives in the shared nav
    // contract (NavState.resolveModule) — the shell holds no module list of its own and
    // falls back to visible_navigation when the payload carries no modules.
    val activeModule = navState.resolveModule(selectedModuleKey, currentRoute)
    val barItems = navState.barItems(selectedModuleKey, currentRoute)

    // L0 roots: exactly the OPEN module's backend-composed destinations, and nothing else.
    //
    // The You tab used to be appended here as client-static chrome. It is now a nav
    // contribution like any other (`bootstrap_copy.go` -> vaccination contributes
    // `you`; Counts contributes `approval` in that trailing slot instead), so the L0 set is
    // whatever the backend composed — no client-side addition. That is what let the Counts
    // module replace the trailing tab without a client release.
    //
    // Exact membership only — a drill (L1+) must never inherit root chrome, so no
    // prefix/substring matching here. See docs/decisions/android-navigation-stack.md.
    val topLevelRoutes = barItems.map { it.href }
    val drawerTopLevelRoutes = navState.availableModules().flatMap { module -> module.navItems.map { it.href } }
    val topLevelRouteKey = topLevelRoutes.joinToString(separator = "\u001F")
    val isTopLevel = isTopLevelRoute(currentRoute, drawerTopLevelRoutes)

    // A process/activity restore can resurrect ModalNavigationDrawer in an open or partially
    // offset state while the sheet is not actually visible yet. On real phones that makes the
    // screen look blank because the page content is translated almost entirely off the right
    // edge. Shell entry and route changes must therefore start from a closed drawer; users can
    // still open it explicitly from the L0 hamburger after the route has settled.
    LaunchedEffect(navState.chrome, currentRoute, topLevelRouteKey, initialDrawerValue) {
        if (
            initialDrawerValue == DrawerValue.Closed &&
            (drawerState.currentValue != DrawerValue.Closed || drawerState.targetValue != DrawerValue.Closed)
        ) {
            drawerState.close()
        }
    }

    ModalNavigationDrawer(
        drawerState = drawerState,
        gesturesEnabled = hasDrawer && isTopLevel,
        drawerContent = {
            if (hasDrawer) {
                ModuleDrawer(
                    modules = navState.modules,
                    selectedModuleKey = activeModule?.key,
                    profile = drawerProfile,
                    onSelectModule = { module ->
                        scope.launch { drawerState.close() }
                        onSelectModule(module)
                        // Selecting a module opens its landing route; the bar swaps with it. A cached
                        // landing href can predate a route rename, so fall back to the module's own
                        // tabs rather than leaving the tap dead (onNavigate itself refuses any href
                        // this build does not host).
                        (listOf(module.href) + module.navItems.map { it.href })
                            .filter { it.isNotBlank() }
                            .firstOrNull { onNavigate(it) }
                    },
                    onOpenLanguage = {
                        scope.launch { drawerState.close() }
                        onOpenLanguage()
                    },
                    onOpenAccount = {
                        scope.launch { drawerState.close() }
                        onNavigate("/you")
                    },
                    onSignOut = {
                        scope.launch { drawerState.close() }
                        onSignOut()
                    },
                )
            }
        },
    ) {
        Scaffold(
            containerColor = MaterialTheme.colorScheme.background,
            bottomBar = {
                if (isTopLevel) {
                    MeshaNavBar(
                        items = barItems,
                        currentRoute = currentRoute,
                        // A bar tab has no fallback candidate; the guard inside onNavigate is what
                        // keeps a stale cached tab from crashing the app.
                        onSelect = { onNavigate(it) },
                    )
                }
            },
        ) { padding ->
            // THE drawer-access decision, made once for the whole app.
            //
            // Every destination's header (MeshaScreenHeader) derives its leading affordance
            // from this one value, so drawer access is a property of the backend-composed nav
            // — NOT something each screen opts into. It used to be opt-in, and every module
            // that forgot to read this local shipped with no way back to another module.
            //
            // Non-null on exactly the L0 roots of the open module (+ the global You tab) when
            // the person has more than one module; null on every L1+ drill, so a hosted child
            // can never grow root chrome. Same exact-membership test the bottom bar uses.
            val drawerOpener: (() -> Unit)? =
                if (hasDrawer && isTopLevel) { // == drawerAvailable(navState.chrome, currentRoute, topLevelRoutes)
                    { scope.launch { drawerState.open() } }
                } else {
                    null
                }
            CompositionLocalProvider(
                LocalDrawerOpener provides drawerOpener,
                LocalIsTopLevelRoot provides isTopLevel,
            ) {
                Column(
                    modifier = Modifier
                        .padding(padding)
                        // Scaffold has already converted its system-bar insets into this
                        // padding. Mark them consumed so child routes that are also safe when
                        // rendered standalone do not apply the status/navigation bars again.
                        .consumeWindowInsets(padding)
                        .fillMaxSize(),
                ) {
                    content()
                }
            }
        }
    }
}

/**
 * Only bootstrap roots own global navigation chrome. A destination must match one of the exact
 * backend-composed root hrefs to receive the bottom bar/drawer; child routes and route patterns do
 * not inherit root chrome from a similar path prefix.
 */
internal fun isTopLevelRoute(currentRoute: String?, topLevelRoutes: Collection<String>): Boolean =
    // Normalise BOTH sides. A backend root href may carry a scoping query arg — the verifier's
    // per-feature drawer entries are "/verify?module=weighing" — and comparing a stripped current
    // route against an unstripped href silently dropped the drawer and bottom bar. Membership is
    // still EXACT on the path; only the query is ignored, so a real drill still gets no chrome.
    currentRoute?.routeBase() in topLevelRoutes.map { it.routeBase() }

/**
 * Whether the destination on screen offers the module drawer — the single rule behind every
 * screen's leading header affordance ([MeshaScreenHeader] renders a hamburger exactly when this
 * is true, and Up otherwise).
 *
 * True only for an exact L0 root of the open module while the person holds more than one module
 * (EXPANDED chrome). Deliberately derived, never authored: drawer access follows the
 * backend-composed nav, so a new module gets it with no client change and no screen can opt out.
 */
internal fun drawerAvailable(
    chrome: NavChrome,
    currentRoute: String?,
    topLevelRoutes: Collection<String>,
): Boolean = chrome == NavChrome.EXPANDED && isTopLevelRoute(currentRoute, topLevelRoutes)

// ---------------------------------------------------------------------------
// Bottom navigation — M3 NavigationBar with the active-indicator pill. Icons are
// the mock's stroked set; colours come from the themed (mock-palette) scheme.
// ---------------------------------------------------------------------------

/**
 * The bottom bar is the backend-composed item list, rendered VERBATIM.
 *
 * There is deliberately no client-appended tab. A hardcoded trailing "You" item used to be
 * added here unconditionally, which violated the golden frontend rule (backend owns visible
 * navigation) and made the bar unchangeable from the backend: the Counts module could not put
 * its Approval queue in that slot without an app release. "You" is now a nav contribution the
 * vaccination and leadership modules declare, and Counts contributes Approval in its place —
 * so which tabs exist, in which order, is entirely a `bootstrap_copy.go` decision.
 */
@Composable
private fun MeshaNavBar(
    items: List<NavItem>,
    currentRoute: String?,
    onSelect: (String) -> Unit,
) {
    val itemColors = NavigationBarItemDefaults.colors(
        selectedIconColor = MaterialTheme.colorScheme.onPrimaryContainer,
        selectedTextColor = MaterialTheme.colorScheme.onSurface,
        indicatorColor = MaterialTheme.colorScheme.primaryContainer,
        unselectedIconColor = MaterialTheme.colorScheme.onSurfaceVariant,
        unselectedTextColor = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    NavigationBar(
        containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
        tonalElevation = 0.dp,
    ) {
        val currentBaseRoute = currentRoute?.routeBase()
        // Backend-composed, MODULE-SCOPED destinations. Labels render verbatim: bootstrap_copy.go
        // already localizes them (en/hi/kn/te), so re-translating client-side would both violate
        // the golden frontend rule and actively mislabel items (the backend calls the vaccination
        // module's own tab "Drives", not "Vaccination").
        items.forEach { item ->
            val isSelected = currentBaseRoute == item.href
            NavigationBarItem(
                selected = isSelected,
                onClick = {
                    if (!isSelected) onSelect(item.href)
                },
                icon = {
                    Icon(
                        imageVector = MeshaIcons.forNavKey(item.key),
                        contentDescription = item.label,
                        modifier = Modifier.size(24.dp),
                    )
                },
                // design-system:ignore: weight-only override on the M3 NavigationBarItem label
                // (no fontSize to pair with); applying a full MeshaType style would also change
                // the bar label's size away from the M3 default.
                label = { Text(item.label, fontWeight = FontWeight.SemiBold) },
                colors = itemColors,
            )
        }
    }
}

private fun String.routeBase(): String = substringBefore('?')

// ---------------------------------------------------------------------------
// Module-switcher drawer — ports the mock's `ovl-drawer` (`mock/vaccination-mobile-
// mock.html`): a profile header, a scrollable MODULES group (the open module carries a
// check + green rail, not-yet-built = a "Soon" badge), then a Sign-out footer.
//
// Fully backend-composed: EVERY row comes from [NavState.modules] (the bootstrap's
// `modules` array, built from the person's department_module_grants via
// bootstrap_copy.go's moduleNavRegistry). There is no client-side module list, no
// role check, and no per-module template — adding a module is a backend registry
// entry, not an app release. Labels are already localized backend-side and are
// rendered verbatim. The client owns only layout: group header, "Soon" badge, and
// icon token, none of which name a specific vertical.
// ---------------------------------------------------------------------------

@Composable
private fun ModuleDrawer(
    modules: List<NavModule>,
    selectedModuleKey: String?,
    profile: DrawerProfile?,
    onSelectModule: (NavModule) -> Unit,
    onOpenLanguage: () -> Unit,
    onOpenAccount: () -> Unit,
    onSignOut: () -> Unit,
) {
    ModalDrawerSheet(
        drawerContainerColor = MeshaColors.Surf,
        drawerShape = RectangleShape,
        modifier = Modifier.width(288.dp).fillMaxHeight(),
    ) {
        Column(Modifier.fillMaxSize()) {
            DrawerHeader(profile)
            Column(
                Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState()),
            ) {
                DrawerGroupLabel(stringResource(DesignSystemR.string.nav_modules))
                val visibleModules = if (profile?.role.equals("operator", ignoreCase = true)) {
                    modules.filter { it.status == NavModuleStatus.AVAILABLE }
                } else {
                    modules
                }
                visibleModules.forEach { module ->
                    if (module.status == NavModuleStatus.AVAILABLE) {
                        val active = module.key == selectedModuleKey
                        DrawerRow(
                            icon = MeshaIcons.forNavKey(module.key),
                            label = module.label,
                            active = active,
                            trailing = if (active) ({ DrawerCheck() }) else null,
                            onClick = { onSelectModule(module) },
                        )
                    } else {
                        // Declared roadmap: rendered, but not clickable — no onClick at all,
                        // so there is nothing to tap into an unbuilt vertical.
                        DrawerSoonRow(module)
                    }
                }
            }
            DrawerFooter(onOpenAccount = onOpenAccount, onSignOut = onSignOut)
        }
    }
}

@Composable
private fun DrawerHeader(profile: DrawerProfile?) {
    Row(
        Modifier
            .fillMaxWidth()
            .background(MeshaColors.Surf2)
            .padding(start = 18.dp, end = 18.dp, top = 22.dp, bottom = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier.size(46.dp).clip(RoundedCornerShape(15.dp)).background(MeshaColors.Brand),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = profile?.initials?.ifBlank { "M" } ?: "M",
                color = MeshaColors.OnBrand,
                // design-system:ignore: 18sp/W800 avatar initials — nearest token (button 15sp/W800)
                // is 3sp smaller, which would visibly shrink the drawer avatar glyph.
                fontSize = 18.sp,
                fontWeight = FontWeight.W800,
            )
        }
        Column(Modifier.weight(1f)) {
            Text(
                profile?.name?.ifBlank { "Mesha" } ?: "Mesha",
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
            )
            val sub = profile?.role?.takeIf { it.isNotBlank() }
            if (sub != null) {
                // design-system:ignore: 11.5sp at default W400; the only 11.5sp token (caption)
                // is W600, which would bolden this sub-label.
                Text(sub, color = MeshaColors.Muted, fontSize = 11.5.sp)
            }
        }
    }
    Box(Modifier.fillMaxWidth().height(1.dp).background(MeshaColors.Hair))
}

@Composable
private fun DrawerGroupLabel(text: String) {
    Text(
        text.uppercase(),
        color = MeshaColors.Faint,
        style = MeshaType.overline,
        modifier = Modifier.padding(start = 18.dp, end = 18.dp, top = 16.dp, bottom = 6.dp),
    )
}

@Composable
private fun DrawerRow(
    icon: ImageVector,
    label: String,
    active: Boolean = false,
    trailing: (@Composable () -> Unit)? = null,
    onClick: () -> Unit,
) {
    val fg = if (active) MeshaColors.BrandD else MeshaColors.Ink
    val iconTint = if (active) MeshaColors.Brand else MeshaColors.Muted
    Row(
        Modifier
            .fillMaxWidth()
            .then(if (active) Modifier.background(MeshaColors.Brand.copy(alpha = 0.10f)) else Modifier)
            .clickable(onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(13.dp),
    ) {
        // Mock `.di.on` inset 3px brand rail.
        if (active) {
            Box(Modifier.width(3.dp).height(20.dp).clip(RoundedCornerShape(2.dp)).background(MeshaColors.Brand))
        }
        Icon(icon, contentDescription = label, tint = iconTint, modifier = Modifier.size(20.dp))
        // design-system:ignore: 14.5sp/W600 — the 14.5sp tokens are body (W400) and bodyStrong
        // (W700); neither carries W600, so either would change this drawer row's weight.
        Text(label, color = fg, fontSize = 14.5.sp, fontWeight = FontWeight.W600, modifier = Modifier.weight(1f))
        trailing?.invoke()
    }
}

/** A declared-but-unbuilt module: disabled row + "Soon" badge. Label is backend copy. */
@Composable
private fun DrawerSoonRow(module: NavModule) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 18.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(13.dp),
    ) {
        Icon(
            MeshaIcons.forNavKey(module.key),
            contentDescription = module.label,
            tint = MeshaColors.Faint,
            modifier = Modifier.size(20.dp),
        )
        // design-system:ignore: 14.5sp/W600 — no W600 token at 14.5sp (body=W400, bodyStrong=W700).
        Text(module.label, color = MeshaColors.Faint, fontSize = 14.5.sp, fontWeight = FontWeight.W600, modifier = Modifier.weight(1f))
        DrawerBadge(stringResource(DesignSystemR.string.nav_soon), brand = false)
    }
}

@Composable
private fun DrawerCheck() {
    Icon(MeshaIcons.Check, contentDescription = "Active", tint = MeshaColors.Brand, modifier = Modifier.size(15.dp))
}

@Composable
private fun DrawerBadge(text: String, brand: Boolean) {
    Text(
        text,
        color = if (brand) MeshaColors.BrandD else MeshaColors.Muted,
        style = MeshaType.dayName,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (brand) MeshaColors.Brand.copy(alpha = 0.16f) else MeshaColors.Surf3)
            .padding(horizontal = 9.dp, vertical = 3.dp),
    )
}

@Composable
private fun DrawerFooter(onOpenAccount: () -> Unit, onSignOut: () -> Unit) {
    Box(Modifier.fillMaxWidth().height(1.dp).background(MeshaColors.Hair))
    // The account sits at the FOOT of the drawer, beside Sign out, because it belongs to the person
    // rather than to Weighing or Vaccination. A principal with the drawer therefore reaches it once
    // here instead of carrying a You tab in every module's bottom bar. A principal without a drawer
    // keeps You on the bar -- that is their only route to it.
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onOpenAccount)
            .padding(start = 18.dp, end = 18.dp, top = 12.dp, bottom = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(13.dp),
    ) {
        Icon(MeshaIcons.forNavKey("you"), contentDescription = null, tint = MeshaColors.Ink, modifier = Modifier.size(20.dp))
        // design-system:ignore: 14.5sp/W600 — no W600 token at 14.5sp (body=W400, bodyStrong=W700).
        Text(stringResource(DesignSystemR.string.nav_you), color = MeshaColors.Ink, fontSize = 14.5.sp, fontWeight = FontWeight.W600)
    }
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onSignOut)
            .padding(start = 18.dp, end = 18.dp, top = 12.dp, bottom = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(13.dp),
    ) {
        Icon(MeshaIcons.Logout, contentDescription = stringResource(DesignSystemR.string.nav_sign_out), tint = MeshaColors.Danger, modifier = Modifier.size(20.dp))
        // design-system:ignore: 14.5sp/W600 — no W600 token at 14.5sp (body=W400, bodyStrong=W700).
        Text(stringResource(DesignSystemR.string.nav_sign_out), color = MeshaColors.Danger, fontSize = 14.5.sp, fontWeight = FontWeight.W600)
    }
}

private fun languageLabel(tag: String): String = when (tag.lowercase()) {
    "hi" -> "हिंदी"
    "kn" -> "ಕನ್ನಡ"
    "te" -> "తెలుగు"
    else -> "English"
}
