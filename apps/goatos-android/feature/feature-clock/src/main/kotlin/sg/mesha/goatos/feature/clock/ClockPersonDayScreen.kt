package sg.mesha.goatos.feature.clock

// telemetry:exempt pure stateless renderer; ClockPersonDayViewModel (in :app) owns the
// AnalyticsEventsClock + CrashReporter wiring for opens and load failures.

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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/** One label→value line inside a punch card; labels are backend copy, values captured data. */
@Immutable
data class ClockDetailLineUi(val label: String, val value: String)

/** One punch event card ([title] = backend copy for the event kind). */
@Immutable
data class ClockPersonEventUi(
    val listKey: String,
    val title: String,
    val flags: List<String>,
    val lines: List<ClockDetailLineUi>,
)

@Immutable
data class ClockPersonDayUiState(
    val isRefreshing: Boolean = false,
    val hasData: Boolean = false,
    val name: String = "",
    /** "designation · park", backend-supplied halves. */
    val subtitle: String = "",
    /** The day's backend-composed summary line (in/out/hours), verbatim. */
    val entryLine: String = "",
    val dateLabel: String = "",
    val events: List<ClockPersonEventUi> = emptyList(),
    val recentTitle: String = "",
    val recent: List<ClockRecentEntryUi> = emptyList(),
    val emptyText: String = "",
)

sealed interface ClockPersonDayEvent {
    data object Refresh : ClockPersonDayEvent
    data object Back : ClockPersonDayEvent
}

/**
 * Person-day detail — the presence board's drill (hosted destination
 * `/clock/team/person/{id}`, Up/Back, no root chrome; docs/features/clock-in-out/plan.md §4.4
 * item 6). Both punches in full — captured/recorded times, address, coordinates, accuracy,
 * device, network, battery, flags — then the person's recent days. Every label is backend copy;
 * every value is captured data rendered verbatim.
 */
@Composable
fun ClockPersonDayScreen(
    state: ClockPersonDayUiState,
    onEvent: (ClockPersonDayEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ClockPersonDayEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 4.dp, vertical = 6.dp),
        ) {
            IconButton(onClick = { onEvent(ClockPersonDayEvent.Back) }) {
                Icon(
                    imageVector = MeshaIcons.ChevronLeft,
                    contentDescription = null,
                    tint = MeshaColors.Ink,
                )
            }
            Column(modifier = Modifier.weight(1f)) {
                Text(text = state.name, style = MeshaType.headerTitle, color = MeshaColors.Ink)
                if (state.subtitle.isNotBlank()) {
                    Text(text = state.subtitle, style = MeshaType.caption, color = MeshaColors.Muted)
                }
            }
            if (state.dateLabel.isNotBlank()) {
                Text(text = state.dateLabel, style = MeshaType.pill, color = MeshaColors.Muted)
                Spacer(Modifier.width(6.dp))
            }
            SyncIconButton(
                isSyncing = state.isRefreshing,
                onSync = { onEvent(ClockPersonDayEvent.Refresh) },
            )
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.entryLine.isNotBlank()) {
                item(key = "entry_line") {
                    Text(text = state.entryLine, style = MeshaType.cardTitle, color = MeshaColors.Ink)
                }
            }
            if (state.events.isEmpty() && state.emptyText.isNotBlank() && state.hasData) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyText,
                        modifier = Modifier.fillMaxWidth(),
                        icon = MeshaIcons.Clock,
                        tone = EmptyTone.Neutral,
                    )
                }
            }
            items(state.events, key = { it.listKey }) { event ->
                ClockPersonEventCard(event)
            }
            if (state.recentTitle.isNotBlank() && state.recent.isNotEmpty()) {
                item(key = "recent_title") {
                    Text(
                        text = state.recentTitle,
                        style = MeshaType.cardTitle,
                        color = MeshaColors.Ink,
                        modifier = Modifier.padding(top = 6.dp),
                    )
                }
            }
            items(state.recent, key = { it.listKey }) { entry ->
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .background(MeshaColors.Surf)
                        .padding(horizontal = 14.dp, vertical = 10.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(text = entry.dateLabel, style = MeshaType.listTitle, color = MeshaColors.Ink)
                        Spacer(Modifier.weight(1f))
                        if (entry.hoursLabel.isNotBlank()) {
                            Text(text = entry.hoursLabel, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
                        }
                    }
                    if (entry.timeLine.isNotBlank()) {
                        Text(text = entry.timeLine, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
                    }
                    if (entry.flags.isNotEmpty()) {
                        Spacer(Modifier.height(4.dp))
                        ClockFlagRow(flags = entry.flags)
                    }
                }
            }
        }
    }
}

@Composable
private fun ClockPersonEventCard(event: ClockPersonEventUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(text = event.title, style = MeshaType.cardTitle, color = MeshaColors.Ink)
        if (event.flags.isNotEmpty()) {
            Spacer(Modifier.height(6.dp))
            ClockFlagRow(flags = event.flags)
        }
        event.lines.forEach { line ->
            Spacer(Modifier.height(6.dp))
            Row(verticalAlignment = Alignment.Top) {
                if (line.label.isNotBlank()) {
                    Text(
                        text = line.label,
                        style = MeshaType.caption,
                        color = MeshaColors.Muted,
                        modifier = Modifier.width(120.dp),
                    )
                }
                Text(text = line.value, style = MeshaType.cardSubtitle, color = MeshaColors.Ink)
            }
        }
    }
}
