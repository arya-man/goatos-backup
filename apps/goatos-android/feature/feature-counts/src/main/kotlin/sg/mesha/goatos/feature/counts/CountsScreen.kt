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
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
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
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
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
    /**
     * The shed's own id, used ONLY to look up its backend-computed subtotal in
     * [CountsFiltersUi.sheds] — never re-derived by summing paged rows sharing this shed. Blank
     * for the "no shed assigned" bucket, matching that facet's own null-key convention.
     */
    val shedId: String = "",
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

/**
 * One selectable value in a census filter dropdown, straight from the backend's `facets`.
 *
 * [key] is what the filter SENDS (a park/shed uuid, or the breed's own value) and [label] is only
 * ever rendered — the two are never interchangeable. [count] is the facet's own head count for
 * this value, shown next to the option so an operator can see the size of a cohort before
 * selecting it. Nothing here is invented on device: the whole vocabulary is backend-owned.
 */
@Immutable
data class CountsFilterOptionUi(
    val key: String,
    val label: String,
    val count: Int,
)

/**
 * The census filter bar's whole state.
 *
 * Park -> shed CASCADES: [sheds] holds ONLY the sheds of the currently selected park, and choosing
 * a different park resets [selectedShedId]. That is load-bearing rather than tidy — 66 of the ~154
 * shed names exist in BOTH parks, so a flat shed list is genuinely ambiguous to read and a shed id
 * left over from another park would filter the census to a cohort the operator did not ask for.
 *
 * [shedFilterSupported] is false when the backend has not shipped the `sheds` facet (or shipped it
 * without park attribution). The shed dropdown then renders DISABLED rather than empty-but-tappable
 * — an operator must never open a filter that silently cannot work.
 */
@Immutable
data class CountsFiltersUi(
    val parks: List<CountsFilterOptionUi> = emptyList(),
    val sheds: List<CountsFilterOptionUi> = emptyList(),
    val breeds: List<CountsFilterOptionUi> = emptyList(),
    /**
     * The lifecycle-status vocabulary (Live/Sold/Culled/Dead/Transferred), backend-owned like
     * every other facet here. Empty when the backend has not shipped the `lifecycle` facet yet —
     * the dimension then renders disabled rather than empty-but-tappable, same contract as
     * [shedFilterSupported].
     */
    val lifecycles: List<CountsFilterOptionUi> = emptyList(),
    val selectedParkId: String = "",
    val selectedShedId: String = "",
    val selectedBreed: String = "",
    /** Blank means "the backend default" (live herd only) — never sent as an explicit filter. */
    val selectedLifecycle: String = "",
    /** Resolved from the facets by the ViewModel, so this screen never maps a key to a label. */
    val selectedParkLabel: String? = null,
    val selectedShedLabel: String? = null,
    val selectedBreedLabel: String? = null,
    val selectedLifecycleLabel: String? = null,
    val shedFilterSupported: Boolean = false,
    /**
     * The FULL shed-facet list (not narrowed by park) used for display subtotals. The [sheds] field
     * above is cascaded (narrowed to the selected park), which makes the dropdown work correctly. But
     * the subtotal divider needs per-shed counts regardless of park selection, so it looks up each
     * shed's subtotal in this full list, not the cascaded one. On the default all-parks view, this
     * allows shed subtotals to render even when [sheds] is empty (because no park is selected).
     */
    val shedSubtotals: List<CountsFilterOptionUi> = emptyList(),
) {
    val hasActiveFilter: Boolean
        get() = selectedParkId.isNotBlank() || selectedShedId.isNotBlank() ||
            selectedBreed.isNotBlank() || selectedLifecycle.isNotBlank()

    /** A park must be chosen before its sheds can be offered — see the cascade note above. */
    val isShedFilterEnabled: Boolean
        get() = shedFilterSupported && selectedParkId.isNotBlank() && sheds.isNotEmpty()

    val lifecycleFilterSupported: Boolean
        get() = lifecycles.isNotEmpty()

    /** Chip row backing the filter bottom sheet's "active filters" summary. */
    val activeChips: List<CountsFilterOptionUi>
        get() = listOfNotNull(
            selectedParkLabel?.let { CountsFilterOptionUi(selectedParkId, it, 0) },
            selectedShedLabel?.let { CountsFilterOptionUi(selectedShedId, it, 0) },
            selectedBreedLabel?.let { CountsFilterOptionUi(selectedBreed, it, 0) },
            selectedLifecycleLabel?.let { CountsFilterOptionUi(selectedLifecycle, it, 0) },
        )
}

@Immutable
data class CountsUiState(
    val title: String,
    val scopeLabel: String = "",
    val totals: CountsTotalsUi = CountsTotalsUi(),
    val hasTotals: Boolean = false,
    val filters: CountsFiltersUi = CountsFiltersUi(),
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

    /**
     * Chooses a park, or clears the park filter with a blank [parkId] ("All parks").
     *
     * Handling this RESETS the shed selection. A shed belongs to exactly one park, so a shed id
     * carried across a park change would either match nothing or — worse, given the repeated shed
     * names — quietly describe a different shed than the one the operator is looking at.
     */
    data class SelectPark(val parkId: String) : CountsEvent

    /** Chooses a shed within the selected park, or clears it with a blank [shedId]. */
    data class SelectShed(val shedId: String) : CountsEvent

    /** Chooses a breed, or clears it with a blank [breed]. */
    data class SelectBreed(val breed: String) : CountsEvent

    /**
     * Chooses a lifecycle status (Live/Sold/Culled/Dead/Transferred), or clears it with a blank
     * [lifecycle] to return to the backend default (the live herd).
     */
    data class SelectLifecycle(val lifecycle: String) : CountsEvent

    /** Drops every filter at once and returns the census to the whole live herd. */
    data object ClearFilters : CountsEvent
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
    // Local open/closed state only, per the golden frontend rule — the filter SHEET's presence on
    // screen is layout, not a backend-owned fact. Every option/selection inside it still comes
    // from state.filters.
    var filterSheetOpen by remember { mutableStateOf(false) }
    RefreshOnResume { onEvent(CountsEvent.Refresh) }

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
            // The hero is the whole-result rollup; the Filters button + active-chip row directly
            // under it is the input that produced it.
            item(key = "hero") { CountsHeroCard(state.totals) }
            item(key = "filters") {
                CountsFiltersButtonRow(
                    filters = state.filters,
                    onOpenSheet = { filterSheetOpen = true },
                    onEvent = onEvent,
                )
            }
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

            // Paging handles prefetch itself (PagingConfig.prefetchDistance) — continuation is
            // viewport-owned and has no operator-facing pagination control. `itemKey` gives every row a stable
            // business identity so a re-page never reorders or duplicates a card. A shed header +
            // backend subtotal renders whenever the shed changes from the PREVIOUS already-loaded
            // row — this is a display grouping over the same page, never a re-aggregation: the
            // subtotal printed is state.filters.sheds' own backend count, not a sum of the rows on
            // screen.
            items(
                count = rows.itemCount,
                key = rows.itemKey { it.grainKey },
            ) { index ->
                val row = rows[index]
                if (row != null) {
                    val previousShedId = if (index > 0) rows.peek(index - 1)?.shedId else null
                    if (index == 0 || previousShedId != row.shedId) {
                        CountsShedSubtotalDivider(row = row, filters = state.filters)
                    }
                    CountsRowCard(row)
                }
            }
        }
    }

    if (filterSheetOpen) {
        CountsFiltersSheet(
            filters = state.filters,
            onEvent = onEvent,
            onDismiss = { filterSheetOpen = false },
        )
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
            SyncIconButton(
                isSyncing = state.isRefreshing,
                onSync = onRefresh,
                contentDescription = stringResource(R.string.counts_refresh_description),
            )
        },
    )
}

/**
 * The census hero: one big total with a proportional adult/kid split bar underneath, replacing
 * the old three flat stat tiles. [CountsTotalsUi] is unchanged — this only restyles how the same
 * backend whole-result rollup renders.
 */
@Composable
private fun CountsHeroCard(totals: CountsTotalsUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(20.dp))
            .background(MeshaColors.Surf)
            .padding(18.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = stringResource(R.string.counts_hero_total_label),
            color = MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
        )
        Text(
            text = totals.totalCount.toString(),
            color = MeshaColors.Ink,
            fontSize = 40.sp,
            fontWeight = FontWeight.W800,
        )
        // Proportional adult/kid split bar. Widths are the same backend totals as the labels
        // beneath them — never re-derived from paged rows.
        val total = (totals.totalAdults + totals.totalKids).coerceAtLeast(1)
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(10.dp)
                .clip(RoundedCornerShape(6.dp))
                .background(MeshaColors.Surf3),
        ) {
            Box(
                modifier = Modifier
                    .weight(totals.totalAdults.coerceAtLeast(0).toFloat().coerceAtLeast(0.0001f) / total)
                    .fillMaxSize()
                    .background(MeshaColors.Brand),
            )
            Box(
                modifier = Modifier
                    .weight(totals.totalKids.coerceAtLeast(0).toFloat().coerceAtLeast(0.0001f) / total)
                    .fillMaxSize()
                    .background(MeshaColors.Teal),
            )
        }
        Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            HeroLegendDot(color = MeshaColors.Brand, label = stringResource(R.string.counts_hero_adults_label), value = totals.totalAdults)
            HeroLegendDot(color = MeshaColors.Teal, label = stringResource(R.string.counts_hero_kids_label), value = totals.totalKids)
        }
        Text(
            text = stringResource(R.string.counts_grain_fmt, totals.totalRows),
            color = MeshaColors.Faint,
            fontSize = 11.sp,
        )
    }
}

@Composable
private fun HeroLegendDot(color: androidx.compose.ui.graphics.Color, label: String, value: Int) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(RoundedCornerShape(4.dp))
                .background(color),
        )
        Text(text = "$value", color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
        Text(text = label, color = MeshaColors.Muted, fontSize = 12.sp)
    }
}

/**
 * Replaces the old always-expanded filter card with a compact Filters button plus a row of
 * active-filter chips. Every chip and every option behind the sheet is still backend-owned data —
 * this is purely a layout change (collapse to summary + on-demand sheet).
 */
@Composable
private fun CountsFiltersButtonRow(
    filters: CountsFiltersUi,
    onOpenSheet: () -> Unit,
    onEvent: (CountsEvent) -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(
            modifier = Modifier
                .clip(RoundedCornerShape(12.dp))
                .background(MeshaColors.Surf2)
                .clickable(onClick = onOpenSheet)
                .padding(horizontal = 14.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(imageVector = MeshaIcons.ChevronDown, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(14.dp))
            Text(
                text = stringResource(R.string.counts_filters_button),
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
        }
        // Active-filter chips. Each chip clears its OWN dimension on tap.
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            filters.selectedParkLabel?.let { FilterChip(it) { onEvent(CountsEvent.SelectPark("")) } }
            filters.selectedShedLabel?.let { FilterChip(it) { onEvent(CountsEvent.SelectShed("")) } }
            filters.selectedBreedLabel?.let { FilterChip(it) { onEvent(CountsEvent.SelectBreed("")) } }
            filters.selectedLifecycleLabel?.let { FilterChip(it) { onEvent(CountsEvent.SelectLifecycle("")) } }
        }
        if (filters.hasActiveFilter) {
            Text(
                text = stringResource(R.string.counts_filters_clear),
                color = MeshaColors.BrandD,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .clickable { onEvent(CountsEvent.ClearFilters) }
                    .padding(horizontal = 8.dp, vertical = 4.dp),
            )
        }
    }
}

@Composable
private fun FilterChip(label: String, onClear: () -> Unit) {
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(10.dp))
            .background(MeshaColors.OkX)
            .clickable(onClick = onClear)
            .padding(horizontal = 10.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(text = label, color = MeshaColors.BrandD, fontSize = 12.sp, fontWeight = FontWeight.W700)
        Icon(imageVector = MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(10.dp))
    }
}

/**
 * Bottom sheet listing every filter dimension — Park, Shed, Breed, and the new Status (lifecycle)
 * facet. Local open/closed state only; every option list and label is [filters], the same
 * backend-owned shape the old inline card rendered from.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun CountsFiltersSheet(
    filters: CountsFiltersUi,
    onEvent: (CountsEvent) -> Unit,
    onDismiss: () -> Unit,
) {
    val allParks = stringResource(R.string.counts_filter_all_parks)
    val allSheds = stringResource(R.string.counts_filter_all_sheds)
    val allBreeds = stringResource(R.string.counts_filter_all_breeds)
    val allLifecycles = stringResource(R.string.counts_filter_all_lifecycles)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = MeshaColors.Surf,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 200.dp)
                .padding(horizontal = 16.dp, vertical = 6.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Text(
                text = stringResource(R.string.counts_filters_sheet_title),
                color = MeshaColors.Ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.W800,
            )
            CountsDropdownField(
                label = stringResource(R.string.counts_filter_lifecycle),
                selectedLabel = filters.selectedLifecycleLabel,
                placeholder = if (filters.lifecycleFilterSupported) allLifecycles else stringResource(R.string.counts_filter_lifecycle_unavailable),
                options = listOf(CountsDropdownOption(key = "", label = allLifecycles)) +
                    filters.lifecycles.map { CountsDropdownOption(it.key, it.label, it.count) },
                onSelect = { onEvent(CountsEvent.SelectLifecycle(it)) },
                enabled = filters.lifecycleFilterSupported,
            )
            CountsDropdownField(
                label = stringResource(R.string.counts_filter_park),
                selectedLabel = filters.selectedParkLabel,
                placeholder = allParks,
                // The "All" sentinel carries a blank key, which is exactly what the ViewModel
                // treats as "send no park_id".
                options = listOf(CountsDropdownOption(key = "", label = allParks)) +
                    filters.parks.map { CountsDropdownOption(it.key, it.label, it.count) },
                onSelect = { onEvent(CountsEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
            )
            CountsDropdownField(
                label = stringResource(R.string.counts_filter_shed),
                selectedLabel = filters.selectedShedLabel,
                placeholder = when {
                    !filters.shedFilterSupported -> stringResource(R.string.counts_filter_shed_unavailable)
                    filters.selectedParkId.isBlank() -> stringResource(R.string.counts_filter_park_first)
                    else -> allSheds
                },
                options = listOf(CountsDropdownOption(key = "", label = allSheds)) +
                    filters.sheds.map { CountsDropdownOption(it.key, it.label, it.count) },
                onSelect = { onEvent(CountsEvent.SelectShed(it)) },
                enabled = filters.isShedFilterEnabled,
            )
            CountsDropdownField(
                label = stringResource(R.string.counts_filter_breed),
                selectedLabel = filters.selectedBreedLabel,
                placeholder = allBreeds,
                options = listOf(CountsDropdownOption(key = "", label = allBreeds)) +
                    filters.breeds.map { CountsDropdownOption(it.key, it.label, it.count) },
                onSelect = { onEvent(CountsEvent.SelectBreed(it)) },
                enabled = filters.breeds.isNotEmpty(),
            )
            CountsSubmitButton(
                label = stringResource(R.string.counts_filters_sheet_done),
                enabled = true,
                onClick = onDismiss,
            )
        }
    }
}

/**
 * Shed group header + backend subtotal micro-bar, rendered once per shed as the paged list
 * scrolls into a new shed. [row]'s own shed subtotal comes from [CountsFiltersUi.shedSubtotals] —
 * the FULL per-shed rollup independent of park selection — never a sum of the rows currently paged
 * into this screen. This allows subtotals to render on the all-parks view even when the cascaded
 * [CountsFiltersUi.sheds] dropdown is empty (because no park is selected).
 */
@Composable
private fun CountsShedSubtotalDivider(row: CountsBreakdownRowUi, filters: CountsFiltersUi) {
    val shedFacet = filters.shedSubtotals.firstOrNull { it.key == row.shedId }
    val shedLabel = row.shedLabel.ifBlank { stringResource(R.string.counts_shed_unassigned) }
    val subtotal = shedFacet?.count
    val shareOfHerd = if (subtotal != null && filters.shedSubtotals.isNotEmpty()) {
        val maxShed = filters.shedSubtotals.maxOf { it.count }.coerceAtLeast(1)
        (subtotal.toFloat() / maxShed).coerceIn(0f, 1f)
    } else {
        null
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = shedLabel,
                color = MeshaColors.Faint,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            if (subtotal != null) {
                Text(
                    text = stringResource(R.string.counts_shed_subtotal_fmt, subtotal),
                    color = MeshaColors.Faint,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W700,
                )
            }
        }
        if (shareOfHerd != null) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(3.dp)
                    .clip(RoundedCornerShape(2.dp))
                    .background(MeshaColors.Surf3),
            ) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth(shareOfHerd)
                        .fillMaxSize()
                        .background(MeshaColors.Brand),
                )
            }
        }
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
