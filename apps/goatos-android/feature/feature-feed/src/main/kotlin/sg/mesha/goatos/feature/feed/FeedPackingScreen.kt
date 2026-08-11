package sg.mesha.goatos.feature.feed

// telemetry:exempt pure stateless renderer; FeedPackingViewModel (in :app) owns the feed_*
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
 * One PEN-SESSION packing line — the bag a packer fills and films.
 *
 * ONE CARD PER SESSION, ONE VIDEO EACH (maintainer decision 2026-08-11, reverting the 2026-08-10
 * pen-day card). A pen's morning and evening shares are separate bags weighed out at separate times,
 * so each gets its own card, its own capture and its own verification item. One clip cannot prove two
 * bags.
 */
@Immutable
data class FeedPackingRowUi(
    val grainKey: String,
    val parkId: String,
    val shedId: String,
    val sessionNo: Int,
    val shedLabel: String,
    /** The PEN, "" for an undivided shed. Carried separately from the composed [shedLabel]
     *  because the completion needs the raw pen, not the display string. */
    val partitionLabel: String,
    val sessionLabel: String,
    val workflow: String,
    val experimentArm: String,
    /** The pen's animals — the denominator this session's ration came from, the same on its sibling
     *  session. Never summed across a pen's sessions. */
    val headCount: Long,
    val items: List<FeedItemQtyUi>,
    val totalKg: String,
    /** "ready" | "blocked" | "empty" */
    val status: String,
    val completed: Boolean,
    /** Verification-lifecycle bucket: "pending" | "pending_verification" | "completed" (empty =
     *  pending). Orthogonal to [status]; drives the 3-state status chip. */
    val lifecycleStatus: String,
    /**
     * Why this line came back to the packer, blank unless it is in rework.
     *
     * A reworked line reads as "pending" in [lifecycleStatus] — the operator's bucket for "needs my
     * action" — so the chip alone cannot distinguish a bag nobody has packed from one whose video
     * was thrown away. This sentence is the only thing that says which, and whether the reason was a
     * rejected video or an afternoon feed correction that moved the quantities. A correction reopens
     * BOTH of a pen's sessions, so both cards carry it. Backend-composed; rendered verbatim.
     */
    val reworkReason: String = "",
)

@Immutable
data class FeedPackingSummaryUi(
    val shedCount: Int = 0,
    val lineCount: Int = 0,
    val blockedLineCount: Int = 0,
    val totalsByItem: List<FeedItemTotalUi> = emptyList(),
)

@Immutable
data class FeedPackingUiState(
    val title: String,
    // The PACKING day shown in the date bar (Asia/Kolkata). A packer works today on the sheet fed
    // tomorrow, so this axis is the packing day and [feedForDateLabel] states the feed day it is for.
    val targetDateLabel: String = "",
    // The FEED day (= packing day + 1), shown in the "This feed is for …" caption. The backend is
    // asked for THIS day; the packing-day axis is display only.
    val feedForDateLabel: String = "",
    // Today's business date (Asia/Kolkata) — the bound the date bar's next-day arrow and DatePicker
    // clamp to, computed once by the ViewModel so the feature module never re-derives "today" itself.
    val today: String = "",
    // Inclusive lower bound (ISO) for the date bar: the ~30-day history floor. Blank = no floor.
    val minDate: String = "",
    // True only when targetDateLabel == today: a past packing day is VIEW ONLY, so rows must not open
    // the capture flow while this is false.
    val canCapture: Boolean = true,
    // Packing filters are farm + workflow only — the worklist endpoint has no shed filter (a packer
    // draws the whole park's bags), so the shed dropdown is deliberately absent here.
    val filters: FeedFilterUi = FeedFilterUi(),
    val summary: FeedPackingSummaryUi = FeedPackingSummaryUi(),
    val hasSummary: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface FeedPackingEvent {
    data object Refresh : FeedPackingEvent
    data class SelectPark(val parkId: String) : FeedPackingEvent

    /** "" (both), "normal", or "experiment". */
    data class SelectWorkflow(val workflow: String) : FeedPackingEvent

    /** The session_no to filter to; 0 = every session. */
    data class SelectSession(val sessionNo: Int) : FeedPackingEvent

    /** The verification-lifecycle bucket to filter to; "" = every status. */
    data class SelectStatus(val status: String) : FeedPackingEvent

    /** The feed day to view; never applied by the ViewModel when it is in the future. */
    data class SelectDate(val date: LocalDate) : FeedPackingEvent

    /** Tap a packing line to open its shed-session completion detail. */
    data class OpenRow(
        val parkId: String,
        /** The row's backend-owned lifecycle bucket, so the capture screen knows the session is
         *  already submitted without re-reading it. */
        val lifecycleStatus: String,
        val shedId: String,
        val sessionNo: Int,
        val workflow: String,
        val shedLabel: String,
        val partitionLabel: String,
        val sessionLabel: String,
    ) : FeedPackingEvent
    data object ClearFilters : FeedPackingEvent
}

@Composable
fun FeedPackingScreen(
    state: FeedPackingUiState,
    rows: LazyPagingItems<FeedPackingRowUi>,
    onEvent: (FeedPackingEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Refresh-on-open (docs/decisions/android-offline-first.md): cached Room rows show instantly
    // and a background refresh fires on every resume, including when the operator pops back here
    // after submitting a feed-packing session, so it shows as pending verification.
    RefreshOnResume { onEvent(FeedPackingEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        FeedHeader(
            title = state.title,
            subtitle = state.targetDateLabel,
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.hasSummary,
            isOffline = state.isOffline,
            onRefresh = { onEvent(FeedPackingEvent.Refresh) },
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
                    onSelectDate = { onEvent(FeedPackingEvent.SelectDate(it)) },
                    minDate = state.minDate.ifBlank { null },
                )
            }
            // "This feed is for <feed day>" — the packing-day axis maps to feed day = packing day + 1,
            // so the operator reads "packed today, for tomorrow" without doing the arithmetic.
            item(key = "feed_for") {
                FeedSectionCaption(
                    stringResource(R.string.feed_packing_for_next_day, formatFeedDayLabel(state.feedForDateLabel)),
                )
            }
            if (!state.canCapture) {
                item(key = "read_only_banner") {
                    FeedReadOnlyBanner(modifier = Modifier.padding(horizontal = 16.dp))
                }
            }
            item(key = "filters") { FeedPackingFilterBar(state.filters, onEvent) }
            item(key = "summary") { FeedPackingSummaryCard(state.summary) }
            item(key = "caption") { FeedSectionCaption(stringResource(R.string.feed_packing_caption)) }

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

            items(count = rows.itemCount, key = rows.itemKey { it.grainKey }) { index ->
                rows[index]?.let { row ->
                    // Past-day rows are VIEW ONLY: `canCapture = false` disables the card's
                    // clickable modifier below, so a tap never reaches this lambda and OpenRow is
                    // never dispatched for a non-today day.
                    //
                    // An ALREADY-SUBMITTED row stays tappable on purpose. Blocking it made the card
                    // dead: an operator who taps a session showing "in review" wants to see what he
                    // sent, and a row that does nothing reads as a broken app. The refusal belongs
                    // one level in — the detail screen receives the row's lifecycle bucket and
                    // shows the submitted state instead of an empty capture form.
                    FeedPackingRowCard(row, canCapture = state.canCapture) {
                        onEvent(
                            FeedPackingEvent.OpenRow(
                                parkId = row.parkId,
                                lifecycleStatus = row.lifecycleStatus,
                                shedId = row.shedId,
                                sessionNo = row.sessionNo,
                                workflow = row.workflow,
                                shedLabel = row.shedLabel,
                                partitionLabel = row.partitionLabel,
                                sessionLabel = row.sessionLabel,
                            ),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun FeedPackingFilterBar(filters: FeedFilterUi, onEvent: (FeedPackingEvent) -> Unit) {
    val bothWorkflows = stringResource(R.string.feed_filter_workflow_all)
    val allSessions = stringResource(R.string.feed_filter_all_sessions)
    val normalLabel = stringResource(R.string.feed_workflow_normal)
    val experimentLabel = stringResource(R.string.feed_workflow_experiment)
    val hasActive = filters.workflow.isNotBlank() || filters.selectedSessionNo != 0 || filters.status.isNotBlank()

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
                        .clickable { onEvent(FeedPackingEvent.ClearFilters) }
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
                onSelect = { onEvent(FeedPackingEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
            FeedDropdownField(
                label = stringResource(R.string.feed_filter_workflow),
                selectedLabel = when (filters.workflow) {
                    "normal" -> normalLabel
                    "experiment" -> experimentLabel
                    else -> null
                },
                placeholder = bothWorkflows,
                options = listOf(
                    FeedDropdownOption(key = "", label = bothWorkflows),
                    FeedDropdownOption(key = "normal", label = normalLabel),
                    FeedDropdownOption(key = "experiment", label = experimentLabel),
                ),
                onSelect = { onEvent(FeedPackingEvent.SelectWorkflow(it)) },
                enabled = true,
                modifier = Modifier.weight(1f),
            )
            FeedSessionDropdown(
                filters = filters,
                allSessions = allSessions,
                onSelect = { onEvent(FeedPackingEvent.SelectSession(it)) },
                modifier = Modifier.weight(1f),
            )
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            FeedStatusDropdown(
                selectedStatus = filters.status,
                onSelect = { onEvent(FeedPackingEvent.SelectStatus(it)) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun FeedPackingSummaryCard(summary: FeedPackingSummaryUi) {
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
            text = stringResource(R.string.feed_totals_title),
            color = MeshaColors.Muted,
            style = MeshaType.pillStrong,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
            FeedStatTile(
                label = stringResource(R.string.feed_stat_sheds),
                value = summary.shedCount.toString(),
                accent = MeshaColors.BrandD,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_stat_lines),
                value = summary.lineCount.toString(),
                accent = MeshaColors.Ink,
                modifier = Modifier.weight(1f),
            )
            FeedStatTile(
                label = stringResource(R.string.feed_stat_blocked),
                value = summary.blockedLineCount.toString(),
                accent = if (summary.blockedLineCount > 0) MeshaColors.Warn else MeshaColors.Teal,
                modifier = Modifier.weight(1f),
            )
        }
        summary.totalsByItem.filter { it.quantityKg.toKgOrZero() != 0.0 || it.blockedCells > 0 }.forEach { total ->
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(text = total.feedItem, color = MeshaColors.Muted, style = MeshaType.caption, modifier = Modifier.weight(1f))
                Text(
                    text = stringResource(R.string.feed_kg_fmt, total.quantityKg),
                    color = MeshaColors.Ink,
                    style = MeshaType.pillStrong,
                )
            }
        }
    }
}

@Composable
private fun FeedPackingRowCard(row: FeedPackingRowUi, canCapture: Boolean, onOpen: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            // Disabled here means Compose never fires onOpen on tap — the same suppression the
            // caller comments on above; a past day's row card is inert, not just visually dimmed.
            .clickable(enabled = canCapture, onClick = onOpen)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(text = row.shedLabel, color = MeshaColors.Ink, style = MeshaType.cardTitle, modifier = Modifier.weight(1f))
            // ONE chip for this card, because there is one video and one verdict per session's bag.
            FeedLifecycleChip(status = row.lifecycleStatus, completedLabel = stringResource(R.string.feed_completed_badge))
            FeedWorkflowChip(row.workflow)
        }
        // The authored session name ("Morning"), backend-owned copy rendered verbatim — never a
        // client-composed "Session 1", which tells a packer nothing about which share of the day the
        // bag is for.
        val subtitle = buildList {
            add(row.sessionLabel)
            if (row.experimentArm.isNotBlank()) add(row.experimentArm)
        }.joinToString(" · ")
        Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.caption)

        // Why this bag is back. It sits ABOVE the quantities on purpose: the packer has already packed
        // it once today, so the first thing they need is that the numbers below are not the numbers
        // they packed to. Without it the card is indistinguishable from one they never touched, and
        // they would be shown the same bag twice with no explanation.
        if (row.reworkReason.isNotBlank()) {
            Text(
                text = row.reworkReason,
                color = MeshaColors.Danger,
                style = MeshaType.caption,
                modifier = Modifier.fillMaxWidth(),
            )
        }

        // Hide 0-kg lines (nothing to pack for that item here); a BLOCKED line is not zero and stays.
        row.items.filter { it.blocked || it.quantityKg.toKgOrZero() != 0.0 }.forEach { item -> FeedItemQtyRow(item) }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(text = stringResource(R.string.feed_pack_total), color = MeshaColors.Muted, style = MeshaType.pillStrong, modifier = Modifier.weight(1f))
            if (row.status == "blocked") {
                Text(text = stringResource(R.string.feed_blocked_label), color = MeshaColors.Danger, style = MeshaType.pillStrong)
            } else {
                Text(text = stringResource(R.string.feed_kg_fmt, row.totalKg), color = MeshaColors.BrandD, style = MeshaType.bodyStrong)
            }
        }
    }
}
