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
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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

/**
 * Oversight of weighing work assigned to SOMEONE ELSE.
 *
 * A SEPARATE destination from the work list and the planner list, not a mode of one shared screen.
 * It never offers a SCAN action -- capture stays with the shed's assignee. It does offer close /
 * reopen / abandon, but only when the backend's own capability flags say this viewer holds the
 * monitor authority: this is the Growth Director's leadership surface, and leaving it purely
 * read-only left them with no reachable way to end or reopen the work they oversee.
 */
@Composable
fun WeighingOperatorsScreen(
    state: WeighingUiState,
    onRefresh: () -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    onReopenAssignment: (WeighingAssignmentUiRow) -> Unit = {},
    onCloseAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onAbandonAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onAssignmentRowVisible: (Int) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    // The row awaiting an abandon CONFIRMATION, or null. Abandon is irreversible and reason-bearing,
    // so the tap opens this and the write only leaves the screen with an operator-typed reason.
    var abandonTarget by remember { mutableStateOf<WeighingAssignmentUiRow?>(null) }
    abandonTarget?.let { target ->
        WeighingAbandonDialog(
            shedLabel = target.label,
            onConfirm = { reason ->
                abandonTarget = null
                onAbandonAssignment(target, reason)
            },
            onDismiss = { abandonTarget = null },
        )
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
            if (state.assignments.isEmpty()) {
                item { WeighingReadOnlyEmptyCard(loading = state.loading, title = stringResource(R.string.weighing_operators_empty_title)) }
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
                            onReopen = { onReopenAssignment(assignment) },
                            onClose = { reason -> onCloseAssignment(assignment, reason) },
                            onAbandon = { abandonTarget = assignment },
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

/**
 * Confirms an ABANDON and collects the reason that will be recorded against it.
 *
 * Abandon ends work that will never finish: it writes an audit action, emits a domain event, and
 * cannot be undone from the phone. [onConfirm] is therefore only ever invoked with a non-blank
 * reason typed by the person ending the work -- the previous one-tap path sent a fabricated
 * constant, which put a sentence nobody wrote into the audit trail.
 */
@Composable
private fun WeighingAbandonDialog(
    shedLabel: String,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var reason by remember { mutableStateOf("") }
    var showError by remember { mutableStateOf(false) }
    val latestReason by rememberUpdatedState(reason)

    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(
                text = stringResource(R.string.weighing_abandon_dialog_title, shedLabel),
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
            )
        },
        text = {
            Column {
                Text(
                    text = stringResource(R.string.weighing_abandon_dialog_subtitle),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(bottom = 10.dp),
                )
                OutlinedTextField(
                    value = reason,
                    onValueChange = {
                        reason = it
                        if (it.isNotBlank()) showError = false
                    },
                    placeholder = { Text(stringResource(R.string.weighing_abandon_dialog_placeholder)) },
                    isError = showError,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = MeshaColors.Brand,
                        unfocusedBorderColor = MeshaColors.Hair,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (showError) {
                    Text(
                        text = stringResource(R.string.weighing_abandon_dialog_error_required),
                        color = MeshaColors.Danger,
                        style = MeshaType.cardSubtitle,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val trimmed = latestReason.trim()
                if (trimmed.isBlank()) showError = true else onConfirm(trimmed)
            }) {
                Text(
                    text = stringResource(R.string.weighing_leadership_abandon),
                    color = MeshaColors.Danger,
                    style = MeshaType.cardSubtitle,
                )
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(
                    text = stringResource(R.string.weighing_abandon_dialog_cancel),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
            }
        },
        containerColor = MeshaColors.Surf,
    )
}

/**
 * One weighing shed row with NO action affordance, shared by the planner and oversight surfaces.
 *
 * The absence of a tap target is the point: both surfaces are read-only, so a row must not look
 * like it leads to a scan.
 */
@Composable
internal fun WeighingReadOnlyCard(row: WeighingAssignmentUiRow) {
    val complete = row.isClosed
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
                text = row.status.ifBlank { stringResource(R.string.weighing_status_scheduled) },
                color = if (complete) MeshaColors.Ok else MeshaColors.BrandD,
                fontSize = 12.sp,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.padding(start = 12.dp),
            )
        }
        Text(
            text = if (row.category.equals("per_shed_partition", ignoreCase = true)) {
                stringResource(R.string.weighing_lump_sum_weighing)
            } else {
                stringResource(R.string.weighing_individual_weighing)
            },
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(7.dp)
                .clip(RoundedCornerShape(4.dp))
                .background(MeshaColors.Bg),
        ) {
            if (complete) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(7.dp)
                        .background(MeshaColors.Brand),
                )
            }
        }
        Text(
            text = if (complete) stringResource(R.string.weighing_status_completed) else stringResource(R.string.weighing_status_scheduled),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
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
