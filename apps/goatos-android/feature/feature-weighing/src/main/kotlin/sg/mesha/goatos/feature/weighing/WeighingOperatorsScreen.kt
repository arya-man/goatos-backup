package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

// telemetry:exempt Oversight renders backend read-model state; the close/reopen/abandon writes it
// offers are instrumented by the weighing service that owns them.

// Which transition the confirmation dialog is for. Plain strings because they go through
// rememberSaveable, which stores Bundle-native types without a custom Saver.
private const val OVERSIGHT_ACTION_CLOSE = "close"
private const val OVERSIGHT_ACTION_REOPEN = "reopen"
private const val OVERSIGHT_ACTION_ABANDON = "abandon"

/**
* Oversight of weighing work assigned to SOMEONE ELSE, grouped BY PERSON.
 *
 * A SEPARATE destination from the work list and the planner list, not a mode of one shared screen.
 * It never offers a SCAN action -- capture stays with the shed's assignee, so nothing here can
 * widen what the viewer may record. It DOES offer close / reopen / abandon, but only when the
 * backend's own capability flags say this viewer holds the monitor authority: this is the Growth
 * Director's leadership surface, and leaving it purely read-only left them with no reachable way
 * to end or reopen the work they oversee. (Superseding note: an earlier revision of this doc
 * described the screen as strictly read-only. That was true before the capability-gated
 * transitions landed; the gate, not the absence of controls, is what keeps it safe.)
 *
 * WHAT IT SHOWS AND WHY. A screen called "Operators" used to render a flat list of SHEDS with no
 * name anywhere on it, so the one question it exists to answer — who did what — had no answer on
 * screen. People now come FIRST, each with the backend's own tallies; the shed list stays below as
 * the detail behind those tallies, and every shed row now names its assignee.
 *
 * NO INVENTED DENOMINATOR. Free-flow weighing has no expected-animal roster, so nothing here is a
 * percentage, a fraction, or a part-filled bar of animals. The person-grain numbers are plain
 * counts from the backend ([WeighingUiState.operatorSummaries]); the only bar drawn is a strip of
 * DISCRETE segments, one per shed that person actually holds, coloured by that shed's state — a
 * real total of a real set, never a share of one that does not exist.
 */
@Composable
fun WeighingOperatorsScreen(
    state: WeighingUiState,
    onRefresh: () -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    onReopenAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onCloseAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onAbandonAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onAssignmentRowVisible: (Int) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    // The transition awaiting confirmation, held as the BUCKET ID rather than the row object so it
    // survives process death; the row itself is re-read from the backend anyway. The typed reason
    // is the one thing here that cannot be recovered from the backend, so it is saved too.
    var pendingAction by rememberSaveable { mutableStateOf<String?>(null) }
    var pendingShedId by rememberSaveable { mutableStateOf<String?>(null) }
    var pendingReason by rememberSaveable { mutableStateOf("") }
    fun dismissPending() {
        pendingAction = null
        pendingShedId = null
        pendingReason = ""
    }
    val pendingRow = pendingShedId?.let { id -> state.assignments.firstOrNull { it.campaignShedId == id } }
    if (pendingRow != null) {
        when (pendingAction) {
            OVERSIGHT_ACTION_ABANDON -> WeighingReasonDialog(
                title = stringResource(R.string.weighing_abandon_dialog_title, pendingRow.label),
                subtitle = stringResource(R.string.weighing_abandon_dialog_subtitle),
                placeholder = stringResource(R.string.weighing_abandon_dialog_placeholder),
                confirmLabel = stringResource(R.string.weighing_leadership_abandon),
                confirmColor = MeshaColors.Danger,
                reason = pendingReason,
                onReasonChange = { pendingReason = it },
                onConfirm = { reason ->
                    dismissPending()
                    onAbandonAssignment(pendingRow, reason)
                },
                onDismiss = ::dismissPending,
            )
            OVERSIGHT_ACTION_CLOSE -> WeighingReasonDialog(
                title = stringResource(R.string.weighing_close_dialog_title, pendingRow.label),
                subtitle = stringResource(R.string.weighing_close_dialog_subtitle),
                placeholder = stringResource(R.string.weighing_close_dialog_placeholder),
                confirmLabel = stringResource(R.string.weighing_leadership_close),
                confirmColor = MeshaColors.BrandD,
                reason = pendingReason,
                onReasonChange = { pendingReason = it },
                onConfirm = { reason ->
                    dismissPending()
                    onCloseAssignment(pendingRow, reason)
                },
                onDismiss = ::dismissPending,
            )
            OVERSIGHT_ACTION_REOPEN -> WeighingReasonDialog(
                title = stringResource(R.string.weighing_reopen_dialog_title, pendingRow.label),
                subtitle = stringResource(R.string.weighing_reopen_dialog_subtitle),
                placeholder = stringResource(R.string.weighing_reopen_dialog_placeholder),
                confirmLabel = stringResource(R.string.weighing_reopen_dialog_confirm),
                confirmColor = MeshaColors.BrandD,
                reason = pendingReason,
                onReasonChange = { pendingReason = it },
                onConfirm = { reason ->
                    dismissPending()
                    onReopenAssignment(pendingRow, reason)
                },
                onDismiss = ::dismissPending,
            )
            else -> Unit
        }
    }
    Column(modifier = modifier.fillMaxSize()) {
        MeshaScreenHeader(
            title = stringResource(R.string.weighing_operators_title),
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_operators_refresh),
                )
            },
        )
        if (state.parkFilters.size > 1) {
            Box(modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                WeighingParkFilters(filters = state.parkFilters, onSelect = onSelectPark)
            }
        }
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(MeshaColors.PageBg)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // PEOPLE FIRST. These rows are whole-filter backend truth, so they are correct even
            // before the paged shed list below has loaded past its first page.
            item(key = "operators-people-heading") {
                SectionHeading(stringResource(R.string.weighing_operators_people_heading))
            }
            if (state.operatorSummaries.isEmpty()) {
                item(key = "operators-people-empty") {
                    WeighingReadOnlyEmptyCard(
                        loading = state.loading,
                        title = stringResource(R.string.weighing_operators_no_people_title),
                    )
                }
            } else {
                items(
                    state.operatorSummaries,
                    key = { row -> row.operatorUserId.ifBlank { "unassigned" } },
                ) { row -> WeighingOperatorCard(row) }
            }

            item(key = "operators-sheds-heading") {
                SectionHeading(stringResource(R.string.weighing_operators_sheds_heading))
            }
            if (state.assignments.isEmpty()) {
                item(key = "operators-sheds-empty") {
                    WeighingReadOnlyEmptyCard(
                        loading = state.loading,
                        title = stringResource(R.string.weighing_operators_empty_title),
                    )
                }
            } else {
                itemsIndexed(state.assignments, key = { _, assignment -> assignment.campaignShedId }) { index, assignment ->
                    LaunchedEffect(assignment.campaignShedId, index, state.assignments.size) {
                        onAssignmentRowVisible(index)
                    }
                    if (state.canEndWeighing || state.canReopenWeighing) {
                        WeighingOversightCard(
                            row = assignment,
                            canEnd = state.canEndWeighing,
                            canReopen = state.canReopenWeighing,
                            onReopen = {
                                pendingAction = OVERSIGHT_ACTION_REOPEN
                                pendingShedId = assignment.campaignShedId
                                pendingReason = ""
                            },
                            onClose = {
                                pendingAction = OVERSIGHT_ACTION_CLOSE
                                pendingShedId = assignment.campaignShedId
                                pendingReason = ""
                            },
                            onAbandon = {
                                pendingAction = OVERSIGHT_ACTION_ABANDON
                                pendingShedId = assignment.campaignShedId
                                pendingReason = ""
                            },
                        )
                    } else {
                        // No oversight authority on this read: the row must not even LOOK actionable.
                        WeighingReadOnlyCard(assignment)
                    }
                }
                if (state.assignmentsLoadingMore) {
                    item(key = "operators-loading-more") { ListLoadingFooter() }
                }
            }
        }
    }
}

@Composable
private fun SectionHeading(text: String) {
    Text(
        text = text,
        color = MeshaColors.Muted,
        fontSize = 12.sp,
        fontWeight = FontWeight.Bold,
        modifier = Modifier.fillMaxWidth(),
    )
}

/**
 * ONE person and what their weighing work adds up to.
 *
 * Every number is the backend's; nothing is recomputed from the shed page below. The state counts
 * are named with their unit ("3 submitted") rather than left as bare numerals beside a name, and
 * only the non-zero ones are rendered, so the card states facts instead of listing zeroes.
 */
@Composable
internal fun WeighingOperatorCard(row: WeighingOperatorUiRow) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = operatorHeadline(row),
            color = if (row.name.isBlank()) MeshaColors.Muted else MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        // What a director asks for first: how much work this person holds, how many animals they
        // have actually put on the scale, and — the part a single number could never say — how
        // many of those they have SUBMITTED. Mid-shift "3 weighed · 0 submitted" is real and is
        // exactly where work is silently lost when someone walks away. All plain counts; none
        // divides another, because free-flow weighing has no expected roster to divide by.
        Text(
            text = listOf(
                if (row.shedCount == 1) {
                    stringResource(R.string.weighing_operators_sheds_one, row.shedCount)
                } else {
                    stringResource(R.string.weighing_operators_sheds_other, row.shedCount)
                },
                if (row.animalsWeighed <= 0) {
                    stringResource(R.string.weighing_operators_nothing_weighed_yet)
                } else {
                    stringResource(
                        R.string.weighing_weighed_submitted_fmt,
                        row.animalsWeighed,
                        row.animalsSubmitted,
                    )
                },
            ).joinToString(" · "),
            color = MeshaColors.Ink,
            style = MeshaType.cardSubtitle,
        )
        // The chip mirrors the operator's own Submit button, so a director and the operator use
        // the same word for the same act. Only shown when there is work AND none of it is in.
        if (row.animalsWeighed > 0 && row.animalsSubmitted <= 0) {
            Text(
                text = stringResource(R.string.weighing_not_submitted_chip),
                color = MeshaColors.Warn,
                style = MeshaType.cardSubtitle,
            )
        }
        OperatorShedStrip(row)
        Text(
            text = operatorStateBreakdown(row),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}

/**
 * The person's sheds as DISCRETE segments, one per shed, coloured by that shed's state.
 *
 * Deliberately not a continuous part-filled bar. The old oversight card drew one of those and left
 * it permanently empty, which read as "no progress" on work that was finished. Segments carry a
 * denominator this screen actually HAS — the number of sheds this person was given — and never
 * imply a share of animals, which weighing cannot know.
 */
@Composable
private fun OperatorShedStrip(row: WeighingOperatorUiRow) {
    if (row.shedCount <= 0) return
    // Order matches the ladder: finished work on the left, untouched work on the right, so the
    // strip reads left-to-right as "how far has this person got".
    val segments: List<Color> = buildList {
        repeat(row.accepted) { add(MeshaColors.Ok) }
        repeat(row.submitted) { add(MeshaColors.Brand) }
        repeat(row.capturing) { add(MeshaColors.BrandD) }
        repeat(row.notStarted) { add(MeshaColors.Bg) }
    }
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        segments.forEach { tint ->
            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(7.dp)
                    .clip(RoundedCornerShape(99.dp))
                    .background(tint),
            )
        }
    }
}

/**
 * The state counts as a sentence, in the farm's words, listing only what is actually there.
 *
 * Rework is listed LAST and separately because it OVERLAPS the four states rather than joining
 * them: a bounced bucket is still submitted or still being weighed.
 */
@Composable
private fun operatorStateBreakdown(row: WeighingOperatorUiRow): String {
    val parts = buildList {
        if (row.accepted > 0) add(stringResource(R.string.weighing_operators_state_accepted_fmt, row.accepted))
        if (row.submitted > 0) add(stringResource(R.string.weighing_operators_state_submitted_fmt, row.submitted))
        if (row.capturing > 0) add(stringResource(R.string.weighing_operators_state_capturing_fmt, row.capturing))
        if (row.notStarted > 0) add(stringResource(R.string.weighing_operators_state_not_started_fmt, row.notStarted))
        if (row.rework > 0) add(stringResource(R.string.weighing_operators_state_rework_fmt, row.rework))
    }
    return parts.joinToString(" · ")
}

/**
 * Who this row is, never a user id.
 *
 * Three genuinely different cases, said differently: a named person, work nobody holds yet, and an
 * assigned bucket whose owner has no active workforce record. The last one used to be
 * indistinguishable from the second, which hid a roster gap behind "unassigned".
 */
@Composable
private fun operatorHeadline(row: WeighingOperatorUiRow): String = when {
    row.name.isNotBlank() -> row.name
    row.isUnassigned -> stringResource(R.string.weighing_operators_unassigned)
    else -> stringResource(R.string.weighing_operators_roster_gap)
}

/**
 * One weighing shed row with NO action affordance, shared by the planner and oversight surfaces.
 *
 * The absence of a tap target is the point: both surfaces are read-only, so a row must not look
 * like it leads to a scan.
 *
 * ONE STATE, SAID ONCE. This card used to derive the bucket's state TWICE from the same field: a
 * badge printed the backend's status string ("Completed") while a second line and the progress bar
 * both asked `isClosed`, which is only true for the LATER 'closed' status. A submitted bucket
 * therefore rendered a Completed badge above a Scheduled line above an empty bar, all three
 * describing the same bucket. [shedStateLabel] is now the single place that decision is made.
 */
@Composable
internal fun WeighingReadOnlyCard(row: WeighingAssignmentUiRow) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = row.label,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = shedStateLabel(row),
                color = if (row.isClosed) MeshaColors.Ok else MeshaColors.BrandD,
                fontSize = 12.sp,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        // WHO did this shed. The screen is called Operators; a shed row with no name on it is the
        // defect this line closes.
        Text(
            text = if (row.operatorName.isNotBlank()) {
                stringResource(R.string.weighing_shed_weighed_by_fmt, row.operatorName)
            } else {
                stringResource(R.string.weighing_shed_weighed_by_nobody)
            },
            color = MeshaColors.Ink,
            style = MeshaType.cardSubtitle,
        )
        Text(
            text = if (row.category.equals("per_shed_partition", ignoreCase = true)) {
                stringResource(R.string.weighing_lump_sum_weighing)
            } else {
                stringResource(R.string.weighing_individual_weighing)
            },
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}

/**
 * WHERE one shed bucket stands, decided ONCE from its status.
 *
 * 'completed' means the operator submitted and a verifier has not looked yet; 'closed' means
 * verified and accepted. Collapsing those two into a single boolean is what let one card claim
 * both "Completed" and "Scheduled" at the same time.
 */
@Composable
internal fun shedStateLabel(row: WeighingAssignmentUiRow): String = when {
    row.isClosed -> stringResource(R.string.weighing_shed_state_accepted)
    row.isSubmittedAndWaitingVerification -> stringResource(R.string.weighing_shed_state_submitted)
    row.status.trim().equals("in_progress", ignoreCase = true) ||
        row.status.trim().equals("in progress", ignoreCase = true) ->
        stringResource(R.string.weighing_shed_state_capturing)
    else -> stringResource(R.string.weighing_shed_state_not_started)
}

@Composable
internal fun WeighingReadOnlyEmptyCard(loading: Boolean, title: String) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(8.dp))
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            text = if (loading) stringResource(R.string.weighing_readonly_loading) else title,
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
        )
        Text(
            text = stringResource(R.string.weighing_readonly_empty_body),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}
