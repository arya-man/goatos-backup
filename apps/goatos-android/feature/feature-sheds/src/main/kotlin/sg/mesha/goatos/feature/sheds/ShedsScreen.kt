package sg.mesha.goatos.feature.sheds

// telemetry:exempt pure stateless renderer; CalendarViewModel owns drive-open analytics and ShedsViewModel owns non-fatal refresh reporting.

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
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
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
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.partitionDisplayLabel
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import sg.mesha.goatos.feature.sheds.R

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
/**
 * SENT_BACK is its own state, NOT a flavour of DELAYED. A shed the verifier returned is not
 * late -- it is finished work that must be done again -- and labelling it "Overdue" told the
 * operator the wrong thing about why it is on his list.
 */
enum class ShedStatus { DONE, PENDING, DELAYED, SENT_BACK }

enum class ShedStatusTone { OK, WARN, DANGER, INFO }

enum class ShedStatusChipKey { DONE, IN_PROGRESS, IN_REVIEW, SUBMITTED, OPEN, OVERDUE, SENT_BACK, COMPLETE }

@Immutable
data class ShedStatusChip(
    val key: ShedStatusChipKey,
    val tone: ShedStatusTone,
)

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

@Immutable
data class ShedDayTab(
    val dateKey: String,
    val dayLabel: String,
    val dateLabel: String,
    val countLabel: String,
    val isSelected: Boolean,
)

@Immutable
data class ShedParkFilter(
    val parkId: String,
    val label: String,
    val isSelected: Boolean,
)

@Immutable
data class ProtocolAdherenceSummary(
    val expectedCount: Int,
    val submittedCount: Int,
    val acceptedCount: Int,
    val reviewItemCount: Int,
    val overdueItemCount: Int = 0,
    /**
     * Animals the verifier SENT BACK. Its own number because the card previously showed only
     * submitted and accepted, so a rejection hid in the gap between them -- a reader could not
     * tell "still with the verifier" from "came back and must be redone".
     */
    val sentBackCount: Int = 0,
    val deferredCount: Int,
    val acceptedPercent: Int,
) {
    val progressFraction: Float =
        if (expectedCount > 0) submittedCount.toFloat() / expectedCount else 0f
}

/**
 * One shed card. Domain values come from the backend payload while localized labels
 * and count captions are app chrome;
 * [actionLabel] is null when the backend returned no action for this principal
 * (e.g. leadership gets no Start/scan) — the absence of the field, not a client
 * `role ==` check, is what hides the affordance.
 */
// @Immutable: vaccineGroups: List<VaccineGroup> otherwise marks this unstable — ShedRow is
// passed directly as a composable parameter per shed card (item 6, perf/stability pass).
@Immutable
data class ShedRow(
    val id: String,
    val name: String,
    val parkId: String = "",
    val parkName: String = "",
    val operatorName: String = "",
    val physicalShed: String = "",
    val partition: String = "",
    val partitionLabel: String? = null,
    val animalStage: String,
    val scheduleDateKey: String = "",
    val scheduleDateLabel: String = "",
    val status: ShedStatus,
    val statusLabel: String,
    val statusChips: List<ShedStatusChip> = emptyList(),
    val vaccineGroups: List<VaccineGroup>,
    val inShed: String,
    val due: String,
    val done: String,
    // The operator's own submitted count (`done`) and the verifier's accepted count are
    // deliberately separate fields: a card that reads "5 DONE" while only 2 have cleared
    // verification is misleading if only one number is shown. Backend-owned (acceptedCount on
    // VaccinationExecutionRowDto); the client never derives this.
    val accepted: String = "0",
    val progressLabel: String,
    val progressFraction: Float,
    val actionLabel: String? = null,
    val shedId: String = id,
    val driveId: String? = null,
    val batchId: String? = null,
    val taskId: String? = null,
    val sopVersionId: String? = null,
    val taskRowVersion: Int? = null,
    val opensRecordOnly: Boolean = false,
    val canOpen: Boolean = true,
)

@Immutable
private data class ShedParkGroup(
    val parkId: String,
    val label: String,
    val rows: List<ShedRow>,
) {
    val targetCount: Int = rows.sumOf { it.inShed.toIntOrNull() ?: 0 }
    val doneCount: Int = rows.sumOf { it.done.toIntOrNull() ?: 0 }
    val openCount: Int = rows.sumOf { it.due.toIntOrNull() ?: 0 }
}

/** Full screen state. Header fields + the shed list + optional roster/kernel context.
 *  @Immutable: rows/rosterChanges List<T> fields otherwise mark this unstable (item 6,
 *  perf/stability pass). */
@Immutable
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
    // Counts are UI chrome, not backend-owned copy: the ViewModel supplies the raw
    // numbers and the screen formats/localizes them via *_fmt string resources.
    // Default 0 = "no counts yet" (loading/empty/error) so those states render no chips.
    val shedCount: Int = 0,
    val dueCount: Int = 0,
    val doneCount: Int = 0,
    val caption: String? = null,
    val roleNote: String? = null,
    val dayTabs: List<ShedDayTab> = emptyList(),
    val parkFilters: List<ShedParkFilter> = emptyList(),
    val adherence: ProtocolAdherenceSummary? = null,
    val rows: List<ShedRow> = emptyList(),
    val hostedFromCalendar: Boolean = false,
    val leadershipMode: Boolean = false,
    val selectedParkId: String? = null,
    // Whether tapping a shed may open it into the operator scan/execute loop. Backend-owned:
    // false for a leadership oversight read (read-only shed list; the open click is blocked so
    // CEO/Director/Park Head never reach the scan screen). Defaults true so operators are
    // unaffected. See VaccinationExecutionResponseDto.viewerReadOnly.
    val canOpenShed: Boolean = true,
    // Backend-owned "vaccines to carry" for the selected day. Rendered verbatim; the screen
    // NEVER sums shed rows to derive these (that produced a partial-page total, e.g. 111 vs 200).
    val carry: DayCarry? = null,
    val rosterChanges: List<RosterChange> = emptyList(),
    val kernelInfo: String? = null,
    // Offline-first sync state (docs/decisions/android-offline-first.md), rendered by
    // sg.mesha.goatos.core.ui.SyncStatusIndicator. [isRefreshing]/[lastSyncedAt]/[isOffline]
    // describe the background network refresh over the ALREADY-RENDERED Room cache above —
    // they never gate whether the rest of this state renders.
    val isRefreshing: Boolean = false,
    val isInitialLoading: Boolean = false,
    val isLoadingMore: Boolean = false,
    val hasMore: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
    // True once a fetch has COMPLETED, whatever it returned. Guards the skeleton so it is never
    // drawn before an answer exists. lastSyncedAt cannot serve this — an empty result never sets
    // it — and isRefreshing flips on every later refresh, so both made the screen wedge on a
    // spinner or yank already-drawn content away mid-refresh.
    val hasLoadedOnce: Boolean = false,
)

sealed interface ShedsEvent {
    data class OpenShedRecord(val shedId: String) : ShedsEvent
    data class SelectDay(val dateKey: String) : ShedsEvent
    data class SelectPark(val parkId: String?) : ShedsEvent
    data object Refresh : ShedsEvent
    data object LoadMore : ShedsEvent
    data object Back : ShedsEvent

    /** A tap on a shed card was blocked before navigation (permission, ownership, schedule, or
     *  already-submitted gate). Sent to the ViewModel purely for telemetry — the nav host still
     *  owns the Toast and the gate logic itself. [reason] is a coarse, non-PII cause. */
    data class OpenBlocked(val reason: String, val shedId: String?) : ShedsEvent
}

// ---------------------------------------------------------------------------
// Tokens (ported from docs/mobile/design-system.md — dark is the default field theme)
// ---------------------------------------------------------------------------

private val PageBg = MeshaColors.PageBg
private val Surf = MeshaColors.Surf
private val Surf2 = MeshaColors.Surf2
private val Surf3 = MeshaColors.Surf3
private val Hair = MeshaColors.Hair
private val Ink = MeshaColors.Ink
private val Muted = MeshaColors.Muted
private val Faint = MeshaColors.Faint
private val Brand = MeshaColors.Brand
private val BrandD = MeshaColors.BrandD
private val Warn = MeshaColors.Warn
private val Danger = MeshaColors.Danger
private val OkBg = MeshaColors.OkX
private val WarnBg = MeshaColors.WarnX
private val DangerBg = MeshaColors.DangerX
private val Info = MeshaColors.Info
private val InfoBg = MeshaColors.InfoX
private val BrandTint = MeshaColors.BrandTint
private val ProgressFill = MeshaColors.BrandGradientHorizontal

private data class StatusTone(val fg: Color, val bg: Color, val edge: Color)

/** delayed = RED (error) is the load-bearing rule from screens.md + the role-drill memory. */
private fun toneFor(status: ShedStatus): StatusTone = when (status) {
    ShedStatus.DONE -> StatusTone(fg = BrandD, bg = OkBg, edge = Brand)
    ShedStatus.PENDING -> StatusTone(fg = Warn, bg = WarnBg, edge = Warn)
    ShedStatus.DELAYED -> StatusTone(fg = Danger, bg = DangerBg, edge = Danger)
    ShedStatus.SENT_BACK -> StatusTone(fg = Danger, bg = DangerBg, edge = Danger)
}

private fun toneFor(tone: ShedStatusTone): StatusTone = when (tone) {
    ShedStatusTone.OK -> StatusTone(fg = BrandD, bg = OkBg, edge = Brand)
    ShedStatusTone.WARN -> StatusTone(fg = Warn, bg = WarnBg, edge = Warn)
    ShedStatusTone.DANGER -> StatusTone(fg = Danger, bg = DangerBg, edge = Danger)
    ShedStatusTone.INFO -> StatusTone(fg = Info, bg = InfoBg, edge = Info)
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
    showProtocolAdherenceCard: Boolean = false,
) {
    RefreshOnResume { onEvent(ShedsEvent.Refresh) }
    val listState = rememberLazyListState()
    // [hostedFromCalendar] and "a park is pinned" are ORTHOGONAL and used to be conflated here:
    // hostedFromCalendar means "this screen is embedded in a calendar flow" (governs chrome —
    // day tabs, protocol adherence card, park-group headers below) while whether the filter is
    // reachable/needed depends only on whether more than one park is actually a choice. A
    // calendar-hosted screen scoped to a single park via the drive card's parkId nav arg still
    // needs a way to widen back to all parks; the old `!state.hostedFromCalendar` gate hid the
    // filter (and the header's filter icon, see ShedsHeader) permanently in that case, with no
    // escape short of navigating back out.
    val canFilterHere = state.parkFilters.size > 1
    val isParkPinned = state.hostedFromCalendar && state.selectedParkId != null
    val pinnedParkLabel = state.parkFilters.firstOrNull { it.parkId == state.selectedParkId }?.label
    val parkGroups = state.parkGroups()
    // Park scope is chosen ONCE, by the filter pills above, which select through the ViewModel and
    // therefore drive the query and its cursor. There used to be a second park selector here -- the
    // per-park summary cards -- which picked a park purely client-side out of already-loaded rows.
    // Two controls for one scope disagreed on screen (the pills reading "All parks" while the cards
    // had CBE selected and the list showed only CBE), and the client-side one silently fought
    // pagination by hiding rows the cursor had already paid for.
    var showParkFilters by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(listState, state.hasMore, state.isLoadingMore, state.rows.size) {
        if (!state.hasMore || state.isLoadingMore || state.rows.isEmpty()) return@LaunchedEffect
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }
            .collect { lastVisibleIndex ->
                val firstShedIndex = if (state.dayTabs.isNotEmpty()) 2 else 2
                val lastShedIndex = firstShedIndex + state.rows.lastIndex
                if (lastVisibleIndex >= lastShedIndex - 3 && state.hasMore && !state.isLoadingMore) {
                    onEvent(ShedsEvent.LoadMore)
                }
            }
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(PageBg),
    ) {
        ShedsHeader(
            state = state,
            onRefresh = { onEvent(ShedsEvent.Refresh) },
            onBack = { onEvent(ShedsEvent.Back) },
            onOpenFilters = { showParkFilters = true },
        )
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.dayTabs.isNotEmpty()) {
                // Leadership reaches this screen from a specific drive/date on the Calendar, so
                // the day strip is redundant for them — show it only for the operator work queue
                // (canOpenShed). VaccineCarryCard stays (it renders nothing without carry data).
                if (!state.leadershipMode) {
                    item { DayTabs(state.dayTabs, onSelect = { onEvent(ShedsEvent.SelectDay(it)) }) }
                }
                item { VaccineCarryCard(carry = state.carry) }
            } else {
                item { DriveMeta(state) }
                item { DayProgress(state) }
            }
            // A calendar drill-in pinned to one park is otherwise silent about WHY the list
            // shows only that park — this makes the scope visible and clearable in one tap,
            // going through the same SelectPark(null) flow the filter pills use (so it drives
            // the query/cursor, not a client-side hide).
            if (isParkPinned && pinnedParkLabel != null) {
                item(key = "pinned-park-chip") {
                    PinnedParkChip(
                        label = pinnedParkLabel,
                        onClear = { onEvent(ShedsEvent.SelectPark(null)) },
                    )
                }
            }
            if (canFilterHere && state.parkFilters.size > 1) {
                item {
                    ParkFilters(
                        filters = state.parkFilters,
                        onSelect = { onEvent(ShedsEvent.SelectPark(it)) },
                    )
                }
            }
            if (showProtocolAdherenceCard && !state.hostedFromCalendar) {
                state.adherence?.let { adherence ->
                    item {
                        ProtocolAdherenceCard(
                            summary = adherence,
                            parkScope = state.parkFilters.firstOrNull { it.isSelected }?.label ?: "All parks",
                        )
                    }
                }
            }
            state.roleNote?.let { note -> item { RoleNote(note) } }
            // NOTHING READ YET is not the same as NOTHING TO DO. A freshly navigated screen starts
            // with an empty state flow and its refresh has not necessarily begun, so requiring
            // a skeleton here left a window where the confident "No sheds" rendered before a
            // single row had been read -- then the real rows landed a frame later. That swap is
            // the flicker seen on every screen entered from the calendar. Until this list has
            // synced once, the honest render is neither skeleton nor empty state: nothing at all.
            // Loading is told by the spinning refresh icon in the app bar, not by a block of
            // shimmer that flashes in and straight back out on every navigation.
            if (state.rows.isEmpty() && !state.hasLoadedOnce) {
                // NOTHING. SyncIconButton in the header shows the spinner.
            } else if (state.rows.isEmpty() && state.caption != null) {
                item {
                    EmptyState(
                        title = state.caption,
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        icon = MeshaIcons.Goat,
                        tone = EmptyTone.Neutral,
                    )
                }
            }
            if (parkGroups.size > 1 && !state.hostedFromCalendar) {
                parkGroups.forEach { group ->
                    item(key = "park-header-${group.parkId}") {
                        ParkGroupHeader(group)
                    }
                    items(group.rows, key = { it.id }) { row ->
                        ShedCard(row = row, onOpen = { onEvent(ShedsEvent.OpenShedRecord(row.id)) })
                    }
                }
            } else {
                items(state.rows, key = { it.id }) { row ->
                    ShedCard(row = row, onOpen = { onEvent(ShedsEvent.OpenShedRecord(row.id)) })
                }
            }
            if (state.isLoadingMore) {
                item(key = "loading-more") {
                    InlineLoadingFooter()
                }
            }
            if (state.rosterChanges.isNotEmpty()) {
                item { SectionCaption(stringResource(R.string.sheds_roster_changes_caption)) }
                item { ChangeCard(state.rosterChanges) }
            }
            state.kernelInfo?.let { info -> item { InfoBox(info) } }
        }
    }
    if (showParkFilters && canFilterHere) {
        ShedsParkFilterSheet(
            filters = state.parkFilters,
            onDismiss = { showParkFilters = false },
            onSelect = {
                onEvent(ShedsEvent.SelectPark(it))
                showParkFilters = false
            },
        )
    }
}

@Composable
private fun InlineLoadingFooter() {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(18.dp),
            color = Muted,
            strokeWidth = 2.dp,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ShedsParkFilterSheet(
    filters: List<ShedParkFilter>,
    onDismiss: () -> Unit,
    onSelect: (String?) -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = Surf,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 10.dp),
        ) {
            Text(
                text = "Filter park",
                color = Ink,
                // design-system:ignore: no 20sp/W800 token (screenTitle is 22sp/W700) — a 2sp
                // drop plus a weight step would visibly shrink this sheet title.
                fontSize = 20.sp,
                fontWeight = FontWeight.ExtraBold,
            )
            Spacer(Modifier.height(14.dp))
            ParkFilters(filters = filters, onSelect = onSelect)
            Spacer(Modifier.height(24.dp))
        }
    }
}

@Composable
private fun ParkFilters(filters: List<ShedParkFilter>, onSelect: (String?) -> Unit) {
    FlowRow(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        val anySelected = filters.any { it.isSelected }
        FilterPill(
            label = "All parks",
            selected = !anySelected,
            onClick = { onSelect(null) },
        )
        filters.forEach { option ->
            FilterPill(
                label = option.label,
                selected = option.isSelected,
                onClick = { onSelect(option.parkId) },
            )
        }
    }
}

/** Visible, clearable "pinned park" indicator for a calendar drill-in scoped to one park —
 *  reads as an active filter chip (not invisible state) and clears back to all parks via the
 *  same [ShedsEvent.SelectPark] flow the filter pills use. Follows the screen's existing
 *  [FilterPill] visual language (selected-pill styling) rather than a new control style. */
@Composable
private fun PinnedParkChip(label: String, onClear: () -> Unit) {
    Row(
        modifier = Modifier.padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Card(
            modifier = Modifier.height(36.dp),
            shape = RoundedCornerShape(18.dp),
            colors = CardDefaults.cardColors(containerColor = Brand),
            border = BorderStroke(1.dp, Brand),
            elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxHeight()
                    .clickable(onClick = onClear)
                    .padding(start = 13.dp, end = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = stringResource(R.string.sheds_park_pinned_fmt, label),
                    color = PageBg,
                    style = MeshaType.pillStrong,
                )
                Spacer(Modifier.width(6.dp))
                Icon(
                    imageVector = MeshaIcons.Close,
                    contentDescription = stringResource(R.string.sheds_park_pinned_clear_description),
                    tint = PageBg,
                    modifier = Modifier.size(14.dp),
                )
            }
        }
    }
}

@Composable
private fun FilterPill(label: String, selected: Boolean, onClick: () -> Unit) {
    val bg = if (selected) Brand else Surf2
    val edge = if (selected) Brand else Hair
    val fg = if (selected) PageBg else Ink
    Card(
        modifier = Modifier.height(36.dp),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.cardColors(containerColor = bg),
        border = BorderStroke(1.dp, edge),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
    ) {
        Box(
            modifier = Modifier
                .fillMaxHeight()
                .clickable(onClick = onClick)
                .padding(horizontal = 13.dp),
            contentAlignment = Alignment.Center,
        ) {
            Text(text = label, color = fg, style = MeshaType.pillStrong)
        }
    }
}

private fun ShedsUiState.parkGroups(): List<ShedParkGroup> =
    rows
        .groupBy { row -> row.parkId.ifBlank { row.parkName.ifBlank { "unknown" } } }
        .map { (parkId, parkRows) ->
            val filterLabel = parkFilters.firstOrNull { it.parkId == parkId }?.label
            ShedParkGroup(
                parkId = parkId,
                label = filterLabel
                    ?: parkRows.firstOrNull()?.parkName?.takeIf { it.isNotBlank() }
                    ?: "Unknown park",
                rows = parkRows,
            )
        }
        .sortedBy { it.label.lowercase() }

@Composable
private fun ParkGroupHeader(group: ShedParkGroup) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(start = 16.dp, end = 16.dp, top = 6.dp, bottom = 2.dp),
        verticalAlignment = Alignment.Bottom,
    ) {
        Text(
            text = group.label,
            color = Ink,
            style = MeshaType.button,
        )
    }
}

@Composable
private fun DayTabs(tabs: List<ShedDayTab>, onSelect: (String) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        tabs.take(7).forEach { tab ->
            val bg = if (tab.isSelected) Brand else Surf2
            val edge = if (tab.isSelected) Brand else Hair
            val labelColor = if (tab.isSelected) PageBg else Muted
            val dateColor = if (tab.isSelected) PageBg else Ink
            Card(
                onClick = { onSelect(tab.dateKey) },
                modifier = Modifier
                    .weight(1f)
                    .height(86.dp),
                shape = RoundedCornerShape(14.dp),
                colors = CardDefaults.cardColors(containerColor = bg),
                elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
                border = BorderStroke(1.dp, edge),
            ) {
                Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(vertical = 12.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.SpaceBetween,
                ) {
                    Text(text = tab.dayLabel, color = labelColor, style = MeshaType.pill)
                    // design-system:ignore: no 20sp token (screenTitle 22sp/W700, dayNumber 15sp/W800);
                    // this day-tab number would change size noticeably either way.
                    Text(text = tab.dateLabel, color = dateColor, fontSize = 20.sp, fontWeight = FontWeight.ExtraBold)
                }
            }
        }
    }
}

/** Backend-owned "vaccines to carry" for the selected day. The screen renders these numbers
 *  verbatim — it never sums shed rows (that produced a partial-page total, 111 vs 200). */
data class CarryVaccine(val label: String, val remaining: Int)
data class DayCarry(val totalRemaining: Int, val vaccines: List<CarryVaccine>)

@Composable
private fun VaccineCarryCard(carry: DayCarry?) {
    if (carry == null || carry.vaccines.isEmpty()) return
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.cardColors(containerColor = Surf),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        border = BorderStroke(1.dp, Hair),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = "Vaccines to carry",
                    color = Ink,
                    style = MeshaType.cardTitle,
                )
                Spacer(Modifier.weight(1f))
                Text(
                    text = "${carry.totalRemaining} doses",
                    color = BrandD,
                    style = MeshaType.listTitle,
                )
            }
            Spacer(Modifier.height(4.dp))
            Text(
                text = "Selected day · all sheds below",
                color = Muted,
                style = MeshaType.caption,
            )
            Spacer(Modifier.height(12.dp))
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                carry.vaccines.forEach { v ->
                    VaccineChip(VaccineGroup(label = v.label, countLabel = "${v.remaining} doses"))
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

/**
 * Sheds header — the clearest case for letting the SHELL own the leading affordance.
 *
 * This one composable is hosted at two routes on purpose (see [AppNavHost]): `/vaccination`, a
 * backend nav_item and therefore an L0 root, and `/calendar/drive`, a hosted L1 child pushed from
 * Calendar. It used to render an unconditional Back chevron, which meant the Vaccination module's
 * own root tab showed Up instead of the module drawer.
 *
 * [MeshaScreenHeader] resolves it from the shell's exact L0 membership instead: drawer at
 * `/vaccination`, Up at `/calendar/drive` — with no route check anywhere in this feature module.
 */
@Composable
private fun ShedsHeader(
    state: ShedsUiState,
    onRefresh: () -> Unit,
    onBack: () -> Unit,
    onOpenFilters: () -> Unit,
) {
    val headerTitle = if (!state.canOpenShed && !state.hostedFromCalendar) {
        "Overview"
    } else {
        stringResource(R.string.sheds_title)
    }
    MeshaScreenHeader(
        // Static screen title — localized client-side (the VM always bakes the
        // English "Today's sheds" chrome string; ignore it, render the screen's own).
        title = headerTitle,
        eyebrow = listOf(state.moduleLabel, state.scopeLabel)
            .filter { it.isNotBlank() }
            .joinToString(" · "),
        eyebrowColor = BrandD,
        onBack = onBack,
        below = {
            // Offline-first sync/stale affordance (docs/decisions/android-offline-first.md):
            // renders nothing while there is no cache yet — a cold-start/error placeholder
            // above already covers that moment — otherwise "Syncing…" / "Updated Xm ago" /
            // "Offline · updated Xm ago", NEVER a second loading wall over live content.
            SyncStatusIndicator(
                isRefreshing = state.isRefreshing,
                lastSyncedAt = state.lastSyncedAt,
                // [ShedsUiState.lastSyncedAt] is only ever non-null once a Room cache row
                // has been observed (set from Resource.lastSyncedAt in the ViewModel), so it
                // doubles as the "do we have anything cached to annotate" signal.
                hasData = state.lastSyncedAt != null,
                isOffline = state.isOffline,
                modifier = Modifier.padding(top = 4.dp),
            )
        },
        actions = {
            // Reachability depends only on whether there is more than one park to choose
            // between — NOT on whether this screen is calendar-hosted (see the canFilterHere
            // comment in ShedsScreen for the full rationale). A calendar drill-in pinned to one
            // park still needs this to widen back to all parks.
            if (state.parkFilters.size > 1) {
                ShedsHeaderIconButton(
                    onClick = onOpenFilters,
                    icon = MeshaIcons.Filter,
                    contentDescription = "Filter park",
                )
                Spacer(Modifier.size(8.dp))
            }
            SyncIconButton(
                isSyncing = state.isRefreshing,
                onSync = onRefresh,
                contentDescription = stringResource(R.string.sheds_refresh_description),
            )
        },
    )
}

@Composable
private fun ShedsHeaderIconButton(
    onClick: () -> Unit,
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    contentDescription: String,
) {
    Box(
        modifier = Modifier
            .size(54.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(Surf)
            .border(1.dp, Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = contentDescription,
            tint = Muted,
            modifier = Modifier.size(24.dp),
        )
    }
}

@Composable
private fun DriveMeta(state: ShedsUiState) {
    // Only render segments the backend actually populated — a blank field must not
    // leave an orphaned "·" separator (loading/empty/error states clear these).
    val shedCountText = if (state.shedCount > 0) stringResource(R.string.sheds_count_fmt, state.shedCount) else null
    val dueText = if (state.dueCount > 0) stringResource(R.string.sheds_due_fmt, state.dueCount) else null
    val parts = listOfNotNull(
        state.date.takeIf { it.isNotBlank() }?.let { it to true },
        state.window.takeIf { it.isNotBlank() }?.let { it to false },
        shedCountText?.let { it to true },
        dueText?.let { it to true },
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
    Text(text = text, color = Ink, style = MeshaType.caption)
}

@Composable
private fun MetaMuted(text: String) {
    Text(text = text, color = Muted, style = MeshaType.caption)
}

@Composable
private fun Dot() {
    // design-system:ignore: this separator is 11.5sp at the default W400; the only 11.5sp
    // token (caption) is W600, which would visibly embolden the "·".
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
            // design-system:ignore: 13sp/W600 has no near token (listTitle is 13.5sp/W700,
            // cta 12.5sp/W700) — both shift size and weight at once.
            Text(text = stringResource(R.string.sheds_day_progress), color = Ink, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.weight(1f))
            Text(text = state.dayProgressLabel, color = BrandD, style = MeshaType.bodyStrong)
        }
        Spacer(Modifier.height(8.dp))
        ProgressBar(state.dayProgressFraction)
        Spacer(Modifier.height(7.dp))
        // Live counts localize via the *_fmt resource; fall back to any VM-supplied
        // summary string when raw counts aren't present (placeholder/sample states).
        val summary = if (state.dueCount > 0) {
            // dueCount is the still-open count, not the denominator. Showing done/open
            // as done/total produced impossible copy such as "4 / 4 done" beside 50%.
            stringResource(R.string.sheds_day_summary_fmt, state.doneCount, state.doneCount + state.dueCount)
        } else {
            state.daySummary
        }
        // design-system:ignore: 10.5sp at the default W400; the only 10.5sp token (overline)
        // is W700 with 0.42sp tracking, which would restyle this summary line.
        Text(text = summary, color = Muted, fontSize = 10.5f.sp)
    }
}

@Composable
private fun ProtocolAdherenceCard(summary: ProtocolAdherenceSummary, parkScope: String) {
    val stateChip = when {
        summary.acceptedCount >= summary.expectedCount && summary.expectedCount > 0 ->
            ShedStatusChip(ShedStatusChipKey.COMPLETE, ShedStatusTone.OK)
        summary.reviewItemCount > 0 ->
            ShedStatusChip(ShedStatusChipKey.IN_REVIEW, ShedStatusTone.INFO)
        summary.submittedCount > 0 ->
            ShedStatusChip(ShedStatusChipKey.SUBMITTED, ShedStatusTone.WARN)
        else -> ShedStatusChip(ShedStatusChipKey.OPEN, ShedStatusTone.WARN)
    }
    val statusChips = listOfNotNull(
        stateChip,
        ShedStatusChip(ShedStatusChipKey.OVERDUE, ShedStatusTone.DANGER).takeIf { summary.overdueItemCount > 0 },
    )
    val progressLabel = "${summary.submittedCount}/${summary.expectedCount} goats submitted"
    val progressCaption = when {
        summary.acceptedCount >= summary.expectedCount && summary.expectedCount > 0 -> "${summary.acceptedPercent}% accepted"
        summary.reviewItemCount > 0 -> "Submitted"
        else -> "Submitted"
    }
    val reviewLine = when (summary.reviewItemCount) {
        0 -> null
        1 -> "1 shed video awaiting review"
        else -> "${summary.reviewItemCount} shed videos awaiting review"
    }
    val acceptedLine = "${summary.acceptedCount}/${summary.expectedCount} goats accepted so far"
    // Hidden at zero on purpose: a permanent "0 sent back" is dead text on every healthy day.
    val sentBackLine = when (summary.sentBackCount) {
        0 -> null
        1 -> "1 goat sent back to redo"
        else -> "${summary.sentBackCount} goats sent back to redo"
    }
    val tone = when {
        summary.acceptedCount >= summary.expectedCount && summary.expectedCount > 0 -> toneFor(ShedStatus.DONE)
        summary.reviewItemCount > 0 || summary.submittedCount > 0 -> toneFor(ShedStatus.PENDING)
        else -> toneFor(ShedStatus.DELAYED)
    }
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = Surf),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        border = BorderStroke(1.dp, Hair),
    ) {
        Column(modifier = Modifier.padding(15.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "Protocol adherence",
                        color = Ink,
                        style = MeshaType.cardTitle,
                    )
                    Text(
                        text = parkScope,
                        color = Muted,
                        style = MeshaType.caption,
                    )
                }
                FlowRow(
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalArrangement = Arrangement.spacedBy(5.dp),
                ) {
                    statusChips.forEach { chip ->
                        StatusPill(label = chip.label(), tone = toneFor(chip.tone))
                    }
                }
            }
            Spacer(Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.Bottom) {
                Text(
                    text = progressLabel,
                    // design-system:ignore: no 18sp token (screenTitle 22sp, headerTitle 16.5sp) —
                    // this headline number would jump size either way.
                    color = Ink,
                    fontSize = 18.sp,
                    fontWeight = FontWeight.ExtraBold,
                )
                Spacer(Modifier.weight(1f))
                Text(
                    text = progressCaption,
                    color = BrandD,
                    style = MeshaType.bodyStrong,
                )
            }
            Spacer(Modifier.height(10.dp))
            ProgressBar(summary.progressFraction)
            Spacer(Modifier.height(10.dp))
            FlowRow(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                reviewLine?.let { CompactFact(it, color = tone.fg) }
                // Danger tone, and shown before the accepted line: work that came back is the
                // thing a reader must act on, not a footnote under the good news.
                sentBackLine?.let { CompactFact(it, color = toneFor(ShedStatus.SENT_BACK).fg) }
                CompactFact(acceptedLine, color = toneFor(ShedStatus.DONE).fg)
            }
        }
    }
}

@Composable
private fun CompactFact(text: String, color: Color) {
    Text(
        text = text,
        color = color,
        style = MeshaType.caption,
        modifier = Modifier
            .clip(RoundedCornerShape(12.dp))
            .background(Surf2)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
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
        // design-system:ignore: 8sp bullet glyph — smallest token is dayName at 9.5sp/W700.
        Text(text = "●", color = Muted, fontSize = 8.sp)
        // design-system:ignore: 11.5sp at default W400; caption (the only 11.5sp token) is W600.
        Text(text = note, color = Muted, fontSize = 11.5f.sp)
    }
}

@Composable
private fun SectionCaption(text: String) {
    Text(
        text = text,
        color = Faint,
        style = MeshaType.pill,
        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 4.dp),
    )
}

// ---------------------------------------------------------------------------
// Shed card
// ---------------------------------------------------------------------------

@Composable
private fun ShedCard(row: ShedRow, onOpen: () -> Unit) {
    val tone = toneFor(row.status)
    // The whole card is the tap target; the redundant "Open shed ›" CTA text is not rendered.
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clickable(enabled = row.canOpen, onClick = onOpen),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.cardColors(containerColor = Surf),
        elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
        border = BorderStroke(1.dp, Hair),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            ShedCardTop(row = row, tone = tone)
            DriveAssignmentStrip(row)
            if (row.vaccineGroups.isNotEmpty()) {
                Spacer(Modifier.height(10.dp))
                VaccineChips(row.vaccineGroups)
            }
            Spacer(Modifier.height(14.dp))
            NumsRow(row)
            Spacer(Modifier.height(12.dp))
            ProgressBar(row.progressFraction)
        }
    }
}

@Composable
private fun DriveAssignmentStrip(row: ShedRow) {
    // Resolved here, not inside the lambda: partitionDisplayLabel is a plain function, so a
    // @Composable stringResource call cannot happen in the formatter it invokes.
    val partitionFmt = stringResource(R.string.sheds_partition_fmt)
    val parts = listOfNotNull(
        row.scheduleDateLabel.takeIf { it.isNotBlank() },
        row.operatorName.takeIf { it.isNotBlank() },
        row.physicalShed.takeIf { it.isNotBlank() },
        // A whole-shed drive shows the shed name only; a partitioned one adds the partition once.
        // Never "<shed> Part whole" or "<shed> Part Parts 1-3" — see [partitionDisplayLabel].
        partitionDisplayLabel(row.partition) { partitionFmt.format(it) },
    )
    if (parts.isEmpty()) return
    Spacer(Modifier.height(10.dp))
    FlowRow(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(Surf2)
            .border(1.dp, Hair, RoundedCornerShape(10.dp))
            .padding(horizontal = 10.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        parts.forEach { label ->
            Text(
                text = label,
                color = Muted,
                // design-system:ignore: the only 10.5sp token (overline) adds W700 + 0.42sp
                // tracking, which would restyle this dense meta strip.
                fontSize = 10.5f.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
            )
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
            Text(text = row.name, color = Ink, style = MeshaType.cardTitle, maxLines = 1)
            row.scheduleDateLabel.takeIf { it.isNotBlank() }?.let {
                // design-system:ignore: 12sp at default W400; cardSubtitle is 12sp/W500.
                Text(text = it, color = Muted, fontSize = 12.sp, maxLines = 1)
            }
            row.animalStage.takeIf { it.isNotBlank() }?.let {
                // design-system:ignore: 11.5sp at default W400; caption is 11.5sp/W600.
                Text(text = it, color = Muted, fontSize = 11.5f.sp, maxLines = 1)
            }
        }
        Spacer(Modifier.width(8.dp))
        FlowRow(
            horizontalArrangement = Arrangement.spacedBy(6.dp),
            verticalArrangement = Arrangement.spacedBy(5.dp),
        ) {
            val chips = row.statusChips.ifEmpty {
                listOf(ShedStatusChip(row.status.toChipKey(), row.status.toChipTone()))
            }
            chips.forEach { chip ->
                StatusPill(label = chip.label(), tone = toneFor(chip.tone))
            }
        }
    }
}

@Composable
private fun ShedStatusChip.label(): String = when (key) {
    ShedStatusChipKey.DONE -> stringResource(R.string.sheds_status_done)
    ShedStatusChipKey.IN_PROGRESS -> stringResource(R.string.sheds_status_in_progress)
    ShedStatusChipKey.IN_REVIEW -> stringResource(R.string.sheds_status_in_review)
    ShedStatusChipKey.SUBMITTED -> stringResource(R.string.sheds_status_submitted)
    ShedStatusChipKey.OPEN -> stringResource(R.string.sheds_status_open)
    ShedStatusChipKey.OVERDUE -> stringResource(R.string.sheds_status_overdue)
    ShedStatusChipKey.SENT_BACK -> stringResource(R.string.sheds_status_sent_back)
    ShedStatusChipKey.COMPLETE -> stringResource(R.string.sheds_status_complete)
}

private fun ShedStatus.toChipKey(): ShedStatusChipKey = when (this) {
    ShedStatus.DONE -> ShedStatusChipKey.DONE
    ShedStatus.PENDING -> ShedStatusChipKey.IN_PROGRESS
    ShedStatus.DELAYED -> ShedStatusChipKey.OVERDUE
    ShedStatus.SENT_BACK -> ShedStatusChipKey.SENT_BACK
}

private fun ShedStatus.toChipTone(): ShedStatusTone = when (this) {
    ShedStatus.DONE -> ShedStatusTone.OK
    ShedStatus.PENDING -> ShedStatusTone.WARN
    ShedStatus.DELAYED -> ShedStatusTone.DANGER
    ShedStatus.SENT_BACK -> ShedStatusTone.DANGER
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
        Text(text = label, color = tone.fg, style = MeshaType.pill, maxLines = 1)
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
            style = MeshaType.pill,
            maxLines = 1,
        )
        Text(
            text = group.countLabel,
            color = if (group.full) Muted else BrandD,
            style = MeshaType.pill,
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
        NumCell(value = row.inShed, label = stringResource(R.string.sheds_num_cell_in_shed), modifier = Modifier.weight(1f))
        NumDivider()
        NumCell(value = row.due, label = stringResource(R.string.sheds_num_cell_due), modifier = Modifier.weight(1f))
        NumDivider()
        NumCell(value = row.done, label = stringResource(R.string.sheds_num_cell_done), modifier = Modifier.weight(1f))
        NumDivider()
        // Distinct from `done` (the operator's own submitted work): this is the verifier's
        // accepted count, so a card can read "5 DONE / 2 ACCEPTED" instead of a single number
        // that silently conflates "I finished" with "it cleared review".
        NumCell(value = row.accepted, label = stringResource(R.string.sheds_num_cell_accepted), modifier = Modifier.weight(1f))
    }
}

@Composable
private fun NumCell(value: String, label: String, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.padding(vertical = 9.dp, horizontal = 4.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        // design-system:ignore: no 16sp token (headerTitle is 16.5sp/W700 with -0.3sp tracking,
        // button 15sp/W800) — this stat number should not gain tracking or drop a full sp.
        Text(text = value, color = Ink, fontSize = 16.sp, fontWeight = FontWeight.ExtraBold)
        Text(text = label, color = Muted, style = MeshaType.dayName)
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
            // design-system:ignore: no 10sp token (dayName 9.5sp/W700, overline 10.5sp/W700+tracking).
            Text(text = change.tag, color = fg, fontSize = 10.sp, fontWeight = FontWeight.SemiBold, maxLines = 1)
        }
        // design-system:ignore: 12.5sp at default W400; the only 12.5sp token (cta) is W700.
        Text(text = change.text, color = Muted, fontSize = 12.5f.sp, modifier = Modifier.weight(1f))
    }
}

@Composable
private fun InfoBox(text: String) {
    Text(
        text = text,
        color = Muted,
        // design-system:ignore: 12sp at default W400; cardSubtitle (12sp) is W500.
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
    shedCount = 4,
    dueCount = 77,
    doneCount = 46,
    dayProgressLabel = "46 / 77",
    dayProgressFraction = 46f / 77f,
    daySummary = "Gandhi 1 · Castro 1 · Mandela 1 · Sumathi 1",
    caption = "Tap a shed for its live status · red = delayed, chase the team",
    roleNote = "Read-only · the ground team runs the drive",
    adherence = ProtocolAdherenceSummary(
        expectedCount = 77,
        submittedCount = 77,
        acceptedCount = 46,
        reviewItemCount = 4,
        deferredCount = 0,
        acceptedPercent = 60,
    ),
    rows = listOf(
        ShedRow(
            id = "mandela1",
            name = "Mandela 1 · CBE",
            animalStage = "K2 kids · 71 in shed",
            status = ShedStatus.DONE,
            statusLabel = "Done",
            vaccineGroups = listOf(VaccineGroup("FMD + HS", "40/40", full = true)),
            inShed = "71",
            due = "0",
            done = "40",
            progressLabel = "40/40 done",
            progressFraction = 1f,
            actionLabel = "View completed record ›",
        ),
        ShedRow(
            id = "castro1",
            name = "Castro 1 · CBE",
            animalStage = "Breeding does · 44 in shed",
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
            animalStage = "Pregnant does · 30 in shed",
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
            animalStage = "Yearling does · 38 in shed",
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
