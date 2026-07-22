package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; ShiftingPendingViewModel (in :app) owns the
// counts_shifting_pending_* AnalyticsEvents + the CrashReporter non-fatal on every page-load failure.

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
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The Shifting "Pending" tab — the operator's queue of movements APPROVED in web
 * (event_status='authorized') and waiting to be physically walked.
 *
 * Read screen, offline-first (docs/decisions/android-offline-first.md): rows are a bounded Room-
 * backed Paging window (~20/page keyset), the summary of the queue is the backend's own, and the
 * farm -> shed filter narrows by the movement's SOURCE location (where the animals stand now).
 * Tapping a row opens the L1 execute screen where the operator does the move, optionally records a
 * video, and presses "Mark done" — only THAT completes the movement.
 */

/** One authorized movement row. Every field is backend-owned; the screen renders, never derives. */
@Immutable
data class ShiftingPendingRowUi(
    val shiftingEventId: String,
    val sourceLabel: String,
    val destinationLabel: String,
    val priority: String,
    val category: String,
    val animalCount: Int,
    val approvedAtLabel: String,
)

/** A dropdown option for the farm/shed filter. Identity is [key]; [label] is display only. */
@Immutable
data class ShiftingPendingFilterOption(val key: String, val label: String)

/** The farm -> shed cascade filter state. Shed is enabled only once a park is chosen. */
@Immutable
data class ShiftingPendingFilterUi(
    val parks: List<ShiftingPendingFilterOption> = emptyList(),
    val selectedParkId: String = "",
    val selectedParkLabel: String? = null,
    val sheds: List<ShiftingPendingFilterOption> = emptyList(),
    val selectedShedId: String = "",
    val selectedShedLabel: String? = null,
) {
    val isShedFilterEnabled: Boolean get() = selectedParkId.isNotBlank() && sheds.isNotEmpty()
    val hasActive: Boolean get() = selectedParkId.isNotBlank() || selectedShedId.isNotBlank()
}

@Immutable
data class ShiftingPendingUiState(
    val filters: ShiftingPendingFilterUi = ShiftingPendingFilterUi(),
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface ShiftingPendingEvent {
    data object Refresh : ShiftingPendingEvent
    data class SelectPark(val parkId: String) : ShiftingPendingEvent
    data class SelectShed(val shedId: String) : ShiftingPendingEvent
    data object ClearFilters : ShiftingPendingEvent
    data class OpenMovement(val shiftingEventId: String) : ShiftingPendingEvent
}

// ---------------------------------------------------------------------------
// Tab host: Raise | Pending
// ---------------------------------------------------------------------------

enum class ShiftingTab { RAISE, PENDING }

/**
 * The Shifting home: two client-local tabs at the top, "Raise" and "Pending". Switching tabs is
 * local UI state, NOT navigation — both tab bodies live inside this one L0 route; only opening a
 * pending movement (the execute screen) is a hosted destination.
 */
@Composable
fun ShiftingHomeScreen(
    selectedTab: ShiftingTab,
    onSelectTab: (ShiftingTab) -> Unit,
    onBack: () -> Unit,
    raiseContent: @Composable () -> Unit,
    pendingContent: @Composable () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(title = "Shifting", subtitle = "Move animals between sheds", onBack = onBack)
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 10.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(MeshaColors.Surf3)
                .padding(4.dp),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            ShiftingTabChip("Raise", selectedTab == ShiftingTab.RAISE, Modifier.weight(1f)) {
                onSelectTab(ShiftingTab.RAISE)
            }
            ShiftingTabChip("Pending", selectedTab == ShiftingTab.PENDING, Modifier.weight(1f)) {
                onSelectTab(ShiftingTab.PENDING)
            }
        }
        Box(modifier = Modifier.fillMaxSize()) {
            when (selectedTab) {
                ShiftingTab.RAISE -> raiseContent()
                ShiftingTab.PENDING -> pendingContent()
            }
        }
    }
}

@Composable
private fun ShiftingTabChip(label: String, selected: Boolean, modifier: Modifier, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (selected) MeshaColors.Ink else MeshaColors.Muted,
        fontSize = 14.sp,
        fontWeight = if (selected) FontWeight.W800 else FontWeight.W600,
        modifier = modifier
            .clip(RoundedCornerShape(9.dp))
            .background(if (selected) MeshaColors.Surf else MeshaColors.Surf3)
            .clickable(onClick = onClick)
            .minimumInteractiveComponentSize()
            .padding(vertical = 9.dp),
        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
    )
}

// ---------------------------------------------------------------------------
// Pending list
// ---------------------------------------------------------------------------

@Composable
fun ShiftingPendingScreen(
    state: ShiftingPendingUiState,
    rows: LazyPagingItems<ShiftingPendingRowUi>,
    onEvent: (ShiftingPendingEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize()) {
        SyncStatusIndicator(
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = rows.itemCount > 0,
            isOffline = state.isOffline,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "filters") { ShiftingPendingFilterBar(state.filters, onEvent) }

            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.ArrowUpDown,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.shiftingEventId }) { index ->
                rows[index]?.let { row ->
                    ShiftingPendingRowCard(row) { onEvent(ShiftingPendingEvent.OpenMovement(row.shiftingEventId)) }
                }
            }
        }
    }
}

@Composable
private fun ShiftingPendingFilterBar(filters: ShiftingPendingFilterUi, onEvent: (ShiftingPendingEvent) -> Unit) {
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
                text = "Filter",
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            if (filters.hasActive) {
                Text(
                    text = "Clear",
                    color = MeshaColors.BrandD,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onEvent(ShiftingPendingEvent.ClearFilters) }
                        .minimumInteractiveComponentSize()
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            CountsDropdownField(
                label = "Farm",
                selectedLabel = filters.selectedParkLabel,
                placeholder = "All farms",
                options = listOf(CountsDropdownOption(key = "", label = "All farms")) +
                    filters.parks.map { CountsDropdownOption(it.key, it.label) },
                onSelect = { onEvent(ShiftingPendingEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
            CountsDropdownField(
                label = "Shed",
                selectedLabel = filters.selectedShedLabel,
                placeholder = if (filters.selectedParkId.isBlank()) "Pick a farm first" else "All sheds",
                options = listOf(CountsDropdownOption(key = "", label = "All sheds")) +
                    filters.sheds.map { CountsDropdownOption(it.key, it.label) },
                onSelect = { onEvent(ShiftingPendingEvent.SelectShed(it)) },
                enabled = filters.isShedFilterEnabled,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun ShiftingPendingRowCard(row: ShiftingPendingRowUi, onClick: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = row.sourceLabel,
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            Icon(
                imageVector = MeshaIcons.ArrowUpDown,
                contentDescription = "to",
                tint = MeshaColors.Muted,
                modifier = Modifier.size(16.dp).padding(horizontal = 2.dp),
            )
            Text(
                text = row.destinationLabel,
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            ShiftingPill(row.priority, MeshaColors.WarnX, MeshaColors.Warn)
            ShiftingPill(row.category, MeshaColors.OkX, MeshaColors.Ok)
            ShiftingPill("${row.animalCount} animal${if (row.animalCount == 1) "" else "s"}", MeshaColors.Surf3, MeshaColors.Muted)
        }
        if (row.approvedAtLabel.isNotBlank()) {
            Text(text = "Approved ${row.approvedAtLabel}", color = MeshaColors.Faint, fontSize = 11.sp)
        }
    }
}

@Composable
private fun ShiftingPill(text: String, bg: androidx.compose.ui.graphics.Color, fg: androidx.compose.ui.graphics.Color) {
    if (text.isBlank()) return
    Text(
        text = text,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}
