package sg.mesha.goatos.feature.penroutines

// telemetry:exempt pure stateless renderer; PenRoutineDetailViewModel (in :app) owns the
// pen_routine_* AnalyticsEventsPenRoutines + CrashReporter wiring for every refresh, check-in,
// capture, upload and submit.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * ONE routine task (`/pen-routines/{taskId}`, maintainer instruction 2026-09-16) — a hosted
 * drill with Up/Back and NO L0 chrome (Android navigation-stack invariant).
 *
 * Everything on it renders as the SERVER composed it: the title, the pen label, the reason and
 * evidence lines, the chip, the instruction, the presence line, every question and option, the
 * done line, the verifier's words, and whether the caller may check in or submit. The screen
 * decides nothing about who the caller is; it only lays the form out and reports taps.
 *
 * The order is the order of the work: check in to the pen (when the routine asks for it),
 * answer the questions, take the captures, submit. Submit is a real button here — unlike a pen
 * visit, a routine has several deliverables and the park head must be able to review them
 * together before the task leaves the phone.
 */
@Composable
fun PenRoutineDetailScreen(
    state: PenRoutineDetailUiState,
    onEvent: (PenRoutineDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(PenRoutineDetailEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            // The pen leads (the backend's own label, verbatim); the park sits under it. The
            // backend's full title is what the card and the recorder chrome carry.
            title = state.penLabel.ifBlank { state.title },
            subtitle = state.parkName.takeIf { it.isNotBlank() },
            onBack = { onEvent(PenRoutineDetailEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(PenRoutineDetailEvent.Refresh) },
                    contentDescription = stringResource(R.string.pen_routines_action_refresh),
                )
            },
        )
        if (state.loading) {
            Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(color = MeshaColors.BrandD)
            }
            return@Column
        }
        val formLive = state.phase == PenRoutinePhase.OPEN || state.phase == PenRoutinePhase.REWORK
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(top = 4.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item(key = "task") { PenRoutineTaskCard(state) }

            if (state.phase == PenRoutinePhase.REWORK) {
                item(key = "rework") { PenRoutineReworkCard(state.reworkReason) }
            }

            if (state.presenceRequired && formLive) {
                item(key = "presence") { PenRoutinePresenceCard(state, onEvent) }
            }

            if (formLive) {
                if (state.questions.isNotEmpty()) {
                    item(key = "questions_label") {
                        PenRoutineSectionLabel(stringResource(R.string.pen_routines_section_questions))
                    }
                }
                items(state.questions, key = { "question:" + it.id }) { question ->
                    PenRoutineQuestionCard(question = question, taskId = state.taskId, enabled = !state.capturing, onEvent = onEvent)
                }
                if (state.photoSlots.isNotEmpty()) {
                    item(key = "photos_label") {
                        PenRoutineSectionLabel(stringResource(R.string.pen_routines_section_photos))
                    }
                }
                items(state.photoSlots, key = { "slot:" + it.fieldKey }) { slot ->
                    PenRoutineSlotCard(taskId = state.taskId, slot = slot, enabled = !state.capturing, onEvent = onEvent)
                }
                if (state.videoSlots.isNotEmpty()) {
                    item(key = "videos_label") {
                        PenRoutineSectionLabel(stringResource(R.string.pen_routines_section_videos))
                    }
                }
                items(state.videoSlots, key = { "slot:" + it.fieldKey }) { slot ->
                    PenRoutineSlotCard(taskId = state.taskId, slot = slot, enabled = !state.capturing, onEvent = onEvent)
                }
                item(key = "submit") { PenRoutineSubmitCard(state, onEvent) }
            } else {
                item(key = "outcome") { PenRoutineOutcomeCard(state) }
            }

            if (state.message != null) {
                item(key = "message") {
                    Text(
                        text = state.message,
                        color = MeshaColors.Danger,
                        style = MeshaType.caption,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp)
                            .clickable { onEvent(PenRoutineDetailEvent.DismissMessage) },
                    )
                }
            }
        }
    }
}

@Composable
private fun PenRoutineSectionLabel(text: String) {
    Text(
        text = text,
        color = MeshaColors.Faint,
        style = MeshaType.sectionLabel,
        modifier = Modifier.fillMaxWidth().padding(horizontal = 32.dp),
    )
}

@Composable
private fun PenRoutineTaskCard(state: PenRoutineDetailUiState) {
    Column(modifier = penRoutineCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            PenRoutineStateChip(label = state.stateChip, tone = state.tone)
            Spacer(Modifier.weight(1f))
            if (state.capturing || state.phase == PenRoutinePhase.SENDING) PenRoutineInlineSpinner()
        }
        if (state.title.isNotBlank()) {
            Text(text = state.title, color = MeshaColors.Ink, style = MeshaType.cardTitle)
        }
        // Backend-composed reason line ("Every day"), verbatim -- why this pen, today.
        if (state.reasonLine.isNotBlank()) {
            Text(text = state.reasonLine, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
        }
        if (state.instruction.isNotBlank()) {
            Text(text = state.instruction, color = MeshaColors.Muted, style = MeshaType.body)
        }
        if (state.evidenceLine.isNotBlank()) {
            Text(text = state.evidenceLine, color = MeshaColors.Faint, style = MeshaType.caption)
        }
    }
}

/** The verifier's own words, verbatim, above the re-opened form. */
@Composable
private fun PenRoutineReworkCard(reworkReason: String) {
    Column(modifier = penRoutineCardModifier(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(
            text = stringResource(R.string.pen_routines_state_sent_back),
            color = MeshaColors.Danger,
            style = MeshaType.bodyStrong,
        )
        if (reworkReason.isNotBlank()) {
            Text(text = reworkReason, color = MeshaColors.Danger, style = MeshaType.caption)
        }
    }
}

/**
 * The presence step: the backend's own line ("Check in to the pen before you start" /
 * "In pen since 07:12") and, while a check-in is offered, the one primary button. Once in the
 * pen the button gives way to the line — there is no check-out button, the submit stamps it.
 */
@Composable
private fun PenRoutinePresenceCard(
    state: PenRoutineDetailUiState,
    onEvent: (PenRoutineDetailEvent) -> Unit,
) {
    Column(modifier = penRoutineCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text(
            text = stringResource(R.string.pen_routines_section_presence),
            color = MeshaColors.Faint,
            style = MeshaType.sectionLabel,
        )
        if (state.presenceLine.isNotBlank()) {
            Text(
                text = state.presenceLine,
                color = if (state.inPen) MeshaColors.Ok else MeshaColors.Ink,
                style = MeshaType.bodyStrong,
            )
        }
        if (state.checkingIn) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                PenRoutineInlineSpinner()
                Text(
                    text = stringResource(R.string.pen_routines_state_checking_in),
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                )
            }
        } else if (!state.inPen) {
            PenRoutinePrimaryButton(
                // A general park task has no pen to name: the button reads the neutral "Check in".
                label = stringResource(
                    if (state.parkTask) R.string.pen_routines_action_check_in_park else R.string.pen_routines_action_check_in,
                ),
                // ONLY the server's `can_check_in` arms the punch. A task the caller may not
                // work keeps a visible, dead button rather than none at all.
                enabled = state.canCheckIn,
                onClick = { onEvent(PenRoutineDetailEvent.CheckIn) },
                modifier = Modifier.fillMaxWidth(),
                icon = MeshaIcons.PenVisit,
            )
        }
    }
}

/** One question: title (with the required mark), hint, and the widget its kind calls for. */
@Composable
private fun PenRoutineQuestionCard(
    question: PenRoutineQuestionUi,
    taskId: String,
    enabled: Boolean,
    onEvent: (PenRoutineDetailEvent) -> Unit,
) {
    Column(modifier = penRoutineCardModifier(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.Top) {
            Text(
                text = question.title,
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
                modifier = Modifier.weight(1f),
            )
            if (question.required) {
                Text(
                    text = stringResource(R.string.pen_routines_required_mark),
                    color = MeshaColors.Danger,
                    style = MeshaType.bodyStrong,
                )
            }
        }
        if (question.hint.isNotBlank()) {
            Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        when (question.kind) {
            PenRoutineQuestionKind.YES_NO, PenRoutineQuestionKind.CHOICE -> PenRoutineChoiceRow(
                options = question.options,
                selected = question.selected,
                onPick = { value -> onEvent(PenRoutineDetailEvent.SetChoice(question.id, value)) },
            )
            PenRoutineQuestionKind.MULTI_CHOICE -> PenRoutineChoiceRow(
                options = question.options,
                selected = question.selected,
                onPick = { value -> onEvent(PenRoutineDetailEvent.ToggleChoice(question.id, value)) },
            )
            PenRoutineQuestionKind.NUMBER -> PenRoutineAnswerField(
                value = question.text,
                onValueChange = { onEvent(PenRoutineDetailEvent.SetText(question.id, it)) },
                label = question.unit.ifBlank { stringResource(R.string.pen_routines_field_number) },
                supporting = penRoutineRangeHint(question),
                isError = question.invalid,
                numeric = true,
            )
            PenRoutineQuestionKind.TEXT -> PenRoutineAnswerField(
                value = question.text,
                onValueChange = { onEvent(PenRoutineDetailEvent.SetText(question.id, it)) },
                label = stringResource(R.string.pen_routines_field_text),
                supporting = null,
                isError = false,
                numeric = false,
            )
        }
        // The question's own captures (maintainer instruction 2026-09-18): the proof that
        // answers THIS question sits under it, never in the task-wide photo/video lists.
        question.proofSlots.forEach { slot ->
            PenRoutineSlotBody(
                taskId = taskId,
                slot = slot,
                enabled = enabled,
                onEvent = onEvent,
                framed = false,
            )
        }
    }
}

/** The min/max hint under a NUMBER field, from the backend's own numbers; null when it set none. */
@Composable
private fun penRoutineRangeHint(question: PenRoutineQuestionUi): String? {
    val min = question.min
    val max = question.max
    return when {
        min != null && max != null -> stringResource(R.string.pen_routines_range_between, penRoutineNumber(min), penRoutineNumber(max))
        min != null -> stringResource(R.string.pen_routines_range_min, penRoutineNumber(min))
        max != null -> stringResource(R.string.pen_routines_range_max, penRoutineNumber(max))
        else -> null
    }
}

private fun penRoutineNumber(value: Double): String =
    if (value == Math.floor(value) && !value.isInfinite()) value.toLong().toString() else value.toString()

/** Selectable chips from the backend's own options. Single- and multi-select share the shape;
 *  the ViewModel decides whether a pick replaces or toggles. */
@Composable
private fun PenRoutineChoiceRow(
    options: List<PenRoutineOptionUi>,
    selected: List<String>,
    onPick: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { option -> // compose-guard:ignore: bounded authored option list (yes/no, a handful of choices)
            val isSelected = option.value in selected
            val background = if (isSelected) MeshaColors.BrandTint else MeshaColors.Surf
            val border = if (isSelected) MeshaColors.BrandD else MeshaColors.Hair
            val labelColor = if (isSelected) MeshaColors.BrandD else MeshaColors.Ink
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 44.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(background)
                    .border(1.dp, border, RoundedCornerShape(12.dp))
                    .clickable(role = Role.RadioButton, onClick = { onPick(option.value) })
                    .padding(horizontal = 14.dp, vertical = 10.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (isSelected) {
                    Icon(imageVector = MeshaIcons.Check, contentDescription = null, tint = labelColor, modifier = Modifier.size(16.dp))
                }
                Text(text = option.label, color = labelColor, style = MeshaType.body)
            }
        }
    }
}

@Composable
private fun PenRoutineAnswerField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    supporting: String?,
    isError: Boolean,
    numeric: Boolean,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier.fillMaxWidth().heightIn(min = 54.dp),
        label = { Text(label) },
        supportingText = supporting?.let { { Text(text = it, color = if (isError) MeshaColors.Danger else MeshaColors.Faint, style = MeshaType.caption) } },
        isError = isError,
        singleLine = numeric,
        minLines = if (numeric) 1 else 3,
        maxLines = if (numeric) 1 else 8,
        shape = RoundedCornerShape(14.dp),
        keyboardOptions = KeyboardOptions(keyboardType = if (numeric) KeyboardType.Decimal else KeyboardType.Text),
        colors = OutlinedTextFieldDefaults.colors(
            focusedTextColor = MeshaColors.Ink,
            unfocusedTextColor = MeshaColors.Ink,
            focusedBorderColor = MeshaColors.BrandD,
            unfocusedBorderColor = MeshaColors.Hair,
            focusedLabelColor = MeshaColors.BrandD,
            unfocusedLabelColor = MeshaColors.Muted,
            cursorColor = MeshaColors.BrandD,
        ),
    )
}

/**
 * One capture slot. The state comes from :app's read of Room — the durable proof row and its
 * upload — so a capture already taken is shown back instead of asked for again.
 */
@Composable
private fun PenRoutineSlotCard(
    taskId: String,
    slot: PenRoutineSlotUi,
    enabled: Boolean,
    onEvent: (PenRoutineDetailEvent) -> Unit,
) {
    PenRoutineSlotBody(taskId = taskId, slot = slot, enabled = enabled, onEvent = onEvent, framed = true)
}

/**
 * One capture slot: its label, preview, pipeline state and camera button(s). [framed] draws it
 * as its own card (the task-wide lists); unframed it nests inside a question's card. An empty
 * slot carrying [PenRoutineSlotUi.videoFieldKey] belongs to a photo-or-video question and
 * offers the recorder beside the camera.
 */
@Composable
private fun PenRoutineSlotBody(
    taskId: String,
    slot: PenRoutineSlotUi,
    enabled: Boolean,
    onEvent: (PenRoutineDetailEvent) -> Unit,
    framed: Boolean,
) {
    val isPhoto = slot.kind == PenRoutineSlotKind.PHOTO
    val modifier = if (framed) penRoutineCardModifier() else Modifier.fillMaxWidth()
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = if (isPhoto) {
                    stringResource(R.string.pen_routines_slot_photo, slot.index)
                } else {
                    stringResource(R.string.pen_routines_slot_video, slot.index)
                },
                color = MeshaColors.Ink,
                style = MeshaType.bodyStrong,
                modifier = Modifier.weight(1f),
            )
            if (slot.required) {
                Text(
                    text = stringResource(R.string.pen_routines_required_mark),
                    color = MeshaColors.Danger,
                    style = MeshaType.bodyStrong,
                )
            }
        }
        if (slot.previewPath.isNotBlank()) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(200.dp)
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf2),
                contentAlignment = Alignment.Center,
            ) {
                ProofMediaPreview(
                    path = slot.previewPath,
                    kind = if (isPhoto) ProofMediaPreviewKind.Photo else ProofMediaPreviewKind.Video,
                    mediaIdentity = "pen_routine:$taskId:${slot.fieldKey}",
                    onPreviewAction = { action -> onEvent(PenRoutineDetailEvent.ProofPreviewAction(slot.fieldKey, action)) },
                )
            }
        }
        if (slot.working) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                PenRoutineInlineSpinner()
                Text(
                    text = slot.progressLabel.ifBlank { stringResource(R.string.pen_routines_state_sending) },
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                )
            }
        }
        if (slot.failureReason.isNotBlank()) {
            Text(text = slot.failureReason, color = MeshaColors.Danger, style = MeshaType.caption)
        }
        val taken = slot.previewPath.isNotBlank()
        val label = when {
            taken && isPhoto -> stringResource(R.string.pen_routines_action_photo_again)
            taken -> stringResource(R.string.pen_routines_action_video_again)
            isPhoto -> stringResource(R.string.pen_routines_action_take_photo)
            else -> stringResource(R.string.pen_routines_action_record_video)
        }
        if (taken) {
            PenRoutineGhostButton(
                label = label,
                enabled = enabled,
                onClick = { onEvent(PenRoutineDetailEvent.CaptureSlot(slot.fieldKey)) },
                modifier = Modifier.fillMaxWidth(),
                icon = if (isPhoto) MeshaIcons.Camera else MeshaIcons.Video,
            )
        } else {
            PenRoutinePrimaryButton(
                label = label,
                enabled = enabled,
                onClick = { onEvent(PenRoutineDetailEvent.CaptureSlot(slot.fieldKey)) },
                modifier = Modifier.fillMaxWidth(),
                icon = if (isPhoto) MeshaIcons.Camera else MeshaIcons.Video,
            )
            if (slot.videoFieldKey.isNotBlank()) {
                PenRoutineGhostButton(
                    label = stringResource(R.string.pen_routines_action_record_video),
                    enabled = enabled,
                    onClick = { onEvent(PenRoutineDetailEvent.CaptureSlot(slot.videoFieldKey)) },
                    modifier = Modifier.fillMaxWidth(),
                    icon = MeshaIcons.Video,
                )
            }
        }
    }
}

/** The Submit button, dead (never hidden) until every gate the ViewModel checks is satisfied. */
@Composable
private fun PenRoutineSubmitCard(
    state: PenRoutineDetailUiState,
    onEvent: (PenRoutineDetailEvent) -> Unit,
) {
    Column(modifier = penRoutineCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (state.failureReason.isNotBlank()) {
            Text(text = state.failureReason, color = MeshaColors.Danger, style = MeshaType.caption)
        }
        if (state.phase == PenRoutinePhase.SENDING) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                PenRoutineInlineSpinner()
                Text(
                    text = stringResource(R.string.pen_routines_state_sending),
                    color = MeshaColors.Muted,
                    style = MeshaType.caption,
                )
            }
        }
        PenRoutinePrimaryButton(
            label = stringResource(R.string.pen_routines_action_submit),
            enabled = state.submitEnabled && !state.capturing,
            onClick = { onEvent(PenRoutineDetailEvent.Submit) },
            modifier = Modifier.fillMaxWidth(),
            icon = MeshaIcons.CheckCircle,
        )
    }
}

/** The read-only view once the task left the form: the backend's answer rows and its lines. */
@Composable
private fun PenRoutineOutcomeCard(state: PenRoutineDetailUiState) {
    Column(modifier = penRoutineCardModifier(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        val (line, color) = when (state.phase) {
            PenRoutinePhase.SENDING -> stringResource(R.string.pen_routines_state_sending) to MeshaColors.Muted
            PenRoutinePhase.IN_REVIEW -> stringResource(R.string.pen_routines_state_in_review) to MeshaColors.Muted
            PenRoutinePhase.DONE -> stringResource(R.string.pen_routines_state_done) to MeshaColors.Ok
            else -> stringResource(R.string.pen_routines_state_locked) to MeshaColors.Muted
        }
        Row(modifier = Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            if (state.phase == PenRoutinePhase.SENDING) PenRoutineInlineSpinner()
            Text(text = line, color = color, style = MeshaType.bodyStrong)
        }
        // Backend-composed done line ("Done 16 Sep by …"), verbatim.
        if (state.doneLine.isNotBlank()) {
            Text(text = state.doneLine, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        if (state.answerRows.isNotEmpty()) {
            Spacer(Modifier.height(2.dp))
            Text(
                text = stringResource(R.string.pen_routines_section_answers),
                color = MeshaColors.Faint,
                style = MeshaType.sectionLabel,
            )
            state.answerRows.forEach { row -> // compose-guard:ignore: bounded authored question list, rendered once read-only
                Column(modifier = Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(text = row.title, color = MeshaColors.Muted, style = MeshaType.caption)
                    Text(text = row.value, color = MeshaColors.Ink, style = MeshaType.body)
                }
            }
        }
        if (state.proofCount > 0) {
            Text(
                text = stringResource(R.string.pen_routines_proofs_count, state.proofCount),
                color = MeshaColors.Faint,
                style = MeshaType.caption,
            )
        }
    }
}
