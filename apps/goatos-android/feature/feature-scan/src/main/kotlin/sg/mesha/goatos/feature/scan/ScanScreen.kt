package sg.mesha.goatos.feature.scan

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
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
)

/** One entry in the live "last taps" feed (given or skipped only). */
data class ScanFeedEntry(
    val primaryTag: String,
    val secondaryTag: String?,
    val vaccineLabel: String,      // "FMD · 1st" or "skip · <reason>"
    val status: ScanStatus,        // DONE or SKIPPED
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

/**
 * Complete, backend-fed state for the Scan screen. Every visible string is a
 * field so nothing is hardcoded in the renderer.
 */
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
)

/** User intents the screen emits; the app/viewmodel layer handles them. */
sealed interface ScanEvent {
    data object Back : ScanEvent
    data object Tap : ScanEvent                            // tap reader / ring to scan
    data object OpenList : ScanEvent                       // open the scan-list sheet
    data object Submit : ScanEvent                         // submit the shed record
    data class SelectGroup(val groupId: String) : ScanEvent
    data class OpenTile(val status: ScanStatus) : ScanEvent
}

// --- mock-ported tokens (dark = default field theme; values from design-system.md) --
private object ScanTokens {
    val brand = MeshaColors.Brand
    val brandD = MeshaColors.BrandD
    val danger = MeshaColors.Danger
    val muted = MeshaColors.Muted
    val faint = MeshaColors.Faint
    val ink = MeshaColors.Ink
    val hair = MeshaColors.Hair
    val surf = MeshaColors.Surf
    val surf3 = MeshaColors.Surf3
    val okX = MeshaColors.OkX        // ~.16 alpha brand
    val dangerX = MeshaColors.DangerX  // ~.15 alpha danger
    val brandSoft = MeshaColors.BrandTint
    val onPrimary = MeshaColors.OnBrand
}

@Composable
fun ScanScreen(
    state: ScanUiState,
    onEvent: (ScanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Surface(color = MaterialTheme.colorScheme.background, modifier = modifier.fillMaxSize()) {
        Column(modifier = Modifier.fillMaxSize()) {
            ScanHeader(state.shedLabel, state.cohortLabel) { onEvent(ScanEvent.Back) }

            // Body scrolls; the submit footer is pinned.
            LazyColumn(
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth(),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
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
                        onTile = { onEvent(ScanEvent.OpenTile(it)) },
                    )
                }
                item {
                    Text(
                        text = state.listTitle,
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
                    items(state.feed) { entry -> FeedRow(entry) }
                }
                item { Spacer(Modifier.height(8.dp)) }
            }

            ScanFooter(
                label = state.submitLabel,
                enabled = state.scanEnabled && state.canSubmit,
                note = state.footNote,
                onSubmit = { onEvent(ScanEvent.Submit) },
            )
        }
    }
}

// --------------------------------------------------------------------------- header
@Composable
private fun ScanHeader(eyebrow: String, title: String, onBack: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Box(
            modifier = Modifier
                .size(38.dp)
                .clip(RoundedCornerShape(10.dp))
                .clickable { onBack() },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.ChevronLeft,
                contentDescription = "Back",
                tint = ScanTokens.ink,
                modifier = Modifier.size(22.dp),
            )
        }
        Spacer(Modifier.width(4.dp))
        Column {
            Text(
                eyebrow,
                color = ScanTokens.brandD,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                title,
                color = ScanTokens.ink,
                fontSize = 18.sp,
                fontWeight = FontWeight.Bold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
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
        horizontalArrangement = Arrangement.Center,
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 6.dp),
    ) {
        Box(
            modifier = Modifier
                .size(20.dp)
                .clip(RoundedCornerShape(6.dp))
                .background(ScanTokens.brandSoft),
            contentAlignment = Alignment.Center,
        ) {
            Icon(imageVector = MeshaIcons.Syringe, contentDescription = null, tint = ScanTokens.brand, modifier = Modifier.size(13.dp))
        }
        Spacer(Modifier.width(7.dp))
        Text(
            text,
            color = ScanTokens.muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.Medium,
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
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
    onTile: (ScanStatus) -> Unit,
) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 4.dp),
    ) {
        CountTile(done, labels.done, ScanTokens.brandD, Modifier.weight(1f)) { onTile(ScanStatus.DONE) }
        CountTile(pending, labels.pending, ScanTokens.ink, Modifier.weight(1f)) { onTile(ScanStatus.PENDING) }
        CountTile(skipped, labels.skipped, ScanTokens.danger, Modifier.weight(1f)) { onTile(ScanStatus.SKIPPED) }
    }
}

@Composable
private fun CountTile(
    count: Int,
    label: String,
    numberColor: Color,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = modifier
            .clip(RoundedCornerShape(11.dp))
            .background(ScanTokens.surf)
            .border(1.dp, ScanTokens.hair, RoundedCornerShape(11.dp))
            .clickable { onClick() }
            .padding(vertical = 6.dp, horizontal = 4.dp),
    ) {
        Text("$count", color = numberColor, fontSize = 16.sp, fontWeight = FontWeight.Black)
        Text(
            label.uppercase(),
            color = ScanTokens.muted,
            fontSize = 9.sp,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.padding(top = 3.dp),
        )
    }
}

// --------------------------------------------------------------------------- feed
@Composable
private fun FeedRow(entry: ScanFeedEntry) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        StatusGlyph(entry.status)
        Spacer(Modifier.width(10.dp))
        Row(modifier = Modifier.weight(1f), verticalAlignment = Alignment.CenterVertically) {
            Text(entry.primaryTag, color = ScanTokens.ink, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
            entry.secondaryTag?.let {
                Spacer(Modifier.width(6.dp))
                TwoTagsBadge()
            }
        }
        Text(entry.vaccineLabel, color = ScanTokens.muted, fontSize = 11.sp)
    }
}

@Composable
private fun FeedEmpty() {
    Text(
        "No taps yet",
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
        Text("2 tags", color = ScanTokens.muted, fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun StatusGlyph(status: ScanStatus, notDue: Boolean = false) {
    val (bg, fg, glyph) = when {
        notDue -> Triple(ScanTokens.dangerX, ScanTokens.danger, "✕")
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
// Scan-list sheet (ovl-scanlist) — searchable roster with per-animal vaccine +
// status. Search text is local UI state (allowed); the rows/statuses come from
// the backend roster (with a local unsynced overlay flag for draft UX).
// ---------------------------------------------------------------------------
@Composable
fun ScanListSheet(
    title: String,
    rows: List<RosterRow>,
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
                placeholder = { Text("Search either RFID tag…", color = ScanTokens.faint, fontSize = 13.sp) },
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp),
            )
            Spacer(Modifier.height(4.dp))
            if (filtered.isEmpty()) {
                Text(
                    "No matching animals",
                    color = ScanTokens.faint,
                    fontSize = 12.sp,
                    textAlign = TextAlign.Center,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 26.dp),
                )
            } else {
                LazyColumn(modifier = Modifier.fillMaxWidth()) {
                    itemsIndexed(filtered) { _, row -> ScanListRow(row) }
                }
            }
        }
    }
}

@Composable
private fun ScanListRow(row: RosterRow) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp, vertical = 10.dp),
    ) {
        StatusGlyph(row.status)
        Spacer(Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(row.primaryTag, color = ScanTokens.ink, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
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
        }
        Text(row.vaccineLabel, color = ScanTokens.muted, fontSize = 11.sp)
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
