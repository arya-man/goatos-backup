package sg.mesha.goatos.feature.leadership

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
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
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.feature.leadership.R

// ─────────────────────────────────────────────────────────────────────────────
// Overdue (v-overdue). The backend classifies each row missed vs in-buffer
// (Asia/Kolkata buffer/lateness math lives server-side); this screen only renders
// the returned `classification` + label + legend. No client date/buffer math.
// ─────────────────────────────────────────────────────────────────────────────

/** Backend classification of an overdue item — the app never computes this. */
enum class OverdueClassification { MISSED, IN_BUFFER }

internal fun OverdueClassification.tone(): Tone = when (this) {
    OverdueClassification.MISSED -> Tone.DANGER
    OverdueClassification.IN_BUFFER -> Tone.WARN
}

// @Immutable: List<T> fields (legend, rows) otherwise mark this unstable, disabling
// recomposition skipping for OverdueScreen (item 6, perf/stability pass).
@Immutable
data class OverdueUiState(
    val eyebrow: String,
    val title: String,
    val sectionTitle: String,
    val legend: List<OverdueLegendItem>,
    val rows: List<OverdueRow>,
    val explainerTitle: String,
    val explainer: String,
)

data class OverdueLegendItem(
    val classification: OverdueClassification,
    val label: String,
)

data class OverdueRow(
    val id: String,
    val title: String,
    val subtitle: String,
    val statusLabel: String,
    val classification: OverdueClassification,
)

@Composable
fun OverdueScreen(
    state: OverdueUiState,
    onEvent: (LeadershipEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(LeadTokens.pageBg),
    ) {
        LeadTopBar(
            eyebrow = state.eyebrow,
            // Title is fixed chrome + a live count — localize here (the VM's English
            // "Overdue · N" title is ignored so it follows the app locale).
            title = stringResource(R.string.overdue_title_fmt, state.rows.size),
            leading = TopBarLeading.BACK,
            onLeading = { onEvent(LeadershipEvent.Back) },
            onRefresh = { onEvent(LeadershipEvent.Refresh) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxWidth().weight(1f),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item { SectionLabel(state.sectionTitle) }
            item { Legend(state.legend) }
            items(state.rows, key = { it.id }) { row ->
                OverdueRowView(row, onEvent)
            }
            item { SectionLabel(state.explainerTitle) }
            item { InfoBox(state.explainer) }
        }
    }
}

@Composable
private fun Legend(items: List<OverdueLegendItem>) {
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        items.forEach { item ->
            val c = item.classification.tone().colors()
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Box(Modifier.size(8.dp).clip(CircleShape).background(c.fg))
                Text(item.label, color = LeadTokens.muted, fontSize = 11.sp)
            }
        }
    }
}

@Composable
private fun OverdueRowView(row: OverdueRow, onEvent: (LeadershipEvent) -> Unit) {
    val tone = row.classification.tone()
    Card(onClick = { onEvent(LeadershipEvent.OverdueRowTapped(row.id)) }) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            GlyphBadge(MeshaIcons.Warn, tone.colors())
            Column(Modifier.weight(1f)) {
                Text(row.title, color = LeadTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.Bold)
                Text(row.subtitle, color = LeadTokens.muted, fontSize = 12.sp)
            }
            StatusPill(row.statusLabel, tone)
        }
    }
}

// region ── Preview ──

internal fun sampleOverdueState() = OverdueUiState(
    eyebrow = "Vaccination",
    title = "Overdue · 38 animals",
    sectionTitle = "Needs rescheduling",
    legend = listOf(
        OverdueLegendItem(OverdueClassification.MISSED, "Missed · past buffer"),
        OverdueLegendItem(OverdueClassification.IN_BUFFER, "In buffer · recoverable"),
    ),
    rows = listOf(
        OverdueRow("o1", "Goat Pox · Yashoda 5", "CBE · 9 days late · past buffer · 24 animals", "Missed", OverdueClassification.MISSED),
        OverdueRow("o2", "Sheep Pox · Castro 1", "CPT · 3 days late · in buffer · 8 animals", "In buffer", OverdueClassification.IN_BUFFER),
        OverdueRow("o3", "FMD · Booster · Mandela 1", "CBE · 2 days late · in buffer · 6 animals", "In buffer", OverdueClassification.IN_BUFFER),
    ),
    explainerTitle = "What the colours mean",
    explainer = "Red · Missed — the dose window closed and the animal wasn't recovered into a " +
        "compatible drive in time; leadership was alerted ahead. Amber · In buffer — overdue but " +
        "still recoverable by reschedule into a compatible drive, no dose missed. In/out-of-buffer " +
        "is computed by backend policy (policySnapshot); the app only renders the label.",
)

/**
 * Localized sample state using stringResource for all static UI chrome.
 * This @Composable variant is used to support locale changes via the language switcher.
 */
@Composable
internal fun sampleOverdueStateLocalized() = OverdueUiState(
    eyebrow = "Vaccination",
    title = "Overdue · 38 animals",
    sectionTitle = stringResource(R.string.overdue_section_title),
    legend = listOf(
        OverdueLegendItem(OverdueClassification.MISSED, stringResource(R.string.overdue_legend_missed)),
        OverdueLegendItem(OverdueClassification.IN_BUFFER, stringResource(R.string.overdue_legend_in_buffer)),
    ),
    rows = listOf(
        OverdueRow("o1", "Goat Pox · Yashoda 5", "CBE · 9 days late · past buffer · 24 animals", "Missed", OverdueClassification.MISSED),
        OverdueRow("o2", "Sheep Pox · Castro 1", "CPT · 3 days late · in buffer · 8 animals", "In buffer", OverdueClassification.IN_BUFFER),
        OverdueRow("o3", "FMD · Booster · Mandela 1", "CBE · 2 days late · in buffer · 6 animals", "In buffer", OverdueClassification.IN_BUFFER),
    ),
    explainerTitle = stringResource(R.string.overdue_explainer_title),
    explainer = stringResource(R.string.overdue_explainer),
)

@Preview(name = "Overdue", widthDp = 380, heightDp = 760, backgroundColor = 0xFF0A0F0C, showBackground = true)
@Composable
private fun OverdueScreenPreview() {
    GoatOsTheme {
        OverdueScreen(state = sampleOverdueState())
    }
}

// endregion
