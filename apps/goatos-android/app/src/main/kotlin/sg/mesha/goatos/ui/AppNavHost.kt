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
import androidx.compose.ui.res.stringResource
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.collectAsLazyPagingItems
import android.net.Uri
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import kotlinx.coroutines.delay
import sg.mesha.goatos.R
import sg.mesha.goatos.capture.BindPhotoCaptureSource
import sg.mesha.goatos.capture.BindVideoCaptureSource
import sg.mesha.goatos.capture.CaptureAccessGate
import sg.mesha.goatos.capture.rememberDelegatingPhotoCaptureSource
import sg.mesha.goatos.capture.rememberDelegatingProofCaptureSource
import sg.mesha.goatos.feature.calendar.CalendarDayScreen
import sg.mesha.goatos.feature.calendar.CalendarEvent
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.counts.AddBirthEvent
import sg.mesha.goatos.feature.counts.AddBirthScreen
import sg.mesha.goatos.feature.counts.AddDeathEvent
import sg.mesha.goatos.feature.counts.AddDeathScreen
import sg.mesha.goatos.feature.counts.CountsEvent
import sg.mesha.goatos.feature.counts.CountsScreen
import sg.mesha.goatos.feature.counts.MilkPreparationScreen
import sg.mesha.goatos.feature.counts.MilkPreparationListEvent
import sg.mesha.goatos.feature.counts.MilkPreparationListScreen
import sg.mesha.goatos.feature.counts.MilkPreparationEvent
import sg.mesha.goatos.feature.counts.MilkFeedingEvent
import sg.mesha.goatos.feature.counts.MilkFeedingScreen
import sg.mesha.goatos.feature.counts.MilkFeedingListScreen
import sg.mesha.goatos.feature.counts.MilkFeedingListEvent
import sg.mesha.goatos.feature.feed.FeedDirectionEvent
import sg.mesha.goatos.feature.feed.FeedCompleteEvent
import sg.mesha.goatos.feature.feed.FeedCompleteScreen
import sg.mesha.goatos.feature.feed.FeedDistributionCompleteScreen
import sg.mesha.goatos.feature.feed.FeedPackingCompleteEvent
import sg.mesha.goatos.feature.feed.FeedPackingCompleteScreen
import sg.mesha.goatos.feature.feed.FeedPackingCompleteStatus
import sg.mesha.goatos.feature.feed.FeedDistributionEvent
import sg.mesha.goatos.feature.feed.FeedDistributionStatus
import sg.mesha.goatos.feature.feed.FeedDirectionScreen
import sg.mesha.goatos.feature.feed.FeedPackingEvent
import sg.mesha.goatos.feature.feed.FeedPackingScreen
import sg.mesha.goatos.feature.feed.FeedTransportCaptureEvent
import sg.mesha.goatos.feature.feed.FeedTransportCaptureScreen
import sg.mesha.goatos.feature.feed.FeedTransportEvent
import sg.mesha.goatos.feature.feed.FeedTransportScreen
import sg.mesha.goatos.feature.feed.FeedTransportSubmitStatus
import sg.mesha.goatos.feature.counts.ShiftingEvent
import sg.mesha.goatos.feature.counts.ShiftingExecuteEvent
import sg.mesha.goatos.feature.counts.ShiftingExecuteScreen
import sg.mesha.goatos.feature.counts.ShiftingActionsScreen
import sg.mesha.goatos.feature.counts.RfidPromoteEvent
import sg.mesha.goatos.feature.counts.RfidPromoteScreen
import sg.mesha.goatos.feature.counts.ShiftingPendingEvent
import sg.mesha.goatos.feature.counts.ShiftingScreen
import sg.mesha.goatos.feature.counts.WorkflowDetailEvent
import sg.mesha.goatos.feature.counts.WorkflowDetailScreen
import sg.mesha.goatos.feature.counts.WorkflowListEvent
import sg.mesha.goatos.feature.counts.WorkflowListScreen
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
import sg.mesha.goatos.viewmodel.AddBirthViewModel
import sg.mesha.goatos.viewmodel.AddDeathViewModel
import sg.mesha.goatos.viewmodel.AlertsViewModel
import sg.mesha.goatos.viewmodel.BirthWorkflowListViewModel
import sg.mesha.goatos.viewmodel.CalendarDayViewModel
import sg.mesha.goatos.viewmodel.DeathWorkflowListViewModel
import sg.mesha.goatos.viewmodel.WorkflowDetailViewModel
import sg.mesha.goatos.viewmodel.WorkflowListViewModel
import sg.mesha.goatos.viewmodel.CalendarViewModel
import sg.mesha.goatos.viewmodel.CountsViewModel
import sg.mesha.goatos.viewmodel.MilkPreparationViewModel
import sg.mesha.goatos.viewmodel.MilkPreparationListViewModel
import sg.mesha.goatos.viewmodel.MilkFeedingViewModel
import sg.mesha.goatos.viewmodel.MilkFeedingListViewModel
import sg.mesha.goatos.viewmodel.FeedCompleteViewModel
import sg.mesha.goatos.viewmodel.FeedDistributionCompleteViewModel
import sg.mesha.goatos.viewmodel.FeedPackingCompleteViewModel
import sg.mesha.goatos.viewmodel.FeedDirectionViewModel
import sg.mesha.goatos.viewmodel.FeedPackingViewModel
import sg.mesha.goatos.viewmodel.FeedTransportCaptureViewModel
import sg.mesha.goatos.viewmodel.FeedTransportViewModel
import sg.mesha.goatos.viewmodel.CoverageBannerViewModel
import sg.mesha.goatos.viewmodel.ProfileViewModel
import sg.mesha.goatos.viewmodel.RecordViewModel
import sg.mesha.goatos.viewmodel.RfidPromoteViewModel
import sg.mesha.goatos.viewmodel.RfidViewModel
import sg.mesha.goatos.viewmodel.ScanViewModel
import sg.mesha.goatos.viewmodel.ShedsViewModel
import sg.mesha.goatos.viewmodel.ShiftingExecuteViewModel
import sg.mesha.goatos.viewmodel.ShiftingPendingViewModel
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
     * Counts module. All four arrive as the counts module's backend-composed `nav_items`, so all
     * four are L0 roots that own the bottom bar — chrome membership is decided by EXACT route
     * equality against the backend's hrefs (`GoatOsShell.isTopLevelRoute`), never by prefix. That
     * exactness is what keeps [COUNTS_BIRTH]/[COUNTS_DEATH] (and their /add + /workflows drills)
     * and [COUNTS_SHIFTING] from accidentally inheriting (or suppressing) chrome just because they
     * share [COUNTS]'s path prefix.
     *
     * Birth and Death are TWO modules with their own routes (maintainer decision 2026-07-27,
     * docs/decisions/birth-death-workflows.md): each opens on the outstanding per-goat SOP work
     * list; recording moves behind the ＋ button. The old combined `/counts/birth-death` form
     * route is REMOVED from navigation.
     */
    const val COUNTS = "/counts"
    const val COUNTS_BIRTH = "/counts/birth"
    const val COUNTS_DEATH = "/counts/death"
    const val COUNTS_SHIFTING = "/counts/shifting"
    const val COUNTS_MILK_PREPARATION = "/counts/milk-preparation"
    const val MILK_PREPARATION_PARK_ID_ARG = "park_id"
    const val COUNTS_MILK_PREPARATION_DETAIL = "/counts/milk-preparation/farms/{$MILK_PREPARATION_PARK_ID_ARG}"
    fun milkPreparationDetailRoute(parkId: String): String = "/counts/milk-preparation/farms/$parkId"
    const val COUNTS_MILK_FEEDING = "/counts/milk-feeding"
    const val MILK_FEEDING_TASK_ID_ARG = "task_id"
    const val COUNTS_MILK_FEEDING_DETAIL = "/counts/milk-feeding/tasks/{$MILK_FEEDING_TASK_ID_ARG}"
    fun milkFeedingDetailRoute(taskId: String): String = "/counts/milk-feeding/tasks/$taskId"

    // L1 drill-ins for one workflow card (distinct hosted destinations with Up/Back and no root
    // chrome — never a prefix reuse of the L0 roots above). Each module keeps its own drill route
    // so Back always lands on the module the operator came from.
    const val WORKFLOW_ID_ARG = "workflow_id"
    const val COUNTS_BIRTH_WORKFLOW = "/counts/birth/workflows/{$WORKFLOW_ID_ARG}"
    const val COUNTS_DEATH_WORKFLOW = "/counts/death/workflows/{$WORKFLOW_ID_ARG}"
    fun birthWorkflowRoute(workflowId: String): String = "/counts/birth/workflows/$workflowId"
    fun deathWorkflowRoute(workflowId: String): String = "/counts/death/workflows/$workflowId"

    // L1 add forms behind each module's ＋ button (hosted destinations with Up/Back, no root chrome).
    const val COUNTS_BIRTH_ADD = "/counts/birth/add"
    const val COUNTS_DEATH_ADD = "/counts/death/add"
    const val COUNTS_SHIFTING_ADD = "/counts/shifting/add"
    const val COUNTS_SHIFTING_SUBMISSION_NOTICE = "counts_shifting_submission_notice"
    const val COUNTS_BIRTH_SUBMISSION_NOTICE = "counts.birth.submissionNotice"

    // The L1 execute destination for one approved movement from Shifting Actions. A
    // distinct hosted destination with Up/Back and no root chrome (Android navigation-stack
    // invariant) — NOT a prefix of COUNTS_SHIFTING reused as a drill target.
    const val COUNTS_SHIFTING_EXECUTE_ARG = "shifting_event_id"
    const val COUNTS_SHIFTING_EXECUTE = "/counts/shifting/execute/{$COUNTS_SHIFTING_EXECUTE_ARG}"
    fun shiftingExecuteRoute(shiftingEventId: String): String =
        "/counts/shifting/execute/$shiftingEventId"

    // Birth's final "Tag the kid" destination. It is reachable only from one kid workflow and is
    // never a Counts root/navigation item.
    const val COUNTS_PROMOTE_GOAT_ARG = "goat_id"
    const val COUNTS_PROMOTE_DISPLAY_ARG = "display_id"
    const val COUNTS_PROMOTE_TEMP_ARG = "temporary_identifier"
    const val COUNTS_PROMOTE_LOCATION_ARG = "location_display"
    const val COUNTS_PROMOTE_ROW_VERSION_ARG = "row_version"
    const val COUNTS_PROMOTE_GOAT =
        "/counts/birth/tag/{$COUNTS_PROMOTE_GOAT_ARG}" +
            "?$COUNTS_PROMOTE_DISPLAY_ARG={$COUNTS_PROMOTE_DISPLAY_ARG}" +
            "&$COUNTS_PROMOTE_TEMP_ARG={$COUNTS_PROMOTE_TEMP_ARG}" +
            "&$COUNTS_PROMOTE_LOCATION_ARG={$COUNTS_PROMOTE_LOCATION_ARG}" +
            "&$COUNTS_PROMOTE_ROW_VERSION_ARG={$COUNTS_PROMOTE_ROW_VERSION_ARG}"

    fun promoteGoatRoute(
        goatId: String,
        displayId: String,
        temporaryIdentifier: String,
        locationDisplay: String,
        rowVersion: Int,
    ): String = "/counts/birth/tag/${Uri.encode(goatId)}" +
        "?$COUNTS_PROMOTE_DISPLAY_ARG=${Uri.encode(displayId)}" +
        "&$COUNTS_PROMOTE_TEMP_ARG=${Uri.encode(temporaryIdentifier)}" +
        "&$COUNTS_PROMOTE_LOCATION_ARG=${Uri.encode(locationDisplay)}" +
        "&$COUNTS_PROMOTE_ROW_VERSION_ARG=$rowVersion"

    // Feed module bar (backend module `feed_direction`). Both are L0 roots and match the
    // backend-composed nav hrefs verbatim, so the module bottom bar navigates straight to them.
    const val FEED_DIRECTION = "/feed/direction"
    const val FEED_PACKING = "/feed/packing"
    const val FEED_TRANSPORT = "/feed/transport"
    const val FEED_TRANSPORT_CAPTURE = "/feed/transport/task/{task_id}/{shed_id}?shed_label={shed_label}"
    fun feedTransportCaptureRoute(taskId:String,shedId:String,shedLabel:String)="/feed/transport/task/${Uri.encode(taskId)}/${Uri.encode(shedId)}?shed_label=${Uri.encode(shedLabel)}"

    // L2 feed-direction completion detail, reached by tapping a shed-session row on either feed
    // screen. Path args are the completion grain; labels are query args (URL-encoded, may contain
    // spaces). Distinct route from the two L0 feed roots (never a prefix reuse).
    const val FEED_COMPLETE =
        "/feed/complete/{park_id}/{shed_id}/{session_no}/{workflow}/{target_date}?shed_label={shed_label}&session_label={session_label}"

    fun feedCompleteRoute(
        parkId: String,
        shedId: String,
        sessionNo: Int,
        workflow: String,
        targetDate: String,
        shedLabel: String,
        sessionLabel: String,
    ): String {
        fun e(value: String): String = Uri.encode(value)
        val park = parkId.ifBlank { "-" }
        return "/feed/complete/${e(park)}/${e(shedId)}/$sessionNo/${e(workflow)}/${e(targetDate)}" +
            "?shed_label=${e(shedLabel)}&session_label=${e(sessionLabel)}"
    }

    // L2 verifier-GATED feed-DISTRIBUTION completion (docs/decisions/feed-distribution-verification.md),
    // reached ONLY by tapping a shed-session row on Feed DIRECTION. Distinct route from the two L0 feed
    // roots and from [FEED_COMPLETE] (the untouched Packing/direction-shared completion) — never a
    // prefix reuse. Same grain args as [FEED_COMPLETE]; the operator records BOTH mandatory proofs here.
    const val FEED_DISTRIBUTION_COMPLETE =
        "/feed/distribution/complete/{park_id}/{shed_id}/{session_no}/{workflow}/{target_date}?shed_label={shed_label}&session_label={session_label}"

    fun feedDistributionCompleteRoute(
        parkId: String,
        shedId: String,
        sessionNo: Int,
        workflow: String,
        targetDate: String,
        shedLabel: String,
        sessionLabel: String,
    ): String {
        fun e(value: String): String = Uri.encode(value)
        val park = parkId.ifBlank { "-" }
        return "/feed/distribution/complete/${e(park)}/${e(shedId)}/$sessionNo/${e(workflow)}/${e(targetDate)}" +
            "?shed_label=${e(shedLabel)}&session_label=${e(sessionLabel)}"
    }

    // L2 verifier-GATED feed-PACKING completion, reached ONLY by tapping a shed-session row on Feed
    // PACKING. Distinct route from the two L0 feed roots, [FEED_COMPLETE] (the untouched packing/
    // direction-shared completion), and [FEED_DISTRIBUTION_COMPLETE] — never a prefix reuse. Same
    // grain args; simpler than distribution — the operator records ONE mandatory proof here.
    const val FEED_PACKING_COMPLETE =
        "/feed/packing/complete/{park_id}/{shed_id}/{session_no}/{workflow}/{target_date}?shed_label={shed_label}&session_label={session_label}"

    fun feedPackingCompleteRoute(
        parkId: String,
        shedId: String,
        sessionNo: Int,
        workflow: String,
        targetDate: String,
        shedLabel: String,
        sessionLabel: String,
    ): String {
        fun e(value: String): String = Uri.encode(value)
        val park = parkId.ifBlank { "-" }
        return "/feed/packing/complete/${e(park)}/${e(shedId)}/$sessionNo/${e(workflow)}/${e(targetDate)}" +
            "?shed_label=${e(shedLabel)}&session_label=${e(sessionLabel)}"
    }

    /**
     * The approver's pending-decision queue. Contributed by the counts module in the TRAILING
     * bar slot that other modules give to [YOU], and gated on the approval permissions — so an
     * operator never receives it and this route is simply not an L0 root for them. Hiding it is
     * not the access control: `/app/counts/approvals` requires the same permission server-side.
     */

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
    return scanDisplayTitle(name = name, physicalShed = physicalShed, partition = partition)
}

internal fun scanDisplayTitle(name: String, physicalShed: String, partition: String): String {
    val base = name.takeIf { it.isNotBlank() }
        ?: physicalShed.takeIf { it.isNotBlank() }
        ?: return ""
    val partitionLabel = partition
        .takeIf { it.isNotBlank() }
        .takeUnless { it.equals("whole", ignoreCase = true) }
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
                            if (selected?.canOpen == false) {
                                Toast.makeText(context, "${selected.name} is scheduled for ${selected.scheduleDateLabel}", Toast.LENGTH_SHORT).show()
                                return@ShedsScreen
                            }
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
                                    if (selected?.canOpen == false) {
                                        Toast.makeText(context, "${selected.name} is scheduled for ${selected.scheduleDateLabel}", Toast.LENGTH_SHORT).show()
                                        return@ShedsScreen
                                    }
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
            DisposableEffect(vm) {
                vm.setCompletionKeySwallowActive(true)
                vm.setCaptureActive(true)
                onDispose {
                    vm.setCompletionKeySwallowActive(false)
                    vm.setCaptureActive(false)
                }
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
            if (state.captureAccessRequired) {
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
            val context = LocalContext.current
            val submitSuccessToast = stringResource(R.string.submit_shed_success_toast)
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
                        Toast.makeText(context, submitSuccessToast, Toast.LENGTH_SHORT).show()
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

        composable(Routes.COUNTS_MILK_PREPARATION) {
            val vm: MilkPreparationListViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            MilkPreparationListScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        is MilkPreparationListEvent.OpenFarm -> navController.navigate(
                            Routes.milkPreparationDetailRoute(event.parkId),
                        ) { launchSingleTop = true }
                        MilkPreparationListEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(
            route = Routes.COUNTS_MILK_PREPARATION_DETAIL,
            arguments = listOf(navArgument(Routes.MILK_PREPARATION_PARK_ID_ARG) { type = NavType.StringType }),
        ) {
            val vm: MilkPreparationViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                MilkPreparationScreen(
                    state = state,
                    onEvent = { event ->
                        when (event) {
                            MilkPreparationEvent.Back -> navController.popBackStack()
                            else -> vm.onEvent(event)
                        }
                    },
                )
            }
        }

        composable(Routes.COUNTS_MILK_FEEDING) {
            val vm: MilkFeedingListViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            MilkFeedingListScreen(state, onEvent = { event -> when (event) {
                is MilkFeedingListEvent.OpenTask -> navController.navigate(Routes.milkFeedingDetailRoute(event.taskId)) { launchSingleTop = true }
                MilkFeedingListEvent.Back -> navController.popBackStack()
                else -> vm.onEvent(event)
            } })
        }

        composable(
            route = Routes.COUNTS_MILK_FEEDING_DETAIL,
            arguments = listOf(navArgument(Routes.MILK_FEEDING_TASK_ID_ARG) { type = NavType.StringType }),
        ) {
            val vm: MilkFeedingViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                MilkFeedingScreen(state, onEvent = { event -> if (event == MilkFeedingEvent.Back) navController.popBackStack() else vm.onEvent(event) })
            }
        }

        // Birth / Death workflow modules (docs/decisions/birth-death-workflows.md): two L0 work
        // lists, each with its own drill-in and add form. The list composable wires Paging load
        // states into the VM so refresh/offline banners track the real page loads.
        composable(Routes.COUNTS_BIRTH) { backStackEntry ->
            val vm: BirthWorkflowListViewModel = hiltViewModel()
            val returnedSubmissionNotice by backStackEntry.savedStateHandle
                .getStateFlow<String?>(Routes.COUNTS_BIRTH_SUBMISSION_NOTICE, null)
                .collectAsStateWithLifecycle()
            WorkflowListDestination(
                vm = vm,
                submissionNotice = returnedSubmissionNotice,
                onOpenCard = { workflowId ->
                    navController.navigate(Routes.birthWorkflowRoute(workflowId)) { launchSingleTop = true }
                },
                onAddNew = { navController.navigate(Routes.COUNTS_BIRTH_ADD) { launchSingleTop = true } },
                onBack = { navController.popBackStack() },
            )
        }

        composable(Routes.COUNTS_DEATH) {
            val vm: DeathWorkflowListViewModel = hiltViewModel()
            WorkflowListDestination(
                vm = vm,
                onOpenCard = { workflowId ->
                    navController.navigate(Routes.deathWorkflowRoute(workflowId)) { launchSingleTop = true }
                },
                onAddNew = { navController.navigate(Routes.COUNTS_DEATH_ADD) { launchSingleTop = true } },
                onBack = { navController.popBackStack() },
            )
        }

        // L1 workflow drill-ins: one goat's SOP action list. The camera is bound only while
        // composed (operator capture role gated) so requires_video actions can record; the
        // `tag_the_kid` action navigates to the existing L2 promote form instead of posting.
        listOf(Routes.COUNTS_BIRTH_WORKFLOW, Routes.COUNTS_DEATH_WORKFLOW).forEach { route ->
            composable(
                route = route,
                arguments = listOf(navArgument(Routes.WORKFLOW_ID_ARG) { type = NavType.StringType }),
            ) {
                val vm: WorkflowDetailViewModel = hiltViewModel()
                val state by vm.state.collectAsStateWithLifecycle()
                val onEvent: (WorkflowDetailEvent) -> Unit = { event ->
                    when (event) {
                        WorkflowDetailEvent.Back -> navController.popBackStack()
                        is WorkflowDetailEvent.OpenPromote -> {
                            vm.onEvent(event)
                            navController.navigate(
                                Routes.promoteGoatRoute(
                                    goatId = event.goatId,
                                    displayId = event.displayId,
                                    temporaryIdentifier = event.temporaryIdentifier,
                                    locationDisplay = event.locationDisplay,
                                    rowVersion = event.rowVersion,
                                ),
                            ) { launchSingleTop = true }
                        }
                        else -> vm.onEvent(event)
                    }
                }
                CaptureAccessGate {
                    BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                    WorkflowDetailScreen(state = state, onEvent = onEvent)
                }
            }
        }

        // L1 add forms behind each module's ＋.
        composable(Routes.COUNTS_BIRTH_ADD) {
            val vm: AddBirthViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            LaunchedEffect(state.returnToBirthList, state.submissionNotice) {
                if (state.returnToBirthList) {
                    navController.previousBackStackEntry?.savedStateHandle?.set(
                        Routes.COUNTS_BIRTH_SUBMISSION_NOTICE,
                        state.submissionNotice,
                    )
                    vm.onEvent(AddBirthEvent.NavigationHandled)
                    navController.popBackStack()
                }
            }
            AddBirthScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        AddBirthEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(Routes.COUNTS_DEATH_ADD) {
            val vm: AddDeathViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            AddDeathScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        AddDeathEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Shifting opens on Actions: the web-approved execution queue. Raising a movement lives
        // behind the top-right ＋ and opens the separate hosted form below, matching Birth/Death.
        composable(Routes.COUNTS_SHIFTING) { backStackEntry ->
            val pendingVm: ShiftingPendingViewModel = hiltViewModel()
            val pendingState by pendingVm.state.collectAsStateWithLifecycle()
            val submissionNotice = remember(backStackEntry) {
                backStackEntry.savedStateHandle
                    .remove<String>(Routes.COUNTS_SHIFTING_SUBMISSION_NOTICE)
            }
            val pendingRows = pendingVm.rows.collectAsLazyPagingItems()
            val refreshState = pendingRows.loadState.refresh
            LaunchedEffect(refreshState) {
                when (refreshState) {
                    is LoadState.Error -> pendingVm.onRowsLoadFailed(refreshState.error)
                    is LoadState.NotLoading -> pendingVm.onRowsLoaded()
                    else -> Unit
                }
            }
            val appendError = (pendingRows.loadState.append as? LoadState.Error)?.error
            LaunchedEffect(appendError) { appendError?.let(pendingVm::onRowsLoadFailed) }

            ShiftingActionsScreen(
                state = pendingState.copy(submissionNotice = submissionNotice),
                rows = pendingRows,
                onEvent = { event ->
                    when (event) {
                        is ShiftingPendingEvent.OpenMovement ->
                            navController.navigate(Routes.shiftingExecuteRoute(event.shiftingEventId)) {
                                launchSingleTop = true
                            }
                        ShiftingPendingEvent.Raise ->
                            navController.navigate(Routes.COUNTS_SHIFTING_ADD) { launchSingleTop = true }
                        ShiftingPendingEvent.Back -> navController.popBackStack()
                        ShiftingPendingEvent.Refresh -> {
                            pendingVm.onEvent(event)
                            pendingRows.refresh()
                        }
                        else -> pendingVm.onEvent(event)
                    }
                },
            )
        }

        // L1 raise form behind Shifting's ＋ action.
        composable(Routes.COUNTS_SHIFTING_ADD) {
            val vm: ShiftingViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            LaunchedEffect(state.returnToActions) {
                if (state.returnToActions) {
                    navController.previousBackStackEntry?.savedStateHandle?.set(
                        Routes.COUNTS_SHIFTING_SUBMISSION_NOTICE,
                        state.submissionNotice ?: "Shifting raised successfully.",
                    )
                    vm.onEvent(ShiftingEvent.NavigationHandled)
                    navController.popBackStack()
                }
            }
            ShiftingScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        ShiftingEvent.Back -> navController.popBackStack()
                        ShiftingEvent.NavigationHandled -> vm.onEvent(event)
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // L1 execute destination: do the physical move, record its mandatory video, then submit.
        composable(
            route = Routes.COUNTS_SHIFTING_EXECUTE,
            arguments = listOf(navArgument(Routes.COUNTS_SHIFTING_EXECUTE_ARG) { type = NavType.StringType }),
        ) {
            val vm: ShiftingExecuteViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val onEvent: (ShiftingExecuteEvent) -> Unit = { event ->
                when (event) {
                    ShiftingExecuteEvent.Back -> navController.popBackStack()
                    else -> vm.onEvent(event)
                }
            }
            // Bind the live camera only while this screen is composed (operator capture role gated),
            // so the optional "Record a video" works and the camera releases on leave.
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                ShiftingExecuteScreen(state = state, onEvent = onEvent)
            }
        }

        // Birth-owned tag form: assign the permanent RFID to this workflow's canonical kid.
        composable(
            route = Routes.COUNTS_PROMOTE_GOAT,
            arguments = listOf(
                navArgument(Routes.COUNTS_PROMOTE_GOAT_ARG) { type = NavType.StringType },
                navArgument(Routes.COUNTS_PROMOTE_DISPLAY_ARG) { type = NavType.StringType; defaultValue = "" },
                navArgument(Routes.COUNTS_PROMOTE_TEMP_ARG) { type = NavType.StringType; defaultValue = "" },
                navArgument(Routes.COUNTS_PROMOTE_LOCATION_ARG) { type = NavType.StringType; defaultValue = "" },
                navArgument(Routes.COUNTS_PROMOTE_ROW_VERSION_ARG) { type = NavType.IntType; defaultValue = 0 },
            ),
        ) {
            val vm: RfidPromoteViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            RfidPromoteScreen(
                state = state,
                onEvent = { event ->
                    when (event) {
                        RfidPromoteEvent.Back -> navController.popBackStack()
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        // Feed module bar: two L0 read screens. Both render a bounded Room-backed Paging window
        // (docs/decisions/mobile-data-fetch-anti-patterns.md) and own no navigation of their own —
        // Feed Direction and Feed Packing are reached from the module-scoped bottom bar the shell
        // renders from the backend nav.
        composable(Routes.FEED_DIRECTION) {
            val vm: FeedDirectionViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val rows = vm.rows.collectAsLazyPagingItems()
            val refreshError = (rows.loadState.refresh as? LoadState.Error)?.error
            val appendError = (rows.loadState.append as? LoadState.Error)?.error
            LaunchedEffect(refreshError, appendError) {
                (refreshError ?: appendError)?.let(vm::onRowsLoadFailed)
            }
            FeedDirectionScreen(
                state = state,
                rows = rows,
                onEvent = { event ->
                    when (event) {
                        FeedDirectionEvent.Refresh -> {
                            vm.onEvent(event)
                            rows.refresh()
                        }
                        // Direction rows open the verifier-GATED distribution flow (two mandatory
                        // proofs -> pending_verification). Packing rows (below) keep the untouched
                        // instant FeedCompleteScreen — docs/decisions/feed-distribution-verification.md.
                        is FeedDirectionEvent.OpenRow -> navController.navigate(
                            Routes.feedDistributionCompleteRoute(
                                parkId = event.parkId,
                                shedId = event.shedId,
                                sessionNo = event.sessionNo,
                                workflow = event.workflow,
                                targetDate = state.targetDateLabel,
                                shedLabel = event.shedLabel,
                                sessionLabel = event.sessionLabel,
                            ),
                        ) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(Routes.FEED_PACKING) {
            val vm: FeedPackingViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val rows = vm.rows.collectAsLazyPagingItems()
            val refreshError = (rows.loadState.refresh as? LoadState.Error)?.error
            val appendError = (rows.loadState.append as? LoadState.Error)?.error
            LaunchedEffect(refreshError, appendError) {
                (refreshError ?: appendError)?.let(vm::onRowsLoadFailed)
            }
            FeedPackingScreen(
                state = state,
                rows = rows,
                onEvent = { event ->
                    when (event) {
                        FeedPackingEvent.Refresh -> {
                            vm.onEvent(event)
                            rows.refresh()
                        }
                        // Packing rows open the verifier-GATED packing flow (ONE mandatory proof ->
                        // pending_verification), mirroring Direction rows above. [FEED_COMPLETE] (the
                        // untouched instant path) stays in the graph but is no longer reached from here.
                        is FeedPackingEvent.OpenRow -> navController.navigate(
                            Routes.feedPackingCompleteRoute(
                                parkId = event.parkId,
                                shedId = event.shedId,
                                sessionNo = event.sessionNo,
                                workflow = event.workflow,
                                // The completion records the FEED day (= packing day + 1), matching the
                                // read query; targetDateLabel is the packing-day axis, feedForDateLabel is
                                // the feed day the backend keys on.
                                targetDate = state.feedForDateLabel,
                                shedLabel = event.shedLabel,
                                sessionLabel = event.sessionLabel,
                            ),
                        ) { launchSingleTop = true }
                        else -> vm.onEvent(event)
                    }
                },
            )
        }

        composable(Routes.FEED_TRANSPORT){val vm:FeedTransportViewModel=hiltViewModel();val state by vm.state.collectAsStateWithLifecycle();FeedTransportScreen(state){event->if(event is FeedTransportEvent.Open)navController.navigate(Routes.feedTransportCaptureRoute(event.row.taskId,event.row.shedId,event.row.shedLabel))else vm.onEvent(event)}}

        composable(
            route = Routes.FEED_TRANSPORT_CAPTURE,
            arguments = listOf(
                navArgument(FeedTransportCaptureViewModel.ARG_TASK_ID) { type = NavType.StringType },
                navArgument(FeedTransportCaptureViewModel.ARG_SHED_ID) { type = NavType.StringType },
                navArgument(FeedTransportCaptureViewModel.ARG_SHED_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
            ),
        ) {
            val vm: FeedTransportCaptureViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val submitted = state.result?.status == FeedTransportSubmitStatus.SYNCED ||
                state.result?.status == FeedTransportSubmitStatus.QUEUED
            LaunchedEffect(submitted) {
                if (submitted) {
                    delay(SUBMIT_SUCCESS_RETURN_DELAY_MS)
                    navController.popBackStack(Routes.FEED_TRANSPORT_CAPTURE, inclusive = true)
                }
            }
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                FeedTransportCaptureScreen(state) { event ->
                    if (event == FeedTransportCaptureEvent.Back) {
                        navController.popBackStack()
                    } else {
                        vm.onEvent(event)
                    }
                }
            }
        }

        // L2 feed-direction completion detail: optional video + Mark done. Camera bound only while
        // composed (operator capture role gated), releasing on leave.
        composable(
            route = Routes.FEED_COMPLETE,
            arguments = listOf(
                navArgument(FeedCompleteViewModel.ARG_PARK_ID) { type = NavType.StringType },
                navArgument(FeedCompleteViewModel.ARG_SHED_ID) { type = NavType.StringType },
                navArgument(FeedCompleteViewModel.ARG_SESSION_NO) { type = NavType.StringType },
                navArgument(FeedCompleteViewModel.ARG_WORKFLOW) { type = NavType.StringType },
                navArgument(FeedCompleteViewModel.ARG_TARGET_DATE) { type = NavType.StringType },
                navArgument(FeedCompleteViewModel.ARG_SHED_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
                navArgument(FeedCompleteViewModel.ARG_SESSION_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
            ),
        ) {
            val vm: FeedCompleteViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val onEvent: (FeedCompleteEvent) -> Unit = { event ->
                when (event) {
                    FeedCompleteEvent.Back -> navController.popBackStack()
                    else -> vm.onEvent(event)
                }
            }
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                FeedCompleteScreen(state = state, onEvent = onEvent)
            }
        }

        // L2 verifier-GATED feed-DISTRIBUTION completion: MANDATORY feed video + MANDATORY water
        // proof (photo or video) -> pending_verification. Both camera bindings are held only while
        // composed (operator capture role gated), releasing on leave.
        composable(
            route = Routes.FEED_DISTRIBUTION_COMPLETE,
            arguments = listOf(
                navArgument(FeedDistributionCompleteViewModel.ARG_PARK_ID) { type = NavType.StringType },
                navArgument(FeedDistributionCompleteViewModel.ARG_SHED_ID) { type = NavType.StringType },
                navArgument(FeedDistributionCompleteViewModel.ARG_SESSION_NO) { type = NavType.StringType },
                navArgument(FeedDistributionCompleteViewModel.ARG_WORKFLOW) { type = NavType.StringType },
                navArgument(FeedDistributionCompleteViewModel.ARG_TARGET_DATE) { type = NavType.StringType },
                navArgument(FeedDistributionCompleteViewModel.ARG_SHED_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
                navArgument(FeedDistributionCompleteViewModel.ARG_SESSION_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
            ),
        ) {
            val vm: FeedDistributionCompleteViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val onEvent: (FeedDistributionEvent) -> Unit = { event ->
                when (event) {
                    FeedDistributionEvent.Back -> navController.popBackStack()
                    else -> vm.onEvent(event)
                }
            }
            // On a successful submission the proofs are durably enqueued (QUEUED) or already
            // SYNCED. Don't strand the operator on the capture screen: show the success tone
            // briefly, then pop back to the Feed Direction list (which is RefreshOnResume, so
            // it refreshes once on landing and shows the session as pending verification).
            val submitted = state.result?.status == FeedDistributionStatus.SYNCED ||
                state.result?.status == FeedDistributionStatus.QUEUED
            LaunchedEffect(submitted) {
                if (submitted) {
                    delay(SUBMIT_SUCCESS_RETURN_DELAY_MS)
                    // Pop this exact destination if it is still on top; a no-op if the operator
                    // already navigated away, so we never pop an extra screen.
                    navController.popBackStack(Routes.FEED_DISTRIBUTION_COMPLETE, inclusive = true)
                }
            }
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                BindPhotoCaptureSource(rememberDelegatingPhotoCaptureSource())
                FeedDistributionCompleteScreen(state = state, onEvent = onEvent)
            }
        }

        // L2 verifier-GATED feed-PACKING completion: ONE MANDATORY packing video ->
        // pending_verification. Camera binding is held only while composed (operator capture role
        // gated), releasing on leave. Simpler than [FEED_DISTRIBUTION_COMPLETE] — no water proof, no
        // photo capture.
        composable(
            route = Routes.FEED_PACKING_COMPLETE,
            arguments = listOf(
                navArgument(FeedPackingCompleteViewModel.ARG_PARK_ID) { type = NavType.StringType },
                navArgument(FeedPackingCompleteViewModel.ARG_SHED_ID) { type = NavType.StringType },
                navArgument(FeedPackingCompleteViewModel.ARG_SESSION_NO) { type = NavType.StringType },
                navArgument(FeedPackingCompleteViewModel.ARG_WORKFLOW) { type = NavType.StringType },
                navArgument(FeedPackingCompleteViewModel.ARG_TARGET_DATE) { type = NavType.StringType },
                navArgument(FeedPackingCompleteViewModel.ARG_SHED_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
                navArgument(FeedPackingCompleteViewModel.ARG_SESSION_LABEL) {
                    type = NavType.StringType
                    defaultValue = ""
                },
            ),
        ) {
            val vm: FeedPackingCompleteViewModel = hiltViewModel()
            val state by vm.state.collectAsStateWithLifecycle()
            val onEvent: (FeedPackingCompleteEvent) -> Unit = { event ->
                when (event) {
                    FeedPackingCompleteEvent.Back -> navController.popBackStack()
                    else -> vm.onEvent(event)
                }
            }
            // On a successful submission the packing video is durably enqueued (QUEUED) or already
            // SYNCED. Don't strand the operator on the capture screen: show the success tone
            // briefly, then pop back to the Feed Packing list (which is RefreshOnResume, so it
            // refreshes once on landing and shows the session as pending verification).
            val submitted = state.result?.status == FeedPackingCompleteStatus.SYNCED ||
                state.result?.status == FeedPackingCompleteStatus.QUEUED
            LaunchedEffect(submitted) {
                if (submitted) {
                    delay(SUBMIT_SUCCESS_RETURN_DELAY_MS)
                    // Pop this exact destination if it is still on top; a no-op if the operator
                    // already navigated away, so we never pop an extra screen.
                    navController.popBackStack(Routes.FEED_PACKING_COMPLETE, inclusive = true)
                }
            }
            CaptureAccessGate {
                BindVideoCaptureSource(rememberDelegatingProofCaptureSource())
                FeedPackingCompleteScreen(state = state, onEvent = onEvent)
            }
        }

        // Approvals were REMOVED from mobile (maintainer decision 2026-07-21): the birth/death/
        // shifting approval queue and approve/reject actions now live only on the admin-web
        // Approvals page, gated to the four org tiers + admin + ceo_internal. There is no mobile
        // route, screen, or nav entry for approvals any more.

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
 * Shared body of the two L0 workflow work-list destinations: collects the Paging rows, wires the
 * refresh/append load states into the VM (so the sync banner and offline empty-state track REAL
 * page loads), and routes card taps / ＋ / Back to the nav controller.
 */
@Composable
private fun WorkflowListDestination(
    vm: WorkflowListViewModel,
    submissionNotice: String? = null,
    onOpenCard: (String) -> Unit,
    onAddNew: () -> Unit,
    onBack: () -> Unit,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val rows = vm.rows.collectAsLazyPagingItems()
    val refreshState = rows.loadState.refresh
    LaunchedEffect(refreshState) {
        when (refreshState) {
            is LoadState.Loading -> vm.onRowsLoading()
            is LoadState.Error -> vm.onRowsLoadFailed(refreshState.error)
            is LoadState.NotLoading -> vm.onRowsLoaded()
        }
    }
    val appendError = (rows.loadState.append as? LoadState.Error)?.error
    LaunchedEffect(appendError) { appendError?.let(vm::onRowsLoadFailed) }
    WorkflowListScreen(
        state = state.copy(submissionNotice = submissionNotice),
        rows = rows,
        onEvent = { event ->
            when (event) {
                is WorkflowListEvent.OpenCard -> {
                    vm.onEvent(event) // analytics
                    onOpenCard(event.workflowId)
                }
                WorkflowListEvent.AddNew -> onAddNew()
                WorkflowListEvent.Back -> onBack()
                WorkflowListEvent.Refresh -> {
                    vm.onEvent(event)
                    rows.refresh()
                }
                else -> vm.onEvent(event)
            }
        },
    )
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

// How long the feed-distribution capture screen lingers on its success tone before
// auto-returning to the Feed Direction list. Long enough to confirm the submit
// registered, short enough not to feel stuck.
private const val SUBMIT_SUCCESS_RETURN_DELAY_MS = 800L

private val supportedRootDestinations = setOf(
    Routes.CALENDAR,
    Routes.VACCINATION,
    Routes.VERIFY,
    Routes.VERIFY_ACTION,
    Routes.YOU,
    Routes.ALERTS,
    Routes.TIMETABLE,
    Routes.FEED_DIRECTION,
    Routes.FEED_PACKING,
    Routes.FEED_TRANSPORT,
    // Counts roots: a Counts-only principal's default landing is the first page they may
    // open (/counts census for CEO/admin, /counts/birth-death for a capture operator).
    // These are registered top-level composables, so cold start must accept them instead
    // of falling back to Calendar (which a Counts-only principal may not be granted).
    // Approvals live in admin-web only (moved off mobile), so there is no approvals root here.
    Routes.COUNTS,
    Routes.COUNTS_BIRTH,
    Routes.COUNTS_DEATH,
    Routes.COUNTS_SHIFTING,
    Routes.COUNTS_MILK_PREPARATION,
    Routes.COUNTS_MILK_FEEDING,
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
