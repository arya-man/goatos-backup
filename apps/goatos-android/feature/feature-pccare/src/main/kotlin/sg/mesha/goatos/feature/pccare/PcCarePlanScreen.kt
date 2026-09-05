package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderers; PcCarePlanViewModel (in :app) owns the pc_care_*
// AnalyticsEvents + CrashReporter wiring for the monitor list and the create wizard.

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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.TextButton
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * The MONITOR face of one PC Care category tab: the read-only task list a planner/monitor sees
 * (the operator's execute face is [PcCareWorklistScreen]). Which face renders is decided by the
 * backend's `pc_care_execute` capability flag; the plan action is offered on `pc_care_plan`.
 * Visual language mirrors WeighingTasksScreen: header + eyebrow, date pill bar, pill-carrying
 * cards, and a floating "Plan task" action.
 */
@Composable
fun PcCareMonitorScreen(
    state: PcCarePlanUiState,
    rows: LazyPagingItems<PcCareTaskCardUi>,
    planEnabled: Boolean,
    onPlanTask: () -> Unit = {},
    onOpenTask: (PcCareTaskCardUi) -> Unit = {},
    /** Opens ONE pen's work by task id — used by the round card's pen rows. */
    onOpenTaskId: (String) -> Unit = {},
    onEvent: (PcCarePlanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(PcCarePlanEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(modifier = Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                eyebrow = "Preventive Care",
                eyebrowColor = MeshaColors.BrandD,
                subtitle = pcCareFriendlyDate(state.monitorDate),
                actions = {
                    SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(PcCarePlanEvent.Refresh) })
                },
            )
            state.message?.let { message ->
                Text(
                    text = message,
                    color = MeshaColors.Warn,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier
                        .padding(horizontal = 16.dp, vertical = 4.dp)
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .background(MeshaColors.WarnX)
                        .clickable { onEvent(PcCarePlanEvent.DismissMessage) }
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                )
            }
            // NO date strip. The planner's list mirrors weighing's: two tabs over the live
            // work, because a planner reads what is outstanding rather than paging a calendar.
            // Each card carries its own date, so nothing is lost by dropping the axis.
            Row(
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                PcCareRoundsTabPill(
                    label = "Active",
                    selected = state.roundsTab == PcCareRoundsTab.ACTIVE,
                    onClick = { onEvent(PcCarePlanEvent.SelectRoundsTab(PcCareRoundsTab.ACTIVE)) },
                )
                PcCareRoundsTabPill(
                    label = "Completed",
                    selected = state.roundsTab == PcCareRoundsTab.COMPLETED,
                    onClick = { onEvent(PcCarePlanEvent.SelectRoundsTab(PcCareRoundsTab.COMPLETED)) },
                )
            }
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(top = 4.dp, bottom = 88.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                // The empty state keys on the ROUND cards, the only list this screen has now.
                // It used to key on the retired paged rows, which are always zero — so it
                // rendered ABOVE real cards. It also no longer mentions a day: there is no date
                // axis on this screen, so "for this day" described a filter that does not exist.
                if (state.roundCards.isEmpty()) {
                    item(key = "empty") {
                        EmptyState(
                            title = if (state.roundsTab == PcCareRoundsTab.COMPLETED) {
                                "Nothing finished yet"
                            } else {
                                "No care work outstanding"
                            },
                            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                            icon = MeshaIcons.Check,
                            tone = EmptyTone.Neutral,
                        )
                    }
                }
                // The planner's list is ROUND-grained (maintainer decision 2026-09-05): one card
                // per round, because these pens were ticked as ONE piece of work. A round-less
                // legacy task is a round of one and renders the same way.
                items(count = state.roundCards.size, key = { i -> state.roundCards[i].cardKey }) { index ->
                    val card = state.roundCards[index]
                    PcCareRoundCard(
                        card = card,
                        open = state.openRoundCardKey == card.cardKey,
                        loadingPens = state.openRoundLoading && state.openRoundCardKey == card.cardKey,
                        pens = if (state.openRoundCardKey == card.cardKey) state.openRoundPens else emptyList(),
                        onTap = {
                            if (card.expandable) {
                                onEvent(PcCarePlanEvent.ToggleRoundCard(card.cardKey, card.roundId))
                            } else if (card.singleTaskId.isNotBlank()) {
                                onOpenTaskId(card.singleTaskId)
                            }
                        },
                        onOpenPen = { taskId -> onOpenTaskId(taskId) },
                        // Ending asks WHY first; the dialog's confirm ends the whole round, or
                        // the single task of a round of one.
                        onClose = {
                            onEvent(PcCarePlanEvent.AskCloseCard(card.cardKey, card.roundId, card.singleTaskId))
                        },
                        onReopen = { onEvent(PcCarePlanEvent.ReopenTask(card.singleTaskId)) },
                        onReopenPen = { taskId -> onEvent(PcCarePlanEvent.ReopenTask(taskId)) },
                        // Ending and reopening are PLANNER authority (the backend refuses them
                        // for anyone else), so both follow planEnabled.
                        actionsEnabled = planEnabled,
                    )
                }
            }
        }
        if (planEnabled) {
            PcCarePlanFab(
                label = "＋ Plan task",
                onClick = onPlanTask,
                modifier = Modifier
                    .align(Alignment.BottomEnd)
                    .padding(16.dp),
            )
        }
        if (state.closingTaskId.isNotBlank()) {
            PcCareEndWorkDialog(
                reason = state.closeReason,
                onReasonChange = { onEvent(PcCarePlanEvent.CloseReasonChanged(it)) },
                onConfirm = {
                    onEvent(
                        PcCarePlanEvent.CloseCard(state.closingRoundId, state.closingSingleTaskId, state.closeReason),
                    )
                },
                onDismiss = { onEvent(PcCarePlanEvent.DismissCloseTask) },
            )
        }
    }
}

/**
 * Ending work asks WHY. Ending is not erasing — the pen stays taken for the day and this
 * sentence is what whoever reads the row later sees in answer to "why did this never happen",
 * so the confirm stays disabled until something is typed.
 */
@Composable
private fun PcCareEndWorkDialog(
    reason: String,
    onReasonChange: (String) -> Unit,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(text = "End this work?", style = MeshaType.cardTitle, color = MeshaColors.Ink) },
        text = {
            Column {
                Text(
                    text = "The pen stays booked for today. Say why this work is being ended.",
                    style = MeshaType.cardSubtitle,
                    color = MeshaColors.Muted,
                )
                Spacer(Modifier.height(12.dp))
                OutlinedTextField(
                    value = reason,
                    onValueChange = onReasonChange,
                    singleLine = false,
                    placeholder = { Text(text = "Reason", style = MeshaType.cardSubtitle) },
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm, enabled = reason.isNotBlank()) {
                Text(text = "End work", color = if (reason.isNotBlank()) MeshaColors.Danger else MeshaColors.Muted)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(text = "Keep it", color = MeshaColors.Muted) }
        },
    )
}

/**
 * One planned task, WeighingTaskCard-shaped: pills up top, pen title, people line, count pill.
 * The whole card opens the task's read-only detail — the oversight answer to "what have they
 * done" (which animals are in, which videos are recorded) without any capture controls.
 */
@Composable
private fun PcCareMonitorTaskCard(
    card: PcCareTaskCardUi,
    onOpen: () -> Unit,
    onClose: () -> Unit,
    onReopen: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(18.dp))
            .clickable(onClick = onOpen)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            PcCareStatusChip(label = card.statusLabel, tone = card.statusTone)
            Spacer(Modifier.weight(1f))
            PcCareTaskPill(label = card.dueDateLabel, fg = MeshaColors.Muted, bg = MeshaColors.Surf3)
        }
        Text(
            // Backend-composed pen display, verbatim.
            text = card.locationDisplay,
            color = MeshaColors.Ink,
            style = MeshaType.cardTitle,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        val peopleLine = listOf(card.parkLabel, card.assigneeLine).filter { it.isNotBlank() }.joinToString(" · ")
        if (peopleLine.isNotBlank()) {
            Text(
                text = peopleLine,
                color = MeshaColors.Muted,
                style = MeshaType.cardSubtitle,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (card.reworkReason.isNotBlank()) {
            Text(
                text = card.reworkReason,
                color = MeshaColors.Danger,
                style = MeshaType.caption,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            if (card.animalCountLabel.isNotBlank()) {
                PcCareTaskPill(label = card.animalCountLabel, fg = MeshaColors.BrandD, bg = MeshaColors.Surf3)
            }
            Spacer(Modifier.weight(1f))
            if (card.closable) {
                Text(
                    text = "End this work",
                    color = MeshaColors.Danger,
                    style = MeshaType.caption,
                    modifier = pcCareInlineActionModifier(onClose),
                )
            }
            if (card.reopenable) {
                Text(
                    text = "Start it again",
                    color = MeshaColors.BrandD,
                    style = MeshaType.caption,
                    modifier = pcCareInlineActionModifier(onReopen),
                )
            }
        }
    }
}

/**
 * The plan-wizard drill (`/pc/plan/{category}`): day → farm → pens → people → review, category
 * fixed by the launching tab. Chrome ports the weighing plan wizard: a segment stepper under the
 * header and a sticky bottom action bar with a context line.
 */
@Composable
fun PcCarePlanWizardScreen(
    state: PcCarePlanUiState,
    onEvent: (PcCarePlanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val stepIndex = PC_CARE_WIZARD_STEPS.indexOf(state.step).coerceAtLeast(0)
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = "Plan a care task",
            eyebrow = "Preventive Care",
            eyebrowColor = MeshaColors.BrandD,
            subtitle = state.selectedCategoryLabel.takeIf { it.isNotBlank() },
            onBack = {
                if (state.step == PcCarePlanStep.DATE) onEvent(PcCarePlanEvent.CloseCreate)
                else onEvent(PcCarePlanEvent.PreviousStep)
            },
        )
        PcCareStepper(stepCount = PC_CARE_WIZARD_STEPS.size, currentIndex = stepIndex)
        state.message?.let { message ->
            Text(
                text = message,
                color = MeshaColors.Warn,
                style = MeshaType.cardSubtitle,
                modifier = Modifier
                    .padding(horizontal = 16.dp, vertical = 4.dp)
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.WarnX)
                    .clickable { onEvent(PcCarePlanEvent.DismissMessage) }
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            when (state.step) {
                PcCarePlanStep.DATE -> {
                    item(key = "step_title") { WizardStepTitle("For which day?") }
                    // The weighing wizard's day list: one radio row per plannable day, today first.
                    // With the removal toggle ON, a day whose removal evening has already begun
                    // is not offered at all (mirrors the server's 20:00 IST rule).
                    val days = pcCareWizardDayOptions(state.today).filter { (iso, _) ->
                        !state.feedRemovalRequired || state.minSelectableDateIso.isBlank() || iso >= state.minSelectableDateIso
                    }
                    items(count = days.size, key = { days[it].first }) { index ->
                        val (iso, label) = days[index]
                        WizardOptionRow(
                            label = label,
                            selected = iso == state.selectedDate,
                            onClick = { onEvent(PcCarePlanEvent.SelectDate(java.time.LocalDate.parse(iso))) },
                        )
                    }
                }
                PcCarePlanStep.PARK -> {
                    item(key = "step_title") { WizardStepTitle("At which farm?") }
                    items(count = state.parks.size, key = { state.parks[it].key }) { index ->
                        val option = state.parks[index]
                        WizardOptionRow(
                            label = option.label,
                            selected = option.key == state.selectedParkId,
                            onClick = { onEvent(PcCarePlanEvent.SelectPark(option.key)) },
                        )
                    }
                }
                PcCarePlanStep.PEN -> {
                    item(key = "step_title") { WizardStepTitle("Which pens?") }
                    items(count = state.pens.size, key = { state.pens[it].shedId + "|partition|" + state.pens[it].partitionLabel }) { index ->
                        val pen = state.pens[index]
                        val taken = pen.existingTaskId.isNotBlank()
                        WizardOptionRow(
                            // Backend-composed pen display, verbatim.
                            label = pen.locationDisplay,
                            selected = pen.selectionKey() in state.selectedPenKeys,
                            enabled = !taken,
                            trailing = if (taken) "Already planned" else "",
                            onClick = { onEvent(PcCarePlanEvent.TogglePen(pen.shedId, pen.partitionLabel)) },
                        )
                    }
                    if (!state.pensEndReached) {
                        item(key = "pens_footer") {
                            // Passive footer: composing it asks for the next page.
                            LaunchedEffect(state.pens.size) { onEvent(PcCarePlanEvent.LoadMorePens) }
                            Box(
                                modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                CircularProgressIndicator(color = MeshaColors.BrandD)
                            }
                        }
                    } else if (state.pensLoading) {
                        item(key = "pens_loading") {
                            Box(
                                modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp),
                                contentAlignment = Alignment.Center,
                            ) {
                                CircularProgressIndicator(color = MeshaColors.BrandD)
                            }
                        }
                    }
                }
                PcCarePlanStep.OPERATORS -> {
                    item(key = "step_title") { WizardStepTitle("Who does the work?") }
                    // Only the people mapped to the CHOSEN farm (backend-owned park grants);
                    // an empty mapping means cross-park and stays offered everywhere.
                    val assignable = state.operators.filter {
                        it.parkIds.isEmpty() || state.selectedParkId in it.parkIds
                    }
                    items(count = assignable.size, key = { assignable[it].key }) { index ->
                        val option = assignable[index]
                        WizardOptionRow(
                            label = option.label,
                            selected = option.key in state.selectedOperatorIds,
                            onClick = { onEvent(PcCarePlanEvent.ToggleOperator(option.key)) },
                        )
                    }
                    if (assignable.isEmpty()) {
                        item(key = "no_operators") {
                            Text(
                                text = "No one is mapped to this farm yet",
                                color = MeshaColors.Muted,
                                style = MeshaType.cardSubtitle,
                                modifier = Modifier.padding(horizontal = 16.dp),
                            )
                        }
                    }
                    // Feed & water removal before deworming (maintainer decision 2026-09-03) —
                    // offered ONLY on the deworming wizard. Wording is minimal wizard chrome like
                    // this screen's other step titles; every refusal sentence stays server-owned.
                    if (state.feedRemovalOffered) {
                        item(key = "feed_removal_toggle") {
                            Column(modifier = Modifier.padding(top = 8.dp)) {
                                WizardStepTitle("Feed removed before deworming?")
                                Text(
                                    text = "Tablets given in feed need feed & water taken out the evening before.",
                                    color = MeshaColors.Muted,
                                    style = MeshaType.caption,
                                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                                )
                            }
                        }
                        item(key = "feed_removal_yes") {
                            WizardOptionRow(
                                label = "Yes — remove feed & water the evening before",
                                selected = state.feedRemovalRequired,
                                onClick = { if (!state.feedRemovalRequired) onEvent(PcCarePlanEvent.ToggleFeedRemoval) },
                            )
                        }
                        item(key = "feed_removal_no") {
                            WizardOptionRow(
                                label = "No — given by injection",
                                selected = !state.feedRemovalRequired,
                                onClick = { if (state.feedRemovalRequired) onEvent(PcCarePlanEvent.ToggleFeedRemoval) },
                            )
                        }
                        if (state.feedRemovalRequired) {
                            item(key = "removal_people_title") {
                                WizardStepTitle("Who removes feed & water?")
                            }
                            items(count = assignable.size, key = { "removal_" + assignable[it].key }) { index ->
                                val option = assignable[index]
                                WizardOptionRow(
                                    label = option.label,
                                    selected = option.key in state.selectedRemovalOperatorIds,
                                    onClick = { onEvent(PcCarePlanEvent.ToggleRemovalOperator(option.key)) },
                                )
                            }
                        }
                    }
                }
                PcCarePlanStep.REVIEW -> {
                    item(key = "step_title") { WizardStepTitle("Review") }
                    item(key = "review") {
                        Column(
                            modifier = pcCareCardModifier(enabled = false, onClick = null),
                            verticalArrangement = Arrangement.spacedBy(6.dp),
                        ) {
                            ReviewLine("Work", state.selectedCategoryLabel)
                            ReviewLine("Day", pcCareFriendlyDate(state.selectedDate) ?: state.selectedDate)
                            ReviewLine("Farm", state.selectedParkLabel)
                            ReviewLine("Pens", state.selectedPenLabel)
                            ReviewLine(
                                "People",
                                state.operators
                                    .filter { it.key in state.selectedOperatorIds }
                                    .joinToString(", ") { it.label },
                            )
                            if (state.feedRemovalRequired) {
                                ReviewLine(
                                    "Feed & water removal",
                                    state.operators
                                        .filter { it.key in state.selectedRemovalOperatorIds }
                                        .joinToString(", ") { it.label },
                                )
                            }
                        }
                    }
                }
                PcCarePlanStep.LIST -> Unit
            }
        }
        PcCareWizardActionBar(contextLine = wizardContextLine(state)) {
            PcCareGhostButton(
                label = "Close",
                enabled = !state.creating,
                onClick = { onEvent(PcCarePlanEvent.CloseCreate) },
                modifier = Modifier.weight(1f),
            )
            if (state.step == PcCarePlanStep.REVIEW) {
                PcCarePrimaryButton(
                    label = if (state.creating) "Creating…" else "Create task",
                    enabled = !state.creating,
                    onClick = { onEvent(PcCarePlanEvent.Create) },
                    modifier = Modifier.weight(1f),
                )
            } else {
                PcCarePrimaryButton(
                    label = "Next",
                    enabled = wizardStepComplete(state),
                    onClick = { onEvent(PcCarePlanEvent.NextStep) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

private fun wizardStepComplete(state: PcCarePlanUiState): Boolean = when (state.step) {
    PcCarePlanStep.DATE -> state.selectedDate.isNotBlank()
    PcCarePlanStep.PARK -> state.selectedParkId.isNotBlank()
    PcCarePlanStep.PEN -> state.selectedPenKeys.isNotEmpty()
    PcCarePlanStep.OPERATORS -> state.selectedOperatorIds.isNotEmpty()
    else -> true
}

private fun wizardContextLine(state: PcCarePlanUiState): String {
    val chosen = listOfNotNull(
        state.selectedCategoryLabel.takeIf { it.isNotBlank() },
        pcCareFriendlyDate(state.selectedDate),
        state.selectedParkLabel.takeIf { it.isNotBlank() },
        state.selectedPenLabel.takeIf { it.isNotBlank() },
        state.selectedOperatorIds.size.takeIf { it > 0 }?.let { count ->
            if (count == 1) "1 person" else "$count people"
        },
    )
    return when {
        chosen.isEmpty() -> "Pick a day to begin"
        else -> chosen.joinToString(" · ")
    }
}

@Composable
private fun WizardStepTitle(text: String) {
    Text(
        text = text,
        color = MeshaColors.Ink,
        style = MeshaType.cardTitle,
        modifier = Modifier.padding(horizontal = 16.dp),
    )
}

@Composable
private fun WizardOptionRow(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
    enabled: Boolean = true,
    trailing: String = "",
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(if (selected) MeshaColors.Surf2 else MeshaColors.Surf)
            .border(1.dp, if (selected) MeshaColors.BrandD else MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.Ink else PcCareDim,
            style = MeshaType.cardSubtitle,
            modifier = Modifier.weight(1f),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        when {
            trailing.isNotBlank() -> Text(text = trailing, color = PcCareDim, style = MeshaType.caption)
            selected -> Text(text = "✓", color = MeshaColors.BrandD, style = MeshaType.bodyStrong)
        }
    }
}

@Composable
private fun ReviewLine(label: String, value: String) {
    Row(modifier = Modifier.fillMaxWidth()) {
        Text(text = label, color = MeshaColors.Muted, style = MeshaType.caption, modifier = Modifier.weight(1f))
        Text(text = value, color = MeshaColors.Ink, style = MeshaType.cardSubtitle)
    }
}

/** The monitor screen's floating plan action — the weighing "New task" pill. */
@Composable
private fun PcCarePlanFab(label: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .height(48.dp)
            .clip(RoundedCornerShape(24.dp))
            .background(MeshaColors.Brand)
            .clickable(onClick = onClick)
            .padding(horizontal = 20.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = MeshaColors.PageBg,
            style = MeshaType.pillStrong,
        )
    }
}

/**
 * The wizard's plannable days: today through today+14 (the ViewModel's window), as
 * (isoDate, label) pairs. A fixed bounded list — never park-scale data.
 */
private fun pcCareWizardDayOptions(todayIso: String): List<Pair<String, String>> {
    // exception:exempt a malformed today renders an empty day list; the ViewModel owns the value
    val today = runCatching { java.time.LocalDate.parse(todayIso) }.getOrNull() ?: return emptyList()
    return (0..14).map { offset ->
        val day = today.plusDays(offset.toLong())
        val base = day.format(java.time.format.DateTimeFormatter.ofPattern("EEE d MMM", java.util.Locale.ENGLISH))
        val label = when (offset) {
            0 -> "Today · $base"
            1 -> "Tomorrow · $base"
            else -> base
        }
        day.toString() to label
    }
}

/**
 * ONE round on the planner's list: the pens it covers, named, on a single card — because the
 * planner ticked them as one piece of work. Tapping opens the pens; tapping a pen opens that
 * pen's own work. A round of ONE (a legacy task with no round) opens straight through.
 */
@Composable
private fun PcCareRoundCard(
    card: PcCareRoundCardUi,
    open: Boolean,
    loadingPens: Boolean,
    pens: List<PcCareRoundPenUi>,
    onTap: () -> Unit,
    onOpenPen: (String) -> Unit,
    onClose: () -> Unit,
    onReopen: () -> Unit,
    onReopenPen: (String) -> Unit,
    actionsEnabled: Boolean,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf2)
            .clickable(onClick = onTap)
            .padding(16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            PcCareTaskPill(label = card.statusLabel, fg = MeshaColors.BrandD, bg = MeshaColors.Surf3)
            Spacer(Modifier.weight(1f))
            PcCareTaskPill(label = card.dateLabel, fg = MeshaColors.Muted, bg = MeshaColors.Surf3)
        }
        Spacer(Modifier.height(10.dp))
        Text(text = card.penCountLabel, style = MeshaType.cardTitle, color = MeshaColors.Ink)
        if (card.pensLabel.isNotBlank()) {
            Spacer(Modifier.height(4.dp))
            // Backend-composed pen names, rendered verbatim.
            Text(text = card.pensLabel, style = MeshaType.cardSubtitle, color = MeshaColors.BrandD)
        }
        if (card.parkAndCrewLabel.isNotBlank()) {
            Spacer(Modifier.height(4.dp))
            Text(text = card.parkAndCrewLabel, style = MeshaType.cardSubtitle, color = MeshaColors.Muted)
        }
        if (card.animalCountLabel.isNotBlank()) {
            Spacer(Modifier.height(8.dp))
            PcCareTaskPill(label = card.animalCountLabel, fg = MeshaColors.BrandD, bg = MeshaColors.Surf3)
        }
        if (actionsEnabled && (card.closable || card.reopenable)) {
            Spacer(Modifier.height(10.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Spacer(Modifier.weight(1f))
                if (card.closable) {
                    Text(
                        text = "End this work",
                        color = MeshaColors.Danger,
                        style = MeshaType.caption,
                        modifier = pcCareInlineActionModifier(onClose),
                    )
                }
                if (card.reopenable) {
                    Text(
                        text = "Start it again",
                        color = MeshaColors.BrandD,
                        style = MeshaType.caption,
                        modifier = pcCareInlineActionModifier(onReopen),
                    )
                }
            }
        }
        if (open) {
            Spacer(Modifier.height(12.dp))
            if (loadingPens && pens.isEmpty()) {
                Text(text = "Loading pens…", style = MeshaType.caption, color = MeshaColors.Muted)
            }
            pens.forEach { pen ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .background(MeshaColors.Surf3)
                        .clickable { onOpenPen(pen.taskId) }
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(text = pen.penLabel, style = MeshaType.cardSubtitle, color = MeshaColors.Ink)
                    Spacer(Modifier.weight(1f))
                    // A multi-pen round reopens PEN BY PEN, mirroring weighing: it closes at
                    // both grains and reopens at the bucket.
                    if (actionsEnabled && pen.reopenable) {
                        Text(
                            text = "Start it again",
                            style = MeshaType.caption,
                            color = MeshaColors.BrandD,
                            modifier = pcCareInlineActionModifier { onReopenPen(pen.taskId) },
                        )
                    } else {
                        Text(text = pen.statusLabel, style = MeshaType.caption, color = MeshaColors.Muted)
                    }
                }
                Spacer(Modifier.height(6.dp))
            }
        }
    }
}

/** The planner list's Active/Completed pill, weighing's WeighingFilterPill shape. */
@Composable
private fun PcCareRoundsTabPill(label: String, selected: Boolean, onClick: () -> Unit) {
    Text(
        text = label,
        style = MeshaType.caption,
        color = if (selected) MeshaColors.PageBg else MeshaColors.Ink,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) MeshaColors.BrandD else MeshaColors.Surf2)
            .clickable(onClick = onClick)
            // A tab is tapped with a thumb in a shed: it gets a real touch target, not just
            // the height of its own text.
            .minimumInteractiveComponentSize()
            .padding(horizontal = 14.dp, vertical = 8.dp),
    )
}
