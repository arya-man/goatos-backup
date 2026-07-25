package sg.mesha.goatos.feature.scan

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.res.pluralStringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.LoadingSkeletonList

// telemetry:exempt pure stateless renderer; AnalyticsPort/funnel wiring lives in ScanViewModel.

import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.SyncStatusIndicator

// ---------------------------------------------------------------------------
// Scan (v-scan) — the field operator's tap-to-scan surface for one shed.
//
// TRD §14 dumb-renderer: this screen RENDERS backend-provided data. It never
// decides which animals are due / eligible, never groups them, never counts
// modules, and never checks role. Every visible label, status, count, action,
// and disabled/error reason arrives via [ScanUiState] (a later ViewModel fills
// it from the app-api). Scan affordances appear only when
// [ScanUiState.scanEnabled] is true — the backend gates the scan action, not a
// client role check.
// ---------------------------------------------------------------------------

/**
 * Per-row scan status. The BACKEND supplies each row's status; the screen only
 * maps it to an icon/tone. This is a render discriminator, not a client
 * derivation of eligibility.
 */
enum class ScanStatus { DONE, PENDING, SKIPPED }

/** Per-animal Room/outbox state for camera evidence. */
enum class ProofUploadStatus { MISSING, UPLOADING, SYNCED, FAILED }

/** Tone for the live scan feed. A duplicate can be a DONE row without being success-colored. */
enum class ScanFeedTone { ACCEPTED, DUPLICATE, REJECTED }

/** A backend-tagged vaccine group used as a filter chip on the roster. */
data class VaccineGroup(
    val id: String,
    val vaccine: String,   // e.g. "FMD"
    val done: Int,
    val due: Int,
    val active: Boolean,   // backend-highlighted active group
)

/** One roster animal (from the backend roster/status read, cached in Room). */
data class RosterRow(
    val primaryTag: String,        // mono RFID tag
    val secondaryTag: String?,     // second RFID tag when double-tagged
    val vaccineLabel: String,      // "FMD · 1st", "due · FMD", or a skip reason
    val status: ScanStatus,
    val unsynced: Boolean = false, // local, not-yet-synced draft scan overlay
    val scannedAtLabel: String? = null,
    val goatId: String = "",
    val obligationId: String = "",
    val proofRequired: Boolean = true,
    val proofClipCount: Int = 0,
    val proofUploadStatus: ProofUploadStatus = ProofUploadStatus.MISSING,
)

/** One entry in the live "last taps" feed (given or skipped only). */
data class ScanFeedEntry(
    val primaryTag: String,
    val secondaryTag: String?,
    val vaccineLabel: String,      // "FMD · 1st" or "skip · <reason>"
    val status: ScanStatus,        // DONE or SKIPPED
    val scannedAtLabel: String? = null,
    val tone: ScanFeedTone = when (status) {
        ScanStatus.SKIPPED -> ScanFeedTone.REJECTED
        else -> ScanFeedTone.ACCEPTED
    },
)

/**
 * The not-due red error state: backend eligibility said this tag has no due
 * vaccine here. Rendered as a red ring + red banner; the audible/haptic alert
 * is fired locally by the app layer via FeedbackPort.
 */
data class ScanError(
    val message: String,
    val tag: String? = null,
)

/** Backend-provided copy for the three count tiles. */
data class ScanTileLabels(
    val done: String,
    val pending: String,
    val skipped: String,
)

/** Optional live RFID reader state surfaced on the vaccination capture screen. */
data class ScanReaderConnection(
    val readerName: String,
    val statusLabel: String,
    val connected: Boolean,
    val actionLabel: String,
)

/**
 * Complete, backend-fed state for the Scan screen. Every visible string is a
 * field so nothing is hardcoded in the renderer.
 */
// @Immutable: vaccineGroups/feed/roster List<T> fields otherwise mark this unstable (item 6,
// perf/stability pass).
@Immutable
data class ScanUiState(
    val shedLabel: String,                 // header eyebrow, e.g. "Vaccination · Gandhi 1"
    val cohortLabel: String,               // header title, e.g. "Milking does"
    val ringDone: Int,                     // shed total scanned
    val ringTotal: Int,                    // shed total due
    val ringUnitLabel: String,             // e.g. "vaccinated"
    val tapHint: String,                   // "Tap reader to animal — reader shows its due vaccine"
    val vaccineGroups: List<VaccineGroup>, // filter chips
    val doneCount: Int,
    val pendingCount: Int,
    val skippedCount: Int,
    val tileLabels: ScanTileLabels,
    val feed: List<ScanFeedEntry>,         // last taps
    val roster: List<RosterRow>,           // scan-list rows
    val listTitle: String,                 // scan-list sheet header
    val submitLabel: String,               // backend-provided CTA text
    val canSubmit: Boolean,                // completion hint (backend revalidates on submit)
    val scanEnabled: Boolean,              // show tap-to-scan affordances at all
    val error: ScanError? = null,          // not-due red state
    val footNote: String = "",             // haptic/tone legend copy
    // Offline-first sync state (docs/decisions/android-offline-first.md), rendered by
    // sg.mesha.goatos.core.ui.SyncStatusIndicator. [isRefreshing]/[lastSyncedAt]/[isOffline]
    // describe the background network refresh over the ALREADY-RENDERED Room cache above.
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    // Local UI selection (not backend-fed) — which tile is active and whether the roster
    // overlay is open. Mirrors the mock's Done/Pending/Skipped chips → scan-list overlay.
    val selectedFilter: ScanStatus? = null,
    val rosterExpanded: Boolean = false,
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
    // Submit proof gate (full-roster): DONE animals whose proof video is not yet SYNCED
    // (MISSING/UPLOADING/FAILED). Non-empty ⇒ submit is blocked; each row carries its proof status
    // and supports CaptureProof (replace) / RetryProof so the operator can resolve it, including
    // animals below the visible scroll window.
    val proofActionNeeded: List<RosterRow> = emptyList(),
    // Transient "already scanned" strip: set on a re-scan of an already-DONE tag, rendered below
    // the tap-hint card, cleared on the next accepted scan. Non-null shows the strip; it never
    // stacks — only ONE feed row per tag exists (see [ScanFeedEntry]/[ScanViewModel.prependFeed]).
    val duplicateNotice: String? = null,
    val readerConnection: ScanReaderConnection? = null,
    val shedId: String? = null,
    val taskId: String? = null,
    val sopVersionId: String? = null,
    val taskRowVersion: Int? = null,
    val isInitialLoading: Boolean = false,
)

/** User intents the screen emits; the app/viewmodel layer handles them. */
sealed interface ScanEvent {
    data object Back : ScanEvent
    data object Tap : ScanEvent                            // tap reader / ring to scan
    data object OpenList : ScanEvent                       // open the scan-list sheet
    data object Submit : ScanEvent                         // submit the shed record
    data object LoadMore : ScanEvent                       // fetch one bounded continuation page
    data object ReconnectReader : ScanEvent                // quick path back to RFID reconnect
    data class SelectGroup(val groupId: String) : ScanEvent
    data class OpenTile(val status: ScanStatus) : ScanEvent
    data class CaptureProof(val goatId: String) : ScanEvent
    data class RetryProof(val goatId: String) : ScanEvent
}

// --- mock-ported tokens (dark = default field theme; values from design-system.md) --
private object ScanTokens {
    val brand = MeshaColors.Brand
    val brandD = MeshaColors.BrandD
    val danger = MeshaColors.Danger
    val warning = MeshaColors.Warn
    val muted = MeshaColors.Muted
    val faint = MeshaColors.Faint
    val ink = MeshaColors.Ink
    val hair = MeshaColors.Hair
    val surf = MeshaColors.Surf
    val surf3 = MeshaColors.Surf3
    val okX = MeshaColors.OkX        // ~.16 alpha brand
    val dangerX = MeshaColors.DangerX  // ~.15 alpha danger
    val warningX = MeshaColors.WarnX
    val brandSoft = MeshaColors.BrandTint
    val onPrimary = MeshaColors.OnBrand
}

@Composable
fun ScanScreen(
    state: ScanUiState,
    onEvent: (ScanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Surface(
        color = MaterialTheme.colorScheme.background,
        modifier = modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            ScanHeader(
                eyebrow = state.shedLabel,
                title = state.cohortLabel,
                onBack = { onEvent(ScanEvent.Back) },
                onSwitchShed = { onEvent(ScanEvent.Back) },
            )

            if (state.isInitialLoading) {
                LoadingSkeletonList(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth(),
                    rows = 5,
                )
            } else {
                // Body scrolls; the submit footer is pinned.
                LazyColumn(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth(),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    item {
                        ReaderConnectionBanner(
                            reader = state.readerConnection ?: ScanReaderConnection(
                                readerName = "RFID reader",
                                statusLabel = "Checking reader connection",
                                connected = false,
                                actionLabel = "Reconnect",
                            ),
                            progressLabel = "${state.ringDone}/${state.ringTotal}",
                            onReconnect = { onEvent(ScanEvent.ReconnectReader) },
                        )
                    }
                item {
                    ScanRing(
                        done = state.ringDone,
                        total = state.ringTotal,
                        unitLabel = state.ringUnitLabel,
                        isError = state.error != null,
                        enabled = state.scanEnabled,
                        onTap = { onEvent(ScanEvent.Tap) },
                    )
                }
                if (state.scanEnabled) {
                    item { TapHint(state.tapHint) }
                }
                state.duplicateNotice?.let { notice ->
                    item { DuplicateNoticeStrip(notice) }
                }
                state.error?.let { err ->
                    item { NotDueBanner(err) }
                }
                if (state.vaccineGroups.isNotEmpty()) {
                    item {
                        VaccineGroupChips(state.vaccineGroups) { id ->
                            onEvent(ScanEvent.SelectGroup(id))
                        }
                    }
                }
                item {
                    CountTiles(
                        done = state.doneCount,
                        pending = state.pendingCount,
                        skipped = state.skippedCount,
                        labels = state.tileLabels,
                        selected = state.selectedFilter,
                        onTile = { onEvent(ScanEvent.OpenTile(it)) },
                    )
                }
                item {
                    SyncStatusIndicator(
                        isRefreshing = state.isRefreshing,
                        lastSyncedAt = state.lastSyncedAt,
                        hasData = state.lastSyncedAt != null,
                        isOffline = state.isOffline,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
                item {
                    Text(
                        text = state.listTitle.ifBlank { stringResource(R.string.scan_list_title_default) },
                        color = ScanTokens.faint,
                        fontSize = 10.sp,
                        textAlign = TextAlign.Center,
                        modifier = Modifier
                            .fillMaxWidth()
                            .clickable { onEvent(ScanEvent.OpenList) }
                            .padding(horizontal = 16.dp, vertical = 4.dp),
                    )
                }
                if (state.feed.isEmpty()) {
                    item { FeedEmpty() }
                } else {
                    // Feed events can repeat the same tag/label/status when an operator rescans.
                    // Include the visible index so Compose keys stay unique for the rolling log.
                    itemsIndexed(
                        state.feed,
                        key = { index, entry -> "${entry.primaryTag}|${entry.vaccineLabel}|${entry.status}|${entry.tone}|$index" },
                        contentType = { _, _ -> "feed_row" },
                    ) { _, entry -> FeedRow(entry) }
                }
                    item { Spacer(Modifier.height(8.dp)) }
                }

                if (state.proofActionNeeded.isNotEmpty()) {
                    ProofActionNeededSection(
                        rows = state.proofActionNeeded,
                        captureEnabled = state.scanEnabled,
                        onEvent = onEvent,
                    )
                }
            }

            ScanFooter(
                label = state.submitLabel.ifBlank { stringResource(R.string.scan_submit_default) },
                enabled = state.canSubmit,
                note = state.footNote,
                onSubmit = { onEvent(ScanEvent.Submit) },
            )
        }
    }

    // Roster overlay (mock ovl-scanlist): opened by a count tile (filtered to that status)
    // or by the "tap … to see the animals" hint (unfiltered). Both are toggles, so closing
    // it — swipe-down, scrim tap, or re-tapping whichever control opened it — replays the
    // same event(s) to clear the state that opened it.
    if (state.selectedFilter != null || state.rosterExpanded) {
        RosterListOverlay(state = state, onEvent = onEvent)
    }
}

@Composable
private fun ReaderConnectionBanner(
    reader: ScanReaderConnection,
    progressLabel: String,
    onReconnect: () -> Unit,
) {
    val bg = if (reader.connected) ScanTokens.okX else ScanTokens.dangerX
    val fg = if (reader.connected) ScanTokens.brandD else ScanTokens.danger
    val border = if (reader.connected) ScanTokens.brand else ScanTokens.danger
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(14.dp))
            .clickable(enabled = !reader.connected) { onReconnect() }
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Box(
            modifier = Modifier
                .size(26.dp)
                .clip(RoundedCornerShape(8.dp))
                .background(bg),
            contentAlignment = Alignment.Center,
        ) {
            Icon(MeshaIcons.Bluetooth, contentDescription = null, tint = fg, modifier = Modifier.size(16.dp))
        }
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(reader.readerName, color = ScanTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.Bold)
            Text(reader.statusLabel, color = fg, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
        }
        Text(progressLabel, color = ScanTokens.muted, fontSize = 12.sp, fontWeight = FontWeight.Black, fontFamily = FontFamily.Monospace)
        if (!reader.connected) {
            Spacer(Modifier.width(10.dp))
            Text(reader.actionLabel, color = fg, fontSize = 12.sp, fontWeight = FontWeight.Black)
        }
    }
}

// --------------------------------------------------------------------------- header
@Composable
private fun ScanHeader(eyebrow: String, title: String, onBack: () -> Unit, onSwitchShed: () -> Unit) {
    val eyebrowText = eyebrow.ifBlank { stringResource(R.string.scan_header_eyebrow) }
    val titleText = title.ifBlank { stringResource(R.string.scan_header_title) }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            // The shell already applies the exact status-bar inset. Keep this row compact so
            // the 48 dp back/action targets begin immediately below that inset instead of
            // looking like a second status-bar spacer.
            .padding(horizontal = 12.dp, vertical = 4.dp),
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(10.dp))
                .clickable { onBack() },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.ChevronLeft,
                contentDescription = stringResource(R.string.scan_back_cd),
                tint = ScanTokens.ink,
                modifier = Modifier.size(22.dp),
            )
        }
        Spacer(Modifier.width(4.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                eyebrowText,
                color = ScanTokens.brandD,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                titleText,
                color = ScanTokens.ink,
                fontSize = 18.sp,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Spacer(Modifier.width(8.dp))
        Text(
            text = stringResource(R.string.scan_switch_shed),
            color = ScanTokens.brandD,
            fontSize = 12.sp,
            fontWeight = FontWeight.Black,
            modifier = Modifier
                .clip(RoundedCornerShape(999.dp))
                .background(ScanTokens.brandSoft)
                .clickable { onSwitchShed() }
                .padding(horizontal = 12.dp, vertical = 8.dp),
        )
    }
}

// --------------------------------------------------------------------------- ring
@Composable
private fun ScanRing(
    done: Int,
    total: Int,
    unitLabel: String,
    isError: Boolean,
    enabled: Boolean,
    onTap: () -> Unit,
) {
    val fraction = if (total > 0) (done.toFloat() / total).coerceIn(0f, 1f) else 0f
    val fg = if (isError) ScanTokens.danger else ScanTokens.brand
    Box(
        modifier = Modifier
            .padding(top = 6.dp, bottom = 2.dp)
            .size(132.dp)
            .then(if (enabled) Modifier.clip(CircleShape).clickable { onTap() } else Modifier),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.fillMaxSize()) {
            val strokeW = 9.dp.toPx()
            val inset = strokeW / 2f
            val arcSize = Size(size.width - strokeW, size.height - strokeW)
            val topLeft = Offset(inset, inset)
            drawArc(
                color = ScanTokens.surf3,
                startAngle = 0f,
                sweepAngle = 360f,
                useCenter = false,
                topLeft = topLeft,
                size = arcSize,
                style = Stroke(width = strokeW),
            )
            drawArc(
                color = fg,
                startAngle = -90f,
                sweepAngle = 360f * fraction,
                useCenter = false,
                topLeft = topLeft,
                size = arcSize,
                style = Stroke(width = strokeW, cap = StrokeCap.Round),
            )
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Row(verticalAlignment = Alignment.Bottom) {
                Text(
                    "$done",
                    color = ScanTokens.ink,
                    fontSize = 28.sp,
                    fontWeight = FontWeight.Black,
                )
                Text(
                    "/$total",
                    color = ScanTokens.faint,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(bottom = 3.dp),
                )
            }
            Text(
                unitLabel.uppercase(),
                color = ScanTokens.muted,
                fontSize = 9.sp,
                fontWeight = FontWeight.Bold,
            )
        }
    }
}

@Composable
private fun TapHint(text: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(ScanTokens.surf)
            .border(1.dp, ScanTokens.hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 13.dp, vertical = 12.dp),
    ) {
        Box(
            modifier = Modifier
                .size(32.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(ScanTokens.brandSoft),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = "RFID",
                color = ScanTokens.brand,
                fontSize = 9.sp,
                lineHeight = 10.sp,
                fontWeight = FontWeight.Black,
                textAlign = TextAlign.Center,
            )
        }
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                "Scan RFID tag now",
                color = ScanTokens.ink,
                fontSize = 14.sp,
                lineHeight = 17.sp,
                fontWeight = FontWeight.Bold,
            )
            Text(
                text,
                color = ScanTokens.muted,
                fontSize = 11.sp,
                lineHeight = 15.sp,
                fontWeight = FontWeight.Medium,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
    }
}

/** Transient strip for a re-scan of an already-DONE tag. Shown once directly under the
 *  "Scan RFID tag now" card instead of stacking a duplicate row in the feed — see
 *  [ScanUiState.duplicateNotice]. Uses the same warning tone as [ScanFeedTone.DUPLICATE]. */
@Composable
private fun DuplicateNoticeStrip(notice: String) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(ScanTokens.warningX)
            .border(1.dp, ScanTokens.warning, RoundedCornerShape(11.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        StatusGlyph(ScanStatus.DONE, tone = ScanFeedTone.DUPLICATE)
        Spacer(Modifier.width(10.dp))
        Text(notice, color = ScanTokens.warning, fontSize = 13.sp, fontWeight = FontWeight.Bold)
    }
}

@Composable
private fun NotDueBanner(err: ScanError) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(ScanTokens.dangerX)
            .border(1.dp, ScanTokens.danger, RoundedCornerShape(11.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        StatusGlyph(ScanStatus.SKIPPED, notDue = true)
        Spacer(Modifier.width(10.dp))
        Column {
            Text(err.message, color = ScanTokens.danger, fontSize = 13.sp, fontWeight = FontWeight.Bold)
            err.tag?.let {
                Text(it, color = ScanTokens.danger, fontSize = 11.sp, fontFamily = FontFamily.Monospace)
            }
        }
    }
}

// --------------------------------------------------------------------------- chips
@Composable
private fun VaccineGroupChips(groups: List<VaccineGroup>, onSelect: (String) -> Unit) {
    // Dep-free wrapping: chunk into rows of up to 3 chips.
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        groups.chunked(3).forEach { rowGroups ->
            Row(
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                rowGroups.forEach { g -> VaccineGroupChip(g) { onSelect(g.id) } }
            }
        }
    }
}

@Composable
private fun VaccineGroupChip(g: VaccineGroup, onClick: () -> Unit) {
    val bg = if (g.active) ScanTokens.brandSoft else ScanTokens.surf
    val border = if (g.active) Color.Transparent else ScanTokens.hair
    val nameColor = if (g.active) ScanTokens.brandD else ScanTokens.ink
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(999.dp))
            .clickable { onClick() }
            .padding(horizontal = 11.dp, vertical = 5.dp),
    ) {
        Text(g.vaccine, color = nameColor, fontSize = 11.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.width(6.dp))
        Text(
            "${g.done}/${g.due}",
            color = if (g.active) ScanTokens.brandD else ScanTokens.muted,
            fontSize = 11.sp,
            fontWeight = FontWeight.Black,
            fontFamily = FontFamily.Monospace,
        )
    }
}

// --------------------------------------------------------------------------- tiles
@Composable
private fun CountTiles(
    done: Int,
    pending: Int,
    skipped: Int,
    labels: ScanTileLabels,
    selected: ScanStatus?,
    onTile: (ScanStatus) -> Unit,
) {
    BoxWithConstraints(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
    ) {
        val gap = 8.dp
        val tileWidth = (maxWidth - (gap * 2)) / 3
        Row(horizontalArrangement = Arrangement.spacedBy(gap), modifier = Modifier.fillMaxWidth()) {
            CountTile(done, labels.done, ScanTokens.brandD, selected == ScanStatus.DONE, Modifier.width(tileWidth)) {
                onTile(ScanStatus.DONE)
            }
            CountTile(pending, labels.pending, ScanTokens.ink, selected == ScanStatus.PENDING, Modifier.width(tileWidth)) {
                onTile(ScanStatus.PENDING)
            }
            CountTile(skipped, labels.skipped, ScanTokens.danger, selected == ScanStatus.SKIPPED, Modifier.width(tileWidth)) {
                onTile(ScanStatus.SKIPPED)
            }
        }
    }
}

@Composable
private fun CountTile(
    count: Int,
    label: String,
    numberColor: Color,
    selected: Boolean,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val bg = if (selected) ScanTokens.brandSoft else ScanTokens.surf
    val border = if (selected) ScanTokens.brand else ScanTokens.hair
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
        modifier = modifier
            .height(64.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(bg)
            .border(1.dp, border, RoundedCornerShape(11.dp))
            .clickable { onClick() }
            .padding(horizontal = 4.dp),
    ) {
        Spacer(Modifier.height(2.dp))
        Text(
            text = "$count",
            color = numberColor,
            fontSize = 17.sp,
            lineHeight = 20.sp,
            fontWeight = FontWeight.Black,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(7.dp))
        Text(
            label.uppercase(),
            color = ScanTokens.muted,
            fontSize = 9.sp,
            lineHeight = 11.sp,
            fontWeight = FontWeight.SemiBold,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

// --------------------------------------------------------------------------- feed
@Composable
private fun FeedRow(entry: ScanFeedEntry) {
    val toneColor = when (entry.tone) {
        ScanFeedTone.ACCEPTED -> ScanTokens.brandD
        ScanFeedTone.DUPLICATE -> ScanTokens.warning
        ScanFeedTone.REJECTED -> ScanTokens.danger
    }
    val tagColor = if (entry.tone == ScanFeedTone.ACCEPTED) ScanTokens.ink else toneColor
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        StatusGlyph(entry.status, tone = entry.tone)
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    entry.primaryTag,
                    color = tagColor,
                    fontSize = 15.sp,
                    lineHeight = 18.sp,
                    fontWeight = FontWeight.SemiBold,
                    fontFamily = FontFamily.Monospace,
                )
                entry.secondaryTag?.let {
                    Spacer(Modifier.width(6.dp))
                    TwoTagsBadge()
                }
            }
            entry.scannedAtLabel?.takeIf { it.isNotBlank() }?.let { label ->
                Text(
                    text = label,
                    color = ScanTokens.brandD,
                    fontSize = 10.sp,
                    lineHeight = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
        }
        Text(entry.vaccineLabel, color = toneColor, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun FeedEmpty() {
    Text(
        stringResource(R.string.scan_feed_empty),
        color = ScanTokens.faint,
        fontSize = 12.sp,
        textAlign = TextAlign.Center,
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 24.dp),
    )
}

@Composable
private fun TwoTagsBadge() {
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(6.dp))
            .background(ScanTokens.surf3)
            .padding(horizontal = 6.dp, vertical = 1.dp),
    ) {
        Text(stringResource(R.string.scan_badge_two_tags), color = ScanTokens.muted, fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun StatusGlyph(status: ScanStatus, notDue: Boolean = false, tone: ScanFeedTone? = null) {
    val (bg, fg, glyph) = when {
        notDue -> Triple(ScanTokens.dangerX, ScanTokens.danger, "✕")
        tone == ScanFeedTone.DUPLICATE -> Triple(ScanTokens.warningX, ScanTokens.warning, "!")
        status == ScanStatus.DONE -> Triple(ScanTokens.okX, ScanTokens.brandD, "✓")
        status == ScanStatus.SKIPPED -> Triple(ScanTokens.dangerX, ScanTokens.danger, "✕")
        else -> Triple(ScanTokens.surf3, ScanTokens.muted, "·")
    }
    Box(
        modifier = Modifier
            .size(24.dp)
            .clip(RoundedCornerShape(8.dp))
            .background(bg),
        contentAlignment = Alignment.Center,
    ) {
        Text(glyph, color = fg, fontSize = 13.sp, fontWeight = FontWeight.Bold)
    }
}

// --------------------------------------------------------------------------- footer
@Composable
private fun ScanFooter(label: String, enabled: Boolean, note: String, onSubmit: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Button(
            onClick = onSubmit,
            enabled = enabled,
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(
                containerColor = ScanTokens.brand,
                contentColor = ScanTokens.onPrimary,
                disabledContainerColor = ScanTokens.surf3,
                disabledContentColor = ScanTokens.muted,
            ),
        ) {
            Text(label, fontWeight = FontWeight.Bold)
        }
        if (note.isNotBlank()) {
            Spacer(Modifier.height(6.dp))
            Text(note, color = ScanTokens.faint, fontSize = 11.sp, textAlign = TextAlign.Center)
        }
    }
}

// ---------------------------------------------------------------------------
// Roster overlay (mock ovl-scanlist) — hosts [ScanListSheet] in a ModalBottomSheet, filtered
// to [ScanUiState.selectedFilter] (a tapped Done/Pending/Skipped tile) or unfiltered when
// opened via [ScanEvent.OpenList]. Dismiss (swipe-down/scrim-tap) replays whichever event(s)
// opened it, so the backing state (selectedFilter/rosterExpanded) stays in sync with the sheet.
// ---------------------------------------------------------------------------
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RosterListOverlay(state: ScanUiState, onEvent: (ScanEvent) -> Unit) {
    val filter = state.selectedFilter
    val rows = remember(state.roster, filter) {
        if (filter == null) state.roster else state.roster.filter { it.status == filter }
    }
    val title = when (filter) {
        ScanStatus.DONE -> state.tileLabels.done
        ScanStatus.PENDING -> state.tileLabels.pending
        ScanStatus.SKIPPED -> state.tileLabels.skipped
        null -> state.listTitle.ifBlank { stringResource(R.string.scan_list_title_default) }
    }
    val dismiss: () -> Unit = {
        if (state.rosterExpanded) onEvent(ScanEvent.OpenList)
        filter?.let { onEvent(ScanEvent.OpenTile(it)) }
    }
    ModalBottomSheet(
        onDismissRequest = dismiss,
        containerColor = ScanTokens.surf,
        contentColor = ScanTokens.ink,
        dragHandle = null, // ScanListSheet draws its own grip below.
    ) {
        ScanListSheet(
            title = title,
            rows = rows,
            captureEnabled = state.scanEnabled,
            hasMore = state.hasMore,
            isLoadingMore = state.isLoadingMore,
            onEvent = onEvent,
        )
    }
}

// ---------------------------------------------------------------------------
// Scan-list sheet (ovl-scanlist) — searchable roster with per-animal vaccine +
// status. Search text is local UI state (allowed); the rows/statuses come from
// the backend roster (with a local unsynced overlay flag for draft UX).
// ---------------------------------------------------------------------------
@Composable
fun ScanListSheet(
    title: String,
    rows: List<RosterRow>,
    captureEnabled: Boolean = true,
    hasMore: Boolean = false,
    isLoadingMore: Boolean = false,
    onEvent: (ScanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    var query by remember { mutableStateOf("") }
    val filtered = remember(query, rows) {
        if (query.isBlank()) {
            rows
        } else {
            rows.filter { r ->
                r.primaryTag.contains(query, ignoreCase = true) ||
                    (r.secondaryTag?.contains(query, ignoreCase = true) == true)
            }
        }
    }
    Surface(color = ScanTokens.surf, modifier = modifier.fillMaxWidth()) {
        Column {
            // grip
            Box(
                modifier = Modifier
                    .padding(top = 8.dp)
                    .fillMaxWidth(),
                contentAlignment = Alignment.Center,
            ) {
                Box(
                    Modifier
                        .width(36.dp)
                        .height(4.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(ScanTokens.surf3),
                )
            }
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.padding(horizontal = 20.dp, vertical = 10.dp),
            ) {
                Text(title, color = ScanTokens.ink, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.width(6.dp))
                Text("· ${filtered.size}", color = ScanTokens.muted, fontSize = 16.sp, fontWeight = FontWeight.Bold)
            }
            OutlinedTextField(
                value = query,
                onValueChange = { query = it },
                singleLine = true,
                placeholder = { Text(stringResource(R.string.scan_search_placeholder), color = ScanTokens.faint, fontSize = 13.sp) },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp),
            )
            Spacer(Modifier.height(4.dp))
            if (filtered.isEmpty()) {
                EmptyState(
                    title = stringResource(R.string.scan_search_empty),
                    icon = MeshaIcons.Goat,
                    tone = EmptyTone.Neutral,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 26.dp),
                )
            } else {
                val rosterListState = rememberLazyListState()
                LaunchedEffect(rosterListState, hasMore, isLoadingMore, filtered.size, query) {
                    if (!hasMore || isLoadingMore || query.isNotBlank() || filtered.isEmpty()) return@LaunchedEffect
                    snapshotFlow { rosterListState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
                        .collect { lastVisibleIndex ->
                            if (lastVisibleIndex >= filtered.lastIndex - 3 && hasMore && !isLoadingMore) {
                                onEvent(ScanEvent.LoadMore)
                            }
                        }
                }
                LazyColumn(
                    state = rosterListState,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    // MOB-011: Use stable keys instead of index to avoid recomposition on insert/reorder.
                    // The key MUST be unique per ROW, not per goat: a multi-vaccine drive (e.g. ET+TT · PPR)
                    // puts the SAME goatId on two rows, so keying by goatId first threw
                    // "Key <uuid> was already used" in LazyColumn measure and popped the screen
                    // (Crashlytics IllegalArgumentException). obligationId is unique per obligation/row;
                    // fall back to a composite that still separates two vaccines of the same goat.
                    items(
                        filtered,
                        key = { row ->
                            row.obligationId.takeIf { it.isNotBlank() }
                                ?: "${row.goatId}|${row.vaccineLabel}|${row.primaryTag}|${row.secondaryTag.orEmpty()}"
                        },
                        contentType = { "scan_row" },
                    ) { row ->
                        ScanListRow(row, captureEnabled, onEvent)
                    }
                    if (isLoadingMore && query.isBlank()) {
                        item {
                            Box(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .padding(horizontal = 16.dp, vertical = 12.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                CircularProgressIndicator(
                                    modifier = Modifier.size(18.dp),
                                    color = ScanTokens.muted,
                                    strokeWidth = 2.dp,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ScanListRow(
    row: RosterRow,
    captureEnabled: Boolean,
    onEvent: (ScanEvent) -> Unit,
) {
    val tagColor = if (row.status == ScanStatus.SKIPPED) ScanTokens.danger else ScanTokens.ink
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            StatusGlyph(row.status)
            Spacer(Modifier.width(10.dp))
            Column(modifier = Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        row.primaryTag,
                        color = tagColor,
                        fontSize = 15.sp,
                        lineHeight = 18.sp,
                        fontWeight = FontWeight.SemiBold,
                        fontFamily = FontFamily.Monospace,
                    )
                    if (row.unsynced) {
                        Spacer(Modifier.width(6.dp))
                        Box(
                            Modifier
                                .size(6.dp)
                                .clip(CircleShape)
                                .background(ScanTokens.brand),
                        )
                    }
                }
                row.secondaryTag?.let {
                    Text("tag 2 · $it", color = ScanTokens.faint, fontSize = 10.sp, fontFamily = FontFamily.Monospace)
                }
                Text(
                    row.vaccineLabel,
                    color = ScanTokens.muted,
                    fontSize = 11.sp,
                    lineHeight = 15.sp,
                    modifier = Modifier.padding(top = 3.dp),
                )
                row.scannedAtLabel?.takeIf { it.isNotBlank() }?.let { label ->
                    Text(
                        text = label,
                        color = ScanTokens.brandD,
                        fontSize = 10.5.sp,
                        lineHeight = 14.sp,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.padding(top = 3.dp),
                    )
                }
            }
        }
        if (row.status == ScanStatus.DONE && row.proofRequired) {
            Spacer(Modifier.height(6.dp))
            ProofActions(row = row, captureEnabled = captureEnabled, onEvent = onEvent)
        }
    }
}

@Composable
private fun ProofActions(
    row: RosterRow,
    captureEnabled: Boolean,
    onEvent: (ScanEvent) -> Unit,
) {
    val (proofLabel, proofColor) = when (row.proofUploadStatus) {
        ProofUploadStatus.MISSING -> stringResource(R.string.scan_proof_needed) to ScanTokens.danger
        ProofUploadStatus.UPLOADING -> stringResource(R.string.scan_proof_uploading) to ScanTokens.warning
        ProofUploadStatus.SYNCED -> pluralStringResource(
            R.plurals.scan_proof_synced,
            row.proofClipCount,
            row.proofClipCount,
        ) to ScanTokens.brandD
        ProofUploadStatus.FAILED -> stringResource(R.string.scan_proof_retry) to ScanTokens.danger
    }
    val addClipLabel = stringResource(R.string.scan_add_clip)
    BoxWithConstraints(modifier = Modifier.fillMaxWidth().padding(start = 34.dp)) {
        // Use the Android compact-window breakpoint. Typical phones are wider than
        // 360 dp, but still need the proof status and primary action stacked; the
        // side-by-side row is reserved for tablet/expanded widths.
        val compact = maxWidth < 600.dp
        val status: @Composable () -> Unit = {
            if (row.proofUploadStatus == ProofUploadStatus.FAILED) {
                TextButton(
                    onClick = { onEvent(ScanEvent.RetryProof(row.goatId)) },
                    enabled = captureEnabled && row.goatId.isNotBlank(),
                    modifier = Modifier.heightIn(min = 48.dp),
                ) {
                    Text(proofLabel, color = proofColor, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                }
            } else {
                Text(
                    proofLabel,
                    color = proofColor,
                    fontSize = 11.sp,
                    lineHeight = 15.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.heightIn(min = 48.dp).padding(vertical = 15.dp),
                )
            }
        }
        val addClip: @Composable (Modifier) -> Unit = { buttonModifier ->
            Button(
                onClick = { onEvent(ScanEvent.CaptureProof(row.goatId)) },
                enabled = captureEnabled && row.goatId.isNotBlank(),
                contentPadding = ButtonDefaults.ContentPadding,
                modifier = buttonModifier.heightIn(min = 48.dp),
            ) {
                Icon(MeshaIcons.Video, contentDescription = null, modifier = Modifier.size(18.dp))
                Spacer(Modifier.width(6.dp))
                Text(addClipLabel, fontSize = 11.sp)
            }
        }
        if (compact) {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                status()
                addClip(Modifier.fillMaxWidth())
            }
        } else {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(modifier = Modifier.weight(1f)) { status() }
                addClip(Modifier)
            }
        }
    }
}

/**
 * Full-roster submit proof gate surface: the DONE animals whose proof video is still
 * MISSING/UPLOADING/FAILED (option 2 — every vaccinated animal needs a synced proof before submit).
 * Renders each one with its tag + [ProofActions] (retry/replace), including animals below the visible
 * scroll window, so the operator can resolve exactly which videos are still pending or failed.
 */
@Composable
private fun ProofActionNeededSection(
    rows: List<RosterRow>,
    captureEnabled: Boolean,
    onEvent: (ScanEvent) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .padding(bottom = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            pluralStringResource(R.plurals.scan_proof_action_needed, rows.size, rows.size),
            color = ScanTokens.danger,
            fontSize = 12.sp,
            fontWeight = FontWeight.Bold,
        )
        rows.forEach { row ->
            Column {
                Text(
                    row.primaryTag,
                    color = ScanTokens.ink,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.Bold,
                    fontFamily = FontFamily.Monospace,
                )
                ProofActions(row = row, captureEnabled = captureEnabled, onEvent = onEvent)
            }
        }
    }
}

// --------------------------------------------------------------------------- preview
private fun previewState() = ScanUiState(
    shedLabel = "Vaccination · Gandhi 1",
    cohortLabel = "Milking does",
    ringDone = 12,
    ringTotal = 40,
    ringUnitLabel = "vaccinated",
    tapHint = "Tap reader to animal — reader shows its due vaccine",
    vaccineGroups = listOf(
        VaccineGroup("g1", "FMD", done = 12, due = 22, active = true),
        VaccineGroup("g2", "HS", done = 0, due = 10, active = false),
        VaccineGroup("g3", "PPR", done = 0, due = 8, active = false),
    ),
    doneCount = 12,
    pendingCount = 27,
    skippedCount = 1,
    tileLabels = ScanTileLabels(done = "Done", pending = "Pending", skipped = "Skipped"),
    feed = listOf(
        ScanFeedEntry("982 000 4512 8830", "900 118 0002 7741", "FMD · 1st", ScanStatus.DONE),
        ScanFeedEntry("982 000 4512 8107", null, "FMD · booster", ScanStatus.DONE),
        ScanFeedEntry("982 000 4512 7654", null, "skip · lactating, defer", ScanStatus.SKIPPED),
    ),
    roster = listOf(
        RosterRow(
            "982 000 4512 8830",
            "900 118 0002 7741",
            "FMD · 1st",
            ScanStatus.DONE,
            unsynced = true,
            goatId = "goat-1",
            proofClipCount = 2,
            proofUploadStatus = ProofUploadStatus.SYNCED,
        ),
        RosterRow("982 000 4512 8107", null, "due · FMD", ScanStatus.PENDING),
        RosterRow("982 000 4512 7654", null, "lactating, defer", ScanStatus.SKIPPED),
    ),
    listTitle = "Tap Done · Pending · Skipped to see the animals",
    submitLabel = "Vaccinate all 40 (12/40)",
    canSubmit = false,
    scanEnabled = true,
    error = null,
    footNote = "eligible → green + buzz + tone · not due → red + double buzz + alert tone",
    isRefreshing = false,
    lastSyncedAt = System.currentTimeMillis(),
    isOffline = false,
)

@Preview(name = "Scan — in progress (dark)")
@Composable
private fun ScanScreenPreview() {
    GoatOsTheme {
        ScanScreen(state = previewState())
    }
}

@Preview(name = "Scan — not-due error")
@Composable
private fun ScanScreenErrorPreview() {
    GoatOsTheme {
        ScanScreen(
            state = previewState().copy(
                error = ScanError(
                    message = "Not due — no due vaccine in this shed",
                    tag = "982 000 4512 9999",
                ),
            ),
        )
    }
}

@Preview(name = "Scan list sheet")
@Composable
private fun ScanListSheetPreview() {
    GoatOsTheme {
        ScanListSheet(
            title = "Scanned this drive",
            rows = previewState().roster,
        )
    }
}
