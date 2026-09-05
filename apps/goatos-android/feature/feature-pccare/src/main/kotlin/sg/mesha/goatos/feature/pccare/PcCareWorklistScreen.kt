package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderer; PcCareWorklistViewModel (in :app) owns the pc_care_*
// AnalyticsEvents + CrashReporter wiring for every read refresh and row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
 * The per-category PC Care worklist — ONE composable backing the four L0 tabs (deworming, ticks
 * removal, hoof trimming, hair trimming); the route decides the category the ViewModel serves.
 *
 * Paged (~20 rows) with stable task-id keys and a PASSIVE loading footer — never a "Load more"
 * button (docs/decisions/mobile-data-fetch-anti-patterns.md).
 */
@Composable
fun PcCareWorklistScreen(
    state: PcCareWorklistUiState,
    rows: LazyPagingItems<PcCareTaskCardUi>,
    onEvent: (PcCareWorklistEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Refresh-on-open (docs/decisions/android-offline-first.md): cached Room rows show instantly
    // and a background refresh fires on every resume — including popping back here after a submit,
    // so the card flips to "In review" without a manual refresh.
    RefreshOnResume { onEvent(PcCareWorklistEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            eyebrow = state.moduleLabel,
            eyebrowColor = MeshaColors.BrandD,
            subtitle = if (state.showDateBar) {
                (pcCareFriendlyDate(state.dateLabel) ?: state.dateLabel).takeIf { it.isNotBlank() }
            } else {
                null
            },
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
                    onSync = { onEvent(PcCareWorklistEvent.Refresh) },
                )
            },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.showDateBar) {
                item(key = "date_bar") {
                    PcCareDateBar(
                        selectedDateIso = state.dateLabel,
                        onSelectDate = { onEvent(PcCareWorklistEvent.SelectDate(it)) },
                    )
                }
            }

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
                    PcCareTaskCard(card) { onEvent(PcCareWorklistEvent.OpenTask(card.taskId)) }
                }
            }

            // Passive loading footer: the next page is already in flight while this spins.
            if (rows.loadState.append is LoadState.Loading) {
                item(key = "loading_footer") {
                    Box(modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator(modifier = Modifier, color = MeshaColors.BrandD)
                    }
                }
            }
        }
    }
}

@Composable
internal fun PcCareTaskCard(
    card: PcCareTaskCardUi,
    onClose: (() -> Unit)? = null,
    onOpen: () -> Unit,
) {
    Column(
        modifier = pcCareCardModifier(enabled = card.openable, onClick = onOpen.takeIf { card.openable }),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            // The backend-composed pen display, rendered verbatim.
            Text(
                text = card.locationDisplay,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                modifier = Modifier.weight(1f),
            )
            PcCareStatusChip(label = card.statusLabel, tone = card.statusTone)
        }
        val subtitle = buildList {
            if (card.parkLabel.isNotBlank()) add(card.parkLabel)
            if (card.dueDateLabel.isNotBlank()) add("Due ${card.dueDateLabel}")
            if (card.animalCountLabel.isNotBlank()) add(card.animalCountLabel)
        }.joinToString(" · ")
        if (subtitle.isNotBlank()) {
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        if (card.assigneeLine.isNotBlank()) {
            Text(text = card.assigneeLine, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        if (card.inventoryRequirements.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                card.inventoryRequirements.forEach { requirement ->
                    PcCareDoseChip(requirement)
                }
            }
        }
        // Why this task is back. Backend-owned sentence, rendered verbatim — the chip alone
        // cannot say why.
        if (card.reworkReason.isNotBlank()) {
            Text(
                text = card.reworkReason,
                color = MeshaColors.Danger,
                style = MeshaType.caption,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (onClose != null && card.closable) {
            Text(
                text = "End this work",
                color = MeshaColors.Danger,
                style = MeshaType.pillStrong,
                modifier = pcCareInlineActionModifier(onClose),
            )
        }
    }
}

@Composable
private fun PcCareDoseChip(requirement: PcCareInventoryRequirementUi) {
    Row(
        modifier = Modifier
            .background(MeshaColors.BrandTint, shape = androidx.compose.foundation.shape.RoundedCornerShape(999.dp))
            .padding(horizontal = 10.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = requirement.vaccineLabel, color = MeshaColors.BrandD, style = MeshaType.pillStrong)
        Text(text = requirement.requiredDosesLabel, color = MeshaColors.Muted, style = MeshaType.pill)
    }
}
