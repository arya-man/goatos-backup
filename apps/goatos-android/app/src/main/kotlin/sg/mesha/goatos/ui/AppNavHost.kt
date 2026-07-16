package sg.mesha.goatos.ui

import androidx.compose.animation.AnimatedContentTransitionScope.SlideDirection
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import android.net.Uri
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import sg.mesha.goatos.capture.BindVideoCaptureSource
import sg.mesha.goatos.capture.CaptureAccessGate
import sg.mesha.goatos.capture.rememberDelegatingProofCaptureSource
import sg.mesha.goatos.feature.calendar.CalendarDayScreen
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
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VerifyDetailScreen
import sg.mesha.goatos.feature.verify.VerifyQueueEvent
import sg.mesha.goatos.feature.verify.VerifyQueueScreen
import sg.mesha.goatos.viewmodel.AlertsViewModel
import sg.mesha.goatos.viewmodel.CalendarDayViewModel
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
import sg.mesha.goatos.viewmodel.VerifyDetailViewModel
import sg.mesha.goatos.viewmodel.VerifyQueueViewModel

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

    // Standalone Verifier section (context/architecture/verifier-app-and-flow.md). A verifier's
    // bootstrap nav contains ONLY this — see MeshaIcons.forNavKey/GoatOsShell.navItemLabel's
    // "verify" key mapping. VERIFY is the queue; VERIFY_DETAIL drills to one item's video +
    // approve/reject, threading both the item id AND its category (the detail VM re-observes
    // that SAME category's Room cache scope rather than adding a second network call).
    const val VERIFY = "/verify"
    const val VERIFY_DETAIL = "/verify/item"
    const val VERIFY_ITEM_ARG = "itemId"
    const val VERIFY_CATEGORY_ARG = "category"

    fun verifyDetailRoute(itemId: String, category: String?): String {
        val args = listOfNotNull(
            VERIFY_ITEM_ARG to itemId,
            category?.takeIf { it.isNotBlank() }?.let { VERIFY_CATEGORY_ARG to it },
        )
        return "$VERIFY_DETAIL?" + args.joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
    }

    const val START = CALENDAR

    /** Optional shed-id arg on the record route so a tapped shed opens ITS record. */
    const val RECORD_SHED_ARG = "shedId"
    const val RESCHEDULE_OBLIGATION_ARG = "obligationId"

    /** Record route for a specific shed (null → generic first-shed record). */
    fun recordRoute(shedId: String?): String =
        if (shedId.isNullOrBlank()) RECORD else "$RECORD?$RECORD_SHED_ARG=${Uri.encode(shedId)}"

    const val SCAN_SHED_ARG = "shedId"
    const val EXECUTION_DRIVE_ARG = "driveId"
    const val EXECUTION_BATCH_ARG = "batchId"
    const val EXECUTION_TASK_ARG = "taskId"
    const val EXECUTION_SOP_VERSION_ARG = "sopVersionId"
    const val EXECUTION_TASK_ROW_VERSION_ARG = "taskRowVersion"

    /** Scan (execute) entry for a shed — threads the shed id so ScanViewModel loads that
     *  shed's per-animal roster from the backend. */
    fun scanRoute(
        shedId: String?,
        driveId: String? = null,
        batchId: String? = null,
        taskId: String? = null,
        sopVersionId: String? = null,
        taskRowVersion: Int? = null,
    ): String = executionRoute(SCAN, shedId, driveId, batchId, taskId, sopVersionId, taskRowVersion)

    fun submitRoute(
        shedId: String?,
        driveId: String? = null,
        batchId: String? = null,
        taskId: String? = null,
        sopVersionId: String? = null,
        taskRowVersion: Int? = null,
    ): String = executionRoute(SUBMIT, shedId, driveId, batchId, taskId, sopVersionId, taskRowVersion)

    private fun executionRoute(
        base: String,
        shedId: String?,
        driveId: String?,
        batchId: String?,
        taskId: String?,
        sopVersionId: String?,
        taskRowVersion: Int?,
    ): String {
        val args = listOfNotNull(
            shedId?.takeIf { it.isNotBlank() }?.let { SCAN_SHED_ARG to it },
            driveId?.takeIf { it.isNotBlank() }?.let { EXECUTION_DRIVE_ARG to it },
            batchId?.takeIf { it.isNotBlank() }?.let { EXECUTION_BATCH_ARG to it },
            taskId?.takeIf { it.isNotBlank() }?.let { EXECUTION_TASK_ARG to it },
            sopVersionId?.takeIf { it.isNotBlank() }?.let { EXECUTION_SOP_VERSION_ARG to it },
            taskRowVersion?.takeIf { it > 0 }?.let { EXECUTION_TASK_ROW_VERSION_ARG to it.toString() },
        )
        return if (args.isEmpty()) base else "$base?" + args.joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
    }

    const val CALENDAR_DAY = "/calendarDay"
    const val CALENDAR_DAY_ARG = "dateKey"
    const val CALENDAR_DAY_STATUS_ARG = "status"

    /** Day-detail (L1) screen opened by a month-grid day tap (arg = ISO date key). History-only
     *  month cells carry `status=completed` so a visible completion marker never drills into an
     *  empty open-work day query. */
    fun calendarDayRoute(dateKey: String, showCompletedHistory: Boolean = false): String =
        buildString {
            append("$CALENDAR_DAY?$CALENDAR_DAY_ARG=${Uri.encode(dateKey)}")
            if (showCompletedHistory) append("&$CALENDAR_DAY_STATUS_ARG=completed")
        }

    fun rescheduleRoute(obligationId: String?): String =
        if (obligationId.isNullOrBlank()) RESCHEDULE else "$RESCHEDULE?$RESCHEDULE_OBLIGATION_ARG=${Uri.encode(obligationId)}"
}

/**
 * Maps a backend calendar deep-link ([CalendarItem.target]/[CalendarHistoryRow.target])
 * to an app route. Live shed-scoped work opens the execute loop, explicit `record/...` targets
 * open the read-only record, and anything else falls back to the vaccination landing.
 *
 * `internal` (not `private`): [sg.mesha.goatos.push.resolvePushRoute] reuses this SAME
 * backend-href -> route mapping for an FCM push carrying an explicit `target`/`href`, so a
 * notification tap opens exactly where a Calendar tap on the same backend item would.
 */
internal fun calendarTargetRoute(target: String?): String {
    if (target.isNullOrBlank()) return Routes.VACCINATION
    if (target.contains("scan/")) {
        val id = target.substringAfter("scan/").substringBefore('/').substringBefore('?')
        val uri = Uri.parse(target)
        val taskId = uri.getQueryParameter("task_id") ?: uri.getQueryParameter("taskId")
        return Routes.scanRoute(
            shedId = id.ifBlank { null },
            driveId = uri.getQueryParameter("drive_id") ?: uri.getQueryParameter("driveId"),
            batchId = uri.getQueryParameter("batch_id") ?: uri.getQueryParameter("batchId"),
            taskId = taskId,
            sopVersionId = uri.getQueryParameter("sop_version_id") ?: uri.getQueryParameter("sopVersionId"),
            taskRowVersion = (uri.getQueryParameter("task_row_version")
                ?: uri.getQueryParameter("taskRowVersion"))?.toIntOrNull(),
        )
    }
    // Past-drive/history rows point at a read-only record.
    if (target.contains("record/")) {
        val id = target.substringAfter("record/").substringBefore('/').substringBefore('?')
        return Routes.recordRoute(id.ifBlank { null })
    }
    val shedId = shedIdFromTarget(target)
    return if (shedId != null) Routes.scanRoute(shedId) else Routes.VACCINATION
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
    // Shared-axis-X motion instead of the default cross-fade: a forward navigation slides
    // the new screen in from the end and the old one out toward the start; Back reverses it.
    // Gives drill-in (Calendar → sheds → Scan → Submit) real directional continuity.
    val motion = tween<Float>(280)
    NavHost(
        navController = navController,
        startDestination = Routes.START,
        modifier = modifier,
        enterTransition = { slideIntoContainer(SlideDirection.Start, tween(280)) + fadeIn(motion) },
        exitTransition = { slideOutOfContainer(SlideDirection.Start, tween(280)) + fadeOut(motion) },
        popEnterTransition = { slideIntoContainer(SlideDirection.End, tween(280)) + fadeIn(motion) },
        popExitTransition = { slideOutOfContainer(SlideDirection.End, tween(280)) + fadeOut(motion) },
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
                        // A MONTH-grid day tap opens the day's own L1 screen (real drill),
                        // never an inline sheet under the grid.
                        is CalendarEvent.OpenDay ->
                            navController.navigate(Routes.calendarDayRoute(event.dateKey, event.showCompletedHistory)) { launchSingleTop = true }
                        // A WEEK-strip day tap is in-screen selection (re-scopes the week agenda
                        // list to that day) and must NOT navigate. Handled by CalendarViewModel.
                        is CalendarEvent.TapDay -> vm.onEvent(event)
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Calendar day detail (L1) — opened by a MONTH-grid day tap. Its own screen showing
        // that day's drives; Back pops to the calendar, an item drills to its backend target.
        composable(
            route = "${Routes.CALENDAR_DAY}?${Routes.CALENDAR_DAY_ARG}={${Routes.CALENDAR_DAY_ARG}}&${Routes.CALENDAR_DAY_STATUS_ARG}={${Routes.CALENDAR_DAY_STATUS_ARG}}",
            arguments = listOf(
                navArgument(Routes.CALENDAR_DAY_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
                navArgument(Routes.CALENDAR_DAY_STATUS_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
            ),
        ) {
            val vm: CalendarDayViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            CalendarDayScreen(
                state = state,
                onBack = { navController.popBackStack() },
                onItemTap = { itemId ->
                    val target = state.items.firstOrNull { it.id == itemId }?.target
                    navController.navigate(calendarTargetRoute(target)) { launchSingleTop = true }
                },
                onLoadMore = vm::loadMore,
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
                            val selected = state.rows.firstOrNull { it.id == event.shedId }
                            val route = when {
                                selected == null -> Routes.VACCINATION
                                selected.status == ShedStatus.DONE -> Routes.recordRoute(selected.shedId)
                                selected.taskId.isNullOrBlank() -> Routes.recordRoute(selected.shedId)
                                else -> Routes.scanRoute(
                                    shedId = selected.shedId,
                                    driveId = selected.driveId,
                                    batchId = selected.batchId,
                                    taskId = selected.taskId,
                                    sopVersionId = selected.sopVersionId,
                                    taskRowVersion = selected.taskRowVersion,
                                )
                            }
                            navController.navigate(route) { launchSingleTop = true }
                        }
                        ShedsEvent.Back -> navController.popBackStack()
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
            route = executionRoutePattern(Routes.SCAN),
            arguments = executionNavArguments(),
        ) { entry ->
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
                        ScanEvent.Submit -> navController.navigate(
                            Routes.submitRoute(
                                shedId = entry.arguments?.getString(Routes.SCAN_SHED_ARG),
                                driveId = entry.arguments?.getString(Routes.EXECUTION_DRIVE_ARG),
                                batchId = entry.arguments?.getString(Routes.EXECUTION_BATCH_ARG),
                                taskId = entry.arguments?.getString(Routes.EXECUTION_TASK_ARG),
                                sopVersionId = entry.arguments?.getString(Routes.EXECUTION_SOP_VERSION_ARG),
                                taskRowVersion = entry.arguments?.getInt(Routes.EXECUTION_TASK_ROW_VERSION_ARG)?.takeIf { it > 0 },
                            ),
                        ) { launchSingleTop = true }
                        ScanEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Submit — stays put; the VM advances the sync lifecycle (draft → syncing → acked).
        composable(
            route = executionRoutePattern(Routes.SUBMIT),
            arguments = executionNavArguments(),
        ) {
            val vm: SubmitViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            // Mandatory permission gate (§4): camera/Bluetooth/location/notifications/storage
            // ALL granted before the capture surface renders at all — no degraded path.
            CaptureAccessGate {
                // MOB-002 camera-only capture: binds the LIVE in-app recorder to this screen's
                // composition lifecycle only — released the moment Submit leaves composition.
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                SubmitScreen(state = state, onEvent = vm::onEvent)
            }
        }

        // Leadership overview — a decision drills to reschedule; the "doses given" KPI
        // opens the per-vaccine drill sheet, other KPIs open the overdue list; the scope
        // + data-gap pills open their sheets; refresh + inert taps stay in the VM.
        composable(Routes.LEADERSHIP) {
            val vm: LeadershipViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val gapsState by vm.gapsState.collectAsStateWithLifecycle()
            val dosesState by vm.dosesState.collectAsStateWithLifecycle()
            var showScope by remember { mutableStateOf(false) }
            var showGaps by remember { mutableStateOf(false) }
            var showGiven by remember { mutableStateOf(false) }
            LaunchedEffect(showGaps) {
                if (showGaps) vm.loadGaps()
            }
            LaunchedEffect(showGiven) {
                if (showGiven) vm.loadDosesGiven()
            }
            LeadershipScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is LeadershipEvent.DecisionTapped ->
                            navController.navigate(Routes.rescheduleRoute(event.id)) { launchSingleTop = true }
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
                    scopes = defaultScopeOptions(),
                    onSelect = { label, token ->
                        // TODO(backend): send the chosen scope token to re-scope the reads.
                        showScope = false
                    },
                    onDismiss = { showScope = false },
                )
            }
            if (showGaps) {
                DataGapsSheet(
                    gapsData = gapsState.items,
                    isLoading = gapsState.isLoading,
                    isLoadingMore = gapsState.isLoadingMore,
                    hasMore = gapsState.hasMore,
                    errorMessage = gapsState.errorMessage,
                    isRefreshing = gapsState.isRefreshing,
                    lastSyncedAt = gapsState.lastSyncedAt,
                    isOffline = gapsState.isOffline,
                    onLoadMore = vm::loadMoreGaps,
                    onDismiss = { showGaps = false },
                )
            }
            if (showGiven) {
                DosesGivenSheet(
                    rows = dosesState.items,
                    isLoading = dosesState.isLoading,
                    errorMessage = dosesState.errorMessage,
                    isRefreshing = dosesState.isRefreshing,
                    lastSyncedAt = dosesState.lastSyncedAt,
                    isOffline = dosesState.isOffline,
                    onDismiss = { showGiven = false },
                )
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
                            navController.navigate(Routes.rescheduleRoute(event.id)) { launchSingleTop = true }
                        LeadershipEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Reschedule form — segment/date selection stays local; Confirm + Back pop back.
        composable(
            route = "${Routes.RESCHEDULE}?${Routes.RESCHEDULE_OBLIGATION_ARG}={${Routes.RESCHEDULE_OBLIGATION_ARG}}",
            arguments = listOf(
                navArgument(Routes.RESCHEDULE_OBLIGATION_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
            ),
        ) {
            val vm: RescheduleViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            RescheduleScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        LeadershipEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Record — read-only; Close pops back. Optional shedId arg selects WHICH shed's
        // record loads (RecordViewModel reads it from SavedStateHandle). The record surface
        // is display-only and has NO verify/rework capability (leadership verify/rework is
        // in the VERIFY_DETAIL surface). When a shed lacks scannable tasks, the record
        // honestly shows "awaiting task assignment" instead of a dead-end.
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

        // Standalone Verifier section (context/architecture/verifier-app-and-flow.md): a
        // verifier's bootstrap nav contains ONLY VERIFY, so this is their entire app. A row
        // drills to VERIFY_DETAIL with both the item id and ITS category threaded through, so
        // the detail VM re-observes that exact Room cache scope (no second network round trip).
        composable(Routes.VERIFY) {
            val vm: VerifyQueueViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            VerifyQueueScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is VerifyQueueEvent.OpenItem ->
                            navController.navigate(Routes.verifyDetailRoute(event.itemId, event.category)) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(
            route = "${Routes.VERIFY_DETAIL}?${Routes.VERIFY_ITEM_ARG}={${Routes.VERIFY_ITEM_ARG}}" +
                "&${Routes.VERIFY_CATEGORY_ARG}={${Routes.VERIFY_CATEGORY_ARG}}",
            arguments = listOf(
                navArgument(Routes.VERIFY_ITEM_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_CATEGORY_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
            ),
        ) {
            val vm: VerifyDetailViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            VerifyDetailScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        VerifyDetailEvent.Close -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }
    }
}

private fun executionRoutePattern(base: String): String =
    "$base?${Routes.SCAN_SHED_ARG}={${Routes.SCAN_SHED_ARG}}" +
        "&${Routes.EXECUTION_DRIVE_ARG}={${Routes.EXECUTION_DRIVE_ARG}}" +
        "&${Routes.EXECUTION_BATCH_ARG}={${Routes.EXECUTION_BATCH_ARG}}" +
        "&${Routes.EXECUTION_TASK_ARG}={${Routes.EXECUTION_TASK_ARG}}" +
        "&${Routes.EXECUTION_SOP_VERSION_ARG}={${Routes.EXECUTION_SOP_VERSION_ARG}}" +
        "&${Routes.EXECUTION_TASK_ROW_VERSION_ARG}={${Routes.EXECUTION_TASK_ROW_VERSION_ARG}}"

private fun executionNavArguments() = listOf(
    navArgument(Routes.SCAN_SHED_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_DRIVE_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_BATCH_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_TASK_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_SOP_VERSION_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_TASK_ROW_VERSION_ARG) { type = NavType.IntType; defaultValue = 0 },
)

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
