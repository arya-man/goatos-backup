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
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
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
    /**
     * The assigned operator's display NAME as the planner catalog reports it, carried on the row so
     * the shed drill can show it. Blank when nobody is assigned yet; a user id is never rendered.
     */
    val operatorLabel: String,
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
    val isCompleted: Boolean = false,
    /**
     * WHY the task finished, as the backend recorded it. Carried on the state rather than left in
     * a snackbar, so a later visit still says whether the task finished clean or was closed over
     * open work.
     */
    val closedReason: String = "",
    /** Shed buckets whose work has NOT been accepted yet. Buckets, never animals. */
    val openBucketCount: Int = 0,
    /** True only while the task is still a draft that nobody can see. */
    val canPublish: Boolean = false,
    /** True while the task is live and can still be ended. */
    val canEnd: Boolean = false,
    /**
     * True when this task can actually be staged into the authoring wizard. False means the row
     * carries no park or no placeable bucket, so the action renders disabled WITH [repeatBlockedReason]
     * instead of looking live and doing nothing.
     */
    val canRepeat: Boolean = false,
    val repeatBlockedReason: String = "",
    /**
     * Operator chips. Filtering, NOT sectioning: a grouped list cannot paginate, because a ~20-row
     * keyset page splits mid-operator and leaves a header showing part of someone's sheds with the
     * rest arriving pages later. Counts come from the whole task, never from the loaded page.
     */
    val operatorFilters: List<WeighingFilterChipUiRow> = emptyList(),
    /** Selected operator id, or null for every operator. */
    val selectedOperatorId: String? = null,
    /** One flat, uniformly paginated list of buckets, already narrowed by the chip. */
    val sheds: List<WeighingTaskShedUiRow> = emptyList(),
    /**
     * Quiet staleness note: the last refresh did not land, so the cached task is what is on screen.
     * Blank when the cache is current. It never replaces the task with an error page.
     */
    val staleNotice: String = "",
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
    onSelectOperator: (String?) -> Unit = {},
    onRefresh: () -> Unit = {},
    onBack: () -> Unit = {},
    onOpenShed: (WeighingTaskShedUiRow) -> Unit = {},
    onEditTask: (() -> Unit)? = null,
    onRepeatTask: (() -> Unit)? = null,
    onPublishTask: () -> Unit = {},
    onEndTask: () -> Unit = {},
    /** Scroll-driven prefetch for the bucket list. Index runs across the whole rendered list. */
    onBucketRowVisible: (Int) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    // The confirm is asked for ONLY when ending the task would leave real work unaccepted. Ending
    // a task whose buckets were all accepted has nothing to warn about.
    var confirmEndOpen by remember { mutableStateOf(false) }
    if (confirmEndOpen) {
        WeighingTaskConfirmSheet(
            title = stringResource(R.string.weighing_task_close_confirm_title),
            body = if (state.openBucketCount == 1) {
                stringResource(
                    R.string.weighing_task_close_confirm_body_one,
                    bucketNoun(state.openBucketCount),
                )
            } else {
                stringResource(
                    R.string.weighing_task_close_confirm_body_other,
                    bucketNoun(state.openBucketCount),
                )
            },
            confirmLabel = stringResource(R.string.weighing_task_close_confirm_label),
            busy = state.busy,
            onConfirm = {
                confirmEndOpen = false
                onEndTask()
            },
            onDismiss = { confirmEndOpen = false },
        )
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        MeshaScreenHeader(
            title = state.parkName.ifBlank { stringResource(R.string.weighing_task_fallback_title) },
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            subtitle = state.dateLabel.takeIf { it.isNotBlank() },
            onBack = onBack,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_task_refresh),
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
                        title = stringResource(R.string.weighing_task_missing_title),
                    )
                }
                return@LazyColumn
            }
            item(key = "task-summary") { TaskSummaryCard(state) }
            if (state.staleNotice.isNotBlank()) {
                item(key = "task-stale") {
                    TaskBanner(text = state.staleNotice, fg = MeshaColors.Warn, bg = MeshaColors.WarnX)
                }
            }
            if (state.isDraft) {
                item(key = "task-draft-banner") {
                    TaskBanner(
                        text = stringResource(R.string.weighing_task_draft_banner),
                        fg = MeshaColors.Warn,
                        bg = MeshaColors.WarnX,
                    )
                }
            }
            if (state.isClosed) {
                item(key = "task-closed-banner") {
                    TaskBanner(
                        text = listOf(stringResource(R.string.weighing_task_closed), state.closedReason)
                            .filter { it.isNotBlank() }
                            .joinToString(" \u00b7 "),
                        fg = MeshaColors.Ok,
                        bg = MeshaColors.OkX,
                    )
                }
            }
            if (state.canPublish) {
                item(key = "task-publish") {
                    TaskPrimaryAction(
                        label = if (state.busy) {
                            stringResource(R.string.weighing_task_publishing)
                        } else {
                            stringResource(R.string.weighing_task_publish)
                        },
                        tone = MeshaColors.BrandD,
                        enabled = !state.busy,
                        onClick = onPublishTask,
                    )
                }
            }
            if (state.canEnd) {
                item(key = "task-end") {
                    val hasOpenWork = state.openBucketCount > 0
                    TaskPrimaryAction(
                        label = when {
                            state.busy -> stringResource(R.string.weighing_task_ending)
                            hasOpenWork -> stringResource(
                                R.string.weighing_task_close_open_fmt,
                                state.openBucketCount,
                            )
                            else -> stringResource(R.string.weighing_task_mark_complete)
                        },
                        tone = if (hasOpenWork) MeshaColors.Warn else MeshaColors.Ok,
                        enabled = !state.busy,
                        onClick = { if (hasOpenWork) confirmEndOpen = true else onEndTask() },
                    )
                }
            }
            if (state.isClosed || state.isCompleted) {
                item(key = "task-reopen") {
                    TaskGhostAction(
                        label = stringResource(R.string.weighing_task_reopen),
                        onClick = null,
                        disabledReason = stringResource(R.string.weighing_task_reopen_blocked),
                    )
                }
            }
            item(key = "task-edit") {
                TaskGhostAction(
                    label = stringResource(R.string.weighing_task_edit),
                    onClick = onEditTask,
                    disabledReason = stringResource(R.string.weighing_task_edit_blocked),
                )
            }
            item(key = "task-repeat") {
                TaskGhostAction(
                    label = stringResource(R.string.weighing_task_repeat),
                    onClick = onRepeatTask.takeIf { state.canRepeat },
                    disabledReason = state.repeatBlockedReason.ifBlank {
                        stringResource(R.string.weighing_task_repeat_blocked)
                    },
                )
            }
            // Operator is a FILTER, not a section. One flat list pages uniformly however many
            // operators the task has; a grouped list cannot, because a keyset page splits mid-group.
            if (state.operatorFilters.size > 1) {
                item(key = "operator-filters") {
                    WeighingFilterChips(
                        options = state.operatorFilters,
                        allLabel = stringResource(R.string.weighing_task_all_operators),
                        onSelect = onSelectOperator,
                    )
                }
            }
            items(
                count = state.sheds.size,
                key = { index -> "shed-${state.sheds[index].campaignShedId}" },
            ) { index ->
                val shed = state.sheds[index]
                LaunchedEffect(index, state.sheds.size) { onBucketRowVisible(index) }
                TaskShedCard(row = shed, onOpen = { onOpenShed(shed) })
            }
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
            text = stringResource(
                R.string.weighing_task_summary_fmt,
                bucketNoun(state.bucketCount),
                operatorNoun(state.operatorCount),
            ),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}

@Composable
private fun TaskShedCard(
    row: WeighingTaskShedUiRow,
    onOpen: () -> Unit,
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
                label = taskCategoryLabel(row.isLumpSum),
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
        // ONE affordance per bucket card. Reopen lives on the shed itself, behind its own
        // confirm, so a single stray tap on a card can never hand a bucket back to an operator.
        Text(
            text = stringResource(R.string.weighing_task_view_shed),
            color = MeshaColors.BrandD,
            style = MeshaType.cta,
            modifier = Modifier.fillMaxWidth(),
        )
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

/** A task-level action that is actually backed by a write. Never rendered when it does not apply. */
@Composable
internal fun TaskPrimaryAction(
    label: String,
    tone: Color,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Text(
        text = label,
        color = if (enabled) tone else MeshaColors.Muted,
        style = MeshaType.cta,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        textAlign = TextAlign.Center,
        modifier = Modifier
            .fillMaxWidth()
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(24.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, if (enabled) tone else MeshaColors.Hair, RoundedCornerShape(24.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 14.dp),
    )
}

/**
 * The confirm the mock asks for before an action loses something.
 *
 * It states the consequence in the farm's own terms and names the count it is about, so a planner
 * is never asked to confirm a number they cannot see.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun WeighingTaskConfirmSheet(
    title: String,
    body: String,
    confirmLabel: String,
    busy: Boolean,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    dismissLabel: String = stringResource(R.string.weighing_task_close_dismiss_label),
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MeshaColors.Surf,
        contentColor = MeshaColors.Ink,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 20.dp)
                .padding(bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(text = title, color = MeshaColors.Ink, style = MeshaType.cardTitle)
            Text(text = body, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            TaskPrimaryAction(
                label = confirmLabel,
                tone = MeshaColors.Warn,
                enabled = !busy,
                onClick = onConfirm,
            )
            TaskPrimaryAction(
                label = dismissLabel,
                tone = MeshaColors.Muted,
                enabled = !busy,
                onClick = onDismiss,
            )
        }
    }
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
fun shedStateLabel(status: String): String = when (status.trim().lowercase()) {
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

@Composable
private fun bucketNoun(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_bucket_one, count)
} else {
    stringResource(R.string.weighing_bucket_other, count)
}

@Composable
private fun shedNoun(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_shed_one, count)
} else {
    stringResource(R.string.weighing_shed_other, count)
}

@Composable
private fun operatorNoun(count: Int): String = if (count == 1) {
    stringResource(R.string.weighing_operator_one, count)
} else {
    stringResource(R.string.weighing_operator_other, count)
}

/** Bucket mode, named the way the farm says it. */
@Composable
private fun taskCategoryLabel(isLumpSum: Boolean): String = if (isLumpSum) {
    stringResource(R.string.weighing_category_lump_sum_title)
} else {
    stringResource(R.string.weighing_category_individual_title)
}
