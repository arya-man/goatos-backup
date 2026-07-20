package sg.mesha.goatos.feature.leadership

// telemetry:exempt pure stateless renderer; LeadershipViewModel owns refresh/closure side effects
// and AppNavHost owns navigation intents.

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
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import sg.mesha.goatos.feature.leadership.R

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
    data class CloseVerificationDrive(val submissionId: String) : LeadershipEvent
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

// @Immutable: several List<T> fields below otherwise mark this unstable, disabling
// recomposition skipping for LeadershipScreen (item 6, perf/stability pass).
@Immutable
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
    val verificationClosures: List<VerificationClosureRow> = emptyList(),
    // Coverage-by-park is present only when the principal's grant set includes it
    // (backend decides via the payload — not a client role gate). null → hidden.
    val coverageByParkTitle: String? = null,
    val coverageByPark: List<ParkCoverageRow> = emptyList(),
    // Offline-first cache sync state
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

// @Immutable: dosesTrend: List<Float> otherwise marks this unstable (item 6, perf/stability pass).
@Immutable
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

data class DataGapPill(
    val label: String,
    val hasGaps: Boolean,
    /** Backend-computed open-gap count, rendered into the localized "%1$d open gaps"
     *  chrome string; the app never counts gaps client-side. Only meaningful when
     *  [hasGaps] is true — defaults to 0 for callers that don't populate it. */
    val openGapsCount: Int = 0,
)

data class KpiTile(
    val id: String,
    val value: String,
    val label: String,
    val tone: Tone,
)

// @Immutable: vaccineGroups: List<VaccineGroupChip> otherwise marks this unstable — ShedSummary
// is passed directly into ShedSummaryRow(shed: ShedSummary, ...) (item 6, perf/stability pass).
@Immutable
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

data class VerificationClosureRow(
    val submissionId: String,
    val title: String,
    val subtitle: String,
    val isQueueing: Boolean = false,
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
            // Static screen title — localized client-side (the VM value is fixed
            // English chrome; the visible title must follow the app locale).
            title = stringResource(R.string.overview_title),
            // No leading argument: `/leadership` is a backend nav_item, so the shared header
            // renders the module drawer here and this screen never asks about it.
            onRefresh = { onEvent(LeadershipEvent.Refresh) },
            refreshContentDescription = stringResource(R.string.overview_refresh_content_description),
        )
        SyncStatusIndicator(
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.kpis.isNotEmpty() || state.todaySheds.isNotEmpty(),
            isOffline = state.isOffline,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
        )
        val hasOverviewData = state.kpis.isNotEmpty() || state.todaySheds.isNotEmpty() ||
            state.backlog.isNotEmpty() || state.needsDecision.isNotEmpty() ||
            state.verificationClosures.isNotEmpty() ||
            (state.coverageByParkTitle != null && state.coverageByPark.isNotEmpty())

        if (!hasOverviewData && state.isRefreshing && state.lastSyncedAt == null) {
            LoadingSkeletonList(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
                rows = 4,
            )
        } else if (!hasOverviewData) {
            EmptyState(
                title = stringResource(R.string.overview_empty_title),
                icon = MeshaIcons.Check,
                tone = EmptyTone.Neutral,
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
            )
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxWidth().weight(1f),
                contentPadding = PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                item { CoverageHero(state.hero, onEvent) }
                item { KpiRow(state.kpis, onEvent) }

                // Section headers are fixed chrome — localize by key, not the VM's English title.
                item { SectionLabel(stringResource(R.string.overview_today_sheds_title)) }
                items(state.todaySheds, key = { it.shedId }) { shed ->
                    ShedSummaryRow(shed, onEvent)
                }

                item { SectionLabel(stringResource(R.string.overview_backlog_title)) }
                items(state.backlog, key = { it.vaccine }) { row -> BacklogRowView(row) }

                item { SectionLabel(stringResource(R.string.overview_needs_decision_title)) }
                if (state.verificationClosures.isNotEmpty()) {
                    item { SectionLabel(stringResource(R.string.overview_verified_closure_title)) }
                    items(state.verificationClosures, key = { it.submissionId }) { row ->
                        VerificationClosureRowView(row, onEvent)
                    }
                }
                items(state.needsDecision, key = { it.id }) { row ->
                    DecisionRowView(row, onEvent)
                }

                // coverageByParkTitle's presence (non-null) is still the backend's grant signal
                // for whether this section renders at all — only the displayed text is localized.
                if (state.coverageByParkTitle != null && state.coverageByPark.isNotEmpty()) {
                    item { SectionLabel(stringResource(R.string.overview_coverage_by_park_title)) }
                    items(state.coverageByPark, key = { it.code }) { park ->
                        ParkCoverageRowView(park, onEvent)
                    }
                }
            }
        }
    }
}

@Composable
private fun VerificationClosureRowView(
    row: VerificationClosureRow,
    onEvent: (LeadershipEvent) -> Unit,
) {
    Card(
        onClick = {
            if (!row.isQueueing) {
                onEvent(LeadershipEvent.CloseVerificationDrive(row.submissionId))
            }
        },
        borderColor = LeadTokens.ok.copy(alpha = 0.3f),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            GlyphBadge(MeshaIcons.Check, Tone.OK.colors())
            Column(Modifier.weight(1f)) {
                Text(row.title, color = LeadTokens.ink, fontSize = 13.sp, fontWeight = FontWeight.Bold)
                Text(row.subtitle, color = LeadTokens.muted, fontSize = 12.sp)
            }
            StatusPill(
                stringResource(
                    if (row.isQueueing) R.string.overview_close_queueing else R.string.overview_close_action,
                ),
                Tone.OK,
            )
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
            // Static hero label — localized client-side (the VM always bakes the English
            // "Process integrity" chrome string; ignore it and render the screen's own copy).
            stringResource(R.string.overview_coverage_label),
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
        if (hero.dosesLine.isNotBlank()) {
            Text(
                hero.dosesLine,
                color = LeadTokens.heroInk.copy(alpha = 0.8f),
                fontSize = 12.5.sp,
                fontWeight = FontWeight.Medium,
            )
        }
        if (hero.dosesTrend.isNotEmpty()) {
            Spacer(Modifier.height(8.dp))
            DosesSparkline(
                points = hero.dosesTrend,
                line = LeadTokens.heroInk,
                modifier = Modifier.fillMaxWidth().height(34.dp),
            )
            Spacer(Modifier.height(12.dp))
        }
        // Only render pills row if at least one pill has a non-blank label
        val hasScopePill = hero.scopePill?.label?.isNotBlank() == true
        val hasAnimalsLabel = hero.animalsLabel.isNotBlank()
        val hasDataGapLabel = hero.dataGapPill.label.isNotBlank()

        if (hasScopePill || hasAnimalsLabel || hasDataGapLabel) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (hasScopePill) {
                    hero.scopePill?.let { pill ->
                        HeroPill(pill.label, trailingChevron = true) {
                            onEvent(LeadershipEvent.OpenScopePicker)
                        }
                    }
                }
                if (hasAnimalsLabel) {
                    HeroPill(hero.animalsLabel)
                }
                if (hasDataGapLabel) {
                    // The pill's presence is still gated on the VM's label (backend signal
                    // that a data-gap pill should show at all); the displayed TEXT is fixed
                    // chrome localized client-side, with the backend's count substituted in.
                    val gapLabel = if (hero.dataGapPill.hasGaps) {
                        stringResource(R.string.overview_open_gaps_label, hero.dataGapPill.openGapsCount)
                    } else {
                        stringResource(R.string.overview_data_gaps_label)
                    }
                    HeroPill(gapLabel, trailingChevron = true) {
                        onEvent(LeadershipEvent.OpenDataGaps)
                    }
                }
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
                Text(kpiLabel(kpi.id, kpi.label), color = LeadTokens.muted, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}

/**
 * Localized label for a KPI tile, keyed by its backend id (fixed UI chrome). Falls back to
 * the VM-supplied [fallback] label for any id the screen doesn't recognize yet, so a new
 * backend-added tile still renders something rather than going blank.
 */
@Composable
private fun kpiLabel(id: String, fallback: String): String = when (id) {
    "critical" -> stringResource(R.string.overview_kpi_critical_label)
    "warnings" -> stringResource(R.string.overview_kpi_warnings_label)
    "open_gaps" -> stringResource(R.string.overview_kpi_open_gaps_label)
    "given" -> stringResource(R.string.overview_kpi_doses_given_label)
    "pending" -> stringResource(R.string.overview_kpi_pending_label)
    else -> fallback
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
                        // shed.assignLabel != null is still the backend's presence signal for
                        // this affordance (assign grant); the button's TEXT is fixed chrome.
                        Text(
                            stringResource(R.string.overview_assign_team_label),
                            color = LeadTokens.brandD,
                            fontSize = 12.sp,
                            fontWeight = FontWeight.SemiBold,
                        )
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

/**
 * Leadership top bar for all three screens in this module.
 *
 * There is deliberately no `leading` parameter any more. It used to be a `TopBarLeading.MENU |
 * BACK` choice each screen made for itself, which is a decision a screen cannot actually get
 * right: the same composable can be hosted at an L0 root and at a drill, and a new screen that
 * picked MENU would render an inert hamburger while one that picked BACK would hide the drawer on
 * a root. [MeshaScreenHeader] resolves it from the shell's exact L0 membership instead — Overview
 * (`/leadership`, a backend nav_item) gets the drawer; Overdue and Reschedule, pushed as hosted
 * children, get Up. Both just pass [onBack].
 */
@Composable
internal fun LeadTopBar(
    eyebrow: String,
    title: String,
    onBack: (() -> Unit)? = null,
    onRefresh: (() -> Unit)? = null,
    refreshContentDescription: String? = null,
) {
    MeshaScreenHeader(
        title = title,
        eyebrow = eyebrow,
        eyebrowColor = LeadTokens.brandD,
        onBack = onBack,
        modifier = Modifier.background(LeadTokens.pageBg),
        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp),
        actions = {
            if (onRefresh != null) {
                IconButton(icon = MeshaIcons.Refresh, contentDescription = refreshContentDescription, onClick = onRefresh)
            }
        },
    )
}

@Composable
internal fun IconButton(icon: ImageVector, contentDescription: String? = null, onClick: () -> Unit) {
    Box(
        Modifier
            .size(48.dp)
            .clip(RoundedCornerShape(11.dp))
            .background(LeadTokens.surf)
            .border(1.dp, LeadTokens.hair, RoundedCornerShape(11.dp))
            .clickable { onClick() },
        contentAlignment = Alignment.Center,
    ) {
        Icon(imageVector = icon, contentDescription = contentDescription, tint = LeadTokens.ink, modifier = Modifier.size(20.dp))
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
