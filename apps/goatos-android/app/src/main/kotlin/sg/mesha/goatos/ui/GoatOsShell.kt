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
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.feature.calendar.CalendarScreen

private const val YOU_KEY = "__you__"

/**
 * The app shell. TRD §14 dumb-renderer: this only REACTS to the backend-computed
 * [NavState.chrome]. It never counts modules and never checks role to decide the
 * chrome. `EXPANDED` surfaces the module switcher (drawer/sidebar); `MINIMAL` is
 * bottom-bar-only, and the drawer extras (language, RFID reader, notifications,
 * sign out) live in the You/Settings tab.
 */
@Composable
fun GoatOsShell(navState: NavState) {
    val showModuleSwitcher = navState.chrome == NavChrome.EXPANDED
    var selected by remember(navState.items) {
        mutableStateOf(navState.items.firstOrNull()?.key ?: YOU_KEY)
    }

    Scaffold(
        bottomBar = {
            NavigationBar {
                navState.items.forEach { item ->
                    NavigationBarItem(
                        selected = selected == item.key,
                        onClick = { selected = item.key },
                        icon = { Text("•") },
                        label = { Text(item.label) },
                    )
                }
                // You/Settings is always present; for MINIMAL principals it also
                // hosts the affordances a drawer would otherwise carry.
                NavigationBarItem(
                    selected = selected == YOU_KEY,
                    onClick = { selected = YOU_KEY },
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
            when (selected) {
                "calendar" -> CalendarScreen()
                YOU_KEY -> PlaceholderRoute("You / Settings")
                else -> PlaceholderRoute(selected)
            }
        }
    }
}

@Composable
private fun PlaceholderRoute(name: String) {
    Text(text = "Route: $name", modifier = Modifier.padding(16.dp))
}
