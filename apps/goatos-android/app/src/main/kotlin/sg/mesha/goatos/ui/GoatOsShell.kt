package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationDrawerItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState

/**
 * The role-aware app shell. TRD §14 dumb-renderer: it renders the backend-computed
 * [NavState] and never counts modules or checks role. The backend already decided:
 *  - [NavState.chrome] EXPANDED  → a module-switcher DRAWER is available (the
 *    principal owns >=2 visible modules); MINIMAL → bottom-bar only, no drawer, and
 *    the drawer extras live in You/Settings.
 *  - [NavState.items] → the bottom-bar destinations (per role/department).
 * The role lens is thus entirely data-driven — one shell serves operator + leadership.
 */
@Composable
fun GoatOsShell(navState: NavState) {
    val navController = rememberNavController()
    val hasDrawer = navState.chrome == NavChrome.EXPANDED
    val drawerState = rememberDrawerState(DrawerValue.Closed)
    val scope = rememberCoroutineScope()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route

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
                        navController.navigate(href) { launchSingleTop = true; restoreState = true }
                    },
                )
            }
        },
    ) {
        Scaffold(
            bottomBar = {
                NavigationBar {
                    navState.items.forEach { item ->
                        NavigationBarItem(
                            selected = currentRoute == item.href,
                            onClick = {
                                navController.navigate(item.href) { launchSingleTop = true; restoreState = true }
                            },
                            icon = { Text("•") },
                            label = { Text(item.label) },
                        )
                    }
                    NavigationBarItem(
                        selected = currentRoute == Routes.YOU,
                        onClick = { navController.navigate(Routes.YOU) { launchSingleTop = true; restoreState = true } },
                        icon = { Text("•") },
                        label = { Text("You") },
                    )
                }
            },
        ) { padding ->
            Column(modifier = Modifier.padding(padding).fillMaxSize()) {
                // Menu affordance exists only for a multi-module principal.
                if (hasDrawer) {
                    ShellTopBar(onMenu = { scope.launch { drawerState.open() } })
                }
                NetBar()
                AppNavHost(navController = navController)
            }
        }
    }
}

@Composable
private fun ShellTopBar(onMenu: () -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "☰",
            fontSize = 22.sp,
            color = MaterialTheme.colorScheme.onBackground,
            modifier = Modifier.clip(CircleShape).clickable(onClick = onMenu).padding(6.dp),
        )
        Spacer(Modifier.size(8.dp))
        Text("Goat OS", fontWeight = FontWeight.W700, color = MaterialTheme.colorScheme.onBackground)
    }
}

/** The module switcher (drawer/sidebar). Lists the backend-visible modules; the
 *  registry can also mark not-yet-built modules as non-tappable "Soon". */
@Composable
private fun ModuleDrawer(
    navState: NavState,
    currentRoute: String?,
    onSelect: (String) -> Unit,
) {
    ModalDrawerSheet {
        Text(
            text = "GOAT OS",
            fontSize = 12.sp,
            fontWeight = FontWeight.W800,
            color = MaterialTheme.colorScheme.primary,
            modifier = Modifier.padding(20.dp),
        )
        navState.items.forEach { item ->
            NavigationDrawerItem(
                label = { Text(item.label) },
                selected = currentRoute == item.href,
                onClick = { onSelect(item.href) },
                modifier = Modifier.padding(horizontal = 12.dp),
            )
        }
    }
}

/** Connectivity + sync bar shown on every signed-in screen (screens.md #netbar).
 *  Static baseline; a sync ViewModel drives Online/Offline + queued/syncing next. */
@Composable
private fun NetBar() {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceVariant)
            .padding(horizontal = 14.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(8.dp).clip(CircleShape).background(Color(0xFF3DA35D)))
        Spacer(Modifier.size(8.dp))
        Text(
            text = "Online · All synced",
            fontSize = 12.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
