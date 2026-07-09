package sg.mesha.goatos.ui

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.leadership.LeadershipScreen
import sg.mesha.goatos.feature.profile.ProfileScreen
import sg.mesha.goatos.feature.record.RecordScreen
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.SubmitScreen

// Route ids. The backend nav item hrefs map onto these; unknown hrefs fall through
// to a placeholder rather than crashing (robust static graph).
object Routes {
    const val CALENDAR = "/calendar"
    const val VACCINATION = "/vaccination"
    const val SCAN = "/scan"
    const val SUBMIT = "/submit"
    const val LEADERSHIP = "/leadership"
    const val RECORD = "/record"
    const val YOU = "you"
    const val START = CALENDAR
}

/**
 * Static navigation graph of every known screen. The graph is fixed; the backend
 * nav (bottom bar + chrome) decides which destinations are *reachable/visible* —
 * the app doesn't invent routes. Calendar is the universal landing (screens.md).
 */
@Composable
fun AppNavHost(
    navController: NavHostController,
    modifier: Modifier = Modifier,
) {
    NavHost(
        navController = navController,
        startDestination = Routes.START,
        modifier = modifier,
    ) {
        composable(Routes.CALENDAR) { CalendarScreen() }
        // Vaccination execution surfaces as the shed-first flow (screens.md).
        composable(Routes.VACCINATION) { ShedsScreen() }
        composable(Routes.SCAN) { ScanScreen() }
        composable(Routes.SUBMIT) { SubmitScreen() }
        composable(Routes.LEADERSHIP) { LeadershipScreen() }
        composable(Routes.RECORD) { RecordScreen() }
        composable(Routes.YOU) { ProfileScreen() }
    }
}

@Composable
internal fun UnknownRoute(href: String) {
    Text(text = "Screen: $href", modifier = Modifier.padding(16.dp))
}
