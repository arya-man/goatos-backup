package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; VendorsListViewModel (in :app) owns the vendors_*
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
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

/**
 * The Vendors module's register list (`/vendors`): every counterparty the farm buys from, searched
 * and narrowed by status. Paged (~20 rows) with stable vendor-id keys and a PASSIVE loading
 * footer — never a "Load more" button (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
@Composable
fun VendorsListScreen(
    state: VendorsListUiState,
    rows: LazyPagingItems<VendorCardUi>,
    onEvent: (VendorsListEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(VendorsListEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                subtitle = state.countLine.ifBlank { null },
                below = {
                    SyncStatusIndicator(
                        isRefreshing = state.isRefreshing,
                        lastSyncedAt = state.lastSyncedAt,
                        hasData = rows.itemCount > 0,
                    )
                },
                actions = {
                    SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(VendorsListEvent.Refresh) })
                },
            )
            VendorsSearchField(
                value = state.search,
                placeholder = SEARCH_PLACEHOLDER,
                onValueChange = { onEvent(VendorsListEvent.SearchChanged(it)) },
            )
            if (state.filters.isNotEmpty()) {
                VendorsFilterRow(filters = state.filters, onSelect = { onEvent(VendorsListEvent.SelectStatus(it)) })
            }
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(top = 4.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (rows.itemCount == 0 && state.emptyMessage != null) {
                    item(key = "empty") {
                        EmptyState(
                            title = state.emptyMessage,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                            icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Store,
                            tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                        )
                    }
                }
                items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                    rows[index]?.let { card ->
                        VendorCard(card) { onEvent(VendorsListEvent.OpenVendor(card.vendorId)) }
                    }
                }
                // Passive loading footer: the next page is already in flight while this spins.
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
                onClick = { onEvent(VendorsListEvent.AddVendor) },
                modifier = Modifier.align(Alignment.BottomEnd).padding(MeshaDimens.gutter),
            )
        }
    }
}

@Composable
private fun VendorCard(card: VendorCardUi, onClick: () -> Unit) {
    VendorsCard(onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            VendorsIconTile(icon = MeshaIcons.Store, tint = MeshaColors.BrandD, background = MeshaColors.BrandTint)
            Column(Modifier.weight(1f)) {
                Text(text = card.name, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(2.dp))
                Text(text = card.typeLine, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            VendorsChip(label = card.statusLabel, tone = card.statusTone)
        }
        if (card.capacityLine.isNotBlank() || card.hasVoiceNote) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (card.capacityLine.isNotBlank()) {
                    Icon(MeshaIcons.Package, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.width(MeshaDimens.iconSm).height(MeshaDimens.iconSm))
                    Text(text = card.capacityLine, color = MeshaColors.Ink, style = MeshaType.rowCaption, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
                }
                if (card.hasVoiceNote) {
                    Icon(MeshaIcons.Microphone, contentDescription = VOICE_NOTE_HINT, tint = MeshaColors.Teal, modifier = Modifier.width(MeshaDimens.iconSm).height(MeshaDimens.iconSm))
                }
            }
        }
    }
}

private const val SEARCH_PLACEHOLDER = "Search by business, contact or phone"
private const val ADD_LABEL = "Add vendor"
private const val VOICE_NOTE_HINT = "Has a voice note"
