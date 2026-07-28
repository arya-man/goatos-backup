package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; ShiftingPendingViewModel (in :app) owns the
// counts_shifting_pending_* AnalyticsEvents + the CrashReporter non-fatal on every page-load failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset

/**
 * The Shifting "Actions" home — raised work appears immediately; approval and completion may
 * happen in either order, and rejected verification evidence returns as rework without moving counts.
 *
 * Read screen, offline-first (docs/decisions/android-offline-first.md): rows are a bounded Room-
 * backed Paging window (~20/page keyset), with business date and disjoint backend status filters.
 * Tapping a row opens the L1 execute screen where the operator does the move, records the mandatory
 * video, and submits it for verification. Raising a new movement lives behind the top-right ＋ and
 * opens a separate L1 form, matching the Birth and Death module pattern.
 */

/** One authorized movement row. Every field is backend-owned; the screen renders, never derives. */
@Immutable
data class ShiftingPendingRowUi(
    val shiftingEventId: String,
    val sourceLabel: String,
    val destinationLabel: String,
    val priority: String,
    val category: String,
    val animalCount: Int,
    val approvedAtLabel: String,
    val actionStateLabel: String = "",
    val primaryActionKey: String = "none",
)

@Immutable
data class ShiftingPendingStatusUi(val key: String, val label: String, val selected: Boolean, val count: Int = 0)
@Immutable data class ShiftingPreviousDateUi(val dateIso: String, val dateLabel: String, val actionCount: Int)

@Immutable
data class ShiftingPendingUiState(
    val dateIso: String = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString(),
    val dateLabel: String = "Today",
    val isToday: Boolean = true,
    val statuses: List<ShiftingPendingStatusUi> = emptyList(),
    val previousDates: List<ShiftingPreviousDateUi> = emptyList(),
    val submissionNotice: String? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface ShiftingPendingEvent {
    data object Refresh : ShiftingPendingEvent
    data object Raise : ShiftingPendingEvent
    data object Back : ShiftingPendingEvent
    data object PrevDay : ShiftingPendingEvent
    data object NextDay : ShiftingPendingEvent
    data object Today : ShiftingPendingEvent
    data class SelectDate(val dateIso: String) : ShiftingPendingEvent
    data class SelectStatus(val status: String) : ShiftingPendingEvent
    data class OpenPreviousDate(val dateIso: String) : ShiftingPendingEvent
    data class OpenMovement(val shiftingEventId: String) : ShiftingPendingEvent
}

@Composable
fun ShiftingActionsScreen(
    state: ShiftingPendingUiState,
    rows: LazyPagingItems<ShiftingPendingRowUi>,
    onEvent: (ShiftingPendingEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ShiftingPendingEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = stringResource(R.string.counts_shifting_actions_title),
            subtitle = stringResource(R.string.counts_shifting_actions_subtitle),
            onBack = { onEvent(ShiftingPendingEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(ShiftingPendingEvent.Refresh) },
                )
                Box(
                    modifier = Modifier
                        .size(48.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Brand)
                        .clickable { onEvent(ShiftingPendingEvent.Raise) },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = MeshaIcons.Plus,
                        contentDescription = stringResource(R.string.counts_shifting_raise),
                        tint = MeshaColors.OnBrand,
                        modifier = Modifier.size(20.dp),
                    )
                }
            },
        )
        SyncStatusIndicator(
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = rows.itemCount > 0,
            isOffline = state.isOffline,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
        )
        if (state.previousDates.isNotEmpty()) {
            Row(modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(MeshaIcons.Bell, "Previous shifting actions", tint = MeshaColors.Warn, modifier = Modifier.size(24.dp))
                state.previousDates.forEach { previous ->
                    Text("${previous.dateLabel} · ${previous.actionCount}", color = MeshaColors.Ink, fontSize = 12.sp, fontWeight = FontWeight.W700,
                        modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(MeshaColors.WarnX)
                            .clickable { onEvent(ShiftingPendingEvent.OpenPreviousDate(previous.dateIso)) }.padding(horizontal = 12.dp, vertical = 5.dp))
                }
            }
        }
        state.submissionNotice?.let {
            Text(
                text = it,
                color = MeshaColors.Ok,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "date") { ShiftingDateBar(state, onEvent) }
            item(key = "status") { ShiftingStatusBar(state.statuses, onEvent) }

            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.ArrowUpDown,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.shiftingEventId }) { index ->
                rows[index]?.let { row ->
                    ShiftingPendingRowCard(row) {
                        if (row.primaryActionKey == "execute") onEvent(ShiftingPendingEvent.OpenMovement(row.shiftingEventId))
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ShiftingDateBar(state: ShiftingPendingUiState, onEvent: (ShiftingPendingEvent) -> Unit) {
    var pickerOpen by remember { mutableStateOf(false) }
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        DateArrow(MeshaIcons.ChevronLeft, true) { onEvent(ShiftingPendingEvent.PrevDay) }
        Row(
            modifier = Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(MeshaColors.Surf2)
                .clickable { pickerOpen = true }.padding(vertical = 10.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(MeshaIcons.Calendar, null, tint = MeshaColors.Muted, modifier = Modifier.size(15.dp))
            Text(state.dateLabel, color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700,
                modifier = Modifier.padding(start = 6.dp))
        }
        DateArrow(MeshaIcons.Chevron, !state.isToday) { onEvent(ShiftingPendingEvent.NextDay) }
        if (!state.isToday) Text(
            "Today", color = MeshaColors.BrandD, fontSize = 12.sp, fontWeight = FontWeight.W800,
            modifier = Modifier.clickable { onEvent(ShiftingPendingEvent.Today) }.minimumInteractiveComponentSize(),
        )
    }
    if (pickerOpen) {
        val todayMillis = LocalDate.now(ZoneId.of("Asia/Kolkata")).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
        val selectedMillis = LocalDate.parse(state.dateIso).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
        val pickerState = rememberDatePickerState(
            initialSelectedDateMillis = selectedMillis,
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long) = utcTimeMillis <= todayMillis
            },
        )
        DatePickerDialog(
            onDismissRequest = { pickerOpen = false },
            confirmButton = { TextButton(onClick = {
                pickerState.selectedDateMillis?.let {
                    onEvent(ShiftingPendingEvent.SelectDate(Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate().toString()))
                }
                pickerOpen = false
            }) { Text(stringResource(android.R.string.ok)) } },
            dismissButton = { TextButton(onClick = { pickerOpen = false }) { Text(stringResource(android.R.string.cancel)) } },
        ) { DatePicker(pickerState) }
    }
}

@Composable
private fun DateArrow(icon: androidx.compose.ui.graphics.vector.ImageVector, enabled: Boolean, onClick: () -> Unit) {
    Icon(icon, null, tint = if (enabled) MeshaColors.Ink else MeshaColors.Hair,
        modifier = Modifier.size(48.dp).clip(RoundedCornerShape(10.dp)).clickable(enabled = enabled, onClick = onClick).padding(14.dp))
}

@Composable
private fun ShiftingStatusBar(statuses: List<ShiftingPendingStatusUi>, onEvent: (ShiftingPendingEvent) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        statuses.forEach { status ->
            Text(
                "${status.label} ${status.count}",
                color = if (status.selected) MeshaColors.OnBrand else MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.clip(RoundedCornerShape(999.dp))
                    .background(if (status.selected) MeshaColors.Brand else MeshaColors.Surf2)
                    .clickable { onEvent(ShiftingPendingEvent.SelectStatus(status.key)) }
                    .padding(horizontal = 14.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun ShiftingPendingRowCard(row: ShiftingPendingRowUi, onClick: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(enabled = row.primaryActionKey == "execute", onClick = onClick)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = row.sourceLabel,
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            Icon(
                imageVector = MeshaIcons.ArrowUpDown,
                contentDescription = "to",
                tint = MeshaColors.Muted,
                modifier = Modifier.size(16.dp).padding(horizontal = 2.dp),
            )
            Text(
                text = row.destinationLabel,
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            ShiftingPill(row.priority, MeshaColors.WarnX, MeshaColors.Warn)
            ShiftingPill(row.category, MeshaColors.OkX, MeshaColors.Ok)
            ShiftingPill("${row.animalCount} animal${if (row.animalCount == 1) "" else "s"}", MeshaColors.Surf3, MeshaColors.Muted)
        }
        if (row.actionStateLabel.isNotBlank()) {
            Text(text = row.actionStateLabel, color = MeshaColors.Faint, fontSize = 11.sp)
        }
    }
}

@Composable
private fun ShiftingPill(text: String, bg: androidx.compose.ui.graphics.Color, fg: androidx.compose.ui.graphics.Color) {
    if (text.isBlank()) return
    Text(
        text = text,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}
