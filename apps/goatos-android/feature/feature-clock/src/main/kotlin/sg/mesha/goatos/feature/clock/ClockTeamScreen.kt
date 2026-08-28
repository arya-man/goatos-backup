package sg.mesha.goatos.feature.clock

// telemetry:exempt pure stateless renderer; ClockTeamViewModel (in :app) owns the clock_*
// AnalyticsEventsClock + CrashReporter wiring for opens, filter changes and load failures.

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/** One day chip; [label] is a short date rendering (data formatting, not copy). */
@Immutable
data class ClockDateChipUi(val date: String, val label: String, val selected: Boolean)

/** One summary tile; label is backend copy, count a whole-filter aggregate, never page math. */
@Immutable
data class ClockTeamTileUi(val key: String, val label: String, val count: Int, val selected: Boolean)

/** One single-select filter chip (park / designation); labels backend-composed. */
@Immutable
data class ClockFilterChipUi(val key: String, val label: String, val selected: Boolean)

/** One roster row. [listKey] carries full identity (member id + business date). */
@Immutable
data class ClockTeamRowUi(
    val listKey: String,
    val memberId: String,
    val name: String,
    /** "designation · park", both halves backend-supplied; blank halves dropped. */
    val subtitle: String,
    /** Backend `time_label` verbatim, plus the client-ticked live elapsed for working-today. */
    val timeLine: String,
    val flags: List<String>,
    val bucket: String,
)

/** One bucket section in fixed order working → clocked_out → not_clocked_in. */
@Immutable
data class ClockTeamSectionUi(val key: String, val title: String, val rows: List<ClockTeamRowUi>)

@Immutable
data class ClockTeamUiState(
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val hasData: Boolean = false,
    val dates: List<ClockDateChipUi> = emptyList(),
    val tiles: List<ClockTeamTileUi> = emptyList(),
    val searchPlaceholder: String = "",
    val query: String = "",
    val parkChips: List<ClockFilterChipUi> = emptyList(),
    val designationChips: List<ClockFilterChipUi> = emptyList(),
    val sections: List<ClockTeamSectionUi> = emptyList(),
    val emptyText: String = "",
    val isLoadingMore: Boolean = false,
    val hasMore: Boolean = false,
)

sealed interface ClockTeamEvent {
    data object Refresh : ClockTeamEvent
    data class SelectDate(val date: String) : ClockTeamEvent
    data class SelectTile(val key: String) : ClockTeamEvent
    data class QueryChanged(val query: String) : ClockTeamEvent
    data class SelectPark(val key: String) : ClockTeamEvent
    data class SelectDesignation(val key: String) : ClockTeamEvent
    data object LoadMore : ClockTeamEvent
    data class OpenPerson(val memberId: String) : ClockTeamEvent
}

/**
 * Team — the leadership presence board (route `/clock/team`,
 * docs/features/clock-in-out/plan.md §4.4). Second L0 bottom-bar destination of the clock
 * module, present only when bootstrap offered the nav item. Keyset infinite scroll (~20/page,
 * prefetch near the end, PASSIVE footer — never a load-more button). Section titles, tile
 * labels, chip copy and empty states are all backend-owned.
 */
@Composable
fun ClockTeamScreen(
    state: ClockTeamUiState,
    onEvent: (ClockTeamEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ClockTeamEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            below = {
                SyncStatusIndicator(
                    isRefreshing = state.isRefreshing,
                    lastSyncedAt = state.lastSyncedAt,
                    hasData = state.hasData,
                )
            },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(ClockTeamEvent.Refresh) },
                )
            },
        )
        DateChipStrip(dates = state.dates, onEvent = onEvent)
        SummaryTiles(tiles = state.tiles, onEvent = onEvent)
        SearchField(
            query = state.query,
            placeholder = state.searchPlaceholder,
            onChanged = { onEvent(ClockTeamEvent.QueryChanged(it)) },
        )
        FilterChipRow(chips = state.parkChips) { onEvent(ClockTeamEvent.SelectPark(it)) }
        FilterChipRow(chips = state.designationChips) { onEvent(ClockTeamEvent.SelectDesignation(it)) }

        val flatCount = state.sections.sumOf { it.rows.size }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            if (flatCount == 0 && state.emptyText.isNotBlank() && state.hasData) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyText,
                        modifier = Modifier.fillMaxWidth(),
                        icon = MeshaIcons.Clock,
                        tone = EmptyTone.Neutral,
                    )
                }
            }
            var rendered = 0
            state.sections.forEach { section ->
                if (section.rows.isEmpty()) return@forEach
                item(key = "section:${section.key}") {
                    Text(
                        text = section.title,
                        style = MeshaType.sectionLabel,
                        color = MeshaColors.Muted,
                        modifier = Modifier.padding(top = 8.dp, bottom = 2.dp),
                    )
                }
                section.rows.forEach { row ->
                    rendered += 1
                    val indexFromEnd = flatCount - rendered
                    item(key = row.listKey) {
                        // Passive keyset prefetch: rendering one of the last ~3 rows asks for the
                        // next page while the person is still scrolling — never a tappable button.
                        if (state.hasMore && !state.isLoadingMore && indexFromEnd <= 2) {
                            LaunchedEffect(row.listKey, flatCount) { onEvent(ClockTeamEvent.LoadMore) }
                        }
                        ClockTeamRow(row = row, onOpen = { onEvent(ClockTeamEvent.OpenPerson(row.memberId)) })
                    }
                }
            }
            if (state.isLoadingMore) {
                item(key = "loading_footer") {
                    Box(
                        modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(color = MeshaColors.BrandD)
                    }
                }
            }
        }
    }
}

@Composable
private fun DateChipStrip(dates: List<ClockDateChipUi>, onEvent: (ClockTeamEvent) -> Unit) {
    if (dates.isEmpty()) return
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        dates.forEach { chip ->
            Text(
                text = chip.label,
                style = MeshaType.pill,
                color = if (chip.selected) MeshaColors.OnBrand else MeshaColors.Ink,
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .background(if (chip.selected) MeshaColors.Brand else MeshaColors.Surf2)
                    .selectable(
                        selected = chip.selected,
                        role = Role.Tab,
                        onClick = { onEvent(ClockTeamEvent.SelectDate(chip.date)) },
                    )
                    .padding(horizontal = 12.dp, vertical = 6.dp),
            )
        }
    }
}

@Composable
private fun SummaryTiles(tiles: List<ClockTeamTileUi>, onEvent: (ClockTeamEvent) -> Unit) {
    if (tiles.isEmpty()) return
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        tiles.forEach { tile ->
            Column(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(12.dp))
                    .background(if (tile.selected) MeshaColors.OkX else MeshaColors.Surf)
                    .selectable(
                        selected = tile.selected,
                        role = Role.Tab,
                        onClick = { onEvent(ClockTeamEvent.SelectTile(tile.key)) },
                    )
                    .padding(horizontal = 8.dp, vertical = 10.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(text = "${tile.count}", style = MeshaType.cardTitle, color = MeshaColors.Ink)
                Spacer(Modifier.height(2.dp))
                Text(text = tile.label, style = MeshaType.caption, color = MeshaColors.Muted, maxLines = 2)
            }
        }
    }
}

@Composable
private fun SearchField(query: String, placeholder: String, onChanged: (String) -> Unit) {
    OutlinedTextField(
        value = query,
        onValueChange = onChanged,
        singleLine = true,
        placeholder = {
            if (placeholder.isNotBlank()) {
                Text(text = placeholder, style = MeshaType.cardSubtitle, color = MeshaColors.Faint)
            }
        },
        leadingIcon = {
            Icon(
                imageVector = MeshaIcons.Search,
                contentDescription = null,
                tint = MeshaColors.Muted,
                modifier = Modifier.size(18.dp),
            )
        },
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        colors = OutlinedTextFieldDefaults.colors(
            focusedBorderColor = MeshaColors.Brand,
            unfocusedBorderColor = MeshaColors.Hair,
            focusedTextColor = MeshaColors.Ink,
            unfocusedTextColor = MeshaColors.Ink,
        ),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
    )
}

@Composable
private fun FilterChipRow(chips: List<ClockFilterChipUi>, onSelect: (String) -> Unit) {
    if (chips.isEmpty()) return
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 3.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        chips.forEach { chip ->
            Text(
                text = chip.label,
                style = MeshaType.pill,
                color = if (chip.selected) MeshaColors.OnBrand else MeshaColors.Ink,
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .background(if (chip.selected) MeshaColors.Brand else MeshaColors.Surf2)
                    .selectable(selected = chip.selected, role = Role.Tab, onClick = { onSelect(chip.key) })
                    .padding(horizontal = 10.dp, vertical = 5.dp),
            )
        }
    }
}

@Composable
private fun ClockTeamRow(row: ClockTeamRowUi, onOpen: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf)
            .selectable(selected = false, role = Role.Button, onClick = onOpen)
            .padding(horizontal = 14.dp, vertical = 10.dp),
    ) {
        Box(
            modifier = Modifier
                .size(10.dp)
                .clip(CircleShape)
                .background(
                    when (row.bucket) {
                        "working" -> MeshaColors.Ok
                        "clocked_out" -> MeshaColors.Muted
                        else -> MeshaColors.Surf3
                    },
                ),
        )
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(text = row.name, style = MeshaType.listTitle, color = MeshaColors.Ink)
            if (row.subtitle.isNotBlank()) {
                Text(text = row.subtitle, style = MeshaType.caption, color = MeshaColors.Muted)
            }
            if (row.timeLine.isNotBlank()) {
                Text(text = row.timeLine, style = MeshaType.cardSubtitle, color = MeshaColors.Ink)
            }
            if (row.flags.isNotEmpty()) {
                Spacer(Modifier.height(4.dp))
                ClockFlagRow(flags = row.flags)
            }
        }
    }
}
