package sg.mesha.goatos.feature.leadership

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

// ─────────────────────────────────────────────────────────────────────────────
// Leadership feature — three stateless screens (Overview / Overdue / Reschedule).
//
// TRD §14 dumb-renderer: every visible label, %, count, status, action, and date
// option below is a FIELD on the UiState. The screen renders backend truth; it
// never does role== checks, never client-aggregates coverage, never classifies
// missed vs in-buffer, and never computes which dates are allowed. A later
// ViewModel fills these states from the mobile app-api scope_token reads.
// ─────────────────────────────────────────────────────────────────────────────

// region ── shared tokens (ported from mock/vaccination-mobile-mock.html, dark) ──

/** Dark-first palette from docs/mobile/design-system.md (the mock CSS vars). */
internal object LeadTokens {
    val brand = MeshaColors.Brand
    val brand2 = MeshaColors.Brand2
    val brandD = MeshaColors.BrandD
    val pageBg = MeshaColors.PageBg
    val surf = MeshaColors.Surf
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
    val heroInk = MeshaColors.OnBrand
}

/** Backend-provided severity tone; the app maps tone → colour, it does not judge. */
enum class Tone { OK, WARN, DANGER, NEUTRAL }

internal data class ToneColors(val fg: Color, val bg: Color)

internal fun Tone.colors(): ToneColors = when (this) {
    Tone.OK -> ToneColors(LeadTokens.ok, LeadTokens.okX)
    Tone.WARN -> ToneColors(LeadTokens.warn, LeadTokens.warnX)
    Tone.DANGER -> ToneColors(LeadTokens.danger, LeadTokens.dangerX)
    Tone.NEUTRAL -> ToneColors(LeadTokens.muted, LeadTokens.surf3)
}

// endregion

// region ── event contract (shared by all three leadership screens) ──

/** Every interaction the leadership screens can raise; hoisted to the app layer. */
sealed interface LeadershipEvent {
    data object Menu : LeadershipEvent
    data object Back : LeadershipEvent
    data object Refresh : LeadershipEvent
    data object OpenProfile : LeadershipEvent

    // Overview
    data object OpenScopePicker : LeadershipEvent
    data object OpenDataGaps : LeadershipEvent
    data class KpiTapped(val id: String) : LeadershipEvent
    data class ShedTapped(val shedId: String) : LeadershipEvent
    data class AssignShed(val shedId: String) : LeadershipEvent
    data class DecisionTapped(val id: String) : LeadershipEvent
    data class ParkTapped(val code: String) : LeadershipEvent

    // Overdue
    data class OverdueRowTapped(val id: String) : LeadershipEvent

    // Reschedule
    data class SegmentSelected(val id: String) : LeadershipEvent
    data class DateSelected(val id: String) : LeadershipEvent
    data object OpenAssignPicker : LeadershipEvent
    data object ConfirmReschedule : LeadershipEvent
}

// endregion

// region ── Overview (v-dhome) UiState ──

data class LeadershipUiState(
    val eyebrow: String,
    val title: String,
    val avatarInitial: String,
    val hero: CoverageHeroState,
    val kpis: List<KpiTile>,
    val todayShedsTitle: String,
    val todaySheds: List<ShedSummary>,
    val backlogTitle: String,
    val backlog: List<BacklogRow>,
    val needsDecisionTitle: String,
    val needsDecision: List<DecisionRow>,
    // Coverage-by-park is present only when the principal's grant set includes it
    // (backend decides via the payload — not a client role gate). null → hidden.
    val coverageByParkTitle: String? = null,
    val coverageByPark: List<ParkCoverageRow> = emptyList(),
)

data class CoverageHeroState(
    val coverageLabel: String,
    /** Backend-computed coverage %, rendered verbatim — never divided on device. */
    val coveragePercent: Int,
    val dosesLine: String,
    /** Backend doses-trend rollup points for the sparkline (already aggregated). */
    val dosesTrend: List<Float>,
    /** Non-null only when the backend grant set includes the park picker. */
    val scopePill: ScopePill? = null,
    val animalsLabel: String,
    val dataGapPill: DataGapPill,
)

data class ScopePill(val label: String)

data class DataGapPill(val label: String, val hasGaps: Boolean)

data class KpiTile(
    val id: String,
    val value: String,
    val label: String,
    val tone: Tone,
)

data class ShedSummary(
    val shedId: String,
    val name: String,
    val park: String,
    val cohort: String,
    val coveragePercent: Int,
    val done: Int,
    val total: Int,
    val vaccineGroups: List<VaccineGroupChip>,
    /** null → no assign affordance (assign grant absent from the payload). */
    val assignLabel: String? = null,
)

data class VaccineGroupChip(
    val vaccine: String,
    val done: Int,
    val due: Int,
)

data class BacklogRow(
    val vaccine: String,
    val note: String? = null,
    val count: String,
    val tone: Tone,
    val barPercent: Int,
)

data class DecisionRow(
    val id: String,
    val title: String,
    val subtitle: String,
    val actionLabel: String,
    val tone: Tone,
)

data class ParkCoverageRow(
    val code: String,
    val name: String,
    val subtitle: String,
    val coverageLabel: String,
    val tone: Tone,
)

// endregion

@Composable
fun LeadershipScreen(
    state: LeadershipUiState,
    onEvent: (LeadershipEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(LeadTokens.pageBg),
    ) {
        LeadTopBar(
            eyebrow = state.eyebrow,
            title = state.title,
            leading = TopBarLeading.MENU,
            avatarInitial = state.avatarInitial,
            onLeading = { onEvent(LeadershipEvent.Menu) },
            onRefresh = { onEvent(LeadershipEvent.Refresh) },
            onAvatar = { onEvent(LeadershipEvent.OpenProfile) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxWidth().weight(1f),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item { CoverageHero(state.hero, onEvent) }
            item { KpiRow(state.kpis, onEvent) }

            item { SectionLabel(state.todayShedsTitle) }
            items(state.todaySheds, key = { it.shedId }) { shed ->
                ShedSummaryRow(shed, onEvent)
            }

            item { SectionLabel(state.backlogTitle) }
            items(state.backlog, key = { it.vaccine }) { row -> BacklogRowView(row) }

            item { SectionLabel(state.needsDecisionTitle) }
            items(state.needsDecision, key = { it.id }) { row ->
                DecisionRowView(row, onEvent)
            }

            if (state.coverageByParkTitle != null && state.coverageByPark.isNotEmpty()) {
                item { SectionLabel(state.coverageByParkTitle) }
                items(state.coverageByPark, key = { it.code }) { park ->
                    ParkCoverageRowView(park, onEvent)
                }
            }
        }
    }
}

// region ── Overview sections ──

@Composable
private fun CoverageHero(hero: CoverageHeroState, onEvent: (LeadershipEvent) -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.BrandGradient)
            .padding(16.dp),
    ) {
        Text(
            hero.coverageLabel,
            color = LeadTokens.heroInk.copy(alpha = 0.75f),
            fontSize = 12.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            "${hero.coveragePercent}%",
            color = LeadTokens.heroInk,
            fontSize = 40.sp,
            fontWeight = FontWeight.ExtraBold,
        )
        Text(
            hero.dosesLine,
            color = LeadTokens.heroInk.copy(alpha = 0.8f),
            fontSize = 12.5.sp,
            fontWeight = FontWeight.Medium,
        )
        Spacer(Modifier.height(8.dp))
        DosesSparkline(
            points = hero.dosesTrend,
            line = LeadTokens.heroInk,
            modifier = Modifier.fillMaxWidth().height(34.dp),
        )
        Spacer(Modifier.height(12.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            hero.scopePill?.let { pill ->
                HeroPill(pill.label, trailingChevron = true) {
                    onEvent(LeadershipEvent.OpenScopePicker)
                }
            }
            HeroPill(hero.animalsLabel)
            HeroPill(hero.dataGapPill.label, trailingChevron = true) {
                onEvent(LeadershipEvent.OpenDataGaps)
            }
        }
    }
}

@Composable
private fun HeroPill(
    label: String,
    trailingChevron: Boolean = false,
    onClick: (() -> Unit)? = null,
) {
    Row(
        Modifier
            .clip(CircleShape)
            .background(MeshaColors.Overlay)
            .then(if (onClick != null) Modifier.clickable { onClick() } else Modifier)
            .padding(horizontal = 11.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(label, color = LeadTokens.heroInk, fontSize = 11.5.sp, fontWeight = FontWeight.Bold)
        if (trailingChevron) {
            Icon(imageVector = MeshaIcons.Chevron, contentDescription = null, tint = LeadTokens.heroInk, modifier = Modifier.size(13.dp))
        }
    }
}

@Composable
private fun DosesSparkline(points: List<Float>, line: Color, modifier: Modifier) {
    if (points.size < 2) {
        Box(modifier)
        return
    }
    Canvas(modifier) {
        val maxV = points.max()
        val minV = points.min()
        val range = (maxV - minV).takeIf { it > 0f } ?: 1f
        val stepX = if (points.size > 1) size.width / (points.size - 1) else size.width
        val path = Path()
        points.forEachIndexed { i, v ->
            val x = stepX * i
            val y = size.height - ((v - minV) / range) * size.height
            if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
        }
        drawPath(
            path = path,
            color = line.copy(alpha = 0.85f),
            style = Stroke(width = 2.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round),
        )
        val lastY = size.height - ((points.last() - minV) / range) * size.height
        drawCircle(color = line, radius = 3.dp.toPx(), center = Offset(size.width, lastY))
    }
}

@Composable
private fun KpiRow(kpis: List<KpiTile>, onEvent: (LeadershipEvent) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        kpis.forEach { kpi ->
            val tone = kpi.tone.colors()
            Column(
                Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(18.dp))
                    .background(LeadTokens.surf)
                    .border(1.dp, LeadTokens.hair, RoundedCornerShape(18.dp))
                    .clickable { onEvent(LeadershipEvent.KpiTapped(kpi.id)) }
                    .padding(15.dp),
            ) {
                Text(kpi.value, color = tone.fg, fontSize = 26.sp, fontWeight = FontWeight.ExtraBold)
                Text(kpi.label, color = LeadTokens.muted, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}

@Composable
private fun ShedSummaryRow(shed: ShedSummary, onEvent: (LeadershipEvent) -> Unit) {
    Card(onClick = { onEvent(LeadershipEvent.ShedTapped(shed.shedId)) }) {
        Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            MiniRing(shed.coveragePercent, shed.done)
            Column(Modifier.weight(1f)) {
                Text(
                    "${shed.name} · ${shed.park}",
                    color = LeadTokens.ink,
                    fontSize = 13.5.sp,
                    fontWeight = FontWeight.Bold,
                )
                Text(shed.cohort, color = LeadTokens.muted, fontSize = 12.sp)
                Spacer(Modifier.height(8.dp))
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    shed.vaccineGroups.forEach { g ->
                        VaccineChip(g)
                    }
                }
                if (shed.assignLabel != null) {
                    Spacer(Modifier.height(10.dp))
                    Row(
                        Modifier
                            .clip(CircleShape)
                            .border(1.dp, LeadTokens.hair, CircleShape)
                            .clickable { onEvent(LeadershipEvent.AssignShed(shed.shedId)) }
                            .padding(horizontal = 12.dp, vertical = 7.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        Icon(imageVector = MeshaIcons.Plus, contentDescription = null, tint = LeadTokens.brandD, modifier = Modifier.size(13.dp))
                        Text(shed.assignLabel, color = LeadTokens.brandD, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                    }
                }
            }
            Text(
                "${shed.done}/${shed.total}",
                color = if (shed.done >= shed.total) LeadTokens.brandD else LeadTokens.muted,
                fontSize = 13.sp,
                fontWeight = FontWeight.Bold,
            )
        }
    }
}

@Composable
private fun VaccineChip(g: VaccineGroupChip) {
    val full = g.done >= g.due
    Row(
        Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(if (full) LeadTokens.surf2 else LeadTokens.surf3)
            .padding(horizontal = 9.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(imageVector = MeshaIcons.Syringe, contentDescription = null, tint = LeadTokens.brandD, modifier = Modifier.size(12.dp))
        Text(
            g.vaccine,
            color = if (full) LeadTokens.muted else LeadTokens.ink,
            fontSize = 11.5.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            "${g.done}/${g.due}",
            color = LeadTokens.muted,
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
private fun BacklogRowView(row: BacklogRow) {
    Card {
        Column {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(row.vaccine, color = LeadTokens.ink, fontSize = 13.5.sp, fontWeight = FontWeight.Bold)
                if (row.note != null) {
                    Spacer(Modifier.width(6.dp))
                    Text(row.note, color = LeadTokens.muted, fontSize = 11.5.sp)
                }
                Spacer(Modifier.weight(1f))
                StatusPill(row.count, row.tone)
            }
            Spacer(Modifier.height(8.dp))
            ProgressBar(row.barPercent, row.tone)
        }
    }
}

@Composable
private fun DecisionRowView(row: DecisionRow, onEvent: (LeadershipEvent) -> Unit) {
    val tone = row.tone.colors()
    Card(
        onClick = { onEvent(LeadershipEvent.DecisionTapped(row.id)) },
        borderColor = tone.fg.copy(alpha = 0.3f),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            GlyphBadge(MeshaIcons.Warn, tone)
            Column(Modifier.weight(1f)) {
                Text(row.title, color = LeadTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.Bold)
                Text(row.subtitle, color = LeadTokens.muted, fontSize = 12.sp)
            }
            StatusPill(row.actionLabel, row.tone)
        }
    }
}

@Composable
private fun ParkCoverageRowView(park: ParkCoverageRow, onEvent: (LeadershipEvent) -> Unit) {
    val tone = park.tone.colors()
    Card(onClick = { onEvent(LeadershipEvent.ParkTapped(park.code)) }) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            GlyphBadge(MeshaIcons.Home, tone)
            Column(Modifier.weight(1f)) {
                Text("${park.code} · ${park.name}", color = LeadTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.Bold)
                Text(park.subtitle, color = LeadTokens.muted, fontSize = 12.sp)
            }
            Text(park.coverageLabel, color = tone.fg, fontSize = 14.sp, fontWeight = FontWeight.ExtraBold)
        }
    }
}

// endregion

// region ── shared composables (used by all three screens) ──

internal enum class TopBarLeading { MENU, BACK }

@Composable
internal fun LeadTopBar(
    eyebrow: String,
    title: String,
    leading: TopBarLeading,
    avatarInitial: String? = null,
    onLeading: () -> Unit = {},
    onRefresh: (() -> Unit)? = null,
    onAvatar: (() -> Unit)? = null,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .background(LeadTokens.pageBg)
            .padding(horizontal = 12.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        IconButton(if (leading == TopBarLeading.MENU) MeshaIcons.Menu else MeshaIcons.ChevronLeft, onClick = onLeading)
        Column(Modifier.weight(1f)) {
            Text(eyebrow, color = LeadTokens.brandD, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
            Text(title, color = LeadTokens.ink, fontSize = 20.sp, fontWeight = FontWeight.Bold)
        }
        if (onRefresh != null) {
            IconButton(MeshaIcons.Refresh, onClick = onRefresh)
        }
        if (avatarInitial != null) {
            Box(
                Modifier
                    .size(36.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(LeadTokens.surf3)
                    .then(if (onAvatar != null) Modifier.clickable { onAvatar() } else Modifier),
                contentAlignment = Alignment.Center,
            ) {
                Text(avatarInitial, color = LeadTokens.brandD, fontSize = 14.sp, fontWeight = FontWeight.Bold)
            }
        }
    }
}

@Composable
internal fun IconButton(icon: ImageVector, onClick: () -> Unit) {
    Box(
        Modifier
            .size(40.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(LeadTokens.surf)
            .border(1.dp, LeadTokens.hair, RoundedCornerShape(11.dp))
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        Icon(imageVector = icon, contentDescription = null, tint = LeadTokens.ink, modifier = Modifier.size(20.dp))
    }
}

@Composable
internal fun SectionLabel(text: String) {
    Text(
        text,
        color = LeadTokens.muted,
        fontSize = 12.sp,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
    )
}

@Composable
internal fun Card(
    onClick: (() -> Unit)? = null,
    borderColor: Color = LeadTokens.hair,
    content: @Composable () -> Unit,
) {
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(LeadTokens.surf)
            .border(1.dp, borderColor, RoundedCornerShape(18.dp))
            .then(if (onClick != null) Modifier.clickable { onClick() } else Modifier)
            .padding(15.dp),
    ) {
        content()
    }
}

@Composable
internal fun StatusPill(text: String, tone: Tone) {
    val c = tone.colors()
    Text(
        text,
        color = c.fg,
        fontSize = 11.5.sp,
        fontWeight = FontWeight.Bold,
        modifier = Modifier
            .clip(CircleShape)
            .background(c.bg)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

@Composable
internal fun GlyphBadge(icon: ImageVector, tone: ToneColors) {
    Box(
        Modifier
            .size(38.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(tone.bg),
        contentAlignment = Alignment.Center,
    ) {
        Icon(imageVector = icon, contentDescription = null, tint = tone.fg, modifier = Modifier.size(18.dp))
    }
}

@Composable
internal fun ProgressBar(percent: Int, tone: Tone) {
    val c = tone.colors()
    Box(
        Modifier
            .fillMaxWidth()
            .height(6.dp)
            .clip(CircleShape)
            .background(LeadTokens.surf3),
    ) {
        Box(
            Modifier
                .fillMaxHeight()
                .fillMaxWidth(percent.coerceIn(0, 100) / 100f)
                .clip(CircleShape)
                .background(c.fg),
        )
    }
}

@Composable
internal fun MiniRing(percent: Int, done: Int) {
    val fg = if (done > 0) LeadTokens.brand else LeadTokens.muted
    Box(Modifier.size(38.dp), contentAlignment = Alignment.Center) {
        Canvas(Modifier.size(38.dp)) {
            val stroke = 4.dp.toPx()
            val d = size.minDimension - stroke
            val topLeft = Offset(stroke / 2, stroke / 2)
            drawArc(
                color = LeadTokens.surf3,
                startAngle = -90f,
                sweepAngle = 360f,
                useCenter = false,
                topLeft = topLeft,
                size = Size(d, d),
                style = Stroke(stroke),
            )
            if (percent > 0) {
                drawArc(
                    color = fg,
                    startAngle = -90f,
                    sweepAngle = 360f * (percent.coerceIn(0, 100) / 100f),
                    useCenter = false,
                    topLeft = topLeft,
                    size = Size(d, d),
                    style = Stroke(stroke, cap = StrokeCap.Round),
                )
            }
        }
        Text("$percent%", color = fg, fontSize = 10.sp, fontWeight = FontWeight.Bold)
    }
}

/** Backend-provided explainer / disabled-reason copy container (no baked wording). */
@Composable
internal fun InfoBox(text: String, accent: Color = LeadTokens.hair) {
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(LeadTokens.surf2)
            .border(1.dp, accent, RoundedCornerShape(14.dp))
            .padding(13.dp),
    ) {
        Text(text, color = LeadTokens.muted, fontSize = 12.sp, lineHeight = 17.sp)
    }
}

// endregion

// region ── Preview ──

internal fun sampleLeadershipState() = LeadershipUiState(
    eyebrow = "Vaccination · Director",
    title = "Overview",
    avatarInitial = "A",
    hero = CoverageHeroState(
        coverageLabel = "Dose coverage · all vaccines",
        coveragePercent = 48,
        dosesLine = "4,650 of 9,698 scheduled doses given",
        dosesTrend = listOf(12f, 18f, 15f, 24f, 30f, 28f, 39f, 46f, 48f),
        scopePill = ScopePill("All parks · 2"),
        animalsLabel = "1,312 animals",
        dataGapPill = DataGapPill("no data gaps", hasGaps = false),
    ),
    kpis = listOf(
        KpiTile("given", "4,650", "Doses given · tap", Tone.OK),
        KpiTile("pending", "5,048", "Pending · tap", Tone.WARN),
    ),
    todayShedsTitle = "Today's sheds · live",
    todaySheds = listOf(
        ShedSummary(
            shedId = "castro1", name = "Castro 1", park = "CBE", cohort = "Breeding does",
            coveragePercent = 35, done = 6, total = 17,
            vaccineGroups = listOf(
                VaccineGroupChip("PPR · Booster", 6, 12),
                VaccineGroupChip("Goat Pox", 0, 5),
            ),
            assignLabel = "Assign team",
        ),
        ShedSummary(
            shedId = "mandela1", name = "Mandela 1", park = "CBE", cohort = "K2 kids",
            coveragePercent = 100, done = 40, total = 40,
            vaccineGroups = listOf(VaccineGroupChip("FMD + HS", 40, 40)),
            assignLabel = "Assign team",
        ),
    ),
    backlogTitle = "Backlog by vaccine · pending doses",
    backlog = listOf(
        BacklogRow("PPR", "largest backlog", "989", Tone.DANGER, 25),
        BacklogRow("FMD", "first + booster", "1,406", Tone.DANGER, 46),
        BacklogRow("ET + TT", "first + booster", "726", Tone.WARN, 72),
        BacklogRow("HS", null, "542", Tone.WARN, 59),
    ),
    needsDecisionTitle = "Needs a decision",
    needsDecision = listOf(
        DecisionRow("d1", "Goat Pox · overdue", "Yashoda 5 · 5 days late", "Reschedule", Tone.DANGER),
    ),
    coverageByParkTitle = "Coverage by park",
    coverageByPark = listOf(
        ParkCoverageRow("CBE", "Coimbatore", "716 animals · dose coverage", "52%", Tone.OK),
        ParkCoverageRow("CPT", "Channapatna", "596 animals · dose coverage", "42%", Tone.WARN),
    ),
)

@Preview(name = "Leadership Overview", widthDp = 380, heightDp = 1300, backgroundColor = 0xFF0A0F0C, showBackground = true)
@Composable
private fun LeadershipScreenPreview() {
    GoatOsTheme {
        LeadershipScreen(state = sampleLeadershipState())
    }
}

// endregion
