package sg.mesha.goatos.feature.record

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone

// ---------------------------------------------------------------------------
// Shed / drive record sheet (screens.md: ovl-shedrec, ovl-driverec) — READ-ONLY.
//
// Opened from calendar history or a shed card. Per TRD §14 dumb-renderer the screen
// only RENDERS the backend record: it never re-derives given/due counts, never
// classifies status, and never decides eligibility. Each per-vaccine-group row and
// the header status/date come straight from RecordUiState (a later ViewModel fills
// it from GET shed-record/{shed_id} or the drive record).
// ---------------------------------------------------------------------------

private object RecordTokens {
    val Bg = MeshaColors.Bg
    val Surf = MeshaColors.Surf
    val Surf2 = MeshaColors.Surf2
    val Surf3 = MeshaColors.Surf3
    val Hair = MeshaColors.Hair
    val Ink = MeshaColors.Ink
    val Muted = MeshaColors.Muted
    val Faint = MeshaColors.Faint
    val BrandD = MeshaColors.BrandD
    val Danger = MeshaColors.Danger
    val Warn = MeshaColors.Warn
    val OkX = MeshaColors.OkX
    val WarnX = MeshaColors.WarnX
    val DangerX = MeshaColors.DangerX
}

/** Status tone for the header/footer pill and for tinted meta values. */
enum class RecordTone { OK, WARN, DANGER, MUTED }

/** One vaccine group in the record: how many were given of how many due, at what dose. */
data class VaccineGroupRow(
    val vaccine: String,
    val given: Int,
    val due: Int,
    val dose: String,
)

/** A non-vaccine summary line (operator·backup, window·proof, or a status line). */
data class RecordMetaRow(
    val label: String,
    val value: String,
    val valueTone: RecordTone? = null,
)

/**
 * Everything the read-only record sheet renders. [title]/[subtitle]/[statusLabel]
 * are backend labels (shed or drive · date · status); [groups] is the per-vaccine
 * breakdown; [meta] carries operator/window/proof or a "not started" status line.
 *
 * Offline-first sync state (docs/decisions/android-offline-first.md):
 * [isRefreshing]/[lastSyncedAt]/[isOffline] describe the background network refresh
 * over the ALREADY-RENDERED Room cache — they never gate whether the rest of this
 * state renders, and are displayed by SyncStatusIndicator.
 */
// @Immutable: groups/meta List<T> fields otherwise mark this unstable (item 6,
// perf/stability pass).
@Immutable
data class RecordUiState(
    val title: String,
    val subtitle: String,
    val groups: List<VaccineGroupRow>,
    val meta: List<RecordMetaRow> = emptyList(),
    val countLabel: String? = null,
    val statusLabel: String? = null,
    val statusTone: RecordTone = RecordTone.OK,
    // Offline-first sync state, rendered by sg.mesha.goatos.core.ui.SyncStatusIndicator.
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface RecordEvent {
    data object Close : RecordEvent
}

@Composable
fun RecordScreen(
    state: RecordUiState,
    onEvent: (RecordEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(RecordTokens.Bg),
    ) {
        // Sheet-styled surface (mock .sheet: Surf, rounded top 26, internal scroll).
        Column(
            modifier = Modifier
                .fillMaxSize()
                .background(RecordTokens.Surf, shape = RoundedCornerShape(topStart = 26.dp, topEnd = 26.dp)),
        ) {
            SheetGrip()
            RecordHeader(state = state, onEvent = onEvent)
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
                contentPadding = PaddingValues(bottom = 20.dp),
            ) {
                item { RecordSummaryCard(state) }
                state.statusLabel?.let { label ->
                    item {
                        Box(modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp)) {
                            StatusPill(text = label, tone = state.statusTone)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SheetGrip() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 10.dp, bottom = 9.dp),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            modifier = Modifier
                .size(width = 40.dp, height = 5.dp)
                .background(RecordTokens.Faint.copy(alpha = 0.5f), shape = RoundedCornerShape(3.dp)),
        )
    }
}

@Composable
private fun RecordHeader(state: RecordUiState, onEvent: (RecordEvent) -> Unit) {
    Row(
        verticalAlignment = Alignment.Top,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 20.dp, end = 12.dp, bottom = 10.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = state.countLabel?.let { "${state.title} · $it" } ?: state.title,
                color = RecordTokens.Ink,
                fontSize = 16.sp,
                fontWeight = FontWeight.W700,
            )
            Text(
                text = state.subtitle,
                color = RecordTokens.Muted,
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        Box(
            modifier = Modifier
                .size(38.dp)
                .background(RecordTokens.Surf2, shape = RoundedCornerShape(12.dp))
                .clickable { onEvent(RecordEvent.Close) },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Close,
                contentDescription = stringResource(R.string.record_close_button),
                tint = RecordTokens.Muted,
                modifier = Modifier.size(16.dp),
            )
        }
    }
}

@Composable
private fun RecordSummaryCard(state: RecordUiState) {
    if (state.groups.isEmpty()) {
        EmptyState(
            title = state.subtitle,
            icon = MeshaIcons.Syringe,
            tone = EmptyTone.Neutral,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
    } else {
        Column(
            modifier = Modifier
                .padding(horizontal = 16.dp)
                .fillMaxWidth()
                .background(RecordTokens.Surf, shape = RoundedCornerShape(16.dp))
                .border(1.dp, RecordTokens.Hair, shape = RoundedCornerShape(16.dp))
                .padding(horizontal = 15.dp),
        ) {
            val lastGroup = state.groups.lastIndex
            val hasMeta = state.meta.isNotEmpty()
            state.groups.forEachIndexed { index, group ->
                VaccineGroupItem(group)
                if (index != lastGroup || hasMeta) {
                    HorizontalDivider(thickness = 1.dp, color = RecordTokens.Surf2)
                }
            }
            state.meta.forEachIndexed { index, meta ->
                MetaItem(meta)
                if (index != state.meta.lastIndex) {
                    HorizontalDivider(thickness = 1.dp, color = RecordTokens.Surf2)
                }
            }
        }
    }
}

@Composable
private fun VaccineGroupItem(group: VaccineGroupRow) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 11.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.weight(1f)) {
            // Syringe = Vaccination module marker (mock icon set).
            Icon(
                imageVector = MeshaIcons.Syringe,
                contentDescription = null,
                tint = RecordTokens.BrandD,
                modifier = Modifier.size(15.dp),
            )
            Spacer(Modifier.width(7.dp))
            Text(text = group.vaccine, color = RecordTokens.Muted, fontSize = 13.sp)
        }
        Spacer(Modifier.width(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = "${group.given} / ${group.due}",
                color = RecordTokens.BrandD,
                fontSize = 13.sp,
                fontWeight = FontWeight.W800,
            )
            Text(
                text = " · ${group.dose}",
                color = RecordTokens.Muted,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
        }
    }
}

@Composable
private fun MetaItem(meta: RecordMetaRow) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 11.dp),
    ) {
        Text(
            text = meta.label,
            color = RecordTokens.Muted,
            fontSize = 13.sp,
            modifier = Modifier.weight(1f),
        )
        Spacer(Modifier.width(10.dp))
        Text(
            text = meta.value,
            color = toneColor(meta.valueTone) ?: RecordTokens.Ink,
            fontSize = 13.sp,
            fontWeight = FontWeight.W700,
        )
    }
}

private fun toneColor(tone: RecordTone?): Color? = when (tone) {
    RecordTone.OK -> RecordTokens.BrandD
    RecordTone.WARN -> RecordTokens.Warn
    RecordTone.DANGER -> RecordTokens.Danger
    RecordTone.MUTED -> RecordTokens.Muted
    null -> null
}

@Composable
private fun StatusPill(text: String, tone: RecordTone) {
    val (bg, fg) = when (tone) {
        RecordTone.OK -> RecordTokens.OkX to RecordTokens.BrandD
        RecordTone.WARN -> RecordTokens.WarnX to RecordTokens.Warn
        RecordTone.DANGER -> RecordTokens.DangerX to RecordTokens.Danger
        RecordTone.MUTED -> RecordTokens.Surf3 to RecordTokens.Muted
    }
    Text(
        text = text,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .background(bg, shape = RoundedCornerShape(999.dp))
            .padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

@Preview(backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun RecordScreenShedPreview() {
    GoatOsTheme {
        RecordScreen(
            state = RecordUiState(
                title = "Castro 1 · Breeding does",
                subtitle = "Shed record · read-only",
                countLabel = "6",
                groups = listOf(
                    VaccineGroupRow(vaccine = "PPR · Booster", given = 6, due = 12, dose = "1 ml SC"),
                    VaccineGroupRow(vaccine = "Goat Pox", given = 0, due = 5, dose = "1 ml SC"),
                ),
                meta = listOf(
                    RecordMetaRow(label = "Operator · backup", value = "Arun Kumar · Indradev Kumar"),
                    RecordMetaRow(label = "Window · proof", value = "08:12–09:40 · video ✓"),
                ),
                statusLabel = "✓ Verified · video proof on file",
                statusTone = RecordTone.OK,
            ),
        )
    }
}

@Preview(name = "Drive record", backgroundColor = 0xFF0B100D, showBackground = true)
@Composable
private fun RecordScreenDrivePreview() {
    GoatOsTheme {
        RecordScreen(
            state = RecordUiState(
                title = "ET + TT · Primary",
                subtitle = "1 Jul 2026 · completed · read-only",
                groups = listOf(
                    VaccineGroupRow(vaccine = "ET + TT", given = 118, due = 120, dose = "1 ml SC"),
                ),
                meta = listOf(
                    RecordMetaRow(label = "Coverage", value = "98%", valueTone = RecordTone.OK),
                    RecordMetaRow(label = "Skipped", value = "2 · not-due / quarantine / sick"),
                    RecordMetaRow(label = "Park · sheds", value = "CBE · Gandhi 1 · Castro 1"),
                    RecordMetaRow(label = "Operator · backup", value = "Arun Kumar · Indradev Kumar"),
                ),
                statusLabel = "✓ Verified · video proof on file",
                statusTone = RecordTone.OK,
            ),
        )
    }
}
