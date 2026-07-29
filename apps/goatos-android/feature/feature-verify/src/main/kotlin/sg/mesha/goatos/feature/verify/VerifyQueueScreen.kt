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
import androidx.compose.foundation.layout.height
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
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
    val scopeType: VerifyScopeType = VerifyScopeType.OTHER,
    val shedLabel: String = "",
    val animalLabel: String = "",
    val weightLabel: String = "",
    val mediaCountLabel: String = "",
    val parkLabel: String = "",
    val operatorLabel: String = "",
    val capturedAtLabel: String = "",
    val statusTone: VerifyTone,
)

enum class VerifyScopeType { INDIVIDUAL, LUMP_SUM, OTHER }

data class VerifyDriveClosure(
    val batchId: String,
    val driveLabel: String,
    val batchLabel: String,
    val totalCount: Int,
    val approvedCount: Int,
    val rejectedCount: Int,
    val pendingCount: Int,
    val videoCount: Int,
    val approvedVideos: Int,
    val rejectedVideos: Int,
    val pendingVideos: Int,
    val shedCount: Int,
    val ready: Boolean,
)

/** A category filter chip. [value] is the raw category key sent to the backend
 *  (`null` = every category this verifier is assigned, [label] then `null` so the Screen
 *  substitutes the localized "All" chrome string — the one label here that is NOT backend
 *  data); a non-null [value] always carries a non-null [label]. Built by the ViewModel from
 *  the distinct categories the backend has actually returned for this verifier — never a
 *  client-hardcoded category enum (categories are a plug-and-play registry per
 *  verification-module-design.md §2.3). */
data class VerifyCategoryOption(val value: String?, val label: String?)

data class VerifyLocationFilterOption(val value: String?, val label: String)

enum class VerifyModuleTab { VACCINATION, WEIGHING }

@Immutable
data class VerifyQueueUiState(
    val rows: List<VerificationQueueRow> = emptyList(),
    val selectedModule: VerifyModuleTab = VerifyModuleTab.VACCINATION,
    val isActionQueue: Boolean = false,
    val categoryOptions: List<VerifyCategoryOption> = emptyList(),
    val selectedCategory: String? = null,
    val parkOptions: List<VerifyLocationFilterOption> = emptyList(),
    val selectedParkId: String? = null,
    val shedOptions: List<VerifyLocationFilterOption> = emptyList(),
    val selectedShedId: String? = null,
    // Offline-first sync state (docs/decisions/android-offline-first.md).
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    // Keyset pagination (~20/page) — see docs/decisions/mobile-data-fetch-anti-patterns.md.
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
    val driveClosures: List<VerifyDriveClosure> = emptyList(),
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
)

sealed interface VerifyQueueEvent {
    data class SelectCategory(val category: String?) : VerifyQueueEvent
    data class SelectPark(val parkId: String?) : VerifyQueueEvent
    data class SelectShed(val shedId: String?) : VerifyQueueEvent
    /** [category] is the tapped row's OWN category (never the queue's filter selection) — the
     *  nav host threads it into the detail route so that screen re-observes the exact same Room
     *  cache scope this row came from, with no extra network call. */
    data class OpenItem(val itemId: String, val category: String) : VerifyQueueEvent
    data object Refresh : VerifyQueueEvent
    data object LoadMore : VerifyQueueEvent
    data class SelectModule(val module: VerifyModuleTab) : VerifyQueueEvent
    data class CloseDrive(val batchId: String) : VerifyQueueEvent
}

@Composable
fun VerifyQueueScreen(
    state: VerifyQueueUiState,
    onEvent: (VerifyQueueEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(VerifyQueueEvent.Refresh) }
    val listState = rememberLazyListState()
    var selectedWeighingScope by remember { mutableStateOf<VerifyScopeType?>(null) }
    LaunchedEffect(listState, state.hasMore, state.isLoadingMore, state.rows.size, state.selectedModule) {
        if (
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
        if (state.selectedModule == VerifyModuleTab.WEIGHING) {
            WeighingScopeTabs(
                rows = state.rows,
                selected = selectedWeighingScope,
                onSelect = { selectedWeighingScope = it },
            )
        }
        if (state.categoryOptions.size > 1) {
            CategoryFilterRow(
                options = state.categoryOptions,
                selected = state.selectedCategory,
                onSelect = { onEvent(VerifyQueueEvent.SelectCategory(it)) },
            )
        }
        if (state.parkOptions.size > 1) {
            LocationFilterRow(
                options = state.parkOptions,
                selected = state.selectedParkId,
                onSelect = { onEvent(VerifyQueueEvent.SelectPark(it)) },
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        if (state.shedOptions.size > 1) {
            LocationFilterRow(
                options = state.shedOptions,
                selected = state.selectedShedId,
                onSelect = { onEvent(VerifyQueueEvent.SelectShed(it)) },
                modifier = Modifier.padding(bottom = 8.dp),
            )
        }
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        ) {
            if (state.isActionQueue) {
                items(state.driveClosures, key = { it.batchId }) { closure ->
                    DriveCloseCard(
                        closure = closure,
                        isClosing = state.closingBatchId == closure.batchId,
                        errorMessage = state.closeErrorMessage.takeIf { state.closeErrorBatchId == closure.batchId },
                        onClose = { onEvent(VerifyQueueEvent.CloseDrive(closure.batchId)) },
                    )
                }
            }
            if (state.rows.isEmpty() && state.isRefreshing && state.lastSyncedAt == null) {
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
                if (state.selectedModule == VerifyModuleTab.WEIGHING) {
                    val visibleRows = if (selectedWeighingScope != null) {
                        state.rows.filter { it.scopeType == selectedWeighingScope }
                    } else {
                        state.rows
                    }
                    val shedGroups = visibleRows.groupBy { it.shedLabel.ifBlank { it.title.ifBlank { it.categoryLabel } } }
                    shedGroups.forEach { (shedLabel, rows) ->
                        item(key = "shed-$shedLabel") {
                            ShedGroupHeader(shedLabel = shedLabel, rows = rows)
                        }
                        items(rows, key = { it.id }) { row ->
                            QueueRowCard(row = row, hierarchical = true, onClick = { onEvent(VerifyQueueEvent.OpenItem(row.id, row.category)) })
                        }
                    }
                } else {
                    items(state.rows, key = { it.id }) { row ->
                        QueueRowCard(row = row, hierarchical = false, onClick = { onEvent(VerifyQueueEvent.OpenItem(row.id, row.category)) })
                    }
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
private fun DriveCloseCard(
    closure: VerifyDriveClosure,
    isClosing: Boolean,
    errorMessage: String?,
    onClose: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 10.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.OkX)
            .border(1.dp, MeshaColors.Ok.copy(alpha = 0.35f), RoundedCornerShape(16.dp))
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    text = closure.batchLabel.ifBlank { stringResource(R.string.verify_drive_ready_title) },
                    color = MeshaColors.Ink,
                    fontSize = 14.5.sp,
                    fontWeight = FontWeight.W800,
                )
                if (closure.driveLabel.isNotBlank()) {
                    Text(
                        text = closure.driveLabel,
                        color = MeshaColors.Muted,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.W700,
                        modifier = Modifier.padding(top = 2.dp),
                    )
                }
                Text(
                    text = stringResource(
                        R.string.verify_drive_ready_subtitle,
                        closure.totalCount,
                        closure.shedCount,
                        closure.approvedVideos,
                        closure.videoCount,
                    ),
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
                Text(
                    text = stringResource(
                        R.string.verify_drive_ready_detail,
                        closure.pendingVideos,
                        closure.rejectedVideos,
                    ),
                    color = MeshaColors.Faint,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Ok)
                    .clickable(enabled = !isClosing, onClick = onClose)
                    .padding(horizontal = 12.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (isClosing) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), color = MeshaColors.Surf, strokeWidth = 2.dp)
                } else {
                    Icon(MeshaIcons.CheckCircle, contentDescription = null, tint = MeshaColors.Surf, modifier = Modifier.size(16.dp))
                    Spacer(Modifier.size(6.dp))
                    Text(
                        text = stringResource(R.string.verify_drive_close),
                        color = MeshaColors.Surf,
                        fontSize = 12.5.sp,
                        fontWeight = FontWeight.W800,
                    )
                }
            }
        }
        errorMessage?.let {
            Text(
                text = it,
                color = MeshaColors.Danger,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
                modifier = Modifier.padding(top = 8.dp),
            )
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
        VerifyModuleTab.WEIGHING to stringResource(R.string.verify_module_weighing),
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
private fun WeighingScopeTabs(
    rows: List<VerificationQueueRow>,
    selected: VerifyScopeType?,
    onSelect: (VerifyScopeType?) -> Unit,
) {
    val individualCount = rows.count { it.scopeType == VerifyScopeType.INDIVIDUAL }
    val lumpSumCount = rows.count { it.scopeType == VerifyScopeType.LUMP_SUM }
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.padding(bottom = 8.dp),
    ) {
        item {
            CategoryChip(
                label = stringResource(R.string.verify_scope_all),
                selected = selected == null,
                onClick = { onSelect(null) },
            )
        }
        item {
            CategoryChip(
                label = stringResource(R.string.verify_scope_individual, individualCount),
                selected = selected == VerifyScopeType.INDIVIDUAL,
                onClick = { onSelect(VerifyScopeType.INDIVIDUAL) },
            )
        }
        item {
            CategoryChip(
                label = stringResource(R.string.verify_scope_lumpsum, lumpSumCount),
                selected = selected == VerifyScopeType.LUMP_SUM,
                onClick = { onSelect(VerifyScopeType.LUMP_SUM) },
            )
        }
    }
}

@Composable
private fun ShedGroupHeader(shedLabel: String, rows: List<VerificationQueueRow>) {
    val videoCount = rows.sumOf { row ->
        row.mediaCountLabel.substringBefore(' ').toIntOrNull() ?: 0
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 10.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                text = shedLabel,
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W800,
            )
            Text(
                text = stringResource(R.string.verify_shed_group_summary, rows.size, videoCount),
                color = MeshaColors.Faint,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(top = 1.dp),
            )
        }
        Box(
            modifier = Modifier
                .height(1.dp)
                .weight(1f)
                .background(MeshaColors.Hair),
        )
    }
}

@Composable
private fun QueueHeader(state: VerifyQueueUiState, onRefresh: () -> Unit) {
    MeshaScreenHeader(
        title = stringResource(if (state.isActionQueue) R.string.verify_action_queue_title else R.string.verify_queue_title),
        below = {
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                hasData = state.lastSyncedAt != null || state.rows.isNotEmpty(),
                isOffline = state.isOffline,
                modifier = Modifier.padding(top = 2.dp),
            )
        },
        actions = {
            SyncIconButton(
                isSyncing = state.isRefreshing,
                onSync = onRefresh,
                contentDescription = stringResource(R.string.verify_queue_refresh),
            )
        },
    )
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
private fun LocationFilterRow(
    options: List<VerifyLocationFilterOption>,
    selected: String?,
    onSelect: (String?) -> Unit,
    modifier: Modifier = Modifier,
) {
    LazyRow(
        contentPadding = PaddingValues(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = modifier,
    ) {
        items(options, key = { it.value ?: "__all__" }) { option ->
            CategoryChip(
                label = option.label,
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
private fun QueueRowCard(row: VerificationQueueRow, hierarchical: Boolean, onClick: () -> Unit) {
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
                    text = if (hierarchical) row.animalLabel.ifBlank { row.title }.ifBlank { row.shedLabel } else row.title,
                    color = MeshaColors.Ink,
                    fontSize = 14.5.sp,
                    fontWeight = FontWeight.W700,
                )
                Text(
                    text = if (hierarchical) {
                        listOf(row.weightLabel, row.mediaCountLabel, row.operatorLabel)
                            .filter { it.isNotBlank() }
                            .joinToString(" · ")
                            .ifBlank { row.subtitle }
                    } else {
                        row.subtitle
                    },
                    color = MeshaColors.Muted,
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            StatusPill(tone = row.statusTone)
        }
        Text(
            text = if (hierarchical) row.scopeType.label() else row.categoryLabel,
            color = MeshaColors.Faint,
            fontSize = 11.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier.padding(top = 10.dp),
        )
    }
}

private fun VerifyScopeType.label(): String = when (this) {
    VerifyScopeType.INDIVIDUAL -> "Individual"
    VerifyScopeType.LUMP_SUM -> "Lump-sum"
    VerifyScopeType.OTHER -> "Evidence"
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
