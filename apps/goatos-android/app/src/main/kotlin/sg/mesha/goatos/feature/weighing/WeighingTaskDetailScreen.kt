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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

// telemetry:exempt Task detail is a read-only planner view; the reopen it exposes is tracked by
// the weighing reopen write, and capture/verification state changes are tracked on the surfaces
// that own those writes.

/**
 * ONE shed bucket on a task, as the planner reads it.
 *
 * Everything here is a fact the task payload actually carries. Weighing has no expected-animal
 * roster, so there is no animal denominator on this screen: [progress] is a position on the
 * bucket's own state ladder, never a fraction of animals.
 */
data class WeighingTaskShedUiRow(
    val campaignId: String,
    val campaignShedId: String,
    val tenantId: String,
    val locationId: String,
    val shedName: String,
    val category: String,
    /** Raw bucket status as the backend reports it. The screen owns how it is named and coloured. */
    val status: String,
    val captureSummary: String,
    val progress: Float,
    val canReopen: Boolean,
) {
    val isLumpSum: Boolean get() = category.equals("per_shed_partition", ignoreCase = true)
    val categoryLabel: String get() = if (isLumpSum) "Lump-sum" else "Individual"
}

/**
 * The buckets one operator owns on this task.
 *
 * Grouping is by operator identity; the header shows the operator's display NAME as the planner
 * catalog reports it. A user id is never rendered, and a name is never invented for an id the
 * catalog does not know.
 */
data class WeighingTaskOperatorGroupUiRow(
    val operatorUserId: String,
    val operatorLabel: String,
    val shedCount: Int,
    val sheds: List<WeighingTaskShedUiRow>,
    val moreShedCount: Int,
)

data class WeighingTaskDetailUiState(
    val campaignId: String = "",
    val found: Boolean = false,
    val parkName: String = "",
    val dateLabel: String = "",
    val status: String = "",
    val statusLabel: String = "",
    val bucketCount: Int = 0,
    val operatorCount: Int = 0,
    val isDraft: Boolean = false,
    val isClosed: Boolean = false,
    val groups: List<WeighingTaskOperatorGroupUiRow> = emptyList(),
    val loading: Boolean = false,
    val busy: Boolean = false,
)

/**
 * L1: one weighing task (one park on one weigh date), with its shed buckets grouped by the
 * operator who owns them.
 *
 * A hosted destination, so it carries Up/Back and no root chrome. Read-only apart from the
 * leadership reopen: a planner is assigned no shed, so this screen never offers a scan action --
 * "View shed" hands off to the existing capture destination, which enforces assignment itself.
 *
 * [onEditTask] and [onRepeatTask] are nullable on purpose: task authoring is a separate surface,
 * and until it is reachable the actions render disabled with the reason instead of pretending to
 * open something.
 */
@Composable
fun WeighingTaskDetailScreen(
    state: WeighingTaskDetailUiState,
    onRefresh: () -> Unit = {},
    onBack: () -> Unit = {},
    onOpenShed: (WeighingTaskShedUiRow) -> Unit = {},
    onReopenShed: (WeighingTaskShedUiRow) -> Unit = {},
    onEditTask: (() -> Unit)? = null,
    onRepeatTask: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        MeshaScreenHeader(
            title = state.parkName.ifBlank { "Task" },
            eyebrow = "WEIGHING",
            eyebrowColor = MeshaColors.BrandD,
            subtitle = state.dateLabel.takeIf { it.isNotBlank() },
            onBack = onBack,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = "Refresh this task",
                )
            },
        )
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (!state.found) {
                item(key = "task-missing") {
                    WeighingReadOnlyEmptyCard(
                        loading = state.loading,
                        title = "This task is no longer in the list",
                    )
                }
                return@LazyColumn
            }
            item(key = "task-summary") { TaskSummaryCard(state) }
            if (state.isDraft) {
                item(key = "task-draft-banner") {
                    TaskBanner(
                        text = "Not visible to operators yet",
                        fg = MeshaColors.Warn,
                        bg = MeshaColors.WarnX,
                    )
                }
            }
            if (state.isClosed) {
                item(key = "task-closed-banner") {
                    TaskBanner(text = "Closed", fg = MeshaColors.Ok, bg = MeshaColors.OkX)
                }
            }
            item(key = "task-edit") {
                TaskGhostAction(
                    label = "Edit sheds & assignment",
                    onClick = onEditTask,
                    disabledReason = "Changing the sheds or who is assigned on a task already created is not available yet",
                )
            }
            item(key = "task-repeat") {
                TaskGhostAction(
                    label = "Repeat this task on another date",
                    onClick = onRepeatTask,
                    disabledReason = "Starting a new task from this one is not available yet",
                )
            }
            state.groups.forEach { group ->
                item(key = "group-${group.operatorUserId}") { OperatorGroupHeader(group) }
                items(
                    count = group.sheds.size,
                    key = { index -> "shed-${group.sheds[index].campaignShedId}" },
                ) { index ->
                    val shed = group.sheds[index]
                    TaskShedCard(
                        row = shed,
                        busy = state.busy,
                        onOpen = { onOpenShed(shed) },
                        onReopen = { onReopenShed(shed) },
                    )
                }
                if (group.moreShedCount > 0) {
                    item(key = "group-more-${group.operatorUserId}") {
                        Text(
                            text = "${group.moreShedCount} more ${bucketNoun(group.moreShedCount)} with ${group.operatorLabel}",
                            color = MeshaColors.Muted,
                            style = MeshaType.cardSubtitle,
                            modifier = Modifier.padding(vertical = 4.dp),
                        )
                    }
                }
            }
            item(key = "task-tail-space") { Spacer(Modifier.height(24.dp)) }
        }
    }
}

@Composable
private fun TaskSummaryCard(state: WeighingTaskDetailUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            DetailPill(
                label = state.statusLabel,
                fg = if (state.isClosed) MeshaColors.Ok else MeshaColors.BrandD,
                bg = if (state.isClosed) MeshaColors.OkX else MeshaColors.Surf3,
            )
            Spacer(Modifier.weight(1f))
            DetailPill(label = state.parkName, fg = MeshaColors.Muted, bg = MeshaColors.Surf3)
        }
        Text(
            text = listOf(state.parkName, state.dateLabel).filter { it.isNotBlank() }.joinToString(" · "),
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = "${state.bucketCount} ${bucketNoun(state.bucketCount)} · " +
                "${state.operatorCount} ${operatorNoun(state.operatorCount)}",
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}

@Composable
private fun OperatorGroupHeader(group: WeighingTaskOperatorGroupUiRow) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = group.operatorLabel,
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = "${group.shedCount} ${shedNoun(group.shedCount)}",
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
        )
    }
}

@Composable
private fun TaskShedCard(
    row: WeighingTaskShedUiRow,
    busy: Boolean,
    onOpen: () -> Unit,
    onReopen: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .clickable(role = Role.Button, onClick = onOpen)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            DetailPill(
                label = shedStateLabel(row.status),
                fg = shedStateFg(row.status),
                bg = shedStateBg(row.status),
            )
            DetailPill(
                label = row.categoryLabel,
                fg = if (row.isLumpSum) MeshaColors.Purple else MeshaColors.BrandD,
                bg = if (row.isLumpSum) MeshaColors.PurpleX else MeshaColors.Surf3,
            )
        }
        Text(
            text = row.shedName,
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = row.captureSummary,
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
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
                        .background(if (row.isLumpSum) MeshaColors.Purple else MeshaColors.Ok),
                )
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "View shed →",
                color = MeshaColors.BrandD,
                style = MeshaType.cta,
                modifier = Modifier.weight(1f),
            )
            if (row.canReopen) {
                Text(
                    text = "Reopen",
                    color = if (busy) MeshaColors.Muted else MeshaColors.Warn,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W800,
                    modifier = Modifier
                        .minimumInteractiveComponentSize()
                        .clip(RoundedCornerShape(9.dp))
                        .background(MeshaColors.WarnX)
                        .clickable(enabled = !busy, role = Role.Button, onClick = onReopen)
                        .padding(horizontal = 10.dp, vertical = 6.dp),
                )
            }
        }
    }
}

@Composable
private fun TaskBanner(text: String, fg: Color, bg: Color) {
    Text(
        text = text,
        color = fg,
        style = MeshaType.cardSubtitle,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    )
}

/** A ghost action that stays visible but says WHY it cannot be used yet, instead of dead-ending. */
@Composable
private fun TaskGhostAction(label: String, onClick: (() -> Unit)?, disabledReason: String) {
    val enabled = onClick != null
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(24.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(24.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = { onClick?.invoke() })
            .padding(horizontal = 16.dp, vertical = 14.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.Ink else MeshaColors.Muted,
            style = MeshaType.cta,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (!enabled) {
            Text(
                text = disabledReason,
                color = MeshaColors.Faint,
                style = MeshaType.cardSubtitle,
            )
        }
    }
}

@Composable
private fun DetailPill(label: String, fg: Color, bg: Color) {
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

/**
 * A bucket's state, named the way the farm says it.
 *
 * Weighing has no separate "submitted" status: a bucket the operator submitted is `completed` and
 * is then waiting for a verifier, so that is what it is called here.
 */
internal fun shedStateLabel(status: String): String = when (status.trim().lowercase()) {
    "", "draft", "published" -> "not started"
    "in_progress" -> "in progress"
    "completed" -> "waiting for verifier"
    "closed" -> "accepted"
    else -> status.trim().lowercase().replace('_', ' ')
}

private fun shedStateFg(status: String): Color = when (status.trim().lowercase()) {
    "closed" -> MeshaColors.Ok
    "completed" -> MeshaColors.Warn
    "in_progress" -> MeshaColors.BrandD
    else -> MeshaColors.Muted
}

private fun shedStateBg(status: String): Color = when (status.trim().lowercase()) {
    "closed" -> MeshaColors.OkX
    "completed" -> MeshaColors.WarnX
    else -> MeshaColors.Surf3
}

private fun bucketNoun(count: Int): String = if (count == 1) "shed bucket" else "shed buckets"

private fun shedNoun(count: Int): String = if (count == 1) "shed" else "sheds"

private fun operatorNoun(count: Int): String = if (count == 1) "operator" else "operators"
