package sg.mesha.goatos.feature.clock

// telemetry:exempt pure stateless renderers; LeaveRequestViewModel / LeaveApprovalViewModel /
// ClockViewModel (in :app) own the clock_leave_* AnalyticsEventsClock + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.distinctUntilChanged
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

/**
 * Leave requests (docs/features/leave-requests/plan.md, maintainer decisions 2026-09-10).
 * Three surfaces, every visible word backend-owned:
 *
 *  - the Leave SECTION on the Clock screen (the person's requests beside their clockings, with
 *    a Request leave button and Withdraw on a pending row) -- [LeaveRowUi] / [LeaveSection];
 *  - the Request leave FORM (hosted drill `/clock/leave/new`) -- [LeaveRequestFormScreen];
 *  - the approver's Leave TAB inside the Approvals module (`/leave/approvals`) --
 *    [LeaveApprovalScreen].
 */

/** One leave request row. [listKey] carries full identity (request id + status + version). */
@Immutable
data class LeaveRowUi(
    val listKey: String,
    val requestId: String,
    /** Requester's name (approver queue) -- blank on the person's own history. */
    val personName: String,
    val parkLabel: String,
    val datesLabel: String,
    val reason: String,
    val statusLabel: String,
    val statusLine: String,
    /** `pending` | `approved` | `rejected` | `withdrawn`; drives the chip tone only. */
    val status: String,
    val raisedAtLabel: String,
    val canWithdraw: Boolean,
    /** Backend label of the slot the approver signs on this row ("Park head" / "HR"). */
    val mySlotLabel: String,
)

/** The leave section's state, folded into [ClockUiState] by the ClockViewModel. */
@Immutable
data class LeaveSectionUi(
    val title: String = "",
    val empty: String = "",
    val requestLabel: String = "",
    val withdrawLabel: String = "",
    /** Backend `leave_today.label` ("On leave · 12–14 Sep 2026 · 3 days"), blank otherwise. */
    val todayLabel: String = "",
    val rows: List<LeaveRowUi> = emptyList(),
    /** The request whose withdraw is on the outbox; its button is disabled meanwhile. */
    val withdrawingRequestId: String? = null,
)

/** The leave section rendered inside the Clock screen's LazyColumn. */
internal fun androidx.compose.foundation.lazy.LazyListScope.leaveSection(
    leave: LeaveSectionUi,
    hasStatus: Boolean,
    onRequestLeave: () -> Unit,
    onWithdraw: (String) -> Unit,
) {
    if (leave.title.isBlank()) return
    item(key = "leave_title") {
        Row(
            modifier = Modifier.fillMaxWidth().padding(top = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(text = leave.title, style = MeshaType.cardTitle, color = MeshaColors.Ink)
            Spacer(Modifier.weight(1f))
            if (leave.requestLabel.isNotBlank()) {
                OutlinedButton(onClick = onRequestLeave) {
                    Text(text = leave.requestLabel, style = MeshaType.button)
                }
            }
        }
    }
    if (leave.todayLabel.isNotBlank()) {
        item(key = "leave_today") {
            Text(
                text = leave.todayLabel,
                style = MeshaType.cardSubtitle,
                color = MeshaColors.Brand,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.BrandTint)
                    .padding(horizontal = 14.dp, vertical = 10.dp),
            )
        }
    }
    if (leave.rows.isEmpty() && leave.empty.isNotBlank() && hasStatus) {
        item(key = "leave_empty") {
            EmptyState(
                title = leave.empty,
                modifier = Modifier.fillMaxWidth(),
                icon = MeshaIcons.Calendar,
                tone = EmptyTone.Neutral,
            )
        }
    }
    items(leave.rows, key = { it.listKey }) { row ->
        LeaveRowCard(
            row = row,
            withdrawLabel = leave.withdrawLabel,
            withdrawBusy = leave.withdrawingRequestId == row.requestId,
            onWithdraw = onWithdraw,
        )
    }
}

@Composable
private fun LeaveRowCard(
    row: LeaveRowUi,
    withdrawLabel: String,
    withdrawBusy: Boolean,
    onWithdraw: (String) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text = row.datesLabel, style = MeshaType.cardTitle, color = MeshaColors.Ink, modifier = Modifier.weight(1f))
            LeaveStatusChip(status = row.status, label = row.statusLabel)
        }
        if (row.reason.isNotBlank()) {
            Text(text = row.reason, style = MeshaType.cardSubtitle, color = MeshaColors.Ink)
        }
        if (row.statusLine.isNotBlank()) {
            Text(text = row.statusLine, style = MeshaType.caption, color = MeshaColors.Muted)
        }
        if (row.raisedAtLabel.isNotBlank()) {
            Text(text = row.raisedAtLabel, style = MeshaType.sectionLabel, color = MeshaColors.Faint)
        }
        if (row.canWithdraw && withdrawLabel.isNotBlank()) {
            Spacer(Modifier.height(4.dp))
            OutlinedButton(
                onClick = { onWithdraw(row.requestId) },
                enabled = !withdrawBusy,
                modifier = Modifier.fillMaxWidth().height(40.dp),
            ) {
                Text(text = withdrawLabel, style = MeshaType.button, color = MeshaColors.Danger)
            }
        }
    }
}

/** Status chip: the LABEL is backend copy; only the tone keys on the status code. */
@Composable
internal fun LeaveStatusChip(status: String, label: String) {
    if (label.isBlank()) return
    val (fg, bg) = when (status) {
        "approved" -> MeshaColors.Brand to MeshaColors.BrandTint
        "rejected" -> MeshaColors.Danger to MeshaColors.DangerX
        "withdrawn" -> MeshaColors.Muted to MeshaColors.Hair
        else -> MeshaColors.Warn to MeshaColors.WarnX
    }
    Text(
        text = label,
        style = MeshaType.pill,
        color = fg,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}

// ---------------------------------------------------------------------------------------------
// Request leave form
// ---------------------------------------------------------------------------------------------

@Immutable
data class LeaveRequestFormUi(
    val title: String = "",
    val fromLabel: String = "",
    val toLabel: String = "",
    val reasonLabel: String = "",
    val reasonHint: String = "",
    val submitLabel: String = "",
    /** ISO dates (YYYY-MM-DD); blank until picked. */
    val startsOn: String = "",
    val endsOn: String = "",
    val reason: String = "",
    /** True from submit tap until the outbox row lands or fails. */
    val submitting: Boolean = false,
    /** Backend/local feedback line under the form; [isError] picks the tone. */
    val message: String = "",
    val isError: Boolean = false,
    /** Set once the write reached the outbox; the host pops back to the Clock screen. */
    val submitted: Boolean = false,
)

sealed interface LeaveRequestFormEvent {
    data class PickStart(val date: String) : LeaveRequestFormEvent
    data class PickEnd(val date: String) : LeaveRequestFormEvent
    data class EditReason(val value: String) : LeaveRequestFormEvent
    data object Submit : LeaveRequestFormEvent
    data object Back : LeaveRequestFormEvent
}

/** The Request leave form (hosted drill, Up/Back, no root chrome). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LeaveRequestFormScreen(
    state: LeaveRequestFormUi,
    onEvent: (LeaveRequestFormEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            onBack = { onEvent(LeaveRequestFormEvent.Back) },
        )
        Column(
            modifier = Modifier.fillMaxSize().padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            LeaveDateField(
                label = state.fromLabel,
                value = state.startsOn,
                minDate = LocalDate.now(),
                onPick = { onEvent(LeaveRequestFormEvent.PickStart(it)) },
            )
            LeaveDateField(
                label = state.toLabel,
                value = state.endsOn,
                minDate = state.startsOn.takeIf { it.isNotBlank() }?.let(LocalDate::parse) ?: LocalDate.now(),
                onPick = { onEvent(LeaveRequestFormEvent.PickEnd(it)) },
            )
            OutlinedTextField(
                value = state.reason,
                onValueChange = { onEvent(LeaveRequestFormEvent.EditReason(it)) },
                label = { Text(state.reasonLabel) },
                placeholder = { Text(state.reasonHint) },
                minLines = 3,
                modifier = Modifier.fillMaxWidth(),
            )
            if (state.message.isNotBlank()) {
                Text(
                    text = state.message,
                    style = MeshaType.cardSubtitle,
                    color = if (state.isError) MeshaColors.Danger else MeshaColors.Brand,
                )
            }
            Spacer(Modifier.height(4.dp))
            Button(
                onClick = { onEvent(LeaveRequestFormEvent.Submit) },
                enabled = !state.submitting && state.startsOn.isNotBlank() && state.endsOn.isNotBlank() && state.reason.isNotBlank(),
                colors = ButtonDefaults.buttonColors(containerColor = MeshaColors.Brand, contentColor = MeshaColors.OnBrand),
                modifier = Modifier.fillMaxWidth().height(52.dp),
            ) {
                Text(text = state.submitLabel, style = MeshaType.button)
            }
        }
    }
}

/**
 * A tappable date field opening the Material date picker. Dates are business DATES: the picker's
 * UTC-midnight millis are mapped straight to a LocalDate, never through the device zone.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LeaveDateField(label: String, value: String, minDate: LocalDate, onPick: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .clickable { open = true }
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(text = label, style = MeshaType.sectionLabel, color = MeshaColors.Muted)
        Spacer(Modifier.height(2.dp))
        Text(
            text = value.takeIf { it.isNotBlank() }?.let { farmDate(it) } ?: "—",
            style = MeshaType.cardTitle,
            color = MeshaColors.Ink,
        )
    }
    if (open) {
        val minMillis = minDate.atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
        val initial = value.takeIf { it.isNotBlank() }?.let { LocalDate.parse(it) } ?: minDate
        val pickerState = rememberDatePickerState(
            initialSelectedDateMillis = initial.atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli(),
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long): Boolean = utcTimeMillis >= minMillis
                override fun isSelectableYear(year: Int): Boolean = year >= minDate.year
            },
        )
        DatePickerDialog(
            onDismissRequest = { open = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let { millis ->
                        onPick(Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate().toString())
                    }
                    open = false
                }) { Text(androidx.compose.ui.res.stringResource(id = android.R.string.ok)) }
            },
            dismissButton = {
                TextButton(onClick = { open = false }) { Text(androidx.compose.ui.res.stringResource(id = android.R.string.cancel)) }
            },
        ) {
            DatePicker(state = pickerState)
        }
    }
}

/** dd/mm/yyyy, the farm's date format, from an ISO business date. */
private fun farmDate(iso: String): String = runCatching { // exception:exempt an unparseable date falls back to the raw ISO string the backend sent; nothing to recover
    val d = LocalDate.parse(iso)
    "%02d/%02d/%04d".format(d.dayOfMonth, d.monthValue, d.year)
}.getOrDefault(iso)

// ---------------------------------------------------------------------------------------------
// Approver queue (the Leave tab of the Approvals module)
// ---------------------------------------------------------------------------------------------

@Immutable
data class LeaveApprovalUiState(
    val title: String = "",
    val empty: String = "",
    val approveLabel: String = "",
    val rejectLabel: String = "",
    val rejectReasonLabel: String = "",
    val rejectReasonHint: String = "",
    val cancelLabel: String = "",
    val rows: List<LeaveRowUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val hasData: Boolean = false,
    /** True while a next page is loading (passive footer, never a tappable Load more). */
    val loadingMore: Boolean = false,
    val hasMore: Boolean = false,
    /** The row whose reject reason is being composed. */
    val rejectingRequestId: String? = null,
    val rejectReason: String = "",
    /** The row whose decision is on the outbox; its buttons are disabled meanwhile. */
    val decidingRequestId: String? = null,
    val message: String = "",
    val isError: Boolean = false,
)

sealed interface LeaveApprovalEvent {
    data object Refresh : LeaveApprovalEvent
    data object LoadMore : LeaveApprovalEvent
    data class Approve(val requestId: String) : LeaveApprovalEvent
    data class OpenReject(val requestId: String) : LeaveApprovalEvent
    data class EditRejectReason(val value: String) : LeaveApprovalEvent
    data object CancelReject : LeaveApprovalEvent
    data object ConfirmReject : LeaveApprovalEvent
}

@Composable
fun LeaveApprovalScreen(
    state: LeaveApprovalUiState,
    onEvent: (LeaveApprovalEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(LeaveApprovalEvent.Refresh) }
    val listState = rememberLazyListState()
    // Infinite scroll: prefetch the next page a few rows before the end (mobile-guard: no
    // tappable Load more on a work queue).
    LaunchedEffect(listState, state.hasMore, state.rows.size) {
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .distinctUntilChanged()
            .collect { last -> if (state.hasMore && !state.loadingMore && last >= state.rows.size - 3) onEvent(LeaveApprovalEvent.LoadMore) }
    }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            below = {
                SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = state.hasData)
            },
            actions = {
                SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(LeaveApprovalEvent.Refresh) })
            },
        )
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.message.isNotBlank()) {
                item(key = "message") {
                    Text(
                        text = state.message,
                        style = MeshaType.cardSubtitle,
                        color = if (state.isError) MeshaColors.Danger else MeshaColors.Brand,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(12.dp))
                            .background(if (state.isError) MeshaColors.DangerX else MeshaColors.BrandTint)
                            .padding(horizontal = 14.dp, vertical = 10.dp),
                    )
                }
            }
            if (state.rows.isEmpty() && state.hasData && state.empty.isNotBlank()) {
                item(key = "empty") {
                    EmptyState(title = state.empty, modifier = Modifier.fillMaxWidth(), icon = MeshaIcons.Calendar, tone = EmptyTone.Neutral)
                }
            }
            items(state.rows, key = { it.listKey }) { row ->
                LeaveApprovalCard(
                    row = row,
                    state = state,
                    onEvent = onEvent,
                )
            }
            if (state.loadingMore) {
                item(key = "loading_more") {
                    Box(modifier = Modifier.fillMaxWidth().padding(12.dp), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                }
            }
        }
    }
}

@Composable
private fun LeaveApprovalCard(row: LeaveRowUi, state: LeaveApprovalUiState, onEvent: (LeaveApprovalEvent) -> Unit) {
    val busy = state.decidingRequestId == row.requestId
    val rejecting = state.rejectingRequestId == row.requestId
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text = row.personName, style = MeshaType.cardTitle, color = MeshaColors.Ink, modifier = Modifier.weight(1f))
            if (row.mySlotLabel.isNotBlank()) {
                LeaveStatusChip(status = "pending", label = row.mySlotLabel)
            }
        }
        if (row.parkLabel.isNotBlank()) {
            Text(text = row.parkLabel, style = MeshaType.sectionLabel, color = MeshaColors.Faint)
        }
        Text(text = row.datesLabel, style = MeshaType.cardSubtitle, color = MeshaColors.Ink)
        if (row.reason.isNotBlank()) {
            Text(text = row.reason, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
        }
        if (row.statusLine.isNotBlank()) {
            Text(text = row.statusLine, style = MeshaType.caption, color = MeshaColors.Muted)
        }
        Text(text = row.raisedAtLabel, style = MeshaType.sectionLabel, color = MeshaColors.Faint)
        if (rejecting) {
            OutlinedTextField(
                value = state.rejectReason,
                onValueChange = { onEvent(LeaveApprovalEvent.EditRejectReason(it)) },
                label = { Text(state.rejectReasonLabel) },
                placeholder = { Text(state.rejectReasonHint) },
                minLines = 2,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = { onEvent(LeaveApprovalEvent.CancelReject) }, enabled = !busy, modifier = Modifier.weight(1f)) {
                    Text(state.cancelLabel, style = MeshaType.button)
                }
                Button(
                    onClick = { onEvent(LeaveApprovalEvent.ConfirmReject) },
                    enabled = !busy && state.rejectReason.isNotBlank(),
                    colors = ButtonDefaults.buttonColors(containerColor = MeshaColors.Danger, contentColor = MeshaColors.Ink),
                    modifier = Modifier.weight(1f),
                ) {
                    Text(state.rejectLabel, style = MeshaType.button)
                }
            }
        } else {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = { onEvent(LeaveApprovalEvent.OpenReject(row.requestId)) }, enabled = !busy, modifier = Modifier.weight(1f)) {
                    Text(state.rejectLabel, style = MeshaType.button, color = MeshaColors.Danger)
                }
                Button(
                    onClick = { onEvent(LeaveApprovalEvent.Approve(row.requestId)) },
                    enabled = !busy,
                    colors = ButtonDefaults.buttonColors(containerColor = MeshaColors.Brand, contentColor = MeshaColors.OnBrand),
                    modifier = Modifier.weight(1f),
                ) {
                    Text(state.approveLabel, style = MeshaType.button)
                }
            }
        }
    }
}
