// telemetry:exempt: pure presentational Compose surfaces — these render state and forward
// user intent through onEvent, doing no I/O of their own. Health telemetry is emitted where the
// behaviour lives, in the view models (AnalyticsEvents.HEALTH_CASE_SUBMITTED /
// HEALTH_WRITE_FAILURE / HEALTH_READ_FAILURE in AddHealthCaseViewModel).
package sg.mesha.goatos.feature.health

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import androidx.compose.material3.rememberDatePickerState
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

@Immutable
data class HealthFilterUi(val key: String, val label: String)

@Immutable
data class HealthSummaryUi(
    val total: Int = 0,
    val due: Int = 0,
    val scheduled: Int = 0,
    val inProgress: Int = 0,
    val completed: Int = 0,
    val rework: Int = 0,
    val held: Int = 0,
    val canceledDeath: Int = 0,
)

@Immutable
data class HealthDateMarkerUi(val dateIso: String, val label: String, val count: Int)

@Immutable
data class HealthWorkItemUi(
    val healthSessionId: String,
    val goatDisplayId: String,
    val diseaseName: String,
    val dayLabel: String,
    val dayNo: Int,
    val durationDays: Int,
    val locationLabel: String,
    val sessionLabel: String,
    val medicineLabel: String,
    val status: String,
    val hasCriticalStep: Boolean,
)

@Immutable
data class HealthPendingCaseUi(
    val outboxItemId: String,
    val goatDisplayId: String,
    val diseaseName: String,
    val startDate: String,
    val statusLabel: String,
)

@Immutable
data class HealthListUiState(
    val pageLabel: String,
    val dateIso: String,
    val dateLabel: String,
    val isToday: Boolean = true,
    val status: String = "",
    val diseaseKey: String = "",
    val parkId: String = "",
    val shedId: String = "",
    val session: String = "",
    val summary: HealthSummaryUi = HealthSummaryUi(),
    /** Backend day markers for OTHER days that still hold open work — rendered as jump chips. */
    val dateMarkers: List<HealthDateMarkerUi> = emptyList(),
    val diseases: List<HealthFilterUi> = emptyList(),
    val parks: List<HealthFilterUi> = emptyList(),
    val sheds: List<HealthFilterUi> = emptyList(),
    /** Local reports are displayed separately and never included in canonical action totals. */
    val pendingCases: List<HealthPendingCaseUi> = emptyList(),
    val refreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val submissionNotice: String? = null,
    val error: String? = null,
)

sealed interface HealthListEvent {
    data object Refresh : HealthListEvent
    data object Back : HealthListEvent
    data object AddNew : HealthListEvent
    data object PrevDay : HealthListEvent
    data object NextDay : HealthListEvent
    data object Today : HealthListEvent
    data class SelectDate(val value: String) : HealthListEvent
    data class SelectStatus(val value: String) : HealthListEvent
    data class SelectDisease(val value: String) : HealthListEvent
    data class SelectPark(val value: String) : HealthListEvent
    data class SelectShed(val value: String) : HealthListEvent
    data class SelectSession(val value: String) : HealthListEvent
    data class OpenItem(val healthSessionId: String) : HealthListEvent

    /**
     * Open the queue of assessments awaiting a decision.
     *
     * A CONTENT row, not app-bar chrome: a feature entry point belongs in the
     * bottom bar or the module drawer, never the top-right, which carries only
     * actions on the screen you are already on.
     */
    data object OpenDiagnosisQueue : HealthListEvent
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HealthListScreen(
    state: HealthListUiState,
    rows: LazyPagingItems<HealthWorkItemUi>,
    onEvent: (HealthListEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(HealthListEvent.Refresh) }
    var showDatePicker by remember { mutableStateOf(false) }

    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Health",
            subtitle = "${state.pageLabel} · ${state.summary.total} actions",
            onBack = { onEvent(HealthListEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.refreshing,
                    onSync = { onEvent(HealthListEvent.Refresh) },
                )
                Box(
                    modifier = Modifier
                        .size(48.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Brand)
                        .clickable { onEvent(HealthListEvent.AddNew) },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = MeshaIcons.Plus,
                        contentDescription = "Report sick goat",
                        tint = MeshaColors.OnBrand,
                        modifier = Modifier.size(20.dp),
                    )
                }
            },
        )
        SyncStatusIndicator(
            isRefreshing = state.refreshing || rows.loadState.refresh is LoadState.Loading,
            lastSyncedAt = state.lastSyncedAt,
            hasData = rows.itemCount > 0,
            // A failed paging refresh is the honest offline/stale signal for this Room-first
            // list — the audit found this flag permanently false, so the banner never warned
            // an operator in a dead-signal shed that the list was stale.
            isOffline = state.isOffline || rows.loadState.refresh is LoadState.Error,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        state.submissionNotice?.let { notice ->
            Card(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
                colors = CardDefaults.cardColors(containerColor = MeshaColors.BrandTint),
                shape = RoundedCornerShape(12.dp),
            ) { Text(notice, modifier = Modifier.padding(10.dp), color = MeshaColors.BrandD, fontWeight = FontWeight.SemiBold) }
        }
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            HealthDateNavButton(MeshaIcons.ChevronLeft) { onEvent(HealthListEvent.PrevDay) }
            Row(
                modifier = Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(MeshaColors.Surf2)
                    .clickable { showDatePicker = true }.padding(vertical = 10.dp),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(MeshaIcons.Calendar, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(15.dp))
                Text(state.dateLabel, color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.Bold, modifier = Modifier.padding(start = 6.dp))
            }
            HealthDateNavButton(MeshaIcons.Chevron) { onEvent(HealthListEvent.NextDay) }
            if (!state.isToday) {
                Text(
                    "Today",
                    color = MeshaColors.Brand,
                    fontWeight = FontWeight.Bold,
                    fontSize = 12.sp,
                    modifier = Modifier.clip(RoundedCornerShape(10.dp)).clickable { onEvent(HealthListEvent.Today) }
                        .padding(horizontal = 8.dp, vertical = 12.dp),
                )
            }
        }
        HealthStatusChips(state) { onEvent(HealthListEvent.SelectStatus(it)) }
        if (state.dateMarkers.isNotEmpty()) {
            // Backend day markers: other days that still hold open treatment work. One tap jumps
            // the worklist to that day, so operators stop blind-stepping through the calendar.
            Row(
                Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 2.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("Other days:", color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                state.dateMarkers.take(6).forEach { marker ->
                    AssistChip(
                        onClick = { onEvent(HealthListEvent.SelectDate(marker.dateIso)) },
                        label = { Text("${marker.label} · ${marker.count}", fontSize = 11.sp) },
                    )
                }
            }
        }
        Row(
            Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 2.dp),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            FilterMenu("Disease", state.diseaseKey, state.diseases) {
                onEvent(HealthListEvent.SelectDisease(it))
            }
            FilterMenu("Park", state.parkId, state.parks) { onEvent(HealthListEvent.SelectPark(it)) }
            FilterMenu("Shed", state.shedId, state.sheds) { onEvent(HealthListEvent.SelectShed(it)) }
            FilterMenu(
                "Session",
                state.session,
                listOf(
                    HealthFilterUi("", "All sessions"),
                    HealthFilterUi("morning", "Morning"),
                    HealthFilterUi("afternoon", "Afternoon"),
                    HealthFilterUi("evening", "Evening"),
                    HealthFilterUi("unscheduled", "Unscheduled"),
                ),
            ) { onEvent(HealthListEvent.SelectSession(it)) }
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // Deliberately above the day's treatment work: an assessment nobody has
            // decided on is an animal whose treatment has not STARTED, which outranks
            // the visits already under way.
            item("diagnosis-queue-entry") {
                Card(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp)
                        .clickable { onEvent(HealthListEvent.OpenDiagnosisQueue) }
                        .minimumInteractiveComponentSize(),
                    colors = CardDefaults.cardColors(containerColor = MeshaColors.Surf2),
                    shape = RoundedCornerShape(12.dp),
                ) {
                    Column(Modifier.padding(12.dp)) {
                        Text(
                            "Waiting on a decision",
                            color = MeshaColors.Ink,
                            fontSize = 14.sp,
                            fontWeight = FontWeight.Bold,
                        )
                        Text(
                            "Animals that were checked but not yet treated",
                            color = MeshaColors.Muted,
                            fontSize = 12.sp,
                            modifier = Modifier.padding(top = 2.dp),
                        )
                    }
                }
            }
            if (state.pendingCases.isNotEmpty()) {
                item("pending-header") { HealthSectionHeader("Reports waiting to sync", MeshaColors.Warn) }
                items(
                    count = state.pendingCases.size,
                    key = { index -> "pending:${state.pendingCases[index].outboxItemId}" },
                    contentType = { "pending-health-case" },
                ) { index ->
                    HealthPendingCaseCard(state.pendingCases[index])
                }
            }
            if (rows.itemCount == 0 && state.pendingCases.isEmpty()) {
                item("empty") {
                    EmptyState(
                        title = state.error ?: "No Health actions for this day",
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                        icon = if (state.error == null) MeshaIcons.Goat else MeshaIcons.Warn,
                        tone = if (state.error == null) EmptyTone.Neutral else EmptyTone.Warn,
                    )
                }
            }
            items(rows.itemCount, key = rows.itemKey { it.healthSessionId }) { index ->
                val item = rows[index] ?: return@items
                val previous = if (index == 0) null else rows.peek(index - 1)?.status?.healthGroupLabel()
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    val group = item.status.healthGroupLabel()
                    if (group != previous) HealthSectionHeader(group, item.status.healthStatusColor())
                    HealthActionCard(item) { onEvent(HealthListEvent.OpenItem(item.healthSessionId)) }
                }
            }
        }
    }

    if (showDatePicker) {
        val selectedMillis = runCatching {
            LocalDate.parse(state.dateIso).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
        }.getOrNull()
        val pickerState = rememberDatePickerState(initialSelectedDateMillis = selectedMillis)
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let {
                        onEvent(HealthListEvent.SelectDate(Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate().toString()))
                    }
                    showDatePicker = false
                }) { Text("Select") }
            },
            dismissButton = { TextButton(onClick = { showDatePicker = false }) { Text("Cancel") } },
        ) { DatePicker(pickerState) }
    }
}

@Composable
private fun HealthPendingCaseCard(item: HealthPendingCaseUi) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(IntrinsicSize.Min)
            .clip(RoundedCornerShape(16.dp)).background(MeshaColors.Surf),
    ) {
        Box(Modifier.width(4.dp).fillMaxHeight().background(MeshaColors.Warn))
        Column(
            Modifier.weight(1f).padding(horizontal = 12.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(
                    Modifier.size(36.dp).clip(CircleShape).background(MeshaColors.BrandTint),
                    contentAlignment = Alignment.Center,
                ) { Icon(MeshaIcons.Goat, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(18.dp)) }
                Column(Modifier.weight(1f)) {
                    Text(item.goatDisplayId, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.ExtraBold)
                    Text(item.diseaseName, color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                }
                Text(
                    item.statusLabel,
                    color = MeshaColors.Warn,
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(MeshaColors.WarnX)
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
            Text(
                "Reported for ${item.startDate}. Treatment actions will appear after sync.",
                color = MeshaColors.Muted,
                fontSize = 11.sp,
            )
        }
    }
}

@Composable
private fun HealthDateNavButton(icon: androidx.compose.ui.graphics.vector.ImageVector, onClick: () -> Unit) {
    Box(
        modifier = Modifier.size(48.dp).clip(RoundedCornerShape(12.dp)).background(MeshaColors.Surf2).clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, contentDescription = null, tint = MeshaColors.Ink, modifier = Modifier.size(16.dp)) }
}

@Composable
private fun HealthStatusChips(state: HealthListUiState, onSelect: (String) -> Unit) {
    val chips = listOf(
        "" to ("All" to state.summary.total),
        "due" to ("Due" to state.summary.due),
        "scheduled" to ("Later" to state.summary.scheduled),
        "rework" to ("Rework" to state.summary.rework),
        "completed" to ("Done" to state.summary.completed),
        "held" to ("Held" to state.summary.held),
    )
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        chips.forEach { (key, pair) ->
            val selected = state.status == key
            Row(
                modifier = Modifier.clip(RoundedCornerShape(999.dp))
                    .background(if (selected) MeshaColors.Brand else MeshaColors.Surf2)
                    .clickable { onSelect(key) }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Text(pair.first, color = if (selected) MeshaColors.OnBrand else MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                Text(pair.second.toString(), color = if (selected) MeshaColors.OnBrand else MeshaColors.Faint, fontSize = 11.sp, fontWeight = FontWeight.Bold)
            }
        }
    }
}

@Composable
private fun FilterMenu(label: String, selected: String, options: List<HealthFilterUi>, onSelect: (String) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    Box {
        OutlinedButton(onClick = { expanded = true }) {
            Text(options.firstOrNull { it.key == selected }?.label ?: label)
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            (listOf(HealthFilterUi("", "All $label")) + options).distinctBy { it.key }.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.label) },
                    onClick = { expanded = false; onSelect(option.key) },
                )
            }
        }
    }
}

@Composable
private fun HealthActionCard(item: HealthWorkItemUi, onClick: () -> Unit) {
    val stripe = item.status.healthStatusColor()
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(IntrinsicSize.Min)
            .clip(RoundedCornerShape(16.dp)).background(MeshaColors.Surf).clickable(onClick = onClick),
    ) {
        Box(Modifier.width(4.dp).fillMaxHeight().background(stripe))
        Column(Modifier.weight(1f).padding(horizontal = 12.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(7.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(
                    Modifier.size(36.dp).clip(CircleShape).background(MeshaColors.BrandTint),
                    contentAlignment = Alignment.Center,
                ) { Icon(MeshaIcons.Goat, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(18.dp)) }
                Column(Modifier.weight(1f)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(item.goatDisplayId, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.ExtraBold)
                        Text(
                            item.diseaseName,
                            color = MeshaColors.Muted,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(MeshaColors.Surf3)
                                .padding(horizontal = 8.dp, vertical = 2.dp),
                        )
                    }
                    Text(item.locationLabel, color = MeshaColors.Faint, fontSize = 11.sp)
                }
                Text(
                    if (item.durationDays > 0) "${item.dayNo}/${item.durationDays}" else "Day ${item.dayNo}",
                    color = MeshaColors.Ink,
                    fontSize = 14.sp,
                    fontWeight = FontWeight.ExtraBold,
                )
            }
            if (item.durationDays > 0) {
                LinearProgressIndicator(
                    progress = { item.dayNo.toFloat() / item.durationDays },
                    modifier = Modifier.fillMaxWidth().height(4.dp).clip(RoundedCornerShape(999.dp)),
                    color = stripe,
                    trackColor = MeshaColors.Surf3,
                )
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("${item.sessionLabel} · ${item.medicineLabel}", color = MeshaColors.Ink, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                    Text(item.dayLabel, color = MeshaColors.Muted, fontSize = 11.sp)
                }
                Text(
                    item.status.replace('_', ' '),
                    color = stripe,
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(MeshaColors.Surf2)
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
            if (item.hasCriticalStep) {
                Text("Critical action requires guarded approval", color = MeshaColors.Danger, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}

@Composable
private fun HealthSectionHeader(label: String, color: Color) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(label, color = color, fontSize = 11.sp, fontWeight = FontWeight.ExtraBold)
        Box(Modifier.weight(1f).height(1.dp).background(MeshaColors.Hair))
    }
}

private fun String.healthGroupLabel(): String = when (lowercase()) {
    "completed" -> "Completed"
    "canceled_death" -> "Stopped — death review"
    "rework" -> "Sent back — needs rework"
    "held" -> "Held"
    "scheduled" -> "Later today"
    else -> "Due now"
}

private fun String.healthStatusColor(): Color = when (lowercase()) {
    "completed", "canceled_death" -> MeshaColors.Ok
    "held" -> MeshaColors.Warn
    "rework" -> MeshaColors.Danger
    "scheduled" -> MeshaColors.Muted
    else -> MeshaColors.Brand
}

@Immutable
data class HealthStepUi(
    val id: String,
    val title: String,
    val detail: String,
    val critical: Boolean,
    val status: String = "",
)

@Immutable
data class HealthDetailUiState(
    val loading: Boolean = true,
    val goatDisplayId: String = "",
    val diseaseName: String = "",
    val dayLabel: String = "",
    val locationLabel: String = "",
    val status: String = "",
    val steps: List<HealthStepUi> = emptyList(),
    val submitting: Boolean = false,
    val closing: Boolean = false,
    val refreshing: Boolean = false,
    /** Backend-derived caller capability (health.execute); false hides Complete entirely. */
    val canComplete: Boolean = false,
    val canRecordVideo: Boolean = false,
    /** Backend-derived caller capability (health.diagnose); false hides the outcome action. */
    val canCloseCase: Boolean = false,
    /**
     * The DIAGNOSIS RULE this case was opened under, handed to the death form so a mid-course
     * death is recorded against the disease already on this screen.
     *
     * Blank for a pre-engine case, which is a real state: the death form then opens with its
     * ordinary disease search. It must never be filled from the disease NAME or the treatment
     * card — the write refuses both, and neither can say which illness was meant.
     */
    val registerRuleId: String = "",
    val videoCaptured: Boolean = false,
    val isCapturingVideo: Boolean = false,
    val videoMessage: String? = null,
    val message: String? = null,
)

@Composable
fun HealthDetailScreen(
    state: HealthDetailUiState,
    onBack: () -> Unit,
    onComplete: () -> Unit,
    onRefresh: () -> Unit,
    onRecordVideo: () -> Unit,
    onReRecordVideo: () -> Unit,
    onCloseCase: (String, String) -> Unit,
    /** Open the death form for this animal, with this case's disease as the cause. */
    onMarkDead: () -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume(onRefresh)
    var showOutcomeDialog by remember { mutableStateOf(false) }
    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Health",
            subtitle = state.goatDisplayId,
            onBack = onBack,
            actions = {
                SyncIconButton(isSyncing = state.refreshing, onSync = onRefresh)
            },
        )
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item("header") {
                Card(colors = CardDefaults.cardColors(containerColor = MeshaColors.Surf)) {
                    Column(Modifier.padding(16.dp)) {
                        Text(state.diseaseName, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
                        Text(state.dayLabel)
                        Text(state.locationLabel, color = MeshaColors.Muted)
                        state.message?.let { Text(it, color = MeshaColors.Brand) }
                        if (state.canCloseCase) {
                            Text(
                                "Record case outcome",
                                color = MeshaColors.Brand,
                                fontWeight = FontWeight.Bold,
                                modifier = Modifier
                                    .padding(top = 8.dp)
                                    .clip(RoundedCornerShape(8.dp))
                                    .clickable(enabled = !state.closing) { showOutcomeDialog = true }
                                    .minimumInteractiveComponentSize()
                                    .padding(vertical = 6.dp),
                            )
                            // An animal can die MID-COURSE, and this is where the person treating
                            // it is standing when that happens. It is DANGER-TINTED and separated
                            // from the outcome action above because the two are not alternatives:
                            // recovered/referred/canceled close this case, while a death ends the
                            // animal and, once approved, closes every other case it has open.
                            //
                            // It OPENS THE DEATH FORM rather than recording anything here. A
                            // second way to record a death would mean a second approval path, and
                            // nobody self-authorizes a death.
                            Text(
                                "The animal died",
                                color = MeshaColors.Danger,
                                fontWeight = FontWeight.Bold,
                                modifier = Modifier
                                    .padding(top = 4.dp)
                                    .clip(RoundedCornerShape(8.dp))
                                    .clickable(enabled = !state.closing) { onMarkDead() }
                                    .minimumInteractiveComponentSize()
                                    .padding(vertical = 6.dp),
                            )
                        }
                    }
                }
            }
            items(state.steps.size, key = { state.steps[it].id }) { index ->
                val step = state.steps[index]
                Card(colors = CardDefaults.cardColors(containerColor = if (step.critical) MeshaColors.DangerX else MeshaColors.Surf)) {
                    Column(Modifier.padding(14.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(step.title, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                            step.status.healthStepStatusLabel()?.let { label ->
                                Text(label, color = step.status.healthStatusColor(), fontSize = 11.sp, fontWeight = FontWeight.Bold)
                            }
                        }
                        Spacer(Modifier.height(4.dp))
                        Text(step.detail)
                        if (step.critical) Text("Guarded handoff — no direct animal-state change", color = MeshaColors.Danger)
                    }
                }
            }
        }
        if (state.canRecordVideo) {
            Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                state.videoMessage?.let { Text(it, color = MeshaColors.Muted, fontSize = 12.sp) }
                OutlinedButton(
                    onClick = if (state.videoCaptured) onReRecordVideo else onRecordVideo,
                    enabled = !state.isCapturingVideo && !state.submitting,
                    modifier = Modifier.fillMaxWidth().padding(top = 4.dp),
                ) {
                    Text(
                        when {
                            state.isCapturingVideo -> "Opening camera…"
                            state.videoCaptured -> "Re-record treatment video"
                            else -> "Record treatment video"
                        },
                    )
                }
            }
        }
        Button(
            onClick = onComplete,
            enabled = state.canComplete,
            modifier = Modifier.fillMaxWidth().padding(16.dp),
        ) {
            Text(
                when {
                    state.submitting -> "Saving…"
                    !state.canRecordVideo -> "Completion is recorded by the operator"
                    !state.videoCaptured -> "Record the treatment video first"
                    else -> "Complete this session"
                },
            )
        }
    }
    if (showOutcomeDialog) {
        HealthOutcomeDialog(
            onDismiss = { showOutcomeDialog = false },
            onConfirm = { outcome, note ->
                showOutcomeDialog = false
                onCloseCase(outcome, note)
            },
        )
    }
}

/** The clinical outcome picker (health.diagnose): recovered / referred / canceled + optional note. */
@Composable
private fun HealthOutcomeDialog(
    onDismiss: () -> Unit,
    onConfirm: (String, String) -> Unit,
) {
    var outcome by remember { mutableStateOf("recovered") }
    var note by remember { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Record case outcome") },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text("Closing stops the remaining treatment sessions for this case. Completed work is kept.")
                listOf(
                    "recovered" to "Recovered — the animal is well",
                    "referred" to "Referred — handed to external care",
                    "canceled" to "Canceled — diagnosis withdrawn",
                ).forEach { (key, label) ->
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.fillMaxWidth().clip(RoundedCornerShape(8.dp))
                            .clickable { outcome = key }.padding(vertical = 6.dp),
                    ) {
                        RadioButton(selected = outcome == key, onClick = { outcome = key })
                        Text(label)
                    }
                }
                OutlinedTextField(
                    value = note,
                    onValueChange = { note = it },
                    label = { Text("Note (optional)") },
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = { TextButton(onClick = { onConfirm(outcome, note.trim()) }) { Text("Record outcome") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

private fun String.healthStepStatusLabel(): String? = when (lowercase()) {
    "completed" -> "Done"
    "guarded" -> "Guarded"
    "pending" -> null
    "" -> null
    else -> replaceFirstChar { it.uppercase() }
}
