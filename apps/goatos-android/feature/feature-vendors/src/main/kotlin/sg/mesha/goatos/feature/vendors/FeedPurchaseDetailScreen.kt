package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; FeedPurchaseDetailViewModel (in :app) owns the
// vendors_* AnalyticsEventsVendors + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/** One feed purchase (L1 drill): the load, its money, and where the delivery stands. Read-only. */
@Composable
fun FeedPurchaseDetailScreen(
    state: FeedPurchaseDetailUiState,
    onEvent: (FeedPurchaseDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(FeedPurchaseDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(FeedPurchaseDetailEvent.Back) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(FeedPurchaseDetailEvent.Refresh) }) },
        )
        if (state.isLoading) {
            LoadingSkeletonList(modifier = Modifier.padding(MeshaDimens.gutter))
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "status") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    VendorsChip(label = state.deliveryLabel, tone = state.deliveryTone)
                }
            }
            if (state.deliveryNote.isNotBlank()) {
                item(key = "delivery_note") {
                    Text(
                        text = state.deliveryNote,
                        color = MeshaColors.Warn,
                        style = MeshaType.body,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                            .background(MeshaColors.WarnX)
                            .padding(horizontal = 14.dp, vertical = 12.dp),
                    )
                }
            }
            items(count = state.sections.size, key = { "section_${state.sections[it].title}" }) { index ->
                VendorsDetailSection(state.sections[index])
            }
        }
    }
}
