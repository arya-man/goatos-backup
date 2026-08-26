package sg.mesha.goatos.feature.weighing

// telemetry:exempt Stateless renderer; the ViewModel owns loading/refresh reporting.

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * Leadership growth (ADG), built from WEIGHING DATA ONLY.
 *
 * An animal here is a scanned tag and nothing else. This screen never resolves a tag to a goat and
 * never reads herd, vaccination or protocol data -- weighing is an isolated feature.
 *
 * The rules this screen is held to, because a growth number that lies is worse than none:
 *  - UNKNOWN IS NOT ZERO. Growth needs the same tag weighed twice. Where that has not happened the
 *    screen says so; it never prints "0 g/day", which would claim the herd is not growing.
 *  - The denominator is always on screen. "312 of 2140" is what makes a herd figure a measurement
 *    rather than a rumour from whichever animals happened to be weighed twice.
 *  - Colour encodes GROWTH, not verification state. Verification is a caption; a rejected reading
 *    is excluded from the maths upstream and unverified ones are counted in words.
 *  - The shed bars share ONE scale, so their lengths are comparable. That is the entire point of
 *    ranking them.
 */
@Immutable
data class WeighingGrowthUiState(
    val title: String = "",
    val eyebrow: String = "",
    val scopeLabel: String = "",

    val headline: WeighingGrowthHeadlineUi = WeighingGrowthHeadlineUi(),
    val trend: List<WeighingGrowthTrendUi> = emptyList(),
    val sheds: List<WeighingGrowthShedUi> = emptyList(),
    /** Count of readings not yet checked by a verifier. Rendered as a CAPTION, never as colour. */
    /** Parks the reader may scope to. Rendered as a SEARCHABLE selector, never as chips: a
     *  tenant can hold many parks and chips do not survive that. */
    val parkOptions: List<WeighingFilterChipUiRow> = emptyList(),
    val selectedParkId: String? = null,
    /** The herd median this period. Shed bars are coloured RELATIVE to it. */
    val herdAdgGPerDay: Double? = null,
    /**
     * Group-weighed sheds, rendered in their OWN section. Never merged into the ADG numbers: a
     * shed total with a head count cannot yield growth per animal, and a screen that quietly
     * folded it in would be averaging two different things.
     */
    val lumpSumSheds: List<WeighingGrowthLumpSumUi> = emptyList(),
    /** The animals behind the losing count. Shown inline so the tap has somewhere to land. */
    val losingAnimals: List<WeighingGrowthLosingUi> = emptyList(),
    val showLosing: Boolean = false,
    val unverifiedCount: Int = 0,
    val hasError: Boolean = false,
    val errorText: String = "",
    val isLoading: Boolean = false,
    val hasLoadedOnce: Boolean = false,
)

@Immutable
data class WeighingGrowthHeadlineUi(
    /**
     * The herd's daily gain: kids weighed twice AND sheds weighed as one total, animal-weighted.
     * Null when nothing was weighed twice -- rendered as a dash, never as zero.
     */
    val adgGPerDay: Double? = null,
    val deltaGPerDay: Double? = null,
    val positivePercent: Double? = null,
    /** Animals losing on their latest weigh — the number the tile shows AND drills into. */
    val negativeCount: Int = 0,
    val animalsWithTwoPlusWeighs: Int = 0,
    val totalAnimalsWeighed: Int = 0,
)

@Immutable
data class WeighingGrowthTrendUi(
    val label: String,
    val medianAdgGPerDay: Double,
)

@Immutable
data class WeighingGrowthLosingUi(
    val scannedIdentifier: String,
    val shedName: String,
    val previousWeightKg: Double,
    val latestWeightKg: Double,
    val adgGPerDay: Double,
) {
    val key: String = scannedIdentifier
}

@Immutable
data class WeighingGrowthLumpSumUi(
    val locationId: String,
    val displayName: String,
    val averageWeightKg: Double?,
    val headCount: Int,
    val weekLabel: String,
) {
    val key: String = locationId.ifBlank { displayName } + ":" + weekLabel
}

@Immutable
data class WeighingGrowthShedUi(
    val locationId: String,
    val displayName: String,
    val animalCount: Int,
    val medianAdgGPerDay: Double?,
    val medianWeightKg: Double?,
) {
    val key: String = locationId.ifBlank { displayName }
}

@Composable
fun WeighingGrowthScreen(
    state: WeighingGrowthUiState,
    onRefresh: () -> Unit = {},
    onOpenLosing: () -> Unit = {},
    onSelectPark: (String?) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // L0 read screen: cached rows render instantly and a background refresh fires on every
    // return to this destination, so a retained ViewModel never shows stale growth numbers.
    RefreshOnResume { onRefresh() }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            eyebrow = state.eyebrow,
            title = state.title,
            subtitle = state.scopeLabel.takeIf { it.isNotBlank() },
            actions = {
                SyncIconButton(
                    isSyncing = state.isLoading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_growth_refresh_cd),
                )
            },
        )
        // NOTHING READ YET is not the same as NOTHING TO SHOW. A freshly navigated screen starts
        // with an empty state flow and its refresh has not necessarily begun, so showing a skeleton
        // before a single page load would be a loading wall that then flips to content or an empty
        // state. That flip is the flicker being removed. Loading is already told by the spinning
        // refresh icon in the header, so a shimmer that flashes in and out on every navigation is
        // pure redundancy.
        if (state.sheds.isEmpty() && state.trend.isEmpty() && !state.hasLoadedOnce) {
            // NOTHING is drawn here. Loading is told by the spinning refresh icon in the app bar,
            // not by a skeleton that flashes in and straight back out on every navigation.
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = androidx.compose.foundation.layout.PaddingValues(
                start = 16.dp, end = 16.dp, top = 12.dp, bottom = 24.dp,
            ),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (state.parkOptions.size > 1) {
                item(key = "park_filter", contentType = "growth_filter") {
                    FilterSelectorRow(
                        label = stringResource(
                            R.string.weighing_growth_park_selector,
                            state.parkOptions.firstOrNull { it.selected }?.label
                                ?: stringResource(R.string.weighing_growth_scope_all),
                        ),
                        options = state.parkOptions,
                        allLabel = stringResource(R.string.weighing_growth_scope_all),
                        onSelect = onSelectPark,
                    )
                }
            }
            item(key = "headline", contentType = "growth_headline") {
                HeadlineTiles(
                    headline = state.headline,
                    lumpSumAnimals = state.lumpSumSheds.sumOf { it.headCount },
                    onOpenLosing = onOpenLosing,
                )
            }
            if (state.trend.isNotEmpty()) {
                item(key = "trend", contentType = "growth_trend") { TrendCard(state.trend) }
            }
            if (state.sheds.isNotEmpty()) {
                item(key = "sheds_title", contentType = "growth_section") {
                    Text(
                        text = stringResource(R.string.weighing_growth_sheds_title),
                        style = MeshaType.cardTitle,
                        color = MeshaColors.Ink,
                    )
                }
                // Windowed: a park can hold ~100 sheds, so these are lazy items with stable keys,
                // never a forEach inside a Column.
                items(state.sheds, key = { it.key }, contentType = { "growth_shed_row" }) { shed ->
                    ShedRow(shed, state.sheds, state.herdAdgGPerDay)
                }
            }
            if (state.sheds.isEmpty() && state.trend.isEmpty()) {
                item(key = "empty", contentType = "growth_empty") {
                    Box(
                        modifier = Modifier.fillMaxWidth().padding(vertical = 48.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            text = if (state.hasError) state.errorText
                            else stringResource(R.string.weighing_growth_empty),
                            style = MeshaType.body,
                            color = MeshaColors.Muted,
                            textAlign = TextAlign.Center,
                        )
                    }
                }
            }
            if (state.showLosing && state.losingAnimals.isNotEmpty()) {
                item(key = "losing_title", contentType = "growth_section") {
                    Text(
                        text = stringResource(R.string.weighing_growth_losing_title),
                        style = MeshaType.cardTitle,
                        color = MeshaColors.Danger,
                    )
                }
                items(
                    state.losingAnimals,
                    key = { it.key },
                    contentType = { "growth_losing_row" },
                ) { animal ->
                    LosingRow(animal)
                }
            }
            if (state.lumpSumSheds.isNotEmpty()) {
                item(key = "lump_title", contentType = "growth_section") {
                    Column {
                        Text(
                            text = stringResource(R.string.weighing_growth_lumpsum_title),
                            style = MeshaType.cardTitle,
                            color = MeshaColors.Ink,
                        )
                        Text(
                            text = stringResource(R.string.weighing_growth_lumpsum_caption),
                            style = MeshaType.caption,
                            color = MeshaColors.Muted,
                        )
                    }
                }
                items(
                    state.lumpSumSheds,
                    key = { it.key },
                    contentType = { "growth_lumpsum_row" },
                ) { shed ->
                    LumpSumRow(shed)
                }
            }
            if (state.unverifiedCount > 0) {
                item(key = "quality", contentType = "growth_quality") {
                    Text(
                        text = pluralStringResource(
                            R.plurals.weighing_growth_quality_unverified,
                            state.unverifiedCount,
                            state.unverifiedCount,
                        ),
                        style = MeshaType.caption,
                        color = MeshaColors.Muted,
                    )
                }
            }
        }
    }
}

@Composable
private fun HeadlineTiles(
    headline: WeighingGrowthHeadlineUi,
    lumpSumAnimals: Int,
    onOpenLosing: () -> Unit,
) {
    val unknown = stringResource(R.string.weighing_growth_unknown_value)
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Tile(
                label = stringResource(R.string.weighing_growth_tile_herd),
                // The dash is the whole point: no second weigh means no growth figure exists.
                value = headline.adgGPerDay?.let { formatGPerDay(it) } ?: unknown,
                valueColor = growthTone(headline.adgGPerDay, null),
                meta = headline.deltaGPerDay?.let { formatDelta(it) }
                    ?: stringResource(R.string.weighing_growth_no_previous),
                modifier = Modifier.weight(1f),
            )
            Tile(
                label = stringResource(R.string.weighing_growth_tile_gaining),
                value = headline.positivePercent?.let { formatPercent(it) } ?: unknown,
                valueColor = MeshaColors.Ink,
                meta = stringResource(R.string.weighing_growth_of_measurable),
                modifier = Modifier.weight(1f),
            )
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Tile(
                label = stringResource(R.string.weighing_growth_tile_measured),
                value = "${headline.animalsWithTwoPlusWeighs}/${headline.totalAnimalsWeighed}",
                valueColor = MeshaColors.Ink,
                // States the SCOPE, not just the rule. Every figure in this row counts
                // individually-weighed animals only; a lumpsum shed contributes no per-animal
                // growth, so leaving its animals uncounted AND unmentioned would let a reader
                // take the herd number for the whole farm.
                meta = if (lumpSumAnimals > 0) {
                    stringResource(R.string.weighing_growth_needs_two_with_lumpsum, lumpSumAnimals)
                } else {
                    stringResource(R.string.weighing_growth_needs_two)
                },
                modifier = Modifier.weight(1f),
            )
            Tile(
                label = stringResource(R.string.weighing_growth_tile_losing),
                value = headline.negativeCount.toString(),
                valueColor = if (headline.negativeCount > 0) MeshaColors.Danger else MeshaColors.Ink,
                meta = stringResource(R.string.weighing_growth_losing_meta),
                modifier = Modifier.weight(1f).clickable(onClick = onOpenLosing),
            )
        }
    }
}

@Composable
private fun Tile(
    label: String,
    value: String,
    valueColor: androidx.compose.ui.graphics.Color,
    meta: String,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Text(text = label, style = MeshaType.caption, color = MeshaColors.Muted, maxLines = 1)
        Text(text = value, style = MeshaType.screenTitle, color = valueColor, maxLines = 1)
        Text(
            text = meta,
            style = MeshaType.caption,
            color = MeshaColors.Muted,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun TrendCard(points: List<WeighingGrowthTrendUi>) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .padding(12.dp),
    ) {
        Text(
            text = stringResource(R.string.weighing_growth_trend_title),
            style = MeshaType.cardTitle,
            color = MeshaColors.Ink,
        )
        Spacer(Modifier.height(10.dp))
        val maxValue = (points.maxOfOrNull { it.medianAdgGPerDay } ?: 0.0).coerceAtLeast(1.0)
        val lineDesc = stringResource(R.string.weighing_growth_trend_title)
        Canvas(
            modifier = Modifier
                .fillMaxWidth()
                .height(110.dp)
                .semantics { contentDescription = lineDesc },
        ) {
            // Zero-anchored: a truncated axis exaggerates a small change into a cliff.
            val baseline = size.height
            val stepX = if (points.size > 1) size.width / (points.size - 1) else 0f
            var prev: Offset? = null
            points.forEachIndexed { index, point ->
                val x = if (points.size > 1) stepX * index else size.width / 2f
                val y = baseline - (point.medianAdgGPerDay / maxValue).toFloat() * baseline
                val here = Offset(x, y.coerceIn(0f, baseline))
                prev?.let {
                    drawLine(color = MeshaColors.Brand, start = it, end = here, strokeWidth = 5f)
                }
                drawCircle(color = MeshaColors.Brand, radius = 7f, center = here)
                prev = here
            }
            drawRect(
                color = MeshaColors.Hair,
                topLeft = Offset(0f, baseline - 1f),
                size = Size(size.width, 1f),
            )
        }
        Spacer(Modifier.height(6.dp))
        // Labels as real Text so they translate and scale, never baked into the canvas.
        Row(modifier = Modifier.fillMaxWidth()) {
            points.forEach { point ->
                Text(
                    text = point.label,
                    style = MeshaType.caption,
                    color = MeshaColors.Muted,
                    textAlign = TextAlign.Center,
                    maxLines = 1,
                    modifier = Modifier.weight(1f),
                )
            }
        }
        Text(
            text = stringResource(R.string.weighing_growth_trend_caption),
            style = MeshaType.caption,
            color = MeshaColors.Muted,
        )
    }
}

@Composable
private fun ShedRow(
    shed: WeighingGrowthShedUi,
    all: List<WeighingGrowthShedUi>,
    herdMedian: Double?,
) {
    // ONE shared scale across every bar. Per-row scaling would make a weak shed look identical to
    // a strong one, which is the opposite of a ranking.
    val widest = all.mapNotNull { it.medianAdgGPerDay }.maxOfOrNull { kotlin.math.abs(it) } ?: 1.0
    val adg = shed.medianAdgGPerDay
    val fraction = if (adg == null || widest <= 0.0) 0f else (kotlin.math.abs(adg) / widest).toFloat()
    val unknown = stringResource(R.string.weighing_growth_unknown_value)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = shed.displayName,
                style = MeshaType.body,
                color = MeshaColors.Ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                // n is always shown: a shed of 6 animals must not silently top the ranking.
                text = stringResource(R.string.weighing_growth_shed_meta, shed.animalCount),
                style = MeshaType.caption,
                color = MeshaColors.Muted,
                maxLines = 1,
            )
        }
        if (adg != null) {
            Box(
                modifier = Modifier
                    .width((90 * fraction).dp.coerceAtLeast(4.dp))
                    .height(14.dp)
                    .clip(RoundedCornerShape(3.dp))
                    .background(growthTone(adg, herdMedian)),
            )
            Spacer(Modifier.width(8.dp))
        }
        Text(
            text = adg?.let { formatGPerDay(it) } ?: unknown,
            style = MeshaType.body,
            color = growthTone(adg, herdMedian),
            maxLines = 1,
        )
    }
}

@Composable
private fun LosingRow(animal: WeighingGrowthLosingUi) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                // The RAW SCANNED TAG. Weighing never resolves it to an animal identity.
                text = animal.scannedIdentifier,
                style = MeshaType.body,
                color = MeshaColors.Ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = stringResource(
                    R.string.weighing_growth_losing_meta_fmt,
                    animal.shedName,
                    animal.previousWeightKg,
                    animal.latestWeightKg,
                ),
                style = MeshaType.caption,
                color = MeshaColors.Muted,
                maxLines = 1,
            )
        }
        Text(
            text = formatGPerDay(animal.adgGPerDay),
            style = MeshaType.body,
            color = MeshaColors.Danger,
            maxLines = 1,
        )
    }
}

@Composable
private fun LumpSumRow(shed: WeighingGrowthLumpSumUi) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = shed.displayName,
                style = MeshaType.body,
                color = MeshaColors.Ink,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = stringResource(
                    R.string.weighing_growth_lumpsum_meta,
                    shed.headCount,
                    shed.weekLabel,
                ),
                style = MeshaType.caption,
                color = MeshaColors.Muted,
                maxLines = 1,
            )
        }
        Text(
            // The SHED average, never presented as an individual animal's weight.
            text = shed.averageWeightKg?.let {
                stringResource(R.string.weighing_growth_lumpsum_avg, it)
            } ?: stringResource(R.string.weighing_growth_unknown_value),
            style = MeshaType.body,
            color = MeshaColors.Ink,
            maxLines = 1,
        )
    }
}

/**
 * Colour means GROWTH, measured against the HERD, and never verification state.
 *
 * Three tones, because "positive" alone is not a judgement a reader can act on: every shed can be
 * positive while half of them drag the herd down. Compared to the herd median, a shed is either
 * pulling its weight or it is not, and that is the sentence a Director needs.
 *
 * With no herd median (nothing computable yet) the bar stays neutral rather than claiming a rank.
 */
private fun growthTone(adg: Double?, herdMedian: Double?) = when {
    adg == null -> MeshaColors.Muted
    adg < 0 -> MeshaColors.Danger
    herdMedian == null -> MeshaColors.Brand
    adg >= herdMedian -> MeshaColors.Brand
    else -> MeshaColors.Warn
}

private fun formatGPerDay(value: Double): String {
    val rounded = kotlin.math.round(value).toInt()
    return "$rounded g/day"
}

private fun formatDelta(value: Double): String {
    val rounded = kotlin.math.round(value).toInt()
    return if (rounded >= 0) "+$rounded vs prev" else "$rounded vs prev"
}

private fun formatPercent(value: Double): String = "${kotlin.math.round(value).toInt()}%"
