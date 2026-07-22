package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; AwaitingRfidViewModel (in :app) owns the
// counts_awaiting_rfid_* AnalyticsEvents + the CrashReporter non-fatal on every page-load failure.

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
 * The "Awaiting RFID" list — goats that were tagged at birth with a provisional temporary tag and
 * have not yet been promoted to a permanent RFID (`GET /app/counts/goats/temporary-tagged`).
 *
 * Read screen, offline-first (docs/decisions/android-offline-first.md): rows are a bounded Room-
 * backed Paging window (~20/page keyset), so re-entering the list renders the cached rows immediately
 * behind a sync indicator. Tapping a row opens the L2 promote screen where the operator assigns the
 * permanent RFID — only THAT retags the animal.
 */

/** One goat awaiting a permanent RFID. Every field is backend-owned; the screen renders, never derives. */
@Immutable
data class AwaitingRfidRowUi(
    val goatId: String,
    val displayId: String,
    val temporaryIdentifier: String,
    val locationDisplay: String,
)

@Immutable
data class AwaitingRfidUiState(
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    // Location filter (park -> shed cascade) — the same backend-owned destinations catalog the
    // birth/shifting pickers use. Ids submit; names render. Empty = unfiltered on that dimension.
    val destinationParks: List<ShiftingParkUi> = emptyList(),
    val parkId: String = "",
    val shedId: String = "",
) {
    val hasLocationFilter: Boolean get() = parkId.isNotBlank() || shedId.isNotBlank()

    /** The sheds of the currently chosen park — the second filter dropdown's whole option set. */
    val shedsForSelectedPark: List<ShiftingShedUi>
        get() = destinationParks.firstOrNull { it.parkId == parkId }?.sheds.orEmpty()
}

sealed interface AwaitingRfidEvent {
    data object Back : AwaitingRfidEvent
    data class OpenGoat(val goatId: String) : AwaitingRfidEvent

    // Location filter — choosing a park resets the shed (a shed belongs to exactly one park).
    data class SelectPark(val parkId: String) : AwaitingRfidEvent
    data class SelectShed(val shedId: String) : AwaitingRfidEvent
    data object ClearFilter : AwaitingRfidEvent
}

@Composable
fun AwaitingRfidScreen(
    state: AwaitingRfidUiState,
    rows: LazyPagingItems<AwaitingRfidRowUi>,
    onEvent: (AwaitingRfidEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(
            title = "Awaiting RFID",
            subtitle = "Goats waiting for a permanent tag",
            onBack = { onEvent(AwaitingRfidEvent.Back) },
        )
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
            item(key = "filter") { AwaitingRfidFilterBar(state, onEvent) }

            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.CheckCircle,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.goatId }) { index ->
                rows[index]?.let { row ->
                    AwaitingRfidRowCard(row) { onEvent(AwaitingRfidEvent.OpenGoat(row.goatId)) }
                }
            }
        }
    }
}

/**
 * Location filter (park -> shed cascade). The vocabulary is the backend-owned shifting-destinations
 * catalog — the same one the birth/shifting pickers use — so this never invents parks or sheds. Ids
 * submit; names render. Choosing a park resets the shed. A "Clear" affordance appears only while a
 * filter is active. Filtering is a server-side narrow: the list re-fetches for the chosen location.
 */
@Composable
private fun AwaitingRfidFilterBar(state: AwaitingRfidUiState, onEvent: (AwaitingRfidEvent) -> Unit) {
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
                text = "Filter by location",
                color = MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            if (state.hasLocationFilter) {
                Text(
                    text = "Clear",
                    color = MeshaColors.BrandD,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onEvent(AwaitingRfidEvent.ClearFilter) }
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            val selectedPark = state.destinationParks.firstOrNull { it.parkId == state.parkId }
            CountsDropdownField(
                label = "Park",
                selectedLabel = selectedPark?.name,
                placeholder = "All parks",
                options = state.destinationParks.map { CountsDropdownOption(it.parkId, it.name) },
                onSelect = { onEvent(AwaitingRfidEvent.SelectPark(it)) },
                // Disabled until the catalog is cached so the operator can't open an empty menu.
                enabled = state.destinationParks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
            val sheds = state.shedsForSelectedPark
            val selectedShed = sheds.firstOrNull { it.shedId == state.shedId }
            CountsDropdownField(
                label = "Shed",
                selectedLabel = selectedShed?.name,
                placeholder = if (state.parkId.isBlank()) "Choose a park first" else "All sheds",
                options = sheds.map { CountsDropdownOption(it.shedId, it.name) },
                onSelect = { onEvent(AwaitingRfidEvent.SelectShed(it)) },
                enabled = sheds.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun AwaitingRfidRowCard(row: AwaitingRfidRowUi, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(row.displayId, color = MeshaColors.Ink, fontSize = 14.sp, fontWeight = FontWeight.W700)
                if (row.locationDisplay.isNotBlank()) {
                    Text(row.locationDisplay, color = MeshaColors.Muted, fontSize = 12.sp)
                }
            }
            AwaitingRfidTempPill(row.temporaryIdentifier)
        }
        Icon(
            imageVector = MeshaIcons.Chevron,
            contentDescription = null,
            tint = MeshaColors.Muted,
            modifier = Modifier.size(18.dp),
        )
    }
}

@Composable
private fun AwaitingRfidTempPill(temporaryIdentifier: String) {
    if (temporaryIdentifier.isBlank()) return
    Text(
        text = "Temp tag · $temporaryIdentifier",
        color = MeshaColors.Warn,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(MeshaColors.WarnX)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}
