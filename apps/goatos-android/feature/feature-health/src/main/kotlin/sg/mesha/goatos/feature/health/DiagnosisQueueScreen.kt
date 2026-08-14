package sg.mesha.goatos.feature.health

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.component.MeshaStatusPill
import sg.mesha.goatos.core.designsystem.component.MeshaTone
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * The Health Director's queue: animals checked, waiting on a decision.
 *
 * Read from Room and refreshed behind it, because it is opened in a shed. A
 * Director who has no signal still sees the queue they last loaded.
 *
 * The rows are ordered NEWEST FIRST by the backend and rendered in that order.
 * Urgency is shown, never re-sorted on: an emergency raises a row's prominence
 * without moving it, because a queue that silently reorders itself between two
 * looks is one the Director cannot keep their place in.
 */
@Composable
fun DiagnosisQueueScreen(
    state: DiagnosisQueueState,
    rows: LazyPagingItems<DiagnosisQueueRow>,
    onEvent: (DiagnosisQueueEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(DiagnosisQueueEvent.Refresh) }

    Column(modifier.fillMaxSize().background(MeshaColors.Bg)) {
        MeshaScreenHeader(
            title = "Waiting on you",
            subtitle = if (state.mayConfirm) {
                "Animals checked, waiting for your decision"
            } else {
                // A manager reading their own submissions: honest about who decides.
                "Animals checked, waiting for the Health Director"
            },
            onBack = { onEvent(DiagnosisQueueEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.refreshing,
                    onSync = { onEvent(DiagnosisQueueEvent.Refresh) },
                )
            },
        )

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            items(rows.itemCount) { index ->
                // A null placeholder cannot happen (placeholders are off) but Paging's
                // accessor is nullable; skipping is correct rather than rendering a blank row.
                val row = rows[index] ?: return@items
                QueueRow(row = row, onOpen = { onEvent(DiagnosisQueueEvent.Open(row.diagnosisRunId)) })
            }

            // A passive footer, never a tappable "Load more": the next page is already
            // in flight, and asking the user to fetch their own list is not their job.
            if (rows.loadState.append is LoadState.Loading) {
                item {
                    Text(
                        "Loading more…",
                        style = MeshaType.caption,
                        color = MeshaColors.Faint,
                        modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                    )
                }
            }

            if (rows.itemCount == 0 && rows.loadState.refresh !is LoadState.Loading) {
                item {
                    MeshaCard {
                        Text("Nothing waiting", style = MeshaType.cardTitle)
                        Text(
                            "Every animal that was checked has been dealt with.",
                            style = MeshaType.body,
                            color = MeshaColors.Faint,
                            modifier = Modifier.padding(top = 4.dp),
                        )
                    }
                }
            }

            state.message?.let { message ->
                item { Text(message, style = MeshaType.body, color = MeshaColors.Faint) }
            }
        }
    }
}

/**
 * One animal awaiting a decision.
 *
 * The emergency count is on the ROW rather than behind the tap, because an
 * emergency is work already owed that no decision gates. Burying it one level
 * down is the failure this layout exists to avoid.
 */
@Composable
private fun QueueRow(row: DiagnosisQueueRow, onOpen: () -> Unit) {
    MeshaCard(accent = if (row.urgent) MeshaColors.Danger else null) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clickable(onClick = onOpen)
                .minimumInteractiveComponentSize(),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(row.goatDisplayId, style = MeshaType.cardTitle)
                Text(row.seen, style = MeshaType.caption, color = MeshaColors.Faint)
            }

            if (row.location.isNotBlank()) {
                Text(
                    row.location,
                    style = MeshaType.caption,
                    color = MeshaColors.Faint,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }

            Text(
                problemHeadline(row.problems),
                style = MeshaType.body,
                modifier = Modifier.padding(top = 6.dp),
            )

            if (row.urgent || row.unexplainedCount > 0) {
                Row(
                    modifier = Modifier.padding(top = 6.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    if (row.urgent) {
                        MeshaStatusPill(label = "Needs action now", tone = MeshaTone.Danger)
                    }
                    if (row.unexplainedCount > 0) {
                        MeshaStatusPill(label = "Not explained", tone = MeshaTone.Warn)
                    }
                }
            }
        }
    }
}
