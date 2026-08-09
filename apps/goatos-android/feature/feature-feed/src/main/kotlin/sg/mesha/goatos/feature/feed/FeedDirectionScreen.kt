package sg.mesha.goatos.feature.feed

// telemetry:exempt pure stateless renderer; FeedDirectionViewModel (in :app) owns the feed_*
// AnalyticsEvents + CrashReporter wiring for every read refresh and filter change.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
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
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import java.time.LocalDate

// ---------------------------------------------------------------------------
// UI models (feature-local; the @HiltViewModel in :app maps the DTOs onto these, so this module
// stays free of core-network per the feature-* -> core-* only rule).
// ---------------------------------------------------------------------------

/** One feed item's quantity on a row. [quantityKg] is null iff [blocked] — a blocked ration has no
 *  number and must be rendered as blocked, never as 0 (the backend's pointer-is-the-contract rule). */
@Immutable
data class FeedItemQtyUi(
    val feedItem: String,
    val quantityKg: String?,
    val blocked: Boolean,
    val blockedReason: String,
)

/** One generated feed instruction: a shed's ration in one session. */
@Immutable
data class FeedDirectionRowUi(
    val grainKey: String,
    // Completion identity (a whole shed-session is completed at once). Carried so a row tap can build
    // the completion request without re-parsing grainKey.
    val parkId: String,
    val shedId: String,
    val sessionNo: Int,
    val shedLabel: String,
    /** The PEN, "" for an undivided shed. Separate from the composed [shedLabel] because the
     *  completion needs the raw pen — a shed-only completion closes every pen at once. */
    val partitionLabel: String,
    val shedTag: String,
    val breed: String,
    val rationGroup: String,
    val experimentArm: String,
    val sessionLabel: String,
    val headCount: Long,
    val headCountInformational: Boolean,
    val workflow: String,
    val items: List<FeedItemQtyUi>,
    val sessionTotalKg: String,
    val blocked: Boolean,
    val overduePending: Boolean,
    val completed: Boolean,
    /** Verification-lifecycle bucket: "pending", "pending_verification", or "completed" (empty =
     *  pending). Drives the 3-state status chip. */
    val lifecycleStatus: String,
)

/** One feed item's whole-scope total. */
@Immutable
data class FeedItemTotalUi(val feedItem: String, val quantityKg: String, val blockedCells: Int)

@Immutable
data class FeedDirectionSummaryUi(
    val shedCount: Int = 0,
    val rowCount: Int = 0,
    val blockedCount: Int = 0,
    val totalsByItem: List<FeedItemTotalUi> = emptyList(),
)

/** A single dropdown filter's rendered state. */
@Immutable
data class FeedFilterUi(
    val parks: List<FeedDropdownOption> = emptyList(),
    val selectedParkId: String = "",
    val selectedParkLabel: String? = null,
    val sheds: List<FeedDropdownOption> = emptyList(),
    val selectedShedId: String = "",
    val selectedShedLabel: String? = null,
    /** "" (both), "normal", or "experiment". */
    val workflow: String = "",
    // Session filter (backend-owned vocabulary). Options carry the session_no as their key; "0" =
    // every session. selectedSessionNo 0 means unfiltered.
    val sessions: List<FeedDropdownOption> = emptyList(),
    val selectedSessionNo: Int = 0,
    val selectedSessionLabel: String? = null,
    /** Verification-lifecycle filter: "" (all), "pending", "pending_verification", or "completed".
     *  The dropdown resolves its label inline (stable client vocabulary), so no label field is needed. */
    val status: String = "",
) {
    val isShedFilterEnabled: Boolean get() = selectedParkId.isNotBlank() && sheds.isNotEmpty()
    val isSessionFilterEnabled: Boolean get() = sessions.isNotEmpty()
}

/** The three backend-owned verification-lifecycle buckets a feed shed-session can be filtered by.
 *  Stable client vocabulary (like the workflow filter); the backend owns the semantics via the
 *  `status` query param. Labels are resolved from string resources at render time. */
object FeedStatus {
    const val PENDING = "pending"
    const val AWAITING = "pending_verification"
    const val COMPLETED = "completed"
}

@Immutable
data class FeedDirectionUiState(
    val title: String,
    val targetDateLabel: String = "",
    // Today's business date (Asia/Kolkata) — the bound the date bar's next-day arrow and DatePicker
    // clamp to, computed once by the ViewModel so the feature module never re-derives "today" itself.
    val today: String = "",
    // True only when targetDateLabel == today: a past day is VIEW ONLY, so rows must not open the
    // capture flow while this is false.
    val canCapture: Boolean = true,
    val filters: FeedFilterUi = FeedFilterUi(),
    val summary: FeedDirectionSummaryUi = FeedDirectionSummaryUi(),
    val hasSummary: Boolean = false,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isOffline: Boolean = false,
)

sealed interface FeedDirectionEvent {
    data object Refresh : FeedDirectionEvent
    data class SelectPark(val parkId: String) : FeedDirectionEvent
    data class SelectShed(val shedId: String) : FeedDirectionEvent

    /** "" (both), "normal", or "experiment". */
    data class SelectWorkflow(val workflow: String) : FeedDirectionEvent

    /** The session_no to filter to; 0 = every session. */
    data class SelectSession(val sessionNo: Int) : FeedDirectionEvent

    /** The verification-lifecycle bucket to filter to; "" = every status. */
    data class SelectStatus(val status: String) : FeedDirectionEvent

    /** The feed day to view; never applied by the ViewModel when it is in the future. */
    data class SelectDate(val date: LocalDate) : FeedDirectionEvent

    /** Tap a row to open its shed-session completion detail. */
    data class OpenRow(
        val parkId: String,
        val shedId: String,
        val sessionNo: Int,
        val workflow: String,
        val shedLabel: String,
        val partitionLabel: String,
        val sessionLabel: String,
    ) : FeedDirectionEvent
    data object ClearFilters : FeedDirectionEvent
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun FeedDirectionScreen(
    state: FeedDirectionUiState,
    rows: LazyPagingItems<FeedDirectionRowUi>,
    onEvent: (FeedDirectionEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Refresh-on-open (docs/decisions/android-offline-first.md): cached Room rows show
    // instantly and a background refresh fires on every resume, including when the
    // operator pops back here after submitting a feed-distribution session.
    RefreshOnResume { onEvent(FeedDirectionEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        FeedHeader(
            title = state.title,
            subtitle = state.targetDateLabel,
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.hasSummary,
            isOffline = state.isOffline,
            onRefresh = { onEvent(FeedDirectionEvent.Refresh) },
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
                    onSelectDate = { onEvent(FeedDirectionEvent.SelectDate(it)) },
                )
            }
            if (!state.canCapture) {
                item(key = "read_only_banner") {
                    FeedReadOnlyBanner(modifier = Modifier.padding(horizontal = 16.dp))
                }
            }
            item(key = "filters") {
                FeedDirectionFilterBar(state.filters, onEvent)
            }
            // Feed direction shows NO ration/quantities (maintainer rule 2026-07-26): it is only the
            // operator's confirmation that a shed-session was fed. The feed itself is shown on the feed
            // PACKING screen. So there is no feed-totals summary card here.
            item(key = "caption") { FeedSectionCaption(stringResource(R.string.feed_direction_caption)) }

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
                    // Past-day rows are VIEW ONLY: `canCapture = false` disables the card's clickable
                    // modifier below, so a tap never reaches this lambda and OpenRow — hence the
                    // verifier-gated capture screen — is never dispatched for a non-today day.
                    FeedDirectionRowCard(row, canCapture = feedSessionCanCapture(row.lifecycleStatus, state.canCapture)) {
                        onEvent(
                            FeedDirectionEvent.OpenRow(
                                parkId = row.parkId,
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
internal fun FeedHeader(
    title: String,
    subtitle: String,
    isRefreshing: Boolean,
    lastSyncedAt: Long?,
    hasData: Boolean,
    isOffline: Boolean,
    onRefresh: () -> Unit,
) {
    MeshaScreenHeader(
        title = title,
        subtitle = subtitle.takeIf { it.isNotBlank() },
        below = {
            SyncStatusIndicator(
                isRefreshing = isRefreshing,
                lastSyncedAt = lastSyncedAt,
                hasData = hasData,
                isOffline = isOffline,
            )
        },
        actions = {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                    .clickable(enabled = !isRefreshing, onClick = onRefresh),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = MeshaIcons.Refresh,
                    contentDescription = stringResource(R.string.feed_refresh_description),
                    tint = MeshaColors.Muted,
                    modifier = Modifier.size(18.dp),
                )
            }
        },
    )
}

@Composable
private fun FeedDirectionFilterBar(filters: FeedFilterUi, onEvent: (FeedDirectionEvent) -> Unit) {
    val allSheds = stringResource(R.string.feed_filter_all_sheds)
    val allSessions = stringResource(R.string.feed_filter_all_sessions)
    val bothWorkflows = stringResource(R.string.feed_filter_workflow_all)
    val normalLabel = stringResource(R.string.feed_workflow_normal)
    val experimentLabel = stringResource(R.string.feed_workflow_experiment)
    val hasActive = filters.selectedShedId.isNotBlank() || filters.workflow.isNotBlank() ||
        filters.selectedSessionNo != 0 || filters.status.isNotBlank()

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
                        .clickable { onEvent(FeedDirectionEvent.ClearFilters) }
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
                onSelect = { onEvent(FeedDirectionEvent.SelectPark(it)) },
                enabled = filters.parks.isNotEmpty(),
                modifier = Modifier.weight(1f),
            )
            FeedDropdownField(
                label = stringResource(R.string.feed_filter_shed),
                selectedLabel = filters.selectedShedLabel,
                placeholder = if (filters.selectedParkId.isBlank()) {
                    stringResource(R.string.feed_filter_park_first)
                } else {
                    allSheds
                },
                options = listOf(FeedDropdownOption(key = "", label = allSheds)) + filters.sheds,
                onSelect = { onEvent(FeedDirectionEvent.SelectShed(it)) },
                enabled = filters.isShedFilterEnabled,
                modifier = Modifier.weight(1f),
            )
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
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
                onSelect = { onEvent(FeedDirectionEvent.SelectWorkflow(it)) },
                enabled = true,
                modifier = Modifier.weight(1f),
            )
            FeedSessionDropdown(
                filters = filters,
                allSessions = allSessions,
                onSelect = { onEvent(FeedDirectionEvent.SelectSession(it)) },
                modifier = Modifier.weight(1f),
            )
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            FeedStatusDropdown(
                selectedStatus = filters.status,
                onSelect = { onEvent(FeedDirectionEvent.SelectStatus(it)) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

/**
 * The session picker, shared by both feed filter bars. The option vocabulary is backend-owned
 * (`filters.sessions`), so the client never invents session numbers or labels. The key "0" is the
 * synthetic "all sessions" entry; every other option's key is the backend session_no.
 */
@Composable
internal fun FeedSessionDropdown(
    filters: FeedFilterUi,
    allSessions: String,
    onSelect: (Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    FeedDropdownField(
        label = stringResource(R.string.feed_filter_session),
        selectedLabel = filters.selectedSessionLabel,
        placeholder = allSessions,
        options = listOf(FeedDropdownOption(key = "0", label = allSessions)) + filters.sessions,
        onSelect = { onSelect(it.toIntOrNull() ?: 0) },
        enabled = filters.isSessionFilterEnabled,
        modifier = modifier,
    )
}

/**
 * The verification-lifecycle status picker, shared by both feed filter bars. Fixed client vocabulary
 * (like the workflow filter) — the backend owns the semantics via the `status` query param and
 * filters the whole scope before paging. Key "" is the synthetic "all statuses" entry.
 */
@Composable
internal fun FeedStatusDropdown(
    selectedStatus: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val allStatuses = stringResource(R.string.feed_filter_all_statuses)
    val options = listOf(
        FeedDropdownOption(key = "", label = allStatuses),
        FeedDropdownOption(key = FeedStatus.PENDING, label = stringResource(R.string.feed_status_pending)),
        FeedDropdownOption(key = FeedStatus.AWAITING, label = stringResource(R.string.feed_status_awaiting)),
        FeedDropdownOption(key = FeedStatus.COMPLETED, label = stringResource(R.string.feed_status_completed)),
    )
    FeedDropdownField(
        label = stringResource(R.string.feed_filter_status),
        selectedLabel = options.firstOrNull { it.key == selectedStatus && it.key.isNotEmpty() }?.label,
        placeholder = allStatuses,
        options = options,
        onSelect = onSelect,
        enabled = true,
        modifier = modifier,
    )
}

/** Parses a wire kg string ("0.000", "451.000") to a Double, treating null/blank/unparseable as 0. */
internal fun String?.toKgOrZero(): Double = this?.trim()?.toDoubleOrNull() ?: 0.0

@Composable
private fun FeedDirectionRowCard(row: FeedDirectionRowUi, canCapture: Boolean, onOpen: () -> Unit) {
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
            FeedDirectionStatusChip(status = row.lifecycleStatus)
            FeedWorkflowChip(row.workflow)
        }
        // Confirmation-only: shed identity + which session. NO ration/quantities/totals/head count — the
        // feed is shown on the feed PACKING screen (maintainer rule 2026-07-26). Tapping the row (today
        // only) opens the distribution video capture that confirms this shed-session was fed.
        val subtitle = buildList {
            if (row.shedTag.isNotBlank()) add(row.shedTag)
            if (row.breed.isNotBlank()) add(row.breed)
            if (row.experimentArm.isNotBlank()) add(row.experimentArm)
            add(row.sessionLabel)
        }.joinToString(" · ")
        if (subtitle.isNotBlank()) {
            Text(text = subtitle, color = MeshaColors.Muted, style = MeshaType.caption)
        }
    }
}

/** The 3-state verification-lifecycle badge for a feed shed-session, shared by both screens. The
 *  "completed" bucket reads differently per screen ("Fed" on direction, "Completed" on packing), so
 *  the caller passes [completedLabel]; the other two buckets are the same everywhere. Mirrors the
 *  backend's [FeedStatus] buckets — empty/unknown falls through to Pending. */
@Composable
internal fun FeedLifecycleChip(status: String, completedLabel: String) {
    data class Chip(val label: String, val fg: androidx.compose.ui.graphics.Color, val bg: androidx.compose.ui.graphics.Color)
    val chip = when (status) {
        FeedStatus.COMPLETED -> Chip(completedLabel, MeshaColors.Ok, MeshaColors.OkX)
        FeedStatus.AWAITING -> Chip(stringResource(R.string.feed_status_awaiting_chip), MeshaColors.Warn, MeshaColors.WarnX)
        else -> Chip(stringResource(R.string.feed_direction_pending), MeshaColors.Muted, MeshaColors.Hair)
    }
    Text(
        text = chip.label,
        color = chip.fg,
        style = MeshaType.pillStrong,
        modifier = Modifier
            .padding(end = 6.dp)
            .clip(RoundedCornerShape(999.dp))
            .background(chip.bg)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}

/** Direction's status badge: "Fed" for the verifier-approved (completed) bucket. */
@Composable
internal fun FeedDirectionStatusChip(status: String) {
    FeedLifecycleChip(status = status, completedLabel = stringResource(R.string.feed_direction_fed))
}

@Composable
internal fun FeedItemQtyRow(item: FeedItemQtyUi) {
    Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(text = item.feedItem, color = MeshaColors.Ink, style = MeshaType.body, modifier = Modifier.weight(1f))
        if (item.blocked) {
            Text(
                text = stringResource(R.string.feed_blocked_label),
                color = MeshaColors.Danger,
                style = MeshaType.pillStrong,
            )
        } else {
            Text(
                text = stringResource(R.string.feed_kg_fmt, item.quantityKg ?: "0"),
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
            )
        }
    }
    if (item.blocked && item.blockedReason.isNotBlank()) {
        Text(text = item.blockedReason, color = MeshaColors.Warn, style = MeshaType.caption)
    }
}

@Composable
internal fun FeedWorkflowChip(workflow: String) {
    val isExperiment = workflow == "experiment"
    val label = stringResource(if (isExperiment) R.string.feed_workflow_experiment else R.string.feed_workflow_normal)
    val bg = if (isExperiment) MeshaColors.TealX else MeshaColors.OkX
    val fg = if (isExperiment) MeshaColors.Teal else MeshaColors.Ok
    Text(
        text = label,
        color = fg,
        style = MeshaType.pillStrong,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 3.dp),
    )
}
