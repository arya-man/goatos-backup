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
import androidx.compose.foundation.clickable
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.SyncIconButton

// telemetry:exempt Leadership weighing summary is read-only in V1; execution and verification state changes are tracked downstream.

@Composable
fun LeadershipWeighingScreen(
    state: WeighingUiState,
    onRefresh: () -> Unit = {},
    onReopenAssignment: (WeighingAssignmentUiRow) -> Unit = {},
    onCloseAssignment: (WeighingAssignmentUiRow, String) -> Unit = { _, _ -> },
    onAssignmentRowVisible: (Int) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize()) {
        MeshaScreenHeader(
            title = stringResource(R.string.weighing_leadership_title),
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_leadership_refresh),
                )
            },
        )
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .background(MeshaColors.PageBg)
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (state.assignments.isEmpty()) {
                item { LeadershipEmptyCard(loading = state.loading) }
            } else {
                itemsIndexed(state.assignments, key = { _, assignment -> assignment.campaignShedId }) { index, assignment ->
                    // The list itself pulls the next page as the reader scrolls near the end.
                    LaunchedEffect(assignment.campaignShedId, index, state.assignments.size) {
                        onAssignmentRowVisible(index)
                    }
                    LeadershipAssignmentCard(
                        assignment,
                        onReopen = { onReopenAssignment(assignment) },
                        onClose = { reason -> onCloseAssignment(assignment, reason) },
                    )
                }
                if (state.assignmentsLoadingMore) {
                    item(key = "assignments-loading-more") { ListLoadingFooter() }
                }
            }
        }
    }
}

@Composable
private fun LeadershipAssignmentCard(
    row: WeighingAssignmentUiRow,
    onReopen: () -> Unit = {},
    onClose: (String) -> Unit = {},
) {
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
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = row.label,
                    color = MeshaColors.Ink,
                    style = MeshaType.cardTitle,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                if (complete) {
                    Text(
                        text = stringResource(R.string.weighing_leadership_tap_to_reopen),
                        color = MeshaColors.BrandD,
                        fontSize = 11.sp,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier
                            .padding(top = 4.dp)
                            .minimumInteractiveComponentSize()
                            .clip(RoundedCornerShape(4.dp))
                            .clickable(
                                role = Role.Button,
                                onClick = onReopen,
                            ),
                    )
                } else {
                    if (row.canClose) {
                        Text(
                            text = stringResource(R.string.weighing_leadership_close),
                            color = MeshaColors.BrandD,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier
                                .padding(top = 4.dp)
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(4.dp))
                                .clickable(
                                    role = Role.Button,
                                    onClick = { onClose("closed from mobile leadership") },
                                ),
                        )
                    } else {
                        Text(
                            text = if (row.pendingVerificationCount > 0) {
                                stringResource(
                                    R.string.weighing_leadership_awaiting_verification,
                                    row.pendingVerificationCount,
                                )
                            } else {
                                stringResource(R.string.weighing_leadership_close_pending)
                            },
                            color = MeshaColors.Muted,
                            fontSize = 11.sp,
                            fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.padding(top = 4.dp),
                        )
                    }
                }
            }
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
private fun LeadershipEmptyCard(loading: Boolean) {
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
            text = if (loading) stringResource(R.string.weighing_leadership_loading) else stringResource(R.string.weighing_leadership_empty_title),
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
        )
        Text(
            text = stringResource(R.string.weighing_leadership_empty_body),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
        )
    }
}
