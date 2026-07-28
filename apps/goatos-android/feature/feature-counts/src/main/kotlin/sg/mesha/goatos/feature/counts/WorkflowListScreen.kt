package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; WorkflowListViewModel (in :app) owns the
// workflow_list_* AnalyticsEvents + the CrashReporter non-fatal on every page-load failure.

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
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import kotlinx.coroutines.delay
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The Birth / Death work-list screens (`/counts/birth`, `/counts/death` —
 * docs/decisions/birth-death-workflows.md, mock/birth-death-mobile-mock.html): each module OPENS on
 * the outstanding per-goat SOP work created by applied birth/death events; recording a new event
 * lives behind the ＋ button (straight to the add form). Cards are a bounded Room-backed Paging
 * window (~20/page keyset); chips/counts/labels are backend-owned and rendered verbatim. Group
 * headers (Overdue / Due today / In review / Completed) are client-side PRESENTATION derived from
 * each card's own backend fields — never re-derived business truth.
 */

/** Which module this list renders. Determines the accent (death is danger-tinted) and copy. */
enum class WorkflowModuleUi { BIRTH, DEATH }

/** One filter chip: backend bucket [key], display [label], backend-computed [count]. */
@Immutable
data class WorkflowChipUi(val key: String, val label: String, val count: Int)

/** One previous business date with at least one actionable overdue workflow card. */
@Immutable
data class WorkflowOverdueDateUi(
    val dateIso: String,
    val dateLabel: String,
    val workflowCount: Int,
)

/** Client-side presentation grouping of a card, derived from its own backend fields. */
enum class WorkflowCardBucket { OVERDUE, DUE, IN_REVIEW, COMPLETED }

/** One per-goat card. Every display value is backend-owned; the screen renders, never derives. */
@Immutable
data class WorkflowCardUi(
    val workflowId: String,
    val displayId: String,
    val roleLabel: String,
    val metaLine: String,
    val actionsDone: Int,
    val actionsTotal: Int,
    /** "Next" / "Done" prefix + the next action title (or the awaiting-verification copy). */
    val nextKindLabel: String,
    val nextTitle: String,
    /** Due chip copy ("2h late" / "15:00" / "—"), with [overdue] carrying the tone. */
    val dueLabel: String,
    val overdue: Boolean,
    val bucket: WorkflowCardBucket,
    /** Both operator proofs are submitted; admin/verifier state remains intentionally internal. */
    val operatorSubmitted: Boolean = false,
) {
    /** Submitted cards remain openable so the operator can review both recorded video rows. */
    val canOpenDetail: Boolean get() = true
}

@Immutable
data class WorkflowListUiState(
    val module: WorkflowModuleUi = WorkflowModuleUi.BIRTH,
    /** Header subtitle ("CPT · 12 open events") — built by the VM from backend chips. */
    val subtitle: String = "",
    /** The selected business date (ISO `YYYY-MM-DD`, Asia/Kolkata) and its display label. */
    val dateIso: String = "",
    val dateLabel: String = "",
    val isToday: Boolean = true,
    val chips: List<WorkflowChipUi> = emptyList(),
    val overdueDates: List<WorkflowOverdueDateUi> = emptyList(),
    val selectedFilter: String = "all",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    /** Transient acknowledgement returned by the add-birth child destination. */
    val submissionNotice: String? = null,
)

sealed interface WorkflowListEvent {
    data object Refresh : WorkflowListEvent
    data class SelectFilter(val key: String) : WorkflowListEvent
    data object PrevDay : WorkflowListEvent
    data object NextDay : WorkflowListEvent
    data object Today : WorkflowListEvent
    data class SelectDate(val dateIso: String) : WorkflowListEvent
    data class OpenOverdueDate(val dateIso: String) : WorkflowListEvent
    data class OpenCard(val workflowId: String) : WorkflowListEvent

    /** ＋ — straight to the add form (maintainer decision: no chooser sheet). */
    data object AddNew : WorkflowListEvent
    data object Back : WorkflowListEvent
}

@Composable
fun WorkflowListScreen(
    state: WorkflowListUiState,
    rows: LazyPagingItems<WorkflowCardUi>,
    onEvent: (WorkflowListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Offline-first read screen: cached cards render instantly; landing on / returning to the
    // screen fires a background refresh (docs/decisions/android-offline-first.md).
    RefreshOnResume { onEvent(WorkflowListEvent.Refresh) }

    val isDeath = state.module == WorkflowModuleUi.DEATH
    val accent = if (isDeath) MeshaColors.Danger else MeshaColors.Brand

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = stringResource(
                if (isDeath) R.string.counts_workflow_death_title else R.string.counts_workflow_birth_title,
            ),
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(WorkflowListEvent.Back) },
            actions = {
                WorkflowOverdueAlert(state.overdueDates, onEvent)
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(WorkflowListEvent.Refresh) },
                )
                // The prominent ＋ — the ONLY way in to recording (mock's addbtn). 48dp: the
                // a11y minimum touch target, tapped wearing gloves.
                Box(
                    modifier = Modifier
                        .size(48.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .background(accent)
                        .clickable { onEvent(WorkflowListEvent.AddNew) },
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = MeshaIcons.Plus,
                        contentDescription = stringResource(
                            if (isDeath) R.string.counts_workflow_add_death else R.string.counts_workflow_add_birth,
                        ),
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
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        state.submissionNotice?.let { notice ->
            CountsResultBanner(
                result = CountsWriteResultUi(CountsWriteStatus.QUEUED, notice),
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        WorkflowDateBar(state, accent, onEvent)
        if (!state.isToday) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 4.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(MeshaColors.WarnX)
                    .padding(horizontal = 10.dp, vertical = 7.dp),
            ) {
                Text(
                    text = stringResource(R.string.counts_workflow_past_note),
                    color = MeshaColors.Warn,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W600,
                )
            }
        }
        WorkflowChipsRow(state.chips, state.selectedFilter, accent, onEvent)

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Goat,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }
            items(count = rows.itemCount, key = rows.itemKey { it.workflowId }) { index ->
                val card = rows[index] ?: return@items
                // Group header when this card starts a new presentation bucket. peek() never
                // triggers a page load; the header is derived from the loaded page's own fields.
                val previousBucket = if (index == 0) null else runCatching { rows.peek(index - 1)?.bucket }.getOrNull()
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (card.bucket != previousBucket) {
                        WorkflowGroupHeader(card.bucket, isDeath)
                    }
                    WorkflowCardRow(card, isDeath) { onEvent(WorkflowListEvent.OpenCard(card.workflowId)) }
                }
            }
        }
    }
}

@Composable
private fun WorkflowOverdueAlert(
    dates: List<WorkflowOverdueDateUi>,
    onEvent: (WorkflowListEvent) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    LaunchedEffect(expanded) {
        if (expanded) {
            delay(5_000)
            expanded = false
        }
    }
    Box {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.WarnX)
                .clickable { expanded = true },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Bell,
                contentDescription = stringResource(R.string.counts_workflow_previous_overdue_open),
                tint = MeshaColors.Warn,
                modifier = Modifier.size(21.dp),
            )
            if (dates.isNotEmpty()) {
                Text(
                    text = dates.sumOf { it.workflowCount }.coerceAtMost(99).toString(),
                    color = MeshaColors.OnBrand,
                    fontSize = 9.sp,
                    fontWeight = FontWeight.W800,
                    modifier = Modifier
                        .align(Alignment.TopEnd)
                        .padding(top = 3.dp, end = 3.dp)
                        .clip(CircleShape)
                        .background(MeshaColors.Danger)
                        .padding(horizontal = 4.dp, vertical = 1.dp),
                )
            }
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            Text(
                text = stringResource(R.string.counts_workflow_previous_overdue_title),
                color = MeshaColors.Ink,
                fontSize = 12.sp,
                fontWeight = FontWeight.W800,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
            )
            if (dates.isEmpty()) {
                Text(
                    text = stringResource(R.string.counts_workflow_previous_overdue_empty),
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                )
            }
            dates.forEach { item ->
                DropdownMenuItem(
                    text = {
                        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Text(item.dateLabel, color = MeshaColors.Ink, fontWeight = FontWeight.W700)
                            Text(
                                text = stringResource(
                                    R.string.counts_workflow_previous_overdue_count,
                                    item.workflowCount,
                                ),
                                color = MeshaColors.Warn,
                                fontSize = 11.sp,
                            )
                        }
                    },
                    onClick = {
                        expanded = false
                        onEvent(WorkflowListEvent.OpenOverdueDate(item.dateIso))
                    },
                    leadingIcon = {
                        Icon(
                            imageVector = MeshaIcons.Calendar,
                            contentDescription = null,
                            tint = MeshaColors.Warn,
                        )
                    },
                )
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Date bar — ‹ [📅 Today · 27 Jul] › (+ "Today" quick chip on a past day)
// ---------------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun WorkflowDateBar(
    state: WorkflowListUiState,
    accent: Color,
    onEvent: (WorkflowListEvent) -> Unit,
) {
    var pickerOpen by remember { mutableStateOf(false) }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        DateNavButton(icon = MeshaIcons.ChevronLeft, enabled = true) { onEvent(WorkflowListEvent.PrevDay) }
        Row(
            modifier = Modifier
                .weight(1f)
                .clip(RoundedCornerShape(12.dp))
                .background(MeshaColors.Surf2)
                .clickable { pickerOpen = true }
                .padding(vertical = 10.dp),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = MeshaIcons.Calendar,
                contentDescription = null,
                tint = MeshaColors.Muted,
                modifier = Modifier.size(15.dp),
            )
            Text(
                text = state.dateLabel,
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(start = 6.dp),
            )
        }
        DateNavButton(icon = MeshaIcons.Chevron, enabled = !state.isToday) { onEvent(WorkflowListEvent.NextDay) }
        if (!state.isToday) {
            Text(
                text = stringResource(R.string.counts_workflow_today),
                color = accent,
                fontSize = 12.sp,
                fontWeight = FontWeight.W800,
                modifier = Modifier
                    .clip(RoundedCornerShape(10.dp))
                    .clickable { onEvent(WorkflowListEvent.Today) }
                    .minimumInteractiveComponentSize()
                    .padding(horizontal = 6.dp),
            )
        }
    }
    if (pickerOpen) {
        // Calendar jump capped at today: future days have no birth/death events by definition.
        val todayUtcMillis = LocalDate.now(IST).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
        val pickerState = rememberDatePickerState(
            initialSelectedDateMillis = state.dateIso.toUtcMillisOrNull() ?: todayUtcMillis,
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long): Boolean = utcTimeMillis <= todayUtcMillis
            },
        )
        DatePickerDialog(
            onDismissRequest = { pickerOpen = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let { millis ->
                        onEvent(WorkflowListEvent.SelectDate(millis.toIsoDate()))
                    }
                    pickerOpen = false
                }) { Text(stringResource(id = android.R.string.ok)) }
            },
            dismissButton = {
                TextButton(onClick = { pickerOpen = false }) {
                    Text(stringResource(id = android.R.string.cancel))
                }
            },
        ) {
            DatePicker(state = pickerState)
        }
    }
}

@Composable
private fun DateNavButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(48.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
            modifier = Modifier.size(16.dp),
        )
    }
}

// ---------------------------------------------------------------------------
// Filter chips — horizontally scrolling, counts are the backend's own
// ---------------------------------------------------------------------------

@Composable
private fun WorkflowChipsRow(
    chips: List<WorkflowChipUi>,
    selectedKey: String,
    accent: Color,
    onEvent: (WorkflowListEvent) -> Unit,
) {
    if (chips.isEmpty()) return
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        chips.forEach { chip ->
            val selected = chip.key == selectedKey
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .background(if (selected) accent else MeshaColors.Surf2)
                    .clickable { onEvent(WorkflowListEvent.SelectFilter(chip.key)) }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Text(
                    text = chip.label,
                    color = if (selected) MeshaColors.OnBrand else MeshaColors.Muted,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                )
                Text(
                    text = chip.count.toString(),
                    color = if (selected) MeshaColors.OnBrand else MeshaColors.Faint,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W800,
                )
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Group header + card
// ---------------------------------------------------------------------------

@Composable
private fun WorkflowGroupHeader(bucket: WorkflowCardBucket, isDeath: Boolean) {
    val (label, color) = when (bucket) {
        WorkflowCardBucket.OVERDUE ->
            stringResource(R.string.counts_workflow_group_overdue) to if (isDeath) MeshaColors.Danger else MeshaColors.Warn
        WorkflowCardBucket.DUE -> stringResource(R.string.counts_workflow_group_due) to MeshaColors.Muted
        WorkflowCardBucket.IN_REVIEW -> stringResource(R.string.counts_workflow_group_in_review) to MeshaColors.Muted
        WorkflowCardBucket.COMPLETED -> stringResource(R.string.counts_workflow_group_completed) to MeshaColors.Ok
    }
    WorkflowSectionHeader(label, color)
}

@Composable
private fun WorkflowSectionHeader(label: String, color: Color) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = label, color = color, fontSize = 11.sp, fontWeight = FontWeight.W800)
        Box(
            modifier = Modifier
                .weight(1f)
                .height(1.dp)
                .background(MeshaColors.Hair),
        )
    }
}

@Composable
private fun WorkflowCardRow(card: WorkflowCardUi, isDeath: Boolean, onClick: () -> Unit) {
    val stripe = when {
        card.bucket == WorkflowCardBucket.COMPLETED -> MeshaColors.Ok
        card.bucket == WorkflowCardBucket.OVERDUE && isDeath -> MeshaColors.Danger
        card.bucket == WorkflowCardBucket.OVERDUE -> MeshaColors.Warn
        else -> Color.Transparent
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .height(IntrinsicSize.Min)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .clickable(enabled = card.canOpenDetail, onClick = onClick),
    ) {
        Box(modifier = Modifier.width(4.dp).fillMaxHeight().background(stripe))
        Column(
            modifier = Modifier
                .weight(1f)
                .padding(horizontal = 12.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(7.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(
                    modifier = Modifier
                        .size(36.dp)
                        .clip(CircleShape)
                        .background(if (isDeath) MeshaColors.DangerX else MeshaColors.BrandTint),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = if (isDeath) MeshaIcons.Warn else MeshaIcons.Goat,
                        contentDescription = null,
                        tint = if (isDeath) MeshaColors.Danger else MeshaColors.BrandD,
                        modifier = Modifier.size(18.dp),
                    )
                }
                Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(
                            text = card.displayId,
                            color = MeshaColors.Ink,
                            fontSize = 15.sp,
                            fontWeight = FontWeight.W800,
                        )
                        if (card.roleLabel.isNotBlank()) {
                            Text(
                                text = card.roleLabel,
                                color = MeshaColors.Muted,
                                fontSize = 11.sp,
                                fontWeight = FontWeight.W700,
                                modifier = Modifier
                                    .clip(RoundedCornerShape(999.dp))
                                    .background(MeshaColors.Surf3)
                                    .padding(horizontal = 8.dp, vertical = 2.dp),
                            )
                        }
                    }
                    if (card.metaLine.isNotBlank()) {
                        Text(text = card.metaLine, color = MeshaColors.Faint, fontSize = 11.sp)
                    }
                }
                Text(
                    text = "${card.actionsDone}/${card.actionsTotal}",
                    color = MeshaColors.Ink,
                    fontSize = 14.sp,
                    fontWeight = FontWeight.W800,
                )
            }
            // Thin progress bar — the backend's own n/total, never recomputed from the action list.
            val fraction = if (card.actionsTotal > 0) card.actionsDone.toFloat() / card.actionsTotal else 0f
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(4.dp)
                    .clip(RoundedCornerShape(999.dp))
                    .background(MeshaColors.Surf3),
            ) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth(fraction.coerceIn(0f, 1f))
                        .height(4.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(if (card.bucket == WorkflowCardBucket.COMPLETED) MeshaColors.Ok else stripe.takeIf { it != Color.Transparent } ?: MeshaColors.Brand),
                )
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(
                    text = card.nextKindLabel,
                    color = MeshaColors.Faint,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W700,
                )
                Text(
                    text = if (card.operatorSubmitted) {
                        stringResource(R.string.counts_workflow_submitted)
                    } else {
                        card.nextTitle
                    },
                    color = MeshaColors.Ink,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W600,
                    modifier = Modifier.weight(1f),
                )
                if (card.dueLabel.isNotBlank()) {
                    Text(
                        text = card.dueLabel,
                        color = if (card.overdue) {
                            if (isDeath) MeshaColors.Danger else MeshaColors.Warn
                        } else {
                            MeshaColors.Muted
                        },
                        fontSize = 11.sp,
                        fontWeight = FontWeight.W700,
                        modifier = Modifier
                            .clip(RoundedCornerShape(999.dp))
                            .background(
                                if (card.overdue) {
                                    if (isDeath) MeshaColors.DangerX else MeshaColors.WarnX
                                } else {
                                    MeshaColors.Surf3
                                },
                            )
                            .padding(horizontal = 8.dp, vertical = 2.dp),
                    )
                }
            }
        }
    }
}

private val IST = java.time.ZoneId.of("Asia/Kolkata")

private fun String.toUtcMillisOrNull(): Long? = runCatching {
    LocalDate.parse(this).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
}.getOrNull()

private fun Long.toIsoDate(): String =
    Instant.ofEpochMilli(this).atZone(ZoneOffset.UTC).toLocalDate().toString()
