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
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.component.MeshaSectionLabel
import sg.mesha.goatos.core.designsystem.component.MeshaStatusPill
import sg.mesha.goatos.core.designsystem.component.MeshaTone
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * The assessment: what the register made of one animal, and the Director's
 * decision on it.
 *
 * Two audiences, ONE screen, split by a backend-owned capability rather than a
 * role check here. The manager who recorded the observation sees exactly what the
 * Director sees — that is deliberate, because they are the person standing next to
 * the animal — but only a holder of the confirm authority is offered the decision.
 *
 * Order is the order a person needs it: act-now first, then what the assessment
 * could not explain, then the ranked diagnoses. The unexplained block is NOT a
 * footnote; naming a disease compresses a set of findings into one label, and what
 * the label misses is where a wrong diagnosis hides.
 */
@Composable
fun DiagnosisProposalScreen(
    state: DiagnosisProposalState,
    onEvent: (DiagnosisProposalEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    // Cached first, fresh behind it: the Director may open this minutes after the
    // manager sent it, and the status may have moved on another device.
    RefreshOnResume { onEvent(DiagnosisProposalEvent.Refresh) }

    Column(modifier = modifier.fillMaxSize().background(MeshaColors.Bg)) {
        MeshaScreenHeader(
            title = "Assessment",
            eyebrow = state.goatDisplayId.ifBlank { null },
            onBack = { onEvent(DiagnosisProposalEvent.Back) },
            actions = {
                SyncIconButton(
                    isSyncing = state.refreshing,
                    onSync = { onEvent(DiagnosisProposalEvent.Refresh) },
                )
            },
        )

        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            if (state.loading && state.problems.isEmpty() && state.emergencies.isEmpty()) {
                item { Text("Loading the assessment…", style = MeshaType.body, color = MeshaColors.Faint) }
            }

            // 1. Act now. These do not wait for the Director and are shown first for
            //    that reason -- a manager who reads nothing else must read this.
            if (state.emergencies.isNotEmpty()) {
                item {
                    MeshaCard(accent = MeshaColors.Danger) {
                        Text("Do this now", style = MeshaType.cardTitle, color = MeshaColors.Danger)
                        Text(
                            "These do not wait for approval.",
                            style = MeshaType.caption,
                            color = MeshaColors.Faint,
                            modifier = Modifier.padding(top = 2.dp),
                        )
                        state.emergencies.forEach { line ->
                            Text("• $line", style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                    }
                }
            }

            // 2. What the assessment could NOT account for.
            if (state.unexplained.isNotEmpty()) {
                item {
                    MeshaCard(accent = MeshaColors.Warn) {
                        Text("Not explained", style = MeshaType.cardTitle, color = MeshaColors.Warn)
                        Text(
                            "Seen on this animal, but none of the problems below account for it.",
                            style = MeshaType.caption,
                            color = MeshaColors.Faint,
                            modifier = Modifier.padding(top = 2.dp),
                        )
                        state.unexplained.forEach { line ->
                            Text("• $line", style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                    }
                }
            }

            // 3. The ranked diagnoses, most serious first.
            if (state.problems.isNotEmpty()) {
                item { MeshaSectionLabel("What may be wrong") }
                items(state.problems.size) { index ->
                    val problem = state.problems[index]
                    ProblemRow(
                        problem = problem,
                        selected = problem.id in state.selected,
                        selectable = state.mayConfirm && !state.decided,
                        onToggle = { onEvent(DiagnosisProposalEvent.ToggleProblem(problem.id)) },
                    )
                }
            } else if (!state.loading) {
                item {
                    MeshaCard {
                        Text("Nothing found", style = MeshaType.cardTitle)
                        Text(
                            "The check did not point to a problem. Watch the animal and check again if it changes.",
                            style = MeshaType.body,
                            color = MeshaColors.Faint,
                            modifier = Modifier.padding(top = 4.dp),
                        )
                    }
                }
            }

            // 4. Done in place, once. No daily follow-up, so it is kept apart from
            //    the problems that open a course.
            if (state.fieldActions.isNotEmpty()) {
                item {
                    MeshaCard {
                        Text("Handle on the spot", style = MeshaType.cardTitle)
                        state.fieldActions.forEach { line ->
                            Text("• $line", style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                    }
                }
            }

            if (state.covered.isNotEmpty()) {
                item {
                    MeshaCard {
                        Text("Covered by the treatment above", style = MeshaType.cardTitle)
                        Text(
                            "No separate course needed. If the animal does not improve, look here first.",
                            style = MeshaType.caption,
                            color = MeshaColors.Faint,
                            modifier = Modifier.padding(top = 2.dp),
                        )
                        state.covered.forEach { line ->
                            Text("• $line", style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                    }
                }
            }

            if (state.rechecks.isNotEmpty()) {
                item {
                    MeshaCard {
                        Text("Check again", style = MeshaType.cardTitle)
                        state.rechecks.forEach { line ->
                            Text("• $line", style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                    }
                }
            }

            // Shown, never acted on: the workflow that owns location is the only
            // thing that moves an animal.
            if (state.housing.hasDirective) {
                item {
                    MeshaCard {
                        Text("Where to keep it", style = MeshaType.cardTitle)
                        if (state.housing.acuity.isNotBlank()) {
                            Text(state.housing.acuity, style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                        if (state.housing.containment.isNotBlank()) {
                            Text(state.housing.containment, style = MeshaType.body, modifier = Modifier.padding(top = 4.dp))
                        }
                        if (state.housing.lowCompetition) {
                            Text(
                                "Keep where it can feed without being pushed off.",
                                style = MeshaType.body,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                        }
                    }
                }
            }

            if (state.notes.isNotEmpty()) {
                item {
                    MeshaCard {
                        Text("Notes", style = MeshaType.cardTitle)
                        state.notes.forEach { line ->
                            Text("• $line", style = MeshaType.body, modifier = Modifier.padding(top = 6.dp))
                        }
                    }
                }
            }

            item { DecisionFooter(state = state, onEvent = onEvent) }

            state.message?.let { message ->
                item { Text(message, style = MeshaType.body, color = MeshaColors.Faint) }
            }
        }
    }
}

/**
 * One proposed diagnosis.
 *
 * Selectable only for a user who may decide. Everyone else reads the same row —
 * the manager must see what the animal is being treated for.
 */
@Composable
private fun ProblemRow(
    problem: ProposedProblem,
    selected: Boolean,
    selectable: Boolean,
    onToggle: () -> Unit,
) {
    val rowModifier = Modifier
        .fillMaxWidth()
        .let { if (selectable) it.clickable(onClick = onToggle).minimumInteractiveComponentSize() else it }

    MeshaCard(accent = if (selected) MeshaColors.Teal else null) {
        Row(modifier = rowModifier, verticalAlignment = Alignment.CenterVertically) {
            if (selectable) {
                Text(
                    text = if (selected) "☑" else "☐",
                    style = MeshaType.cardTitle,
                    modifier = Modifier.padding(end = 10.dp),
                )
            }
            Column(modifier = Modifier.fillMaxWidth()) {
                Text(problem.label, style = MeshaType.cardTitle)
                Row(
                    modifier = Modifier.padding(top = 4.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    MeshaStatusPill(label = problem.confidence, tone = confidenceTone(problem.confidence))
                    if (!problem.canConfirm) {
                        MeshaStatusPill(label = "No plan yet", tone = MeshaTone.Muted)
                    }
                }
                if (!problem.canConfirm && problem.blockedReason.isNotBlank()) {
                    Text(
                        problem.blockedReason,
                        style = MeshaType.caption,
                        color = MeshaColors.Faint,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
            }
        }
    }
}

/**
 * The Director's decision.
 *
 * Sending NOTHING is a real decision — it declines the whole assessment — so the
 * button stays enabled on an empty selection and says so. What disables it is a
 * tick on something that has no treatment plan, which the screen names rather than
 * failing at the write.
 */
@Composable
private fun DecisionFooter(
    state: DiagnosisProposalState,
    onEvent: (DiagnosisProposalEvent) -> Unit,
) {
    when {
        state.decided -> MeshaCard {
            Text(
                if (state.status == "confirmed") "Treatment approved" else "Assessment closed",
                style = MeshaType.cardTitle,
            )
            Text(
                "This has already been decided.",
                style = MeshaType.body,
                color = MeshaColors.Faint,
                modifier = Modifier.padding(top = 4.dp),
            )
        }

        !state.mayConfirm -> MeshaCard {
            Text("Waiting for approval", style = MeshaType.cardTitle)
            Text(
                "The Health Director decides which of these to treat.",
                style = MeshaType.body,
                color = MeshaColors.Faint,
                modifier = Modifier.padding(top = 4.dp),
            )
        }

        else -> Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            state.blockedSelections.forEach { problem ->
                Text(
                    "${problem.label} has no treatment plan yet, so it cannot be approved.",
                    style = MeshaType.body,
                    color = MeshaColors.Danger,
                )
            }
            if (state.selected.isEmpty()) {
                Text(
                    "Nothing selected. Sending now records that none of these should be treated.",
                    style = MeshaType.caption,
                    color = MeshaColors.Faint,
                )
            }
            MeshaPrimaryButton(
                text = if (state.selected.isEmpty()) "Treat none of these" else "Approve treatment",
                enabled = state.canSend,
                onClick = { onEvent(DiagnosisProposalEvent.Send) },
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** Confidence drives the tone; severity drives the ORDER, which the backend owns. */
private fun confidenceTone(confidence: String): MeshaTone = when (confidence.lowercase()) {
    "confirmed" -> MeshaTone.Danger
    "probable" -> MeshaTone.Warn
    else -> MeshaTone.Muted
}
