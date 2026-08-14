package sg.mesha.goatos.feature.health

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.text.KeyboardOptions
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.component.MeshaSectionLabel
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * The observation form.
 *
 * It is laid out the way a person walks an animal — vitals, head, chest, gut,
 * skin, legs, then the sex-specific checks — rather than grouped by data type.
 * The operator is holding a sick goat; the screen should follow their hands.
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

@Composable
fun ObservationFormScreen(
    state: ObservationScreenState,
    onEvent: (ObservationFormEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    val form = state.form
    val blockers = form.blockers()
    val missing = form.missingFields()

    fun update(block: ObservationFormState.() -> ObservationFormState) {
        onEvent(ObservationFormEvent.UpdateForm(form.block()))
    }

    Column(modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Check this animal",
            subtitle = listOf(state.goatDisplayId, state.goatTag).filter { it.isNotBlank() }.joinToString(" · "),
            onBack = { onEvent(ObservationFormEvent.Back) },
        )

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            item {
                Text(
                    "Answer every question. If something looks normal, say so — leaving it blank " +
                        "is not the same as checking it.",
                    style = MeshaType.body,
                    color = MeshaColors.Muted,
                )
            }

            section("Temperature and hydration") {
                TemperatureField(form.temp) { update { copy(temp = it) } }
                SingleChoice("Skin tent", ObservationOptions.skinTent, form.skinTent, ::skinTentLabel) {
                    update { copy(skinTent = it) }
                }
                YesNo("Sunken flank", form.stomachInside) { update { copy(stomachInside = it) } }
            }

            section("Head") {
                MultiChoice("Eyes", ObservationOptions.eyes, form.eyes, setOf("normal"), ::plainLabel) {
                    update { copy(eyes = it) }
                }
                SingleChoice("Mouth", ObservationOptions.mouth, form.mouth, ::mouthLabel) {
                    update { copy(mouth = it) }
                }
                YesNo("Runny nose", form.nasal) { update { copy(nasal = it) } }
                YesNo("Froth at the mouth", form.frothyMouth) { update { copy(frothyMouth = it) } }
                YesNo("Cannot open the mouth", form.lockedJaw) { update { copy(lockedJaw = it) } }
                MultiChoice("Nervous signs", ObservationOptions.neuro, form.neuro, setOf("none"), ::neuroLabel) {
                    update { copy(neuro = it) }
                }
            }

            section("Eyelid colour") {
                SingleChoice("FAMACHA score", ObservationOptions.famacha, form.famacha, ::plainLabel) {
                    update { copy(famacha = it) }
                }
                YesNo("Yellow gums or eyes", form.yellow) { update { copy(yellow = it) } }
            }

            section("Chest") {
                MultiChoice("Breathing", ObservationOptions.breathing, form.breathing, setOf("normal"), ::breathingLabel) {
                    update { copy(breathing = it) }
                }
            }

            section("Stomach and feeding") {
                MultiChoice("Left side", ObservationOptions.leftStomach, form.leftStomach, setOf("normal"), ::stomachLabel) {
                    update { copy(leftStomach = it) }
                }
                SingleChoice("Rumen movement", ObservationOptions.rumenMovement, form.rumenMovement, ::rumenLabel) {
                    update { copy(rumenMovement = it) }
                }
                MultiChoice("Eating", ObservationOptions.eating, form.eating, setOf("normal", "not_eating"), ::eatingLabel) {
                    update { copy(eating = it) }
                }
                YesNo("Loose motion", form.diarrhea) { update { copy(diarrhea = it) } }
                YesNo("Pushed away from feed", form.competition) { update { copy(competition = it) } }
            }

            section("Movement") {
                SingleChoice("How it stands", ObservationOptions.activity, form.activity, ::activityLabel) {
                    update { copy(activity = it) }
                }
                SingleChoice("Legs and feet", ObservationOptions.leg, form.leg, ::legLabel) {
                    update { copy(leg = it) }
                }
            }

            section("Skin and coat") {
                MultiChoice("Wounds", ObservationOptions.wounds, form.wounds, setOf("no"), ::woundLabel) {
                    update { copy(wounds = it) }
                }
                SingleChoice("Lumps", ObservationOptions.lumps, form.lumps, ::lumpLabel) {
                    update { copy(lumps = it) }
                }
                SingleChoice("Rashes", ObservationOptions.rashCharacter, form.rashCharacter, ::rashLabel) {
                    update { copy(rashCharacter = it) }
                }
                YesNo("Hair loss", form.hairloss) { update { copy(hairloss = it) } }
                YesNo("Ticks", form.ticks) { update { copy(ticks = it) } }
                YesNo("Maggots on the body", form.flystrike) { update { copy(flystrike = it) } }
                YesNo("Maggots at the ear tag", form.eartagFlystrike) { update { copy(eartagFlystrike = it) } }
                YesNo("Torn ear tag", form.eartagWound) { update { copy(eartagWound = it) } }
            }

            section("Other") {
                YesNo("Red urine", form.redUrine) { update { copy(redUrine = it) } }
                YesNo("Swelling under the jaw", form.bodyEdema) { update { copy(bodyEdema = it) } }
            }

            // Sex-specific blocks. A field that does not apply is not asked, which
            // is what "hidden fields record not-applicable" means in practice.
            if (form.isFemale) {
                section("Udder and kidding") {
                    SingleChoice("Udder", ObservationOptions.udder, form.udder, ::udderLabel) {
                        update { copy(udder = it) }
                    }
                    SingleChoice("Milk", ObservationOptions.lactation, form.lactation, ::lactationLabel) {
                        update { copy(lactation = it) }
                    }
                    // The milk test is only asked when there is milk to test.
                    if (form.cmtApplies) {
                        SingleChoice("Milk test", ObservationOptions.cmt, form.cmt, ::cmtLabel) {
                            update { copy(cmt = it) }
                        }
                    }
                    SingleChoice("Back passage", ObservationOptions.vulva, form.vulva, ::vulvaLabel) {
                        update { copy(vulva = it) }
                    }
                }
            }
            if (form.isMale) {
                section("Urine") {
                    SingleChoice("Passing urine", ObservationOptions.straining, form.straining, ::strainingLabel) {
                        update { copy(straining = it) }
                    }
                }
            }

            item { BlockerNotice(blockers, missing) }

            item {
                MeshaPrimaryButton(
                    text = if (state.submitting) "Sending…" else "Send for assessment",
                    onClick = { onEvent(ObservationFormEvent.Submit) },
                    enabled = form.canSubmit() && !state.submitting,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            state.message?.let { item { Text(it, style = MeshaType.body, color = MeshaColors.BrandD) } }
        }
    }
}

/** One titled group of questions. */
private fun androidx.compose.foundation.lazy.LazyListScope.section(
    title: String,
    content: @Composable () -> Unit,
) {
    item {
        MeshaCard {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                MeshaSectionLabel(title)
                content()
            }
        }
    }
}

/**
 * Tells the operator exactly what is left, and names contradictions as
 * contradictions rather than as missing answers.
 */
@Composable
private fun BlockerNotice(blockers: List<ObservationBlocker>, missing: List<String>) {
    if (blockers.isEmpty()) return
    val contradictions = blockers.filter { it != ObservationBlocker.INCOMPLETE }.map(::blockerText)
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            contradictions.forEach { Text(it, style = MeshaType.bodyStrong, color = MeshaColors.Warn) }
            if (ObservationBlocker.INCOMPLETE in blockers && missing.isNotEmpty()) {
                Text(
                    "Still to check: " + missing.joinToString(", "),
                    style = MeshaType.body,
                    color = MeshaColors.Muted,
                )
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

@Composable
private fun TemperatureField(value: String, onChange: (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("Temperature (°F)", style = MeshaType.fieldLabel, color = MeshaColors.Muted)
        TextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
            colors = TextFieldDefaults.colors(
                focusedContainerColor = MeshaColors.Surf2,
                unfocusedContainerColor = MeshaColors.Surf2,
                focusedTextColor = MeshaColors.Ink,
                unfocusedTextColor = MeshaColors.Ink,
            ),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun YesNo(label: String, value: Boolean?, onChange: (Boolean) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MeshaType.fieldLabel, color = MeshaColors.Muted)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Chip("No", selected = value == false, modifier = Modifier.weight(1f)) { onChange(false) }
            Chip("Yes", selected = value == true, modifier = Modifier.weight(1f)) { onChange(true) }
        }
    }
}

@Composable
private fun SingleChoice(
    label: String,
    options: List<String>,
    selected: String,
    labelFor: (String) -> String,
    onChange: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MeshaType.fieldLabel, color = MeshaColors.Muted)
        WrapRow(options) { option ->
            Chip(labelFor(option), selected = selected == option) { onChange(option) }
        }
    }
}

@Composable
private fun MultiChoice(
    label: String,
    options: List<String>,
    selected: Set<String>,
    exclusive: Set<String>,
    labelFor: (String) -> String,
    onChange: (Set<String>) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MeshaType.fieldLabel, color = MeshaColors.Muted)
        WrapRow(options) { option ->
            Chip(labelFor(option), selected = option in selected) {
                onChange(toggleMultiValue(selected, option, exclusive))
            }
        }
    }
}

/**
 * Lays options out in rows of two. A shed is a bad place for a dense grid, and
 * two columns keeps each tap target wide enough for a gloved thumb.
 */
@Composable
private fun WrapRow(options: List<String>, chip: @Composable (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        options.chunked(2).forEach { pair ->
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
                pair.forEach { option ->
                    Row(modifier = Modifier.weight(1f)) { chip(option) }
                }
                if (pair.size == 1) Row(modifier = Modifier.weight(1f)) {}
            }
        }
    }
}

@Composable
private fun Chip(text: String, selected: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Text(
        text = text,
        style = MeshaType.body,
        textAlign = TextAlign.Center,
        color = if (selected) MeshaColors.OnBrand else MeshaColors.Ink,
        modifier = modifier
            .fillMaxWidth()
            // A gloved thumb in a shed needs the full touch target, not just the
            // painted area.
            .minimumInteractiveComponentSize()
            .clip(RoundedCornerShape(10.dp))
            .background(if (selected) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(onClick = onClick)
            .padding(vertical = 12.dp, horizontal = 10.dp),
    )
}

// --- Operator-facing labels.
//
// Every one of these is farm language. The wire values (orf_scabs, famacha,
// no_urine) are the engine's vocabulary and must never reach a screen.

private fun plainLabel(value: String) = value
private fun skinTentLabel(v: String) = when (v) {
    "lt2" -> "Snaps back"
    "2-4" -> "Slow"
    else -> "Very slow"
}
private fun mouthLabel(v: String) = if (v == "normal") "Normal" else "Scabs"
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
