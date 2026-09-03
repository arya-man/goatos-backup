package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleDetailViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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

/** One sale (L1 drill): the sale, the buyer, the money, and the animals tagged to it. */
@Composable
fun SaleDetailScreen(
    state: SaleDetailUiState,
    onEvent: (SaleDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(SaleDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(SaleDetailEvent.Back) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(SaleDetailEvent.Refresh) }) },
        )
        if (state.isLoading) {
            LoadingSkeletonList(modifier = Modifier.padding(MeshaDimens.gutter))
            return@Column
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "status") {
                Row(verticalAlignment = Alignment.CenterVertically) { VendorsChip(label = state.statusLabel, tone = state.statusTone) }
            }
            state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
            items(count = state.sections.size, key = { "section_${state.sections[it].title}" }) { index ->
                VendorsDetailSection(state.sections[index])
            }
            item(key = "tagged") { TaggedAnimalsCard(state) }
        }
        if (state.canTagAnimals) {
            VendorsWizardBar(contextLine = "") {
                VendorsPrimaryButton(label = TAG_ANIMALS, enabled = true, onClick = { onEvent(SaleDetailEvent.TagAnimals) }, modifier = Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun TaggedAnimalsCard(state: SaleDetailUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = TAGGED_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        Text(text = state.taggedLine.ifBlank { TAGGED_NONE }, color = MeshaColors.Ink, style = MeshaType.rowValue)
        state.taggedGroups.forEach { group ->
            Column {
                Text(text = "${group.location} · ${group.animals}", color = MeshaColors.Ink, style = MeshaType.body)
                if (group.tags.isNotBlank()) Text(text = group.tags, color = MeshaColors.Muted, style = MeshaType.caption)
            }
        }
        if (!state.canTagAnimals && state.tagDisabledReason.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(text = state.tagDisabledReason, color = MeshaColors.Muted, style = MeshaType.caption)
        }
    }
}

private const val TAG_ANIMALS = "Tag animals to sale"
private const val TAGGED_TITLE = "ANIMALS TAGGED"
private const val TAGGED_NONE = "No animals tagged yet"
