package sg.mesha.goatos.feature.toxin

// telemetry:exempt pure stateless renderer; ToxinTaskListViewModel (in :app) owns the toxin_*
// AnalyticsEventsToxin + CrashReporter wiring for every read refresh and row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
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
 * The Toxin module's L0 list (`/toxin`): every aflatoxin test round waiting on someone.
 *
 * Paged (~20 rows) with stable task-id keys and a PASSIVE loading footer — never a "Load more"
 * button (docs/decisions/mobile-data-fetch-anti-patterns.md).
 *
 * Refresh-on-open matters MORE here than on most read screens: the seven steps are
 * person-independent, so a teammate may have advanced (or finished) a round since this phone last
 * looked, and a round whose settle window elapsed becomes actionable purely by the passage of
 * SERVER time with no local event at all.
 */
@Composable
fun ToxinTaskListScreen(
    state: ToxinTaskListUiState,
    rows: LazyPagingItems<ToxinTaskCardUi>,
    onEvent: (ToxinTaskListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ToxinTaskListEvent.Refresh) }
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
                    onSync = { onEvent(ToxinTaskListEvent.Refresh) },
                )
            },
        )
        if (state.filters.isNotEmpty()) {
            ToxinFilterRow(filters = state.filters, onEvent = onEvent)
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Check,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                rows[index]?.let { card ->
                    ToxinTaskCard(card) { onEvent(ToxinTaskListEvent.OpenTask(card.taskId)) }
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

/**
 * The list's filter chips. Order, labels and counts are all BACKEND-COMPOSED — this only draws
 * them and reports the tapped key back. The row scrolls horizontally so a future fourth chip, or
 * a long translated label, never squeezes the others off-screen.
 */
@Composable
private fun ToxinFilterRow(
    filters: List<ToxinFilterUi>,
    onEvent: (ToxinTaskListEvent) -> Unit,
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
            ToxinFilterChip(filter = filter) {
                onEvent(ToxinTaskListEvent.SelectFilter(filter.key))
            }
        }
    }
}

@Composable
private fun ToxinFilterChip(filter: ToxinFilterUi, onClick: () -> Unit) {
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
        // Backend-owned chip copy and count, rendered verbatim.
        Text(text = filter.label, color = labelColor, style = MeshaType.pill)
        Text(text = filter.count.toString(), color = labelColor.copy(alpha = 0.75f), style = MeshaType.pill)
    }
}

@Composable
internal fun ToxinTaskCard(card: ToxinTaskCardUi, onOpen: () -> Unit) {
    Column(
        modifier = toxinCardModifier(enabled = card.openable, onClick = onOpen.takeIf { card.openable }),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            // Backend-composed farm line (feed, vendor, load, date), rendered verbatim.
            Text(
                text = card.contextLine,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                modifier = Modifier.weight(1f),
            )
            ToxinStatusChip(label = card.statusChip, tone = card.statusTone)
        }
        // Why this round exists at all when it is a retest. Backend-owned sentence, verbatim.
        if (card.originLine.isNotBlank()) {
            Text(text = card.originLine, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        if (card.stepsTotal > 0) {
            ToxinStepProgress(done = card.stepsDone, total = card.stepsTotal)
        }
    }
}

/**
 * How far this round has got, as a filled bar plus the bare `done/total` figures.
 *
 * Deliberately NOT a composed sentence: the backend owns every word on this screen and publishes
 * no progress copy, so the renderer shows the two numbers it was actually given rather than
 * inventing a phrase around them.
 */
@Composable
internal fun ToxinStepProgress(done: Int, total: Int, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(
            modifier = Modifier
                .weight(1f)
                .height(6.dp)
                .clip(RoundedCornerShape(20.dp))
                .background(MeshaColors.Surf3),
        ) {
            val filled = done.coerceIn(0, total)
            if (filled > 0) {
                Box(
                    modifier = Modifier
                        .weight(filled.toFloat())
                        .fillMaxSize()
                        .background(MeshaColors.Brand),
                )
            }
            if (filled < total) {
                Box(modifier = Modifier.weight((total - filled).toFloat()).fillMaxSize())
            }
        }
        Text(text = "$done/$total", color = MeshaColors.Muted, style = MeshaType.pillStrong)
    }
}
