package sg.mesha.goatos.feature.leadershiptasks

// telemetry:exempt pure stateless renderer; LeadershipTaskListViewModel (in :app) owns the
// leadership_task_* AnalyticsEventsLeadershipTasks + CrashReporter wiring for every refresh and row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The Leadership Tasks L0 list (`/leadership-tasks`): a director's raised tasks, or a CXO's
 * assigned ones — the backend decides which, the screen only renders rows.
 *
 * Paged (~20 rows) with stable task-id keys and a PASSIVE loading footer — never a "Load more"
 * button (docs/decisions/mobile-data-fetch-anti-patterns.md). The "+" is offered ONLY when the
 * backend said `can_raise`; both header actions act on THIS screen (raise into this list,
 * refresh this list) and travel to no other feature.
 */
@Composable
fun LeadershipTaskListScreen(
    state: LeadershipTaskListUiState,
    rows: LazyPagingItems<LeadershipTaskCardUi>,
    onEvent: (LeadershipTaskListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(LeadershipTaskListEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            below = {
                SyncStatusIndicator(
                    isRefreshing = state.isRefreshing,
                    lastSyncedAt = state.lastSyncedAt,
                    hasData = rows.itemCount > 0,
                )
            },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(LeadershipTaskListEvent.Refresh) },
                    contentDescription = stringResource(R.string.leadership_tasks_action_refresh),
                )
                if (state.canRaise) {
                    Spacer(Modifier.width(8.dp))
                    // The raise action, rightmost and in brand green: the one thing a director
                    // comes to this screen to do (maintainer instruction 2026-09-04).
                    LeadershipRaiseButton(
                        contentDescription = stringResource(R.string.leadership_tasks_action_raise),
                        onClick = { onEvent(LeadershipTaskListEvent.RaiseTask) },
                    )
                }
            },
        )
        if (state.filters.isNotEmpty()) {
            LeadershipFilterRow(filters = state.filters, onEvent = onEvent)
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Tasks,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                rows[index]?.let { card ->
                    LeadershipTaskCard(card) { onEvent(LeadershipTaskListEvent.OpenTask(card.taskId)) }
                }
            }

            // Passive loading footer: the next page is already in flight while this spins.
            if (rows.loadState.append is LoadState.Loading) {
                item(key = "loading_footer") {
                    Box(
                        modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(color = MeshaColors.BrandD)
                    }
                }
            }
        }
    }
}

/** The filter chips. Order, labels and counts are BACKEND-COMPOSED; the tap reports the key. */
@Composable
private fun LeadershipFilterRow(
    filters: List<LeadershipTaskFilterUi>,
    onEvent: (LeadershipTaskListEvent) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        filters.forEach { filter ->
            LeadershipFilterChip(filter = filter) {
                onEvent(LeadershipTaskListEvent.SelectFilter(filter.key))
            }
        }
    }
}

@Composable
private fun LeadershipFilterChip(filter: LeadershipTaskFilterUi, onClick: () -> Unit) {
    val background = if (filter.selected) MeshaColors.BrandTint else MeshaColors.Surf
    val border = if (filter.selected) MeshaColors.BrandD else MeshaColors.Hair
    val labelColor = if (filter.selected) MeshaColors.BrandD else MeshaColors.Muted
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(background)
            .border(1.dp, border, RoundedCornerShape(999.dp))
            .selectable(selected = filter.selected, role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = filter.label, color = labelColor, style = MeshaType.pill)
        Text(text = filter.count.toString(), color = labelColor.copy(alpha = 0.75f), style = MeshaType.pill)
    }
}

@Composable
internal fun LeadershipTaskCard(card: LeadershipTaskCardUi, onOpen: () -> Unit) {
    Row(
        modifier = leadershipCardModifier(onClick = onOpen),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // An unseen assigned task carries a quiet brand rail on its left edge — the one visual
        // difference between "new to you" and "already looked at", nothing louder.
        Box(
            modifier = Modifier
                .width(3.dp)
                .height(44.dp)
                .clip(RoundedCornerShape(2.dp))
                .background(if (card.unseen) MeshaColors.Brand else MeshaColors.Surf),
        )
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                // Backend-composed number ("#12"), verbatim.
                Text(
                    text = card.numberLabel,
                    color = if (card.unseen) MeshaColors.BrandD else MeshaColors.Faint,
                    style = MeshaType.pillStrong,
                    modifier = Modifier.weight(1f),
                )
                LeadershipStatusChip(label = card.statusChip, status = card.status)
            }
            Text(
                text = card.title,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                // Backend-composed meta line, verbatim.
                Text(
                    text = card.metaLine,
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (card.attachmentCount > 0) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                        Icon(
                            imageVector = MeshaIcons.Paperclip,
                            contentDescription = null,
                            tint = MeshaColors.Faint,
                            modifier = Modifier.size(14.dp),
                        )
                        Text(text = card.attachmentCount.toString(), color = MeshaColors.Faint, style = MeshaType.caption)
                    }
                }
            }
        }
    }
}
