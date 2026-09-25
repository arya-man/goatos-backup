package sg.mesha.goatos.feature.workboard

// telemetry:exempt pure stateless renderer; WorkBoardDetailViewModel (in :app) owns the
// work_board_* AnalyticsEventsWorkBoard + CrashReporter wiring for the row open.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume

/**
 * ONE board row (`/work/item/{rowKey}`) — a hosted drill with Up/Back and NO L0 chrome (Android
 * navigation-stack invariant). It renders the row Room already holds; it never fetches.
 *
 * The "Open" button exists ONLY when the row carries an `href` this build can route to; a row
 * whose module has no phone screen yet shows no button at all rather than a dead one.
 */
@Composable
fun WorkBoardDetailScreen(
    state: WorkBoardDetailUiState,
    onEvent: (WorkBoardDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val row = state.row
    // Refresh-on-open: cached subtasks render at once and a background read follows every time
    // the reader lands on or returns to the row.
    RefreshOnResume { onEvent(WorkBoardDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = row?.title?.ifBlank { null } ?: stringResource(R.string.work_board_title),
            subtitle = row?.let { moduleLabel(it.module) },
            onBack = { onEvent(WorkBoardDetailEvent.Back) },
        )
        if (state.loading) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = MeshaColors.BrandD)
            }
            return@Column
        }
        if (row == null) {
            EmptyState(
                title = stringResource(R.string.work_board_detail_missing),
                modifier = Modifier.fillMaxWidth().padding(16.dp),
                icon = MeshaIcons.Warn,
                tone = EmptyTone.Warn,
            )
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item(key = "summary") {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(IntrinsicSize.Min),
                    horizontalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    WorkBoardStatTile(
                        label = stringResource(R.string.work_board_tile_done),
                        value = row.done.toString(),
                        accent = MeshaColors.Ok,
                        modifier = Modifier.weight(1f),
                    )
                    WorkBoardStatTile(
                        label = stringResource(R.string.work_board_tile_pending),
                        value = row.pending.toString(),
                        accent = MeshaColors.BrandD,
                        modifier = Modifier.weight(1f),
                    )
                    WorkBoardStatTile(
                        label = stringResource(R.string.work_board_tile_attention),
                        value = row.needsAttention.toString(),
                        accent = if (row.needsAttention > 0) MeshaColors.Danger else MeshaColors.Muted,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
            item(key = "row") {
                Column(
                    modifier = workBoardCardModifier(),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        WorkBoardChip(label = workStateLabel(row.workState, row.lane), accent = laneAccent(row.lane))
                        Spacer(Modifier.weight(1f))
                        WorkBoardChip(label = row.clockLabel, accent = severityAccent(row.severity))
                    }
                    if (row.subtitle.isNotBlank()) {
                        Text(text = row.subtitle, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
                    }
                    WorkBoardFactLine(label = stringResource(R.string.work_board_detail_pen), value = row.penLabel)
                    WorkBoardFactLine(label = stringResource(R.string.work_board_detail_park), value = row.parkName)
                    WorkBoardFactLine(label = stringResource(R.string.work_board_detail_owner), value = ownerLine(row))
                    WorkBoardFactLine(label = stringResource(R.string.work_board_detail_lane), value = laneLabel(row.lane))
                }
            }
            item(key = "subtasks_head") {
                Row(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(
                        text = stringResource(R.string.work_board_subtasks_title),
                        color = MeshaColors.Ink,
                        style = MeshaType.bodyStrong,
                        modifier = Modifier.weight(1f),
                    )
                    if (state.subtaskTotal > 0) {
                        Text(
                            text = stringResource(R.string.work_board_subtasks_count_fmt, state.subtasks.size, state.subtaskTotal),
                            color = MeshaColors.Muted,
                            style = MeshaType.caption,
                        )
                    }
                }
            }
            when {
                state.subtasks.isNotEmpty() -> {
                    items(count = state.subtasks.size, key = { i -> "subtask:" + state.subtasks[i].key }) { i ->
                        WorkBoardSubtaskCard(state.subtasks[i])
                        // Infinite scroll, never a "Load more" row: the next page is asked for
                        // while three units are still below the fold.
                        if (i >= state.subtasks.size - 3 && state.hasMoreSubtasks) {
                            LaunchedEffect(state.subtasks.size) { onEvent(WorkBoardDetailEvent.LoadMoreSubtasks) }
                        }
                    }
                }
                state.subtasksFailed -> item(key = "subtasks_failed") {
                    Text(
                        text = stringResource(R.string.work_board_subtasks_failed),
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
                state.subtasksLoading -> item(key = "subtasks_loading") {
                    Text(
                        text = stringResource(R.string.work_board_subtasks_loading),
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
                else -> item(key = "subtasks_empty") {
                    Text(
                        text = stringResource(R.string.work_board_subtasks_empty),
                        color = MeshaColors.Muted,
                        style = MeshaType.caption,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
            }
            if (state.canOpen) {
                item(key = "open") {
                    WorkBoardPrimaryButton(
                        label = stringResource(R.string.work_board_action_open),
                        enabled = true,
                        onClick = { onEvent(WorkBoardDetailEvent.Open) },
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = MeshaIcons.Chevron,
                    )
                }
            }
        }
    }
}

/** One unit of the row's work: its name, the backend's line about it, its state, and its steps. */
@Composable
private fun WorkBoardSubtaskCard(subtask: WorkBoardSubtaskUi) {
    Column(
        modifier = workBoardCardModifier(),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = subtask.name,
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
                modifier = Modifier.weight(1f),
            )
            WorkBoardChip(
                label = workStateLabel(subtask.workState, subtask.lane),
                accent = if (subtask.needsAttention) MeshaColors.Danger else laneAccent(subtask.lane),
            )
        }
        if (subtask.subtitle.isNotBlank()) {
            Text(text = subtask.subtitle, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        if (subtask.steps.isNotEmpty()) {
            // Each step's backend detail rides beside it (a weight, a submit time), as the web
            // drawer shows it; a sent-back step's detail is the verifier's reason, on its own line.
            val stepLines = subtask.steps.map { step ->
                val base = step.name + ": " + stepStateLabel(step.state)
                if (step.detail.isNotBlank() && step.state != "rework") "$base (${step.detail})" else base
            }
            Text(
                text = stepLines.joinToString(" · "),
                color = MeshaColors.Muted,
                style = MeshaType.caption,
            )
            subtask.steps.filter { it.state == "rework" && it.detail.isNotBlank() }.forEach { step ->
                Text(text = step.detail, color = MeshaColors.Danger, style = MeshaType.caption)
            }
        }
        if (subtask.ownerName.isNotBlank()) {
            Text(text = subtask.ownerName, color = MeshaColors.Muted, style = MeshaType.caption)
        }
    }
}
