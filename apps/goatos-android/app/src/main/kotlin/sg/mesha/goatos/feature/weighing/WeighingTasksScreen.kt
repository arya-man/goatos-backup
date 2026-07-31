package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

// telemetry:exempt Planner task list is read-only; weighing state changes are tracked on the
// execution and verification surfaces that own those writes.

/** The two task tabs. The split itself is server-owned; this only names the selected view. */
enum class WeighingTasksTab { ACTIVE, COMPLETED }

/**
 * ONE weighing task: one park on one weigh date, with its shed buckets summarised.
 *
 * Every count here counts BUCKETS that exist, never animals: weighing is free-flow and has no
 * expected-animal roster, so an animal-grain denominator would be invented.
 */
data class WeighingTaskUiRow(
    val campaignId: String,
    val parkId: String,
    val parkName: String,
    val status: String,
    val dateLabel: String,
    val monthLabel: String,
    val bucketCount: Int,
    val shedNames: List<String>,
    val moreShedCount: Int,
    val individualCount: Int,
    val lumpSumCount: Int,
    val operatorCount: Int,
    val toVerifyCount: Int,
    val reworkCount: Int,
    val acceptedCount: Int,
) {
    val progress: Float
        get() = if (bucketCount <= 0) 0f else acceptedCount.toFloat() / bucketCount.toFloat()
}

data class WeighingTasksUiState(
    val tab: WeighingTasksTab = WeighingTasksTab.ACTIVE,
    /** Whole-filter tallies from the backend. Never counted from [tasks]. */
    val activeCount: Int = 0,
    val completedCount: Int = 0,
    val tasks: List<WeighingTaskUiRow> = emptyList(),
    val repeatCandidate: WeighingTaskUiRow? = null,
    val parkFilters: List<WeighingParkFilterUiRow> = emptyList(),
    val todayLabel: String = "",
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
)

/**
 * The planner's task list: ONE card per task (one park on one weigh date), not one per shed.
 *
 * Read-only by design and with no scan action: a planner is assigned no sheds, and the weighing
 * write requires the caller to be the bucket's assignee.
 */
@Composable
fun WeighingTasksScreen(
    state: WeighingTasksUiState,
    onRefresh: () -> Unit = {},
    onSelectTab: (WeighingTasksTab) -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    onOpenTask: (String) -> Unit = {},
    /**
     * Null while starting a task from a previous one is not wired. The row then renders visibly
     * disabled with its reason instead of silently swallowing the tap.
     */
    onRepeatTask: ((String) -> Unit)? = null,
    onNewTask: () -> Unit = {},
    onTaskRowVisible: (Int) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = "Tasks",
                eyebrow = "WEIGHING",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = state.todayLabel.takeIf { it.isNotBlank() },
                actions = {
                    SyncIconButton(
                        isSyncing = state.loading,
                        onSync = onRefresh,
                        contentDescription = "Refresh weighing tasks",
                    )
                },
            )
            Row(
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                WeighingFilterPill(
                    label = "Active ${state.activeCount}",
                    selected = state.tab == WeighingTasksTab.ACTIVE,
                    onClick = { onSelectTab(WeighingTasksTab.ACTIVE) },
                )
                WeighingFilterPill(
                    label = "Completed ${state.completedCount}",
                    selected = state.tab == WeighingTasksTab.COMPLETED,
                    onClick = { onSelectTab(WeighingTasksTab.COMPLETED) },
                )
            }
            // Park chips narrow the COMPLETED history, matching the product model: the active tab
            // is the short live list a planner reads whole.
            if (state.tab == WeighingTasksTab.COMPLETED && state.parkFilters.isNotEmpty()) {
                Box(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    WeighingParkFilters(filters = state.parkFilters, onSelect = onSelectPark)
                }
            }
            LazyColumn(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(horizontal = 16.dp, vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                val repeat = state.repeatCandidate
                if (state.tab == WeighingTasksTab.ACTIVE && repeat != null) {
                    item(key = "repeat-last") {
                        RepeatLastTaskButton(
                            row = repeat,
                            onClick = onRepeatTask?.let { handler -> { handler(repeat.campaignId) } },
                        )
                    }
                }
                if (state.tasks.isEmpty()) {
                    item(key = "tasks-empty") {
                        WeighingReadOnlyEmptyCard(
                            loading = state.loading,
                            title = if (state.tab == WeighingTasksTab.COMPLETED) {
                                "No completed tasks yet"
                            } else {
                                "No active tasks"
                            },
                        )
                    }
                } else {
                    itemsIndexed(state.tasks, key = { _, task -> task.campaignId }) { index, task ->
                        LaunchedEffect(task.campaignId, index, state.tasks.size) {
                            onTaskRowVisible(index)
                        }
                        val previousMonth = state.tasks.getOrNull(index - 1)?.monthLabel
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            if (state.tab == WeighingTasksTab.COMPLETED &&
                                task.monthLabel.isNotBlank() &&
                                task.monthLabel != previousMonth
                            ) {
                                Text(
                                    text = task.monthLabel,
                                    color = MeshaColors.Muted,
                                    style = MeshaType.sectionLabel,
                                )
                            }
                            WeighingTaskCard(row = task, onOpen = { onOpenTask(task.campaignId) })
                        }
                    }
                    if (state.loadingMore) {
                        item(key = "tasks-loading-more") { ListLoadingFooter() }
                    }
                }
                item(key = "tasks-tail-space") { Spacer(Modifier.height(72.dp)) }
            }
        }
        NewTaskAction(
            onClick = onNewTask,
            modifier = Modifier
                .align(Alignment.BottomEnd)
                .padding(16.dp),
        )
    }
}

@Composable
private fun WeighingTaskCard(row: WeighingTaskUiRow, onOpen: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .clickable(onClick = onOpen)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TaskPill(
                label = taskStatusLabel(row.status),
                fg = taskStatusFg(row.status),
                bg = taskStatusBg(row.status),
            )
            if (row.toVerifyCount > 0) {
                TaskPill(label = "${row.toVerifyCount} to verify", fg = MeshaColors.Warn, bg = MeshaColors.WarnX)
            }
            if (row.reworkCount > 0) {
                TaskPill(
                    label = "${row.reworkCount} back with operator",
                    fg = MeshaColors.Danger,
                    bg = MeshaColors.DangerX,
                )
            }
            Spacer(Modifier.weight(1f))
            TaskPill(label = row.dateLabel, fg = MeshaColors.Muted, bg = MeshaColors.Surf3)
        }
        Text(
            text = "${row.parkName} · ${row.bucketCount} ${bucketWord(row.bucketCount)}",
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (row.shedNames.isNotEmpty()) {
            Text(
                text = row.shedNames.joinToString(" · ") +
                    if (row.moreShedCount > 0) " +${row.moreShedCount} more" else "",
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (row.individualCount > 0) {
                TaskPill(label = "${row.individualCount} individual", fg = MeshaColors.BrandD, bg = MeshaColors.Surf3)
            }
            if (row.lumpSumCount > 0) {
                TaskPill(label = "${row.lumpSumCount} lump-sum", fg = MeshaColors.Purple, bg = MeshaColors.PurpleX)
            }
            if (row.operatorCount > 0) {
                TaskPill(
                    label = "${row.operatorCount} ${if (row.operatorCount == 1) "operator" else "operators"}",
                    fg = MeshaColors.Muted,
                    bg = MeshaColors.Surf3,
                )
            }
        }
        // The denominator is the planned BUCKETS on this task, a set we actually know.
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(7.dp)
                .clip(RoundedCornerShape(99.dp))
                .background(MeshaColors.Bg),
        ) {
            if (row.progress > 0f) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth(row.progress)
                        .height(7.dp)
                        .clip(RoundedCornerShape(99.dp))
                        .background(MeshaColors.Ok),
                )
            }
        }
        Text(
            text = "${row.acceptedCount} of ${row.bucketCount} accepted →",
            color = MeshaColors.BrandD,
            style = MeshaType.cta,
        )
    }
}

@Composable
private fun RepeatLastTaskButton(row: WeighingTaskUiRow, onClick: (() -> Unit)?) {
    val enabled = onClick != null
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(24.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(24.dp))
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(
            text = "Repeat last task · ${row.parkName} · ${row.bucketCount} ${bucketWord(row.bucketCount)}",
            color = if (enabled) MeshaColors.Ink else MeshaColors.Muted,
            style = MeshaType.cta,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (!enabled) {
            Text(
                text = "Starting a task from an earlier one is not available yet",
                color = MeshaColors.Muted,
                style = MeshaType.caption,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun NewTaskAction(onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .height(48.dp)
            .clip(RoundedCornerShape(24.dp))
            .background(MeshaColors.Brand)
            .clickable(onClick = onClick)
            .padding(horizontal = 20.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = "+ New task",
            color = MeshaColors.PageBg,
            fontSize = 13.sp,
            fontWeight = FontWeight.W800,
        )
    }
}

@Composable
private fun TaskPill(label: String, fg: Color, bg: Color) {
    Text(
        text = label,
        color = fg,
        fontSize = 12.sp,
        fontWeight = FontWeight.W800,
        maxLines = 1,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}

private fun bucketWord(count: Int): String = if (count == 1) "shed bucket" else "shed buckets"

private fun taskStatusLabel(status: String): String = when (val normalized = status.trim().lowercase()) {
    "" -> "scheduled"
    else -> normalized.replace('_', ' ')
}

private fun taskStatusFg(status: String): Color = when (status.trim().lowercase()) {
    "completed", "closed" -> MeshaColors.Ok
    "published", "in_progress" -> MeshaColors.BrandD
    "delayed" -> MeshaColors.Warn
    else -> MeshaColors.Muted
}

private fun taskStatusBg(status: String): Color = when (status.trim().lowercase()) {
    "completed", "closed" -> MeshaColors.OkX
    "delayed" -> MeshaColors.WarnX
    else -> MeshaColors.Surf3
}
