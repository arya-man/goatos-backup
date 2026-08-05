package sg.mesha.goatos.feature.verify

import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.border
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

// telemetry:exempt: pure stateless renderer — AnalyticsPort/funnel wiring lives in
// VerifyQueueViewModel (:app), which owns every side effect this screen triggers.
/**
 * The verifier-only workspace's reusable queue (context/architecture/verifier-app-and-flow.md):
 * every backend-composed drawer module opens this category-filtered queue of pending media to
 * verify. Row tap drills into [VerifyDetailScreen] (video playback + approve/reject). No
 * capture, no ops, no roster, no config — this + the detail screen are the entire section.
 */

/** Status tone for a queue row's pill and the detail screen's header pill. */
enum class VerifyTone { PENDING, APPROVED, REJECTED }

/** One row in the queue — [categoryLabel]/[title]/[subtitle] are backend-composed display
 *  strings (TRD dumb-renderer rule); the client never derives them from raw ids. [statusTone]
 *  is the one client-owned bit (pending/approved/rejected) — its localized pill TEXT is
 *  resolved by [StatusPill] from a client string resource, never a hardcoded English literal. */
data class VerificationQueueRow(
    val id: String,
    val category: String,
    val categoryLabel: String,
    val title: String,
    val subtitle: String,
    val scopeType: VerifyScopeType = VerifyScopeType.OTHER,
    val shedLabel: String = "",
    val animalLabel: String = "",
    val weightLabel: String = "",
    val mediaCountLabel: String = "",
    val parkLabel: String = "",
    val operatorLabel: String = "",
    val capturedAtLabel: String = "",
    val statusTone: VerifyTone,
)

enum class VerifyScopeType { INDIVIDUAL, LUMP_SUM, OTHER }

data class VerifyDriveClosure(
    val batchId: String,
    val driveLabel: String,
    val batchLabel: String,
    val totalCount: Int,
    val approvedCount: Int,
    val rejectedCount: Int,
    val pendingCount: Int,
    val videoCount: Int,
    val approvedVideos: Int,
    val rejectedVideos: Int,
    val pendingVideos: Int,
    val shedCount: Int,
    val ready: Boolean,
)

/** One backend-defined page tab. [value] is the disjoint raw category filter and [label] is
 *  backend-owned display copy from the verification registry. */
data class VerifyCategoryOption(val value: String?, val label: String?)

/** One backend-defined disjoint secondary tab at verification-item grain. */
data class VerifyStatusOption(val value: String, val label: String)

data class VerifyLocationFilterOption(val value: String?, val label: String)

enum class VerifyModuleTab { VACCINATION, WEIGHING }

@Immutable
data class VerifyQueueUiState(
    val rows: List<VerificationQueueRow> = emptyList(),
    /** Backend-owned module identity + display label for this queue's chrome. Blank when the
     *  category has no module to name -- never substituted with a client-invented one. */
    val moduleKey: String = "",
    val moduleLabel: String = "",
    /** Null when the queue's category has no dedicated chrome here (counts, feed) or could not
     *  be resolved at all -- never coerced to a module the verifier did not ask for. */
    val selectedModule: VerifyModuleTab? = null,
    val isUnsupportedModule: Boolean = false,
    val isActionQueue: Boolean = false,
    val categoryOptions: List<VerifyCategoryOption> = emptyList(),
    val selectedCategory: String? = null,
    val statusOptions: List<VerifyStatusOption> = emptyList(),
    val selectedStatus: String = "pending",
    val selectedBusinessDate: String = "",
    val businessTimezone: String = "Asia/Kolkata",
    val missedOnly: Boolean = false,
    val hasMissed: Boolean = false,
    val parkOptions: List<VerifyLocationFilterOption> = emptyList(),
    val selectedParkId: String? = null,
    val shedOptions: List<VerifyLocationFilterOption> = emptyList(),
    val selectedShedId: String? = null,
    // Offline-first sync state (docs/decisions/android-offline-first.md).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    // Keyset pagination (~20/page) — see docs/decisions/mobile-data-fetch-anti-patterns.md.
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
    val driveClosures: List<VerifyDriveClosure> = emptyList(),
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
)

sealed interface VerifyQueueEvent {
    data class SelectCategory(val category: String?) : VerifyQueueEvent
    data class SelectModule(val module: VerifyModuleTab) : VerifyQueueEvent
    data class SelectPark(val parkId: String?) : VerifyQueueEvent
    data class SelectShed(val shedId: String?) : VerifyQueueEvent
    data class SelectStatus(val status: String) : VerifyQueueEvent
    data class SelectBusinessDate(val businessDate: String) : VerifyQueueEvent
    data object ToggleMissed : VerifyQueueEvent
    /** [category] is the tapped row's OWN category (never the queue's filter selection) — the
     *  nav host threads it into the detail route so that screen re-observes the exact same Room
     *  cache scope this row came from, with no extra network call. */
    data class OpenItem(val itemId: String, val category: String) : VerifyQueueEvent
    data object Refresh : VerifyQueueEvent
    data object LoadMore : VerifyQueueEvent
    data class CloseDrive(val batchId: String) : VerifyQueueEvent
    /** ONE summary per screen exit, never per scroll frame — see
     *  `AnalyticsEvents.VERIFY_QUEUE_SCROLL_SUMMARY`. */
    data class ScrollSummary(val maxScrollIndex: Int, val rowCount: Int) : VerifyQueueEvent
}

@Composable
fun VerifyQueueScreen(
    state: VerifyQueueUiState,
    onEvent: (VerifyQueueEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(VerifyQueueEvent.Refresh) }
    val listState = rememberLazyListState()
    var selectedWeighingScope by remember { mutableStateOf<VerifyScopeType?>(null) }
    var datePickerOpen by remember { mutableStateOf(false) }
    LaunchedEffect(listState, state.hasMore, state.isLoadingMore, state.rows.size, state.selectedCategory) {
        if (
            !state.hasMore ||
            state.isLoadingMore ||
            state.rows.isEmpty()
        ) {
            return@LaunchedEffect
        }
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { lastVisibleIndex ->
                if (lastVisibleIndex >= state.rows.lastIndex - 3 && state.hasMore && !state.isLoadingMore) {
                    onEvent(VerifyQueueEvent.LoadMore)
                }
            }
    }
    // Scroll depth is THROTTLED to one summary per screen exit — an event per scroll frame would
    // drown the funnel and drain the battery. Track only the deepest index reached in memory,
    // then flush it once on dispose (nav-away, process death excepted).
    var maxScrollIndexSeen by remember { mutableStateOf(0) }
    val latestRowCount by rememberUpdatedState(state.rows.size)
    LaunchedEffect(listState) {
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { lastVisibleIndex ->
                if (lastVisibleIndex > maxScrollIndexSeen) maxScrollIndexSeen = lastVisibleIndex
            }
    }
    DisposableEffect(Unit) {
        onDispose { onEvent(VerifyQueueEvent.ScrollSummary(maxScrollIndexSeen, latestRowCount)) }
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        QueueHeader(
            state = state,
            onRefresh = { onEvent(VerifyQueueEvent.Refresh) },
            onMissed = { onEvent(VerifyQueueEvent.ToggleMissed) },
        )
        if (state.categoryOptions.isNotEmpty()) {
            CategoryFilterRow(
                options = state.categoryOptions,
                selected = state.selectedCategory,
                onSelect = { onEvent(VerifyQueueEvent.SelectCategory(it)) },
            )
        }
        if (!state.isActionQueue && state.statusOptions.isNotEmpty()) {
            StatusFilterRow(
                options = state.statusOptions,
                selected = state.selectedStatus,
                onSelect = { onEvent(VerifyQueueEvent.SelectStatus(it)) },
            )
            BusinessDateRow(
                businessDate = state.selectedBusinessDate,
                businessTimezone = state.businessTimezone,
                missedOnly = state.missedOnly,
                onPrevious = {
                    state.selectedBusinessDate.toLocalDateOrNull()?.minusDays(1)?.let {
                        onEvent(VerifyQueueEvent.SelectBusinessDate(it.toString()))
                    }
                },
                onNext = {
                    val today = LocalDate.now(ZoneId.of(state.businessTimezone))
                    state.selectedBusinessDate.toLocalDateOrNull()?.plusDays(1)?.takeIf { !it.isAfter(today) }?.let {
                        onEvent(VerifyQueueEvent.SelectBusinessDate(it.toString()))
                    }
                },
                onOpenCalendar = { datePickerOpen = true },
            )
        }
        if (state.moduleKey == "weighing") {
            WeighingScopeTabs(
                rows = state.rows,
                selected = selectedWeighingScope,
                onSelect = { selectedWeighingScope = it },
            )
        }
        if (state.parkOptions.size > 1) {
            LocationFilterRow(
                options = state.parkOptions,
                selected = state.selectedParkId,
                onSelect = { onEvent(VerifyQueueEvent.SelectPark(it)) },
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        if (state.shedOptions.size > 1) {
            LocationFilterRow(
                options = state.shedOptions,
                selected = state.selectedShedId,
                onSelect = { onEvent(VerifyQueueEvent.SelectShed(it)) },
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        ) {
            if (state.isActionQueue) {
                items(state.driveClosures, key = { it.batchId }) { closure ->
                    DriveCloseCard(
                        closure = closure,
                        isClosing = state.closingBatchId == closure.batchId,
                        errorMessage = state.closeErrorMessage.takeIf { state.closeErrorBatchId == closure.batchId },
                        onClose = { onEvent(VerifyQueueEvent.CloseDrive(closure.batchId)) },
                    )
                }
            }
            if (state.rows.isEmpty() && state.isRefreshing && state.lastSyncedAt == null) {
                item { LoadingSkeletonList(modifier = Modifier.fillMaxWidth()) }
            } else if (state.isUnsupportedModule) {
                // An empty "all caught up" here would be a lie: nothing was read at all.
                item {
                    EmptyState(
                        title = stringResource(R.string.verify_queue_unsupported_module),
                        subtitle = stringResource(R.string.verify_queue_unsupported_module_subtitle),
                        icon = MeshaIcons.Video,
                        tone = EmptyTone.Neutral,
                    )
                }
            } else if (state.rows.isEmpty()) {
                item {
                    EmptyState(
                        title = stringResource(if (state.isActionQueue) R.string.verify_action_queue_empty else R.string.verify_queue_empty),
                        subtitle = stringResource(if (state.isActionQueue) R.string.verify_action_queue_empty_subtitle else R.string.verify_queue_empty_date_subtitle),
                        icon = MeshaIcons.Video,
                        tone = EmptyTone.Positive,
                    )
                }
            } else {
                if (state.moduleKey == "weighing") {
                    val visibleRows = if (selectedWeighingScope != null) {
                        state.rows.filter { it.scopeType == selectedWeighingScope }
                    } else {
                        state.rows
                    }
                    val shedGroups = visibleRows.groupBy { it.shedLabel.ifBlank { it.title.ifBlank { it.categoryLabel } } }
                    shedGroups.forEach { (shedLabel, rows) ->
                        item(key = "shed-$shedLabel") {
                            ShedGroupHeader(shedLabel = shedLabel, rows = rows)
                        }
                        items(rows, key = { it.id }) { row ->
                            QueueRowCard(row = row, hierarchical = true, onClick = { onEvent(VerifyQueueEvent.OpenItem(row.id, row.category)) })
                        }
                    }
                } else {
                    items(state.rows, key = { it.id }) { row ->
                        QueueRowCard(row = row, hierarchical = false, onClick = { onEvent(VerifyQueueEvent.OpenItem(row.id, row.category)) })
                    }
                }
                if (state.isLoadingMore) {
                    item {
                        InlineLoadingFooter()
                    }
                }
            }
            item { Spacer(Modifier.size(24.dp)) }
        }
    }
    if (datePickerOpen) {
        val selectedMillis = state.selectedBusinessDate.toLocalDateOrNull()
            ?.atStartOfDay(ZoneOffset.UTC)
            ?.toInstant()
            ?.toEpochMilli()
        val pickerState = rememberDatePickerState(initialSelectedDateMillis = selectedMillis)
        DatePickerDialog(
            onDismissRequest = { datePickerOpen = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let { millis ->
                        val selected = Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate()
                        val today = LocalDate.now(ZoneId.of(state.businessTimezone))
                        if (!selected.isAfter(today)) onEvent(VerifyQueueEvent.SelectBusinessDate(selected.toString()))
                    }
                    datePickerOpen = false
                }) { Text(stringResource(android.R.string.ok)) }
            },
            dismissButton = {
                TextButton(onClick = { datePickerOpen = false }) { Text(stringResource(android.R.string.cancel)) }
            },
        ) { DatePicker(state = pickerState) }
    }
}

@Composable
private fun DriveCloseCard(
    closure: VerifyDriveClosure,
    isClosing: Boolean,
    errorMessage: String?,
    onClose: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 10.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.OkX)
            .border(1.dp, MeshaColors.Ok.copy(alpha = 0.35f), RoundedCornerShape(16.dp))
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    text = closure.batchLabel.ifBlank { stringResource(R.string.verify_drive_ready_title) },
                    color = MeshaColors.Ink,
                    fontSize = 14.5.sp,
                    fontWeight = FontWeight.W800,
                )
                if (closure.driveLabel.isNotBlank()) {
                    Text(
                        text = closure.driveLabel,
                        color = MeshaColors.Muted,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.W700,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
                Text(
                    text = stringResource(
                        R.string.verify_drive_ready_subtitle,
                        closure.totalCount,
                        closure.shedCount,
                        closure.approvedVideos,
                        closure.videoCount,
                    ),
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
                Text(
                    text = stringResource(
                        R.string.verify_drive_ready_detail,
                        closure.pendingVideos,
                        closure.rejectedVideos,
                    ),
                    color = MeshaColors.Faint,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Ok)
                    .clickable(enabled = !isClosing, onClick = onClose)
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (isClosing) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), color = MeshaColors.Surf, strokeWidth = 2.dp)
                } else {
                    Icon(MeshaIcons.CheckCircle, contentDescription = null, tint = MeshaColors.Surf, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.size(6.dp))
                    Text(
                        text = stringResource(R.string.verify_drive_close),
                        color = MeshaColors.Surf,
                        fontSize = 12.5.sp,
                        fontWeight = FontWeight.W800,
                    )
                }
            }
        }
        errorMessage?.let {
            Text(
                text = it,
                color = MeshaColors.Danger,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}

@Composable
private fun WeighingScopeTabs(
    rows: List<VerificationQueueRow>,
    selected: VerifyScopeType?,
    onSelect: (VerifyScopeType?) -> Unit,
) {
    val individualCount = rows.count { it.scopeType == VerifyScopeType.INDIVIDUAL }
    val lumpSumCount = rows.count { it.scopeType == VerifyScopeType.LUMP_SUM }
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.padding(bottom = 8.dp),
    ) {
        item {
            CategoryChip(
                label = stringResource(R.string.verify_scope_all),
                selected = selected == null,
                onClick = { onSelect(null) },
            )
        }
        item {
            CategoryChip(
                label = stringResource(R.string.verify_scope_individual, individualCount),
                selected = selected == VerifyScopeType.INDIVIDUAL,
                onClick = { onSelect(VerifyScopeType.INDIVIDUAL) },
            )
        }
        item {
            CategoryChip(
                label = stringResource(R.string.verify_scope_lumpsum, lumpSumCount),
                selected = selected == VerifyScopeType.LUMP_SUM,
                onClick = { onSelect(VerifyScopeType.LUMP_SUM) },
            )
        }
    }
}

@Composable
private fun ShedGroupHeader(shedLabel: String, rows: List<VerificationQueueRow>) {
    val videoCount = rows.sumOf { row ->
        row.mediaCountLabel.substringBefore(' ').toIntOrNull() ?: 0
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 10.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                text = shedLabel,
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W800,
            )
            Text(
                text = stringResource(R.string.verify_shed_group_summary, rows.size, videoCount),
                color = MeshaColors.Faint,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(top = 1.dp),
            )
        }
        Box(
            modifier = Modifier
                .height(1.dp)
                .weight(1f)
                .background(MeshaColors.Hair),
        )
    }
}

@Composable
// The eyebrow is the BACKEND's own module label (filterOptions.moduleLabel), not a client
// string table keyed off a guessed module: it is blank exactly when there is no module to name,
// which is the case main's client-side `when` existed to protect (a Counts verifier must never
// read "VACCINATION"), and it satisfies the backend-owns-visible-copy rule at the same time.
private fun QueueHeader(state: VerifyQueueUiState, onRefresh: () -> Unit, onMissed: () -> Unit) {
    MeshaScreenHeader(
        eyebrow = null,
        // The title is the MODULE the nav bar is on -- Counts, Vaccination, Weighing. A verifier
        // only ever verifies videos, so "Video verification" spent the most prominent line
        // restating the job (and wrapped to two lines doing it) while the one thing that actually
        // changes between taps -- which module you are looking at -- was demoted to an eyebrow.
        title = state.moduleLabel.takeIf { !state.isActionQueue && it.isNotBlank() }
            ?: stringResource(if (state.isActionQueue) R.string.verify_action_queue_title else R.string.verify_queue_title),
        below = {
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                hasData = state.lastSyncedAt != null || state.rows.isNotEmpty(),
                isOffline = state.isOffline,
                modifier = Modifier.padding(top = 2.dp),
            )
        },
        actions = {
            if (!state.isActionQueue) {
                Box {
                    IconButton(onClick = onMissed) {
                        Icon(
                            imageVector = MeshaIcons.Bell,
                            contentDescription = stringResource(R.string.verify_missed_open),
                            tint = if (state.missedOnly) MeshaColors.Brand else MeshaColors.Muted,
                        )
                    }
                    if (state.hasMissed) {
                        Box(
                            Modifier
                                .align(Alignment.TopEnd)
                                .padding(top = 8.dp, end = 8.dp)
                                .size(9.dp)
                                .clip(RoundedCornerShape(999.dp))
                                .background(MeshaColors.Danger),
                        )
                    }
                }
            }
            SyncIconButton(
                isSyncing = state.isRefreshing,
                onSync = onRefresh,
                contentDescription = stringResource(R.string.verify_queue_refresh),
            )
        },
    )
}

@Composable
private fun StatusFilterRow(
    options: List<VerifyStatusOption>,
    selected: String,
    onSelect: (String) -> Unit,
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.padding(bottom = 8.dp),
    ) {
        items(options, key = { it.value }) { option ->
            CategoryChip(label = option.label, selected = option.value == selected, onClick = { onSelect(option.value) })
        }
    }
}

@Composable
private fun BusinessDateRow(
    businessDate: String,
    businessTimezone: String,
    missedOnly: Boolean,
    onPrevious: () -> Unit,
    onNext: () -> Unit,
    onOpenCalendar: () -> Unit,
) {
    val selected = businessDate.toLocalDateOrNull()
    val today = LocalDate.now(ZoneId.of(businessTimezone))
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onPrevious, enabled = !missedOnly) {
            Icon(MeshaIcons.ChevronLeft, contentDescription = stringResource(R.string.verify_date_previous), tint = MeshaColors.Muted)
        }
        Row(
            modifier = Modifier
                .weight(1f)
                .minimumInteractiveComponentSize()
                .clip(RoundedCornerShape(12.dp))
                .background(if (missedOnly) MeshaColors.WarnX else MeshaColors.Surf2)
                .border(1.dp, if (missedOnly) MeshaColors.Warn else MeshaColors.Hair, RoundedCornerShape(12.dp))
                .clickable(onClick = onOpenCalendar)
                .padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.Center,
        ) {
            Icon(MeshaIcons.Calendar, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(17.dp))
            Spacer(Modifier.size(7.dp))
            Text(
                text = if (missedOnly) stringResource(R.string.verify_missed_before_today) else selected?.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)).orEmpty(),
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
        }
        IconButton(onClick = onNext, enabled = !missedOnly && selected != null && selected.isBefore(today)) {
            Icon(MeshaIcons.Chevron, contentDescription = stringResource(R.string.verify_date_next), tint = MeshaColors.Muted)
        }
    }
}

private fun String.toLocalDateOrNull(): LocalDate? = runCatching { LocalDate.parse(this) }.getOrNull()

@Composable
private fun CategoryFilterRow(
    options: List<VerifyCategoryOption>,
    selected: String?,
    onSelect: (String?) -> Unit,
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.padding(bottom = 8.dp),
    ) {
        items(options, key = { it.value ?: "__all__" }) { option ->
            CategoryChip(
                label = option.label ?: stringResource(R.string.verify_category_all),
                selected = option.value == selected,
                onClick = { onSelect(option.value) },
            )
        }
    }
}

@Composable
private fun LocationFilterRow(
    options: List<VerifyLocationFilterOption>,
    selected: String?,
    onSelect: (String?) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = modifier,
    ) {
        items(options, key = { it.value ?: "__all__" }) { option ->
            CategoryChip(
                label = option.label,
                selected = option.value == selected,
                onClick = { onSelect(option.value) },
            )
        }
    }
}

@Composable
private fun CategoryChip(label: String, selected: Boolean, onClick: () -> Unit) {
    val bg = if (selected) MeshaColors.BrandTint else MeshaColors.Surf2
    val fg = if (selected) MeshaColors.BrandD else MeshaColors.Muted
    val border = if (selected) MeshaColors.Brand else MeshaColors.Hair
    Text(
        text = label,
        color = fg,
        fontSize = 12.5.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    )
}

@Composable
private fun QueueRowCard(row: VerificationQueueRow, hierarchical: Boolean, onClick: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 10.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Video,
                    contentDescription = null,
                    tint = MeshaColors.Brand,
                    modifier = Modifier.size(18.dp),
                )
            }
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    text = if (hierarchical) row.animalLabel.ifBlank { row.title }.ifBlank { row.shedLabel } else row.title,
                    color = MeshaColors.Ink,
                    fontSize = 14.5.sp,
                    fontWeight = FontWeight.W700,
                )
                Text(
                    text = if (hierarchical) {
                        listOf(row.weightLabel, row.mediaCountLabel, row.operatorLabel)
                            .filter { it.isNotBlank() }
                            .joinToString(" · ")
                            .ifBlank { row.subtitle }
                    } else {
                        row.subtitle
                    },
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            StatusPill(tone = row.statusTone)
        }
        Text(
            text = if (hierarchical) row.scopeType.label() else row.categoryLabel,
            color = MeshaColors.Faint,
            fontSize = 11.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(top = 10.dp),
        )
    }
}

private fun VerifyScopeType.label(): String = when (this) {
    VerifyScopeType.INDIVIDUAL -> "Individual"
    VerifyScopeType.LUMP_SUM -> "Lump-sum"
    VerifyScopeType.OTHER -> "Evidence"
}

/** Resolves the LOCALIZED status label for [tone] — the pill text is never a hardcoded
 *  English literal from the ViewModel; only the tone (pending/approved/rejected) is data. */
@Composable
private fun statusToneLabel(tone: VerifyTone): String = when (tone) {
    VerifyTone.PENDING -> stringResource(R.string.verify_status_pending)
    VerifyTone.APPROVED -> stringResource(R.string.verify_status_approved)
    VerifyTone.REJECTED -> stringResource(R.string.verify_status_rejected)
}

@Composable
internal fun StatusPill(tone: VerifyTone) {
    val (bg, fg) = when (tone) {
        VerifyTone.PENDING -> MeshaColors.WarnX to MeshaColors.Warn
        VerifyTone.APPROVED -> MeshaColors.OkX to MeshaColors.Ok
        VerifyTone.REJECTED -> MeshaColors.DangerX to MeshaColors.Danger
    }
    Text(
        text = statusToneLabel(tone),
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

@Composable
private fun InlineLoadingFooter() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 6.dp)
            .padding(vertical = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(modifier = Modifier.size(16.dp), color = MeshaColors.Muted, strokeWidth = 2.dp)
    }
}
