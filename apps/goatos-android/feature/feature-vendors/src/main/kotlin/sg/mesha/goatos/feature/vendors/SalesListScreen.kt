package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SalesListViewModel (in :app) owns the vendors_*
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
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
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

/** The Procurement module's third tab (`/vendors/sales`): the sales ledger. */
@Composable
fun SalesListScreen(
    state: SalesListUiState,
    rows: LazyPagingItems<SaleCardUi>,
    onEvent: (SalesListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(SalesListEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                subtitle = state.countLine.ifBlank { null },
                below = {
                    SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = rows.itemCount > 0)
                },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(SalesListEvent.Refresh) }) },
            )
            if (state.filters.isNotEmpty()) {
                VendorsFilterRow(filters = state.filters, onSelect = { onEvent(SalesListEvent.SelectFarm(it)) })
            }
            // A refresh that inserts rows ABOVE the first visible one (a sale recorded a moment
            // ago) keeps the old row anchored; snap to the top so the new row is seen, but only
            // when the person is already near the top -- never yank a deliberate scroll.
            val listState = rememberLazyListState()
            val firstKey = if (rows.itemCount > 0) rows.peek(0)?.listKey else null
            LaunchedEffect(firstKey) {
                if (firstKey != null && listState.firstVisibleItemIndex in 1..3) listState.scrollToItem(0)
            }
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(top = 4.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (rows.itemCount == 0 && state.emptyMessage != null) {
                    item(key = "empty") {
                        EmptyState(
                            title = state.emptyMessage,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                            icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Sale,
                            tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                        )
                    }
                }
                items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                    rows[index]?.let { card -> SaleCard(card) { onEvent(SalesListEvent.OpenSale(card.dealId)) } }
                }
                if (rows.loadState.append is LoadState.Loading) {
                    item(key = "loading_footer") {
                        Box(Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                            CircularProgressIndicator(color = MeshaColors.BrandD)
                        }
                    }
                }
            }
        }
        if (state.canAdd) {
            VendorsAddButton(
                label = ADD_LABEL,
                onClick = { onEvent(SalesListEvent.AddSale) },
                modifier = Modifier.align(Alignment.BottomEnd).padding(MeshaDimens.gutter),
            )
        }
    }
}

@Composable
private fun SaleCard(card: SaleCardUi, onClick: () -> Unit) {
    VendorsCard(onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            VendorsIconTile(icon = MeshaIcons.Sale, tint = MeshaColors.Teal, background = MeshaColors.TealX)
            Column(Modifier.weight(1f)) {
                Text(text = card.buyer, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(2.dp))
                Text(text = card.productLine, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            VendorsChip(label = card.statusLabel, tone = card.statusTone)
        }
        Text(text = card.valueLine, color = MeshaColors.Ink, style = MeshaType.rowValue, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(text = card.metaLine, color = MeshaColors.Muted, style = MeshaType.rowCaption, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

private const val ADD_LABEL = "Record sale"
