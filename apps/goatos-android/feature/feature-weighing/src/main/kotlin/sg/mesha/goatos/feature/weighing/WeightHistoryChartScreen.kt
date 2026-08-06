package sg.mesha.goatos.feature.weighing

// telemetry:exempt stateless renderer; the ViewModel owns loading/refresh reporting.

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.clickable
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Weight history, for leadership only.
 *
 * One series per RFID tag (or per shed for a lump-sum weighing), drawn as bars across the weigh
 * days that actually happened. Deliberately plain Compose `Canvas`: a chart library would be a new
 * dependency for one screen, and the shapes here are bars and baselines.
 *
 * The honesty rules this screen is held to, because a weight chart that lies is worse than none:
 *  - a weigh day that did NOT happen is never interpolated; bars sit on the days that exist and a
 *    gap between them stays a gap.
 *  - one bar renders at a fixed readable width, not stretched to fill the plot.
 *  - a truncated payload says so, rather than presenting a partial series as the whole history.
 *  - lump-sum and individual weights are never mixed on one axis; the axis caption names which
 *    kind is on screen.
 */
@Immutable
data class WeightHistoryChartUiRow(
    /** The RFID tag, or the shed name when this series is a lump-sum weighing. */
    val label: String,
    val shedName: String,
    /** "individual" or "lump_sum" — drives the axis caption, never mixed in one chart. */
    val captureKind: String,
    val points: List<WeightHistoryChartUiPoint>,
) {
    /**
     * Built ONCE here rather than in the LazyColumn key lambda, which would allocate a new String
     * per row on every recomposition of the list. Kind is part of it because a shed's lump-sum
     * series and an animal's individual series can carry the same label.
     */
    val key: String = captureKind + ":" + label
}

@Immutable
data class WeightHistoryChartUiPoint(
    val dateLabel: String,
    val value: Double,
    /** Sent back by the verifier. Drawn in danger tone so a rejected weigh reads as suspect. */
    val sentBack: Boolean = false,
    /**
     * Captured but not yet checked by a verifier. Drawn in a warn tone: the measurement is real
     * and belongs on the chart, but presenting it in the same colour as an accepted weight would
     * claim a verification that has not happened.
     */
    val awaitingCheck: Boolean = false,
)

/**
 * The ViewModel's contract for this screen. Every field here is DATA (an id, an enum token, a
 * count, a raw backend string) rather than a rendered sentence -- the ViewModel cannot call
 * `stringResource`, so it must never own English text. [WeightHistoryChartScreen] is the only
 * place that turns these into hi/kn/te-aware labels, the same pattern [WeighingScreen] uses for
 * `backendStatus` -> `mapWeighingStatusLabel`.
 */
@Immutable
data class WeightHistoryChartUiState(
    /** "individual" | "lump_sum" | null -- never a display label. */
    val selectedCaptureKind: String? = null,
    val parkChips: List<WeighingFilterChipUiRow> = emptyList(),
    val shedChips: List<WeighingFilterChipUiRow> = emptyList(),
    val rows: List<WeightHistoryChartUiRow> = emptyList(),
    /** Raw park/shed names from the backend -- data, not translated. */
    val selectedParkName: String? = null,
    val selectedShedName: String? = null,
    /** Free-text tag search. At ~80 animals per shed the list is unusable without it. */
    val searchQuery: String = "",
    /** Count backing both the plural "N animals" label and the narrow-first wall message. */
    val matchCount: Int = 0,
    /**
     * Set when the result set is too large to render honestly and the reader must narrow first.
     * Dumping 8,000 series cards is not a list, it is a denial of service on the reader.
     */
    val mustNarrow: Boolean = false,
    /** False only before the first successful load; distinct from an empty result set. */
    val hasData: Boolean = false,
    /** True when the backend returned data but zero series exist at all (vs. filtered to zero). */
    val seriesEmpty: Boolean = false,
    /** Raw backend error text, already language-neutral (server messages are not localized). */
    val errorMessage: String? = null,
    val payloadTruncated: Boolean = false,
    val perSeriesCapped: Boolean = false,
    val isLoading: Boolean = false,
)

@Composable
fun WeightHistoryChartScreen(
    state: WeightHistoryChartUiState,
    onSelectKind: (String?) -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    onSelectShed: (String?) -> Unit = {},
    onSearch: (String) -> Unit = {},
    onRefresh: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // L0 read screen: refresh on every return so the chart never renders a stale window.
    RefreshOnResume { onRefresh() }

    // All display text is resolved HERE, at render time, from the raw ids/enums/counts the
    // ViewModel emits -- the ViewModel cannot call stringResource, so it must never own English
    // text. This is the same split WeighingScreen uses for backendStatus -> mapWeighingStatusLabel.
    val allWeightsLabel = stringResource(R.string.weighing_history_all_weights)
    val allParksLabel = stringResource(R.string.weighing_all_parks)
    val allShedsLabel = stringResource(R.string.weighing_history_all_sheds)
    val kindChips = listOf(
        WeighingFilterChipUiRow(
            id = WEIGHT_KIND_INDIVIDUAL,
            label = stringResource(R.string.weighing_history_kind_individual),
            selected = state.selectedCaptureKind == WEIGHT_KIND_INDIVIDUAL,
        ),
        WeighingFilterChipUiRow(
            id = WEIGHT_KIND_LUMP_SUM,
            label = stringResource(R.string.weighing_history_kind_lump_sum),
            selected = state.selectedCaptureKind == WEIGHT_KIND_LUMP_SUM,
        ),
    )
    val axisCaption = when (state.selectedCaptureKind) {
        WEIGHT_KIND_INDIVIDUAL -> stringResource(R.string.weighing_history_axis_individual)
        WEIGHT_KIND_LUMP_SUM -> stringResource(R.string.weighing_history_axis_lump_sum)
        // With no kind chosen both grains are on screen. The caption says so rather than naming a
        // unit that is only true for half the cards.
        else -> stringResource(R.string.weighing_history_axis_mixed)
    }
    val truncationNotice = when {
        state.payloadTruncated && state.perSeriesCapped ->
            stringResource(R.string.weighing_history_truncation_both_fmt, MAX_POINTS_PER_SERIES)
        state.payloadTruncated -> stringResource(R.string.weighing_history_truncation_payload)
        state.perSeriesCapped ->
            stringResource(R.string.weighing_history_truncation_per_series_fmt, MAX_POINTS_PER_SERIES)
        else -> ""
    }
    val narrowFirstMessage = if (state.mustNarrow) {
        stringResource(R.string.weighing_history_narrow_first_fmt, state.matchCount)
    } else {
        ""
    }
    val resultCountLabel = if (state.mustNarrow || state.rows.isEmpty()) {
        ""
    } else {
        pluralStringResource(R.plurals.weighing_history_animal_count, state.matchCount, state.matchCount)
    }
    val parkSelectorLabel = stringResource(
        R.string.weighing_history_park_selector_fmt,
        state.selectedParkName ?: allParksLabel,
    )
    val shedSelectorLabel = stringResource(
        R.string.weighing_history_shed_selector_fmt,
        state.selectedShedName ?: allShedsLabel,
    )
    val searchHint = stringResource(R.string.weighing_history_search_hint)
    val emptyNoData = stringResource(R.string.weighing_history_empty_no_data)
    val emptyMessage = when {
        // A backend error is already language-neutral server text; passing it through matches how
        // every other screen in this module surfaces a fetch failure.
        state.errorMessage != null -> state.errorMessage
        !state.hasData -> if (state.isLoading) "" else emptyNoData
        state.seriesEmpty -> emptyNoData
        else -> stringResource(R.string.weighing_history_empty_filtered)
    }

    // MeshaScreenHeader, not a title drawn inside the list: every other destination in this bar
    // (Tasks, Videos, Alerts) wears the module eyebrow + title + actions, and a screen that
    // renders its own heading loses the drawer affordance and reads as a different app.
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            eyebrow = stringResource(R.string.weighing_eyebrow),
            title = stringResource(R.string.weighing_history_title),
            actions = {
                SyncIconButton(
                    isSyncing = state.isLoading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_history_refresh_cd),
                )
            },
        )
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // These controls only mean anything once the first load has resolved -- before that there
        // are no kinds/parks/sheds to filter by, exactly like the ViewModel's dto == null branch.
        if (state.hasData) {
            item {
                Text(
                    text = axisCaption,
                    style = MeshaType.cardSubtitle,
                    color = MeshaColors.Muted,
                    modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp),
                )
            }
            // Chips reuse the module's own pill, so this screen cannot drift from the rest of the app.
            item { WeighingFilterChips(options = kindChips, allLabel = allWeightsLabel, onSelect = onSelectKind) }
        }
        // Parks stay chips while they fit; sheds almost never do.
        if (state.parkChips.size in 2..SELECTOR_THRESHOLD) {
            item { WeighingFilterChips(options = state.parkChips, allLabel = allParksLabel, onSelect = onSelectPark) }
        } else if (state.parkChips.size > SELECTOR_THRESHOLD) {
            item {
                FilterSelectorRow(
                    label = parkSelectorLabel,
                    options = state.parkChips,
                    allLabel = allParksLabel,
                    onSelect = onSelectPark,
                )
            }
        }
        // SHEDS ARE NEVER CHIPS. A park holds ~100 of them, and even five pills already wrap to a
        // second row and push the chart below the fold. The selector is one row at any scale, and
        // it is the only control here that can survive a real park.
        if (state.shedChips.isNotEmpty()) {
            item {
                FilterSelectorRow(
                    label = shedSelectorLabel,
                    options = state.shedChips,
                    allLabel = allShedsLabel,
                    onSelect = onSelectShed,
                )
            }
        }
        if (state.hasData) {
            item {
                TagSearchField(
                    query = state.searchQuery,
                    hint = searchHint,
                    onQueryChange = onSearch,
                )
            }
        }
        if (resultCountLabel.isNotBlank()) {
            item {
                Text(
                    text = resultCountLabel,
                    style = MeshaType.caption,
                    color = MeshaColors.Muted,
                    modifier = Modifier.padding(horizontal = 16.dp),
                )
            }
        }
        if (truncationNotice.isNotBlank()) {
            item {
                Text(
                    text = truncationNotice,
                    style = MeshaType.caption,
                    color = MeshaColors.Warn,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp),
                )
            }
        }
        if (state.mustNarrow) {
            item {
                Box(
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 48.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = narrowFirstMessage,
                        style = MeshaType.body,
                        color = MeshaColors.Muted,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        } else if (state.rows.isEmpty()) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 16.dp, vertical = 48.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = emptyMessage,
                        style = MeshaType.body,
                        color = MeshaColors.Muted,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        } else {
            items(
                state.rows,
                key = { it.key },
                contentType = { "weight_series_card" },
            ) { row ->
                WeightSeriesCard(row)
            }
        }
    }
    }
}

/** "individual" | "lump_sum" -- raw capture-kind tokens, matched against [WeightHistoryChartUiState.selectedCaptureKind]. */
private const val WEIGHT_KIND_INDIVIDUAL = "individual"
private const val WEIGHT_KIND_LUMP_SUM = "lump_sum"

/**
 * Weigh days drawn per series (see [WeightHistoryChartUiRow]). Mirrors the ViewModel's own cap so
 * the truncation notice names the same number it actually applied; kept here rather than passed
 * through state because it never varies at runtime.
 */
private const val MAX_POINTS_PER_SERIES = 20

@Composable
private fun WeightSeriesCard(row: WeightHistoryChartUiRow) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .padding(14.dp),
    ) {
        Text(
            text = row.label,
            style = MeshaType.cardTitle,
            color = MeshaColors.Ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = row.shedName,
            style = MeshaType.cardSubtitle,
            color = MeshaColors.Muted,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(12.dp))
        WeightBars(row.points)
        Spacer(Modifier.height(8.dp))
        // Date labels live OUTSIDE the canvas as real Text, so they scale with the reader's font
        // and translate in hi/kn/te instead of being baked into the drawing.
        Row(modifier = Modifier.fillMaxWidth()) {
            row.points.forEach { point ->
                Text(
                    text = point.dateLabel,
                    style = MeshaType.caption,
                    color = MeshaColors.Muted,
                    textAlign = TextAlign.Center,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

/** A single bar's width when a series has only one weigh day. */
private val SINGLE_BAR_WIDTH = 56.dp

@Composable
private fun WeightBars(points: List<WeightHistoryChartUiPoint>) {
    val maxValue = points.maxOfOrNull { it.value } ?: 0.0
    Column(modifier = Modifier.fillMaxWidth()) {
        // Value labels above the bars: a bar chart without numbers makes a reader estimate, and
        // this is a weight record, not a mood board.
        Row(modifier = Modifier.fillMaxWidth()) {
            points.forEach { point ->
                Text(
                    text = formatWeight(point.value),
                    style = MeshaType.caption,
                    color = when {
                        point.sentBack -> MeshaColors.Danger
                        point.awaitingCheck -> MeshaColors.Warn
                        else -> MeshaColors.BrandD
                    },
                    textAlign = TextAlign.Center,
                    maxLines = 1,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        Spacer(Modifier.height(4.dp))
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .height(120.dp),
            verticalAlignment = Alignment.Bottom,
        ) {
            points.forEach { point ->
                // Status is announced, not just coloured. A screen reader previously read out a
                // bare number, and in sunlight a sent-back weigh looked identical to an accepted
                // one -- the reader had no way to know a value was refused or unchecked.
                val pointDesc = when {
                    point.sentBack -> stringResource(R.string.weighing_history_point_sent_back, point.dateLabel, formatWeight(point.value))
                    point.awaitingCheck -> stringResource(R.string.weighing_history_point_awaiting, point.dateLabel, formatWeight(point.value))
                    else -> stringResource(R.string.weighing_history_point_accepted, point.dateLabel, formatWeight(point.value))
                }
                Box(
                    modifier = Modifier
                        .weight(1f)
                        .semantics { contentDescription = pointDesc },
                    contentAlignment = Alignment.BottomCenter,
                ) {
                    val fraction = if (maxValue > 0.0) (point.value / maxValue).toFloat() else 0f
                    Canvas(
                        modifier = Modifier
                            .then(
                                // One point must not become a slab across the card.
                                if (points.size == 1) Modifier.width(SINGLE_BAR_WIDTH) else Modifier.fillMaxWidth(0.62f),
                            )
                            .height(120.dp),
                    ) {
                        val barTop = size.height * (1f - fraction.coerceIn(0.06f, 1f))
                        drawRoundRect(
                            color = when {
                                point.sentBack -> MeshaColors.Danger
                                point.awaitingCheck -> MeshaColors.Warn
                                else -> MeshaColors.Brand
                            },
                            topLeft = Offset(0f, barTop),
                            size = Size(size.width, size.height - barTop),
                            cornerRadius = CornerRadius(6f, 6f),
                        )
                    }
                }
            }
        }
        // Baseline: the bars need a floor to be read against.
        Canvas(modifier = Modifier.fillMaxWidth().height(1.dp)) {
            drawRect(color = MeshaColors.Hair, size = Size(size.width, size.height))
        }
    }
}

private fun formatWeight(value: Double): String {
    val rounded = kotlin.math.round(value * 10.0) / 10.0
    return if (rounded % 1.0 == 0.0) "${rounded.toInt()}" else "$rounded"
}


/** Above this many values a chip row stops being scannable and becomes a searchable picker. */
private const val SELECTOR_THRESHOLD = 8

/**
 * One tappable row that opens a SEARCHABLE list, for dimensions with too many values to wear as
 * chips. Shows the current selection so the reader never has to guess what the chart is scoped to.
 */
@Composable
internal fun FilterSelectorRow(
    label: String,
    options: List<WeighingFilterChipUiRow>,
    allLabel: String,
    onSelect: (String?) -> Unit,
    /** Names the DIMENSION being picked ("Shed"). The sheet used [allLabel] here, which just
     *  repeated the first row's own text back at the reader. */
    title: String = label.substringBefore(":").trim(),
) {
    var open by remember { mutableStateOf(false) }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .clickable { open = true }
            .padding(horizontal = 14.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            style = MeshaType.body,
            color = MeshaColors.Ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        Text(text = "▾", style = MeshaType.body, color = MeshaColors.Muted)
    }
    if (open) {
        SearchablePickerDialog(
            title = title,
            options = options,
            allLabel = allLabel,
            onPick = {
                onSelect(it)
                open = false
            },
            onDismiss = { open = false },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SearchablePickerDialog(
    title: String,
    options: List<WeighingFilterChipUiRow>,
    allLabel: String,
    onPick: (String?) -> Unit,
    onDismiss: () -> Unit,
) {
    var query by remember { mutableStateOf("") }
    // Remembered on the query so typing does not rescan the list on every unrelated recomposition.
    val filtered = remember(options, query) {
        if (query.isBlank()) options else options.filter { it.label.contains(query, ignoreCase = true) }
    }
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = MeshaColors.Surf2,
        dragHandle = { BottomSheetDefaults.DragHandle(color = MeshaColors.Hair) },
    ) {
        Column(modifier = Modifier.padding(bottom = 24.dp)) {
            Text(
                text = title,
                style = MeshaType.cardTitle,
                color = MeshaColors.Ink,
                modifier = Modifier.padding(start = 20.dp, end = 20.dp, bottom = 12.dp),
            )
            TagSearchField(
                query = query,
                hint = stringResource(R.string.weighing_history_picker_search),
                onQueryChange = { query = it },
            )
            Spacer(Modifier.height(8.dp))
            if (filtered.isEmpty()) {
                Text(
                    text = stringResource(R.string.weighing_history_nothing_matches_fmt, query),
                    style = MeshaType.body,
                    color = MeshaColors.Muted,
                    modifier = Modifier.padding(horizontal = 20.dp, vertical = 24.dp),
                )
            }
            LazyColumn(modifier = Modifier.heightIn(max = 420.dp)) {
                item(key = "__all__") {
                    PickerRow(
                        label = allLabel,
                        selected = options.none { it.selected },
                        onClick = { onPick(null) },
                    )
                }
                items(filtered, key = { it.id }, contentType = { "picker_row" }) { option ->
                    PickerRow(label = option.label, selected = option.selected, onClick = { onPick(option.id) })
                }
            }
        }
    }
}

@Composable
internal fun PickerRow(label: String, selected: Boolean = false, onClick: () -> Unit) {
    // The selected row is stated with a mark AND a colour, not colour alone: on a phone in
    // sunlight a green-vs-white text difference is the first thing to disappear.
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .background(if (selected) MeshaColors.Surf2 else Color.Transparent)
            .padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            style = MeshaType.body,
            color = if (selected) MeshaColors.Brand else MeshaColors.Ink,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        if (selected) {
            Text(text = "\u2713", style = MeshaType.body, color = MeshaColors.Brand)
        }
    }
}

@Composable
internal fun TagSearchField(query: String, hint: String, onQueryChange: (String) -> Unit) {
    BasicTextField(
        value = query,
        onValueChange = onQueryChange,
        singleLine = true,
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
        cursorBrush = SolidColor(MeshaColors.Brand),
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        decorationBox = { inner ->
            if (query.isEmpty()) {
                Text(text = hint, style = MeshaType.body, color = MeshaColors.Muted)
            }
            inner()
        },
    )
}
