package sg.mesha.goatos.feature.counts

// telemetry:exempt stateless renderer; MilkPreparationListViewModel owns refresh telemetry/state.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

enum class MilkPreparationCardBucket { TO_PREPARE, IN_REVIEW, COMPLETED, REWORK }

@Immutable
data class MilkPreparationChipUi(val key: String, val label: String, val count: Int)

@Immutable
data class MilkPreparationCardUi(
    val parkId: String,
    val parkLabel: String,
    val shedId: String,
    val shedLabel: String,
    val cohortCount: Int,
    val headCount: Int,
    val totalMilkLabel: String,
    val citricAcidLabel: String,
    val statusLabel: String,
    val actionLabel: String,
    val reworkReason: String,
    val bucket: MilkPreparationCardBucket,
    val canOpen: Boolean = true,
    val detailLabel: String = "",
    val preparationRequired: Boolean = true,
    /** Videos already recorded for this farm-day and held in the durable draft, 0 when untouched. */
    val capturedProofCount: Int = 0,
) {
    /**
     * True when the operator started this farm's preparation and left before submitting, so the list
     * can offer to resume instead of reading as untouched work.
     */
    val isInProgress: Boolean get() = capturedProofCount > 0 && bucket == MilkPreparationCardBucket.TO_PREPARE
}

@Immutable
data class MilkPreparationListUiState(
    val subtitle: String = "",
    val dateLabel: String = "",
    val selectedDate: String = "",
    val feedingDateLabel: String = "",
    val chips: List<MilkPreparationChipUi> = emptyList(),
    val selectedFilter: String = "all",
    val cards: List<MilkPreparationCardUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    val emptyMessage: String? = null,
)

sealed interface MilkPreparationListEvent {
    data object Refresh : MilkPreparationListEvent
    data class SelectFilter(val key: String) : MilkPreparationListEvent
    data class OpenFarm(val parkId: String, val preparationDate: String = "") : MilkPreparationListEvent
    data class NavigateDate(val delta: Int) : MilkPreparationListEvent
    data object Back : MilkPreparationListEvent
}

@Composable
fun MilkPreparationListScreen(
    state: MilkPreparationListUiState,
    onEvent: (MilkPreparationListEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(MilkPreparationListEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Milk Preparation",
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(MilkPreparationListEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(MilkPreparationListEvent.Refresh) },
                )
            },
        )
        SyncStatusIndicator(
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.cards.isNotEmpty(),
            isOffline = state.isOffline,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        MilkWorkDateBar(
            state.dateLabel,
            state.feedingDateLabel.takeIf(String::isNotBlank)?.let { "Feeds $it" }.orEmpty(),
            onPreviousDate = { onEvent(MilkPreparationListEvent.NavigateDate(-1)) },
            onNextDate = { onEvent(MilkPreparationListEvent.NavigateDate(1)) },
        )
        MilkStatusChips(state.chips, state.selectedFilter) { onEvent(MilkPreparationListEvent.SelectFilter(it)) }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.cards.isEmpty() && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                        icon = MeshaIcons.Feed,
                        tone = EmptyTone.Neutral,
                    )
                }
            }
            items(state.cards, key = { it.parkId }) { card ->
                MilkPreparationCard(card) { onEvent(MilkPreparationListEvent.OpenFarm(card.parkId, state.selectedDate)) }
            }
        }
    }
}

@Composable
internal fun MilkWorkDateBar(
    dateLabel: String,
    secondaryLabel: String = "",
    onPreviousDate: () -> Unit = {},
    onNextDate: () -> Unit = {},
) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        MilkDateButton(MeshaIcons.ChevronLeft, onClick = onPreviousDate)
        Column(
            modifier = Modifier.weight(1f).clip(RoundedCornerShape(12.dp)).background(MeshaColors.Surf2)
                .padding(vertical = 9.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(MeshaIcons.Calendar, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(15.dp))
                Text(dateLabel, color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
            }
            if (secondaryLabel.isNotBlank()) {
                Text(secondaryLabel, color = MeshaColors.Faint, fontSize = 10.sp)
            }
        }
        MilkDateButton(MeshaIcons.Chevron, onClick = onNextDate)
    }
}

@Composable
private fun MilkDateButton(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    onClick: () -> Unit = {},
) {
    Box(
        modifier = Modifier
            .size(48.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(16.dp))
    }
}

@Composable
internal fun MilkStatusChips(chips: List<MilkPreparationChipUi>, selectedFilter: String, onSelect: (String) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        chips.forEach { chip ->
            val selected = chip.key == selectedFilter
            Row(
                modifier = Modifier.clip(RoundedCornerShape(999.dp))
                    .background(if (selected) MeshaColors.Brand else MeshaColors.Surf2)
                    .clickable { onSelect(chip.key) }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Text(chip.label, color = if (selected) MeshaColors.OnBrand else MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700)
                Text(chip.count.toString(), color = if (selected) MeshaColors.OnBrand else MeshaColors.Faint, fontSize = 11.sp, fontWeight = FontWeight.W800)
            }
        }
    }
}

@Composable
private fun MilkPreparationCard(card: MilkPreparationCardUi, onClick: () -> Unit) {
    val stripe = when (card.bucket) {
        MilkPreparationCardBucket.COMPLETED -> MeshaColors.Ok
        MilkPreparationCardBucket.REWORK -> MeshaColors.Danger
        MilkPreparationCardBucket.IN_REVIEW -> MeshaColors.Warn
        MilkPreparationCardBucket.TO_PREPARE -> Color.Transparent
    }
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(androidx.compose.foundation.layout.IntrinsicSize.Min)
            .clip(RoundedCornerShape(16.dp)).background(MeshaColors.Surf)
            .clickable(enabled = card.canOpen, onClick = onClick),
    ) {
        Box(Modifier.width(4.dp).fillMaxHeight().background(stripe))
        Column(Modifier.weight(1f).padding(12.dp), verticalArrangement = Arrangement.spacedBy(9.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(Modifier.size(38.dp).clip(CircleShape).background(MeshaColors.BrandTint), contentAlignment = Alignment.Center) {
                    Icon(MeshaIcons.Feed, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(19.dp))
                }
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(card.parkLabel, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W800)
                    Text(card.detailLabel.ifBlank { "${card.parkLabel} · ${card.cohortCount} cohorts · ${card.headCount} animals" }, color = MeshaColors.Faint, fontSize = 11.sp)
                }
                Text(
                    if (card.isInProgress) "In progress" else card.statusLabel,
                    color = when {
                        card.isInProgress -> MeshaColors.BrandD
                        card.bucket == MilkPreparationCardBucket.COMPLETED -> MeshaColors.Ok
                        card.bucket == MilkPreparationCardBucket.REWORK -> MeshaColors.Danger
                        card.bucket == MilkPreparationCardBucket.IN_REVIEW -> MeshaColors.Warn
                        else -> MeshaColors.Muted
                    },
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W800,
                    modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(MeshaColors.Surf3)
                        .padding(horizontal = 8.dp, vertical = 3.dp),
                )
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                MilkMetric(card.totalMilkLabel, Modifier.weight(1f))
                MilkMetric(card.citricAcidLabel, Modifier.weight(1f))
            }
            Text(
                card.reworkReason.ifBlank { card.actionLabel },
                color = if (card.bucket == MilkPreparationCardBucket.REWORK) MeshaColors.Danger else MeshaColors.Ink,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
            )
        }
    }
}

@Composable
private fun MilkMetric(text: String, modifier: Modifier = Modifier) {
    Text(
        text,
        color = MeshaColors.Muted,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = modifier.clip(RoundedCornerShape(10.dp)).background(MeshaColors.Surf2)
            .padding(horizontal = 9.dp, vertical = 7.dp),
    )
}
