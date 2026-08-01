package sg.mesha.goatos.feature.weighing.leadership

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
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.feature.weighing.WeighingTaskConfirmSheet
import sg.mesha.goatos.feature.weighing.R

// telemetry:exempt Leadership shed detail is a read-only view of an existing shed read; the one
// write it exposes is the weighing reopen, which is tracked by that write.

/** One captured animal record, as leadership reads it. Never editable on this screen. */
data class WeighingShedRecordUiRow(
    val id: String,
    /** The tag the operator read, or blank when the animal was weighed without a readable tag. */
    val tagLabel: String,
    val hasTag: Boolean,
    val hasVideo: Boolean,
    val weightLabel: String,
)

/** One label/value line in the shed's field group. */
data class WeighingShedFieldUiRow(
    val label: String,
    val value: String,
    /** A pill-rendered value (mode, state) rather than plain text. */
    val pill: Boolean = false,
    val pillFg: Color? = null,
    val pillBg: Color? = null,
    /** A secondary, de-emphasised reading (the shed's herd estimate) rather than a hard fact. */
    val faint: Boolean = false,
)

data class WeighingShedDetailUiState(
    val shedName: String = "",
    /** "<Park> · <weigh date>", supplied by the task the reader came from. */
    val contextLabel: String = "",
    val found: Boolean = false,
    val isLumpSum: Boolean = false,
    val fields: List<WeighingShedFieldUiRow> = emptyList(),
    /**
     * The visible window of captured records. The screen shows a page at a time and grows the
     * window as the reader scrolls; [moreRecords] is what is still held back.
     */
    val records: List<WeighingShedRecordUiRow> = emptyList(),
    val moreRecords: Int = 0,
    val canReopen: Boolean = false,
    /** The state the bucket is in today, named the way the farm says it. Shown in the reopen confirm. */
    val stateLabel: String = "",
    /** The operator the bucket would go back to. Shown in the reopen confirm. */
    val operatorLabel: String = "",
    /** Set when the bucket is already back with its operator, so reopen would be a no-op. */
    val reopenBlockedReason: String = "",
    /**
     * Quiet staleness note: the last refresh did not land, so what is on screen is the last saved
     * reading. Blank when the cache is current. It never replaces the bucket with an error page.
     */
    val staleNotice: String = "",
    val loading: Boolean = false,
    val busy: Boolean = false,
    val error: String? = null,
    val message: String? = null,
)

/**
 * Leadership's read-only view of ONE shed bucket.
 *
 * A hosted destination: Up/Back, no root chrome. It deliberately carries NO scan entry point, no
 * weight entry, and no submit — capture belongs to the operator's own destination, and a planner is
 * assigned no shed. The single write here is the leadership reopen, which is the same one the task
 * detail offers.
 */
@Composable
fun WeighingShedDetailScreen(
    state: WeighingShedDetailUiState,
    onRefresh: () -> Unit = {},
    onBack: () -> Unit = {},
    onRecordRowVisible: (Int) -> Unit = {},
    onReopen: () -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onRefresh() }
    // Reopen hands the bucket back to its operator and takes it out of the verifier's hands, so it
    // is confirmed first. One stray tap must never do this.
    var confirmReopenOpen by remember { mutableStateOf(false) }
    if (confirmReopenOpen) {
        val shed = state.shedName.ifBlank { stringResource(R.string.weighing_shed_fallback_this_shed) }
        val from = state.stateLabel.takeIf { it.isNotBlank() }
            ?.let { stringResource(R.string.weighing_shed_reopen_from_fmt, it) }
            .orEmpty()
        val to = state.operatorLabel.takeIf { it.isNotBlank() }
            ?.let { stringResource(R.string.weighing_shed_reopen_to_fmt, it) }
            ?: stringResource(R.string.weighing_shed_reopen_to_operator)
        WeighingTaskConfirmSheet(
            title = stringResource(R.string.weighing_shed_reopen_title),
            body = stringResource(R.string.weighing_shed_reopen_body_fmt, shed, from, to),
            confirmLabel = stringResource(R.string.weighing_shed_reopen_confirm),
            busy = state.busy,
            onConfirm = {
                confirmReopenOpen = false
                onReopen()
            },
            onDismiss = { confirmReopenOpen = false },
            dismissLabel = stringResource(R.string.weighing_shed_reopen_dismiss),
        )
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg),
    ) {
        MeshaScreenHeader(
            title = state.shedName.ifBlank { stringResource(R.string.weighing_shed_fallback_title) },
            eyebrow = stringResource(R.string.weighing_eyebrow),
            eyebrowColor = MeshaColors.BrandD,
            subtitle = state.contextLabel.takeIf { it.isNotBlank() },
            onBack = onBack,
            actions = {
                SyncIconButton(
                    isSyncing = state.loading,
                    onSync = onRefresh,
                    contentDescription = stringResource(R.string.weighing_shed_refresh),
                )
            },
        )
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            state.message?.let { message ->
                item(key = "shed-message") { ShedBanner(text = message, fg = MeshaColors.Ok, bg = MeshaColors.OkX) }
            }
            if (state.staleNotice.isNotBlank() && state.found) {
                item(key = "shed-stale") {
                    ShedBanner(text = state.staleNotice, fg = MeshaColors.Warn, bg = MeshaColors.WarnX)
                }
            }
            if (!state.found) {
                item(key = "shed-missing") {
                    ShedEmptyCard(
                        loading = state.loading,
                        title = state.error ?: stringResource(R.string.weighing_shed_missing_title),
                    )
                }
                return@LazyColumn
            }
            item(key = "shed-fields") { ShedFieldGroup(state.fields) }
            if (!state.isLumpSum) {
                item(key = "shed-records-label") {
                    Text(
                        text = stringResource(R.string.weighing_shed_records_label),
                        color = MeshaColors.Muted,
                        style = MeshaType.sectionLabel,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
                if (state.records.isEmpty()) {
                    item(key = "shed-records-empty") {
                        ShedEmptyCard(loading = state.loading, title = stringResource(R.string.weighing_shed_records_empty))
                    }
                } else {
                    items(
                        count = state.records.size,
                        key = { index -> "record-${state.records[index].id}" },
                    ) { index ->
                        // Scroll-driven: composing a row near the tail of the window reveals the
                        // next page. One page per trigger, same as every other weighing list.
                        LaunchedEffect(index, state.records.size) { onRecordRowVisible(index) }
                        ShedRecordRow(state.records[index])
                    }
                    if (state.moreRecords > 0) {
                        item(key = "shed-records-more") { LoadingMoreRecordsFooter() }
                    }
                }
            }
            if (state.canReopen) {
                item(key = "shed-reopen") {
                    ReopenAction(busy = state.busy, onReopen = { confirmReopenOpen = true })
                }
            } else if (state.reopenBlockedReason.isNotBlank()) {
                item(key = "shed-reopen-blocked") {
                    ShedBanner(text = state.reopenBlockedReason, fg = MeshaColors.Purple, bg = MeshaColors.PurpleX)
                }
            }
            item(key = "shed-tail-space") { Spacer(Modifier.height(24.dp)) }
        }
    }
}

@Composable
private fun ShedFieldGroup(fields: List<WeighingShedFieldUiRow>) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(11.dp),
    ) {
        fields.forEach { field ->
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = field.label,
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.weight(1f),
                )
                if (field.pill) {
                    ShedPill(
                        label = field.value,
                        fg = field.pillFg ?: MeshaColors.BrandD,
                        bg = field.pillBg ?: MeshaColors.Surf3,
                    )
                } else {
                    Text(
                        text = field.value,
                        color = if (field.faint) MeshaColors.Faint else MeshaColors.Ink,
                        style = MeshaType.bodyStrong,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
        }
    }
}

@Composable
private fun ShedRecordRow(row: WeighingShedRecordUiRow) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .clip(RoundedCornerShape(99.dp))
                .background(if (row.hasTag) MeshaColors.Ok else MeshaColors.Muted),
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = if (row.hasTag) row.tagLabel else stringResource(R.string.weighing_shed_no_tag),
                color = MeshaColors.Ink,
                style = MeshaType.listTitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Text(
                text = if (row.hasVideo) {
                    stringResource(R.string.weighing_shed_video_linked)
                } else {
                    stringResource(R.string.weighing_shed_no_video)
                },
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
            )
        }
        Text(
            text = row.weightLabel,
            color = MeshaColors.BrandD,
            style = MeshaType.listTitle,
        )
    }
}

@Composable
private fun ReopenAction(busy: Boolean, onReopen: () -> Unit) {
    Text(
        text = if (busy) {
            stringResource(R.string.weighing_shed_reopening)
        } else {
            stringResource(R.string.weighing_shed_reopen_action)
        },
        color = if (busy) MeshaColors.Muted else MeshaColors.Warn,
        style = MeshaType.cta,
        modifier = Modifier
            .fillMaxWidth()
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(24.dp))
            .background(MeshaColors.WarnX)
            .clickable(enabled = !busy, role = Role.Button, onClick = onReopen)
            .padding(horizontal = 16.dp, vertical = 14.dp),
    )
}

@Composable
private fun ShedBanner(text: String, fg: Color, bg: Color) {
    Text(
        text = text,
        color = fg,
        style = MeshaType.cardSubtitle,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(bg)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    )
}

@Composable
private fun ShedEmptyCard(loading: Boolean, title: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .padding(18.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(16.dp),
                color = MeshaColors.Brand,
                strokeWidth = 2.dp,
            )
        }
        Text(
            text = if (loading) stringResource(R.string.weighing_shed_loading) else title,
            color = MeshaColors.Muted,
            style = MeshaType.body,
        )
    }
}

@Composable
private fun LoadingMoreRecordsFooter() {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CircularProgressIndicator(
            modifier = Modifier.size(16.dp),
            color = MeshaColors.Brand,
            strokeWidth = 2.dp,
        )
        Text(
            text = stringResource(R.string.weighing_shed_loading_more_records),
            color = MeshaColors.Muted,
            style = MeshaType.cardSubtitle,
            modifier = Modifier.padding(start = 8.dp),
        )
    }
}

@Composable
private fun ShedPill(label: String, fg: Color, bg: Color) {
    Text(
        text = label,
        color = fg,
        fontSize = 12.sp,
        fontWeight = FontWeight.W800,
        maxLines = 1,
        modifier = Modifier
            .clip(RoundedCornerShape(9.dp))
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 6.dp),
    )
}
