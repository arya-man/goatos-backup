package sg.mesha.goatos.ui

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import android.net.Uri
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import sg.mesha.goatos.feature.calendar.CalendarEvent
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.leadership.LeadershipEvent
import sg.mesha.goatos.feature.leadership.LeadershipScreen
import sg.mesha.goatos.feature.leadership.OverdueScreen
import sg.mesha.goatos.feature.leadership.RescheduleScreen
import sg.mesha.goatos.feature.profile.AlertsScreen
import sg.mesha.goatos.feature.profile.ProfileEvent
import sg.mesha.goatos.feature.profile.ProfileScreen
import sg.mesha.goatos.feature.profile.ProfileUiState
import sg.mesha.goatos.feature.profile.RfidScreen
import sg.mesha.goatos.feature.profile.SettingKind
import sg.mesha.goatos.feature.record.RecordEvent
import sg.mesha.goatos.feature.record.RecordScreen
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedsEvent
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.SubmitScreen
import sg.mesha.goatos.feature.timetable.TimetableScreen
import sg.mesha.goatos.viewmodel.AlertsViewModel
import sg.mesha.goatos.viewmodel.CalendarViewModel
import sg.mesha.goatos.viewmodel.CoverageBannerViewModel
import sg.mesha.goatos.viewmodel.LeadershipViewModel
import sg.mesha.goatos.viewmodel.OverdueViewModel
import sg.mesha.goatos.viewmodel.ProfileViewModel
import sg.mesha.goatos.viewmodel.RecordViewModel
import sg.mesha.goatos.viewmodel.RescheduleViewModel
import sg.mesha.goatos.viewmodel.RfidViewModel
import sg.mesha.goatos.viewmodel.ScanViewModel
import sg.mesha.goatos.viewmodel.ShedsViewModel
import sg.mesha.goatos.viewmodel.SubmitViewModel
import sg.mesha.goatos.viewmodel.TimetableViewModel

// Route ids. The backend nav item hrefs map onto these; unknown hrefs fall through
// to a placeholder rather than crashing (robust static graph).
object Routes {
    const val CALENDAR = "/calendar"
    const val VACCINATION = "/vaccination"
    const val SCAN = "/scan"
    const val SUBMIT = "/submit"
    const val LEADERSHIP = "/leadership"
    const val RECORD = "/record"
    const val OVERDUE = "/overdue"
    const val RESCHEDULE = "/reschedule"
    const val YOU = "you"
    const val RFID = "/rfid"
    const val ALERTS = "/alerts"
    /** Read-only HRMS shift roster mirror (docs/hr/roster-rbac-design.md) — TRD §14: mobile
     *  never writes positions/leave/backups, all CRUD stays web-only. */
    const val TIMETABLE = "/timetable"
    const val START = CALENDAR

    /** Optional shed-id arg on the record route so a tapped shed opens ITS record. */
    const val RECORD_SHED_ARG = "shedId"

    /** Record route for a specific shed (null → generic first-shed record). */
    fun recordRoute(shedId: String?): String =
        if (shedId.isNullOrBlank()) RECORD else "$RECORD?$RECORD_SHED_ARG=${Uri.encode(shedId)}"

    const val SCAN_SHED_ARG = "shedId"

    /** Scan (execute) entry for a shed — threads the shed id so ScanViewModel loads that
     *  shed's per-animal roster from the backend. */
    fun scanRoute(shedId: String?): String =
        if (shedId.isNullOrBlank()) SCAN else "$SCAN?$SCAN_SHED_ARG=${Uri.encode(shedId)}"
}

/**
 * Maps a backend calendar deep-link ([CalendarItem.target]/[CalendarHistoryRow.target])
 * to an app route. A shed-scoped target opens that shed's record; anything else drills
 * into the vaccination execution list. The row's context is no longer discarded.
 */
private fun calendarTargetRoute(target: String?): String {
    if (target.isNullOrBlank()) return Routes.VACCINATION
    // Past-drive/history rows point at a read-only record.
    if (target.contains("record/")) {
        val id = target.substringAfter("record/").substringBefore('/').substringBefore('?')
        return Routes.recordRoute(id.ifBlank { null })
    }
    val shedId = shedIdFromTarget(target)
    return if (shedId != null) Routes.recordRoute(shedId) else Routes.VACCINATION
}

/** Extracts a shed id from a backend href, supporting `.../sheds/{id}` and `?shed_id={id}`. */
private fun shedIdFromTarget(target: String): String? {
    val marker = "sheds/"
    val idx = target.indexOf(marker)
    if (idx >= 0) {
        val id = target.substring(idx + marker.length).substringBefore('/').substringBefore('?')
        if (id.isNotBlank()) return id
    }
    Regex("[?&]shed_id=([^&]+)").find(target)?.groupValues?.getOrNull(1)?.let {
        if (it.isNotBlank()) return it
    }
    return null
}

/**
 * Static navigation graph of every known screen. The graph is fixed; the backend
 * nav (bottom bar + chrome) decides which destinations are *reachable/visible* —
 * the app doesn't invent routes. Calendar is the universal landing (screens.md).
 *
 * Each destination binds a `@HiltViewModel` via [hiltViewModel]; the screen renders
 * the VM's reactive [state] and its `onEvent` splits into two: LOCAL events go to the
 * VM (which does the copy-with-changed-field on the StateFlow), NAVIGATION events
 * drive the [navController]. The feature screens stay stateless dumb-renderers.
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
        // Calendar — universal landing. Segment switch is local; day/item taps drill
        // into the vaccination execution flow.
        composable(Routes.CALENDAR) {
            val vm: CalendarViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            // Coverage banner (docs/hr/roster-rbac-design.md S4.6/S4.8) is resolved by a
            // separate small VM and merged into CalendarUiState at this call site. The VM
            // self-loads via GET /app/roster/my-coverage (no scope/identity params needed —
            // the backend resolves the authenticated principal's own coverage) and stays
            // hidden whenever has_coverage is false.
            val coverageVm: CoverageBannerViewModel = hiltViewModel()
            val coverageState by coverageVm.state.collectAsStateWithLifecycle()
            CalendarScreen(
                state = state.copy(coverageBanner = coverageState),
                onEvent = { event ->
                    when (event) {
                        is CalendarEvent.TapItem -> {
                            // Honour the backend-attached drill target instead of always /vaccination.
                            val target = state.weekItems.firstOrNull { it.id == event.itemId }?.target
                                ?: state.historyRows.firstOrNull { it.id == event.itemId }?.target
                            navController.navigate(calendarTargetRoute(target)) { launchSingleTop = true }
                        }
                        is CalendarEvent.TapDay ->
                            navController.navigate(Routes.VACCINATION) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Vaccination execution surfaces as the shed-first flow (screens.md). Tapping a
        // shed opens the execute loop (Scan → Submit) — the operator's core task — with
        // the tapped shed threaded through. Refresh stays in the VM.
        composable(Routes.VACCINATION) {
            val vm: ShedsViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            ShedsScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is ShedsEvent.OpenShedRecord -> {
                            // Done sheds open the read-only record; anything still due opens
                            // the execute loop (Scan → Submit). Mirrors the mock's shed card
                            // ("View completed record ›" vs "Start / scan").
                            val done = state.rows.firstOrNull { it.id == event.shedId }?.status == ShedStatus.DONE
                            val route = if (done) Routes.recordRoute(event.shedId) else Routes.scanRoute(event.shedId)
                            navController.navigate(route) { launchSingleTop = true }
                        }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Scan — Submit drills to the shed-record submit; Back pops; group/tile/tap stay local.
        // The shed id arg feeds the per-shed roster; capture is enabled only while this
        // screen is composed (disabled on navigate-away) so keyboard-wedge reads never
        // land off-screen (e.g. while Submit is on top of the back stack).
        composable(
            route = "${Routes.SCAN}?${Routes.SCAN_SHED_ARG}={${Routes.SCAN_SHED_ARG}}",
            arguments = listOf(
                navArgument(Routes.SCAN_SHED_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
            ),
        ) {
            val vm: ScanViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            DisposableEffect(vm) {
                vm.setCaptureActive(true)
                onDispose { vm.setCaptureActive(false) }
            }
            ScanScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        ScanEvent.Submit ->
                            navController.navigate(Routes.SUBMIT) { launchSingleTop = true }
                        ScanEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Submit — stays put; the VM advances the sync lifecycle (draft → syncing → acked).
        composable(Routes.SUBMIT) {
            val vm: SubmitViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            SubmitScreen(state = state, onEvent = vm::onEvent)
        }

        // Leadership overview — a decision drills to reschedule; the "doses given" KPI
        // opens the per-vaccine drill sheet, other KPIs open the overdue list; the scope
        // + data-gap pills open their sheets; refresh + inert taps stay in the VM.
        composable(Routes.LEADERSHIP) {
            val vm: LeadershipViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            var showScope by remember { mutableStateOf(false) }
            var showGaps by remember { mutableStateOf(false) }
            var showGiven by remember { mutableStateOf(false) }
            LeadershipScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is LeadershipEvent.DecisionTapped ->
                            navController.navigate(Routes.RESCHEDULE) { launchSingleTop = true }
                        // Leadership taps a shed → the read-only record (their lens is follow-up).
                        is LeadershipEvent.ShedTapped ->
                            navController.navigate(Routes.recordRoute(event.shedId)) { launchSingleTop = true }
                        is LeadershipEvent.OpenScopePicker -> showScope = true
                        is LeadershipEvent.OpenDataGaps -> showGaps = true
                        is LeadershipEvent.KpiTapped ->
                            if (event.id == "given") {
                                showGiven = true
                            } else {
                                navController.navigate(Routes.OVERDUE) { launchSingleTop = true }
                            }
                        else -> vm.onEvent(event)
                    }
                },
            )
            if (showScope) {
                ScopePickerSheet(
                    // TODO(backend): send the chosen scope token to re-scope the reads.
                    onSelect = { showScope = false },
                    onDismiss = { showScope = false },
                )
            }
            if (showGaps) {
                DataGapsSheet(onDismiss = { showGaps = false })
            }
            if (showGiven) {
                DosesGivenSheet(onDismiss = { showGiven = false })
            }
        }

        // Overdue list — a row drills to reschedule; Back pops; refresh stays in the VM.
        composable(Routes.OVERDUE) {
            val vm: OverdueViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            OverdueScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is LeadershipEvent.OverdueRowTapped ->
                            navController.navigate(Routes.RESCHEDULE) { launchSingleTop = true }
                        LeadershipEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Reschedule form — segment/date selection stays local; Confirm + Back pop back.
        composable(Routes.RESCHEDULE) {
            val vm: RescheduleViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            RescheduleScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        LeadershipEvent.ConfirmReschedule,
                        LeadershipEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Record — read-only; Close pops back. Optional shedId arg selects WHICH shed's
        // record loads (RecordViewModel reads it from SavedStateHandle).
        composable(
            route = "${Routes.RECORD}?${Routes.RECORD_SHED_ARG}={${Routes.RECORD_SHED_ARG}}",
            arguments = listOf(
                navArgument(Routes.RECORD_SHED_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
            ),
        ) {
            val vm: RecordViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            RecordScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        RecordEvent.Close -> navController.popBackStack()
                    }
                },
            )
        }

        // You / Settings — RFID + notifications drill to their surfaces; language opens the
        // picker sheet (persisted via the VM); sign out clears the session (MainActivity's
        // gate then shows login).
        composable(Routes.YOU) {
            val vm: ProfileViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            var showLanguage by remember { mutableStateOf(false) }
            ProfileScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        ProfileEvent.PairRfid ->
                            navController.navigate(Routes.RFID) { launchSingleTop = true }
                        ProfileEvent.ToggleNotifications ->
                            navController.navigate(Routes.ALERTS) { launchSingleTop = true }
                        ProfileEvent.OpenTimetable ->
                            navController.navigate(Routes.TIMETABLE) { launchSingleTop = true }
                        ProfileEvent.OpenLanguage -> showLanguage = true
                        ProfileEvent.SignOut -> vm.signOut()
                    }
                },
            )
            if (showLanguage) {
                LanguageSheet(
                    current = currentLanguageCode(state),
                    onSelect = { code ->
                        vm.setLanguage(code)
                        showLanguage = false
                    },
                    onDismiss = { showLanguage = false },
                )
            }
        }

        composable(Routes.RFID) {
            val vm: RfidViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            RfidScreen(state = state, onEvent = vm::onEvent)
        }

        composable(Routes.ALERTS) {
            val vm: AlertsViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            AlertsScreen(state = state, onEvent = vm::onEvent)
        }

        // Timetable — read-only HRMS shift roster mirror; Back pops via system back (no
        // explicit Back event, matching the RFID/Alerts routes' pattern).
        composable(Routes.TIMETABLE) {
            val vm: TimetableViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            TimetableScreen(state = state, onEvent = vm::onEvent)
        }
    }
}

@Composable
internal fun UnknownRoute(href: String) {
    Text(text = "Screen: $href", modifier = Modifier.padding(16.dp))
}

/**
 * Reverse-maps the Language settings row's native-label value to a language code so
 * [LanguageSheet] can check the active row. The label is the display value the VM
 * persists; codes match the sheet's en/hi/kn/te options.
 */
private fun currentLanguageCode(state: ProfileUiState): String =
    when (state.rows.firstOrNull { it.kind == SettingKind.LANGUAGE }?.value) {
        "हिंदी", "हिन्दी" -> "hi"
        "ಕನ್ನಡ" -> "kn"
        "తెలుగు" -> "te"
        else -> "en"
    }
