package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; the shifting ViewModels (in :app) own the capture,
// answer and submit AnalyticsEvents and the CrashReporter non-fatals for every card they render.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md): one card of the
 * shifting SOP as the phone renders it -- the raise extras, the completion card or the
 * high-priority card. Every title, hint, kind and question is the PINNED card's (served by the
 * backend); this module adds only verbs and capture state. The slot machinery lives in :app and
 * maps its state onto these models.
 */
@Immutable
data class ShiftingSopSlotUi(
    /** The card's slot key = the proof register field_key stamped on the upload. */
    val key: String,
    val title: String,
    val hint: String = "",
    /** video | photo | either -- what the slot accepts. */
    val kind: String = "video",
    val required: Boolean = true,
    val captured: Boolean = false,
    val capturedPhoto: Boolean = false,
    val isCapturing: Boolean = false,
    val failed: Boolean = false,
    val message: String? = null,
)

@Immutable
data class ShiftingSopQuestionUi(
    val id: String,
    val kind: String,
    val title: String,
    val hint: String = "",
    val required: Boolean = false,
    /** (value, label) pairs for choice / multi. */
    val options: List<Pair<String, String>> = emptyList(),
    val allowOther: Boolean = false,
    val unit: String = "",
)

@Immutable
data class ShiftingSopCardUi(
    val instruction: String = "",
    val slots: List<ShiftingSopSlotUi> = emptyList(),
    /** Only the questions that currently apply (conditionals already resolved). */
    val questions: List<ShiftingSopQuestionUi> = emptyList(),
    val answers: Map<String, String> = emptyMap(),
) {
    val isEmpty: Boolean get() = slots.isEmpty() && questions.isEmpty() && instruction.isBlank()
    val anyCapturing: Boolean get() = slots.any { it.isCapturing }
}

/**
 * The card's instruction, capture slots and questions. [locked] hides every action (the work is
 * committed) while the captures stay visible. An `either` slot offers both verbs.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ShiftingSopCardSection(
    card: ShiftingSopCardUi,
    locked: Boolean,
    onCapture: (slotKey: String, kind: String?) -> Unit,
    onAnswer: (questionId: String, value: String) -> Unit,
    modifier: Modifier = Modifier,
    heading: String? = null,
) {
    if (card.isEmpty) return
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        heading?.let { Text(it, color = MeshaColors.Ink, style = MeshaType.listTitle) }
        if (card.instruction.isNotBlank()) {
            Text(card.instruction, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
        }
        card.slots.forEach { slot ->
            SlotRow(slot = slot, locked = locked || card.anyCapturing && !slot.isCapturing, onCapture = onCapture)
        }
        card.questions.forEach { q ->
            QuestionRow(
                question = q,
                answer = card.answers[q.id].orEmpty(),
                otherText = card.answers[q.id + "_other"].orEmpty(),
                enabled = !locked,
                onAnswer = onAnswer,
            )
        }
    }
}

@Composable
private fun SlotRow(slot: ShiftingSopSlotUi, locked: Boolean, onCapture: (String, String?) -> Unit) {
    Column(modifier = cardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                slot.title + if (slot.required) " (required)" else " (optional)",
                color = MeshaColors.Muted,
                style = MeshaType.fieldLabel,
                modifier = Modifier.weight(1f),
            )
            if (slot.captured) {
                Icon(MeshaIcons.CheckCircle, contentDescription = "captured", tint = if (slot.failed) MeshaColors.Warn else MeshaColors.Ok, modifier = Modifier.size(18.dp))
            }
        }
        if (slot.isCapturing) {
            ActionButton(label = "Recording…", enabled = false, loading = true, onClick = {})
        } else if (!locked || slot.captured) {
            val photoSlot = slot.kind == "photo"
            val verb = when {
                slot.captured && (photoSlot || slot.capturedPhoto) -> "Retake photo"
                slot.captured -> "Re-record"
                photoSlot -> "Take live photo"
                else -> "Record live video"
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                ActionButton(label = verb, enabled = !locked, modifier = Modifier.weight(1f), onClick = { onCapture(slot.key, null) })
                if (slot.kind == "either" && !locked) {
                    ActionButton(label = "Take live photo", enabled = true, modifier = Modifier.weight(1f), onClick = { onCapture(slot.key, "photo") })
                }
            }
        }
        if (slot.hint.isNotBlank() && !slot.captured) Text(slot.hint, color = MeshaColors.Faint, style = MeshaType.rowCaption)
        slot.message?.let { Text(it, color = if (slot.failed) MeshaColors.Warn else MeshaColors.Faint, style = MeshaType.rowCaption) }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun QuestionRow(
    question: ShiftingSopQuestionUi,
    answer: String,
    otherText: String,
    enabled: Boolean,
    onAnswer: (String, String) -> Unit,
) {
    Column(modifier = cardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(
            question.title + if (question.required) " *" else "",
            color = MeshaColors.Ink,
            style = MeshaType.rowValue,
        )
        if (question.hint.isNotBlank()) Text(question.hint, color = MeshaColors.Faint, style = MeshaType.rowCaption)
        when (question.kind) {
            "choice", "multi" -> {
                val picked = if (question.kind == "multi") answer.split(",").map { it.trim() }.filter { it.isNotBlank() }.toSet() else setOf(answer)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    question.options.forEach { (value, label) ->
                        val selected = value in picked
                        Text(
                            text = label,
                            color = if (selected) MeshaColors.BrandD else MeshaColors.Ink,
                            style = if (selected) MeshaType.pillStrong else MeshaType.cardSubtitle,
                            modifier = Modifier
                                .minimumInteractiveComponentSize()
                                .clip(RoundedCornerShape(999.dp))
                                .background(if (selected) MeshaColors.OkX else MeshaColors.Surf2)
                                .then(
                                    if (enabled) {
                                        Modifier.clickable {
                                            val next = if (question.kind == "multi") {
                                                (if (selected) picked - value else picked + value).joinToString(",")
                                            } else {
                                                value
                                            }
                                            onAnswer(question.id, next)
                                        }
                                    } else {
                                        Modifier
                                    },
                                )
                                .padding(horizontal = 12.dp, vertical = 6.dp),
                        )
                    }
                }
                if (question.kind == "choice" && question.allowOther && answer == "other") {
                    CountsTextField(value = otherText, onValueChange = { onAnswer(question.id + "_other", it) }, label = "Say which", readOnly = !enabled)
                }
            }
            "number" -> CountsTextField(
                value = answer,
                onValueChange = { v -> if (v.isEmpty() || v.all { it.isDigit() || it == '.' || it == '-' }) onAnswer(question.id, v) },
                label = question.unit.ifBlank { question.title },
                numeric = true,
                readOnly = !enabled,
            )
            else -> CountsTextField(
                value = answer,
                onValueChange = { onAnswer(question.id, it) },
                label = question.title,
                singleLine = false,
                readOnly = !enabled,
            )
        }
    }
}

@Composable
private fun ActionButton(label: String, enabled: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier, loading: Boolean = false) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
            .then(if (enabled) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        // The leading glyph keeps one fixed 18dp box whether it shows the icon or the spinner, so a
        // capture starting never nudges the label; state changes colour only.
        val iconTint = if (enabled) MeshaColors.BrandD else MeshaColors.Faint
        Box(modifier = Modifier.size(18.dp), contentAlignment = Alignment.Center) {
            if (loading) {
                CircularProgressIndicator(modifier = Modifier.fillMaxSize(), strokeWidth = 2.dp, color = MeshaColors.Muted)
            } else {
                Icon(MeshaIcons.Video, contentDescription = null, tint = iconTint, modifier = Modifier.fillMaxSize())
            }
        }
        Text(label, color = if (enabled || loading) MeshaColors.Ink else MeshaColors.Faint, style = MeshaType.cta)
    }
}

@Composable
private fun cardModifier(): Modifier = Modifier
    .fillMaxWidth()
    .clip(RoundedCornerShape(16.dp))
    .background(MeshaColors.Surf)
    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
    .padding(14.dp)
