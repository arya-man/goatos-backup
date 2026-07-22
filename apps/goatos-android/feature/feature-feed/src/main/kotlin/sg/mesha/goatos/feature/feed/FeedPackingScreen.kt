package sg.mesha.goatos.feature.feed

// telemetry:exempt pure stateless renderer; FeedPackingViewModel (in :app) owns the feed_*
// AnalyticsEvents + CrashReporter wiring for every read refresh and filter change.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone

// ---------------------------------------------------------------------------
// UI models (feature-local; mapped from DTOs by the :app @HiltViewModel).
// ---------------------------------------------------------------------------

/** One shed/session packing line — the bag a packer fills. */
@Immutable
data class FeedPackingRowUi(
    val grainKey: String,
    val shedLabel: String,
    val sessionLabel: String,
    val workflow: String,
    val experimentArm: String,
    val headCount: Long,
    val items: List<FeedItemQtyUi>,
    val totalKg: String,
    /** "ready" | "blocked" | "empty" */
    val status: String,
)

@Immutable
data class FeedPackingSummaryUi(
    val shedCount: Int = 0,
    val lineCount: Int = 0,
    val blockedLineCount: Int = 0,
    val totalsByItem: List<FeedItemTotalUi> = emptyList(),
)

@Immutable
data class FeedPackingUiState(
    val title: String,
    val targetDateLabel: String = "",
    // Packing filters are farm + workflow only — the worklist endpoint has no shed filter (a packer
    // draws the whole park's bags), so the shed dropdown is deliberately absent here.
    val filters: FeedFilterUi = FeedFilterUi(),
    val summary: FeedPackingSummaryUi = FeedPackingSummaryUi(),
    val hasSummary: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface FeedPackingEvent {
    data object Refresh : FeedPackingEvent
    data class SelectPark(val parkId: String) : FeedPackingEvent

    /** "" (both), "normal", or "experiment". */
    data class SelectWorkflow(val workflow: String) : FeedPackingEvent

    /** The session_no to filter to; 0 = every session. */
    data class SelectSession(val sessionNo: Int) : FeedPackingEvent
    data object ClearFilters : FeedPackingEvent
}

@Composable
fun FeedPackingScreen(
    state: FeedPackingUiState,
    rows: LazyPagingItems<FeedPackingRowUi>,
    onEvent: (FeedPackingEvent) -> Unit = {},
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
            onRefresh = { onEvent(FeedPackingEvent.Refresh) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "filters") { FeedPackingFilterBar(state.filters, onEvent) }
            item(key = "summary") { FeedPackingSummaryCard(state.summary) }
            item(key = "caption") { FeedSectionCaption(stringResource(R.string.feed_packing_caption)) }

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
                rows[index]?.let { row -> FeedPackingRowCard(row) }
            }
        }
    }
}

@Composable
private fun FeedPackingFilterBar(filters: FeedFilterUi, onEvent: (FeedPackingEvent) -> Unit) {
    val bothWorkflows = stringResource(R.string.feed_filter_workflow_all)
    val allSessions = stringResource(R.string.feed_filter_all_sessions)
    val normalLabel = stringResource(R.string.feed_workflow_normal)
    val experimentLabel = stringResource(R.string.feed_workflow_experiment)
    val hasActive = filters.workflow.isNotBlank() || filters.selectedSessionNo != 0

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
                        .clickable { onEvent(FeedPackingEvent.ClearFilters) }
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
                onSelect = { onEvent(FeedPackingEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
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
                onSelect = { onEvent(FeedPackingEvent.SelectWorkflow(it)) },
                enabled = true,
                modifier = Modifier.weight(1f),
            )
            FeedSessionDropdown(
                filters = filters,
                allSessions = allSessions,
                onSelect = { onEvent(FeedPackingEvent.SelectSession(it)) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun FeedPackingSummaryCard(summary: FeedPackingSummaryUi) {
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
                label = stringResource(R.string.feed_stat_lines),
                value = summary.lineCount.toString(),
                accent = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_stat_blocked),
                value = summary.blockedLineCount.toString(),
                accent = if (summary.blockedLineCount > 0) MeshaColors.Warn else MeshaColors.Teal,
                modifier = Modifier.weight(1f),
            )
        }
        summary.totalsByItem.filter { it.quantityKg.toKgOrZero() != 0.0 || it.blockedCells > 0 }.forEach { total ->
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
private fun FeedPackingRowCard(row: FeedPackingRowUi) {
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
            add(row.sessionLabel)
            if (row.experimentArm.isNotBlank()) add(row.experimentArm)
        }.joinToString(" · ")
        Text(text = subtitle, color = MeshaColors.Muted, fontSize = 12.sp)
        // Hide 0-kg lines (nothing to pack for that item here); a BLOCKED line is not zero and stays.
        row.items.filter { it.blocked || it.quantityKg.toKgOrZero() != 0.0 }.forEach { item -> FeedItemQtyRow(item) }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(text = stringResource(R.string.feed_pack_total), color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W700, modifier = Modifier.weight(1f))
            if (row.status == "blocked") {
                Text(text = stringResource(R.string.feed_blocked_label), color = MeshaColors.Danger, fontSize = 12.sp, fontWeight = FontWeight.W700)
            } else {
                Text(text = stringResource(R.string.feed_kg_fmt, row.totalKg), color = MeshaColors.BrandD, fontSize = 13.sp, fontWeight = FontWeight.W800)
            }
        }
    }
}
