package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.snapshotFlow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.R as DesignSystemR
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// ─────────────────────────────────────────────────────────────────────────────
// Bottom-sheet overlays (mock/vaccination-mobile-mock.html · screens.md "Overlays
// → components"). Every sheet is a STATELESS dark-theme renderer: it takes its
// callbacks as params and holds only inline FAKE data marked `TODO(backend)`. The
// backend contract will own the real rows, statuses, labels, and options (golden
// frontend rule) — these ports match the mock's structure so the wiring is real.
//
// No Material icons — text glyphs only (design-system §5 ports the mock's line-icon
// set later). Bodies use foundation LazyColumn with a capped height + internal
// scroll (design-system §4 "capped height + internal scroll" sheet fix).
//
// The five sheets:
//   1. SyncSheet         (ovl-sync)  — connectivity + Room-outbox queue detail
//   2. LanguageSheet     (ovl-lang)  — en/hi/kn/te with native labels
//   3. ScopePickerSheet  (ovl-scope) — park picker → backend scope token
//   4. DataGapsSheet     (ovl-gaps)  — animals excluded from coverage + reason
//   5. DosesGivenSheet   (ovl-given) — per-vaccine doses-given breakdown
// ─────────────────────────────────────────────────────────────────────────────

/** Overlay palette — aliased to the design system (MeshaColors); no raw hex here. */
private object OverlayTokens {
    val brandD = MeshaColors.BrandD
    val sheetBg = MeshaColors.Surf
    val surf2 = MeshaColors.Surf2
    val surf3 = MeshaColors.Surf3
    val ink = MeshaColors.Ink
    val muted = MeshaColors.Muted
    val faint = MeshaColors.Faint
    val hair = MeshaColors.Hair
    val danger = MeshaColors.Danger
    val dangerX = MeshaColors.DangerX
    val warn = MeshaColors.Warn
    val warnX = MeshaColors.WarnX
    val ok = MeshaColors.Ok
    val okX = MeshaColors.OkX
}

// region ── shared sheet scaffold ──────────────────────────────────────────────

/**
 * The shared bottom-sheet chrome every overlay wraps its content in: a
 * [ModalBottomSheet] with a grip, a sticky header (title + optional subtitle), the
 * caller's scrollable body, and an optional footer "more" line. `skipPartially
 * Expanded` keeps the sheet at one height (mock sheets don't half-open).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun OverlaySheet(
    title: String,
    subtitle: String?,
    onDismiss: () -> Unit,
    footer: String? = null,
    body: @Composable ColumnScope.() -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = OverlayTokens.sheetBg,
        shape = RoundedCornerShape(topStart = 26.dp, topEnd = 26.dp),
        dragHandle = { OverlayGrip() },
    ) {
        Column(Modifier.fillMaxWidth().padding(bottom = 22.dp)) {
            Column(Modifier.padding(horizontal = 20.dp)) {
                Text(title, color = OverlayTokens.ink, fontSize = 16.sp, fontWeight = FontWeight.W700)
                if (subtitle != null) {
                    Spacer(Modifier.height(2.dp))
                    Text(subtitle, color = OverlayTokens.muted, fontSize = 12.sp)
                }
            }
            Spacer(Modifier.height(10.dp))
            body()
            if (footer != null) {
                Spacer(Modifier.height(10.dp))
                Text(
                    footer,
                    color = OverlayTokens.faint,
                    fontSize = 11.5.sp,
                    modifier = Modifier.padding(horizontal = 20.dp),
                )
            }
        }
    }
}

@Composable
private fun OverlayGrip() {
    Box(Modifier.fillMaxWidth().padding(top = 10.dp, bottom = 6.dp), contentAlignment = Alignment.Center) {
        Box(
            Modifier
                .size(width = 38.dp, height = 4.dp)
                .clip(CircleShape)
                .background(OverlayTokens.surf3),
        )
    }
}

/** Picker row (mock `.popt`/`.lopt`): leading glyph/initials, name + sub, check when selected. */
@Composable
private fun OverlayPickerRow(
    lead: String,
    name: String,
    sub: String,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .heightIn(min = 44.dp)
            .clickable { onClick() }
            .padding(horizontal = 20.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier.size(38.dp).clip(RoundedCornerShape(12.dp)).background(OverlayTokens.surf3),
            contentAlignment = Alignment.Center,
        ) {
            Text(lead, color = OverlayTokens.brandD, fontSize = 12.sp, fontWeight = FontWeight.W700)
        }
        Column(Modifier.weight(1f)) {
            Text(name, color = OverlayTokens.ink, fontSize = 13.5.sp, fontWeight = FontWeight.W700)
            Text(sub, color = OverlayTokens.muted, fontSize = 11.5.sp)
        }
        if (selected) {
            Text("✓", color = OverlayTokens.ok, fontSize = 15.sp, fontWeight = FontWeight.W800)
        }
    }
}

/** Card container (mock `.card`): surf bg + hairline + 18 radius. */
@Composable
private fun OverlayCard(content: @Composable ColumnScope.() -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(OverlayTokens.surf2)
            .border(1.dp, OverlayTokens.hair, RoundedCornerShape(16.dp))
            .padding(13.dp),
        content = content,
    )
}

@Composable
private fun OverlayPill(text: String, fg: Color, bg: Color) {
    Text(
        text,
        color = fg,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier.clip(CircleShape).background(bg).padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

/**
 * One physical-tag chip on a data-gaps card: a small labeled slot ("TAG 1" / "TAG 2") with the tag
 * value in mono, or a muted "—" when the animal has no active tag of that type. Fixed shape whether
 * present or missing, so the two chips stay aligned and a missing tag reads as "known-empty", not
 * "forgotten".
 */
@Composable
private fun GapTagChip(label: String, value: String?, modifier: Modifier = Modifier) {
    val hasValue = !value.isNullOrBlank()
    Column(
        modifier
            .clip(RoundedCornerShape(9.dp))
            .background(OverlayTokens.surf3)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    ) {
        Text(
            label.uppercase(),
            color = OverlayTokens.faint,
            fontSize = 9.sp,
            fontWeight = FontWeight.W700,
            letterSpacing = 0.5.sp,
        )
        Text(
            if (hasValue) value!! else "—",
            color = if (hasValue) OverlayTokens.ink else OverlayTokens.muted,
            fontSize = 12.5.sp,
            fontWeight = FontWeight.W600,
            fontFamily = FontFamily.Monospace,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun OverlayBar(percent: Int, fill: Color) {
    Box(
        Modifier.fillMaxWidth().height(6.dp).clip(CircleShape).background(OverlayTokens.surf3),
    ) {
        Box(
            Modifier
                .fillMaxHeight()
                .fillMaxWidth(percent.coerceIn(0, 100) / 100f)
                .clip(CircleShape)
                .background(fill),
        )
    }
}

@Composable
private fun OverlayInfoBox(text: String) {
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(OverlayTokens.surf3)
            .padding(13.dp),
    ) {
        Text(text, color = OverlayTokens.muted, fontSize = 12.sp, lineHeight = 17.sp)
    }
}

// endregion

// region ── 1. SyncSheet (ovl-sync) ─────────────────────────────────────────────

private enum class SyncItemState { SYNCED, QUEUED, UPLOADING, SYNCING, FAILED }

private data class SyncItem(
    val id: String,
    val shed: String,
    /** Raw op-type key (localized to a label in the row Composable). */
    val opType: String,
    val state: SyncItemState,
    val progress: Float,
    /** Backend error message (data) — rendered verbatim when present, else a localized state detail. */
    val backendDetail: String?,
    val attemptCount: Int,
    val maxAttempts: Int,
)

/**
 * Connectivity + sync-queue detail (screens.md `#netbar` → `ovl-sync`). Shows the
 * online/offline state, the queue summary (All synced / N queued / Syncing N…),
 * each queued shed record with its state + progress, and a retry-all affordance.
 *
 * Wired to `SyncRepository.observeStatus()` (via `SyncStatusViewModel` at the shell): the caller
 * passes the live `isOnline` + counts + the [queue] of `SyncQueueItem`s, mapped here to the
 * renderer's [SyncItem]. `onRetryAll` re-arms the failed/dead-letter rows.
 */
@Composable
fun SyncSheet(
    isOnline: Boolean = false,
    syncingCount: Int = 0,
    queuedCount: Int = 0,
    queue: List<SyncQueueItem> = emptyList(),
    onRetryAll: () -> Unit = {},
    onDismiss: () -> Unit,
) {
    val syncing = syncingCount
    val pending = queuedCount
    val summary = when {
        syncing > 0 -> stringResource(DesignSystemR.string.sync_summary_syncing_fmt, syncing)
        pending > 0 -> stringResource(DesignSystemR.string.sync_summary_queued_fmt, pending)
        else -> stringResource(DesignSystemR.string.sync_summary_all_synced)
    }
    val conn = stringResource(if (isOnline) DesignSystemR.string.sync_online else DesignSystemR.string.sync_offline)
    val items = queue.map { it.toSyncItem() }

    OverlaySheet(
        title = stringResource(DesignSystemR.string.ovl_sync_title),
        subtitle = stringResource(DesignSystemR.string.ovl_sync_subtitle),
        onDismiss = onDismiss,
        footer = stringResource(DesignSystemR.string.sync_footer),
    ) {
        // Connectivity + queue summary.
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box(Modifier.size(8.dp).clip(CircleShape).background(if (isOnline) OverlayTokens.ok else OverlayTokens.danger))
            Text(
                "$conn · $summary",
                color = OverlayTokens.muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
            )
            Spacer(Modifier.weight(1f))
            if (pending > 0) {
                Text(
                    "↻ ${stringResource(DesignSystemR.string.sync_retry_all)}",
                    color = OverlayTokens.brandD,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier.clip(CircleShape).clickable {
                        onRetryAll()
                    }.padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        Spacer(Modifier.height(6.dp))
        LazyColumn(
            Modifier.fillMaxWidth().heightIn(max = 340.dp),
            contentPadding = PaddingValues(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            items(items, key = { it.id }) { item -> SyncRow(item) }
        }
    }
}

@Composable
private fun SyncRow(item: SyncItem) {
    val (glyph, fg, bg, labelRes) = when (item.state) {
        SyncItemState.SYNCED -> SyncVisual("✓", OverlayTokens.ok, OverlayTokens.okX, DesignSystemR.string.sync_state_synced)
        SyncItemState.QUEUED -> SyncVisual("◷", OverlayTokens.muted, OverlayTokens.surf3, DesignSystemR.string.sync_state_queued)
        SyncItemState.UPLOADING -> SyncVisual("↑", OverlayTokens.warn, OverlayTokens.warnX, DesignSystemR.string.sync_state_uploading)
        SyncItemState.SYNCING -> SyncVisual("↻", OverlayTokens.warn, OverlayTokens.warnX, DesignSystemR.string.sync_state_syncing)
        SyncItemState.FAILED -> SyncVisual("✕", OverlayTokens.danger, OverlayTokens.dangerX, DesignSystemR.string.sync_state_failed)
    }
    val label = stringResource(labelRes)
    val opLabel = when (item.opType) {
        "SHED_SUBMIT" -> stringResource(DesignSystemR.string.sync_optype_shed)
        "PROOF_UPLOAD" -> stringResource(DesignSystemR.string.sync_optype_proof)
        "RESCHEDULE" -> stringResource(DesignSystemR.string.sync_optype_reschedule)
        "VERIFICATION_VERDICT" -> stringResource(DesignSystemR.string.sync_optype_verification_verdict)
        else -> item.opType
    }
    // Backend error (data) wins; otherwise a localized per-state detail.
    val detail = item.backendDetail ?: when (item.state) {
        SyncItemState.QUEUED -> stringResource(DesignSystemR.string.sync_detail_queued)
        SyncItemState.SYNCING -> stringResource(DesignSystemR.string.sync_detail_syncing)
        SyncItemState.UPLOADING -> stringResource(DesignSystemR.string.sync_detail_uploading)
        SyncItemState.SYNCED -> stringResource(DesignSystemR.string.sync_detail_synced)
        SyncItemState.FAILED -> stringResource(DesignSystemR.string.sync_detail_attempt_fmt, item.attemptCount, item.maxAttempts)
    }
    val showBar = item.state == SyncItemState.UPLOADING || item.state == SyncItemState.SYNCING
    OverlayCard {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Box(Modifier.size(34.dp).clip(RoundedCornerShape(10.dp)).background(bg), contentAlignment = Alignment.Center) {
                Text(glyph, color = fg, fontSize = 15.sp, fontWeight = FontWeight.W800)
            }
            Column(Modifier.weight(1f)) {
                Text("${item.shed} · $opLabel", color = OverlayTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
                Text(detail, color = OverlayTokens.muted, fontSize = 11.5.sp)
                if (showBar) {
                    Spacer(Modifier.height(6.dp))
                    OverlayBar((item.progress * 100).toInt(), fg)
                }
            }
            OverlayPill(label, fg, bg)
        }
    }
}

private data class SyncVisual(val glyph: String, val fg: Color, val bg: Color, val labelRes: Int)

/** Maps a durable outbox row ([SyncQueueItem]) to the sheet's renderer row ([SyncItem]). The
 *  outbox only knows the scope id ([SyncQueueItem.groupKey]) + op kind, so the shed/park labels
 *  the mock shows come from those until the backend attaches richer per-item labels. */
private fun SyncQueueItem.toSyncItem(): SyncItem {
    val itemState = when {
        status == SyncItemStatus.SUCCEEDED -> SyncItemState.SYNCED
        status == SyncItemStatus.QUEUED -> SyncItemState.QUEUED
        status == SyncItemStatus.IN_FLIGHT && opType == "PROOF_UPLOAD" -> SyncItemState.UPLOADING
        status == SyncItemStatus.IN_FLIGHT -> SyncItemState.SYNCING
        else -> SyncItemState.FAILED
    }
    // Labels/details are localized in SyncRow (Composable); the mapper only carries data:
    // the raw op-type key, the backend error (if any), and the retry counters.
    val inProgress = itemState == SyncItemState.UPLOADING || itemState == SyncItemState.SYNCING
    return SyncItem(
        id = id,
        shed = groupKey,
        opType = opType,
        state = itemState,
        progress = if (inProgress) 0.6f else 0f,
        backendDetail = lastError,
        attemptCount = attemptCount,
        maxAttempts = maxAttempts,
    )
}

// endregion

// region ── 2. LanguageSheet (ovl-lang) ─────────────────────────────────────────

private data class LangOption(val code: String, val lead: String, val native: String, val english: String)

/**
 * Language picker (ovl-lang). The four supported locales with their native labels;
 * [current] is the active language code (the checked row). [onSelect] hands the
 * chosen code back so the caller can persist it via DataStore (design-system §7:
 * locale is kept for every screen on this phone).
 */
@Composable
fun LanguageSheet(current: String, onSelect: (code: String) -> Unit, onDismiss: () -> Unit) {
    val langs = listOf(
        LangOption("en", "EN", "English", "English"),
        LangOption("hi", "हिं", "हिंदी", "Hindi"),
        LangOption("kn", "ಕ", "ಕನ್ನಡ", "Kannada"),
        LangOption("te", "తె", "తెలుగు", "Telugu"),
    )
    OverlaySheet(
        title = stringResource(DesignSystemR.string.lang_sheet_title),
        subtitle = stringResource(DesignSystemR.string.lang_sheet_subtitle),
        onDismiss = onDismiss,
        footer = stringResource(DesignSystemR.string.lang_sheet_footer),
    ) {
        Column {
            langs.forEach { l ->
                OverlayPickerRow(
                    lead = l.lead,
                    name = l.native,
                    sub = l.english,
                    selected = l.code == current,
                    onClick = { onSelect(l.code) },
                )
            }
        }
    }
}

// endregion

// region ── 3. ScopePickerSheet (ovl-scope) ─────────────────────────────────────

data class ScopeOption(val token: String, val lead: String, val name: String, val sub: String)

/**
 * Park scope picker (ovl-scope, director + CEO/COO). Each row maps to a backend
 * **scope token**; [onSelect] returns the chosen human label + token. The caller sends the
 * token to the backend, which re-scopes the overview rollup + backlog + follow-up
 * reads (the app never filters by park itself).
 *
 * Sourced from the bootstrap grant set (park_id + scope_token pairs).
 * The "All parks" scope is always first. Park-specific scopes follow.
 */
@Composable
fun ScopePickerSheet(
    scopes: List<ScopeOption> = defaultScopeOptions(),
    onSelect: (label: String, token: String) -> Unit,
    onDismiss: () -> Unit,
) {
    OverlaySheet(
        title = stringResource(DesignSystemR.string.ovl_scope_title),
        subtitle = stringResource(DesignSystemR.string.ovl_scope_subtitle),
        onDismiss = onDismiss,
        footer = stringResource(DesignSystemR.string.scope_footer),
    ) {
        Column {
            scopes.forEachIndexed { index, scope ->
                OverlayPickerRow(
                    lead = scope.lead,
                    name = scope.name,
                    sub = scope.sub,
                    selected = index == 0,
                    onClick = { onSelect(scope.name, scope.token) },
                )
            }
        }
    }
}

/** Default scope options when bootstrap data is not available yet. */
fun defaultScopeOptions(): List<ScopeOption> = listOf(
    ScopeOption("all", "◎", "All parks", "1,312 animals · company-wide"),
    ScopeOption("cbe", "CB", "CBE · Coimbatore", "716 animals · 52% coverage"),
    ScopeOption("cpt", "CP", "CPT · Channapatna", "596 animals · 42% coverage"),
)

// endregion

// region ── 4. DataGapsSheet (ovl-gaps) ─────────────────────────────────────────

/**
 * One data-gaps entry — always a SINGLE animal (never a by-reason group). [displayId] = Goat OS
 * passport id (G-XXXXXX, headlines the card), [tag1]/[tag2] = the two physical tags ("Tag 1"/"Tag 2",
 * null when no active tag of that type — rendered as "—"), [location] = park · shed, [reason] = why
 * it's excluded (the pill).
 */
data class GapRow(
    val displayId: String,
    val location: String,
    val reason: String,
    val tag1: String? = null,
    val tag2: String? = null,
)

/**
 * Data-gaps sheet (ovl-gaps). Animals the schedule can't evaluate until fixed —
 * excluded from the coverage % with the reason.
 *
 * Wired to `GET /app/vaccination/gaps?park_id=<id>` → one row PER ANIMAL (display id + tags + reason).
 * The overlay is HONEST with empty/error states: if no gaps exist, the sheet shows empty; if the load
 * fails (no network), it shows the error.
 */
@Composable
fun DataGapsSheet(
    gapsData: List<GapRow> = emptyList(),
    isLoading: Boolean = false,
    isLoadingMore: Boolean = false,
    hasMore: Boolean = false,
    errorMessage: String? = null,
    isRefreshing: Boolean = false,
    lastSyncedAt: Long? = null,
    isOffline: Boolean = false,
    onLoadMore: () -> Unit = {},
    onDismiss: () -> Unit,
) {
    OverlaySheet(
        title = stringResource(DesignSystemR.string.ovl_gaps_title),
        subtitle = stringResource(DesignSystemR.string.ovl_gaps_subtitle),
        onDismiss = onDismiss,
        footer = stringResource(DesignSystemR.string.gaps_footer),
    ) {
        Column(
            Modifier.padding(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            // Cached rows can still show after a failed background refresh — surface the
            // stale/offline signal so they never read as fresh (renders nothing on a cold
            // load/empty/error, which the branches below own).
            SyncStatusIndicator(
                isRefreshing = isRefreshing,
                lastSyncedAt = lastSyncedAt,
                hasData = gapsData.isNotEmpty(),
                isOffline = isOffline,
            )
            when {
                isLoading -> {
                    Text(stringResource(DesignSystemR.string.ovl_loading), color = OverlayTokens.muted, fontSize = 13.sp)
                }
                errorMessage != null -> {
                    OverlayInfoBox(stringResource(DesignSystemR.string.ovl_error_fmt, errorMessage))
                }
                gapsData.isEmpty() -> {
                    OverlayInfoBox(stringResource(DesignSystemR.string.gaps_empty))
                }
                else -> {
                    val listState = rememberLazyListState()
                    LaunchedEffect(listState, hasMore, isLoadingMore, gapsData.size) {
                        if (!hasMore || isLoadingMore || gapsData.isEmpty()) return@LaunchedEffect
                        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
                            .collect { lastVisibleIndex ->
                                if (lastVisibleIndex >= listState.layoutInfo.totalItemsCount - 4 && hasMore && !isLoadingMore) {
                                    onLoadMore()
                                }
                            }
                    }
                    // Every entry is one animal: display id + both physical tags + the reason pill.
                    // LazyColumn(heightIn) so a long gaps list scrolls inside the sheet and composes
                    // lazily, instead of a plain Column { forEach } that eagerly builds every card and
                    // clips past the sheet edge (mirrors DosesGivenSheet).
                    LazyColumn(
                        state = listState,
                        modifier = Modifier.fillMaxWidth().heightIn(max = 340.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(gapsData, key = { it.displayId }) { gap ->
                            OverlayCard {
                                Row(verticalAlignment = Alignment.Top) {
                                    Column(Modifier.weight(1f)) {
                                        Text(gap.displayId, color = OverlayTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
                                        if (gap.location.isNotBlank()) {
                                            Text(gap.location, color = OverlayTokens.muted, fontSize = 11.5.sp)
                                        }
                                    }
                                    Spacer(Modifier.width(10.dp))
                                    // The pill is the reason it's excluded — the actionable fact.
                                    OverlayPill(gap.reason, OverlayTokens.warn, OverlayTokens.warnX)
                                }
                                Spacer(Modifier.height(9.dp))
                                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                    GapTagChip(stringResource(DesignSystemR.string.gaps_tag1_label), gap.tag1, Modifier.weight(1f))
                                    GapTagChip(stringResource(DesignSystemR.string.gaps_tag2_label), gap.tag2, Modifier.weight(1f))
                                }
                            }
                        }
                        if (isLoadingMore) {
                            item(key = "loading-more-gaps") {
                                Box(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(vertical = 12.dp),
                                    contentAlignment = Alignment.Center,
                                ) {
                                    CircularProgressIndicator(
                                        modifier = Modifier.size(18.dp),
                                        color = OverlayTokens.muted,
                                        strokeWidth = 2.dp,
                                    )
                                }
                            }
                        }
                        item { OverlayInfoBox(stringResource(DesignSystemR.string.gaps_note)) }
                    }
                }
            }
        }
    }
}

// endregion

// region ── 5. DosesGivenSheet (ovl-given) ──────────────────────────────────────

data class GivenRow(val vaccine: String, val given: String, val coverage: String, val percent: Int)

/**
 * Doses-given drill (ovl-given). Per-vaccine completed doses this cycle with a
 * coverage bar (scope-aware).
 *
 * Wired to `GET /app/vaccination/coverage` → per-vaccine given + coverage % from the scope-token rollup.
 * Shows honest empty/error states: if no vaccines, "No data available"; if load fails, "Error: ...".
 */
@Composable
fun DosesGivenSheet(
    rows: List<GivenRow> = emptyList(),
    isLoading: Boolean = false,
    errorMessage: String? = null,
    isRefreshing: Boolean = false,
    lastSyncedAt: Long? = null,
    isOffline: Boolean = false,
    onDismiss: () -> Unit,
) {
    OverlaySheet(
        title = stringResource(DesignSystemR.string.ovl_doses_title),
        subtitle = stringResource(DesignSystemR.string.ovl_doses_subtitle),
        onDismiss = onDismiss,
        footer = stringResource(DesignSystemR.string.doses_footer),
    ) {
        Column(
            Modifier.fillMaxWidth().heightIn(max = 340.dp).padding(horizontal = 20.dp),
        ) {
            // Stale/offline signal for cached rows kept after a failed refresh (self-hides
            // on cold load/empty/error).
            SyncStatusIndicator(
                isRefreshing = isRefreshing,
                lastSyncedAt = lastSyncedAt,
                hasData = rows.isNotEmpty(),
                isOffline = isOffline,
            )
            when {
                isLoading -> {
                    Text(stringResource(DesignSystemR.string.ovl_loading), color = OverlayTokens.muted, fontSize = 13.sp)
                }
                errorMessage != null -> {
                    OverlayInfoBox(stringResource(DesignSystemR.string.ovl_error_fmt, errorMessage))
                }
                rows.isEmpty() -> {
                    OverlayInfoBox(stringResource(DesignSystemR.string.doses_empty))
                }
                else -> {
                    LazyColumn(
                        Modifier.fillMaxWidth(),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(rows, key = { it.vaccine }) { row ->
                            val tone = if (row.percent >= 60) OverlayTokens.ok else OverlayTokens.warn
                            val toneBg = if (row.percent >= 60) OverlayTokens.okX else OverlayTokens.warnX
                            OverlayCard {
                                Row(verticalAlignment = Alignment.CenterVertically) {
                                    Text(row.vaccine, color = OverlayTokens.ink, fontSize = 13.5.sp, fontWeight = FontWeight.W700)
                                    Spacer(Modifier.width(8.dp))
                                    Text(row.given, color = OverlayTokens.muted, fontSize = 11.5.sp)
                                    Spacer(Modifier.weight(1f))
                                    OverlayPill(row.coverage, tone, toneBg)
                                }
                                Spacer(Modifier.height(8.dp))
                                OverlayBar(row.percent, tone)
                            }
                        }
                    }
                }
            }
        }
    }
}

// endregion
