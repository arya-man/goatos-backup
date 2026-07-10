package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.OwnedModule
import sg.mesha.goatos.viewmodel.ProfileViewModel
import sg.mesha.goatos.viewmodel.SyncStatusViewModel

/** Drawer-header identity (mock `.dp`): avatar initials + name + "role · location" sub. */
data class DrawerProfile(val name: String, val role: String, val initials: String)

/**
 * The role-aware app shell — Material 3 (Expressive) chrome on the mock's dark palette.
 * TRD §14 dumb-renderer: renders the backend-computed [NavState] and never counts
 * modules or checks role.
 *  - EXPANDED chrome → a module-switcher drawer (>=2 owned modules); MINIMAL → bottom
 *    bar only.
 *  - [NavState.items] → M3 [NavigationBar] destinations, each with its mock icon and the
 *    M3 active-indicator pill; a trailing "You" tab is always present.
 */
@Composable
fun GoatOsShell(navState: NavState) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()

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

    val navigate: (String) -> Unit = { href ->
        navController.navigate(href) { launchSingleTop = true; restoreState = true }
    }

    GoatOsShellChrome(
        navState = navState,
        currentRoute = backStackEntry?.destination?.route,
        onNavigate = navigate,
        drawerProfile = DrawerProfile(profile.name, profile.roleLabel, profile.initials),
        languageLabel = languageLabel(AppLocaleState.tag),
        onOpenLanguage = { showLanguage = true },
        onSignOut = profileVm::signOut,
    ) {
        // Pinned above screen content on every route; non-blocking, auto-hides on reconnect.
        OfflineBanner(visible = showOffline, onOpenDetails = { showSyncSheet = true })
        AppNavHost(navController = navController)
    }

    if (showSyncSheet) {
        SyncSheet(
            isOnline = syncStatus.online,
            syncingCount = syncStatus.inFlightCount,
            queuedCount = syncStatus.pendingCount,
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
    onNavigate: (String) -> Unit,
    initialDrawerValue: DrawerValue = DrawerValue.Closed,
    drawerProfile: DrawerProfile? = null,
    languageLabel: String = "English",
    onOpenLanguage: () -> Unit = {},
    onSignOut: () -> Unit = {},
    content: @Composable () -> Unit,
) {
    val hasDrawer = navState.chrome == NavChrome.EXPANDED
    val drawerState = rememberDrawerState(initialDrawerValue)
    val scope = rememberCoroutineScope()

    ModalNavigationDrawer(
        drawerState = drawerState,
        gesturesEnabled = hasDrawer,
        drawerContent = {
            if (hasDrawer) {
                ModuleDrawer(
                    navState = navState,
                    currentRoute = currentRoute,
                    profile = drawerProfile,
                    languageLabel = languageLabel,
                    onSelect = { href ->
                        scope.launch { drawerState.close() }
                        onNavigate(href)
                    },
                    onOpenLanguage = {
                        scope.launch { drawerState.close() }
                        onOpenLanguage()
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
                MeshaNavBar(
                    items = navState.items,
                    currentRoute = currentRoute,
                    onSelect = onNavigate,
                    onYou = { onNavigate(Routes.YOU) },
                )
            },
        ) { padding ->
            Column(modifier = Modifier.padding(padding).fillMaxSize()) {
                if (hasDrawer) {
                    ShellTopBar(onMenu = { scope.launch { drawerState.open() } })
                }
                content()
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Bottom navigation — M3 NavigationBar with the active-indicator pill. Icons are
// the mock's stroked set; colours come from the themed (mock-palette) scheme.
// ---------------------------------------------------------------------------

@Composable
private fun MeshaNavBar(
    items: List<NavItem>,
    currentRoute: String?,
    onSelect: (String) -> Unit,
    onYou: () -> Unit,
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
        items.forEach { item ->
            NavigationBarItem(
                selected = currentRoute == item.href,
                onClick = { onSelect(item.href) },
                icon = {
                    Icon(
                        imageVector = MeshaIcons.forNavKey(item.key),
                        contentDescription = item.label,
                        modifier = Modifier.size(24.dp),
                    )
                },
                label = { Text(item.label, fontWeight = FontWeight.SemiBold) },
                colors = itemColors,
            )
        }
        NavigationBarItem(
            selected = currentRoute == Routes.YOU,
            onClick = onYou,
            icon = { Icon(MeshaIcons.User, contentDescription = "You", modifier = Modifier.size(24.dp)) },
            label = { Text("You", fontWeight = FontWeight.SemiBold) },
            colors = itemColors,
        )
    }
}

@Composable
private fun ShellTopBar(onMenu: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = MeshaIcons.Menu,
            contentDescription = "Menu",
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier
                .size(38.dp)
                .clip(RoundedCornerShape(12.dp))
                .clickable(onClick = onMenu)
                .padding(8.dp),
        )
        Spacer(Modifier.size(10.dp))
        Text("Mesha", fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.onBackground)
    }
}

// ---------------------------------------------------------------------------
// Module-switcher drawer — ports the mock's `ovl-drawer` (`mock/vaccination-mobile-
// mock.html`): a profile header, a scrollable MODULES group (owned = active with a
// check + green rail, not-yet-built = a "Soon" badge) and SETTINGS group, then a
// Sign-out footer. Backend-driven: the owned rows come straight from
// [NavState.ownedModules]; only the coming-soon rows are fixed product roadmap.
// ---------------------------------------------------------------------------

/** Not-yet-built verticals the mock lists as "Soon" (honest: they are NOT shipped). */
private data class SoonModule(val key: String, val label: String, val icon: ImageVector)
private val SOON_MODULES = listOf(
    SoonModule("feed_direction", "Feed direction", MeshaIcons.Feed),
    SoonModule("breeding", "Breeding", MeshaIcons.Goat),
)

@Composable
private fun ModuleDrawer(
    navState: NavState,
    currentRoute: String?,
    profile: DrawerProfile?,
    languageLabel: String,
    onSelect: (String) -> Unit,
    onOpenLanguage: () -> Unit,
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
                DrawerGroupLabel("Modules")
                val owned = navState.ownedModules
                owned.forEach { module ->
                    val active = moduleRoute(module) == currentRoute
                    DrawerRow(
                        icon = MeshaIcons.forNavKey(module.module),
                        label = moduleLabel(module),
                        active = active,
                        // Mock `.di.on`: the active module carries the check; others are plain.
                        trailing = if (active) ({ DrawerCheck() }) else null,
                        onClick = { onSelect(moduleRoute(module)) },
                    )
                }
                SOON_MODULES
                    .filterNot { soon -> owned.any { it.module.equals(soon.key, ignoreCase = true) } }
                    .forEach { soon -> DrawerSoonRow(soon) }

                DrawerGroupLabel("Settings")
                DrawerRow(
                    icon = MeshaIcons.Globe,
                    label = "Language",
                    trailing = { DrawerBadge(languageLabel, brand = true) },
                    onClick = onOpenLanguage,
                )
                DrawerRow(icon = MeshaIcons.Bluetooth, label = "RFID reader", onClick = { onSelect(Routes.RFID) })
                DrawerRow(icon = MeshaIcons.Bell, label = "Notifications", onClick = { onSelect(Routes.ALERTS) })
            }
            DrawerFooter(onSignOut)
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
                color = Color(0xFF08130B),
                fontSize = 18.sp,
                fontWeight = FontWeight.W800,
            )
        }
        Column(Modifier.weight(1f)) {
            Text(
                profile?.name?.ifBlank { "Mesha" } ?: "Mesha",
                color = MeshaColors.Ink,
                fontSize = 15.5.sp,
                fontWeight = FontWeight.W700,
            )
            val sub = profile?.role?.takeIf { it.isNotBlank() }
            if (sub != null) {
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
        fontSize = 10.5.sp,
        fontWeight = FontWeight.W700,
        letterSpacing = 0.6.sp,
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
        Text(label, color = fg, fontSize = 14.5.sp, fontWeight = FontWeight.W600, modifier = Modifier.weight(1f))
        trailing?.invoke()
    }
}

@Composable
private fun DrawerSoonRow(soon: SoonModule) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 18.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(13.dp),
    ) {
        Icon(soon.icon, contentDescription = soon.label, tint = MeshaColors.Faint, modifier = Modifier.size(20.dp))
        Text(soon.label, color = MeshaColors.Faint, fontSize = 14.5.sp, fontWeight = FontWeight.W600, modifier = Modifier.weight(1f))
        DrawerBadge("Soon", brand = false)
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
        fontSize = 9.5.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (brand) MeshaColors.Brand.copy(alpha = 0.16f) else MeshaColors.Surf3)
            .padding(horizontal = 9.dp, vertical = 3.dp),
    )
}

@Composable
private fun DrawerFooter(onSignOut: () -> Unit) {
    Box(Modifier.fillMaxWidth().height(1.dp).background(MeshaColors.Hair))
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onSignOut)
            .padding(start = 18.dp, end = 18.dp, top = 12.dp, bottom = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(13.dp),
    ) {
        Icon(MeshaIcons.Logout, contentDescription = "Sign out", tint = MeshaColors.Danger, modifier = Modifier.size(20.dp))
        Text("Sign out", color = MeshaColors.Danger, fontSize = 14.5.sp, fontWeight = FontWeight.W600)
    }
}

/** Owned-module label from its backend key (e.g. `vaccination` → "Vaccination"). */
private fun moduleLabel(module: OwnedModule): String =
    module.module.replace('_', ' ').split(' ').joinToString(" ") { part ->
        part.replaceFirstChar { if (it.isLowerCase()) it.titlecase() else it.toString() }
    }

/** Home route for an owned module. Only vaccination ships today; others land on Calendar. */
private fun moduleRoute(module: OwnedModule): String =
    when (module.module.lowercase()) {
        "vaccination" -> Routes.VACCINATION
        else -> Routes.CALENDAR
    }

private fun languageLabel(tag: String): String = when (tag.lowercase()) {
    "hi" -> "हिंदी"
    "kn" -> "ಕನ್ನಡ"
    "te" -> "తెలుగు"
    else -> "English"
}
