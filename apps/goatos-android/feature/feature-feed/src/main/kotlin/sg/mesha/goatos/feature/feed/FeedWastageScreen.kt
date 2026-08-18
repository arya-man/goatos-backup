package sg.mesha.goatos.feature.feed

// telemetry:exempt pure stateless renderer; FeedWastageViewModel (in :app) owns the feed_wastage_*
// AnalyticsEvents + CrashReporter wiring for every read refresh and filter change.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import java.time.LocalDate

// ---------------------------------------------------------------------------
// UI models (feature-local; mapped from DTOs by the :app @HiltViewModel).
// ---------------------------------------------------------------------------

/**
 * One PEN-DAY wastage line — the leftover-feed video an EXPERIMENT pen owes for one feed day
 * (maintainer decision 2026-08-18).
 *
 * The grain is the PEN-DAY: unlike packing there is deliberately NO session (wastage is what is
 * LEFT OVER after the day's feeding, measured once) and every row is an experiment pen. One pen,
 * one feed day, one video, one recorded value.
 */
@Immutable
data class FeedWastageRowUi(
    /** Stable list key carrying the FULL identity: shed, pen AND feed day — never the shed alone. */
    val listKey: String,
    val parkId: String,
    val parkLabel: String,
    val shedId: String,
    val shedLabel: String,
    /** The PEN, "" for an undivided shed. Carried separately from the composed [shedLabel]
     *  because the completion needs the raw pen, not the display string. */
    val partitionLabel: String,
    val experimentArm: String,
    /** Context, never a gate or a quantity. */
    val headCount: Long,
    val completed: Boolean,
    /** Verification-lifecycle bucket: "pending" | "pending_verification" | "completed" (empty =
     *  pending). Drives the 3-state status chip. */
    val lifecycleStatus: String,
    /** Why this pen came back to the operator, blank unless it is in rework. Backend-composed
     *  farm copy; rendered verbatim — the chip alone reads "pending" and cannot say why. */
    val reworkReason: String = "",
    /** The verifier's recorded leftover weight ("3.5"), blank until she has recorded one. "0" is
     *  a real measurement — an empty trough — and renders like any other value. */
    val wastageKg: String = "",
)

@Immutable
data class FeedWastageSummaryUi(
    val totalPens: Int = 0,
    val pendingPens: Int = 0,
    val inReviewPens: Int = 0,
    val completedPens: Int = 0,
)

@Immutable
data class FeedWastageUiState(
    val title: String,
    // The FEED day shown in the date bar (Asia/Kolkata). Wastage is measured on the feed day
    // itself — what is left over after that day's feeding — so unlike packing there is no +1 axis.
    val targetDateLabel: String = "",
    // Today's business date (Asia/Kolkata) — the bound the date bar's next-day arrow and DatePicker
    // clamp to, computed once by the ViewModel so the feature module never re-derives "today".
    val today: String = "",
    // Inclusive lower bound (ISO) for the date bar: the ~30-day history floor. Blank = no floor.
    val minDate: String = "",
    // True only when targetDateLabel == today: a past feed day is VIEW ONLY, so rows must not open
    // the capture flow while this is false.
    val canCapture: Boolean = true,
    // Wastage filters are farm + status only: the grain is the pen-DAY (no session) and every row
    // is an experiment pen (no workflow), so those two dropdowns are deliberately absent here.
    val filters: FeedFilterUi = FeedFilterUi(),
    val summary: FeedWastageSummaryUi = FeedWastageSummaryUi(),
    val hasSummary: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface FeedWastageEvent {
    data object Refresh : FeedWastageEvent
    data class SelectPark(val parkId: String) : FeedWastageEvent

    /** The verification-lifecycle bucket to filter to; "" = every status. */
    data class SelectStatus(val status: String) : FeedWastageEvent

    /** The feed day to view; never applied by the ViewModel when it is in the future. */
    data class SelectDate(val date: LocalDate) : FeedWastageEvent

    /** Tap a pen row to open its wastage capture detail. */
    data class OpenRow(
        val parkId: String,
        val parkLabel: String,
        /** The row's backend-owned lifecycle bucket, so the capture screen knows the pen-day is
         *  already submitted without re-reading it. */
        val lifecycleStatus: String,
        val shedId: String,
        val shedLabel: String,
        val partitionLabel: String,
        /** The pen's authored trial group — display context for the capture screen's subtitle. */
        val experimentArm: String,
    ) : FeedWastageEvent
    data object ClearFilters : FeedWastageEvent
}

@Composable
fun FeedWastageScreen(
    state: FeedWastageUiState,
    rows: LazyPagingItems<FeedWastageRowUi>,
    onEvent: (FeedWastageEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Refresh-on-open (docs/decisions/android-offline-first.md): cached Room rows show instantly
    // and a background refresh fires on every resume, including when the operator pops back here
    // after submitting a pen's wastage video, so it shows as pending verification.
    RefreshOnResume { onEvent(FeedWastageEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        FeedHeader(
            title = state.title,
            subtitle = state.targetDateLabel,
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.hasSummary,
            isOffline = state.isOffline,
            onRefresh = { onEvent(FeedWastageEvent.Refresh) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "date_bar") {
                FeedDateBar(
                    selectedDate = state.targetDateLabel,
                    today = state.today,
                    onSelectDate = { onEvent(FeedWastageEvent.SelectDate(it)) },
                    minDate = state.minDate.ifBlank { null },
                )
            }
            if (!state.canCapture) {
                item(key = "read_only_banner") {
                    FeedReadOnlyBanner(modifier = Modifier.padding(horizontal = 16.dp))
                }
            }
            item(key = "filters") { FeedWastageFilterBar(state.filters, onEvent) }
            item(key = "summary") { FeedWastageSummaryCard(state.summary) }
            item(key = "caption") { FeedSectionCaption(stringResource(R.string.feed_wastage_caption)) }

            if (rows.itemCount == 0 && state.emptyMessage != null) {
                item(key = "empty") {
                    EmptyState(
                        title = state.emptyMessage,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Feed,
                        tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                    )
                }
            }

            items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                rows[index]?.let { row ->
                    // Past-day rows are VIEW ONLY: `canCapture = false` disables the card's
                    // clickable modifier so a tap never dispatches OpenRow for a non-today day.
                    //
                    // An ALREADY-SUBMITTED row stays tappable on purpose (same rationale as the
                    // packing list): the detail screen shows the submitted state instead of an
                    // empty capture form.
                    FeedWastageRowCard(row, canCapture = state.canCapture) {
                        onEvent(
                            FeedWastageEvent.OpenRow(
                                parkId = row.parkId,
                                parkLabel = row.parkLabel,
                                lifecycleStatus = row.lifecycleStatus,
                                shedId = row.shedId,
                                shedLabel = row.shedLabel,
                                partitionLabel = row.partitionLabel,
                                experimentArm = row.experimentArm,
                            ),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun FeedWastageFilterBar(filters: FeedFilterUi, onEvent: (FeedWastageEvent) -> Unit) {
    val hasActive = filters.status.isNotBlank()
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = stringResource(R.string.feed_filters_title),
                color = MeshaColors.Muted,
                style = MeshaType.pillStrong,
                modifier = Modifier.weight(1f),
            )
            if (hasActive) {
                Text(
                    text = stringResource(R.string.feed_filters_clear),
                    color = MeshaColors.BrandD,
                    style = MeshaType.pillStrong,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onEvent(FeedWastageEvent.ClearFilters) }
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            FeedDropdownField(
                label = stringResource(R.string.feed_filter_farm),
                selectedLabel = filters.selectedParkLabel,
                placeholder = stringResource(R.string.feed_filter_farm),
                options = filters.parks,
                onSelect = { onEvent(FeedWastageEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
            FeedStatusDropdown(
                selectedStatus = filters.status,
                onSelect = { onEvent(FeedWastageEvent.SelectStatus(it)) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun FeedWastageSummaryCard(summary: FeedWastageSummaryUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = stringResource(R.string.feed_wastage_summary_title),
            color = MeshaColors.Muted,
            style = MeshaType.pillStrong,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
            FeedStatTile(
                label = stringResource(R.string.feed_wastage_stat_pens),
                value = summary.totalPens.toString(),
                accent = MeshaColors.BrandD,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_status_pending),
                value = summary.pendingPens.toString(),
                accent = if (summary.pendingPens > 0) MeshaColors.Warn else MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_status_awaiting_chip),
                value = summary.inReviewPens.toString(),
                accent = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_status_completed),
                value = summary.completedPens.toString(),
                accent = MeshaColors.Teal,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun FeedWastageRowCard(row: FeedWastageRowUi, canCapture: Boolean, onOpen: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            // Disabled means Compose never fires onOpen on tap — a past day's row card is inert.
            .clickable(enabled = canCapture, onClick = onOpen)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            // The backend-composed shed + pen display, rendered verbatim.
            Text(text = row.shedLabel, color = MeshaColors.Ink, style = MeshaType.cardTitle, modifier = Modifier.weight(1f))
            FeedLifecycleChip(status = row.lifecycleStatus, completedLabel = stringResource(R.string.feed_completed_badge))
        }
        // The pen's authored trial group plus its head count — context for the person filming, so
        // they know which trial the leftover belongs to.
        val subtitle = buildList {
            if (row.experimentArm.isNotBlank()) add(row.experimentArm)
            if (row.headCount > 0) add(stringResource(R.string.feed_head_count_fmt, row.headCount))
        }.joinToString(" · ")
        if (subtitle.isNotBlank()) {
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.caption)
        }

        // Why this pen is back. Without it the card is indistinguishable from one nobody filmed.
        if (row.reworkReason.isNotBlank()) {
            Text(
                text = row.reworkReason,
                color = MeshaColors.Danger,
                style = MeshaType.caption,
                modifier = Modifier.fillMaxWidth(),
            )
        }

        // The verifier's recorded leftover weight, once she has read one off the video. "0 kg" is
        // a real measurement (an empty trough) and renders exactly like any other value.
        if (row.wastageKg.isNotBlank()) {
            Text(
                text = stringResource(R.string.feed_wastage_recorded_fmt, row.wastageKg),
                color = MeshaColors.BrandD,
                style = MeshaType.pillStrong,
            )
        }
    }
}
