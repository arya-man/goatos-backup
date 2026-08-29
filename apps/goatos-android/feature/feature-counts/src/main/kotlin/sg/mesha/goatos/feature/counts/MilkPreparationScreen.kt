package sg.mesha.goatos.feature.counts

// telemetry:exempt presentational renderer; capture/write telemetry lives in MilkPreparationViewModel.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

@Immutable
data class MilkPreparationStepUi(
    val code: String,
    val label: String,
    val captured: Boolean = false,
    val capturing: Boolean = false,
    val answer: String = "",
    val unit: String = "",
    val requiresAnswer: Boolean = true,
    val enabled: Boolean = false,
) {
    val answerComplete: Boolean get() = !requiresAnswer || answer.toDoubleOrNull()?.let { it > 0 } == true
}

@Immutable
data class MilkPreparationUiState(
    val preparationDate: String = "",
    val feedingDate: String = "",
    val selectedParkId: String = "",
    val parkLabel: String = "",
    val goatMilkUsed: Boolean? = null,
    val morningMilkCollected: String = "",
    val eveningMilkCollected: String = "",
    val steps: List<MilkPreparationStepUi> = emptyList(),
    val message: String? = null,
    val submitting: Boolean = false,
    val submitted: Boolean = false,
    val verificationStatus: String = "",
    val reworkReason: String = "",
    val cohortCount: Int = 0,
    val headCount: Int = 0,
    val totalMilkLabel: String = "",
    val citricAcidLabel: String = "",
    val citricAcidGrams: Double = 0.0,
    val milkDirectionLines: List<String> = emptyList(),
    val citricAcidRateLabel: String = "",
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val lastSyncedAt: Long? = null,
    val isToday: Boolean = true,
) {
    val hasCaptured: Boolean get() = steps.any { it.captured }
    val isEditable: Boolean get() = isToday && (verificationStatus.isBlank() || verificationStatus == "not_submitted" || verificationStatus == "rework")
    val morningQuestionEnabled: Boolean get() = isEditable && !submitted
    val eveningQuestionEnabled: Boolean get() = morningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true && morningQuestionEnabled
    val goatMilkQuestionEnabled: Boolean get() = eveningQuestionEnabled && eveningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true && !hasCaptured
    val answersValid: Boolean get() = morningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true &&
        eveningMilkCollected.toDoubleOrNull()?.let { it >= 0 } == true &&
        goatMilkUsed != null &&
        steps.all { it.answerComplete }
    val canSubmit: Boolean get() = selectedParkId.isNotBlank() && citricAcidGrams > 0 && isEditable && steps.isNotEmpty() && steps.all { it.captured } && answersValid && !submitting && !submitted
    val completedStepCount: Int get() = steps.count { it.captured }
}

sealed interface MilkPreparationEvent {
    data class SetGoatMilkUsed(val used: Boolean) : MilkPreparationEvent
    data class CaptureStep(val stepCode: String) : MilkPreparationEvent

    /**
     * Replace a step's already-recorded clip. Distinct from [CaptureStep] so the ViewModel can DROP
     * the discarded take's queued upload — a re-record must not leave the verifier two videos.
     */
    data class ReCaptureStep(val stepCode: String) : MilkPreparationEvent
    data class SetCollectedMilk(val shift: String, val value: String) : MilkPreparationEvent
    data class SetStepAnswer(val stepCode: String, val value: String) : MilkPreparationEvent
    data object Submit : MilkPreparationEvent
    data object Refresh : MilkPreparationEvent
    data object Back : MilkPreparationEvent
}

@Composable
fun MilkPreparationScreen(
    state: MilkPreparationUiState,
    onEvent: (MilkPreparationEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(MilkPreparationEvent.Refresh) }
    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.parkLabel.ifBlank { "Milk Preparation" },
            subtitle = listOf(state.preparationDate, state.feedingDate.takeIf(String::isNotBlank)?.let { "feeds $it" })
                .filterNotNull().filter(String::isNotBlank).joinToString(" · ").ifBlank { null },
            onBack = { onEvent(MilkPreparationEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(MilkPreparationEvent.Refresh) },
                )
            },
        )
        SyncStatusIndicator(
            isRefreshing = state.isRefreshing,
            lastSyncedAt = state.lastSyncedAt,
            hasData = state.parkLabel.isNotBlank(),
            isOffline = state.isOffline,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "context") { MilkPreparationContextCard(state) }
            item(key = "direction") { MilkDirectionCard(state) }
            if (state.verificationStatus == "pending_verification" || state.verificationStatus == "completed" || state.submitted) {
                item(key = "status") { MilkPreparationStatusCard(state) }
            } else {
                item(key = "milking-questions") { GoatMilkingQuestions(state, onEvent) }
                item(key = "steps-title") { MilkPreparationSectionTitle("Preparation steps") }
                items(state.steps, key = { it.code }) { step ->
                    MilkPreparationStepCard(step, state.selectedParkId.isNotBlank() && state.isEditable, onAnswer = { onEvent(MilkPreparationEvent.SetStepAnswer(step.code, it)) }, onRecord = { onEvent(MilkPreparationEvent.CaptureStep(step.code)) }, onReRecord = { onEvent(MilkPreparationEvent.ReCaptureStep(step.code)) })
                }
                item(key = "submit") { MilkPreparationSubmitButton(state) { onEvent(MilkPreparationEvent.Submit) } }
            }
            state.message?.let { message ->
                item(key = "message") {
                    Text(
                        message,
                        color = if (message.contains("background", true)) MeshaColors.BrandD else MeshaColors.Danger,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.W600,
                    )
                }
            }
        }
    }
}

@Composable
private fun GoatMilkingQuestions(state: MilkPreparationUiState, onEvent: (MilkPreparationEvent) -> Unit) {
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(MeshaColors.Surf).padding(12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text("Goat Milking Report", color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W800)
        MilkNumberField("Morning milk collected (in litres)", state.morningMilkCollected, "L", state.morningQuestionEnabled) { onEvent(MilkPreparationEvent.SetCollectedMilk("morning", it)) }
        MilkNumberField("Evening milk collected (in litres)", state.eveningMilkCollected, "L", state.eveningQuestionEnabled) { onEvent(MilkPreparationEvent.SetCollectedMilk("evening", it)) }
        Text("Is goat milk used for feeding?", color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
        Text("Choose before recording. The choice locks after the first video.", color = MeshaColors.Faint, fontSize = 11.sp)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            MilkChoiceButton("Yes", state.goatMilkUsed == true, state.goatMilkQuestionEnabled, Modifier.weight(1f)) { onEvent(MilkPreparationEvent.SetGoatMilkUsed(true)) }
            MilkChoiceButton("No", state.goatMilkUsed == false, state.goatMilkQuestionEnabled, Modifier.weight(1f)) { onEvent(MilkPreparationEvent.SetGoatMilkUsed(false)) }
        }
    }
}

@Composable
private fun MilkDirectionCard(state: MilkPreparationUiState) {
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(MeshaColors.Surf).padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(7.dp),
    ) {
        Text("Milk Direction", color = MeshaColors.Ink, fontSize = 14.sp, fontWeight = FontWeight.W800)
        state.milkDirectionLines.forEach { line ->
            Text("• $line", color = MeshaColors.Muted, fontSize = 12.sp, fontWeight = FontWeight.W600)
        }
        if (state.milkDirectionLines.isEmpty()) Text("Milk direction is being calculated.", color = MeshaColors.Faint, fontSize = 12.sp)
        Box(Modifier.fillMaxWidth().height(1.dp).background(MeshaColors.Hair))
        Text("Total: ${state.totalMilkLabel}", color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W800)
        Text(
            "Citric acid: ${state.citricAcidRateLabel} × ${state.totalMilkLabel} = ${state.citricAcidLabel}",
            color = MeshaColors.Muted,
            fontSize = 12.sp,
        )
    }
}

@Composable
private fun MilkNumberField(label: String, value: String, unit: String, enabled: Boolean = true, onValueChange: (String) -> Unit) {
    OutlinedTextField(
        value = value,
        onValueChange = { next -> if (next.isEmpty() || next.matches(Regex("\\d*(\\.\\d*)?"))) onValueChange(next) },
        label = { Text(label) },
        suffix = { Text(unit) },
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
        singleLine = true,
        enabled = enabled,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun MilkPreparationContextCard(state: MilkPreparationUiState) {
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(18.dp)).background(MeshaColors.Surf).padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Box(Modifier.size(38.dp).clip(CircleShape).background(MeshaColors.BrandTint), contentAlignment = Alignment.Center) {
                Icon(MeshaIcons.Feed, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(19.dp))
            }
            Column(Modifier.weight(1f)) {
                Text("Daily milk preparation", color = MeshaColors.Ink, fontSize = 16.sp, fontWeight = FontWeight.W800)
                Text("One verifier reviews the complete proof set", color = MeshaColors.Faint, fontSize = 11.sp)
            }
        }
        listOf(
            "Farm" to state.parkLabel,
            "Cohorts" to state.cohortCount.toString(),
            "Animals" to state.headCount.toString(),
            "Milk" to state.totalMilkLabel,
            "Citric acid" to state.citricAcidLabel,
        ).chunked(2).forEach { pair ->
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                pair.forEach { (label, value) -> MilkPreparationFact(label, value, Modifier.weight(1f)) }
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            val fraction = if (state.steps.isNotEmpty()) state.completedStepCount.toFloat() / state.steps.size else 0f
            Box(Modifier.weight(1f).height(5.dp).clip(RoundedCornerShape(999.dp)).background(MeshaColors.Surf3)) {
                Box(Modifier.fillMaxWidth(fraction).height(5.dp).background(MeshaColors.Brand))
            }
            Text("${state.completedStepCount}/${state.steps.size} videos", color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W700)
        }
    }
}

@Composable
private fun MilkPreparationFact(label: String, value: String, modifier: Modifier) {
    Row(
        modifier.clip(RoundedCornerShape(10.dp)).background(MeshaColors.Surf2).padding(horizontal = 10.dp, vertical = 7.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, color = MeshaColors.Faint, fontSize = 11.sp)
        Text(value.ifBlank { "—" }, color = MeshaColors.Ink, fontSize = 11.sp, fontWeight = FontWeight.W700)
    }
}

@Composable
private fun MilkChoiceButton(label: String, selected: Boolean, enabled: Boolean, modifier: Modifier, onClick: () -> Unit) {
    Box(
        modifier.clip(RoundedCornerShape(12.dp)).background(if (selected) MeshaColors.Brand else MeshaColors.Surf2)
            .clickable(enabled = enabled, onClick = onClick).padding(vertical = 11.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = if (selected) MeshaColors.OnBrand else MeshaColors.Muted, fontWeight = FontWeight.W800)
    }
}

@Composable
private fun MilkPreparationSectionTitle(title: String) {
    Row(Modifier.fillMaxWidth().padding(top = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(title, color = MeshaColors.Faint, fontSize = 11.sp, fontWeight = FontWeight.W800)
        Box(Modifier.weight(1f).height(1.dp).background(MeshaColors.Hair))
    }
}

@Composable
private fun MilkPreparationStepCard(
    step: MilkPreparationStepUi,
    canEdit: Boolean,
    onAnswer: (String) -> Unit,
    onRecord: () -> Unit,
    onReRecord: () -> Unit = onRecord,
) {
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(MeshaColors.Surf).padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.size(30.dp).clip(RoundedCornerShape(8.dp)).background(MeshaColors.Surf2), contentAlignment = Alignment.Center) {
                Icon(MeshaIcons.Video, contentDescription = null, tint = if (step.captured) MeshaColors.Ok else MeshaColors.Muted, modifier = Modifier.size(16.dp))
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(step.label, color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
                Text(if (step.captured) "Video saved" else "Fresh live-camera video", color = if (step.captured) MeshaColors.Ok else MeshaColors.Faint, fontSize = 11.sp)
            }
            Text(
                if (step.captured) "Done" else "Video",
                color = if (step.captured) MeshaColors.Ok else MeshaColors.Warn,
                fontSize = 10.sp,
                fontWeight = FontWeight.W800,
                modifier = Modifier.clip(RoundedCornerShape(999.dp)).background(if (step.captured) MeshaColors.OkX else MeshaColors.WarnX)
                    .padding(horizontal = 7.dp, vertical = 3.dp),
            )
        }
        if (step.requiresAnswer) {
            MilkNumberField(step.label, step.answer, step.unit, step.enabled && canEdit, onAnswer)
        } else {
            Text(
                "Amount is calculated automatically. Record the mixing and storage video.",
                color = MeshaColors.Muted,
                fontSize = 12.sp,
            )
        }
        val canRecord = !step.captured && step.enabled && step.answerComplete && canEdit
        // A captured step is not a dead end: an unusable clip can be replaced from here rather than
        // submitted and bounced by the verifier (maintainer request 2026-07-30).
        val clickable = if (step.captured) canEdit && !step.capturing else canRecord && !step.capturing
        Box(
            Modifier.fillMaxWidth().height(48.dp).clip(RoundedCornerShape(12.dp))
                .background(if (canRecord) MeshaColors.Brand else MeshaColors.Surf3)
                .clickable(enabled = clickable, onClick = if (step.captured) onReRecord else onRecord),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                when {
                    step.capturing -> "Opening camera…"
                    step.captured -> "Recorded · Re-record"
                    else -> "Record video"
                },
                color = if (canRecord) MeshaColors.OnBrand else MeshaColors.Muted,
                fontWeight = FontWeight.W800,
            )
        }
    }
}

@Composable
private fun MilkPreparationSubmitButton(state: MilkPreparationUiState, onSubmit: () -> Unit) {
    Box(
        Modifier.fillMaxWidth().height(52.dp).clip(RoundedCornerShape(14.dp))
            .background(if (state.canSubmit) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = state.canSubmit, onClick = onSubmit),
        contentAlignment = Alignment.Center,
    ) {
        Text(if (state.submitting) "Submitting…" else "Submit answers & videos", color = if (state.canSubmit) MeshaColors.OnBrand else MeshaColors.Muted, fontWeight = FontWeight.W800)
    }
}

@Composable
private fun MilkPreparationStatusCard(state: MilkPreparationUiState) {
    val completed = state.verificationStatus == "completed"
    val title = if (completed) "Preparation verified" else "Submitted for verification"
    val detail = if (completed) "The verifier approved the complete proof set." else "The proof set is waiting for verifier review."
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(if (completed) MeshaColors.OkX else MeshaColors.WarnX).padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(if (completed) MeshaIcons.CheckCircle else MeshaIcons.Clock, contentDescription = null, tint = if (completed) MeshaColors.Ok else MeshaColors.Warn)
        Column {
            Text(title, color = MeshaColors.Ink, fontWeight = FontWeight.W800)
            Text(detail, color = MeshaColors.Muted, fontSize = 11.sp)
        }
    }
}
