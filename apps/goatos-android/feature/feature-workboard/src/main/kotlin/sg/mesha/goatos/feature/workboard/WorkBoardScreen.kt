package sg.mesha.goatos.feature.workboard

// telemetry:exempt pure stateless renderer; WorkBoardViewModel (in :app) owns the
// work_board_* AnalyticsEventsWorkBoard + CrashReporter wiring for every refresh, filter change
// and row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The Work Board's My Work L0 screen (`/work`, maintainer decision 2026-09-10): every module's
 * work for one park and one business day, already scoped by the backend to what the caller may
 * see. The screen only renders rows — it never filters by role and never derives a lane.
 *
 * Paged (~20 rows) with stable `row_key` keys and a PASSIVE loading footer — never a "Load more"
 * button (docs/decisions/mobile-data-fetch-anti-patterns.md). The summary tiles and every chip
 * count come from `/work-board/summary`, never from the page in memory. The one header action
 * refreshes THIS screen and travels nowhere.
 */
@Composable
fun WorkBoardScreen(
    state: WorkBoardUiState,
    rows: LazyPagingItems<WorkBoardRowUi>,
    onEvent: (WorkBoardEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Refresh-on-open (docs/decisions/android-offline-first.md): cached Room rows show instantly
    // and a background refresh fires on every resume.
    RefreshOnResume { onEvent(WorkBoardEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title.ifBlank { stringResource(R.string.work_board_title) },
            subtitle = if (state.ownRowsOnly) stringResource(R.string.work_board_own_rows_note) else null,
            below = {
                SyncStatusIndicator(
                    isRefreshing = state.isRefreshing,
                    lastSyncedAt = state.lastSyncedAt,
                    hasData = state.hasSummary || rows.itemCount > 0,
                    isOffline = state.isOffline,
                )
            },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(WorkBoardEvent.Refresh) },
                    contentDescription = stringResource(R.string.work_board_action_refresh),
                )
            },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "date_bar") { WorkBoardDateBar(state, onEvent) }
            item(key = "lanes") {
                WorkBoardChipRow(
                    chips = state.lanes,
                    label = { laneLabel(it) },
                    onSelect = { onEvent(WorkBoardEvent.SelectLane(it)) },
                )
            }
            if (state.modules.size > 2) {
                // One module alone offers no choice; the row appears once there are two to switch between.
                item(key = "modules") {
                    WorkBoardChipRow(
                        chips = state.modules,
                        label = { moduleLabel(it) },
                        onSelect = { onEvent(WorkBoardEvent.SelectModule(it)) },
                    )
                }
            }
            item(key = "summary") { WorkBoardSummaryTiles(state) }

            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    val isError = state.emptyMessage == WorkBoardEmptyMessage.ERROR
                    EmptyState(
                        title = when (state.emptyMessage) {
                            WorkBoardEmptyMessage.LOADING -> stringResource(R.string.work_board_loading)
                            WorkBoardEmptyMessage.EMPTY_OWN -> stringResource(R.string.work_board_empty_own)
                            WorkBoardEmptyMessage.EMPTY_ALL -> stringResource(R.string.work_board_empty_all)
                            WorkBoardEmptyMessage.ERROR -> stringResource(R.string.work_board_error)
                        },
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (isError) MeshaIcons.Warn else MeshaIcons.ClipboardCheck,
                        tone = if (isError) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            // Keyed per ROW (row_key = module|source_type|source_id), never per entity: one pen or
            // one animal can sit on several rows across modules.
            items(count = rows.itemCount, key = rows.itemKey { it.rowKey }) { index ->
                rows[index]?.let { row ->
                    WorkBoardRowCard(row) { onEvent(WorkBoardEvent.OpenRow(row.rowKey)) }
                }
            }

            // Passive loading footer: the next page is already in flight while this spins.
            if (rows.loadState.append is LoadState.Loading) {
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

/** The business-day stepper: previous / label / next, with a quiet "Today" mark. */
@Composable
private fun WorkBoardDateBar(state: WorkBoardUiState, onEvent: (WorkBoardEvent) -> Unit) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = { onEvent(WorkBoardEvent.PreviousDay) }) {
            Icon(
                imageVector = MeshaIcons.ChevronLeft,
                contentDescription = stringResource(R.string.work_board_previous_day),
                tint = MeshaColors.Ink,
            )
        }
        Column(
            modifier = Modifier.weight(1f),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(text = state.dateLabel, color = MeshaColors.Ink, style = MeshaType.cardTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (state.isToday) {
                Text(text = stringResource(R.string.work_board_today), color = MeshaColors.BrandD, style = MeshaType.caption)
            }
        }
        IconButton(onClick = { onEvent(WorkBoardEvent.NextDay) }) {
            Icon(
                imageVector = MeshaIcons.Chevron,
                contentDescription = stringResource(R.string.work_board_next_day),
                tint = MeshaColors.Ink,
            )
        }
    }
}

/** Done / Pending / Needs attention — WHOLE-FILTER counts from the summary. */
@Composable
private fun WorkBoardSummaryTiles(state: WorkBoardUiState) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(IntrinsicSize.Min),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        WorkBoardStatTile(
            label = stringResource(R.string.work_board_tile_done),
            value = if (state.hasSummary) state.doneCount.toString() else "–",
            accent = MeshaColors.Ok,
            modifier = Modifier.weight(1f),
        )
        WorkBoardStatTile(
            label = stringResource(R.string.work_board_tile_pending),
            value = if (state.hasSummary) state.pendingCount.toString() else "–",
            accent = MeshaColors.BrandD,
            modifier = Modifier.weight(1f),
        )
        WorkBoardStatTile(
            label = stringResource(R.string.work_board_tile_attention),
            value = if (state.hasSummary) state.needsAttentionCount.toString() else "–",
            accent = if (state.needsAttentionCount > 0) MeshaColors.Danger else MeshaColors.Muted,
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
internal fun WorkBoardRowCard(row: WorkBoardRowUi, onOpen: () -> Unit) {
    Row(
        modifier = workBoardCardModifier(onClick = onOpen),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box(
            modifier = Modifier
                .size(40.dp)
                .background(laneAccent(row.lane).copy(alpha = 0.14f), androidx.compose.foundation.shape.RoundedCornerShape(12.dp)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = if (row.lane == "done") MeshaIcons.CheckCircle else moduleIcon(row.module),
                contentDescription = null,
                tint = laneAccent(row.lane),
                modifier = Modifier.size(20.dp),
            )
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(text = moduleLabel(row.module), color = MeshaColors.Faint, style = MeshaType.caption, maxLines = 1)
            // Backend-composed title, verbatim, on its own full-width line: a long farm title and
            // a long clock label must never squeeze each other into "Pack Godel…".
            Text(
                text = row.title,
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                // The clock in farm wording, tinted by the server's severity, beside the state.
                WorkBoardChip(label = row.clockLabel, accent = severityAccent(row.severity))
                WorkBoardChip(label = workStateLabel(row.workState, row.lane), accent = laneAccent(row.lane))
            }
            if (row.subtitle.isNotBlank()) {
                Text(
                    text = row.subtitle,
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            // Where the pending work is, when the source said (the feed cards): the same split the
            // web card and drawer show, so no surface calls an unfilmed pen "started".
            if (row.hasPendingSplit) {
                Text(
                    text = pendingSplitLine(row),
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            // The pen display, verbatim — never composed here.
            if (row.penLabel.isNotBlank()) {
                Text(
                    text = row.penLabel,
                    color = MeshaColors.Ink,
                    style = MeshaType.pillStrong,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = ownerLine(row),
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (row.total > 0) {
                    Text(
                        text = stringResource(R.string.work_board_counts_fmt, row.done, row.total),
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                        maxLines = 1,
                    )
                }
            }
        }
    }
}
