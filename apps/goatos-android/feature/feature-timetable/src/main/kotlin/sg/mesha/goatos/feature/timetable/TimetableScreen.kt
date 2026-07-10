package sg.mesha.goatos.feature.timetable

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

// ---------------------------------------------------------------------------
// Timetable (HRMS shift roster) — screens.md-style TRD §14 dumb renderer.
//
// mock/goatos-dashboard-mock.html People -> Timetable (`data-sub="timetable"`) is the
// admin-web CRUD surface: "Operational position | Shift | CBE | CPT | Week OFF | Backup"
// as a per-center pivot table, editable there. Mobile MIRRORS it READ-ONLY (the mock's
// own copy: "The mobile app mirrors this read-only. Edited here (web CRUD) only.") — this
// screen renders the operator's center rows from `GET /app/roster/timetable` as a
// scrollable list (one row = one position seat), not the 2-center pivot table, since the
// pivot is specific to the CBE/CPT sample deployment and would not generalise past 2
// centers.
//
// The mock's "Shift" (a shift TIME) is NOT modeled here: the EnrichedPosition contract
// (contracts/openapi/app-api.yaml) has no shift-time field. The seat holder's display
// name IS modeled (person_display_name) — this screen renders exactly what the contract
// gives it and never fabricates a time. Every value below is backend-provided; the
// client only glue-maps enums (tier/status) to short labels — the same allowance
// AlertRow's tone gets for its pill.
// ---------------------------------------------------------------------------

/** Backend `position_tier` enum — glue-mapped to a short label only. */
enum class PositionTier { ASSISTANT, MANAGER, HEAD, DIRECTOR, CXO, UNKNOWN }

/**
 * One fixed operational position seat (mirrors the EnrichedPosition schema 1:1).
 * [holderName] is the backend-resolved `person_display_name` — null when the seat is
 * unfilled. Never a raw UUID: the screen renders "Unassigned" for a null holder, it
 * never falls back to an id fragment.
 */
@Immutable
data class TimetableRow(
    val id: String,
    val positionLabel: String,
    val tier: PositionTier,
    val holderName: String?,
    val weekOffLabel: String,
    val backupLabel: String,
    val statusLabel: String,
    val isActive: Boolean,
)

// @Immutable: rows: List<TimetableRow> otherwise marks this unstable (item 6, perf/stability pass).
@Immutable
data class TimetableUiState(
    val title: String = "Timetable",
    val subtitle: String = "",
    val rows: List<TimetableRow> = emptyList(),
    val emptyLabel: String = "No positions configured",
    /** Set only on a load failure — an honest error state, never a fabricated roster. */
    val errorLabel: String? = null,
)

sealed interface TimetableEvent {
    data object Refresh : TimetableEvent
}

private fun tierLabel(tier: PositionTier): String = when (tier) {
    PositionTier.ASSISTANT -> "Assistant"
    PositionTier.MANAGER -> "Manager"
    PositionTier.HEAD -> "Head"
    PositionTier.DIRECTOR -> "Director"
    PositionTier.CXO -> "CXO"
    PositionTier.UNKNOWN -> "—"
}

@Composable
fun TimetableScreen(
    state: TimetableUiState,
    onEvent: (TimetableEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LazyColumn(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.Bg),
        contentPadding = PaddingValues(bottom = MeshaDimens.space6),
    ) {
        item { TimetableHeader(state) }
        if (state.rows.isEmpty()) {
            item { TimetableEmpty(state.errorLabel ?: state.emptyLabel) }
        } else {
            items(state.rows.size) { index -> TimetableRowCard(state.rows[index]) }
        }
    }
}

@Composable
private fun TimetableHeader(state: TimetableUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = MeshaDimens.gutter, vertical = MeshaDimens.screenTop),
    ) {
        Text(text = state.title, style = MeshaType.screenTitle, color = MeshaColors.Ink)
        if (state.subtitle.isNotBlank()) {
            Text(
                text = state.subtitle,
                style = MeshaType.cardSubtitle,
                color = MeshaColors.Muted,
                modifier = Modifier.padding(top = MeshaDimens.space1),
            )
        }
    }
}

@Composable
private fun TimetableRowCard(row: TimetableRow) {
    val statusColor = if (row.isActive) MeshaColors.Ok else MeshaColors.Faint
    Column(
        modifier = Modifier
            .padding(start = MeshaDimens.gutter, end = MeshaDimens.gutter, top = MeshaDimens.space3)
            .fillMaxWidth()
            .background(MeshaColors.Surf2, shape = RoundedCornerShape(MeshaDimens.radiusCard))
            .border(MeshaDimens.hairline, MeshaColors.Hair, shape = RoundedCornerShape(MeshaDimens.radiusCard))
            .padding(MeshaDimens.space4),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = row.positionLabel,
                style = MeshaType.cardTitle,
                color = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            TierTag(row.tier)
        }
        Spacer(Modifier.height(MeshaDimens.space2))
        Row(horizontalArrangement = Arrangement.spacedBy(MeshaDimens.space6)) {
            MetaCell(label = "Holder", value = row.holderName ?: "Unassigned")
            MetaCell(label = "Week OFF", value = row.weekOffLabel)
            MetaCell(label = "Backup", value = row.backupLabel)
        }
        Text(
            text = row.statusLabel,
            style = MeshaType.caption,
            color = statusColor,
            modifier = Modifier.padding(top = MeshaDimens.space2),
        )
    }
}

@Composable
private fun TierTag(tier: PositionTier) {
    Box(
        modifier = Modifier
            .background(MeshaColors.Surf3, shape = MeshaDimens.pill)
            .padding(horizontal = MeshaDimens.space3, vertical = MeshaDimens.space1),
    ) {
        Text(text = tierLabel(tier), style = MeshaType.pill, color = MeshaColors.Muted)
    }
}

@Composable
private fun MetaCell(label: String, value: String) {
    Column {
        Text(text = label.uppercase(), style = MeshaType.fieldLabel, color = MeshaColors.Faint)
        Text(
            text = value,
            style = MeshaType.body,
            color = MeshaColors.Muted,
            modifier = Modifier.padding(top = MeshaDimens.space1),
        )
    }
}

@Composable
private fun TimetableEmpty(message: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = MeshaDimens.gutter, vertical = MeshaDimens.space8),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = message,
            style = MeshaType.body,
            color = MeshaColors.Faint,
            textAlign = TextAlign.Center,
        )
    }
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun TimetableScreenPreview() {
    GoatOsTheme {
        TimetableScreen(
            state = TimetableUiState(
                title = "Timetable",
                subtitle = "Shift roster — the operational source for who executes each day.",
                rows = listOf(
                    TimetableRow(
                        id = "p1",
                        positionLabel = "Feeding AM1",
                        tier = PositionTier.ASSISTANT,
                        holderName = "Arun Kumar",
                        weekOffLabel = "Mon",
                        backupLabel = "Backup AM1",
                        statusLabel = "Active",
                        isActive = true,
                    ),
                    TimetableRow(
                        id = "p2",
                        positionLabel = "Preventive Care Manager",
                        tier = PositionTier.MANAGER,
                        holderName = null,
                        weekOffLabel = "—",
                        backupLabel = "Backup Manager",
                        statusLabel = "Active",
                        isActive = true,
                    ),
                ),
            ),
        )
    }
}
