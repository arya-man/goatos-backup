package sg.mesha.goatos.feature.verify

import androidx.compose.foundation.background
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
import androidx.compose.foundation.border
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

// telemetry:exempt: pure stateless renderer — AnalyticsPort/funnel wiring lives in
// VerifyQueueViewModel (:app), which owns every side effect this screen triggers.
/**
 * The standalone Verifier section's queue (context/architecture/verifier-app-and-flow.md): a
 * verifier who opens the app sees ONLY this — a category-filtered queue of pending media to
 * verify. Row tap drills into [VerifyDetailScreen] (video playback + approve/reject). No
 * capture, no ops, no roster, no config — this + the detail screen are the entire section.
 */

/** Status tone for a queue row's pill and the detail screen's header pill. */
enum class VerifyTone { PENDING, APPROVED, REJECTED }

/** One row in the queue — [categoryLabel]/[title]/[subtitle] are backend-composed display
 *  strings (TRD dumb-renderer rule); the client never derives them from raw ids. [statusTone]
 *  is the one client-owned bit (pending/approved/rejected) — its localized pill TEXT is
 *  resolved by [StatusPill] from a client string resource, never a hardcoded English literal. */
data class VerificationQueueRow(
    val id: String,
    val category: String,
    val categoryLabel: String,
    val title: String,
    val subtitle: String,
    val statusTone: VerifyTone,
)

/** A category filter chip. [value] is the raw category key sent to the backend
 *  (`null` = every category this verifier is assigned, [label] then `null` so the Screen
 *  substitutes the localized "All" chrome string — the one label here that is NOT backend
 *  data); a non-null [value] always carries a non-null [label]. Built by the ViewModel from
 *  the distinct categories the backend has actually returned for this verifier — never a
 *  client-hardcoded category enum (categories are a plug-and-play registry per
 *  verification-module-design.md §2.3). */
data class VerifyCategoryOption(val value: String?, val label: String?)

enum class VerifyModuleTab { VACCINATION, COUNTS, FEED_DIRECTION }

@Immutable
data class VerifyQueueUiState(
    val rows: List<VerificationQueueRow> = emptyList(),
    val selectedModule: VerifyModuleTab = VerifyModuleTab.VACCINATION,
    val isActionQueue: Boolean = false,
    val categoryOptions: List<VerifyCategoryOption> = emptyList(),
    val selectedCategory: String? = null,
    // Offline-first sync state (docs/decisions/android-offline-first.md).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    // Keyset pagination (~20/page) — see docs/decisions/mobile-data-fetch-anti-patterns.md.
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
)

sealed interface VerifyQueueEvent {
    data class SelectCategory(val category: String?) : VerifyQueueEvent
    /** [category] is the tapped row's OWN category (never the queue's filter selection) — the
     *  nav host threads it into the detail route so that screen re-observes the exact same Room
     *  cache scope this row came from, with no extra network call. */
    data class OpenItem(val itemId: String, val category: String) : VerifyQueueEvent
    data object Refresh : VerifyQueueEvent
    data object LoadMore : VerifyQueueEvent
    data class SelectModule(val module: VerifyModuleTab) : VerifyQueueEvent
}

@Composable
fun VerifyQueueScreen(
    state: VerifyQueueUiState,
    onEvent: (VerifyQueueEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(VerifyQueueEvent.Refresh) }
    val listState = rememberLazyListState()
    LaunchedEffect(listState, state.hasMore, state.isLoadingMore, state.rows.size, state.selectedModule) {
        if (
            state.selectedModule != VerifyModuleTab.VACCINATION ||
            !state.hasMore ||
            state.isLoadingMore ||
            state.rows.isEmpty()
        ) {
            return@LaunchedEffect
        }
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { lastVisibleIndex ->
                if (lastVisibleIndex >= state.rows.lastIndex - 3 && state.hasMore && !state.isLoadingMore) {
                    onEvent(VerifyQueueEvent.LoadMore)
                }
            }
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        QueueHeader(state = state, onRefresh = { onEvent(VerifyQueueEvent.Refresh) })
        if (!state.isActionQueue) {
            ModuleTabs(
                selected = state.selectedModule,
                onSelect = { onEvent(VerifyQueueEvent.SelectModule(it)) },
            )
        }
        if (state.categoryOptions.size > 1) {
            CategoryFilterRow(
                options = state.categoryOptions,
                selected = state.selectedCategory,
                onSelect = { onEvent(VerifyQueueEvent.SelectCategory(it)) },
            )
        }
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        ) {
            if (state.selectedModule != VerifyModuleTab.VACCINATION) {
                item {
                    EmptyState(
                        title = stringResource(R.string.verify_module_under_construction),
                        subtitle = stringResource(R.string.verify_module_under_construction_subtitle),
                        icon = MeshaIcons.Video,
                        tone = EmptyTone.Neutral,
                    )
                }
            } else if (state.rows.isEmpty() && state.isRefreshing && state.lastSyncedAt == null) {
                item { LoadingSkeletonList(modifier = Modifier.fillMaxWidth()) }
            } else if (state.rows.isEmpty()) {
                item {
                    EmptyState(
                        title = stringResource(if (state.isActionQueue) R.string.verify_action_queue_empty else R.string.verify_queue_empty),
                        subtitle = stringResource(if (state.isActionQueue) R.string.verify_action_queue_empty_subtitle else R.string.verify_queue_empty_subtitle),
                        icon = MeshaIcons.Video,
                        tone = EmptyTone.Positive,
                    )
                }
            } else {
                items(state.rows, key = { it.id }) { row ->
                    QueueRowCard(row = row, onClick = { onEvent(VerifyQueueEvent.OpenItem(row.id, row.category)) })
                }
                if (state.isLoadingMore) {
                    item {
                        InlineLoadingFooter()
                    }
                }
            }
            item { Spacer(Modifier.size(24.dp)) }
        }
    }
}

@Composable
private fun ModuleTabs(
    selected: VerifyModuleTab,
    onSelect: (VerifyModuleTab) -> Unit,
) {
    val tabs = listOf(
        VerifyModuleTab.VACCINATION to stringResource(R.string.verify_module_vaccination),
        VerifyModuleTab.COUNTS to stringResource(R.string.verify_module_counts),
        VerifyModuleTab.FEED_DIRECTION to stringResource(R.string.verify_module_feed_direction),
    )
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.padding(bottom = 10.dp),
    ) {
        items(tabs, key = { it.first.name }) { (tab, label) ->
            CategoryChip(label = label, selected = tab == selected, onClick = { onSelect(tab) })
        }
    }
}

@Composable
private fun QueueHeader(state: VerifyQueueUiState, onRefresh: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                text = stringResource(if (state.isActionQueue) R.string.verify_action_queue_title else R.string.verify_queue_title),
                color = MeshaColors.Ink,
                fontSize = 20.sp,
                fontWeight = FontWeight.W800,
            )
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                hasData = state.lastSyncedAt != null || state.rows.isNotEmpty(),
                isOffline = state.isOffline,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        SyncIconButton(
            isSyncing = state.isRefreshing,
            onSync = onRefresh,
            contentDescription = stringResource(R.string.verify_queue_refresh),
        )
    }
}

@Composable
private fun CategoryFilterRow(
    options: List<VerifyCategoryOption>,
    selected: String?,
    onSelect: (String?) -> Unit,
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.padding(bottom = 8.dp),
    ) {
        items(options, key = { it.value ?: "__all__" }) { option ->
            CategoryChip(
                label = option.label ?: stringResource(R.string.verify_category_all),
                selected = option.value == selected,
                onClick = { onSelect(option.value) },
            )
        }
    }
}

@Composable
private fun CategoryChip(label: String, selected: Boolean, onClick: () -> Unit) {
    val bg = if (selected) MeshaColors.BrandTint else MeshaColors.Surf2
    val fg = if (selected) MeshaColors.BrandD else MeshaColors.Muted
    val border = if (selected) MeshaColors.Brand else MeshaColors.Hair
    Text(
        text = label,
        color = fg,
        fontSize = 12.5.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    )
}

@Composable
private fun QueueRowCard(row: VerificationQueueRow, onClick: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 10.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(
                modifier = Modifier
                    .size(40.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Video,
                    contentDescription = null,
                    tint = MeshaColors.Brand,
                    modifier = Modifier.size(18.dp),
                )
            }
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(
                    text = row.title,
                    color = MeshaColors.Ink,
                    fontSize = 14.5.sp,
                    fontWeight = FontWeight.W700,
                )
                Text(
                    text = row.subtitle,
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            StatusPill(tone = row.statusTone)
        }
        Text(
            text = row.categoryLabel,
            color = MeshaColors.Faint,
            fontSize = 11.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(top = 10.dp),
        )
    }
}

/** Resolves the LOCALIZED status label for [tone] — the pill text is never a hardcoded
 *  English literal from the ViewModel; only the tone (pending/approved/rejected) is data. */
@Composable
private fun statusToneLabel(tone: VerifyTone): String = when (tone) {
    VerifyTone.PENDING -> stringResource(R.string.verify_status_pending)
    VerifyTone.APPROVED -> stringResource(R.string.verify_status_approved)
    VerifyTone.REJECTED -> stringResource(R.string.verify_status_rejected)
}

@Composable
internal fun StatusPill(tone: VerifyTone) {
    val (bg, fg) = when (tone) {
        VerifyTone.PENDING -> MeshaColors.WarnX to MeshaColors.Warn
        VerifyTone.APPROVED -> MeshaColors.OkX to MeshaColors.Ok
        VerifyTone.REJECTED -> MeshaColors.DangerX to MeshaColors.Danger
    }
    Text(
        text = statusToneLabel(tone),
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

@Composable
private fun InlineLoadingFooter() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 6.dp)
            .padding(vertical = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(modifier = Modifier.size(16.dp), color = MeshaColors.Muted, strokeWidth = 2.dp)
    }
}
