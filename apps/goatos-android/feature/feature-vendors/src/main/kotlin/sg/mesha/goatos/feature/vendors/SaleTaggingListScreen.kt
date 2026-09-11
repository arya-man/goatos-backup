package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleTaggingListViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read refresh and row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The park head's tag-only queue (L0, `/sale-tagging`; maintainer decision 2026-09-11): the sales
 * at their park still owed animals. A row shows what the sale is and how many animals are still
 * to tag -- never the buyer, never the money -- and opens the tagging screen.
 */
@Composable
fun SaleTaggingListScreen(
    state: SaleTaggingListUiState,
    onEvent: (SaleTaggingListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(SaleTaggingListEvent.Refresh) }
    val listState = rememberLazyListState()
    val shouldLoadMore = remember(state.hasMore, state.loadingMore, state.rows.size) {
        derivedStateOf {
            if (!state.hasMore || state.loadingMore || state.rows.isEmpty()) return@derivedStateOf false
            val lastVisible = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: return@derivedStateOf false
            lastVisible >= listState.layoutInfo.totalItemsCount - 3
        }
    }
    LaunchedEffect(shouldLoadMore.value) { if (shouldLoadMore.value) onEvent(SaleTaggingListEvent.LoadMore) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = SUBTITLE,
            below = { SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = state.rows.isNotEmpty()) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(SaleTaggingListEvent.Refresh) }) },
        )
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.rows.isEmpty() && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Sale,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }
            items(count = state.rows.size, key = { state.rows[it].dealId }) { index ->
                SaleTaggingCard(state.rows[index]) { onEvent(SaleTaggingListEvent.OpenSale(state.rows[index].dealId)) }
            }
            if (state.loadingMore) {
                item(key = "loading_footer") {
                    Box(Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator(color = MeshaColors.BrandD)
                    }
                }
            }
        }
    }
}

@Composable
private fun SaleTaggingCard(card: SaleTaggingCardUi, onClick: () -> Unit) {
    VendorsCard(onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            VendorsIconTile(icon = MeshaIcons.Sale, tint = MeshaColors.Teal, background = MeshaColors.TealX)
            Column(Modifier.weight(1f)) {
                Text(text = card.title, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(2.dp))
                Text(text = card.countLine, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            VendorsChip(label = card.remainingChip, tone = VendorsTone.INFO)
        }
        Text(text = card.metaLine, color = MeshaColors.Muted, style = MeshaType.rowCaption, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

private const val SUBTITLE = "Sales still to be tagged at your park"
