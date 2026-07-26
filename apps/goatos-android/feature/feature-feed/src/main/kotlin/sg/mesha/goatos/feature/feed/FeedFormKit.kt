package sg.mesha.goatos.feature.feed

// telemetry:exempt purely presentational form primitives with no user action of their own; the feed
// screens and their :app ViewModels own the AnalyticsEvents + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDefaults
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

// Shared building blocks for the two Feed screens (Direction + Packing). Deliberately mirrors the
// Counts module's form kit so the two verticals read and behave identically; feature modules may not
// depend on one another, so the shape is duplicated rather than imported.

/** A backend-supplied option for one of the feed filter dropdowns (farm / shed / workflow).
 *  Public because the public [FeedFilterUi] carries a `List<FeedDropdownOption>`, and a public
 *  type may not expose an internal one (explicit-api). */
@Immutable
data class FeedDropdownOption(
    val key: String,
    val label: String,
)

/** A single-choice dropdown over a backend-supplied vocabulary. Only open/closed is local state;
 *  the options, labels, and selection all come from backend-owned state passed in. */
@Composable
internal fun FeedDropdownField(
    label: String,
    selectedLabel: String?,
    placeholder: String,
    options: List<FeedDropdownOption>,
    onSelect: (String) -> Unit,
    enabled: Boolean,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(text = label, color = MeshaColors.Muted, fontSize = 12.sp)
        Box(modifier = Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(if (enabled) MeshaColors.Surf else MeshaColors.Surf3)
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                    .clickable(enabled = enabled) { expanded = true }
                    .padding(horizontal = 12.dp, vertical = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = selectedLabel ?: placeholder,
                    color = if (selectedLabel != null) MeshaColors.Ink else MeshaColors.Faint,
                    fontSize = 14.sp,
                    fontWeight = if (selectedLabel != null) FontWeight.W600 else FontWeight.W400,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    imageVector = MeshaIcons.ChevronDown,
                    contentDescription = null,
                    tint = if (enabled) MeshaColors.Muted else MeshaColors.Faint,
                    modifier = Modifier.size(16.dp),
                )
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
                modifier = Modifier.heightIn(max = 320.dp),
            ) {
                options.forEach { option ->
                    DropdownMenuItem(
                        text = {
                            Text(
                                text = option.label,
                                fontSize = 14.sp,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.widthIn(min = 180.dp),
                            )
                        },
                        onClick = {
                            expanded = false
                            onSelect(option.key)
                        },
                    )
                }
            }
        }
    }
}

/** One KPI tile (whole-scope total). */
@Composable
internal fun FeedStatTile(label: String, value: String, accent: androidx.compose.ui.graphics.Color, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf3)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(text = value, color = accent, fontSize = 18.sp, fontWeight = FontWeight.W800, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(text = label, color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W600)
    }
}

/** A section caption line above a list. */
@Composable
internal fun FeedSectionCaption(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        color = MeshaColors.Faint,
        fontSize = 11.sp,
        modifier = modifier.padding(horizontal = 16.dp),
    )
}

/**
 * Shared date-navigation bar for the two Feed read screens (Direction + Packing). Purely a UI
 * affordance over the existing `target_date` query parameter both screens already send — this never
 * introduces a new backend field. [selectedDate] is the ISO `YYYY-MM-DD` string the caller's
 * ViewModel selection already carries; [today] is the caller's business-day "today" (Asia/Kolkata),
 * so a future date can never be reached from the prev/next arrows OR the [DatePicker] itself.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun FeedDateBar(
    selectedDate: String,
    today: String,
    onSelectDate: (LocalDate) -> Unit,
    modifier: Modifier = Modifier,
) {
    val selected = remember(selectedDate) { selectedDate.toLocalDateOrNull() ?: LocalDate.now() }
    val todayDate = remember(today) { today.toLocalDateOrNull() ?: LocalDate.now() }
    val isToday = selected == todayDate
    var pickerOpen by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(horizontal = 8.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(10.dp))
                .clickable { onSelectDate(selected.minusDays(1)) },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.ChevronLeft,
                contentDescription = stringResource(R.string.feed_date_prev_description),
                tint = MeshaColors.Muted,
                modifier = Modifier.size(18.dp),
            )
        }
        Row(
            modifier = Modifier
                .weight(1f, fill = false)
                .clip(RoundedCornerShape(10.dp))
                .clickable { pickerOpen = true }
                .padding(horizontal = 10.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(
                imageVector = MeshaIcons.Calendar,
                contentDescription = null,
                tint = MeshaColors.Muted,
                modifier = Modifier.size(16.dp),
            )
            Text(
                text = selected.format(FEED_DATE_LABEL_FORMATTER),
                color = MeshaColors.Ink,
                fontSize = 14.sp,
                fontWeight = FontWeight.W700,
            )
            if (isToday) {
                Text(
                    text = stringResource(R.string.feed_date_today_chip),
                    color = MeshaColors.Ok,
                    fontSize = 10.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier
                        .clip(RoundedCornerShape(999.dp))
                        .background(MeshaColors.OkX)
                        .padding(horizontal = 8.dp, vertical = 2.dp),
                )
            }
        }
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(RoundedCornerShape(10.dp))
                .clickable(enabled = !isToday) { onSelectDate(selected.plusDays(1)) },
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = MeshaIcons.Chevron,
                contentDescription = stringResource(R.string.feed_date_next_description),
                tint = if (isToday) MeshaColors.Faint else MeshaColors.Muted,
                modifier = Modifier.size(18.dp),
            )
        }
    }

    if (pickerOpen) {
        val todayEndMillis = todayDate.toEpochMillisUtc()
        val pickerState = rememberDatePickerState(
            initialSelectedDateMillis = selected.toEpochMillisUtc(),
            yearRange = IntRange(DatePickerDefaults.YearRange.first, todayDate.year),
            selectableDates = object : SelectableDates {
                // Never selectable in the future — the same clamp the prev/next arrows apply.
                override fun isSelectableDate(utcTimeMillis: Long): Boolean = utcTimeMillis <= todayEndMillis
                override fun isSelectableYear(year: Int): Boolean = year <= todayDate.year
            },
        )
        DatePickerDialog(
            onDismissRequest = { pickerOpen = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let { millis -> onSelectDate(millis.toLocalDateUtc()) }
                    pickerOpen = false
                }) {
                    Text(stringResource(id = android.R.string.ok))
                }
            },
            dismissButton = {
                TextButton(onClick = { pickerOpen = false }) {
                    Text(stringResource(id = android.R.string.cancel))
                }
            },
        ) {
            DatePicker(state = pickerState)
        }
    }
}

/** Shown above the row list on both Feed read screens when the operator has stepped to a past day
 *  via [FeedDateBar] — that day's rows are historical record, not an executable worklist. */
@Composable
internal fun FeedReadOnlyBanner(modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf3)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = MeshaIcons.Eye,
            contentDescription = null,
            tint = MeshaColors.Muted,
            modifier = Modifier.size(16.dp),
        )
        Text(
            text = stringResource(R.string.feed_date_read_only_banner),
            color = MeshaColors.Muted,
            fontSize = 12.sp,
            fontWeight = FontWeight.W600,
        )
    }
}

private val FEED_DATE_LABEL_FORMATTER: DateTimeFormatter = DateTimeFormatter.ofPattern("EEE, d MMM")
private val FEED_ISO_DATE_FORMATTER: DateTimeFormatter = DateTimeFormatter.ISO_LOCAL_DATE

private fun String.toLocalDateOrNull(): LocalDate? = runCatching { LocalDate.parse(this, FEED_ISO_DATE_FORMATTER) }.getOrNull()

private fun LocalDate.toEpochMillisUtc(): Long = atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()

private fun Long.toLocalDateUtc(): LocalDate = Instant.ofEpochMilli(this).atZone(ZoneOffset.UTC).toLocalDate()

