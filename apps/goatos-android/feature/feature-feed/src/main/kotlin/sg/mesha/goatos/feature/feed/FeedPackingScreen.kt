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

/** One shed/session packing line — the bag a packer fills. */
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
    val headCount: Long,
    val items: List<FeedItemQtyUi>,
    val totalKg: String,
    /** "ready" | "blocked" | "empty" */
    val status: String,
    val completed: Boolean,
    /** Verification-lifecycle bucket: "pending" | "pending_verification" | "completed" (empty =
     *  pending). Orthogonal to [status]; drives the 3-state status chip. */
    val lifecycleStatus: String,
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
            FeedLifecycleChip(status = row.lifecycleStatus, completedLabel = stringResource(R.string.feed_completed_badge))
            FeedWorkflowChip(row.workflow)
        }
        val subtitle = buildList {
            add(row.sessionLabel)
            if (row.experimentArm.isNotBlank()) add(row.experimentArm)
        }.joinToString(" · ")
        Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.caption)
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
