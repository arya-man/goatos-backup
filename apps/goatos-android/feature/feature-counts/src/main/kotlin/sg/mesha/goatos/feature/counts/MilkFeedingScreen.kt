package sg.mesha.goatos.feature.counts

// telemetry:exempt presentational renderer; write/capture telemetry lives in MilkFeedingViewModel.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

@Immutable data class MilkFeedingWatchlistUi(val goatId: String, val label: String, val drankMilk: Boolean? = null)
@Immutable data class MilkFeedingProofUi(val code: String, val label: String, val captured: Boolean = false, val capturing: Boolean = false)

@Immutable
data class MilkFeedingUiState(
    val taskId: String = "", val parkId: String = "", val parkLabel: String = "",
    val feedingDate: String = "", val sessionNo: Int = 0, val dueTime: String = "", val status: String = "not_submitted",
    val watchlist: List<MilkFeedingWatchlistUi> = emptyList(),
    val totalKidsFed: String = "", val attempt1NotDrinking: String = "", val attempt2NotDrinking: String = "",
    val newRefusalIds: String = "", val remarks: String = "", val udderNotDrinking: String = "", val orsNotDrinking: String = "",
    val proofs: List<MilkFeedingProofUi> = emptyList(), val submitting: Boolean = false, val queued: Boolean = false, val message: String? = null,
    val available: Boolean = true, val blockedReason: String = "",
) {
    private val total get() = totalKidsFed.toIntOrNull()
    private val a1 get() = attempt1NotDrinking.toIntOrNull()
    private val a2 get() = if ((a1 ?: 0) == 0) 0 else attempt2NotDrinking.toIntOrNull()
    private val watchlistRefusals get() = watchlist.count { it.drankMilk == false }
    val newRefusalCount get() = maxOf(0, (a2 ?: 0) - watchlistRefusals)
    private val enteredIds get() = newRefusalIds.split(',').map(String::trim).filter(String::isNotBlank).distinct()
    private val udder get() = if ((a2 ?: 0) == 0) 0 else udderNotDrinking.toIntOrNull()
    private val ors get() = if ((udder ?: 0) == 0) 0 else orsNotDrinking.toIntOrNull()
    val canSubmit get() = available && taskId.isNotBlank() && status in setOf("not_submitted", "rework") && !queued && !submitting &&
        watchlist.all { it.drankMilk != null } && total != null && total!! >= 0 && a1 != null && a1!! in 0..total!! &&
        a2 != null && a2!! in 0..a1!! && enteredIds.size == newRefusalCount && udder != null && udder!! in 0..a2!! &&
        ors != null && ors!! in 0..udder!! && proofs.all { it.captured }
}

@Immutable data class MilkFeedingCardUi(val taskId: String, val parkLabel: String, val sessionNo: Int, val dueTime: String, val headCount: Int, val status: String, val reworkReason: String = "", val available: Boolean = true, val blockedReason: String = "", val capturedProofCount: Int = 0) {
    val canOpen: Boolean get() = available && status in setOf("not_submitted", "rework")

    /**
     * True when the operator started this session and left before submitting, so the list can offer
     * to resume instead of reading as untouched work.
     */
    val isInProgress: Boolean get() = capturedProofCount > 0 && canOpen
}
@Immutable data class MilkFeedingListUiState(
    val subtitle: String = "", val dateLabel: String = "", val selectedDate: String = "", val isToday: Boolean = true, val chips: List<MilkPreparationChipUi> = emptyList(),
    val selectedFilter: String = "all", val cards: List<MilkFeedingCardUi> = emptyList(), val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null, val isOffline: Boolean = false, val emptyMessage: String? = null,
)
sealed interface MilkFeedingListEvent {
    data object Refresh : MilkFeedingListEvent
    data class SelectFilter(val key: String) : MilkFeedingListEvent
    data class OpenTask(val taskId: String, val feedingDate: String = "") : MilkFeedingListEvent
    data class NavigateDate(val delta: Int) : MilkFeedingListEvent
    data class SelectDate(val date: String) : MilkFeedingListEvent
    data object Back : MilkFeedingListEvent
}

@Composable
fun MilkFeedingListScreen(state: MilkFeedingListUiState, onEvent: (MilkFeedingListEvent) -> Unit, modifier: Modifier = Modifier) {
    RefreshOnResume { onEvent(MilkFeedingListEvent.Refresh) }
    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Milk Feeding", subtitle = state.subtitle.ifBlank { null }, onBack = { onEvent(MilkFeedingListEvent.Back) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(MilkFeedingListEvent.Refresh) }) },
        )
        SyncStatusIndicator(state.isRefreshing, state.lastSyncedAt, state.cards.isNotEmpty(), state.isOffline, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        MilkWorkDateBar(
            state.dateLabel,
            selectedDate = state.selectedDate,
            isToday = state.isToday,
            onPreviousDate = { onEvent(MilkFeedingListEvent.NavigateDate(-1)) },
            onNextDate = { onEvent(MilkFeedingListEvent.NavigateDate(1)) },
            onSelectDate = { onEvent(MilkFeedingListEvent.SelectDate(it)) },
        )
        MilkStatusChips(state.chips, state.selectedFilter) { onEvent(MilkFeedingListEvent.SelectFilter(it)) }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            if (state.cards.isEmpty() && state.emptyMessage != null) item(key = "empty") {
                EmptyState(
                    title = state.emptyMessage,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
                    icon = MeshaIcons.Feed,
                    tone = EmptyTone.Neutral,
                )
            }
            items(state.cards, key = { it.taskId }) { task ->
                MilkFeedingWorkCard(task) { onEvent(MilkFeedingListEvent.OpenTask(task.taskId, state.selectedDate)) }
            }
        }
    }
}

@Composable
private fun MilkFeedingWorkCard(task: MilkFeedingCardUi, onClick: () -> Unit) {
    val bucket = when (task.status) {
        "pending_verification" -> MilkPreparationCardBucket.IN_REVIEW
        "completed" -> MilkPreparationCardBucket.COMPLETED
        "rework" -> MilkPreparationCardBucket.REWORK
        else -> MilkPreparationCardBucket.TO_PREPARE
    }
    val stripe = when (bucket) {
        MilkPreparationCardBucket.COMPLETED -> MeshaColors.Ok
        MilkPreparationCardBucket.REWORK -> MeshaColors.Danger
        MilkPreparationCardBucket.IN_REVIEW -> MeshaColors.Warn
        MilkPreparationCardBucket.TO_PREPARE -> Color.Transparent
    }
    val statusLabel = when {
        task.isInProgress -> "In progress"
        bucket == MilkPreparationCardBucket.COMPLETED -> "Completed"
        bucket == MilkPreparationCardBucket.REWORK -> "Rework"
        bucket == MilkPreparationCardBucket.IN_REVIEW -> "In review"
        else -> if (task.available) "Need action" else "Locked"
    }
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp).height(androidx.compose.foundation.layout.IntrinsicSize.Min)
            .clip(RoundedCornerShape(16.dp)).background(MeshaColors.Surf)
            .clickable(enabled = task.canOpen, onClick = onClick),
    ) {
        Box(Modifier.width(4.dp).fillMaxHeight().background(stripe))
        Column(Modifier.weight(1f).padding(12.dp), verticalArrangement = Arrangement.spacedBy(9.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(Modifier.size(38.dp).clip(CircleShape).background(MeshaColors.BrandTint), contentAlignment = Alignment.Center) {
                    Icon(MeshaIcons.Feed, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(19.dp))
                }
                Column(Modifier.weight(1f)) {
                    Text(task.parkLabel, color = MeshaColors.Ink, fontSize = 15.sp, fontWeight = FontWeight.W800)
                    Text("Session ${task.sessionNo} · ${task.dueTime}", color = MeshaColors.Faint, fontSize = 11.sp)
                }
                Text(statusLabel, color = if (bucket == MilkPreparationCardBucket.REWORK) MeshaColors.Danger else MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W800, modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(MeshaColors.Surf3).padding(horizontal = 8.dp, vertical = 3.dp))
            }
            Text("${task.headCount} milk kids", color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W700, modifier = Modifier.clip(RoundedCornerShape(10.dp)).background(MeshaColors.Surf2).padding(horizontal = 9.dp, vertical = 7.dp))
            Text(task.blockedReason.ifBlank { task.reworkReason.ifBlank { if (task.isInProgress) "Resume · ${if (task.capturedProofCount == 1) "1 video" else "${task.capturedProofCount} videos"} saved" else if (bucket == MilkPreparationCardBucket.TO_PREPARE) "Open feeding report" else statusLabel } }, color = if (bucket == MilkPreparationCardBucket.REWORK) MeshaColors.Danger else if (!task.available) MeshaColors.Faint else MeshaColors.Ink, fontSize = 12.sp, fontWeight = FontWeight.W600)
        }
    }
}

sealed interface MilkFeedingEvent {
    data class SetWatchlistAnswer(val goatId: String, val drank: Boolean) : MilkFeedingEvent
    data class SetNumber(val field: String, val value: String) : MilkFeedingEvent
    data class SetText(val field: String, val value: String) : MilkFeedingEvent
    data class CaptureProof(val code: String) : MilkFeedingEvent

    /** Replace a captured clip; the ViewModel drops the discarded take's queued upload. */
    data class ReCaptureProof(val code: String) : MilkFeedingEvent
    data object Submit : MilkFeedingEvent
    data object Back : MilkFeedingEvent
}

@Composable
fun MilkFeedingScreen(state: MilkFeedingUiState, onEvent: (MilkFeedingEvent) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = "Milk Feeding · Session ${state.sessionNo}", subtitle = "${state.parkLabel} · ${state.dueTime}", onBack = { onEvent(MilkFeedingEvent.Back) })
        if (!state.available) {
            Column(Modifier.fillMaxWidth().padding(16.dp)) {
                Card {
                    Text("Session locked", color = MeshaColors.Ink, fontWeight = FontWeight.W800)
                    Text(state.blockedReason.ifBlank { "This feeding session is not available yet." }, color = MeshaColors.Muted)
                }
            }
            return@Column
        }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            item(key = "intro") { Card { Text("Answer the feeding report, then record both fresh videos.", color = MeshaColors.Muted, fontSize = 12.sp) } }
            if (state.watchlist.isNotEmpty()) {
                item(key = "watch-title") { Title("Watchlist kids — did they drink?") }
                items(state.watchlist, key = { it.goatId }) { kid -> // compose-guard:ignore: watchlist grain is exactly one row per unique goat
                    Card {
                        Text(kid.label, color = MeshaColors.Ink, fontWeight = FontWeight.W700)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Choice("Yes", kid.drankMilk == true, Modifier.weight(1f)) { onEvent(MilkFeedingEvent.SetWatchlistAnswer(kid.goatId, true)) }
                            Choice("No", kid.drankMilk == false, Modifier.weight(1f)) { onEvent(MilkFeedingEvent.SetWatchlistAnswer(kid.goatId, false)) }
                        }
                    }
                }
            }
            item(key = "attempt1-title") { Title("Attempt 1") }
            item(key = "total") { NumberField("Count (total kids fed)", state.totalKidsFed) { onEvent(MilkFeedingEvent.SetNumber("total", it)) } }
            item(key = "a1") { NumberField("How many did not drink cow milk?", state.attempt1NotDrinking) { onEvent(MilkFeedingEvent.SetNumber("attempt1", it)) } }
            if ((state.attempt1NotDrinking.toIntOrNull() ?: 0) > 0) item(key = "a2") { NumberField("Attempt 2 — how many still did not drink?", state.attempt2NotDrinking) { onEvent(MilkFeedingEvent.SetNumber("attempt2", it)) } }
            if (state.newRefusalCount > 0) {
                item(key = "ids") { TextField("${state.newRefusalCount} new refusal Goat OS ID(s), comma separated", state.newRefusalIds) { onEvent(MilkFeedingEvent.SetText("ids", it)) } }
                item(key = "remarks") { TextField("Remarks (optional)", state.remarks) { onEvent(MilkFeedingEvent.SetText("remarks", it)) } }
            }
            if ((state.attempt2NotDrinking.toIntOrNull() ?: 0) > 0) item(key = "udder") { NumberField("How many did not drink udder milk?", state.udderNotDrinking) { onEvent(MilkFeedingEvent.SetNumber("udder", it)) } }
            if ((state.udderNotDrinking.toIntOrNull() ?: 0) > 0) item(key = "ors") { NumberField("How many did not drink ORS?", state.orsNotDrinking) { onEvent(MilkFeedingEvent.SetNumber("ors", it)) } }
            item(key = "proof-title") { Title("Mandatory proof videos") }
            // A captured proof stays replaceable: an unusable clip is re-recorded here instead of
            // being submitted and bounced by the verifier (maintainer request 2026-07-30).
            items(state.proofs, key = { it.code }) { proof ->
                Card {
                    Text(proof.label, color = MeshaColors.Ink, fontWeight = FontWeight.W700)
                    Action(
                        if (proof.captured) "Recorded · Re-record" else "Record live video",
                        !proof.capturing,
                    ) {
                        onEvent(
                            if (proof.captured) {
                                MilkFeedingEvent.ReCaptureProof(proof.code)
                            } else {
                                MilkFeedingEvent.CaptureProof(proof.code)
                            },
                        )
                    }
                }
            }
            item(key = "submit") { Action(if (state.submitting) "Submitting…" else "Submit answers & proofs", state.canSubmit) { onEvent(MilkFeedingEvent.Submit) } }
            state.message?.let { item(key = "message") { Text(it, color = MeshaColors.Danger, fontSize = 12.sp) } }
        }
    }
}

@Composable private fun Card(content: @Composable ColumnScope.() -> Unit) = Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(MeshaColors.Surf).padding(12.dp), verticalArrangement = Arrangement.spacedBy(9.dp), content = content)
@Composable private fun Title(value: String) = Text(value, color = MeshaColors.Faint, fontSize = 11.sp, fontWeight = FontWeight.W800)
@Composable private fun Choice(label: String, selected: Boolean, modifier: Modifier, onClick: () -> Unit) = Box(modifier.height(48.dp).clip(RoundedCornerShape(12.dp)).background(if (selected) MeshaColors.Brand else MeshaColors.Surf2).clickable(onClick = onClick), contentAlignment = Alignment.Center) { Text(label, color = if (selected) MeshaColors.OnBrand else MeshaColors.Muted, fontWeight = FontWeight.W800) }
@Composable private fun NumberField(label: String, value: String, onChange: (String) -> Unit) = OutlinedTextField(value, { if (it.isEmpty() || it.all(Char::isDigit)) onChange(it) }, Modifier.fillMaxWidth(), label = { Text(label) }, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number), singleLine = true)
@Composable private fun TextField(label: String, value: String, onChange: (String) -> Unit) = OutlinedTextField(value, onChange, Modifier.fillMaxWidth(), label = { Text(label) })
@Composable private fun Action(label: String, enabled: Boolean, onClick: () -> Unit) = Box(Modifier.fillMaxWidth().height(50.dp).clip(RoundedCornerShape(12.dp)).background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3).clickable(enabled = enabled, onClick = onClick), contentAlignment = Alignment.Center) { Text(label, color = if (enabled) MeshaColors.OnBrand else MeshaColors.Muted, fontWeight = FontWeight.W800) }
