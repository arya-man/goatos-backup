package sg.mesha.goatos.feature.sheds

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme

/**
 * Today's sheds / Drive status (`v-sheds`).
 *
 * A backend-scoped lens rendered from [ShedsUiState]: same route, same screen, for
 * operator (execute) and leadership (read-only follow-up). Per TRD §14 dumb-renderer
 * this composable NEVER checks `role ==`, never derives which animals are due, and
 * never decides whether an affordance shows — every visible label, status, and
 * action is a field on the state. A ViewModel/app layer (built later) fills the
 * state from the app-api; this feature module stays stateless.
 */

// ---------------------------------------------------------------------------
// State + events
// ---------------------------------------------------------------------------

/**
 * Per-shed status. The colour mapping is fixed by the design system, but WHICH status
 * a shed carries is backend-provided (never derived on device):
 *  - [DONE]    -> brand green
 *  - [PENDING] -> warn amber (in progress / not yet complete)
 *  - [DELAYED] -> error / RED (delayed · not started — leadership chases the team)
 */
enum class ShedStatus { DONE, PENDING, DELAYED }

/** One vaccine group in a shed's mix-and-match set. Backend-tagged; [full] dims the chip. */
data class VaccineGroup(
    val label: String,
    val countLabel: String,
    val full: Boolean = false,
)

/** Tone for a roster-change tag; the reason vocabulary itself is backend-provided. */
enum class ChangeTone { WARN, DANGER, INFO, OK }

/** A roster-change row (quarantine / death / shift / birth …) shown under the sheds. */
data class RosterChange(
    val tag: String,
    val tone: ChangeTone,
    val text: String,
)

/**
 * One shed card. Every visible label/status/count comes from the backend payload;
 * [actionLabel] is null when the backend returned no action for this principal
 * (e.g. leadership gets no Start/scan) — the absence of the field, not a client
 * `role ==` check, is what hides the affordance.
 */
data class ShedRow(
    val id: String,
    val name: String,
    val cohort: String,
    val status: ShedStatus,
    val statusLabel: String,
    val vaccineGroups: List<VaccineGroup>,
    val inShed: String,
    val due: String,
    val done: String,
    val progressLabel: String,
    val progressFraction: Float,
    val actionLabel: String? = null,
)

/** Full screen state. Header fields + the shed list + optional roster/kernel context. */
data class ShedsUiState(
    val moduleLabel: String,
    val scopeLabel: String,
    val title: String,
    val date: String,
    val window: String,
    val shedCountLabel: String,
    val dueLabel: String,
    val dayProgressLabel: String,
    val dayProgressFraction: Float,
    val daySummary: String,
    val caption: String? = null,
    val roleNote: String? = null,
    val rows: List<ShedRow> = emptyList(),
    val rosterChanges: List<RosterChange> = emptyList(),
    val kernelInfo: String? = null,
)

sealed interface ShedsEvent {
    data class OpenShedRecord(val shedId: String) : ShedsEvent
    data object Refresh : ShedsEvent
}

// ---------------------------------------------------------------------------
// Tokens (ported from docs/mobile/design-system.md — dark is the default field theme)
// ---------------------------------------------------------------------------

private val PageBg = Color(0xFF0A0F0C)
private val Surf = Color(0xFF131A15)
private val Surf2 = Color(0xFF1A241D)
private val Surf3 = Color(0xFF222E25)
private val Hair = Color(0xFF28352B)
private val Ink = Color(0xFFECF4EE)
private val Muted = Color(0xFF8FA497)
private val Faint = Color(0xFF5F7367)
private val Brand = Color(0xFF8AD457)
private val BrandD = Color(0xFFB7EA8C)
private val Warn = Color(0xFFF0B54B)
private val Danger = Color(0xFFFB6F63)
private val OkBg = Color(0x298AD457)
private val WarnBg = Color(0x26F0B54B)
private val DangerBg = Color(0x26FB6F63)
private val Info = Color(0xFF5B9BE8)
private val InfoBg = Color(0x295B9BE8)
private val BrandTint = Color(0x218AD457)
private val ProgressFill = Brush.horizontalGradient(listOf(Color(0xFF93DA5E), Color(0xFF5FB531)))

private data class StatusTone(val fg: Color, val bg: Color, val edge: Color)

/** delayed = RED (error) is the load-bearing rule from screens.md + the role-drill memory. */
private fun toneFor(status: ShedStatus): StatusTone = when (status) {
    ShedStatus.DONE -> StatusTone(fg = BrandD, bg = OkBg, edge = Brand)
    ShedStatus.PENDING -> StatusTone(fg = Warn, bg = WarnBg, edge = Warn)
    ShedStatus.DELAYED -> StatusTone(fg = Danger, bg = DangerBg, edge = Danger)
}

private fun changeTone(tone: ChangeTone): Pair<Color, Color> = when (tone) {
    ChangeTone.WARN -> Warn to WarnBg
    ChangeTone.DANGER -> Danger to DangerBg
    ChangeTone.INFO -> Info to InfoBg
    ChangeTone.OK -> BrandD to OkBg
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun ShedsScreen(
    state: ShedsUiState,
    onEvent: (ShedsEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(PageBg),
    ) {
        ShedsHeader(state = state, onRefresh = { onEvent(ShedsEvent.Refresh) })
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item { DriveMeta(state) }
            item { DayProgress(state) }
            state.roleNote?.let { note -> item { RoleNote(note) } }
            state.caption?.let { caption -> item { SectionCaption(caption) } }
            items(state.rows, key = { it.id }) { row ->
                ShedCard(row = row, onOpen = { onEvent(ShedsEvent.OpenShedRecord(row.id)) })
            }
            if (state.rosterChanges.isNotEmpty()) {
                item { SectionCaption("Roster changes · since this drive was scheduled") }
                item { ChangeCard(state.rosterChanges) }
            }
            state.kernelInfo?.let { info -> item { InfoBox(info) } }
        }
    }
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

@Composable
private fun ShedsHeader(state: ShedsUiState, onRefresh: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = "${state.moduleLabel} · ${state.scopeLabel}",
                color = BrandD,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Spacer(Modifier.height(2.dp))
            Text(
                text = state.title,
                color = Ink,
                fontSize = 22.sp,
                fontWeight = FontWeight.Bold,
            )
        }
        Box(
            modifier = Modifier
                .size(40.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(Surf2)
                .border(1.dp, Hair, RoundedCornerShape(12.dp))
                .clickable(onClick = onRefresh),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Refresh,
                contentDescription = "Refresh",
                tint = Muted,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

@Composable
private fun DriveMeta(state: ShedsUiState) {
    // Only render segments the backend actually populated — a blank field must not
    // leave an orphaned "·" separator (loading/empty/error states clear these).
    val parts = listOfNotNull(
        state.date.takeIf { it.isNotBlank() }?.let { it to true },
        state.window.takeIf { it.isNotBlank() }?.let { it to false },
        state.shedCountLabel.takeIf { it.isNotBlank() }?.let { it to true },
        state.dueLabel.takeIf { it.isNotBlank() }?.let { it to true },
    )
    if (parts.isEmpty()) return
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(Surf2)
            .padding(horizontal = 13.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        parts.forEachIndexed { index, (text, strong) ->
            if (index > 0) Dot()
            if (strong) MetaStrong(text) else MetaMuted(text)
        }
    }
}

@Composable
private fun MetaStrong(text: String) {
    Text(text = text, color = Ink, fontSize = 11.5f.sp, fontWeight = FontWeight.SemiBold)
}

@Composable
private fun MetaMuted(text: String) {
    Text(text = text, color = Muted, fontSize = 11.5f.sp, fontWeight = FontWeight.Medium)
}

@Composable
private fun Dot() {
    Text(text = "·", color = Faint, fontSize = 11.5f.sp)
}

@Composable
private fun DayProgress(state: ShedsUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(Surf)
            .border(1.dp, Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 15.dp, vertical = 13.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text = "Day progress", color = Ink, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.weight(1f))
            Text(text = state.dayProgressLabel, color = BrandD, fontSize = 14.sp, fontWeight = FontWeight.ExtraBold)
        }
        Spacer(Modifier.height(8.dp))
        ProgressBar(state.dayProgressFraction)
        Spacer(Modifier.height(7.dp))
        Text(text = state.daySummary, color = Muted, fontSize = 10.5f.sp)
    }
}

@Composable
private fun RoleNote(note: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(Surf2)
            .border(1.dp, Hair, RoundedCornerShape(12.dp))
            .padding(horizontal = 13.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = "●", color = Muted, fontSize = 8.sp)
        Text(text = note, color = Muted, fontSize = 11.5f.sp)
    }
}

@Composable
private fun SectionCaption(text: String) {
    Text(
        text = text,
        color = Faint,
        fontSize = 11.sp,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp),
    )
}

// ---------------------------------------------------------------------------
// Shed card
// ---------------------------------------------------------------------------

@Composable
private fun ShedCard(row: ShedRow, onOpen: () -> Unit) {
    val tone = toneFor(row.status)
    Card(
        onClick = onOpen,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.cardColors(containerColor = Surf),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        border = BorderStroke(1.dp, Hair),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            ShedCardTop(row = row, tone = tone)
            Spacer(Modifier.height(12.dp))
            VaccineChips(row.vaccineGroups)
            Spacer(Modifier.height(14.dp))
            NumsRow(row)
            Spacer(Modifier.height(12.dp))
            ProgressBar(row.progressFraction)
            row.actionLabel?.let { label ->
                Spacer(Modifier.height(12.dp))
                ActionFooter(label = label, status = row.status, onClick = onOpen)
            }
        }
    }
}

@Composable
private fun ShedCardTop(row: ShedRow, tone: StatusTone) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        // Left status edge marker (mock's coloured left border): delayed = red.
        Box(
            modifier = Modifier
                .width(3.dp)
                .height(42.dp)
                .clip(RoundedCornerShape(2.dp))
                .background(tone.edge),
        )
        Spacer(Modifier.width(11.dp))
        ShedAvatar()
        Spacer(Modifier.width(11.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(text = row.name, color = Ink, fontSize = 15.5f.sp, fontWeight = FontWeight.Bold, maxLines = 1)
            Text(text = row.cohort, color = Muted, fontSize = 12.sp, maxLines = 1)
        }
        Spacer(Modifier.width(8.dp))
        StatusPill(label = row.statusLabel, tone = tone)
    }
}

@Composable
private fun ShedAvatar() {
    Box(
        modifier = Modifier
            .size(42.dp)
            .clip(RoundedCornerShape(13.dp))
            .background(BrandTint),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.size(20.dp)) {
            val s = size.minDimension
            val roof = Path().apply {
                moveTo(s * 0.5f, s * 0.14f)
                lineTo(s * 0.9f, s * 0.46f)
                lineTo(s * 0.1f, s * 0.46f)
                close()
            }
            drawPath(path = roof, color = Brand)
            drawRoundRect(
                color = Brand,
                topLeft = Offset(s * 0.22f, s * 0.46f),
                size = Size(s * 0.56f, s * 0.4f),
                cornerRadius = CornerRadius(s * 0.06f),
            )
        }
    }
}

@Composable
private fun StatusPill(label: String, tone: StatusTone) {
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(tone.bg)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    ) {
        Text(text = label, color = tone.fg, fontSize = 11.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
    }
}

@Composable
private fun VaccineChips(groups: List<VaccineGroup>) {
    FlowRow(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        groups.forEach { VaccineChip(it) }
    }
}

@Composable
private fun VaccineChip(group: VaccineGroup) {
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(Surf2)
            .border(1.dp, Hair, RoundedCornerShape(9.dp))
            .padding(horizontal = 9.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Box(
            modifier = Modifier
                .size(5.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(if (group.full) Muted else Brand),
        )
        Text(
            text = group.label,
            color = if (group.full) Muted else Ink,
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
            maxLines = 1,
        )
        Text(
            text = group.countLabel,
            color = if (group.full) Muted else BrandD,
            fontSize = 11.sp,
            fontWeight = FontWeight.ExtraBold,
            maxLines = 1,
        )
    }
}

@Composable
private fun NumsRow(row: ShedRow) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(IntrinsicSize.Min)
            .clip(RoundedCornerShape(12.dp))
            .background(Surf2),
    ) {
        NumCell(value = row.inShed, label = "IN SHED", modifier = Modifier.weight(1f))
        NumDivider()
        NumCell(value = row.due, label = "DUE", modifier = Modifier.weight(1f))
        NumDivider()
        NumCell(value = row.done, label = "DONE", modifier = Modifier.weight(1f))
    }
}

@Composable
private fun NumCell(value: String, label: String, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.padding(vertical = 9.dp, horizontal = 4.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(text = value, color = Ink, fontSize = 16.sp, fontWeight = FontWeight.ExtraBold)
        Text(text = label, color = Muted, fontSize = 9.5f.sp, fontWeight = FontWeight.Bold)
    }
}

@Composable
private fun NumDivider() {
    Box(
        modifier = Modifier
            .fillMaxHeight()
            .width(1.dp)
            .background(Hair),
    )
}

@Composable
private fun ActionFooter(label: String, status: ShedStatus, onClick: () -> Unit) {
    // Colour follows the backend-provided status (delayed = red); the label itself
    // is whatever action the backend returned — the screen does not compose it.
    val color = if (status == ShedStatus.DELAYED) Danger else BrandD
    Text(
        text = label,
        color = color,
        fontSize = 12.5f.sp,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(8.dp))
            .clickable(onClick = onClick)
            .padding(vertical = 4.dp),
    )
}

// ---------------------------------------------------------------------------
// Roster changes + info box
// ---------------------------------------------------------------------------

@Composable
private fun ChangeCard(changes: List<RosterChange>) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(Surf)
            .border(1.dp, Hair, RoundedCornerShape(16.dp))
            .padding(horizontal = 14.dp),
    ) {
        changes.forEachIndexed { index, change ->
            ChangeRow(change)
            if (index < changes.lastIndex) {
                Box(Modifier.fillMaxWidth().height(1.dp).background(Surf2))
            }
        }
    }
}

@Composable
private fun ChangeRow(change: RosterChange) {
    val (fg, bg) = changeTone(change.tone)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 11.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(
            modifier = Modifier
                .clip(RoundedCornerShape(6.dp))
                .background(bg)
                .padding(horizontal = 8.dp, vertical = 3.dp),
        ) {
            Text(text = change.tag, color = fg, fontSize = 10.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
        }
        Text(text = change.text, color = Muted, fontSize = 12.5f.sp, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun InfoBox(text: String) {
    Text(
        text = text,
        color = Muted,
        fontSize = 12.sp,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(Surf)
            .border(1.dp, Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
    )
}

// ---------------------------------------------------------------------------
// Shared bits
// ---------------------------------------------------------------------------

@Composable
private fun ProgressBar(fraction: Float) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(7.dp)
            .clip(RoundedCornerShape(4.dp))
            .background(Surf3),
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth(fraction.coerceIn(0f, 1f))
                .height(7.dp)
                .clip(RoundedCornerShape(4.dp))
                .background(ProgressFill),
        )
    }
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

@Preview(name = "Drive status (leadership lens)", showBackground = true, backgroundColor = 0xFF0A0F0C)
@Composable
private fun ShedsScreenPreview() {
    GoatOsTheme {
        ShedsScreen(state = previewState())
    }
}

private fun previewState(): ShedsUiState = ShedsUiState(
    moduleLabel = "Vaccination",
    scopeLabel = "All parks · 2",
    title = "Drive status",
    date = "Tue 7 Jul 2026",
    window = "08:00–20:00",
    shedCountLabel = "4 sheds",
    dueLabel = "77 due",
    dayProgressLabel = "46 / 77",
    dayProgressFraction = 46f / 77f,
    daySummary = "Gandhi 1 · Castro 1 · Mandela 1 · Sumathi 1",
    caption = "Tap a shed for its live status · red = delayed, chase the team",
    roleNote = "Read-only · the ground team runs the drive",
    rows = listOf(
        ShedRow(
            id = "mandela1",
            name = "Mandela 1 · CBE",
            cohort = "K2 kids · 71 in shed",
            status = ShedStatus.DONE,
            statusLabel = "Done",
            vaccineGroups = listOf(VaccineGroup("FMD + HS", "40/40", full = true)),
            inShed = "71",
            due = "40",
            done = "40",
            progressLabel = "40/40 done",
            progressFraction = 1f,
            actionLabel = "View completed record ›",
        ),
        ShedRow(
            id = "castro1",
            name = "Castro 1 · CBE",
            cohort = "Breeding does · 44 in shed",
            status = ShedStatus.PENDING,
            statusLabel = "In progress",
            vaccineGroups = listOf(
                VaccineGroup("PPR · Booster", "6/12"),
                VaccineGroup("Goat Pox", "0/5"),
            ),
            inShed = "44",
            due = "17",
            done = "6",
            progressLabel = "6/17 done",
            progressFraction = 6f / 17f,
            actionLabel = "View live status ›",
        ),
        ShedRow(
            id = "sumathi1",
            name = "Sumathi 1 · CBE",
            cohort = "Pregnant does · 30 in shed",
            status = ShedStatus.DELAYED,
            statusLabel = "Delayed · chase team",
            vaccineGroups = listOf(VaccineGroup("ET + TT · Booster", "0/9")),
            inShed = "30",
            due = "9",
            done = "0",
            progressLabel = "0/9 done",
            progressFraction = 0f,
            actionLabel = "Not started — chase the team ›",
        ),
        ShedRow(
            id = "sumathi2",
            name = "Sumathi 2 · CPT",
            cohort = "Yearling does · 38 in shed",
            status = ShedStatus.DELAYED,
            statusLabel = "Delayed · chase team",
            vaccineGroups = listOf(VaccineGroup("PPR · Booster", "0/38")),
            inShed = "38",
            due = "38",
            done = "0",
            progressLabel = "0/38 done",
            progressFraction = 0f,
            actionLabel = "Not started — chase the team ›",
        ),
    ),
    rosterChanges = listOf(
        RosterChange("Quarantine", ChangeTone.WARN, "3 does moved to Q2 (ICU) — skipped today, re-checked on release"),
        RosterChange("Death", ChangeTone.DANGER, "2 died — all future doses auto-cancelled"),
        RosterChange("Shifted", ChangeTone.INFO, "1 moved to Yashoda 5 — now counted in that shed's drive"),
        RosterChange("Birth", ChangeTone.OK, "2 born in K0 — auto-scheduled from birth date after warm-up"),
    ),
    kernelInfo = "Eligible counts update live: the obligation engine reads birth / death / " +
        "shifting / quarantine events and reschedules or cancels doses automatically — no manual edit.",
)
