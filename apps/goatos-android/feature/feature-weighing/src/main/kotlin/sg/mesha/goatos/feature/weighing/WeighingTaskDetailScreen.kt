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
 * How many rungs a shed bucket's state ladder has: capture started, submitted, accepted.
 *
 * The ladder is a POSITION, never a share. Weighing is free-flow — migration 000079 dropped the
 * expected-animal roster and `expected_animal_count` is a fixed bucket-grain 1 — so any fraction
 * rendered for a bucket would be a share of a total that does not exist. The rungs are drawn as
 * discrete segments for exactly that reason: a part-filled continuous bar was read on the farm as
 * "70% of the animals are done".
 */
const val WEIGHING_BUCKET_LADDER_STEPS = 3

/**
 * ONE operator's filter chip on a task, as FACTS rather than as a finished sentence.
 *
 * The name and the count travel separately so the screen can say what the number MEANS. Rendering
 * them pre-joined produced "Dinakar 2", which reads as part of a person's name rather than as the
 * two sheds he owns: a number with no unit is the same defect as a share with no denominator.
 */
data class WeighingOperatorFilterUiRow(
    val id: String,
    /** The operator's display name. Never a user id. */
    val name: String,
    /** How many shed buckets on this task this operator owns. Buckets, never animals. */
    val shedCount: Int,
    val selected: Boolean,
)

/**
 * ONE shed bucket on a task, as the planner reads it.
 *
 * Everything here is a fact the task payload actually carries. Weighing has no expected-animal
 * roster, so there is no animal denominator on this screen: [ladderStep] is a position on the
 * bucket's own state ladder and [capturedCount] is a plain backend count reported as-is. Neither
 * is ever divided by anything, and this row deliberately carries no fractional field at all.
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
    /** True when a verifier bounced this bucket's evidence back to the operator. */
    val reworked: Boolean,
    /**
     * Backend-owned count of the weight records this bucket ACTUALLY holds. Reported as a plain
     * count ("5 weighed") and never turned into a percentage.
     */
    val capturedCount: Int,
    /** Rung on the bucket's state ladder, 0..[WEIGHING_BUCKET_LADDER_STEPS]. Not a fraction. */
    val ladderStep: Int,
    val canReopen: Boolean,
) {
    val isLumpSum: Boolean get() = category.equals("per_shed_partition", ignoreCase = true)
    val categoryLabel: String get() = if (isLumpSum) "Lump-sum" else "Individual"
}

/**
 * One operator chip on this task: WHO, and HOW MANY shed buckets are theirs.
 *
 * The count is carried as a NUMBER, never pre-baked into the label. A bare "Dinakar 2" cannot say
 * whether the 2 is sheds or animals, and this screen shows both units — the chip counts sheds while
 * every card below it talks about animals captured. Keeping the number separate lets the screen name
 * the unit it is counting, in the reader's own language.
 */
data class WeighingTaskOperatorFilterUiRow(
    /** Stable filter key: the operator's user id, or "unassigned" for the no-operator bucket. */
    val id: String,
    /** The operator's display NAME as the backend resolved it. A user id is never rendered. */
    val operatorLabel: String,
    /** Shed buckets this operator owns across the WHOLE task, never just the loaded page. */
    val shedCount: Int,
    val selected: Boolean,
)

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
    /**
     * Shed buckets nobody has submitted yet — not started, part-captured, or bounced back to the
     * operator. This is work the FARM still owes, and it is the number the close button names.
     * Buckets, never animals.
     */
    val openBucketCount: Int = 0,
    /**
     * Shed buckets the operator HAS submitted that no verifier has accepted yet. Unaccepted work
     * the task is still carrying, but not open work — it sits in the verifier's queue, which is
     * exactly what each of those cards says. Counted and named separately so the close button can
     * never read "4 still open" beside two cards that say "waiting for verifier".
     */
    val awaitingVerificationBucketCount: Int = 0,
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
    val operatorFilters: List<WeighingOperatorFilterUiRow> = emptyList(),
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
) {
    /** Every bucket a verifier has not accepted yet, whichever queue it is sitting in. */
    val unacceptedBucketCount: Int get() = openBucketCount + awaitingVerificationBucketCount
}

/**
 * The close button's label, naming the two kinds of unaccepted work separately.
 *
 * One screen must never carry two answers to "how much is left": the numbers here are the same
 * buckets the cards below render, described the same way.
 */
@Composable
private fun closeTaskLabel(state: WeighingTaskDetailUiState): String = when {
    state.openBucketCount > 0 && state.awaitingVerificationBucketCount > 0 -> stringResource(
        R.string.weighing_task_close_split_fmt,
        state.openBucketCount,
        state.awaitingVerificationBucketCount,
    )
    state.awaitingVerificationBucketCount > 0 -> stringResource(
        R.string.weighing_task_close_awaiting_fmt,
        state.awaitingVerificationBucketCount,
    )
    else -> stringResource(R.string.weighing_task_close_open_fmt, state.openBucketCount)
}

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
            body = if (state.unacceptedBucketCount == 1) {
                stringResource(
                    R.string.weighing_task_close_confirm_body_one,
                    bucketNoun(state.unacceptedBucketCount),
                )
            } else {
                stringResource(
                    R.string.weighing_task_close_confirm_body_other,
                    bucketNoun(state.unacceptedBucketCount),
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
                    // Ending is warned about whenever ANY bucket is unaccepted, but the label
                    // names the two kinds separately: the same fact must read the same as the
                    // cards below it.
                    val hasUnacceptedWork = state.unacceptedBucketCount > 0
                    TaskPrimaryAction(
                        label = when {
                            state.busy -> stringResource(R.string.weighing_task_ending)
                            hasUnacceptedWork -> closeTaskLabel(state)
                            else -> stringResource(R.string.weighing_task_mark_complete)
                        },
                        tone = if (hasUnacceptedWork) MeshaColors.Warn else MeshaColors.Ok,
                        enabled = !state.busy,
                        onClick = { if (hasUnacceptedWork) confirmEndOpen = true else onEndTask() },
                    )
                }
            }
            // Operator is a FILTER, not a section. One flat list pages uniformly however many
            // operators the task has; a grouped list cannot, because a keyset page splits mid-group.
            //
            // The chip names its unit. It counts SHED BUCKETS while the cards below it talk about
            // animals captured, so an unlabelled "Dinakar 2" would put two different units next to
            // each other with nothing to tell them apart.
            items(
                count = state.sheds.size,
                key = { index -> "shed-${state.sheds[index].campaignShedId}" },
            ) { index ->
                val shed = state.sheds[index]
                LaunchedEffect(index, state.sheds.size) { onBucketRowVisible(index) }
                TaskShedCard(row = shed, onOpen = { onOpenShed(shed) })
            }
            // Secondary actions sit BELOW the work. Each of these is a disabled-with-reason ghost
            // about some OTHER date or some other version of this task; above the list they asked
            // the reader to consider repeating a task before they had seen what is in it.
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
                        // The count is spelled out with its UNIT here, at render time, because
                        // that is where the words live: "Dinakar · 2 sheds", never "Dinakar 2".
                        options = state.operatorFilters.map { row ->
                            WeighingFilterChipUiRow(
                                id = row.id,
                                label = "${row.name} · ${shedNoun(row.shedCount)}",
                                selected = row.selected,
                            )
                        },
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
        // WHO this shed belongs to, on the card itself. The assignment is the reason the task
        // exists, so a planner who just split four sheds between two people must be able to read
        // the split off the list -- not recover it by tapping a chip and watching the list shrink.
        if (row.operatorLabel.isNotBlank()) {
            Text(
                text = stringResource(R.string.weighing_task_shed_operator_fmt, row.operatorLabel),
                color = MeshaColors.BrandD,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Text(
            text = captureSummary(row),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        BucketStateLadder(step = row.ladderStep, lumpSum = row.isLumpSum)
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

/**
 * WHERE a bucket stands, in the farm's own words, plus what it actually holds.
 *
 * The count is the backend's plain tally of weight records and is reported as-is: weighing is
 * free-flow, so there is no expected-animal total to take a share of. A lump-sum bucket holds ONE
 * whole-shed weight rather than a per-animal tally, so a count is deliberately omitted there — "1
 * weighed" would misname a whole shed as a single animal.
 */
@Composable
private fun captureSummary(row: WeighingTaskShedUiRow): String {
    val state = when {
        row.reworked -> stringResource(R.string.weighing_bucket_state_rework)
        row.status.trim().lowercase() == "closed" -> stringResource(R.string.weighing_bucket_state_accepted)
        row.status.trim().lowercase() == "completed" -> stringResource(R.string.weighing_bucket_state_submitted)
        row.status.trim().lowercase() == "in_progress" -> stringResource(R.string.weighing_bucket_state_in_progress)
        else -> stringResource(R.string.weighing_bucket_state_not_started)
    }
    if (row.isLumpSum || row.capturedCount <= 0) return state
    val weighed = stringResource(R.string.weighing_bucket_weighed_fmt, row.capturedCount)
    return "$weighed · $state"
}

/**
 * A bucket's position on its state ladder, drawn as DISCRETE rungs.
 *
 * Deliberately not a continuous bar: weighing has no expected-animal denominator, and a
 * part-filled continuous bar was read on the farm as a percentage of animals done. Separate
 * segments read as "step 2 of 3", which is the only thing this can honestly say.
 */
@Composable
private fun BucketStateLadder(step: Int, lumpSum: Boolean) {
    val reached = step.coerceIn(0, WEIGHING_BUCKET_LADDER_STEPS)
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        repeat(WEIGHING_BUCKET_LADDER_STEPS) { index ->
            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(7.dp)
                    .clip(RoundedCornerShape(99.dp))
                    .background(
                        when {
                            index >= reached -> MeshaColors.Bg
                            lumpSum -> MeshaColors.Purple
                            else -> MeshaColors.Ok
                        },
                    ),
            )
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
