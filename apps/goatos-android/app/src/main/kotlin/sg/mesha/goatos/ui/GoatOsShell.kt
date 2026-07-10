package sg.mesha.goatos.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.NavigationDrawerItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState

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

    GoatOsShellChrome(
        navState = navState,
        currentRoute = backStackEntry?.destination?.route,
        onNavigate = { href -> navController.navigate(href) { launchSingleTop = true; restoreState = true } },
    ) {
        AppNavHost(navController = navController)
    }
}

/**
 * The shell's chrome (drawer/module-switcher + bottom bar + Scaffold) around an arbitrary
 * [content] slot, factored out of [GoatOsShell] so it can be driven by a static [NavState]
 * fixture and a static landing composable — no [androidx.navigation.NavHostController] or Hilt
 * ViewModel required. [GoatOsShell] is the real app entry (content = [AppNavHost]);
 * `RoleChromeScreenshotTest` is the other caller (content = one role's landing screen), so every
 * role's nav chrome renders through the exact same code path a device would use.
 */
@Composable
fun GoatOsShellChrome(
    navState: NavState,
    currentRoute: String?,
    onNavigate: (String) -> Unit,
    initialDrawerValue: DrawerValue = DrawerValue.Closed,
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
                    onSelect = { href ->
                        scope.launch { drawerState.close() }
                        onNavigate(href)
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

/** Module switcher (drawer). Lists the backend-visible modules with their mock icons. */
@Composable
private fun ModuleDrawer(
    navState: NavState,
    currentRoute: String?,
    onSelect: (String) -> Unit,
) {
    ModalDrawerSheet(drawerContainerColor = MaterialTheme.colorScheme.surfaceContainer) {
        Text(
            text = "MESHA",
            style = MaterialTheme.typography.labelLarge,
            fontWeight = FontWeight.ExtraBold,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(20.dp),
        )
        navState.items.forEach { item ->
            NavigationDrawerItem(
                icon = {
                    Icon(
                        imageVector = MeshaIcons.forNavKey(item.key),
                        contentDescription = item.label,
                        modifier = Modifier.size(24.dp),
                    )
                },
                label = { Text(item.label) },
                selected = currentRoute == item.href,
                onClick = { onSelect(item.href) },
                colors = NavigationDrawerItemDefaults.colors(),
                modifier = Modifier.padding(NavigationDrawerItemDefaults.ItemPadding),
            )
        }
    }
}
