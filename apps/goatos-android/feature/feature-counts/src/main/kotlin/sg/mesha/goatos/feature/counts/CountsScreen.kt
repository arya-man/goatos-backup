package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer with no data access of its own; CountsViewModel owns
// the counts_* analytics events and the CrashReporter non-fatal on every refresh/page failure.

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
import androidx.compose.foundation.lazy.items
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
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * Counts — the census read screen (`/counts`), an L0 root destination.
 *
 * A stateless renderer, per the golden frontend rule (AGENTS.md): every visible number, label,
 * and row here is a field on [CountsUiState] filled from the backend contract. This composable
 * never checks a role, never derives a total, and never re-aggregates the page it was handed.
 *
 * Two data shapes arrive separately and deliberately:
 *  - [CountsUiState.totals] are the backend's whole-result rollups over the FULL filtered set.
 *    They are NOT summed from [rows] — a KPI computed from the ~20 rows currently paged into
 *    memory would silently under-report the herd.
 *  - [rows] is a Paging window. Both the network fetch and the Room read behind it are bounded to
 *    one screen-page, so the list can grow to any cohort size without ever materializing it.
 */

// ---------------------------------------------------------------------------
// State + events
// ---------------------------------------------------------------------------

/** One aggregated census grain, exactly as the backend grouped it. */
@Immutable
data class CountsBreakdownRowUi(
    val grainKey: String,
    val farmLabel: String,
    val shedLabel: String,
    val managementStage: String,
    val breed: String,
    val sex: String,
    val count: Int,
)

/**
 * Whole-result census KPIs. [projectedAt] is the backend's own freshness stamp for the
 * aggregate — distinct from [CountsUiState.lastSyncedAt], which is when THIS DEVICE last synced.
 */
@Immutable
data class CountsTotalsUi(
    val totalCount: Int = 0,
    val totalKids: Int = 0,
    val totalAdults: Int = 0,
    val totalRows: Int = 0,
    val projectedAt: String = "",
)

@Immutable
data class CountsUiState(
    val title: String,
    val scopeLabel: String = "",
    val totals: CountsTotalsUi = CountsTotalsUi(),
    val hasTotals: Boolean = false,
    /** Honest zero/error copy shown when there is nothing cached AND nothing served. */
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    // Offline-first sync state (docs/decisions/android-offline-first.md). These describe the
    // background refresh running OVER already-rendered cached data; they never gate whether the
    // cached content below renders.
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface CountsEvent {
    data object Refresh : CountsEvent
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun CountsScreen(
    state: CountsUiState,
    rows: LazyPagingItems<CountsBreakdownRowUi>,
    onEvent: (CountsEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        CountsHeader(state = state, onRefresh = { onEvent(CountsEvent.Refresh) })
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "totals") { CountsTotalsCard(state) }
            item(key = "caption") {
                SectionCaption(stringResource(R.string.counts_breakdown_caption))
            }

            // A cold cache with a failed first load is an honest error state; a cold cache with a
            // successful empty response is an honest zero state. Neither is a blank wall, and
            // neither is shown while cached rows exist.
            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Goat,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            // Paging handles prefetch itself (PagingConfig.prefetchDistance) — there is no manual
            // "load more" button and no index arithmetic here. `itemKey` gives every row a stable
            // business identity so a re-page never reorders or duplicates a card.
            items(
                count = rows.itemCount,
                key = rows.itemKey { it.grainKey },
            ) { index ->
                rows[index]?.let { row -> CountsRowCard(row) }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Header + cards
// ---------------------------------------------------------------------------

/**
 * Counts landing header.
 *
 * Renders through the shared [MeshaScreenHeader] so `/counts` gets the same L0 chrome every other
 * root destination gets — in particular the module drawer, which this screen previously had no
 * affordance for at all, stranding an operator inside Counts with no way back to another module.
 * The leading button is resolved by the shell from backend-composed L0 membership, never here.
 *
 * The screen keeps what is genuinely its own: the Refresh action and the offline-first freshness
 * line ("Updated Nm ago" / "Syncing…" / "Offline · updated Nm ago").
 */
@Composable
private fun CountsHeader(state: CountsUiState, onRefresh: () -> Unit) {
    MeshaScreenHeader(
        // Backend-owned copy: both render verbatim from the counts contract.
        title = state.title,
        subtitle = state.scopeLabel.takeIf { it.isNotBlank() },
        below = {
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                hasData = state.hasTotals,
                isOffline = state.isOffline,
            )
        },
        actions = {
            Box(
                modifier = Modifier
                    .size(38.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                    .clickable(enabled = !state.isRefreshing, onClick = onRefresh),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Refresh,
                    contentDescription = stringResource(R.string.counts_refresh_description),
                    tint = MeshaColors.Muted,
                    modifier = Modifier.size(18.dp),
                )
            }
        },
    )
}

@Composable
private fun CountsTotalsCard(state: CountsUiState) {
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
            text = stringResource(R.string.counts_totals_title),
            color = MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
            StatTile(
                label = stringResource(R.string.counts_stat_total),
                value = state.totals.totalCount.toString(),
                accent = MeshaColors.BrandD,
                modifier = Modifier.weight(1f),
            )
            StatTile(
                label = stringResource(R.string.counts_stat_adults),
                value = state.totals.totalAdults.toString(),
                accent = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            StatTile(
                label = stringResource(R.string.counts_stat_kids),
                value = state.totals.totalKids.toString(),
                accent = MeshaColors.Teal,
                modifier = Modifier.weight(1f),
            )
        }
        Text(
            text = stringResource(R.string.counts_grain_fmt, state.totals.totalRows),
            color = MeshaColors.Faint,
            fontSize = 11.sp,
        )
    }
}

@Composable
private fun StatTile(label: String, value: String, accent: androidx.compose.ui.graphics.Color, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(vertical = 10.dp, horizontal = 12.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(text = value, color = accent, fontSize = 22.sp, fontWeight = FontWeight.W800)
        Text(text = label, color = MeshaColors.Muted, fontSize = 11.sp)
    }
}

@Composable
private fun CountsRowCard(row: CountsBreakdownRowUi) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            // Location (farm · shed) leads: an operator scans this list by WHERE the
            // animals are, so it carries the primary weight. Management stage is the
            // qualifier underneath it.
            Text(
                text = listOf(row.farmLabel, row.shedLabel)
                    .filter { it.isNotBlank() }
                    .joinToString(" · "),
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W700,
            )
            Text(
                // Raw management_stage text: free-form source data with no controlled
                // vocabulary, rendered verbatim rather than normalized on device.
                text = row.managementStage.ifBlank { stringResource(R.string.counts_stage_unknown) },
                color = MeshaColors.Muted,
                fontSize = 12.sp,
            )
            Text(
                text = listOf(row.breed, row.sex).filter { it.isNotBlank() }.joinToString(" · "),
                color = MeshaColors.Faint,
                fontSize = 11.sp,
            )
        }
        Text(
            text = row.count.toString(),
            color = MeshaColors.BrandD,
            fontSize = 18.sp,
            fontWeight = FontWeight.W800,
        )
    }
}

@Composable
private fun SectionCaption(text: String) {
    Text(
        text = text,
        color = MeshaColors.Faint,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier.padding(horizontal = 16.dp, vertical = 2.dp),
    )
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

@Preview(showBackground = true, backgroundColor = 0xFF0A0F0C)
@Composable
private fun CountsRowPreview() {
    GoatOsTheme {
        CountsRowCard(
            CountsBreakdownRowUi(
                grainKey = "preview",
                farmLabel = "CBE",
                shedLabel = "Shed 4",
                managementStage = "Pregnant",
                breed = "Boer",
                sex = "female",
                count = 42,
            ),
        )
    }
}
