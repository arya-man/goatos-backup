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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.SyncStatusIndicator

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
 *
 * Fallback flags are set by the ViewModel when a field is empty; the Screen uses them
 * to decide whether to render the backend value or a localized fallback via stringResource.
 * [isBackupSlot] is a backend flag used by the Screen to choose between "Backup slot"
 * and "—" when rendering a fallback label.
 */
@Immutable
data class TimetableRow(
    val id: String,
    val positionLabel: String,
    val positionLabelFallback: Boolean = false,
    val tier: PositionTier,
    val holderName: String?,
    val weekOffLabel: String,
    val weekOffLabelFallback: Boolean = false,
    val backupLabel: String,
    val backupLabelFallback: Boolean = false,
    val isBackupSlot: Boolean = false,
    val statusLabel: String,
    val statusLabelFallback: Boolean = false,
    val isActive: Boolean,
)

// @Immutable: rows: List<TimetableRow> otherwise marks this unstable (item 6, perf/stability pass).
@Immutable
data class TimetableUiState(
    val title: String = "Timetable",
    val rows: List<TimetableRow> = emptyList(),
    /**
     * Error code: "no_center" when operator has no primary location assigned, "load_failed" when
     * the roster API call failed, or null when the screen is loaded or loading successfully.
     * The Screen renders the localized error message based on this code.
     */
    val errorCode: String? = null,
    /** A background refresh failed but prior rows are kept on screen (refresh never wipes). */
    val isOffline: Boolean = false,
)

sealed interface TimetableEvent {
    data object Refresh : TimetableEvent
}

@Composable
private fun tierLabel(tier: PositionTier): String = when (tier) {
    PositionTier.ASSISTANT -> stringResource(R.string.timetable_tier_assistant)
    PositionTier.MANAGER -> stringResource(R.string.timetable_tier_manager)
    PositionTier.HEAD -> stringResource(R.string.timetable_tier_head)
    PositionTier.DIRECTOR -> stringResource(R.string.timetable_tier_director)
    PositionTier.CXO -> stringResource(R.string.timetable_tier_cxo)
    PositionTier.UNKNOWN -> stringResource(R.string.timetable_tier_unknown)
}

/** Localized subtitle for the Timetable screen. */
@Composable
private fun subtitleText(): String = stringResource(R.string.timetable_subtitle)

/** Localized error message based on the error code. */
@Composable
private fun errorText(errorCode: String?): String? = when (errorCode) {
    "no_center" -> stringResource(R.string.timetable_error_no_center)
    "load_failed" -> stringResource(R.string.timetable_error_load)
    else -> null
}

/** Localized empty-state message when no positions are configured. */
@Composable
private fun emptyText(): String = stringResource(R.string.timetable_empty)

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
            item { TimetableEmpty(errorText(state.errorCode) ?: emptyText()) }
        } else {
            // MOB-011: Use stable keys instead of index to avoid recomposition on reorder
            items(state.rows, key = { row -> row.id }, contentType = { "timetable_row" }) { row ->
                TimetableRowCard(row)
            }
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
        Text(text = stringResource(R.string.timetable_title), style = MeshaType.screenTitle, color = MeshaColors.Ink)
        Text(
            text = subtitleText(),
            style = MeshaType.cardSubtitle,
            color = MeshaColors.Muted,
            modifier = Modifier.padding(top = MeshaDimens.space1),
        )
        // A failed refresh keeps the last-loaded roster on screen; flag it as stale rather
        // than blanking to an error (this HRMS mirror has no Room cache, so only in-session
        // rows survive — still better than wiping them on a transient network blip).
        if (state.isOffline && state.rows.isNotEmpty()) {
            SyncStatusIndicator(
                isRefreshing = false,
                lastSyncedAt = null,
                hasData = true,
                isOffline = true,
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
                text = positionLabelText(row),
                style = MeshaType.cardTitle,
                color = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            TierTag(row.tier)
        }
        Spacer(Modifier.height(MeshaDimens.space2))
        Row(horizontalArrangement = Arrangement.spacedBy(MeshaDimens.space6)) {
            MetaCell(label = stringResource(R.string.timetable_label_holder), value = row.holderName ?: stringResource(R.string.timetable_unassigned))
            MetaCell(label = stringResource(R.string.timetable_label_week_off), value = weekOffLabelText(row))
            MetaCell(label = stringResource(R.string.timetable_label_backup), value = backupLabelText(row))
        }
        Text(
            text = statusLabelText(row),
            style = MeshaType.caption,
            color = statusColor,
            modifier = Modifier.padding(top = MeshaDimens.space2),
        )
    }
}

/** Render positionLabel; use fallback if the label is empty. */
@Composable
private fun positionLabelText(row: TimetableRow): String =
    if (row.positionLabelFallback) stringResource(R.string.timetable_position_fallback) else row.positionLabel

/** Render weekOffLabel; use fallback if the label is empty. */
@Composable
private fun weekOffLabelText(row: TimetableRow): String =
    if (row.weekOffLabelFallback) stringResource(R.string.timetable_empty_value) else row.weekOffLabel

/** Render backupLabel; use fallback if appropriate. */
@Composable
private fun backupLabelText(row: TimetableRow): String =
    if (row.backupLabelFallback) {
        if (row.isBackupSlot) stringResource(R.string.timetable_backup_slot)
        else stringResource(R.string.timetable_empty_value)
    } else {
        row.backupLabel
    }

/** Render statusLabel; use fallback if the label is empty. */
@Composable
private fun statusLabelText(row: TimetableRow): String =
    if (row.statusLabelFallback) stringResource(R.string.timetable_empty_value) else row.statusLabel

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
    EmptyState(
        title = message,
        icon = MeshaIcons.Clock,
        tone = EmptyTone.Neutral,
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = MeshaDimens.space8),
    )
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun TimetableScreenPreview() {
    GoatOsTheme {
        TimetableScreen(
            state = TimetableUiState(
                rows = listOf(
                    TimetableRow(
                        id = "p1",
                        positionLabel = "Feeding AM1",
                        positionLabelFallback = false,
                        tier = PositionTier.ASSISTANT,
                        holderName = "Arun Kumar",
                        weekOffLabel = "Mon",
                        weekOffLabelFallback = false,
                        backupLabel = "Backup AM1",
                        backupLabelFallback = false,
                        isBackupSlot = false,
                        statusLabel = "Active",
                        statusLabelFallback = false,
                        isActive = true,
                    ),
                    TimetableRow(
                        id = "p2",
                        positionLabel = "Preventive Care Manager",
                        positionLabelFallback = false,
                        tier = PositionTier.MANAGER,
                        holderName = null,
                        weekOffLabel = "",
                        weekOffLabelFallback = true,
                        backupLabel = "",
                        backupLabelFallback = true,
                        isBackupSlot = true,
                        statusLabel = "Active",
                        statusLabelFallback = false,
                        isActive = true,
                    ),
                ),
            ),
        )
    }
}
