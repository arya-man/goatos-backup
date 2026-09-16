package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.feature.scan.ProofUploadStatus

/*
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): what the pinned weighing SOP
 * asks beside the scan and the weight, in TWO SEPARATE sections that are never merged -- the
 * per-animal extras render on each animal's row, the whole-pen extras render in the pen section.
 * Every title, hint and option is the served SOP's own copy, rendered verbatim; the phone holds no
 * slot or question of its own. The PRIMARY capture of each section keeps its existing control (the
 * per-animal video opened after the scan, the pen's group-video strip); these are the rest.
 */

/** Everything the operator does to the authored capture sections, as one event stream. */
sealed interface WeighingCaptureSopEvent {
    data class CaptureAnimalSlot(val animalId: String, val slotKey: String, val photo: Boolean) : WeighingCaptureSopEvent
    data class AnimalAnswer(val animalId: String, val questionId: String, val value: String) : WeighingCaptureSopEvent
    data class ToggleAnimalAnswer(val animalId: String, val questionId: String, val value: String) : WeighingCaptureSopEvent
    data class AnimalOtherText(val animalId: String, val questionId: String, val text: String) : WeighingCaptureSopEvent
    data class CapturePenSlot(val slotKey: String, val photo: Boolean) : WeighingCaptureSopEvent
    data class PenAnswer(val questionId: String, val value: String) : WeighingCaptureSopEvent
    data class TogglePenAnswer(val questionId: String, val value: String) : WeighingCaptureSopEvent
    data class PenOtherText(val questionId: String, val text: String) : WeighingCaptureSopEvent
}

/** One authored question, as the screen renders it. */
@Immutable
data class WeighingCaptureQuestionUi(
    val id: String,
    val kind: String,
    val title: String,
    val hint: String = "",
    val required: Boolean = false,
    val options: List<Pair<String, String>> = emptyList(),
    val allowOther: Boolean = false,
    val unit: String = "",
)

/** One recorded answer: pick-one [value], pick-many [values], "other" free text [otherText]. */
@Immutable
data class WeighingCaptureAnswerUi(
    val value: String = "",
    val values: Set<String> = emptySet(),
    val otherText: String = "",
)

/** One authored capture slot beyond a section's primary capture. */
@Immutable
data class WeighingCaptureSlotUi(
    val key: String,
    val title: String,
    val hint: String = "",
    /** video | photo | either. */
    val kind: String = "video",
    val required: Boolean = true,
    val min: Int = 1,
    val max: Int = 1,
    /** Whole-pen slots only: this slot's captures, in capture order. */
    val proofs: List<WeighingProofUiRow> = emptyList(),
)

/**
 * The pinned SOP's capture sections as this screen needs them. [animalExtraSlots] /
 * [animalQuestions] belong to the PER-ANIMAL section; everything prefixed `pen` belongs to the
 * WHOLE-PEN section. Empty lists = the seeded shape (today's screen, unchanged).
 */
@Immutable
data class WeighingCaptureSopUi(
    val animalExtraSlots: List<WeighingCaptureSlotUi> = emptyList(),
    val animalQuestions: List<WeighingCaptureQuestionUi> = emptyList(),
    /** The first whole-pen slot's title and ceiling (the existing group-video strip). */
    val primaryPenSlotTitle: String = "",
    val primaryPenSlotMax: Int = SHED_PROOF_VIDEO_LIMIT,
    val penExtraSlots: List<WeighingCaptureSlotUi> = emptyList(),
    val penQuestions: List<WeighingCaptureQuestionUi> = emptyList(),
    val penAnswers: Map<String, WeighingCaptureAnswerUi> = emptyMap(),
    /** Farm-worded list of what the pen still owes (slot counts, unanswered questions). */
    val penCapturesMissing: List<String> = emptyList(),
)

/** Per-animal extras on one animal's row: slot chips, then the per-animal questions. */
@Composable
internal fun WeighingAnimalCaptureExtras(
    row: WeighingRosterUiRow,
    sop: WeighingCaptureSopUi,
    locked: Boolean,
    onCaptureSlot: (slotKey: String, photo: Boolean) -> Unit,
    onAnswer: (questionId: String, value: String) -> Unit,
    onToggleAnswer: (questionId: String, value: String) -> Unit,
    onOtherText: (questionId: String, text: String) -> Unit,
) {
    if (sop.animalExtraSlots.isEmpty() && sop.animalQuestions.isEmpty()) return
    Column(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
        sop.animalExtraSlots.forEach { slot ->
            val status = row.extraSlotStatuses[slot.key] ?: ProofUploadStatus.MISSING
            CaptureSlotRow(
                slot = slot,
                statusText = slotStatusText(slot, status),
                done = status == ProofUploadStatus.SYNCED,
                enabled = !locked && row.weightSaved,
                onCapture = { photo -> onCaptureSlot(slot.key, photo) },
            )
        }
        sop.animalQuestions.forEach { q ->
            CaptureQuestionField(
                question = q,
                answer = row.answers[q.id] ?: WeighingCaptureAnswerUi(),
                locked = locked,
                onAnswer = { onAnswer(q.id, it) },
                onToggle = { onToggleAnswer(q.id, it) },
                onOther = { onOtherText(q.id, it) },
            )
        }
        if (row.capturesMissing.isNotEmpty()) {
            Text(
                text = stringResource(R.string.weighing_capture_still_needed, row.capturesMissing.joinToString(", ")),
                color = MeshaColors.Warn,
                style = MeshaType.caption,
            )
        }
    }
}

/** Whole-pen extras in the pen section: one strip per extra slot, then the whole-pen questions. */
@Composable
internal fun WeighingPenCaptureExtras(
    sop: WeighingCaptureSopUi,
    locked: Boolean,
    onCaptureSlot: (slotKey: String, photo: Boolean) -> Unit,
    onAnswer: (questionId: String, value: String) -> Unit,
    onToggleAnswer: (questionId: String, value: String) -> Unit,
    onOtherText: (questionId: String, text: String) -> Unit,
) {
    if (sop.penExtraSlots.isEmpty() && sop.penQuestions.isEmpty()) return
    Column(verticalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
        sop.penExtraSlots.forEach { slot ->
            val synced = slot.proofs.count { it.status == ProofUploadStatus.SYNCED }
            CaptureSlotRow(
                slot = slot,
                statusText = stringResource(R.string.weighing_capture_pen_slot_count, synced, slot.max),
                done = synced >= slot.min && slot.proofs.size <= slot.max,
                enabled = !locked && slot.proofs.size < slot.max,
                onCapture = { photo -> onCaptureSlot(slot.key, photo) },
            )
        }
        sop.penQuestions.forEach { q ->
            CaptureQuestionField(
                question = q,
                answer = sop.penAnswers[q.id] ?: WeighingCaptureAnswerUi(),
                locked = locked,
                onAnswer = { onAnswer(q.id, it) },
                onToggle = { onToggleAnswer(q.id, it) },
                onOther = { onOtherText(q.id, it) },
            )
        }
        if (sop.penCapturesMissing.isNotEmpty()) {
            Text(
                text = stringResource(R.string.weighing_capture_still_needed, sop.penCapturesMissing.joinToString(", ")),
                color = MeshaColors.Warn,
                style = MeshaType.caption,
            )
        }
    }
}

@Composable
private fun slotStatusText(slot: WeighingCaptureSlotUi, status: ProofUploadStatus): String = when (status) {
    ProofUploadStatus.SYNCED -> stringResource(R.string.weighing_capture_slot_saved)
    ProofUploadStatus.UPLOADING -> stringResource(R.string.weighing_capture_slot_uploading)
    ProofUploadStatus.FAILED -> stringResource(R.string.weighing_capture_slot_failed)
    ProofUploadStatus.MISSING -> if (slot.required) {
        stringResource(R.string.weighing_capture_slot_needed)
    } else {
        stringResource(R.string.weighing_capture_slot_optional)
    }
}

@Composable
private fun CaptureSlotRow(
    slot: WeighingCaptureSlotUi,
    statusText: String,
    done: Boolean,
    enabled: Boolean,
    onCapture: (photo: Boolean) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (done) MeshaColors.BrandTint else MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(text = slot.title, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
        if (slot.hint.isNotBlank()) Text(text = slot.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        Text(text = statusText, color = if (done) MeshaColors.Ok else MeshaColors.Muted, style = MeshaType.caption)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (slot.kind != "photo") {
                CaptureButton(stringResource(R.string.weighing_capture_record_video), enabled) { onCapture(false) }
            }
            if (slot.kind != "video") {
                CaptureButton(stringResource(R.string.weighing_capture_take_photo), enabled) { onCapture(true) }
            }
        }
    }
}

@Composable
private fun CaptureButton(text: String, enabled: Boolean, onClick: () -> Unit) {
    Text(
        text = text,
        color = if (enabled) MeshaColors.BrandD else MeshaColors.Faint,
        style = MeshaType.cta,
        textAlign = TextAlign.Center,
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(12.dp))
            .border(1.dp, if (enabled) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(12.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 10.dp),
    )
}

@Composable
private fun CaptureQuestionField(
    question: WeighingCaptureQuestionUi,
    answer: WeighingCaptureAnswerUi,
    locked: Boolean,
    onAnswer: (String) -> Unit,
    onToggle: (String) -> Unit,
    onOther: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.fillMaxWidth()) {
        Text(
            text = if (question.required) question.title else stringResource(R.string.weighing_capture_question_optional, question.title),
            color = MeshaColors.Ink,
            style = MeshaType.bodyStrong,
        )
        if (question.hint.isNotBlank()) Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        when (question.kind) {
            "choice", "multi" -> {
                question.options.forEach { (value, label) ->
                    val picked = if (question.kind == "choice") answer.value == value else value in answer.values
                    Text(
                        text = label,
                        color = if (picked) MeshaColors.BrandD else MeshaColors.Ink,
                        style = MeshaType.body,
                        modifier = Modifier
                            .fillMaxWidth()
                            .minimumInteractiveComponentSize()
                            .clip(RoundedCornerShape(10.dp))
                            .background(if (picked) MeshaColors.BrandTint else MeshaColors.Surf2)
                            .clickable(enabled = !locked) { if (question.kind == "choice") onAnswer(value) else onToggle(value) }
                            .padding(horizontal = 12.dp, vertical = 8.dp),
                    )
                }
                if (question.kind == "choice" && question.allowOther && answer.value == "other") {
                    OutlinedTextField(
                        value = answer.otherText,
                        onValueChange = onOther,
                        enabled = !locked,
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            else -> OutlinedTextField(
                value = answer.value,
                onValueChange = onAnswer,
                enabled = !locked,
                singleLine = question.kind == "number",
                suffix = if (question.unit.isNotBlank()) ({ Text(question.unit) }) else null,
                keyboardOptions = if (question.kind == "number") KeyboardOptions(keyboardType = KeyboardType.Decimal) else KeyboardOptions.Default,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}
