package sg.mesha.goatos.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState

/**
 * The app shell. TRD §14 dumb-renderer: it only REACTS to the backend-computed
 * [NavState.chrome] + [NavState.items]. It never counts modules or checks role.
 * `EXPANDED` surfaces the module switcher; `MINIMAL` is bottom-bar-only, and the
 * drawer extras (language, RFID reader, notifications, sign out) live in You/Settings.
 */
@Composable
fun GoatOsShell(navState: NavState) {
    val navController = rememberNavController()
    val showModuleSwitcher = navState.chrome == NavChrome.EXPANDED
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route

    Scaffold(
        bottomBar = {
            NavigationBar {
                navState.items.forEach { item ->
                    NavigationBarItem(
                        selected = currentRoute == item.href,
                        onClick = {
                            navController.navigate(item.href) {
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { Text("•") },
                        label = { Text(item.label) },
                    )
                }
                NavigationBarItem(
                    selected = currentRoute == Routes.YOU,
                    onClick = {
                        navController.navigate(Routes.YOU) {
                            launchSingleTop = true
                            restoreState = true
                        }
                    },
                    icon = { Text("•") },
                    label = { Text("You") },
                )
            }
        },
    ) { padding ->
        Column(modifier = Modifier.padding(padding).fillMaxSize()) {
            if (showModuleSwitcher) {
                // Present ONLY because the backend returned >=2 visible modules.
                Text(text = "☰  Modules", modifier = Modifier.padding(12.dp))
            }
            AppNavHost(navController = navController)
        }
    }
}
