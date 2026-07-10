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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import androidx.compose.ui.text.font.FontWeight
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
    val park: String,
    val detail: String,
    val state: SyncItemState,
    val progress: Float,
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
        syncing > 0 -> "Syncing $syncing…"
        pending > 0 -> "$pending queued"
        else -> "All synced"
    }
    val items = queue.map { it.toSyncItem() }

    OverlaySheet(
        title = "Sync status",
        subtitle = "Records save on the phone first, then sync when online",
        onDismiss = onDismiss,
        footer = "Offline-first: the outbox uploads video proof then the shed record, retries with backoff, and never creates a duplicate.",
    ) {
        // Connectivity + queue summary.
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box(Modifier.size(8.dp).clip(CircleShape).background(if (isOnline) OverlayTokens.ok else OverlayTokens.danger))
            Text(
                "${if (isOnline) "Online" else "Offline"} · $summary",
                color = OverlayTokens.muted,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
            )
            Spacer(Modifier.weight(1f))
            if (pending > 0) {
                Text(
                    "↻ Retry all",
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
    val (glyph, fg, bg, label) = when (item.state) {
        SyncItemState.SYNCED -> SyncVisual("✓", OverlayTokens.ok, OverlayTokens.okX, "Synced")
        SyncItemState.QUEUED -> SyncVisual("◷", OverlayTokens.muted, OverlayTokens.surf3, "Queued")
        SyncItemState.UPLOADING -> SyncVisual("↑", OverlayTokens.warn, OverlayTokens.warnX, "Uploading proof")
        SyncItemState.SYNCING -> SyncVisual("↻", OverlayTokens.warn, OverlayTokens.warnX, "Syncing record")
        SyncItemState.FAILED -> SyncVisual("✕", OverlayTokens.danger, OverlayTokens.dangerX, "Failed")
    }
    val showBar = item.state == SyncItemState.UPLOADING || item.state == SyncItemState.SYNCING
    OverlayCard {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Box(Modifier.size(34.dp).clip(RoundedCornerShape(10.dp)).background(bg), contentAlignment = Alignment.Center) {
                Text(glyph, color = fg, fontSize = 15.sp, fontWeight = FontWeight.W800)
            }
            Column(Modifier.weight(1f)) {
                Text("${item.shed} · ${item.park}", color = OverlayTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
                Text(item.detail, color = OverlayTokens.muted, fontSize = 11.5.sp)
                if (showBar) {
                    Spacer(Modifier.height(6.dp))
                    OverlayBar((item.progress * 100).toInt(), fg)
                }
            }
            OverlayPill(label, fg, bg)
        }
    }
}

private data class SyncVisual(val glyph: String, val fg: Color, val bg: Color, val label: String)

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
    val detail = lastError ?: when (itemState) {
        SyncItemState.QUEUED -> "Waiting to sync"
        SyncItemState.SYNCING -> "Submitting…"
        SyncItemState.UPLOADING -> "Uploading proof…"
        SyncItemState.SYNCED -> "On file"
        SyncItemState.FAILED -> "Attempt $attemptCount of $maxAttempts"
    }
    val inProgress = itemState == SyncItemState.UPLOADING || itemState == SyncItemState.SYNCING
    return SyncItem(
        id = id,
        shed = groupKey,
        park = opTypeLabel(opType),
        detail = detail,
        state = itemState,
        progress = if (inProgress) 0.6f else 0f,
    )
}

private fun opTypeLabel(opType: String): String = when (opType) {
    "SHED_SUBMIT" -> "Shed record"
    "PROOF_UPLOAD" -> "Video proof"
    "RESCHEDULE" -> "Reschedule"
    else -> opType
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
        title = "Choose language",
        subtitle = "Kept for every screen on this phone",
        onDismiss = onDismiss,
        footer = "More languages added as teams grow",
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
        title = "View a park",
        subtitle = "Company-wide, or drill into one park",
        onDismiss = onDismiss,
        footer = "Scope also filters the drives and backlog below",
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

data class GapRow(val title: String, val detail: String, val count: String)

/**
 * Data-gaps sheet (ovl-gaps). Animals the schedule can't evaluate until fixed —
 * excluded from the coverage % with the reason.
 *
 * Wired to `GET /app/vaccination/gaps?park_id=<id>` → animals excluded + reason summary.
 * The overlay is currently HONEST with empty/error states: if no gaps exist, the sheet
 * shows empty. If the load fails (no network), it shows the error.
 */
@Composable
fun DataGapsSheet(
    gapsData: List<GapRow> = emptyList(),
    isLoading: Boolean = false,
    errorMessage: String? = null,
    onDismiss: () -> Unit,
) {
    OverlaySheet(
        title = "Data gaps",
        subtitle = "Animals the schedule can't evaluate until fixed",
        onDismiss = onDismiss,
        footer = "Fix on the web dashboard · Data Ops → Herd Register",
    ) {
        Column(
            Modifier.padding(horizontal = 20.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            when {
                isLoading -> {
                    Text("Loading…", color = OverlayTokens.muted, fontSize = 13.sp)
                }
                errorMessage != null -> {
                    OverlayInfoBox("Error: $errorMessage")
                }
                gapsData.isEmpty() -> {
                    OverlayInfoBox("No data gaps — all animals have required information.")
                }
                else -> {
                    gapsData.forEach { gap ->
                        OverlayCard {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Column(Modifier.weight(1f)) {
                                    Text(gap.title, color = OverlayTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
                                    Text(gap.detail, color = OverlayTokens.muted, fontSize = 11.5.sp)
                                }
                                Spacer(Modifier.width(10.dp))
                                OverlayPill("${gap.count} animals", OverlayTokens.warn, OverlayTokens.warnX)
                            }
                        }
                    }
                    OverlayInfoBox(
                        "These animals are excluded from the coverage % until the missing data is filled. Everything else is fully tracked.",
                    )
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
    onDismiss: () -> Unit,
) {
    OverlaySheet(
        title = "Doses given",
        subtitle = "Completed doses this cycle · by vaccine",
        onDismiss = onDismiss,
        footer = "Every dose is a verified shed record with video proof",
    ) {
        Column(
            Modifier.fillMaxWidth().heightIn(max = 340.dp).padding(horizontal = 20.dp),
        ) {
            when {
                isLoading -> {
                    Text("Loading…", color = OverlayTokens.muted, fontSize = 13.sp)
                }
                errorMessage != null -> {
                    OverlayInfoBox("Error: $errorMessage")
                }
                rows.isEmpty() -> {
                    OverlayInfoBox("No vaccination data available for this scope.")
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
