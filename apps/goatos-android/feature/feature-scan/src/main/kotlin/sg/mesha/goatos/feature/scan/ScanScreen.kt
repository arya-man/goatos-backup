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
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.widthIn
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone

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

/**
 * Roster animals that have not yet been scanned (backend status "due"/"in_progress" etc.,
 * mapped to [ScanStatus.PENDING] by [sg.mesha.goatos.viewmodel.ScanViewModel]). The scan screen's
 * main body must render one visible row per outstanding animal here, not just a bare count —
 * a "1 PENDING" tile with zero corresponding rows leaves the operator with nothing to tap after
 * a verifier sends an animal back for rework (confirmed live UI defect: 4/5 done rendered only
 * the 4 done rows, the 1 due animal had no row at all).
 *
 * NOTE ON REWORK: the per-animal scan roster (`GET /app/vaccination/execution/sheds/{shed_id}/roster`,
 * [sg.mesha.goatos.core.network.dto.ScanRosterRowDto]) does not carry a rework/rejected indicator
 * distinct from never-scanned — the backend intentionally maps a rejected completion back to
 * status="due" with scannedAt=null (see repository.go's scanRosterSQL CASE, "SENT BACK" comment)
 * so the animal re-enters the same PENDING vocabulary as a never-scanned animal. This screen
 * therefore cannot and must not invent a "sent back" badge from this payload — it renders the
 * animal as due, same as any other outstanding row, until the backend exposes a distinct signal.
 */
fun ScanUiState.rosterRowsAwaitingScan(): List<RosterRow> =
    roster.filter { it.status == ScanStatus.PENDING }

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
    val proofStatusLabel: String? = null,
    val scanSyncFailed: Boolean = false,
    val goatId: String = "",
    val obligationId: String = "",
    /** `obligation_instances.row_version` for [obligationId] — the server-issued capture-cycle
     *  discriminator. See `sg.mesha.goatos.core.data.capture.scanCaptureIdempotencyKey`. */
    val obligationRowVersion: Int = 0,
    val proofRequired: Boolean = true,
    val proofClipCount: Int = 0,
    val proofUploadStatus: ProofUploadStatus = ProofUploadStatus.MISSING,
    val evidenceCount: Int = 0,
    val evidenceSyncedCount: Int = 0,
    val evidenceUploading: Boolean = false,
    val evidenceFailed: Boolean = false,
    val captureInFlight: Boolean = false,
    val canCaptureEvidence: Boolean = false,
)

/** One entry in the live "last taps" feed (given or skipped only). */
data class ScanFeedEntry(
    val primaryTag: String,
    val secondaryTag: String?,
    val vaccineLabel: String,      // "FMD · 1st" or "skip · <reason>"
    val status: ScanStatus,        // DONE or SKIPPED
    val scannedAtLabel: String? = null,
    val proofStatusLabel: String? = null,
    val goatId: String = "",
    val proofRequired: Boolean = false,
    val proofUploadStatus: ProofUploadStatus = ProofUploadStatus.MISSING,
    val evidenceSyncedCount: Int = 0,
    val evidenceUploading: Boolean = false,
    val evidenceFailed: Boolean = false,
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

/** One backend/Room-backed destination in the current vaccination drive. */
@Immutable
data class ShedSwitchOption(
    val shedId: String,
    val shedLabel: String,
    val taskId: String,
    val driveId: String?,
    val batchId: String?,
    val sopVersionId: String?,
    val taskRowVersion: Int?,
    val scannedCount: Int,
    val animalCount: Int,
    val videoCount: Int,
    val syncedVideoCount: Int,
    val syncingCount: Int,
    val needsAttentionCount: Int,
    val isCurrent: Boolean,
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
    val captureAccessRequired: Boolean = false, // operator execution route needs camera/RFID/upload access even after scans finish
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
    val proofActionNeeded: List<RosterRow> = emptyList(),
    val duplicateNotice: String? = null,
    val readerConnection: ScanReaderConnection? = null,
    val shedId: String? = null,
    val taskId: String? = null,
    val sopVersionId: String? = null,
    val taskRowVersion: Int? = null,
    val evidenceError: String? = null,
    val shedOptions: List<ShedSwitchOption> = emptyList(),
    val canSwitchShed: Boolean = false,
    val shedSwitcherOpen: Boolean = false,
    val shedSwitcherRefreshing: Boolean = false,
    val shedSwitcherOffline: Boolean = false,
    val proofReplacementGoatId: String? = null,
    val showSubmitConfirmation: Boolean = false,
)

/** User intents the screen emits; the app/viewmodel layer handles them. */
sealed interface ScanEvent {
    data object Back : ScanEvent
    data object OpenList : ScanEvent                       // open the scan-list sheet
    data object Submit : ScanEvent                         // submit the shed record
    data object LoadMore : ScanEvent                       // fetch one bounded continuation page
    data object ReconnectReader : ScanEvent                // quick path back to RFID reconnect
    data object OpenShedSwitcher : ScanEvent
    data object DismissShedSwitcher : ScanEvent
    data class SwitchShed(val option: ShedSwitchOption) : ScanEvent
    data class CaptureVideo(val goatId: String) : ScanEvent
    data class CaptureProof(val goatId: String) : ScanEvent
    data class RetryProof(val goatId: String) : ScanEvent
    data class ArmProofReplacement(val goatId: String) : ScanEvent
    data class SelectGroup(val groupId: String) : ScanEvent
    data class OpenTile(val status: ScanStatus) : ScanEvent
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

/**
 * DELIBERATELY EXEMPT from [sg.mesha.goatos.core.ui.RefreshOnResume].
 *
 * This is a MID-CAPTURE surface, the exact case the refresh-on-open rule carves out ("Do not use
 * this on screens with in-progress user input (scan-capture, forms)" — RefreshOnResume KDoc,
 * docs/decisions/android-offline-first.md). The operator's session state here — the per-animal
 * draft done overlay built from live RFID reads, the captured-but-unsynced proofs, the grown scan
 * window — is UNSUBMITTED work that exists only in the ViewModel. A resume-triggered
 * `refreshScanRoster` re-pulls the backend roster; every camera/proof capture, permission dialog
 * and app-switch fires ON_RESUME, so a resume refresh would repeatedly re-baseline the roster
 * under a half-finished shed and could discard reads the operator has already made. That is lost
 * operator work, which is strictly worse than showing a roster that is a few minutes stale.
 *
 * There is accordingly no `ScanEvent.Refresh` to call: the roster is refreshed once on entry
 * (`ScanViewModel.init`) and continuation pages are pulled explicitly via [ScanEvent.LoadMore].
 * Post-submit freshness is covered by the LIST screens the operator returns to, which ARE
 * RefreshOnResume.
 */
@Composable
fun ScanScreen(
    state: ScanUiState,
    onEvent: (ScanEvent) -> Unit = {},
    onConfirmSubmit: () -> Unit = {},
    onDismissSubmitConfirmation: () -> Unit = {},
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
                canSwitchShed = state.canSwitchShed,
                onBack = { onEvent(ScanEvent.Back) },
                onSwitchShed = { onEvent(ScanEvent.OpenShedSwitcher) },
            )

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
                        onReconnect = { onEvent(ScanEvent.ReconnectReader) },
                    )
                }
                item {
                    ScanRing(
                        done = state.ringDone,
                        total = state.ringTotal,
                        unitLabel = state.ringUnitLabel,
                        tone = when {
                            state.error != null -> ScanRingTone.ERROR
                            state.proofActionNeeded.isNotEmpty() -> ScanRingTone.WARNING
                            else -> ScanRingTone.SUCCESS
                        },
                    )
                }
                if (state.error != null) {
                    item { NotDueBanner(state.error) }
                } else {
                    state.duplicateNotice?.takeIf { it.isNotBlank() }?.let { message ->
                        item { OperatorNoticeBanner(message) }
                    }
                    state.evidenceError?.takeIf { it.isNotBlank() }?.let { message ->
                        item { OperatorNoticeBanner(message) }
                    }
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
                        proofWarning = state.proofActionNeeded.isNotEmpty(),
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
                            .padding(horizontal = 16.dp, vertical = 4.dp),
                    )
                }
                state.listTitle.takeIf { it.isNotBlank() }?.let { title ->
                    item {
                        Text(
                            text = title,
                            color = ScanTokens.faint,
                            fontSize = 10.sp,
                            textAlign = TextAlign.Center,
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable { onEvent(ScanEvent.OpenList) }
                                .padding(start = 16.dp, end = 16.dp, top = 2.dp, bottom = 1.dp),
                        )
                    }
                }
                if (state.proofActionNeeded.isNotEmpty()) {
                    item { ProofGate(rows = state.proofActionNeeded) }
                    items(
                        state.proofActionNeeded,
                        key = { row ->
                            val baseParts = listOf(row.goatId, row.vaccineLabel, row.primaryTag)
                            val rowId = row.obligationId.takeIf { it.isNotBlank() }
                                ?: (baseParts + row.secondaryTag.orEmpty())
                                    .filter { it.isNotBlank() }
                                    .joinToString("|")
                            "proof-$rowId"
                        },
                        contentType = { "proof_needed_row" },
                    ) { row ->
                        ProofNeededFeedRow(
                            row = row,
                            armedForReplacement = state.proofReplacementGoatId == row.goatId,
                        ) {
                            if (row.proofUploadStatus == ProofUploadStatus.FAILED || row.evidenceFailed) {
                                onEvent(ScanEvent.RetryProof(row.goatId))
                            } else if (row.proofUploadStatus == ProofUploadStatus.SYNCED || row.evidenceSyncedCount > 0) {
                                onEvent(ScanEvent.ArmProofReplacement(row.goatId))
                            }
                        }
                    }
                }
                val proofActionTags = state.proofActionNeeded.flatMap { row ->
                    listOfNotNull(row.primaryTag, row.secondaryTag)
                }.toSet()
                val visibleFeed = state.feed.filterNot { entry ->
                    entry.primaryTag in proofActionTags || entry.secondaryTag in proofActionTags
                }
                if (visibleFeed.isEmpty()) {
                    if (state.proofActionNeeded.isEmpty()) {
                        item { FeedEmpty() }
                    }
                } else {
                    // Feed events can repeat the same tag/label/status when an operator rescans.
                    // Include the visible index so Compose keys stay unique for the rolling log.
                    itemsIndexed(
                        visibleFeed,
                        key = { index, entry -> "${entry.primaryTag}|${entry.vaccineLabel}|${entry.status}|${entry.tone}|$index" },
                        contentType = { _, _ -> "feed_row" },
                    ) { _, entry ->
                        FeedRow(
                            entry = entry,
                            armedForReplacement = state.proofReplacementGoatId == entry.goatId,
                            onReplace = { onEvent(ScanEvent.ArmProofReplacement(entry.goatId)) },
                        )
                    }
                }
                item { Spacer(Modifier.height(8.dp)) }
            }

            ScanFooter(
                label = state.submitLabel.ifBlank { stringResource(R.string.scan_submit_default) },
                enabled = state.scanEnabled && state.canSubmit,
                note = state.footNote,
                onSubmit = { onEvent(ScanEvent.Submit) },
            )
        }
    }

    if (state.showSubmitConfirmation) {
        SubmitConfirmationDialog(
            title = stringResource(R.string.scan_submit_confirmation_title),
            subtitle = stringResource(R.string.scan_submit_confirmation_subtitle),
            dismissLabel = stringResource(R.string.scan_submit_confirmation_dismiss),
            confirmLabel = stringResource(R.string.scan_submit_confirmation_confirm),
            onConfirm = onConfirmSubmit,
            onDismiss = onDismissSubmitConfirmation,
        )
    }

    // Roster overlay (mock ovl-scanlist): opened by a count tile (filtered to that status)
    // or by the "tap … to see the animals" hint (unfiltered). Both are toggles, so closing
    // it — swipe-down, scrim tap, or re-tapping whichever control opened it — replays the
    // same event(s) to clear the state that opened it.
    if (state.selectedFilter != null || state.rosterExpanded) {
        RosterListOverlay(state = state, onEvent = onEvent)
    }
    if (state.shedSwitcherOpen) {
        ShedSwitcherOverlay(state = state, onEvent = onEvent)
    }
}

@Composable
private fun ReaderConnectionBanner(
    reader: ScanReaderConnection,
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
        if (!reader.connected) {
            Spacer(Modifier.width(10.dp))
            Text(reader.actionLabel, color = fg, fontSize = 12.sp, fontWeight = FontWeight.Black)
        }
    }
}

// --------------------------------------------------------------------------- header
@Composable
private fun ScanHeader(
    eyebrow: String,
    title: String,
    canSwitchShed: Boolean,
    onBack: () -> Unit,
    onSwitchShed: () -> Unit,
) {
    val eyebrowText = eyebrow.ifBlank { stringResource(R.string.scan_header_eyebrow) }
    val titleText = title.ifBlank { stringResource(R.string.scan_header_title) }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 4.dp),
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(ScanTokens.surf)
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
        if (canSwitchShed) {
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
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ShedSwitcherOverlay(state: ScanUiState, onEvent: (ScanEvent) -> Unit) {
    ModalBottomSheet(
        onDismissRequest = { onEvent(ScanEvent.DismissShedSwitcher) },
        containerColor = ScanTokens.surf,
        contentColor = ScanTokens.ink,
    ) {
        ShedSwitcherSheet(state = state, onEvent = onEvent)
    }
}

/** Stateless sheet body kept public for screenshot/accessibility tests. */
@Composable
fun ShedSwitcherSheet(state: ScanUiState, onEvent: (ScanEvent) -> Unit = {}) {
    Surface(color = ScanTokens.surf, modifier = Modifier.fillMaxWidth()) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp)
                .padding(bottom = 24.dp),
        ) {
            Text(
                stringResource(R.string.scan_switch_shed_title),
                color = ScanTokens.ink,
                fontSize = 18.sp,
                fontWeight = FontWeight.Bold,
            )
            Text(
                stringResource(R.string.scan_switch_shed_subtitle),
                color = ScanTokens.muted,
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 2.dp, bottom = 12.dp),
            )
            SyncStatusIndicator(
                isRefreshing = state.shedSwitcherRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                hasData = state.shedOptions.isNotEmpty(),
                isOffline = state.shedSwitcherOffline,
                modifier = Modifier.fillMaxWidth(),
            )
            if (state.shedOptions.isEmpty()) {
                Text(
                    text = if (state.shedSwitcherRefreshing) {
                        stringResource(R.string.scan_switch_shed_loading)
                    } else {
                        stringResource(R.string.scan_switch_shed_empty)
                    },
                    color = ScanTokens.muted,
                    fontSize = 13.sp,
                    modifier = Modifier.padding(vertical = 24.dp),
                )
            } else {
                LazyColumn(
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(420.dp),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    items(
                        items = state.shedOptions,
                        key = { "partition:${it.shedLabel}|${it.shedId}|${it.taskId}" },
                        contentType = { "shed_switch_option" },
                    ) { option ->
                        ShedSwitchRow(
                            option = option,
                            onClick = { onEvent(ScanEvent.SwitchShed(option)) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun ShedSwitchRow(option: ShedSwitchOption, onClick: () -> Unit) {
    val syncing = option.syncingCount > 0
    val statusColor = when {
        option.needsAttentionCount > 0 -> ScanTokens.danger
        syncing -> ScanTokens.warning
        option.scannedCount > 0 -> ScanTokens.brandD
        else -> ScanTokens.muted
    }
    val status = when {
        option.needsAttentionCount > 0 ->
            pluralStringResource(
                R.plurals.scan_switch_shed_attention,
                option.needsAttentionCount,
                option.needsAttentionCount,
            )
        syncing -> pluralStringResource(
            R.plurals.scan_switch_shed_syncing,
            option.syncingCount,
            option.syncingCount,
        )
        option.scannedCount > 0 -> stringResource(R.string.scan_switch_shed_synced)
        else -> stringResource(R.string.scan_switch_shed_not_started)
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(if (option.isCurrent) ScanTokens.brandSoft else ScanTokens.surf3)
            .border(
                width = 1.dp,
                color = if (option.isCurrent) ScanTokens.brand else ScanTokens.hair,
                shape = RoundedCornerShape(14.dp),
            )
            .clickable(enabled = !option.isCurrent && option.taskId.isNotBlank()) { onClick() }
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                option.shedLabel,
                color = ScanTokens.ink,
                fontSize = 15.sp,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.weight(1f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (option.isCurrent) {
                Text(
                    stringResource(R.string.scan_switch_shed_current),
                    color = ScanTokens.brandD,
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Bold,
                )
            } else {
                Icon(
                    imageVector = MeshaIcons.Chevron,
                    contentDescription = null,
                    tint = ScanTokens.muted,
                    modifier = Modifier.size(16.dp),
                )
            }
        }
        Spacer(Modifier.width(8.dp))
        Text(
            stringResource(
                R.string.scan_switch_shed_progress_fmt,
                option.scannedCount,
                option.animalCount,
                option.videoCount,
                option.syncedVideoCount,
            ),
            color = ScanTokens.muted,
            fontSize = 11.sp,
            modifier = Modifier.padding(top = 5.dp),
        )
        Text(
            status,
            color = statusColor,
            fontSize = 11.sp,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.padding(top = 3.dp),
        )
    }
}

// --------------------------------------------------------------------------- ring
private enum class ScanRingTone { SUCCESS, WARNING, ERROR }

@Composable
private fun ScanRing(
    done: Int,
    total: Int,
    unitLabel: String,
    tone: ScanRingTone,
) {
    val fraction = if (total > 0) (done.toFloat() / total).coerceIn(0f, 1f) else 0f
    val fg = when (tone) {
        ScanRingTone.SUCCESS -> ScanTokens.brand
        ScanRingTone.WARNING -> ScanTokens.warning
        ScanRingTone.ERROR -> ScanTokens.danger
    }
    Box(
        modifier = Modifier
            .padding(top = 6.dp, bottom = 2.dp)
            .size(132.dp),
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
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            modifier = Modifier.offset(y = if (unitLabel.isBlank()) 0.dp else 2.dp),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier.offset(y = if (unitLabel.isBlank()) 0.dp else 1.dp),
            ) {
                Text(
                    "$done",
                    color = ScanTokens.ink,
                    fontSize = 28.sp,
                    lineHeight = 28.sp,
                    fontWeight = FontWeight.Black,
                )
                Text(
                    "/$total",
                    color = ScanTokens.faint,
                    fontSize = 13.sp,
                    lineHeight = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(start = 1.dp, top = 7.dp),
                )
            }
            if (unitLabel.isNotBlank()) {
                Text(
                    unitLabel.uppercase(),
                    color = ScanTokens.muted,
                    fontSize = 9.sp,
                    lineHeight = 10.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.padding(top = 3.dp),
                )
            }
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

@Composable
private fun OperatorNoticeBanner(message: String) {
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
        Text(
            text = message,
            color = ScanTokens.warning,
            fontSize = 12.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
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
    proofWarning: Boolean,
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
            CountTile(done, labels.done, if (proofWarning) ScanTokens.warning else ScanTokens.brandD, selected == ScanStatus.DONE, Modifier.width(tileWidth)) {
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
private fun FeedRow(
    entry: ScanFeedEntry,
    armedForReplacement: Boolean = false,
    onReplace: () -> Unit = {},
) {
    val toneColor = when (entry.tone) {
        ScanFeedTone.ACCEPTED -> ScanTokens.brandD
        ScanFeedTone.DUPLICATE -> ScanTokens.warning
        ScanFeedTone.REJECTED -> ScanTokens.danger
    }
    val tagColor = if (entry.tone == ScanFeedTone.ACCEPTED) ScanTokens.ink else toneColor
    val secondary = when {
        armedForReplacement -> stringResource(R.string.scan_proof_replace_armed)
        // Synced is terminal: check it BEFORE failed/uploading so a green row can never also claim
        // its proof is still uploading or failed.
        entry.proofUploadStatus == ProofUploadStatus.SYNCED || entry.evidenceSyncedCount > 0 ->
            entry.proofStatusLabel ?: entry.scannedAtLabel
        entry.evidenceFailed || entry.proofUploadStatus == ProofUploadStatus.FAILED ->
            entry.proofStatusLabel ?: "Upload failed · auto retrying"
        entry.evidenceUploading || entry.proofUploadStatus == ProofUploadStatus.UPLOADING ->
            entry.proofStatusLabel ?: "Uploading proof…"
        else -> entry.scannedAtLabel
    }
    val secondaryColor = when {
        entry.proofUploadStatus == ProofUploadStatus.SYNCED || entry.evidenceSyncedCount > 0 ->
            if (entry.tone == ScanFeedTone.ACCEPTED) ScanTokens.brandD else toneColor
        entry.evidenceFailed || entry.proofUploadStatus == ProofUploadStatus.FAILED -> ScanTokens.danger
        entry.evidenceUploading || entry.proofUploadStatus == ProofUploadStatus.UPLOADING -> ScanTokens.warning
        entry.tone == ScanFeedTone.ACCEPTED -> ScanTokens.brandD
        else -> toneColor
    }
    val replaceable = entry.status == ScanStatus.DONE &&
        entry.proofRequired &&
        (entry.proofUploadStatus == ProofUploadStatus.SYNCED || entry.evidenceSyncedCount > 0)
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        StatusGlyph(entry.status, tone = entry.tone)
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            TagLine(primaryTag = entry.primaryTag, secondaryTag = entry.secondaryTag, color = tagColor)
            if (!secondary.isNullOrBlank()) {
                Text(
                    secondary,
                    color = secondaryColor,
                    fontSize = 10.sp,
                    lineHeight = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(top = 2.dp),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Text(
                entry.vaccineLabel,
                color = toneColor,
                fontSize = 11.sp,
                lineHeight = 13.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        if (replaceable) {
            Spacer(Modifier.width(10.dp))
            ProofRowActionPill(
                label = stringResource(R.string.scan_proof_replace_action),
                toneColor = toneColor,
                onClick = onReplace,
            )
        }
    }
}

@Composable
private fun ProofGate(rows: List<RosterRow>) {
    val failed = rows.any { it.proofUploadStatus == ProofUploadStatus.FAILED || it.evidenceFailed }
    val count = rows.size
    val copy = if (count == 1) {
        stringResource(R.string.scan_one_animal_needs_proof)
    } else {
        stringResource(R.string.scan_many_animals_need_proof, count)
    }
    Text(
        text = copy,
        color = if (failed) ScanTokens.danger else ScanTokens.warning,
        fontSize = 13.sp,
        lineHeight = 18.sp,
        fontWeight = FontWeight.Bold,
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, top = 0.dp, bottom = 4.dp),
    )
}

@Composable
private fun ProofNeededFeedRow(
    row: RosterRow,
    armedForReplacement: Boolean = false,
    onAction: () -> Unit = {},
) {
    val (line, tone) = proofLineAndTone(row)
    val retryable = row.proofUploadStatus == ProofUploadStatus.FAILED || row.evidenceFailed
    val replaceable = row.proofUploadStatus == ProofUploadStatus.SYNCED || row.evidenceSyncedCount > 0
    ScanRosterFlatRow(
        primaryTag = row.primaryTag,
        secondaryTag = row.secondaryTag,
        vaccineLabel = row.vaccineLabel,
        status = ScanStatus.DONE,
        tone = tone,
        secondaryLine = if (armedForReplacement) stringResource(R.string.scan_proof_replace_armed) else line,
        actionLabel = when {
            retryable -> stringResource(R.string.scan_proof_retry_action)
            replaceable -> stringResource(R.string.scan_proof_replace_action)
            else -> null
        },
        onAction = onAction,
        modifier = if (retryable || replaceable) Modifier.clickable { onAction() } else Modifier,
    )
}

@Composable
private fun ScanRosterFlatRow(
    primaryTag: String,
    secondaryTag: String?,
    vaccineLabel: String,
    status: ScanStatus,
    tone: ScanFeedTone,
    secondaryLine: String?,
    actionLabel: String? = null,
    onAction: () -> Unit = {},
    showStatusGlyph: Boolean = true,
    compactStatusGlyph: Boolean = false,
    pendingGlyph: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val toneColor = when (tone) {
        ScanFeedTone.ACCEPTED -> ScanTokens.brandD
        ScanFeedTone.DUPLICATE -> ScanTokens.warning
        ScanFeedTone.REJECTED -> ScanTokens.danger
    }
    val tagColor = if (tone == ScanFeedTone.REJECTED) ScanTokens.danger else ScanTokens.ink
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = modifier
            .fillMaxWidth()
            .height(68.dp)
            .padding(horizontal = 16.dp, vertical = 6.dp),
    ) {
        if (showStatusGlyph) {
            StatusGlyph(status, tone = tone, compact = compactStatusGlyph)
            Spacer(Modifier.width(10.dp))
        } else if (pendingGlyph) {
            PendingGlyph()
            Spacer(Modifier.width(10.dp))
        }
        Column(modifier = Modifier.weight(1f)) {
            TagLine(primaryTag = primaryTag, secondaryTag = secondaryTag, color = tagColor)
            if (!secondaryLine.isNullOrBlank()) {
                Text(
                    secondaryLine,
                    color = toneColor,
                    fontSize = 11.sp,
                    lineHeight = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(top = 2.dp),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Text(
                vaccineLabel,
                color = toneColor,
                fontSize = 11.sp,
                lineHeight = 13.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
        if (!actionLabel.isNullOrBlank()) {
            Spacer(Modifier.width(10.dp))
            ProofRowActionPill(
                label = actionLabel,
                toneColor = toneColor,
                onClick = onAction,
            )
        }
    }
}

@Composable
private fun ProofRowActionPill(
    label: String,
    toneColor: Color,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .widthIn(min = 76.dp)
            .height(36.dp)
            .clip(RoundedCornerShape(999.dp))
            .background(toneColor.copy(alpha = 0.22f))
            .border(1.dp, toneColor.copy(alpha = 0.48f), RoundedCornerShape(999.dp))
            .clickable { onClick() }
            .padding(horizontal = 13.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            label,
            color = ScanTokens.ink,
            fontSize = 11.sp,
            lineHeight = 11.sp,
            fontWeight = FontWeight.Black,
            textAlign = TextAlign.Center,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun PendingGlyph() {
    Box(
        modifier = Modifier
            .size(20.dp)
            .clip(CircleShape)
            .border(2.dp, ScanTokens.hair, CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            modifier = Modifier
                .size(6.dp)
                .clip(CircleShape)
                .background(ScanTokens.faint.copy(alpha = 0.45f)),
        )
    }
}

@Composable
private fun TagLine(primaryTag: String, secondaryTag: String?, color: Color) {
    BoxWithConstraints {
        val tagWidth = (this@BoxWithConstraints.maxWidth - if (secondaryTag != null) 52.dp else 0.dp)
            .coerceAtMost(178.dp)
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                primaryTag,
                color = color,
                fontSize = 15.sp,
                lineHeight = 18.sp,
                fontWeight = FontWeight.SemiBold,
                fontFamily = FontFamily.Monospace,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.widthIn(max = tagWidth),
            )
            if (secondaryTag != null) {
                Spacer(Modifier.width(6.dp))
                TwoTagsBadge()
            }
        }
    }
}

private fun proofLineAndTone(row: RosterRow): Pair<String, ScanFeedTone> = when {
    row.proofUploadStatus == ProofUploadStatus.UPLOADING || row.evidenceUploading ->
        (row.proofStatusLabel ?: "Uploading proof · retrying if needed") to ScanFeedTone.DUPLICATE
    row.proofUploadStatus == ProofUploadStatus.FAILED || row.evidenceFailed ->
        (row.proofStatusLabel ?: "Upload failed · scan again to replace") to ScanFeedTone.REJECTED
    row.proofUploadStatus == ProofUploadStatus.SYNCED || row.evidenceSyncedCount > 0 ->
        (row.proofStatusLabel ?: "Proof synced") to ScanFeedTone.ACCEPTED
    else ->
        "Scan again to record proof" to ScanFeedTone.DUPLICATE
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
            .width(46.dp)
            .height(22.dp)
            .clip(RoundedCornerShape(6.dp))
            .background(ScanTokens.surf3),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            stringResource(R.string.scan_badge_two_tags),
            color = ScanTokens.muted,
            fontSize = 9.sp,
            lineHeight = 10.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
        )
    }
}

@Composable
private fun StatusGlyph(status: ScanStatus, notDue: Boolean = false, tone: ScanFeedTone? = null, compact: Boolean = false) {
    val (bg, fg, glyph) = when {
        notDue -> Triple(ScanTokens.dangerX, ScanTokens.danger, "✕")
        tone == ScanFeedTone.DUPLICATE -> Triple(ScanTokens.warningX, ScanTokens.warning, "!")
        tone == ScanFeedTone.REJECTED -> Triple(ScanTokens.dangerX, ScanTokens.danger, "✕")
        status == ScanStatus.DONE -> Triple(ScanTokens.okX, ScanTokens.brandD, "✓")
        status == ScanStatus.SKIPPED -> Triple(ScanTokens.dangerX, ScanTokens.danger, "✕")
        else -> Triple(Color.Transparent, ScanTokens.muted, "")
    }
    val size = if (compact) 20.dp else 24.dp
    val corner = if (compact) 7.dp else 8.dp
    Box(
        modifier = Modifier
            .size(size)
            .clip(RoundedCornerShape(corner))
            .then(if (glyph.isBlank()) Modifier.border(1.dp, ScanTokens.hair, RoundedCornerShape(corner)) else Modifier)
            .background(bg),
        contentAlignment = Alignment.Center,
    ) {
        Text(glyph, color = fg, fontSize = if (compact) 11.sp else 13.sp, fontWeight = FontWeight.Bold)
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
        null -> state.listTitle
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
            proofReplacementGoatId = state.proofReplacementGoatId,
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
    proofReplacementGoatId: String? = null,
    hasMore: Boolean = false,
    isLoadingMore: Boolean = false,
    onEvent: (ScanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    @Suppress("UNUSED_VARIABLE")
    val proofCaptureIsAutomatic = captureEnabled
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
                LazyColumn(state = rosterListState, modifier = Modifier.fillMaxWidth()) {
                    // MOB-011: Use stable keys instead of index to avoid recomposition on insert/reorder
                    items(
                        filtered,
                        key = { row ->
                            row.obligationId.takeIf { it.isNotBlank() }
                                ?: "${row.goatId}|${row.vaccineLabel}|${row.primaryTag}|${row.secondaryTag.orEmpty()}"
                        },
                        contentType = { "scan_row" },
                    ) { row -> ScanListRow(row = row) }
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
private fun InlineScannedGoatCard(row: RosterRow, onEvent: (ScanEvent) -> Unit) {
    val vaccineHeading = if (row.status == ScanStatus.DONE) "Vaccines covered for this goat" else "Due vaccines for this goat"
    // Synced is terminal and is checked before syncing/failed, so a goat whose clip already reached
    // the backend never reads as still syncing.
    val proofText = when {
        row.captureInFlight -> "Opening camera…"
        row.evidenceSyncedCount > 0 -> "${row.evidenceSyncedCount} clip${if (row.evidenceSyncedCount == 1) "" else "s"} ready"
        row.evidenceUploading -> "${row.evidenceCount} clip${if (row.evidenceCount == 1) "" else "s"} syncing"
        row.evidenceFailed -> "Proof upload needs retry"
        else -> "Proof needed"
    }
    val proofColor = when {
        row.evidenceSyncedCount > 0 -> ScanTokens.brand
        row.evidenceFailed -> ScanTokens.danger
        else -> ScanTokens.warning
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 6.dp)
            .background(ScanTokens.surf, RoundedCornerShape(18.dp))
            .border(1.dp, ScanTokens.hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
    ) {
        Row(verticalAlignment = Alignment.Top) {
            StatusGlyph(row.status)
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    row.primaryTag,
                    color = ScanTokens.ink,
                    fontSize = 16.sp,
                    lineHeight = 19.sp,
                    fontWeight = FontWeight.Bold,
                    fontFamily = FontFamily.Monospace,
                )
                row.secondaryTag?.let {
                    Text("second tag · $it", color = ScanTokens.faint, fontSize = 10.sp, fontFamily = FontFamily.Monospace)
                }
            }
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = vaccineHeading,
                    color = ScanTokens.faint,
                    fontSize = 9.sp,
                    fontWeight = FontWeight.Bold,
                )
                Text(
                    text = row.vaccineLabel,
                    color = ScanTokens.ink,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.SemiBold,
                    lineHeight = 15.sp,
                )
            }
            Spacer(Modifier.width(10.dp))
            Box(
                modifier = Modifier
                    .background(proofColor.copy(alpha = 0.14f), RoundedCornerShape(999.dp))
                    .border(1.dp, proofColor.copy(alpha = 0.35f), RoundedCornerShape(999.dp))
                    .padding(horizontal = 10.dp, vertical = 5.dp),
            ) {
                Text(proofText, color = proofColor, fontSize = 10.5.sp, fontWeight = FontWeight.Bold)
            }
        }
    }
}

@Composable
private fun ScanListRow(row: RosterRow) {
    val tone = when {
        row.status == ScanStatus.SKIPPED -> ScanFeedTone.REJECTED
        row.proofUploadStatus == ProofUploadStatus.FAILED || row.evidenceFailed -> ScanFeedTone.REJECTED
        row.status == ScanStatus.DONE && row.proofRequired &&
            row.proofUploadStatus != ProofUploadStatus.SYNCED &&
            row.evidenceSyncedCount <= 0 -> ScanFeedTone.DUPLICATE
        else -> ScanFeedTone.ACCEPTED
    }
    val secondaryLine = when {
        row.status == ScanStatus.PENDING -> null
        // Synced is terminal: check it BEFORE uploading/failed so a row that renders green can never
        // also claim its proof is still uploading.
        row.proofUploadStatus == ProofUploadStatus.SYNCED || row.evidenceSyncedCount > 0 ->
            row.proofStatusLabel ?: row.scannedAtLabel
        row.proofUploadStatus == ProofUploadStatus.UPLOADING || row.evidenceUploading ->
            row.proofStatusLabel ?: "Uploading proof · retrying if needed"
        row.proofUploadStatus == ProofUploadStatus.FAILED || row.evidenceFailed ->
            row.proofStatusLabel ?: "Upload failed · retry or scan again"
        row.scannedAtLabel != null -> row.scannedAtLabel
        row.status == ScanStatus.DONE && tone == ScanFeedTone.DUPLICATE -> "Scan again to record proof"
        else -> null
    }
    ScanRosterFlatRow(
        primaryTag = row.primaryTag,
        secondaryTag = row.secondaryTag,
        vaccineLabel = row.vaccineLabel,
        status = row.status,
        tone = tone,
        secondaryLine = secondaryLine,
        showStatusGlyph = row.status != ScanStatus.PENDING,
        compactStatusGlyph = true,
        pendingGlyph = row.status == ScanStatus.PENDING,
    )
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
        RosterRow("982 000 4512 8830", "900 118 0002 7741", "FMD · 1st", ScanStatus.DONE, unsynced = true),
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
