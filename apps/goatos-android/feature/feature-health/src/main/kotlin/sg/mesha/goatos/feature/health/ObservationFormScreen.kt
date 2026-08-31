package sg.mesha.goatos.feature.health

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.component.MeshaSectionLabel
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * The observation form.
 *
 * It is laid out the way a person walks an animal — vitals, head, then down the
 * body, then the sex-specific checks — rather than grouped by data type. The
 * operator is holding a sick goat; the screen should follow their hands.
 *
 * It is walked in FOUR STEPS rather than as one 29-question scroll. The questions
 * and the completeness rule are unchanged; what the steps add is a horizon. A
 * single scroll gave the operator no idea how much was left, put the submit button
 * below every question, and reported what was missing as a 29-name comma list at
 * the bottom. Now each step gates its own "Next", so a gap is found where the
 * animal still is rather than after the whole pass.
 *
 * Questions that describe ONE PART of the animal are asked as one card with
 * several answers — the mouth is one look, not three yes/no rows for scabs, froth
 * and a jaw that will not open. A finding whose SITE or CHARACTER the engine acts
 * on keeps its own card, because those answers are what separate one diagnosis
 * from another and must not be folded away behind a yes. Every card is one tap for
 * a healthy animal.
 *
 * There is no disease anywhere on it. The manager records what they see; the
 * assessment comes back from the server and the Director decides.
 */

@Immutable
data class ObservationScreenState(
    val goatDisplayId: String = "",
    val goatTag: String = "",
    val locationLabel: String = "",
    val form: ObservationFormState = ObservationFormState(),
    val submitting: Boolean = false,
    val message: String? = null,
)

sealed interface ObservationFormEvent {
    /**
     * The assessment came back. Carries the run so the screen can open it.
     *
     * Emitted when the queued observation actually reaches the server, which may be
     * moments later or hours later out of a shed with no signal — never at the
     * moment of tapping submit, because at that point no assessment exists.
     */
    data class Assessed(val diagnosisRunId: String) : ObservationFormEvent

    data class UpdateForm(val form: ObservationFormState) : ObservationFormEvent
    data object Submit : ObservationFormEvent
    data object Back : ObservationFormEvent
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun ObservationFormScreen(
    state: ObservationScreenState,
    onEvent: (ObservationFormEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    val form = state.form
    val steps = ObservationStep.entries

    // Step is screen-local: it is where the operator has scrolled to, not a fact
    // about the animal, so it belongs here rather than in the view model. Saveable
    // so a rotation or a process death does not drop them back to the temperature.
    var stepIndex by rememberSaveable { mutableStateOf(0) }
    stepIndex = stepIndex.coerceIn(0, steps.lastIndex)
    val step = steps[stepIndex]
    val isLastStep = stepIndex == steps.lastIndex

    val missingHere = form.missingFields(step)
    val blockers = form.blockers()
    val contradictions = blockers.filter { it != ObservationBlocker.INCOMPLETE }

    // Back walks the form backwards before it leaves it. A manager who taps back on
    // step three means "let me fix the head", not "throw this animal's check away".
    BackHandler(enabled = stepIndex > 0) { stepIndex-- }

    val listState = rememberLazyListState()
    LaunchedEffect(stepIndex) { listState.scrollToItem(0) }

    // Every answer is written onto the LATEST form, never onto the copy this
    // composition captured.
    //
    // Without this the questions clobber each other: each card's handler holds the
    // form as it was when that card was last composed, so answering the skin tent
    // wrote `formWithoutTemperature.copy(skinTent = ...)` and silently blanked the
    // temperature that had just been typed — then typing the temperature blanked the
    // skin tent back. Observed on a real phone, both directions. A form that quietly
    // drops an answer the operator already gave is worse than one that asks twice,
    // because "Still to check" then names a field they are sure they answered.
    val latestForm by rememberUpdatedState(form)
    fun update(block: ObservationFormState.() -> ObservationFormState) {
        onEvent(ObservationFormEvent.UpdateForm(latestForm.block()))
    }

    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Observation form",
            eyebrow = listOfNotNull(
                state.goatTag.takeIf { it.isNotBlank() } ?: state.goatDisplayId.takeIf { it.isNotBlank() },
                form.sex.takeIf { it.isNotBlank() }?.uppercase(),
            ).joinToString(" · ").ifBlank { "NEW HEALTH CHECK" },
            onBack = { if (stepIndex > 0) stepIndex-- else onEvent(ObservationFormEvent.Back) },
            below = { StepProgress(current = stepIndex, total = steps.size) },
        )

        LazyColumn(
            state = listState,
            modifier = Modifier.weight(1f).fillMaxWidth(),
            contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item { RequiredBanner(form) }

            when (step) {
                ObservationStep.VITALS -> vitalsStep(form, ::update)
                ObservationStep.HEAD -> headStep(form, ::update)
                ObservationStep.BODY -> bodyStep(form, ::update)
                ObservationStep.FINAL -> finalStep(form, ::update)
            }

            if (contradictions.isNotEmpty()) {
                item { ContradictionNotice(contradictions) }
            }
            state.message?.let { item { Text(it, style = MeshaType.body, color = MeshaColors.BrandD) } }
        }

        StepFooter(
            isLastStep = isLastStep,
            submitting = state.submitting,
            missingHere = missingHere,
            // The last step's button submits the WHOLE form, so it answers to the
            // whole form's rules — a contradiction two steps back must still stop it.
            canSubmit = form.canSubmit(),
            onNext = { if (stepIndex < steps.lastIndex) stepIndex++ },
            onSubmit = { onEvent(ObservationFormEvent.Submit) },
        )
    }
}

// --- Steps.

private fun LazyListScope.vitalsStep(
    form: ObservationFormState,
    update: ((ObservationFormState.() -> ObservationFormState) -> Unit),
) {
    heading(form.stepHeading(ObservationStep.VITALS))

    item {
        QuestionCard("Rectal temperature", trailing = "°F") {
            TemperatureField(form.temp) { update { copy(temp = it) } }
        }
    }
    item {
        // FAMACHA and the yellow-membrane check are one look at the same eyelid, so
        // they are one card. The score carries its own tone: 1–2 is a healthy pink,
        // 3 is the watch point, 4–5 is the anaemia that kills.
        //
        // Scoring the eyelid also SETTLES the yellow question as "no", because that
        // is the same look. Without it, "Yellow" would be a three-state chip an
        // operator could only answer "no" to by tapping it on and off again — and
        // nothing on screen would tell them the blank one was still owed.
        QuestionCard("FAMACHA") {
            ChipFlow {
                ObservationOptions.famacha.forEach { score ->
                    ToneChip(score, selected = form.famacha == score, tone = famachaTone(score)) {
                        update { copy(famacha = score, yellow = yellow ?: false) }
                    }
                }
                ToneChip("Yellow", selected = form.yellow == true, tone = ChipTone.WARN) {
                    update { copy(yellow = form.yellow != true) }
                }
            }
        }
    }
    item {
        QuestionCard("Skin tent") {
            SingleChoice(ObservationOptions.skinTent, form.skinTent, ::skinTentLabel, normal = "lt2") {
                update { copy(skinTent = it) }
            }
        }
    }
    item {
        QuestionCard("Sunken flank") {
            YesNo(form.stomachInside) { update { copy(stomachInside = it) } }
        }
    }
}

private fun LazyListScope.headStep(
    form: ObservationFormState,
    update: ((ObservationFormState.() -> ObservationFormState) -> Unit),
) {
    heading(form.stepHeading(ObservationStep.HEAD))

    item {
        QuestionCard("Eyes") {
            MultiChoice(ObservationOptions.eyes, form.eyes, setOf("normal"), ::plainLabel, normal = "normal") {
                update { copy(eyes = it) }
            }
        }
    }
    item {
        // The mouth is ONE look. Scabs, froth and a jaw that will not open used to be
        // three separate questions, which asked the operator to open the same mouth
        // three times. Selecting nothing but "Normal" answers all three at once.
        QuestionCard("Mouth") {
            val scabs = form.mouth == "orf_scabs"
            val froth = form.frothyMouth == true
            val locked = form.lockedJaw == true
            val allClear = form.mouth == "normal" && form.frothyMouth == false && form.lockedJaw == false
            ChipFlow {
                ToneChip("Normal", selected = allClear, tone = ChipTone.OK) {
                    update { copy(mouth = "normal", frothyMouth = false, lockedJaw = false) }
                }
                ToneChip("Scabs", selected = scabs, tone = ChipTone.WARN) {
                    update {
                        copy(
                            mouth = if (scabs) "normal" else "orf_scabs",
                            frothyMouth = frothyMouth ?: false,
                            lockedJaw = lockedJaw ?: false,
                        )
                    }
                }
                ToneChip("Froth", selected = froth, tone = ChipTone.WARN) {
                    update { copy(frothyMouth = !froth, mouth = mouth.ifBlank { "normal" }, lockedJaw = lockedJaw ?: false) }
                }
                ToneChip("Cannot open", selected = locked, tone = ChipTone.DANGER) {
                    update { copy(lockedJaw = !locked, mouth = mouth.ifBlank { "normal" }, frothyMouth = frothyMouth ?: false) }
                }
            }
        }
    }
    item {
        QuestionCard("Breathing") {
            MultiChoice(ObservationOptions.breathing, form.breathing, setOf("normal"), ::breathingLabel, normal = "normal") {
                update { copy(breathing = it) }
            }
        }
    }
    item {
        QuestionCard("Runny nose") {
            YesNo(form.nasal) { update { copy(nasal = it) } }
        }
    }
}

private fun LazyListScope.bodyStep(
    form: ObservationFormState,
    update: ((ObservationFormState.() -> ObservationFormState) -> Unit),
) {
    heading("Gut · belly")

    item {
        QuestionCard("Left side") {
            MultiChoice(ObservationOptions.leftStomach, form.leftStomach, setOf("normal"), ::stomachLabel, normal = "normal") {
                update { copy(leftStomach = it) }
            }
        }
    }
    item {
        QuestionCard("Rumen movement") {
            SingleChoice(ObservationOptions.rumenMovement, form.rumenMovement, ::rumenLabel, normal = "felt") {
                update { copy(rumenMovement = it) }
            }
        }
    }
    item {
        QuestionCard("Loose motion") {
            YesNo(form.diarrhea) { update { copy(diarrhea = it) } }
        }
    }
    item {
        QuestionCard("Eating") {
            MultiChoice(
                ObservationOptions.eating, form.eating, setOf("normal", "not_eating"), ::eatingLabel, normal = "normal",
            ) { update { copy(eating = it) } }
        }
    }

    heading("Skin · body · legs")

    item {
        // Ticks and hair loss are one pass of the hand over the coat.
        QuestionCard("Skin & coat") {
            val ticks = form.ticks == true
            val hairloss = form.hairloss == true
            ChipFlow {
                ToneChip("Normal", selected = form.ticks == false && form.hairloss == false, tone = ChipTone.OK) {
                    update { copy(ticks = false, hairloss = false) }
                }
                ToneChip("Ticks", selected = ticks, tone = ChipTone.WARN) {
                    update { copy(ticks = !ticks, hairloss = hairloss ?: false) }
                }
                ToneChip("Hair loss", selected = hairloss, tone = ChipTone.WARN) {
                    update { copy(hairloss = !hairloss, ticks = ticks ?: false) }
                }
            }
        }
    }
    // Wounds, lumps, rashes and maggots stay FOUR questions rather than one
    // screening chip with a follow-up. The site of a wound and the character of a
    // rash are what separate one diagnosis from another, so they cannot be folded
    // away behind a yes; and a single card could not offer "no rash" once a wound
    // had been ticked. Each is still one tap for a healthy animal.
    item {
        QuestionCard("Wounds") {
            MultiChoice(ObservationOptions.wounds, form.wounds, setOf("no"), ::woundLabel, normal = "no") {
                update { copy(wounds = it) }
            }
        }
    }
    item {
        QuestionCard("Lumps") {
            SingleChoice(ObservationOptions.lumps, form.lumps, ::lumpLabel, normal = "no") {
                update { copy(lumps = it) }
            }
        }
    }
    item {
        QuestionCard("Rashes") {
            SingleChoice(ObservationOptions.rashCharacter, form.rashCharacter, ::rashLabel, normal = "none") {
                update { copy(rashCharacter = it) }
            }
        }
    }
    item {
        // Maggots on the body and maggots at the ear tag are one look over the
        // animal, and a torn tag is what lets the second one start.
        QuestionCard("Maggots and ear tag") {
            val body = form.flystrike == true
            val tag = form.eartagFlystrike == true
            val torn = form.eartagWound == true
            ChipFlow {
                ToneChip(
                    "Normal",
                    selected = form.flystrike == false && form.eartagFlystrike == false && form.eartagWound == false,
                    tone = ChipTone.OK,
                ) { update { copy(flystrike = false, eartagFlystrike = false, eartagWound = false) } }
                ToneChip("On the body", selected = body, tone = ChipTone.DANGER) {
                    update { copy(flystrike = !body, eartagFlystrike = eartagFlystrike ?: false, eartagWound = eartagWound ?: false) }
                }
                ToneChip("At the ear tag", selected = tag, tone = ChipTone.DANGER) {
                    update { copy(eartagFlystrike = !tag, flystrike = flystrike ?: false, eartagWound = eartagWound ?: false) }
                }
                ToneChip("Ear tag torn", selected = torn, tone = ChipTone.WARN) {
                    update { copy(eartagWound = !torn, flystrike = flystrike ?: false, eartagFlystrike = eartagFlystrike ?: false) }
                }
            }
        }
    }
    item {
        QuestionCard("How it stands") {
            SingleChoice(ObservationOptions.activity, form.activity, ::activityLabel, normal = "standing") {
                update { copy(activity = it) }
            }
        }
    }
    item {
        QuestionCard("Legs and feet") {
            SingleChoice(ObservationOptions.leg, form.leg, ::legLabel, normal = "normal") {
                update { copy(leg = it) }
            }
        }
    }
    item {
        QuestionCard("Nervous signs") {
            MultiChoice(ObservationOptions.neuro, form.neuro, setOf("none"), ::neuroLabel, normal = "none") {
                update { copy(neuro = it) }
            }
        }
    }
}

private fun LazyListScope.finalStep(
    form: ObservationFormState,
    update: ((ObservationFormState.() -> ObservationFormState) -> Unit),
) {
    heading(form.stepHeading(ObservationStep.FINAL))

    if (form.isFemale) {
        item {
            QuestionCard("Udder") {
                SingleChoice(ObservationOptions.udder, form.udder, ::udderLabel, normal = "normal") {
                    update { copy(udder = it) }
                }
            }
        }
        item {
            QuestionCard("Milk") {
                // Answering "no milk" also drops any milk test already recorded.
                // Without that the card below vanishes while its answer stays, and
                // the operator is left holding a CMT_WITHOUT_MILK contradiction with
                // no control on screen to clear it.
                SingleChoice(ObservationOptions.lactation, form.lactation, ::lactationLabel, normal = "no") {
                    update { copy(lactation = it, cmt = if (it == "no") "" else cmt) }
                }
            }
        }
        if (form.cmtApplies) {
            item {
                // The subtitle answers the question the card raises: a milk test that
                // appears from nowhere reads as a form defect.
                QuestionCard("Milk test", subtitle = "asked because there is milk") {
                    SingleChoice(ObservationOptions.cmt, form.cmt, ::cmtLabel, normal = "neg") {
                        update { copy(cmt = it) }
                    }
                }
            }
        }
        item {
            QuestionCard("Back passage") {
                SingleChoice(ObservationOptions.vulva, form.vulva, ::vulvaLabel, normal = "none") {
                    update { copy(vulva = it) }
                }
            }
        }
    }
    if (form.isMale) {
        item {
            QuestionCard("Passing urine") {
                SingleChoice(ObservationOptions.straining, form.straining, ::strainingLabel, normal = "no") {
                    update { copy(straining = it) }
                }
            }
        }
    }
    item {
        // Three unrelated whole-body checks that each take one glance.
        QuestionCard("Anything else") {
            val red = form.redUrine == true
            val edema = form.bodyEdema == true
            val pushed = form.competition == true
            ChipFlow {
                ToneChip(
                    "None",
                    selected = form.redUrine == false && form.bodyEdema == false && form.competition == false,
                    tone = ChipTone.OK,
                ) { update { copy(redUrine = false, bodyEdema = false, competition = false) } }
                ToneChip("Red urine", selected = red, tone = ChipTone.DANGER) {
                    update { copy(redUrine = !red, bodyEdema = bodyEdema ?: false, competition = competition ?: false) }
                }
                ToneChip("Swelling under the jaw", selected = edema, tone = ChipTone.WARN) {
                    update { copy(bodyEdema = !edema, redUrine = redUrine ?: false, competition = competition ?: false) }
                }
                ToneChip("Pushed off feed", selected = pushed, tone = ChipTone.WARN) {
                    update { copy(competition = !pushed, redUrine = redUrine ?: false, bodyEdema = bodyEdema ?: false) }
                }
            }
        }
    }
}

// --- Chrome.

/** One filled segment per completed step, the way the mock paces the form. */
@Composable
private fun StepProgress(current: Int, total: Int) {
    Row(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp),
    ) {
        repeat(total) { index ->
            Box(
                Modifier
                    .weight(1f)
                    .height(4.dp)
                    .clip(RoundedCornerShape(2.dp))
                    .background(if (index <= current) MeshaColors.Brand else MeshaColors.Surf3),
            )
        }
    }
}

/**
 * States the rule the whole form runs on, and which half of the sex-specific
 * questions this animal is being asked.
 */
@Composable
private fun RequiredBanner(form: ObservationFormState) {
    val sexNote = when {
        form.isFemale -> "Female fields shown; male-only fields hidden."
        form.isMale -> "Male fields shown; female-only fields hidden."
        else -> "Sex-specific fields appear once this animal's record loads."
    }
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.BrandTint)
            .border(1.dp, MeshaColors.Brand.copy(alpha = 0.35f), RoundedCornerShape(14.dp))
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(
            text = if (form.isMale) "♂" else "♀",
            style = MeshaType.body,
            color = MeshaColors.Brand,
        )
        Text(
            text = "Answer every question. $sexNote",
            style = MeshaType.bodyStrong,
            color = MeshaColors.BrandD,
        )
    }
}

/** The uppercase step heading, e.g. HEAD · EYES · BREATHING. */
private fun LazyListScope.heading(text: String) {
    item { MeshaSectionLabel(text, modifier = Modifier.padding(top = 6.dp, bottom = 2.dp)) }
}

/** One question: a title, an optional note, and its answers. */
@Composable
private fun QuestionCard(
    title: String,
    subtitle: String? = null,
    trailing: String? = null,
    content: @Composable () -> Unit,
) {
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(title, style = MeshaType.cardTitle, color = MeshaColors.Ink)
                trailing?.let { Text(it, style = MeshaType.caption, color = MeshaColors.Muted) }
                subtitle?.let { Text(it, style = MeshaType.caption, color = MeshaColors.Faint) }
            }
            content()
        }
    }
}

/**
 * The submit bar, pinned so it is reachable without scrolling past every question.
 *
 * It names what is still missing IN THIS STEP rather than in the whole form. The
 * old screen listed all 29 field names in one comma run at the very bottom, which
 * told an operator that something was unanswered without telling them where.
 */
@Composable
private fun StepFooter(
    isLastStep: Boolean,
    submitting: Boolean,
    missingHere: List<String>,
    canSubmit: Boolean,
    onNext: () -> Unit,
    onSubmit: () -> Unit,
) {
    val ready = missingHere.isEmpty()
    Column(
        verticalArrangement = Arrangement.spacedBy(8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            .fillMaxWidth()
            .background(MeshaColors.Bg)
            .padding(horizontal = 16.dp, vertical = 12.dp),
    ) {
        MeshaPrimaryButton(
            text = when {
                submitting -> "Sending…"
                isLastStep -> "Submit observation"
                else -> "Next"
            },
            onClick = if (isLastStep) onSubmit else onNext,
            enabled = ready && !submitting && (!isLastStep || canSubmit),
            modifier = Modifier.fillMaxWidth(),
        )
        Text(
            text = when {
                !ready -> "Still to check: " + missingHere.joinToString(", ")
                isLastStep && canSubmit -> "Every field answered · nothing left blank"
                isLastStep -> "Fix the contradiction above to submit"
                else -> "This part is done"
            },
            style = MeshaType.caption,
            color = if (ready) MeshaColors.Faint else MeshaColors.Warn,
            textAlign = TextAlign.Center,
        )
    }
}

/** Names a contradiction as a contradiction rather than as a missing answer. */
@Composable
private fun ContradictionNotice(blockers: List<ObservationBlocker>) {
    MeshaCard(accent = MeshaColors.Warn) {
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            blockers.forEach {
                Text(blockerText(it), style = MeshaType.bodyStrong, color = MeshaColors.Warn)
            }
        }
    }
}

private fun blockerText(blocker: ObservationBlocker): String = when (blocker) {
    ObservationBlocker.NOT_EATING_WITH_FEED ->
        "You marked the animal as not eating and also as taking feed. Pick one."
    ObservationBlocker.WOUNDS_EXCLUSIVE ->
        "You marked no wounds and also a wound. Pick one."
    ObservationBlocker.FEMALE_STRAINING ->
        "Straining to urinate is not recorded for a female."
    ObservationBlocker.CMT_WITHOUT_MILK ->
        "There is no milk, so the milk test cannot be read."
    ObservationBlocker.INCOMPLETE -> ""
}

// --- Inputs.

@Composable
private fun TemperatureField(value: String, onChange: (String) -> Unit) {
    val reading = value.toDoubleOrNull()
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        TextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            textStyle = MeshaType.body.copy(
                fontSize = 30.sp,
                fontWeight = FontWeight.W700,
                fontFamily = FontFamily.Monospace,
            ),
            colors = TextFieldDefaults.colors(
                focusedContainerColor = MeshaColors.Surf2,
                unfocusedContainerColor = MeshaColors.Surf2,
                focusedTextColor = MeshaColors.Ink,
                unfocusedTextColor = MeshaColors.Ink,
                cursorColor = MeshaColors.Brand,
                // Material draws an underline under a filled field. The rest of this
                // screen has none, so it read as a stray rule across the card.
                focusedIndicatorColor = Color.Transparent,
                unfocusedIndicatorColor = Color.Transparent,
                disabledIndicatorColor = Color.Transparent,
            ),
            shape = RoundedCornerShape(12.dp),
            modifier = Modifier.fillMaxWidth(),
        )
        // The threshold is an ACTION, not a diagnosis: the form never names a
        // disease, but a manager holding a cold kid should not wait for the server
        // to tell them to warm it.
        Text(
            text = "Below 100 warm it now · above 106 cool it now",
            style = MeshaType.caption,
            color = if (reading != null && (reading < 100.0 || reading > 106.0)) MeshaColors.Danger else MeshaColors.Faint,
        )
    }
}

@Composable
private fun YesNo(value: Boolean?, onChange: (Boolean) -> Unit) {
    ChipFlow {
        ToneChip("No", selected = value == false, tone = ChipTone.OK) { onChange(false) }
        ToneChip("Yes", selected = value == true, tone = ChipTone.WARN) { onChange(true) }
    }
}

@Composable
private fun SingleChoice(
    options: List<String>,
    selected: String,
    labelFor: (String) -> String,
    normal: String?,
    onChange: (String) -> Unit,
) {
    ChipFlow {
        options.forEach { option ->
            ToneChip(
                labelFor(option),
                selected = selected == option,
                tone = if (option == normal) ChipTone.OK else ChipTone.WARN,
            ) { onChange(option) }
        }
    }
}

@Composable
private fun MultiChoice(
    options: List<String>,
    selected: Set<String>,
    exclusive: Set<String>,
    labelFor: (String) -> String,
    normal: String?,
    onChange: (Set<String>) -> Unit,
) {
    ChipFlow {
        options.forEach { option ->
            ToneChip(
                labelFor(option),
                selected = option in selected,
                tone = if (option == normal) ChipTone.OK else ChipTone.WARN,
            ) { onChange(toggleMultiValue(selected, option, exclusive)) }
        }
    }
}

/**
 * Answers wrap to their own width rather than being forced into two columns.
 *
 * The old grid gave "Dragging back legs" and "Bone out of place" half a phone
 * each, so long answers wrapped or truncated while "Yes" and "No" sat in the same
 * oversized boxes. Wrapping by content keeps every answer readable and fits more
 * of them on screen.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ChipFlow(content: @Composable () -> Unit) {
    FlowRow(
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        modifier = Modifier.fillMaxWidth(),
    ) { content() }
}

/**
 * What a selected answer MEANS, carried in its colour.
 *
 * Green is reassuring, amber is a finding worth acting on, red is one that will
 * not wait. An unselected chip is neutral, so a filled-in card reads at a glance:
 * all green is a healthy animal, any amber is why this check was raised.
 */
private enum class ChipTone { OK, WARN, DANGER }

private fun famachaTone(score: String): ChipTone = when (score) {
    "1", "2" -> ChipTone.OK
    "3" -> ChipTone.WARN
    else -> ChipTone.DANGER
}

@Composable
private fun ToneChip(text: String, selected: Boolean, tone: ChipTone, onClick: () -> Unit) {
    val accent: Color = when (tone) {
        ChipTone.OK -> MeshaColors.Ok
        ChipTone.WARN -> MeshaColors.Warn
        ChipTone.DANGER -> MeshaColors.Danger
    }
    val fill: Color = if (selected) {
        when (tone) {
            ChipTone.OK -> MeshaColors.OkX
            ChipTone.WARN -> MeshaColors.WarnX
            ChipTone.DANGER -> MeshaColors.DangerX
        }
    } else {
        MeshaColors.Surf2
    }
    val shape = RoundedCornerShape(12.dp)
    Text(
        text = text,
        style = if (selected) MeshaType.bodyStrong else MeshaType.body,
        textAlign = TextAlign.Center,
        color = if (selected) accent else MeshaColors.Muted,
        modifier = Modifier
            // A gloved thumb in a shed needs the full touch target, not just the
            // painted area.
            .minimumInteractiveComponentSize()
            .clip(shape)
            .background(fill)
            .border(1.dp, if (selected) accent else MeshaColors.Hair, shape)
            .clickable(onClick = onClick)
            .padding(vertical = 11.dp, horizontal = 14.dp),
    )
}

// --- Operator-facing labels.
//
// Every one of these is farm language. The wire values (orf_scabs, famacha,
// no_urine) are the engine's vocabulary and must never reach a screen.

private fun plainLabel(value: String) = value.replaceFirstChar { it.uppercase() }
private fun skinTentLabel(v: String) = when (v) {
    "lt2" -> "Snaps back"
    "2-4" -> "Slow"
    else -> "Very slow"
}
private fun neuroLabel(v: String) = when (v) {
    "none" -> "Normal"
    "circling" -> "Circling"
    "head_tilt" -> "Head tilted"
    "star_gazing" -> "Head back"
    "blind" -> "Cannot see"
    "tremors" -> "Shivering"
    else -> "Unsteady"
}
private fun breathingLabel(v: String) = when (v) {
    "normal" -> "Normal"
    "fast" -> "Fast"
    "labored" -> "Struggling"
    "cough" -> "Coughing"
    else -> "Panting"
}
private fun stomachLabel(v: String) = when (v) {
    "normal" -> "Normal"
    "bloating" -> "Blown up"
    else -> "Water sound"
}
private fun rumenLabel(v: String) = if (v == "felt") "Moving" else "Not moving"
private fun eatingLabel(v: String) = when (v) {
    "normal" -> "Eating well"
    "not_eating" -> "Not eating"
    "concentrate" -> "Took feed"
    "green_feed" -> "Took green"
    else -> "Took dry"
}
private fun activityLabel(v: String) = when (v) {
    "standing" -> "Standing"
    "down" -> "Cannot stand"
    "limping" -> "Limping"
    "back_leg_drag" -> "Dragging back legs"
    "front_knees" -> "On front knees"
    else -> "Weak"
}
private fun legLabel(v: String) = when (v) {
    "normal" -> "Normal"
    "arthritis" -> "Swollen joint"
    "fracture" -> "Bone out of place"
    else -> "Rotten hoof"
}
private fun woundLabel(v: String) = when (v) {
    "no" -> "None"
    "horn" -> "Horn"
    "neck" -> "Neck"
    "body" -> "Body"
    else -> "Legs"
}
private fun lumpLabel(v: String) = when (v) {
    "no" -> "None"
    "neck" -> "Neck"
    else -> "Body"
}
private fun rashLabel(v: String) = when (v) {
    "none" -> "None"
    "flat_itchy" -> "Flat and itchy"
    else -> "Raised bumps"
}
private fun udderLabel(v: String) = when (v) {
    "normal" -> "Normal"
    "swollen_hard" -> "Hard and swollen"
    "rashes" -> "Rash"
    "wound" -> "Wound"
    else -> "Lumps"
}
private fun lactationLabel(v: String) = when (v) {
    "no" -> "No milk"
    "milk" -> "Milk"
    "colostrum" -> "First milk"
    "water" -> "Watery"
    else -> "Pus"
}
private fun cmtLabel(v: String) = if (v == "pos") "Positive" else "Negative"
private fun vulvaLabel(v: String) = when (v) {
    "none" -> "Normal"
    "lochia_normal" -> "Clean discharge"
    "discharge_bad_smell" -> "Bad smell"
    "pus" -> "Pus"
    else -> "Tissue hanging"
}
private fun strainingLabel(v: String) = when (v) {
    "no" -> "Normal"
    "straining" -> "Straining"
    else -> "No urine"
}
