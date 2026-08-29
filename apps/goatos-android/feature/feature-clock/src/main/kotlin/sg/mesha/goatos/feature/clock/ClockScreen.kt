package sg.mesha.goatos.feature.clock

// telemetry:exempt pure stateless renderer; ClockViewModel (in :app) owns the clock_*
// AnalyticsEventsClock + CrashReporter wiring for every punch attempt, refusal and refresh.

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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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

/** One recent-day row, every visible word backend-composed. [listKey] carries full identity. */
@Immutable
data class ClockRecentEntryUi(
    val listKey: String,
    val dateLabel: String,
    val timeLine: String,
    val hoursLabel: String,
    val flags: List<String>,
)

/** The full-screen mock-location refusal panel; [message] is the backend template already
 *  formatted with the offending app labels by the ViewModel. */
@Immutable
data class ClockRefusalUi(
    val message: String,
    val checkAgainLabel: String,
)

@Immutable
data class ClockUiState(
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val hasStatus: Boolean = false,
    /** Backend copy `state.*`, with the clock-in label substituted for the clocked-in state. */
    val stateHeadline: String = "",
    /** Client-ticked elapsed rendering of the backend-served clock_in_at; empty when closed. */
    val elapsedLine: String = "",
    val flags: List<String> = emptyList(),
    /** Backend copy `action.clock_in` / `action.clock_out`; null hides the button. */
    val punchLabel: String? = null,
    /** False while a punch outbox row is still pending (optimistic disable). */
    val punchEnabled: Boolean = true,
    val recentTitle: String = "",
    val emptyRecent: String = "",
    val recent: List<ClockRecentEntryUi> = emptyList(),
    val refusal: ClockRefusalUi? = null,
    /** The backend `state` key (`not_clocked_in` | `clocked_in` | `clocked_out`); drives the
     *  punch direction in the ViewModel, never rendered. */
    val stateKey: String = "",
    /** Backend `punch_refused_copy` template (%s = offending app labels); kept for re-formatting. */
    val refusalTemplate: String = "",
    /** Backend copy `check_again`. */
    val checkAgainLabel: String = "",
    /** Backend copy `refusal.location` — the location-is-mandatory punch refusal. */
    val locationRequiredMessage: String = "",
)

sealed interface ClockEvent {
    data object Refresh : ClockEvent
    data object Punch : ClockEvent
    data object CheckAgain : ClockEvent
}

/**
 * My Clock — the punch page (route `/clock`, docs/features/clock-in-out/plan.md §4.1). L0
 * bottom-bar destination of the clock module. Renders ONLY backend copy from the status
 * contract's `copy` map; the sole client-composed figure is the ticking elapsed line, a pure
 * rendering of the backend-served `clock_in_at` (hours truth stays backend-owned).
 */
@Composable
fun ClockScreen(
    state: ClockUiState,
    onEvent: (ClockEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ClockEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            below = {
                SyncStatusIndicator(
                    isRefreshing = state.isRefreshing,
                    lastSyncedAt = state.lastSyncedAt,
                    hasData = state.hasStatus,
                )
            },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(ClockEvent.Refresh) },
                )
            },
        )
        val refusal = state.refusal
        if (refusal != null) {
            ClockRefusalPanel(refusal = refusal, onCheckAgain = { onEvent(ClockEvent.CheckAgain) })
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "state") {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(14.dp))
                        .background(MeshaColors.Surf)
                        .padding(16.dp),
                ) {
                    if (state.stateHeadline.isNotBlank()) {
                        Text(text = state.stateHeadline, style = MeshaType.screenTitle, color = MeshaColors.Ink)
                    }
                    if (state.elapsedLine.isNotBlank()) {
                        Spacer(Modifier.height(4.dp))
                        Text(text = state.elapsedLine, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
                    }
                    if (state.flags.isNotEmpty()) {
                        Spacer(Modifier.height(8.dp))
                        ClockFlagRow(flags = state.flags)
                    }
                    val punchLabel = state.punchLabel
                    if (punchLabel != null) {
                        Spacer(Modifier.height(16.dp))
                        Button(
                            onClick = { onEvent(ClockEvent.Punch) },
                            enabled = state.punchEnabled,
                            colors = ButtonDefaults.buttonColors(containerColor = MeshaColors.Brand, contentColor = MeshaColors.OnBrand),
                            modifier = Modifier.fillMaxWidth().height(52.dp),
                        ) {
                            Text(text = punchLabel, style = MeshaType.button)
                        }
                    }
                }
            }
            if (state.recentTitle.isNotBlank()) {
                item(key = "recent_title") {
                    Text(
                        text = state.recentTitle,
                        style = MeshaType.cardTitle,
                        color = MeshaColors.Ink,
                        modifier = Modifier.padding(top = 6.dp),
                    )
                }
            }
            if (state.recent.isEmpty() && state.emptyRecent.isNotBlank() && state.hasStatus) {
                item(key = "empty_recent") {
                    EmptyState(
                        title = state.emptyRecent,
                        modifier = Modifier.fillMaxWidth(),
                        icon = MeshaIcons.Clock,
                        tone = EmptyTone.Neutral,
                    )
                }
            }
            items(state.recent, key = { it.listKey }) { entry ->
                ClockRecentEntryRow(entry)
            }
        }
    }
}

@Composable
private fun ClockRecentEntryRow(entry: ClockRecentEntryUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf)
            .padding(horizontal = 14.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text = entry.dateLabel, style = MeshaType.cardTitle, color = MeshaColors.Ink)
            Spacer(Modifier.weight(1f))
            if (entry.hoursLabel.isNotBlank()) {
                Text(text = entry.hoursLabel, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
            }
        }
        if (entry.timeLine.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(text = entry.timeLine, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
        }
        if (entry.flags.isNotEmpty()) {
            Spacer(Modifier.height(6.dp))
            ClockFlagRow(flags = entry.flags)
        }
    }
}

/** Backend-composed flag chip labels, rendered verbatim. */
@Composable
internal fun ClockFlagRow(flags: List<String>) {
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        flags.forEach { flag ->
            Text(
                text = flag,
                style = MeshaType.pill,
                color = MeshaColors.Warn,
                modifier = Modifier
                    .clip(RoundedCornerShape(999.dp))
                    .background(MeshaColors.WarnX)
                    .padding(horizontal = 8.dp, vertical = 3.dp),
            )
        }
    }
}

/**
 * Full-screen refusal: the backend's farm-worded template already formatted with the offending
 * app labels. "Check again" re-runs the scan after the person uninstalls.
 */
@Composable
private fun ClockRefusalPanel(refusal: ClockRefusalUi, onCheckAgain: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        EmptyState(
            title = refusal.message,
            modifier = Modifier.fillMaxWidth(),
            icon = MeshaIcons.Warn,
            tone = EmptyTone.Warn,
        )
        if (refusal.checkAgainLabel.isNotBlank()) {
            Spacer(Modifier.height(20.dp))
            OutlinedButton(onClick = onCheckAgain, modifier = Modifier.fillMaxWidth().height(48.dp)) {
                Text(text = refusal.checkAgainLabel, style = MeshaType.button)
            }
        }
    }
}
