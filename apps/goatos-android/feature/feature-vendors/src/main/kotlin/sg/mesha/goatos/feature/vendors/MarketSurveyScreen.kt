package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; MarketSurveyViewModel (in :app) owns the market_*
// AnalyticsEventsMarket + CrashReporter wiring for every read refresh and card open.

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
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
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
 * The Sales module's Market tab (`/sales/market`, maintainer decision 2026-09-14; it moved
 * out of Procurement on 2026-09-20): one
 * card per city on the morning call list. The whole day is one small list (a handful of cities
 * by construction -- the backend caps the list at 50), so it renders from the cached day blob
 * with no paging; a tap opens the city's entry form.
 */
@Composable
fun MarketSurveyScreen(
    state: MarketSurveyUiState,
    onEvent: (MarketSurveyEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(MarketSurveyEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                subtitle = listOf(state.dateLine, state.summaryLine).filter { it.isNotBlank() }.joinToString(" · ").ifBlank { null },
                below = {
                    SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = state.cards.isNotEmpty())
                },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(MarketSurveyEvent.Refresh) }) },
            )
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(top = 4.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (state.cards.isEmpty() && state.emptyMessage != null) {
                    item(key = "empty") {
                        EmptyState(
                            title = state.emptyMessage,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                            icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Package,
                            tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                        )
                    }
                }
                items(items = state.cards, key = { it.cityId }) { card ->
                    MarketCityCard(card = card, hint = if (state.canRecord) HINT_TAP_TO_ENTER else HINT_READ_ONLY) {
                        onEvent(MarketSurveyEvent.OpenCity(card.cityId))
                    }
                }
            }
        }
    }
}

@Composable
private fun MarketCityCard(card: MarketCityCardUi, hint: String, onClick: () -> Unit) {
    VendorsCard(onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            VendorsIconTile(icon = MeshaIcons.Package, tint = MeshaColors.Info, background = MeshaColors.InfoX)
            Column(Modifier.weight(1f)) {
                Text(text = card.cityName, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(2.dp))
                Text(text = card.progressLine, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            VendorsChip(label = card.statusLabel, tone = card.statusTone)
        }
        Text(text = hint, color = MeshaColors.Muted, style = MeshaType.rowCaption, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

private const val HINT_TAP_TO_ENTER = "Tap to enter today's prices"
private const val HINT_READ_ONLY = "Tap to see today's prices"
