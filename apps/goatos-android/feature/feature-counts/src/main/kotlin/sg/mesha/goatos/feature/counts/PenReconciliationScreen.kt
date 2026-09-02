package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; PenReconciliationViewModel (in :app) owns the
// counts_pen_reconciliation_* AnalyticsEvents + the CrashReporter non-fatal on every page-load
// failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
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
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The Herd Operations "Reconcile" home (`/counts/reconcile`, an L0 bottom-bar root) — the queue of
 * "wrong pen" cards raised by weighing submits (docs/decisions/pen-reconciliation.md). The herd
 * register is truth: each card says where the animal was FOUND and where it BELONGS, and the
 * operator physically walks it back.
 *
 * Read screen, offline-first (docs/decisions/android-offline-first.md): rows are a bounded Room-
 * backed Paging window (~20/page keyset) with disjoint backend status chips. Tapping an actionable
 * row opens the L1 execute screen where the operator records the mandatory return video and
 * submits. Completed / waiting-for-review rows are visible history, not tappable. Scrolling loads
 * more passively — never a "Load more" button.
 */

/** One Reconcile card row. Every field is backend-owned; the screen renders, never derives. */
@Immutable
data class PenReconciliationRowUi(
    val cardId: String,
    /** The tag exactly as scanned — what the operator reads on the animal's ear. */
    val scannedIdentifier: String,
    val goatDisplayId: String,
    /** Backend-composed pen label where the animal was scanned. Rendered verbatim. */
    val foundLabel: String,
    /** Backend-composed pen label the register says the animal lives in. Rendered verbatim. */
    val belongsLabel: String,
    val statusLabel: String = "",
    val statusTone: PenReconciliationTone = PenReconciliationTone.Neutral,
    val primaryActionKey: String = "none",
    val raisedAtLabel: String = "",
    /** The verifier's reason when evidence was rejected; rendered verbatim when present. */
    val reworkReason: String? = null,
)

/** Visual weight for a row's state pill: work to do, waiting on review, or finished. */
enum class PenReconciliationTone { Action, Waiting, Done, Neutral }

@Immutable
data class PenReconciliationStatusUi(
    val key: String,
    val label: String,
    val selected: Boolean,
    val count: Int = 0,
)

@Immutable
data class PenReconciliationUiState(
    val statuses: List<PenReconciliationStatusUi> = emptyList(),
    val submissionNotice: CountsWriteResultUi? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface PenReconciliationEvent {
    data object Refresh : PenReconciliationEvent
    data object Back : PenReconciliationEvent
    data class SelectStatus(val status: String) : PenReconciliationEvent
    data class OpenCard(val cardId: String) : PenReconciliationEvent
}

@Composable
fun PenReconciliationScreen(
    state: PenReconciliationUiState,
    rows: LazyPagingItems<PenReconciliationRowUi>,
    onEvent: (PenReconciliationEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(PenReconciliationEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Reconcile",
            subtitle = "Return animals to their registered pens",
            onBack = { onEvent(PenReconciliationEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(PenReconciliationEvent.Refresh) },
                )
            },
        )
        SyncStatusIndicator(
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = rows.itemCount > 0,
            isOffline = state.isOffline,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp),
        )
        state.submissionNotice?.let { notice ->
            CountsResultBanner(
                result = notice,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp),
            )
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "status") { PenReconciliationStatusBar(state.statuses, onEvent) }

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

            items(count = rows.itemCount, key = rows.itemKey { it.cardId }) { index ->
                rows[index]?.let { row ->
                    PenReconciliationRowCard(row) {
                        if (row.primaryActionKey == "execute") onEvent(PenReconciliationEvent.OpenCard(row.cardId))
                    }
                }
            }
        }
    }
}

@Composable
private fun PenReconciliationStatusBar(
    statuses: List<PenReconciliationStatusUi>,
    onEvent: (PenReconciliationEvent) -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        statuses.forEach { status ->
            Text(
                "${status.label} ${status.count}",
                color = if (status.selected) MeshaColors.OnBrand else MeshaColors.Muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.clip(RoundedCornerShape(999.dp))
                    .background(if (status.selected) MeshaColors.Brand else MeshaColors.Surf2)
                    .clickable { onEvent(PenReconciliationEvent.SelectStatus(status.key)) }
                    .padding(horizontal = 14.dp, vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun PenReconciliationRowCard(row: PenReconciliationRowUi, onClick: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(enabled = row.primaryActionKey == "execute", onClick = onClick)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            // The scanned tag leads: it is what the operator reads on the animal's ear to find it.
            Text(
                text = row.scannedIdentifier,
                color = MeshaColors.Ink,
                fontSize = 15.sp,
                fontWeight = FontWeight.W800,
                modifier = Modifier.weight(1f),
            )
            if (row.goatDisplayId.isNotBlank()) {
                Text(row.goatDisplayId, color = MeshaColors.Faint, fontSize = 12.sp)
            }
        }
        LocationLine(label = "Found in", value = row.foundLabel, valueColor = MeshaColors.Warn)
        LocationLine(label = "Belongs in", value = row.belongsLabel, valueColor = MeshaColors.Ok)
        row.reworkReason?.takeIf { it.isNotBlank() }?.let { reason ->
            // The verifier's own words, verbatim — never composed on the phone.
            Text(
                text = reason,
                color = MeshaColors.Warn,
                fontSize = 12.sp,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(10.dp))
                    .background(MeshaColors.WarnX)
                    .padding(horizontal = 10.dp, vertical = 8.dp),
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (row.statusLabel.isNotBlank()) {
                val (bg, fg) = when (row.statusTone) {
                    PenReconciliationTone.Action -> MeshaColors.WarnX to MeshaColors.Warn
                    PenReconciliationTone.Waiting -> MeshaColors.Surf3 to MeshaColors.Muted
                    PenReconciliationTone.Done -> MeshaColors.OkX to MeshaColors.Ok
                    PenReconciliationTone.Neutral -> MeshaColors.Surf3 to MeshaColors.Muted
                }
                PenReconciliationPill(row.statusLabel, bg, fg)
            }
            if (row.raisedAtLabel.isNotBlank()) {
                Text(row.raisedAtLabel, color = MeshaColors.Faint, fontSize = 11.sp)
            }
        }
    }
}

@Composable
private fun LocationLine(label: String, value: String, valueColor: androidx.compose.ui.graphics.Color) {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(
            text = label,
            color = MeshaColors.Muted,
            fontSize = 11.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(end = 8.dp),
        )
        Icon(
            imageVector = MeshaIcons.ArrowUpDown,
            contentDescription = null,
            tint = MeshaColors.Faint,
            modifier = Modifier.size(12.dp),
        )
        Text(
            text = value,
            color = valueColor,
            fontSize = 13.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(start = 8.dp),
        )
    }
}

@Composable
private fun PenReconciliationPill(text: String, bg: androidx.compose.ui.graphics.Color, fg: androidx.compose.ui.graphics.Color) {
    if (text.isBlank()) return
    Text(
        text = text,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}
