package sg.mesha.goatos.feature.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.SyncStatusIndicator

// ---------------------------------------------------------------------------
// UI models (feature-local; the @HiltViewModel in :app maps the DTOs onto these, so this module
// stays free of core-network per the feature-* -> core-* only rule).
// ---------------------------------------------------------------------------

/** One feed item's quantity on a row. [quantityKg] is null iff [blocked] — a blocked ration has no
 *  number and must be rendered as blocked, never as 0 (the backend's pointer-is-the-contract rule). */
@Immutable
data class FeedItemQtyUi(
    val feedItem: String,
    val quantityKg: String?,
    val blocked: Boolean,
    val blockedReason: String,
)

/** One generated feed instruction: a shed's ration in one session. */
@Immutable
data class FeedDirectionRowUi(
    val grainKey: String,
    val shedLabel: String,
    val shedTag: String,
    val breed: String,
    val rationGroup: String,
    val experimentArm: String,
    val sessionLabel: String,
    val headCount: Long,
    val headCountInformational: Boolean,
    val workflow: String,
    val items: List<FeedItemQtyUi>,
    val sessionTotalKg: String,
    val blocked: Boolean,
    val overduePending: Boolean,
)

/** One feed item's whole-scope total. */
@Immutable
data class FeedItemTotalUi(val feedItem: String, val quantityKg: String, val blockedCells: Int)

@Immutable
data class FeedDirectionSummaryUi(
    val shedCount: Int = 0,
    val rowCount: Int = 0,
    val blockedCount: Int = 0,
    val totalsByItem: List<FeedItemTotalUi> = emptyList(),
)

/** A single dropdown filter's rendered state. */
@Immutable
data class FeedFilterUi(
    val parks: List<FeedDropdownOption> = emptyList(),
    val selectedParkId: String = "",
    val selectedParkLabel: String? = null,
    val sheds: List<FeedDropdownOption> = emptyList(),
    val selectedShedId: String = "",
    val selectedShedLabel: String? = null,
    /** "" (both), "normal", or "experiment". */
    val workflow: String = "",
) {
    val isShedFilterEnabled: Boolean get() = selectedParkId.isNotBlank() && sheds.isNotEmpty()
}

@Immutable
data class FeedDirectionUiState(
    val title: String,
    val targetDateLabel: String = "",
    val filters: FeedFilterUi = FeedFilterUi(),
    val summary: FeedDirectionSummaryUi = FeedDirectionSummaryUi(),
    val hasSummary: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface FeedDirectionEvent {
    data object Refresh : FeedDirectionEvent
    data class SelectPark(val parkId: String) : FeedDirectionEvent
    data class SelectShed(val shedId: String) : FeedDirectionEvent

    /** "" (both), "normal", or "experiment". */
    data class SelectWorkflow(val workflow: String) : FeedDirectionEvent
    data object ClearFilters : FeedDirectionEvent
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun FeedDirectionScreen(
    state: FeedDirectionUiState,
    rows: LazyPagingItems<FeedDirectionRowUi>,
    onEvent: (FeedDirectionEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        FeedHeader(
            title = state.title,
            subtitle = state.targetDateLabel,
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.hasSummary,
            isOffline = state.isOffline,
            onRefresh = { onEvent(FeedDirectionEvent.Refresh) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "filters") {
                FeedDirectionFilterBar(state.filters, onEvent)
            }
            item(key = "summary") { FeedDirectionSummaryCard(state.summary) }
            item(key = "caption") { FeedSectionCaption(stringResource(R.string.feed_direction_caption)) }

            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Feed,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.grainKey }) { index ->
                rows[index]?.let { row -> FeedDirectionRowCard(row) }
            }
        }
    }
}

@Composable
internal fun FeedHeader(
    title: String,
    subtitle: String,
    isRefreshing: Boolean,
    lastSyncedAt: Long?,
    hasData: Boolean,
    isOffline: Boolean,
    onRefresh: () -> Unit,
) {
    MeshaScreenHeader(
        title = title,
        subtitle = subtitle.takeIf { it.isNotBlank() },
        below = {
            SyncStatusIndicator(
                isRefreshing = isRefreshing,
                lastSyncedAt = lastSyncedAt,
                hasData = hasData,
                isOffline = isOffline,
            )
        },
        actions = {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                    .clickable(enabled = !isRefreshing, onClick = onRefresh),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Refresh,
                    contentDescription = stringResource(R.string.feed_refresh_description),
                    tint = MeshaColors.Muted,
                    modifier = Modifier.size(18.dp),
                )
            }
        },
    )
}

@Composable
private fun FeedDirectionFilterBar(filters: FeedFilterUi, onEvent: (FeedDirectionEvent) -> Unit) {
    val allSheds = stringResource(R.string.feed_filter_all_sheds)
    val bothWorkflows = stringResource(R.string.feed_filter_workflow_all)
    val normalLabel = stringResource(R.string.feed_workflow_normal)
    val experimentLabel = stringResource(R.string.feed_workflow_experiment)
    val hasActive = filters.selectedShedId.isNotBlank() || filters.workflow.isNotBlank()

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = stringResource(R.string.feed_filters_title),
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            if (hasActive) {
                Text(
                    text = stringResource(R.string.feed_filters_clear),
                    color = MeshaColors.BrandD,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onEvent(FeedDirectionEvent.ClearFilters) }
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            FeedDropdownField(
                label = stringResource(R.string.feed_filter_farm),
                selectedLabel = filters.selectedParkLabel,
                placeholder = stringResource(R.string.feed_filter_farm),
                options = filters.parks,
                onSelect = { onEvent(FeedDirectionEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
            FeedDropdownField(
                label = stringResource(R.string.feed_filter_shed),
                selectedLabel = filters.selectedShedLabel,
                placeholder = if (filters.selectedParkId.isBlank()) {
                    stringResource(R.string.feed_filter_park_first)
                } else {
                    allSheds
                },
                options = listOf(FeedDropdownOption(key = "", label = allSheds)) + filters.sheds,
                onSelect = { onEvent(FeedDirectionEvent.SelectShed(it)) },
                enabled = filters.isShedFilterEnabled,
                modifier = Modifier.weight(1f),
            )
        }
        FeedDropdownField(
            label = stringResource(R.string.feed_filter_workflow),
            selectedLabel = when (filters.workflow) {
                "normal" -> normalLabel
                "experiment" -> experimentLabel
                else -> null
            },
            placeholder = bothWorkflows,
            options = listOf(
                FeedDropdownOption(key = "", label = bothWorkflows),
                FeedDropdownOption(key = "normal", label = normalLabel),
                FeedDropdownOption(key = "experiment", label = experimentLabel),
            ),
            onSelect = { onEvent(FeedDirectionEvent.SelectWorkflow(it)) },
            enabled = true,
        )
    }
}

@Composable
private fun FeedDirectionSummaryCard(summary: FeedDirectionSummaryUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = stringResource(R.string.feed_totals_title),
            color = MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
            FeedStatTile(
                label = stringResource(R.string.feed_stat_sheds),
                value = summary.shedCount.toString(),
                accent = MeshaColors.BrandD,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_stat_rows),
                value = summary.rowCount.toString(),
                accent = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_stat_blocked),
                value = summary.blockedCount.toString(),
                accent = if (summary.blockedCount > 0) MeshaColors.Warn else MeshaColors.Teal,
                modifier = Modifier.weight(1f),
            )
        }
        // Whole-scope kg per feed item (backend rollup, never re-summed from the paged rows).
        summary.totalsByItem.forEach { total ->
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(text = total.feedItem, color = MeshaColors.Muted, fontSize = 12.sp, modifier = Modifier.weight(1f))
                Text(
                    text = stringResource(R.string.feed_kg_fmt, total.quantityKg),
                    color = MeshaColors.Ink,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                )
            }
        }
    }
}

@Composable
private fun FeedDirectionRowCard(row: FeedDirectionRowUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(text = row.shedLabel, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
            FeedWorkflowChip(row.workflow)
        }
        val subtitle = buildList {
            if (row.shedTag.isNotBlank()) add(row.shedTag)
            if (row.breed.isNotBlank()) add(row.breed)
            if (row.experimentArm.isNotBlank()) add(row.experimentArm)
            add(row.sessionLabel)
        }.joinToString(" · ")
        Text(text = subtitle, color = MeshaColors.Muted, fontSize = 12.sp)
        Text(
            text = stringResource(R.string.feed_head_count_fmt, row.headCount),
            color = MeshaColors.Faint,
            fontSize = 11.sp,
        )
        row.items.forEach { item -> FeedItemQtyRow(item) }
        if (!row.blocked) {
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(text = stringResource(R.string.feed_session_total), color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
                Text(text = stringResource(R.string.feed_kg_fmt, row.sessionTotalKg), color = MeshaColors.BrandD, fontSize = 13.sp, fontWeight = FontWeight.W800)
            }
        }
    }
}

@Composable
internal fun FeedItemQtyRow(item: FeedItemQtyUi) {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(text = item.feedItem, color = MeshaColors.Ink, fontSize = 13.sp, modifier = Modifier.weight(1f))
        if (item.blocked) {
            Text(
                text = stringResource(R.string.feed_blocked_label),
                color = MeshaColors.Danger,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
            )
        } else {
            Text(
                text = stringResource(R.string.feed_kg_fmt, item.quantityKg ?: "0"),
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
        }
    }
    if (item.blocked && item.blockedReason.isNotBlank()) {
        Text(text = item.blockedReason, color = MeshaColors.Warn, fontSize = 11.sp)
    }
}

@Composable
internal fun FeedWorkflowChip(workflow: String) {
    val isExperiment = workflow == "experiment"
    val label = stringResource(if (isExperiment) R.string.feed_workflow_experiment else R.string.feed_workflow_normal)
    val bg = if (isExperiment) MeshaColors.TealX else MeshaColors.OkX
    val fg = if (isExperiment) MeshaColors.Teal else MeshaColors.Ok
    Text(
        text = label,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}
