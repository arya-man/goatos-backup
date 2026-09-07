package sg.mesha.goatos.feature.penvisits

// telemetry:exempt pure stateless renderer; PenVisitListViewModel (in :app) owns the
// pen_visit_* AnalyticsEventsPenVisits + CrashReporter wiring for every refresh and row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
 * The pen-visit "For me" L0 list (`/pen-visits`): the park head's own visits — the backend
 * decides which, the screen only renders rows.
 *
 * Paged (~20 rows) with stable task-id keys and a PASSIVE loading footer — never a "Load more"
 * button (docs/decisions/mobile-data-fetch-anti-patterns.md). The one header action refreshes
 * THIS list and travels to no other feature.
 */
@Composable
fun PenVisitListScreen(
    state: PenVisitListUiState,
    rows: LazyPagingItems<PenVisitCardUi>,
    onEvent: (PenVisitListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(PenVisitListEvent.Refresh) }
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
                    onSync = { onEvent(PenVisitListEvent.Refresh) },
                    contentDescription = stringResource(R.string.pen_visits_action_refresh),
                )
            },
        )
        if (state.filters.isNotEmpty()) {
            PenVisitFilterRow(filters = state.filters, onEvent = onEvent)
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
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.PenVisit,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                rows[index]?.let { card ->
                    PenVisitCard(card) { onEvent(PenVisitListEvent.OpenTask(card.taskId)) }
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
private fun PenVisitFilterRow(
    filters: List<PenVisitFilterUi>,
    onEvent: (PenVisitListEvent) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        filters.forEach { filter -> // compose-guard:ignore: two backend filter chips (To do / Done)
            PenVisitFilterChip(filter = filter) {
                onEvent(PenVisitListEvent.SelectFilter(filter.key))
            }
        }
    }
}

@Composable
private fun PenVisitFilterChip(filter: PenVisitFilterUi, onClick: () -> Unit) {
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
internal fun PenVisitCard(card: PenVisitCardUi, onOpen: () -> Unit) {
    Row(
        modifier = penVisitCardModifier(onClick = onOpen),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box(
            modifier = Modifier
                .size(40.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(penVisitToneAccent(card.tone).copy(alpha = 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = if (card.done) MeshaIcons.CheckCircle else MeshaIcons.PenVisit,
                contentDescription = null,
                tint = penVisitToneAccent(card.tone),
                modifier = Modifier.size(20.dp),
            )
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                // Backend-composed pen label, verbatim — the one line that names where to go.
                Text(
                    text = card.penLabel,
                    color = MeshaColors.Ink,
                    style = MeshaType.cardTitle,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (card.sending) {
                    PenVisitSendingMark()
                } else {
                    PenVisitStateChip(label = card.stateChip, tone = card.tone)
                }
            }
            // Backend-composed reason line ("Vaccination yesterday"), verbatim -- WHY the pen is
            // on the list is the second thing a park head reads, right under WHERE.
            if (card.reasonLine.isNotBlank()) {
                Text(
                    text = card.reasonLine,
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            // The park, quietly: the same person may hold both parks one day.
            if (card.parkName.isNotBlank()) {
                Text(
                    text = card.parkName,
                    color = MeshaColors.Faint,
                    style = MeshaType.caption,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/** The quiet mark a card wears while its video/submit is still on the wire. */
@Composable
private fun PenVisitSendingMark() {
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(MeshaColors.Info.copy(alpha = 0.16f))
            .padding(horizontal = 8.dp, vertical = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        CircularProgressIndicator(color = MeshaColors.Info, strokeWidth = 2.dp, modifier = Modifier.size(12.dp))
        Text(text = stringResource(R.string.pen_visits_state_sending), color = MeshaColors.Info, style = MeshaType.pillStrong)
    }
}
