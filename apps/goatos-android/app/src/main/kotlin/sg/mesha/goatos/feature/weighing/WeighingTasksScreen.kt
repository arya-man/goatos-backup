package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import androidx.compose.material3.Text
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

// telemetry:exempt Planner all-tasks list is read-only; weighing state changes are tracked on the
// execution and verification surfaces that own those writes.

/**
 * The planner's flat all-tasks list across parks.
 *
 * This is a SEPARATE destination from the work list and the operators surface, not a mode of one
 * shared screen. It is deliberately read-only and renders NO scan action: a planner is assigned no
 * sheds, and the weighing write requires the caller to be the shed's assignee, so a tappable row
 * here could only ever lead to a refused submit.
 */
@Composable
fun WeighingTasksScreen(
    state: WeighingUiState,
    onRefresh: () -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    onCreateOrEditTask: () -> Unit = {},
    onTogglePlannerShed: (String) -> Unit = {},
    onPlannerShedCategory: (String, String) -> Unit = { _, _ -> },
    onAssignmentRowVisible: (Int) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    Column(modifier = modifier.fillMaxSize()) {
        MeshaScreenHeader(
            title = "All tasks",
            eyebrow = "WEIGHING",
            eyebrowColor = MeshaColors.BrandD,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = "Refresh weighing tasks",
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
            // Planning comes first: this surface is where the week's weighing work is authored.
            if (state.plannerMode) {
                item(key = "planner") {
                    PlannerRootContent(
                        state = state,
                        onCreateOrEditTask = onCreateOrEditTask,
                        onTogglePlannerShed = onTogglePlannerShed,
                        onPlannerShedCategory = onPlannerShedCategory,
                    )
                }
                item(key = "scheduled-heading") {
                    Text(
                        text = "SCHEDULED WORK",
                        color = MeshaColors.Muted,
                        style = MeshaType.cardSubtitle,
                    )
                }
            }
            if (state.assignments.isEmpty()) {
                item { WeighingReadOnlyEmptyCard(loading = state.loading, title = "No weighing tasks") }
            } else {
                itemsIndexed(state.assignments, key = { _, assignment -> assignment.campaignShedId }) { index, assignment ->
                    LaunchedEffect(assignment.campaignShedId, index, state.assignments.size) {
                        onAssignmentRowVisible(index)
                    }
                    WeighingReadOnlyCard(assignment)
                }
                if (state.assignmentsLoadingMore) {
                    item(key = "tasks-loading-more") { ListLoadingFooter() }
                }
            }
        }
    }
}
