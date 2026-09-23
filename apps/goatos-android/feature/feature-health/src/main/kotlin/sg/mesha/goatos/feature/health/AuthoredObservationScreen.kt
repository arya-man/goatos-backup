package sg.mesha.goatos.feature.health

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.text.KeyboardOptions
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * THE OBSERVATION FORM, DRAWN FROM WHAT THE SERVER PUBLISHED.
 *
 * The form this replaces was a thousand lines of Compose: one hand-written function per page, the
 * questions written into each, and `enum ObservationStep { VITALS, HEAD, BODY, FINAL }` deciding
 * which question sat where. Moving a question to another page was a release, and so was changing a
 * word of it.
 *
 * This screen knows how to draw a QUESTION -- pick one, pick any, a measurement -- and nothing
 * about which questions exist. Pages, order, titles, options, bounds and the conditions that hide
 * a question all arrive from the published register for THIS animal's type. A farm adding a
 * question on Health Config sees it here on the next refresh, with no build.
 *
 * It keeps the two things the old screen got right and the scroll before it got wrong: the form is
 * WALKED IN PAGES, so a gap is found where the animal still is rather than at the bottom of a long
 * list, and each page gates its own Next.
 */
@Composable
fun AuthoredObservationScreen(
    state: ObservationScreenState,
    form: AuthoredForm,
    answers: AuthoredAnswers,
    onAnswers: (AuthoredAnswers) -> Unit,
    onSubmit: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // Where the operator has scrolled to is not a fact about the animal, so it lives here rather
    // than in the view model. Saveable so a rotation does not drop them back to the first page.
    var pageIndex by rememberSaveable { mutableIntStateOf(0) }
    if (form.pages.isEmpty()) {
        EmptyAuthoredForm(state = state, onBack = onBack, modifier = modifier)
        return
    }
    pageIndex = pageIndex.coerceIn(0, form.pages.lastIndex)
    val page = form.pages[pageIndex]
    val isLastPage = pageIndex == form.pages.lastIndex

    val sex = state.form.sex
    // The SUB-STAGE the register reads inside a type (K1 vs K2 on the milk ladder). It comes from
    // the animal's record, never from the form: it decides how a missed feed is READ, and a
    // manager who could type it could turn a real refusal into a learner's miss.
    val stage = state.form.kidStage
    val shown = page.applicableQuestions(answers, sex, stage)
    val missingHere = page.missing(answers, sex, stage)

    Column(modifier.fillMaxSize().background(MeshaColors.Bg)) {
        MeshaScreenHeader(
            title = state.goatTag.ifBlank { state.goatDisplayId },
            // Which rulebook this animal is judged against. An operator seeing two different forms
            // for two animals in the same pen is owed the reason.
            subtitle = "${form.typeLabel} · ${page.title}",
            onBack = onBack,
        )
        PageProgress(current = pageIndex, total = form.pages.size)

        LazyColumn(
            modifier = Modifier.weight(1f).fillMaxWidth(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (page.hint.isNotBlank()) {
                item("page-hint") {
                    Text(page.hint, style = MeshaType.body, color = MeshaColors.Faint)
                }
            }
            items(shown, key = { it.id }) { question ->
                QuestionCard(
                    question = question,
                    answers = answers,
                    onAnswers = onAnswers,
                )
            }
            item("footer") {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (missingHere.isNotEmpty()) {
                        // Named for THIS page, not the whole form. The old screen reported what was
                        // missing as a twenty-nine-name list at the bottom, which is a list nobody
                        // reads while holding an animal.
                        Text(
                            "Still to answer: ${missingHere.joinToString(", ")}",
                            style = MeshaType.body,
                            color = MeshaColors.Warn,
                        )
                    }
                    MeshaPrimaryButton(
                        text = if (isLastPage) "Submit" else "Next",
                        enabled = missingHere.isEmpty() && !state.submitting,
                        onClick = { if (isLastPage) onSubmit() else pageIndex += 1 },
                    )
                    state.message?.let {
                        Text(it, style = MeshaType.body, color = MeshaColors.Danger)
                    }
                }
            }
        }
    }
}

/**
 * A type whose questions nobody has written yet.
 *
 * It is a real state -- what a farm has between creating a type on Health Config and authoring it
 * -- so it says which type and what to do, rather than showing an empty form that reads as an
 * animal with nothing to check.
 */
@Composable
private fun EmptyAuthoredForm(
    state: ObservationScreenState,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxSize().background(MeshaColors.Bg)) {
        MeshaScreenHeader(
            title = state.goatTag.ifBlank { state.goatDisplayId },
            subtitle = "",
            onBack = onBack,
        )
        MeshaCard(Modifier.padding(16.dp)) {
            Text(
                state.message ?: "This animal's type has no questions written yet.",
                style = MeshaType.body,
                color = MeshaColors.Faint,
            )
        }
    }
}

/** One filled segment per completed page, the pacing the mock gives the form. */
@Composable
private fun PageProgress(current: Int, total: Int) {
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
 * One authored question.
 *
 * A MEASUREMENT takes a number; everything else is a row of answers, one tap each. Pick-one
 * replaces, pick-any toggles -- and an UNKNOWN kind falls through to the answer rows rather than
 * being skipped, because a question drawn awkwardly is recoverable and a question never shown is
 * invisible.
 */
@Composable
private fun QuestionCard(
    question: AuthoredQuestion,
    answers: AuthoredAnswers,
    onAnswers: (AuthoredAnswers) -> Unit,
) {
    MeshaCard {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text(question.title, style = MeshaType.bodyStrong, color = MeshaColors.Ink)
            if (question.hint.isNotBlank()) {
                Text(question.hint, style = MeshaType.body, color = MeshaColors.Faint)
            }

            if (question.isNumber) {
                OutlinedTextField(
                    value = answers.of(question.id).firstOrNull().orEmpty(),
                    onValueChange = { onAnswers(answers.setNumber(question.id, it)) },
                    label = { Text(question.unit.ifBlank { "Value" }) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    modifier = Modifier.fillMaxWidth(),
                )
            } else {
                question.options.forEach { option ->
                    val picked = option.value in answers.of(question.id)
                    AnswerRow(
                        label = option.label.ifBlank { option.value },
                        picked = picked,
                        onClick = {
                            onAnswers(
                                if (question.isMulti) answers.toggle(question, option.value)
                                else answers.pick(question.id, option.value),
                            )
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun AnswerRow(label: String, picked: Boolean, onClick: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (picked) MeshaColors.BrandTint else MeshaColors.Surf2)
            .border(
                1.dp,
                if (picked) MeshaColors.Brand else MeshaColors.Line,
                RoundedCornerShape(12.dp),
            )
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(
            label,
            style = MeshaType.body,
            fontWeight = if (picked) FontWeight.SemiBold else FontWeight.Normal,
            color = if (picked) MeshaColors.BrandD else MeshaColors.Ink,
        )
    }
}
