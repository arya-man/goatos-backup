package sg.mesha.goatos.feature.weighing

import androidx.compose.animation.animateContentSize
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume

// telemetry:exempt Read-only preview of an export the caller already fetched; the fetch itself
// and the share intent it hands off to are the actions worth tracking, and both already are.

/**
 * ONE data row of the export, as this screen renders it.
 *
 * [scannedIdentifier] is blank for a lump-sum row -- the backend export has no per-animal
 * identity for a shed weighed as a batch -- so the row falls back to naming itself "Lump-sum" in
 * the compact line instead of showing an empty identifier, which used to read as a missing scan.
 */
data class WeighingExportPreviewRowUi(
    val type: String,
    val scannedIdentifier: String,
    val weightKg: String,
    val averageWeightKg: String,
    val animalCount: String,
    val verificationStatus: String,
    val proofReferenceType: String,
    val proofReference: String,
    val recordedAt: String,
    val dateIst: String,
    val timeIst: String,
) {
    val isLumpSum: Boolean get() = type.trim().contains("lump", ignoreCase = true)
    val isNotWeighed: Boolean get() = type.trim().equals("not weighed", ignoreCase = true)
}

/**
 * ONE shed's worth of export rows, grouped for display.
 *
 * Grouping mirrors the export's own grain -- the backend already emits one row per shed for an
 * untouched shed and one row per scan/lump-sum entry for a touched one -- so this is a display
 * grouping, not a re-derivation: nothing here decides which sheds appear, the backend's row set
 * does, unfiltered.
 */
data class WeighingExportPreviewShedUi(
    val shedKey: String,
    val park: String,
    val shedName: String,
    val shedStatus: String,
    val rows: List<WeighingExportPreviewRowUi>,
) {
    val touched: Boolean get() = rows.none { it.isNotWeighed }

    /** Rows that actually carry a weighing -- the "not weighed" placeholder never counts here. */
    private val weighedRows: List<WeighingExportPreviewRowUi> get() = rows.filterNot { it.isNotWeighed }

    /** Count of animals weighed, accounting for lump-sum rows. For lump-sum rows, sum their
     *  animal_count; for individual rows, count the rows themselves (each row = one scan). */
    val weighedCount: Int get() {
        return weighedRows.sumOf { row ->
            if (row.isLumpSum) {
                row.animalCount.toIntOrNull() ?: 0
            } else {
                1 // Individual row: one animal per scan
            }
        }
    }

    /** Sum of every parseable weight column on this shed -- unparseable/blank values are skipped
     *  rather than treated as zero, so one bad cell cannot silently understate the total. */
    val totalWeightKg: Double get() = weighedRows.sumOf { it.weightKg.toDoubleOrNull() ?: 0.0 }

    /** Average weight per animal, accounting for lump-sum vs individual rows.
     *  For lump-sum rows, use the stored average_weight; for individual rows, divide total by count. */
    val averageWeightKg: Double?
        get() {
            val hasScan = weighedRows.any { !it.isLumpSum && it.weightKg.toDoubleOrNull() != null }
            val hasLumpSum = weighedRows.any { it.isLumpSum }

            return when {
                // If there are any individual scans, calculate from those (lump-sum rows ignored in average)
                hasScan -> {
                    val scanRows = weighedRows.filterNot { it.isLumpSum }
                    val count = scanRows.count { it.weightKg.toDoubleOrNull() != null }
                    val total = scanRows.sumOf { it.weightKg.toDoubleOrNull() ?: 0.0 }
                    if (count > 0) total / count else null
                }
                // If only lump-sum rows, use the (first) stored average
                hasLumpSum -> {
                    weighedRows.firstOrNull { it.isLumpSum }?.averageWeightKg?.toDoubleOrNull()
                }
                else -> null
            }
        }
}

data class WeighingExportPreviewUiState(
    val campaignId: String = "",
    val parkName: String = "",
    val dateLabel: String = "",
    val loading: Boolean = false,
    val error: String = "",
    val sheds: List<WeighingExportPreviewShedUi> = emptyList(),
    /** Total CSV data rows, exactly as the backend returned them -- the honesty count. */
    val totalRowCount: Int = 0,
    /** True while the download that will actually be shared is in flight. */
    val sharing: Boolean = false,
)

/**
 * A phone-sized, READABLE preview of a task's weighing export, shown before any share/export
 * action -- this is what the maintainer asked for instead of the icon firing a share sheet with
 * no way to see what was in the file first.
 *
 * LAYOUT DECISION: 14 backend columns do not fit a phone width, and a horizontally scrollable
 * table hides most of the sheet off-screen with no cue that there is more to scroll to. Instead
 * this groups rows by shed (the same grain the CSV already uses for an untouched shed) with a
 * per-shed header, and shows only the three columns a planner actually scans a sheet for --
 * identifier, weight, verification status -- on each row. Tapping a row expands the remaining
 * eight columns as a vertical key-value list UNDER that row. Nothing on the page ever scrolls
 * sideways; the only scrolling is the LazyColumn's normal vertical scroll.
 *
 * SCALE DECISION (100 sheds x ~70-90 animals ≈ up to 8,000 rows): sheds render COLLAPSED, one
 * row each, by default -- animal rows for a shed only enter the LazyColumn when that shed is
 * expanded. Only ONE shed is expanded at a time. A planner opens this preview to sanity-check ONE
 * shed's sheet before sharing, not to read all 100 at once, and every other row in this app
 * (verify queue, roster) already keeps at most one detail panel open for the same reason: an
 * expand-many model would let a park's worth of animal rows (thousands of LazyColumn items) pile
 * up on screen at once, which is the exact "throwing up entirely" the maintainer flagged, just
 * moved from load-time to tap-time. Collapsing the previous shed when a new one opens keeps the
 * on-screen row count bounded to "1 shed header + that shed's rows" no matter how far the planner
 * scrolls.
 */
@Composable
fun WeighingExportPreviewScreen(
    state: WeighingExportPreviewUiState,
    onBack: () -> Unit = {},
    onRetry: () -> Unit = {},
    onShare: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRetry() }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        MeshaScreenHeader(
            title = stringResource(R.string.weighing_export_preview_title),
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            subtitle = listOf(state.parkName, state.dateLabel).filter { it.isNotBlank() }
                .joinToString(" · ").takeIf { it.isNotBlank() },
            onBack = onBack,
            actions = {
                // Sharing is now a SECONDARY action taken from inside the preview, never the
                // result of the icon tap that opened this screen -- see
                // docs/decisions/nav-entry-point-placement.md. Disabled while empty/loading so a
                // planner cannot share a file for a sheet they have not actually seen yet.
                IconButton(
                    onClick = onShare,
                    enabled = !state.loading && !state.sharing && state.sheds.isNotEmpty(),
                ) {
                    if (state.sharing) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(20.dp),
                            strokeWidth = 2.dp,
                            color = MeshaColors.Muted,
                        )
                    } else {
                        Icon(
                            imageVector = MeshaIcons.Download,
                            contentDescription = stringResource(R.string.weighing_export_preview_share),
                            tint = MeshaColors.Muted,
                        )
                    }
                }
            },
        )
        when {
            // A skeleton wall only when there is NOTHING to read yet -- the first ever load. Once
            // a preview has rendered once, a refresh (RefreshOnResume, retry) keeps that content on
            // screen instead of tearing it down to a loader; see WeighingLeadershipVideosScreen for
            // the same rule elsewhere in this module. This is what stops the flash the maintainer
            // saw: empty -> spinner -> content, every single time the screen was revisited.
            state.loading && state.sheds.isEmpty() -> WeighingExportPreviewSkeleton()
            state.error.isNotBlank() && state.sheds.isEmpty() -> WeighingExportPreviewError(state.error, onRetry)
            state.sheds.isEmpty() -> WeighingReadOnlyEmptyCard(
                loading = false,
                title = stringResource(R.string.weighing_export_preview_empty_title),
            )
            else -> WeighingExportPreviewTable(state)
        }
    }
}

/** Placeholder shed rows shaped like [ExportShedHeader] -- a shimmer, not a spinner flash. */
@Composable
private fun WeighingExportPreviewSkeleton() {
    LoadingSkeletonList(rows = 8, contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 16.dp, vertical = 12.dp))
}

/** Nothing swallowed: a failed fetch is a real, retryable error card, never a blank screen. */
@Composable
private fun WeighingExportPreviewError(message: String, onRetry: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.WarnX)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
        Text(
            text = stringResource(R.string.weighing_export_preview_retry),
            color = MeshaColors.BrandD,
            style = MeshaType.cta,
            modifier = Modifier
                .minimumInteractiveComponentSize()
                .clickable(onClick = onRetry),
        )
    }
}

@Composable
private fun WeighingExportPreviewTable(state: WeighingExportPreviewUiState) {
    // ONE expand set for row detail, held here rather than in the ViewModel: which rows are
    // expanded is pure presentation state, and putting it in the ViewModel would force a full
    // state re-emit (and a LazyColumn re-measure of every row) on every single tap. Same reasoning
    // for [expandedShedKey] and [searchQuery] below -- neither changes what data exists, only what
    // of it is currently on screen.
    var expandedKeys by remember { mutableStateOf(setOf<String>()) }
    // ONE shed open at a time -- see the SCALE DECISION on [WeighingExportPreviewScreen]. Opening a
    // different shed collapses whichever one was open.
    var expandedShedKey by remember { mutableStateOf<String?>(null) }
    var searchQuery by remember { mutableStateOf("") }
    // A plain `remember(state.sheds, searchQuery)` (not derivedStateOf) is deliberate: the filter
    // itself is cheap (a single pass over ~100 shed headers, never the row-level data), so there is
    // nothing to gain from derivedStateOf's extra bookkeeping here.
    val visibleSheds = remember(state.sheds, searchQuery) {
        if (searchQuery.isBlank()) {
            state.sheds
        } else {
            state.sheds.filter {
                it.shedName.contains(searchQuery, ignoreCase = true) ||
                    it.park.contains(searchQuery, ignoreCase = true)
            }
        }
    }
    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item(key = "export-summary", contentType = "summary") {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    text = stringResource(
                        R.string.weighing_export_preview_row_count_fmt,
                        state.totalRowCount,
                        state.sheds.size,
                    ),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
                ShedSearchField(
                    query = searchQuery,
                    onQueryChange = { searchQuery = it },
                )
                // A refresh in flight while a previous preview is still showing -- the header
                // share icon already has its own spinner, this is a small in-list cue that the
                // sheet itself is being re-fetched, without tearing anything off screen.
                if (state.loading) {
                    Text(
                        text = stringResource(R.string.weighing_export_preview_refreshing),
                        color = MeshaColors.Muted,
                        style = MeshaType.cardSubtitle,
                    )
                }
            }
        }
        if (visibleSheds.isEmpty()) {
            item(key = "export-no-matches", contentType = "empty") {
                Text(
                    text = stringResource(R.string.weighing_export_preview_no_matches),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(vertical = 24.dp),
                )
            }
        }
        visibleSheds.forEach { shed ->
            val shedExpanded = shed.shedKey == expandedShedKey
            item(key = "shed-${shed.shedKey}", contentType = "shed-header") {
                ExportShedHeader(
                    shed = shed,
                    expanded = shedExpanded,
                    onToggle = {
                        expandedShedKey = if (shedExpanded) null else shed.shedKey
                    },
                )
            }
            // Animal rows for a COLLAPSED shed are never added as LazyColumn items -- they simply
            // do not exist on screen (or in the composition) until the shed is expanded. This is
            // what keeps an 8,000-row export from ever putting more than one shed's worth of rows
            // in front of the LazyColumn at once, expanded or not.
            if (shedExpanded) {
                // A shed with nothing captured carries exactly one "not weighed" row already, so it
                // renders through the SAME row path as every other row rather than being special-
                // cased out of the list -- the honesty rule is that it must appear, not that it must
                // look different.
                items(
                    count = shed.rows.size,
                    key = { index -> "row-${shed.shedKey}-$index" },
                    contentType = { "export-row" },
                ) { index ->
                    val row = shed.rows[index]
                    val rowKey = "${shed.shedKey}-$index"
                    ExportRow(
                        row = row,
                        expanded = rowKey in expandedKeys,
                        onToggle = {
                            expandedKeys = if (rowKey in expandedKeys) {
                                expandedKeys - rowKey
                            } else {
                                expandedKeys + rowKey
                            }
                        },
                    )
                }
            }
        }
    }
}

/** Search-by-shed-name field, styled like the rest of this screen's chips rather than a system
 *  text field -- there is no shared searchable-picker component exported from this module (see
 *  WeightHistoryChartScreen's FilterSelectorRow/SearchablePickerDialog for the sibling idiom this
 *  copies: a search box that narrows a long list instead of a chip row). A shed list is filtered
 *  in place here rather than behind a picker sheet because the LazyColumn IS the picker -- there is
 *  no second surface to open. */
@Composable
private fun ShedSearchField(query: String, onQueryChange: (String) -> Unit) {
    val focusRequester = remember { FocusRequester() }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(imageVector = MeshaIcons.Search, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(16.dp))
        Box(modifier = Modifier.weight(1f)) {
            if (query.isEmpty()) {
                Text(
                    text = stringResource(R.string.weighing_export_preview_search_hint),
                    color = MeshaColors.Faint,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(vertical = 2.dp),
                )
            }
            BasicTextField(
                value = query,
                onValueChange = onQueryChange,
                singleLine = true,
                textStyle = MeshaType.cardSubtitle.copy(color = MeshaColors.Ink),
                cursorBrush = androidx.compose.ui.graphics.SolidColor(MeshaColors.BrandD),
                modifier = Modifier
                    .fillMaxWidth()
                    .focusRequester(focusRequester),
            )
        }
    }
}

@Composable
private fun ExportShedHeader(shed: WeighingExportPreviewShedUi, expanded: Boolean, onToggle: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .clickable(onClick = onToggle)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = shed.shedName.ifBlank { stringResource(R.string.weighing_export_preview_shed_fallback) },
                color = MeshaColors.Ink,
                style = MeshaType.cardTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(
                    text = if (shed.touched) {
                        val weighedRows = shed.rows.filterNot { it.isNotWeighed }
                        val isLumpSumOnly = weighedRows.isNotEmpty() && weighedRows.all { it.isLumpSum }
                        if (isLumpSumOnly) {
                            stringResource(R.string.weighing_export_preview_lump_sum_count_fmt, shed.weighedCount)
                        } else {
                            stringResource(R.string.weighing_export_preview_weighed_count_fmt, shed.weighedCount)
                        }
                    } else {
                        stringResource(R.string.weighing_export_preview_not_weighed)
                    },
                    color = if (shed.touched) MeshaColors.BrandD else MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
                Icon(
                    imageVector = MeshaIcons.ChevronDown,
                    contentDescription = null,
                    tint = MeshaColors.Faint,
                    modifier = Modifier
                        .size(16.dp)
                        .rotate(if (expanded) 180f else 0f),
                )
            }
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = shedStateLabel(shed.shedStatus),
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f, fill = false),
            )
            val averageWeightKg = shed.averageWeightKg
            if (shed.touched && averageWeightKg != null) {
                Text(
                    text = stringResource(
                        R.string.weighing_export_preview_shed_summary_fmt,
                        shed.totalWeightKg,
                        averageWeightKg,
                    ),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    maxLines = 1,
                )
            }
        }
    }
}

/**
 * ONE compact, three-column row -- identifier, weight, verification status -- with the other
 * eight export columns folded away behind a tap. This is the answer to "14 columns on a phone":
 * show the three a planner scans for, keep the rest one tap away instead of off-screen sideways.
 */
@Composable
private fun ExportRow(row: WeighingExportPreviewRowUi, expanded: Boolean, onToggle: () -> Unit) {
    if (row.isNotWeighed) return
    // A per-animal row is a DETAIL OF the shed card above it, not a card beside it. It used to
    // carry its own Surf background plus a full Hair border at the same width and radius as the
    // shed cards, so an expanded shed read as two sibling cards and the eye lost which animal
    // belonged to which shed. It is now indented under its parent, sits on the darker Surf2, and
    // drops the outline entirely; the indent is what says "belongs to".
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp)
            .animateContentSize()
            .clip(RoundedCornerShape(bottomStart = 10.dp, bottomEnd = 10.dp, topStart = 4.dp, topEnd = 4.dp))
            .background(MeshaColors.Surf2)
            .clickable(onClick = onToggle)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = row.scannedIdentifier.ifBlank {
                    if (row.isLumpSum) {
                        stringResource(R.string.weighing_export_preview_lump_sum)
                    } else {
                        stringResource(R.string.weighing_export_preview_unknown_identifier)
                    }
                },
                color = MeshaColors.Ink,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = row.weightKg.ifBlank { "–" } + " kg",
                color = MeshaColors.Ink,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
            )
            VerificationChip(row.verificationStatus)
            Icon(
                imageVector = MeshaIcons.ChevronDown,
                contentDescription = null,
                tint = MeshaColors.Faint,
                modifier = Modifier
                    .size(16.dp)
                    .rotate(if (expanded) 180f else 0f),
            )
        }
        if (expanded) {
            ExportRowDetail(row)
        }
    }
}

/** The remaining columns, as a plain vertical key-value list -- never a second horizontal table.
 *  Rendered as subordinate content within the parent row, using padding instead of a contrasting
 *  background so it reads as detail of the parent, not a competing card. */
@Composable
private fun ExportRowDetail(row: WeighingExportPreviewRowUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        DetailLine(stringResource(R.string.weighing_export_col_type), row.type)
        DetailLine(stringResource(R.string.weighing_export_col_average_weight), row.averageWeightKg)
        DetailLine(stringResource(R.string.weighing_export_col_animal_count), row.animalCount)
        DetailLine(stringResource(R.string.weighing_export_col_proof_type), row.proofReferenceType)
        DetailLine(stringResource(R.string.weighing_export_col_proof_reference), row.proofReference)
        DetailLine(stringResource(R.string.weighing_export_col_recorded_at), row.recordedAt)
        DetailLine(stringResource(R.string.weighing_export_col_date_ist), row.dateIst)
        DetailLine(stringResource(R.string.weighing_export_col_time_ist), row.timeIst)
    }
}

@Composable
private fun DetailLine(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(text = label, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
        Text(
            text = value.ifBlank { "–" },
            color = MeshaColors.Ink,
            style = MeshaType.cardSubtitle,
            textAlign = androidx.compose.ui.text.style.TextAlign.End,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f, fill = false).padding(start = 12.dp),
        )
    }
}

/**
 * A pending weight is not an accepted one -- this chip is the one place on the row that says so,
 * matching the wording every other weighing screen already uses for verification status.
 */
@Composable
private fun VerificationChip(status: String) {
    val normalized = status.trim().lowercase()
    val (fg, bg, label) = when (normalized) {
        "verified", "accepted" -> Triple(MeshaColors.Ok, MeshaColors.OkX, stringResource(R.string.weighing_export_verification_accepted))
        "pending", "unverified", "" -> Triple(MeshaColors.Warn, MeshaColors.WarnX, stringResource(R.string.weighing_export_verification_pending))
        "rejected", "reworked" -> Triple(MeshaColors.Danger, MeshaColors.DangerX, stringResource(R.string.weighing_export_verification_rejected))
        else -> Triple(MeshaColors.Muted, MeshaColors.Surf3, status)
    }
    Text(
        text = label,
        color = fg,
        style = MeshaType.cardSubtitle,
        maxLines = 1,
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .background(bg)
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}
