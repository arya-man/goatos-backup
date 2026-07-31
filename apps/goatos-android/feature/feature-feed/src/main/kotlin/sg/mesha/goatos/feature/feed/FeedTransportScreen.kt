package sg.mesha.goatos.feature.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.time.LocalDate
import kotlinx.coroutines.flow.distinctUntilChanged
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume

@Immutable
data class FeedTransportRowUi(
    val taskId: String,
    val parkId: String,
    val shedId: String,
    val shedLabel: String,
    val parkLabel: String,
    val status: String,
    val reworkReason: String?,
)

@Immutable
data class FeedTransportUiState(
    val date: String = "",
    val today: String = "",
    val canCapture: Boolean = true,
    val filters: FeedTransportFilterUi = FeedTransportFilterUi(),
    val rows: List<FeedTransportRowUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val hasMore: Boolean = false,
    val isLoadingMore: Boolean = false,
)

@Immutable
data class FeedTransportFilterUi(
    val parks: List<FeedDropdownOption> = emptyList(),
    val selectedParkId: String = "",
    val selectedParkLabel: String? = null,
    val sheds: List<FeedDropdownOption> = emptyList(),
    val selectedShedId: String = "",
    val selectedShedLabel: String? = null,
    val status: String = "",
) {
    val isShedFilterEnabled: Boolean get() = selectedParkId.isNotBlank() && sheds.isNotEmpty()
}

sealed interface FeedTransportEvent {
    data object Refresh : FeedTransportEvent
    data object LoadMore : FeedTransportEvent
    data class SelectDate(val date: LocalDate) : FeedTransportEvent
    data class SelectPark(val parkId: String) : FeedTransportEvent
    data class SelectShed(val shedId: String) : FeedTransportEvent
    data class SelectStatus(val status: String) : FeedTransportEvent
    data object ClearFilters : FeedTransportEvent
    data class Open(val row: FeedTransportRowUi) : FeedTransportEvent
}

@Composable
fun FeedTransportScreen(
    state: FeedTransportUiState,
    onEvent: (FeedTransportEvent) -> Unit,
) {
    RefreshOnResume { onEvent(FeedTransportEvent.Refresh) }
    val listState = rememberLazyListState()

    LaunchedEffect(listState, state.hasMore, state.isLoadingMore, state.rows.size) {
        if (!state.hasMore || state.rows.isEmpty()) return@LaunchedEffect
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: -1 }
            .distinctUntilChanged()
            .collect { lastVisibleIndex ->
                if (
                    lastVisibleIndex >= state.rows.lastIndex - 2 &&
                    state.hasMore &&
                    !state.isLoadingMore
                ) {
                    onEvent(FeedTransportEvent.LoadMore)
                }
            }
    }

    Column(modifier = Modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        FeedHeader(
            title = stringResource(R.string.feed_transport_title),
            subtitle = if (state.date == state.today) {
                stringResource(R.string.feed_transport_today, state.date)
            } else {
                stringResource(R.string.feed_transport_date, state.date)
            },
            isRefreshing = state.isRefreshing,
            lastSyncedAt = null,
            hasData = state.rows.isNotEmpty(),
            isOffline = state.isOffline,
            onRefresh = { onEvent(FeedTransportEvent.Refresh) },
        )
        LazyColumn(
            state = listState,
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(bottom = 20.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "feed-transport-date") {
                FeedDateBar(
                    selectedDate = state.date,
                    today = state.today,
                    onSelectDate = { onEvent(FeedTransportEvent.SelectDate(it)) },
                )
            }
            if (!state.canCapture) {
                item(key = "feed-transport-read-only") {
                    FeedReadOnlyBanner(modifier = Modifier.padding(horizontal = 16.dp))
                }
            }
            item(key = "feed-transport-filters") {
                FeedTransportFilterBar(state.filters, onEvent)
            }
            if (state.rows.isEmpty() && !state.isRefreshing) {
                item(key = "feed-transport-empty") {
                    EmptyState(
                        title = stringResource(R.string.feed_transport_empty),
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        tone = EmptyTone.Neutral,
                    )
                }
            }
            items(state.rows, key = { it.taskId }) { row ->
                FeedTransportRowCard(row = row, canCapture = state.canCapture) {
                    onEvent(FeedTransportEvent.Open(row))
                }
            }
            if (state.isLoadingMore) {
                item(key = "feed-transport-loading") {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(16.dp),
                        horizontalArrangement = Arrangement.Center,
                    ) {
                        CircularProgressIndicator(
                            modifier = Modifier.size(20.dp),
                            strokeWidth = 2.dp,
                            color = MeshaColors.Muted,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun FeedTransportFilterBar(
    filters: FeedTransportFilterUi,
    onEvent: (FeedTransportEvent) -> Unit,
) {
    val allFarms = stringResource(R.string.feed_filter_all_farms)
    val allSheds = stringResource(R.string.feed_filter_all_sheds)
    val hasActive = filters.selectedParkId.isNotBlank() || filters.selectedShedId.isNotBlank() ||
        filters.status.isNotBlank()

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
                fontSize = 12.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            if (hasActive) {
                Text(
                    text = stringResource(R.string.feed_filters_clear),
                    color = MeshaColors.BrandD,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.W700,
                    modifier = Modifier
                        .clip(RoundedCornerShape(8.dp))
                        .clickable { onEvent(FeedTransportEvent.ClearFilters) }
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
        }
        Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            FeedDropdownField(
                label = stringResource(R.string.feed_filter_farm),
                selectedLabel = filters.selectedParkLabel,
                placeholder = allFarms,
                options = listOf(FeedDropdownOption("", allFarms)) + filters.parks,
                onSelect = { onEvent(FeedTransportEvent.SelectPark(it)) },
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
                options = listOf(FeedDropdownOption("", allSheds)) + filters.sheds,
                onSelect = { onEvent(FeedTransportEvent.SelectShed(it)) },
                enabled = filters.isShedFilterEnabled,
                modifier = Modifier.weight(1f),
            )
        }
        FeedTransportStatusDropdown(
            selectedStatus = filters.status,
            onSelect = { onEvent(FeedTransportEvent.SelectStatus(it)) },
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun FeedTransportStatusDropdown(
    selectedStatus: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val allStatuses = stringResource(R.string.feed_filter_all_statuses)
    val options = listOf(
        FeedDropdownOption("", allStatuses),
        FeedDropdownOption("due", stringResource(R.string.feed_status_pending)),
        FeedDropdownOption("verification_due", stringResource(R.string.feed_status_awaiting)),
        FeedDropdownOption("rework", stringResource(R.string.feed_status_rework)),
        FeedDropdownOption("completed", stringResource(R.string.feed_status_completed)),
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

@Composable
private fun FeedTransportRowCard(row: FeedTransportRowUi, canCapture: Boolean, onOpen: () -> Unit) {
    val actionable = canCapture && (row.status == "due" || row.status == "rework")
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
            .clickable(enabled = actionable, onClick = onOpen)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = row.shedLabel,
                color = MeshaColors.Ink,
                fontSize = 15.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.weight(1f),
            )
            FeedLifecycleChip(
                status = when (row.status) {
                    "completed" -> FeedStatus.COMPLETED
                    "verification_due" -> FeedStatus.AWAITING
                    else -> FeedStatus.PENDING
                },
                completedLabel = stringResource(R.string.feed_status_completed),
            )
        }
        Text(text = row.parkLabel, color = MeshaColors.Muted, fontSize = 12.sp)
        row.reworkReason?.takeIf { it.isNotBlank() }?.let { reason ->
            Text(
                text = stringResource(R.string.feed_transport_rework, reason),
                color = MeshaColors.Danger,
                fontSize = 12.sp,
            )
        }
    }
}

enum class FeedTransportSubmitStatus { QUEUED, SYNCED, FAILED }

@Immutable
data class FeedTransportResultUi(val status: FeedTransportSubmitStatus, val message: String)

@Immutable
data class FeedTransportCaptureUiState(
    val shedLabel: String = "",
    val isCapturing: Boolean = false,
    val videoCaptured: Boolean = false,
    val videoMessage: String? = null,
    val result: FeedTransportResultUi? = null,
) {
    val submitEnabled: Boolean
        get() = videoCaptured && !isCapturing &&
            result?.status != FeedTransportSubmitStatus.SYNCED &&
            result?.status != FeedTransportSubmitStatus.QUEUED
}

sealed interface FeedTransportCaptureEvent {
    data object RecordVideo : FeedTransportCaptureEvent

    /** Replace the recorded clip; the ViewModel drops the discarded take's queued upload. */
    data object ReRecordVideo : FeedTransportCaptureEvent
    data object Submit : FeedTransportCaptureEvent
    data object Back : FeedTransportCaptureEvent
}

@Composable
fun FeedTransportCaptureScreen(
    state: FeedTransportCaptureUiState,
    onEvent: (FeedTransportCaptureEvent) -> Unit = {},
) {
    val committed = state.result?.status == FeedTransportSubmitStatus.SYNCED ||
        state.result?.status == FeedTransportSubmitStatus.QUEUED
    FeedCaptureScaffold(
        title = state.shedLabel,
        subtitle = null,
        instruction = stringResource(R.string.feed_transport_caption),
        onBack = { onEvent(FeedTransportCaptureEvent.Back) },
    ) {
        FeedProofCard(title = stringResource(R.string.feed_transport_video_title)) {
            when {
                state.videoCaptured -> FeedVerificationCaptured(
                    label = stringResource(R.string.feed_transport_video_recorded),
                    reRecordLabel = stringResource(R.string.feed_proof_rerecord),
                    onReRecord = { onEvent(FeedTransportCaptureEvent.ReRecordVideo) },
                    enabled = !committed,
                )
                state.isCapturing -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_transport_video_uploading),
                    enabled = false,
                    primary = false,
                    loading = true,
                    onClick = {},
                )
                else -> FeedVerificationActionButton(
                    label = stringResource(R.string.feed_transport_record_video),
                    enabled = !committed,
                    primary = false,
                    onClick = { onEvent(FeedTransportCaptureEvent.RecordVideo) },
                )
            }
            if (state.result == null) {
                state.videoMessage?.let { Text(text = it, color = MeshaColors.Muted, fontSize = 12.sp) }
            }
        }

        FeedVerificationActionButton(
            label = stringResource(R.string.feed_transport_submit),
            enabled = state.submitEnabled,
            primary = true,
            onClick = { onEvent(FeedTransportCaptureEvent.Submit) },
        )
        if (!state.submitEnabled && !committed && !state.videoCaptured) {
            Text(
                text = stringResource(R.string.feed_transport_need_video),
                color = MeshaColors.Faint,
                fontSize = 12.sp,
            )
        }

        state.result?.let { result ->
            Text(
                text = result.message,
                color = if (result.status == FeedTransportSubmitStatus.FAILED) MeshaColors.Danger else MeshaColors.Ok,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
        }
    }
}
