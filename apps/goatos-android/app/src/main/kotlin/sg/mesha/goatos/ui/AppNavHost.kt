package sg.mesha.goatos.ui

import android.widget.Toast
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.paging.LoadState
import androidx.paging.compose.collectAsLazyPagingItems
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
import sg.mesha.goatos.feature.counts.ApprovalEvent
import sg.mesha.goatos.feature.counts.ApprovalScreen
import sg.mesha.goatos.feature.counts.BirthDeathEvent
import sg.mesha.goatos.feature.counts.BirthDeathScreen
import sg.mesha.goatos.feature.counts.CountsEvent
import sg.mesha.goatos.feature.counts.CountsScreen
import sg.mesha.goatos.feature.counts.ShiftingEvent
import sg.mesha.goatos.feature.counts.ShiftingScreen
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
import sg.mesha.goatos.feature.sheds.ShedRow
import sg.mesha.goatos.feature.sheds.ShedStatus
import sg.mesha.goatos.feature.sheds.ShedsEvent
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.SubmitScreen
import sg.mesha.goatos.feature.submit.SyncState
import sg.mesha.goatos.feature.timetable.TimetableScreen
import sg.mesha.goatos.feature.verify.VerifyDetailEvent
import sg.mesha.goatos.feature.verify.VerifyDetailScreen
import sg.mesha.goatos.feature.verify.VerifyQueueEvent
import sg.mesha.goatos.feature.verify.VerifyQueueScreen
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.availableModules
import sg.mesha.goatos.viewmodel.AlertsViewModel
import sg.mesha.goatos.viewmodel.ApprovalViewModel
import sg.mesha.goatos.viewmodel.BirthDeathViewModel
import sg.mesha.goatos.viewmodel.CalendarDayViewModel
import sg.mesha.goatos.viewmodel.CalendarViewModel
import sg.mesha.goatos.viewmodel.CountsViewModel
import sg.mesha.goatos.viewmodel.CoverageBannerViewModel
import sg.mesha.goatos.viewmodel.ProfileViewModel
import sg.mesha.goatos.viewmodel.RecordViewModel
import sg.mesha.goatos.viewmodel.RfidViewModel
import sg.mesha.goatos.viewmodel.ScanViewModel
import sg.mesha.goatos.viewmodel.ShedsViewModel
import sg.mesha.goatos.viewmodel.ShiftingViewModel
import sg.mesha.goatos.viewmodel.SubmitViewModel
import sg.mesha.goatos.viewmodel.TimetableViewModel
import sg.mesha.goatos.viewmodel.VerifyDetailViewModel
import sg.mesha.goatos.viewmodel.VerifyQueueViewModel

// Route ids. The backend nav item hrefs map onto these; unknown hrefs fall through
// to a placeholder rather than crashing (robust static graph).
object Routes {
    const val CALENDAR = "/calendar"
    const val VACCINATION = "/vaccination"
    /**
     * Hosted Calendar child destination. It deliberately differs from the
     * top-level Vaccination module route so a Calendar drill never activates
     * top-level bottom navigation chrome for roles that also have Vaccination
     * in their root navigation.
     */
    const val CALENDAR_DRIVE = "/calendar/drive"
    const val CALENDAR_DRIVE_DATE_ARG = "dateKey"
    const val CALENDAR_DRIVE_HOSTED_ARG = "calendarHosted"
    const val SCAN = "/scan"
    const val SUBMIT = "/submit"
    const val RECORD = "/record"
    /**
     * Profile/settings. Path-shaped like every other route because it is now a BACKEND-composed
     * nav item (`bootstrap_copy.go` — the vaccination and leadership modules each contribute
     * `you`), not client-static chrome the shell appends to the bar. The shell matches it by
     * exact href, so this constant and the backend contribution must agree.
     */
    const val YOU = "/you"
    const val RFID = "/rfid"
    const val ALERTS = "/alerts"
    /** Read-only HRMS shift roster mirror (docs/hr/roster-rbac-design.md) — TRD §14: mobile
     *  never writes positions/leave/backups, all CRUD stays web-only. */
    const val TIMETABLE = "/timetable"

    /**
     * Counts module. All three arrive as the counts module's backend-composed `nav_items`, so all
     * three are L0 roots that own the bottom bar — chrome membership is decided by EXACT route
     * equality against the backend's hrefs (`GoatOsShell.isTopLevelRoute`), never by prefix. That
     * exactness is what keeps [COUNTS_BIRTH_DEATH] and [COUNTS_SHIFTING] from accidentally
     * inheriting (or suppressing) chrome just because they share [COUNTS]'s path prefix.
     */
    const val COUNTS = "/counts"
    const val COUNTS_BIRTH_DEATH = "/counts/birth-death"
    const val COUNTS_SHIFTING = "/counts/shifting"

    /**
     * The approver's pending-decision queue. Contributed by the counts module in the TRAILING
     * bar slot that other modules give to [YOU], and gated on the approval permissions — so an
     * operator never receives it and this route is simply not an L0 root for them. Hiding it is
     * not the access control: `/app/counts/approvals` requires the same permission server-side.
     */
    const val COUNTS_APPROVALS = "/counts/approvals"

    // Standalone Verifier section (context/architecture/verifier-app-and-flow.md). A verifier's
    // bootstrap nav contains ONLY this — see MeshaIcons.forNavKey/GoatOsShell.navItemLabel's
    // "verify" key mapping. VERIFY is the queue; VERIFY_DETAIL drills to one item's video +
    // approve/reject, threading both the item id AND its category (the detail VM re-observes
    // that SAME category's Room cache scope rather than adding a second network call).
    const val VERIFY = "/verify"
    const val VERIFY_ACTION = "/verify/action"
    const val VERIFY_DETAIL = "/verify/item"
    const val VERIFY_ACTION_DETAIL = "/verify/action/item"
    const val VERIFY_ITEM_ARG = "itemId"
    const val VERIFY_CATEGORY_ARG = "category"
    const val VERIFY_ACTION_ARG = "actionMode"
    const val VERIFY_PARK_ARG = "parkId"
    const val VERIFY_SHED_ARG = "shedId"

    fun verifyDetailRoute(
        itemId: String,
        category: String?,
        actionMode: Boolean = false,
        parkId: String? = null,
        shedId: String? = null,
    ): String {
        val args = listOfNotNull(
            VERIFY_ITEM_ARG to itemId,
            category?.takeIf { it.isNotBlank() }?.let { VERIFY_CATEGORY_ARG to it },
            VERIFY_ACTION_ARG to actionMode.toString(),
            parkId?.takeIf { it.isNotBlank() }?.let { VERIFY_PARK_ARG to it },
            shedId?.takeIf { it.isNotBlank() }?.let { VERIFY_SHED_ARG to it },
        )
        val base = if (actionMode) VERIFY_ACTION_DETAIL else VERIFY_DETAIL
        return "$base?" + args.joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
    }

    /** Optional shed-id arg on the record route so a tapped shed opens ITS record. */
    const val RECORD_SHED_ARG = "shedId"

    /** Record route for a specific shed (null → generic first-shed record). */
    fun recordRoute(shedId: String?): String =
        if (shedId.isNullOrBlank()) RECORD else "$RECORD?$RECORD_SHED_ARG=${Uri.encode(shedId)}"

    const val SCAN_SHED_ARG = "shedId"
    const val EXECUTION_DRIVE_ARG = "driveId"
    const val EXECUTION_BATCH_ARG = "batchId"
    const val EXECUTION_TASK_ARG = "taskId"
    const val EXECUTION_SOP_VERSION_ARG = "sopVersionId"
    const val EXECUTION_TASK_ROW_VERSION_ARG = "taskRowVersion"
    const val EXECUTION_SCAN_TITLE_ARG = "scanTitle"

    /** Scan (execute) entry for a shed — threads the shed id so ScanViewModel loads that
     *  shed's per-animal roster from the backend. */
    fun scanRoute(
        shedId: String?,
        driveId: String? = null,
        batchId: String? = null,
        taskId: String? = null,
        sopVersionId: String? = null,
        taskRowVersion: Int? = null,
        scanTitle: String? = null,
    ): String = executionRoute(SCAN, shedId, driveId, batchId, taskId, sopVersionId, taskRowVersion, scanTitle)

    fun submitRoute(
        shedId: String?,
        driveId: String? = null,
        batchId: String? = null,
        taskId: String? = null,
        sopVersionId: String? = null,
        taskRowVersion: Int? = null,
        scanTitle: String? = null,
    ): String = executionRoute(SUBMIT, shedId, driveId, batchId, taskId, sopVersionId, taskRowVersion, scanTitle)

    private fun executionRoute(
        base: String,
        shedId: String?,
        driveId: String?,
        batchId: String?,
        taskId: String?,
        sopVersionId: String?,
        taskRowVersion: Int?,
        scanTitle: String?,
    ): String {
        val args = buildList {
            shedId?.takeIf { it.isNotBlank() }?.let { add(SCAN_SHED_ARG to it) }
            driveId?.takeIf { it.isNotBlank() }?.let { add(EXECUTION_DRIVE_ARG to it) }
            batchId?.takeIf { it.isNotBlank() }?.let { add(EXECUTION_BATCH_ARG to it) }
            taskId?.takeIf { it.isNotBlank() }?.let { add(EXECUTION_TASK_ARG to it) }
            sopVersionId?.takeIf { it.isNotBlank() }?.let { add(EXECUTION_SOP_VERSION_ARG to it) }
            taskRowVersion?.takeIf { it > 0 }?.let { add(EXECUTION_TASK_ROW_VERSION_ARG to it.toString()) }
            scanTitle?.takeIf { it.isNotBlank() }?.let { add(EXECUTION_SCAN_TITLE_ARG to it) }
        }
        if (args.isEmpty()) return base
        return "$base?" + args.joinToString("&") { (key, value) -> "$key=${Uri.encode(value)}" }
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

    fun calendarDriveRoute(dateKey: String?): String =
        buildString {
            append("$CALENDAR_DRIVE?$CALENDAR_DRIVE_HOSTED_ARG=true")
            dateKey?.takeIf { it.isNotBlank() }?.let {
                append("&$CALENDAR_DRIVE_DATE_ARG=${Uri.encode(it)}")
            }
        }

}

/**
 * Maps a backend calendar deep-link ([CalendarItem.target]/[CalendarHistoryRow.target])
 * to an app route. Live shed-scoped work opens the execute loop, explicit `record/...` targets
 * open the read-only record, and anything else falls back to the hosted Calendar
 * drive child. A drill must never reuse a top-level route because that would
 * reactivate root navigation chrome inside the back stack.
 *
 * `internal` (not `private`): [sg.mesha.goatos.push.resolvePushRoute] reuses this SAME
 * backend-href -> route mapping for an FCM push carrying an explicit `target`/`href`, so a
 * notification tap opens exactly where a Calendar tap on the same backend item would.
 */
internal fun calendarTargetRoute(target: String?, fallbackDateKey: String? = null): String {
    val fallbackDriveRoute = Routes.calendarDriveRoute(fallbackDateKey)
    if (target.isNullOrBlank()) return fallbackDriveRoute
    val normalizedTarget = target.substringBefore('?').trimEnd('/')
    if (normalizedTarget == Routes.VACCINATION) return fallbackDriveRoute
    if (target.contains("scan/")) {
        val id = target.substringAfter("scan/").substringBefore('/').substringBefore('?')
        val uri = Uri.parse(target)
        val taskId = uri.getQueryParameter("task_id") ?: uri.getQueryParameter("taskId")
        if (taskId.isNullOrBlank()) return fallbackDriveRoute
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
    val uri = Uri.parse(target)
    val taskId = uri.getQueryParameter("task_id") ?: uri.getQueryParameter("taskId")
    return if (shedId != null && !taskId.isNullOrBlank()) {
        Routes.scanRoute(shedId, taskId = taskId)
    } else fallbackDriveRoute
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

private fun shedExecutionRoute(selected: ShedRow?, fallbackRoute: String): String = when {
    selected == null -> fallbackRoute
    selected.opensRecordOnly -> fallbackRoute
    selected.taskId.isNullOrBlank() -> Routes.recordRoute(selected.shedId)
    else -> Routes.scanRoute(
        shedId = selected.shedId,
        driveId = selected.driveId,
        batchId = selected.batchId,
        taskId = selected.taskId,
        sopVersionId = selected.sopVersionId,
        taskRowVersion = selected.taskRowVersion,
        scanTitle = selected.scanDisplayTitle(),
    )
}

private fun ShedRow.scanDisplayTitle(): String {
    val base = name.takeIf { it.isNotBlank() }
        ?: physicalShed.takeIf { it.isNotBlank() }
        ?: return ""
    val partitionLabel = partition
        .takeIf { it.isNotBlank() }
        ?.let { if (it.startsWith("Part ", ignoreCase = true)) it else "Part $it" }
    val shouldAppendPartition = partitionLabel != null && !base.contains(partitionLabel, ignoreCase = true)
    return if (shouldAppendPartition) "$base - $partitionLabel" else base
}

/**
 * Static navigation graph of every known screen. The graph is fixed; the backend
 * nav (bottom bar + chrome) decides which destinations are *reachable/visible* —
 * the app doesn't invent routes. The first backend-visible root is the landing destination.
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
    startDestination: String = Routes.CALENDAR,
    showProtocolAdherenceCard: Boolean = false,
) {
    // Shared-axis-X motion instead of the default cross-fade: a forward navigation slides
    // the new screen in from the end and the old one out toward the start; Back reverses it.
    // Gives drill-in (Calendar → sheds → Scan → Submit) real directional continuity.
    val motion = tween<Float>(280)
    NavHost(
        navController = navController,
        startDestination = startDestination,
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
            val monthItems = vm.monthItems.collectAsLazyPagingItems()
            // Coverage banner (docs/hr/roster-rbac-design.md S4.6/S4.8) is resolved by a
            // separate small VM and merged into CalendarUiState at this call site. The VM
            // self-loads via GET /app/roster/my-coverage (no scope/identity params needed —
            // the backend resolves the authenticated principal's own coverage) and stays
            // hidden whenever has_coverage is false.
            val coverageVm: CoverageBannerViewModel = hiltViewModel()
            val coverageState by coverageVm.state.collectAsStateWithLifecycle()
            CalendarScreen(
                state = state.copy(coverageBanner = coverageState),
                monthItems = monthItems,
                onEvent = { event ->
                    when (event) {
                        is CalendarEvent.TapItem -> {
                            // The paged row carries its backend target directly; navigation is O(1)
                            // and never searches/copies a growing list in ViewModel memory.
                            vm.onEvent(event)
                            navController.navigate(calendarTargetRoute(event.target, event.dateKey)) { launchSingleTop = true }
                        }
                        // A MONTH-grid day tap opens the day's own L1 screen (real drill),
                        // never an inline sheet under the grid.
                        is CalendarEvent.OpenDay ->
                            navController.navigate(Routes.calendarDayRoute(event.dateKey, event.showCompletedHistory)) { launchSingleTop = true }
                        // A WEEK-strip day tap is in-screen selection (re-scopes the week agenda
                        // list to that day) and must NOT navigate. Handled by CalendarViewModel.
                        is CalendarEvent.TapDay -> vm.onEvent(event)
                        CalendarEvent.Refresh -> {
                            if (state.selectedSegmentId == "month") {
                                // Month has two independent data layers:
                                // - ViewModel-managed overview/metadata/error state.
                                // - Paging-managed schedule rows.
                                //
                                // A transient schedule failure must not leave the user stuck on a
                                // stale top-level error after they tap Retry. Refresh both layers so
                                // the banner/offline state and the paged month rows recover together.
                                vm.onEvent(event)
                                monthItems.refresh()
                            } else {
                                vm.onEvent(event)
                            }
                        }
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
        ) { backStackEntry ->
            val vm: CalendarDayViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val fallbackDateKey = backStackEntry.arguments?.getString(Routes.CALENDAR_DAY_ARG)
            CalendarDayScreen(
                state = state,
                onBack = { navController.popBackStack() },
                onItemTap = { itemId ->
                    val target = state.items.firstOrNull { it.id == itemId }?.target
                    navController.navigate(calendarTargetRoute(target, fallbackDateKey)) { launchSingleTop = true }
                },
                onLoadMore = vm::loadMore,
            )
        }

        // Vaccination execution surfaces as the shed-first flow (screens.md). Tapping a
        // shed opens the execute loop (Scan → Submit) — the operator's core task — with
        // the tapped shed threaded through. Refresh stays in the VM.
        // The same shed-first feature has two distinct navigation identities:
        // - /vaccination is a top-level module destination when bootstrap exposes it.
        // - /calendar/drive is a hosted child pushed from Calendar.
        // Keeping those routes separate is what guarantees L1+ never inherits the
        // bottom bar merely because Vaccination is also a root tab for this actor.
        composable(Routes.VACCINATION) {
            val vm: ShedsViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val context = LocalContext.current
            ShedsScreen(
                state = state,
                showProtocolAdherenceCard = showProtocolAdherenceCard,
                onEvent = { event ->
                    when (event) {
                        is ShedsEvent.OpenShedRecord -> {
                            val selected = state.rows.firstOrNull { it.id == event.shedId }
                            if (selected?.opensRecordOnly == true) {
                                Toast.makeText(context, "${selected.name} already submitted", Toast.LENGTH_SHORT).show()
                                return@ShedsScreen
                            }
                            val route = shedExecutionRoute(selected, Routes.VACCINATION)
                            navController.navigate(route) { launchSingleTop = true }
                        }
                        // /vaccination is an L0 backend nav root for operators. It must never
                        // expose an Up affordance or pop to a previous role/shell state. Hosted
                        // shed queues (/calendar/drive) handle Back in their own route below.
                        ShedsEvent.Back -> Unit
                        else -> vm.onEvent(event)
                    }
                },
            )
        }
        composable(
            route = "${Routes.CALENDAR_DRIVE}?${Routes.CALENDAR_DRIVE_HOSTED_ARG}={${Routes.CALENDAR_DRIVE_HOSTED_ARG}}&${Routes.CALENDAR_DRIVE_DATE_ARG}={${Routes.CALENDAR_DRIVE_DATE_ARG}}",
            arguments = listOf(
                navArgument(Routes.CALENDAR_DRIVE_HOSTED_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
                navArgument(Routes.CALENDAR_DRIVE_DATE_ARG) {
                    type = NavType.StringType
                    nullable = true
                    defaultValue = null
                },
            ),
        ) {
                val vm: ShedsViewModel = hiltViewModel()
                val state by vm.state.collectAsStateWithLifecycle()
                val context = LocalContext.current
                ShedsScreen(
                    state = state,
                    showProtocolAdherenceCard = showProtocolAdherenceCard,
                    onEvent = { event ->
                        when (event) {
                            is ShedsEvent.OpenShedRecord -> {
                                // A leadership oversight read (canOpenShed=false) is read-only:
                                // block the click here so CEO/Director/Park Head never navigate
                                // into the operator scan/execute loop. Operators reach sheds via
                                // the Vaccination (Drives) route, a different composable, and are
                                // unaffected.
                                if (state.canOpenShed) {
                                    // Done sheds open the read-only record; anything still due opens
                                    // the execute loop (Scan → Submit). Mirrors the mock's shed card
                                    // ("View completed record ›" vs "Start / scan").
                                    val selected = state.rows.firstOrNull { it.id == event.shedId }
                                    if (selected?.opensRecordOnly == true) {
                                        Toast.makeText(context, "${selected.name} already submitted", Toast.LENGTH_SHORT).show()
                                        return@ShedsScreen
                                    }
                                    val route = shedExecutionRoute(selected, Routes.CALENDAR_DRIVE)
                                    navController.navigate(route) { launchSingleTop = true }
                                }
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
            LaunchedEffect(vm, state.scanEnabled) {
                vm.setCaptureActive(state.scanEnabled)
            }
            DisposableEffect(vm) {
                onDispose { vm.setCaptureActive(false) }
            }
            val onScanEvent: (ScanEvent) -> Unit = { event ->
                    when (event) {
                        ScanEvent.Submit -> navController.navigate(
                            Routes.submitRoute(
                                shedId = entry.arguments?.getString(Routes.SCAN_SHED_ARG)?.takeIf { it.isNotBlank() }
                                    ?: state.shedId,
                                driveId = entry.arguments?.getString(Routes.EXECUTION_DRIVE_ARG),
                                batchId = entry.arguments?.getString(Routes.EXECUTION_BATCH_ARG),
                                taskId = entry.arguments?.getString(Routes.EXECUTION_TASK_ARG)?.takeIf { it.isNotBlank() }
                                    ?: state.taskId,
                                sopVersionId = entry.arguments?.getString(Routes.EXECUTION_SOP_VERSION_ARG)?.takeIf { it.isNotBlank() }
                                    ?: state.sopVersionId,
                                taskRowVersion = entry.arguments?.getInt(Routes.EXECUTION_TASK_ROW_VERSION_ARG)?.takeIf { it > 0 }
                                    ?: state.taskRowVersion,
                                scanTitle = entry.arguments?.getString(Routes.EXECUTION_SCAN_TITLE_ARG)?.takeIf { it.isNotBlank() },
                            ),
                        ) { launchSingleTop = true }
                        ScanEvent.Back -> navController.popBackStack()
                        ScanEvent.ReconnectReader -> navController.navigate(Routes.RFID) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                }
            if (state.scanEnabled) {
                CaptureAccessGate {
                    BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                    ScanScreen(state = state, onEvent = onScanEvent)
                }
            } else {
                // Verifier/leadership may inspect a shed, but only operators ever receive
                // scanner/camera controls or permission prompts.
                ScanScreen(state = state, onEvent = onScanEvent)
            }
        }

        // Submit — stays put; the VM advances the sync lifecycle (draft → syncing → acked).
        composable(
            route = executionRoutePattern(Routes.SUBMIT),
            arguments = executionNavArguments(),
        ) {
            val vm: SubmitViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
            // Once the submission this operator just enqueued is accepted (ACKED — durably
            // queued/accepted by the backend), return to the vaccination sheds queue instead
            // of stranding them on the acknowledged submit screen. Gated on having actually
            // watched this submission go in-flight this session (QUEUED/SYNCING) so a cold
            // re-entry into an already-completed task does not immediately bounce away.
            var sawSubmitInFlight by rememberSaveable { mutableStateOf(false) }
            LaunchedEffect(state.syncState) {
                when (state.syncState) {
                    SyncState.QUEUED, SyncState.SYNCING -> sawSubmitInFlight = true
                    SyncState.ACKED -> if (sawSubmitInFlight) {
                        sawSubmitInFlight = false
                        navController.popBackStack(Routes.VACCINATION, inclusive = false)
                    }
                    else -> Unit
                }
            }
            if (state.isCaptureRoleBlocked) {
                SubmitScreen(state = state, onEvent = vm::onEvent)
            } else {
                // Capture permissions are operator-only. On Submit, bind the capture source for
                // the whole operator surface instead of trying to infer it from the current form
                // snapshot: form fields arrive asynchronously and SOPs can move proof controls.
                // Binding is cheap when no proof field is present, but a missing binding makes a
                // visible Record/Gallery control silently no-op.
                CaptureAccessGate {
                    SubmitScreen(state = state, onEvent = vm::onEvent)
                }
            }
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

        // --- Counts module -------------------------------------------------------------
        // The census read screen plus its two write forms. All three are backend-composed root
        // destinations; navigating between them is lateral (root -> root), which is why each
        // write form still renders its own Up affordance rather than relying on root chrome.

        composable(Routes.COUNTS) {
            val vm: CountsViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            // Room-backed Paging window: the screen renders one bounded page at a time and
            // Paging prefetches the next as the operator scrolls. No manual load-more.
            val rows = vm.rows.collectAsLazyPagingItems()
            // A page-load failure is reported once per distinct error, next to the cached rows
            // that stay on screen — never as a wipe or a blank wall.
            val refreshError = (rows.loadState.refresh as? LoadState.Error)?.error
            val appendError = (rows.loadState.append as? LoadState.Error)?.error
            LaunchedEffect(refreshError, appendError) {
                (refreshError ?: appendError)?.let(vm::onRowsLoadFailed)
            }
            CountsScreen(
                state = state,
                rows = rows,
                // Birth/Death and Shifting are reached from the module-scoped bottom bar,
                // so this screen owns no navigation of its own.
                onEvent = { event ->
                    when (event) {
                        // Refresh re-runs the mediator against the backend; the VM clears
                        // its summary banner state. The breakdown pages refresh in parallel.
                        CountsEvent.Refresh -> {
                            vm.onEvent(event)
                            rows.refresh()
                        }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(Routes.COUNTS_BIRTH_DEATH) {
            val vm: BirthDeathViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            BirthDeathScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        BirthDeathEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(Routes.COUNTS_SHIFTING) {
            val vm: ShiftingViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            ShiftingScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        ShiftingEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // The approver's queue. An L0 root like the other counts destinations — the backend gates
        // the nav item on the approval permissions, so an operator never receives it and never
        // reaches this route (and `/app/counts/approvals` 403s them server-side regardless).
        composable(Routes.COUNTS_APPROVALS) {
            val vm: ApprovalViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            // Room-backed Paging window: one bounded page at a time, next page prefetched on
            // scroll. No manual load-more, and no whole-backlog pull.
            val rows = vm.rows.collectAsLazyPagingItems()
            val refreshError = (rows.loadState.refresh as? LoadState.Error)?.error
            val appendError = (rows.loadState.append as? LoadState.Error)?.error
            LaunchedEffect(refreshError, appendError) {
                (refreshError ?: appendError)?.let(vm::onRowsLoadFailed)
            }
            ApprovalScreen(
                state = state,
                rows = rows,
                onEvent = { event ->
                    when (event) {
                        // Refresh re-runs the mediator against the backend; the VM only clears
                        // its banner state.
                        ApprovalEvent.Refresh -> {
                            vm.onEvent(event)
                            rows.refresh()
                        }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Standalone Verifier section (context/architecture/verifier-app-and-flow.md): a
        // verifier's bootstrap nav contains ONLY VERIFY, so this is their entire app. A row
        // drills to VERIFY_DETAIL with both the item id and ITS category threaded through, so
        // the detail VM re-observes that exact Room cache scope (no second network round trip).
        composable(
            route = "${Routes.VERIFY}?${Routes.VERIFY_ACTION_ARG}={${Routes.VERIFY_ACTION_ARG}}",
            arguments = listOf(navArgument(Routes.VERIFY_ACTION_ARG) { type = NavType.BoolType; defaultValue = false }),
        ) { entry ->
            val vm: VerifyQueueViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            VerifyQueueScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is VerifyQueueEvent.OpenItem ->
                            navController.navigate(
                                Routes.verifyDetailRoute(
                                    itemId = event.itemId,
                                    category = event.category,
                                    actionMode = state.isActionQueue,
                                    parkId = state.selectedParkId,
                                    shedId = state.selectedShedId,
                                ),
                            ) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(
            route = "${Routes.VERIFY_ACTION}?${Routes.VERIFY_ACTION_ARG}={${Routes.VERIFY_ACTION_ARG}}",
            arguments = listOf(navArgument(Routes.VERIFY_ACTION_ARG) { type = NavType.BoolType; defaultValue = true }),
        ) { entry ->
            val vm: VerifyQueueViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            VerifyQueueScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is VerifyQueueEvent.OpenItem ->
                            navController.navigate(
                                Routes.verifyDetailRoute(
                                    itemId = event.itemId,
                                    category = event.category,
                                    actionMode = true,
                                    parkId = state.selectedParkId,
                                    shedId = state.selectedShedId,
                                ),
                            ) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(
            route = "${Routes.VERIFY_DETAIL}?${Routes.VERIFY_ITEM_ARG}={${Routes.VERIFY_ITEM_ARG}}" +
                "&${Routes.VERIFY_CATEGORY_ARG}={${Routes.VERIFY_CATEGORY_ARG}}" +
                "&${Routes.VERIFY_ACTION_ARG}={${Routes.VERIFY_ACTION_ARG}}" +
                "&${Routes.VERIFY_PARK_ARG}={${Routes.VERIFY_PARK_ARG}}" +
                "&${Routes.VERIFY_SHED_ARG}={${Routes.VERIFY_SHED_ARG}}",
            arguments = listOf(
                navArgument(Routes.VERIFY_ITEM_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_CATEGORY_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_ACTION_ARG) { type = NavType.BoolType; defaultValue = false },
                navArgument(Routes.VERIFY_PARK_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_SHED_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
            ),
        ) {
            val vm: VerifyDetailViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            LaunchedEffect(state.autoCloseAfterDecision) {
                if (state.autoCloseAfterDecision) {
                    navController.popBackStack()
                }
            }
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

        composable(
            route = "${Routes.VERIFY_ACTION_DETAIL}?${Routes.VERIFY_ITEM_ARG}={${Routes.VERIFY_ITEM_ARG}}" +
                "&${Routes.VERIFY_CATEGORY_ARG}={${Routes.VERIFY_CATEGORY_ARG}}" +
                "&${Routes.VERIFY_ACTION_ARG}={${Routes.VERIFY_ACTION_ARG}}" +
                "&${Routes.VERIFY_PARK_ARG}={${Routes.VERIFY_PARK_ARG}}" +
                "&${Routes.VERIFY_SHED_ARG}={${Routes.VERIFY_SHED_ARG}}",
            arguments = listOf(
                navArgument(Routes.VERIFY_ITEM_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_CATEGORY_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_ACTION_ARG) { type = NavType.BoolType; defaultValue = true },
                navArgument(Routes.VERIFY_PARK_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
                navArgument(Routes.VERIFY_SHED_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
            ),
        ) {
            val vm: VerifyDetailViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            LaunchedEffect(state.autoCloseAfterDecision) {
                if (state.autoCloseAfterDecision) {
                    navController.popBackStack()
                }
            }
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

/**
 * Cold start must never land on a route the backend did not expose to this principal, and it
 * must honor the DEFAULT MODULE's landing href, not merely the first bottom-bar item.
 *
 * Precedence: the backend's default (first available) module's href, then the first supported
 * bottom-bar item, then Calendar as a safe fallback. Operator/verifier are unaffected -- their
 * default module href IS their landing (/vaccination, /verify). The `in supportedRootDestinations`
 * guard keeps an unknown future root from failing startup with a 403.
 */
internal fun startDestinationFor(navState: NavState): String {
    navState.availableModules().firstOrNull()?.href
        ?.takeIf { it in supportedRootDestinations }
        ?.let { return it }
    return navState.items.firstOrNull { it.href in supportedRootDestinations }?.href
        ?: Routes.CALENDAR
}

private val supportedRootDestinations = setOf(
    Routes.CALENDAR,
    Routes.VACCINATION,
    Routes.VERIFY,
    Routes.VERIFY_ACTION,
    Routes.YOU,
    Routes.ALERTS,
    Routes.TIMETABLE,
    // Counts roots: a Counts-only principal's default landing is the first page they may
    // open (/counts census for CEO/admin, /counts/birth-death for a capture operator).
    // These are registered top-level composables, so cold start must accept them instead
    // of falling back to Calendar (which a Counts-only principal may not be granted).
    Routes.COUNTS,
    Routes.COUNTS_BIRTH_DEATH,
    Routes.COUNTS_SHIFTING,
    Routes.COUNTS_APPROVALS,
)

private fun executionRoutePattern(base: String): String =
    "$base?${Routes.SCAN_SHED_ARG}={${Routes.SCAN_SHED_ARG}}" +
        "&${Routes.EXECUTION_DRIVE_ARG}={${Routes.EXECUTION_DRIVE_ARG}}" +
        "&${Routes.EXECUTION_BATCH_ARG}={${Routes.EXECUTION_BATCH_ARG}}" +
        "&${Routes.EXECUTION_TASK_ARG}={${Routes.EXECUTION_TASK_ARG}}" +
        "&${Routes.EXECUTION_SOP_VERSION_ARG}={${Routes.EXECUTION_SOP_VERSION_ARG}}" +
        "&${Routes.EXECUTION_TASK_ROW_VERSION_ARG}={${Routes.EXECUTION_TASK_ROW_VERSION_ARG}}" +
        "&${Routes.EXECUTION_SCAN_TITLE_ARG}={${Routes.EXECUTION_SCAN_TITLE_ARG}}"

private fun executionNavArguments() = listOf(
    navArgument(Routes.SCAN_SHED_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_DRIVE_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_BATCH_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_TASK_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_SOP_VERSION_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
    navArgument(Routes.EXECUTION_TASK_ROW_VERSION_ARG) { type = NavType.IntType; defaultValue = 0 },
    navArgument(Routes.EXECUTION_SCAN_TITLE_ARG) { type = NavType.StringType; nullable = true; defaultValue = null },
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
