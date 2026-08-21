package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure stateless renderer; PcCarePlanViewModel (in :app) owns the pc_care_*
// AnalyticsEvents + CrashReporter wiring for the monitor list and the create wizard.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
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
 * The planner tab (`/pc/tasks`, offered by the backend only to principals who plan care work):
 * a flat monitor list of tasks for the chosen category + date, plus a simple stepped create flow
 * (category -> date -> park -> pen -> operators -> review -> create). Nav offers are
 * backend-composed; this screen performs no role checks of its own.
 */
@Composable
fun PcCarePlanScreen(
    state: PcCarePlanUiState,
    rows: LazyPagingItems<PcCareTaskCardUi>,
    onEvent: (PcCarePlanEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(PcCarePlanEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        when (state.step) {
            PcCarePlanStep.LIST -> PlanMonitorList(state, rows, onEvent)
            else -> PlanCreateWizard(state, onEvent)
        }
    }
}

@Composable
private fun PlanMonitorList(
    state: PcCarePlanUiState,
    rows: LazyPagingItems<PcCareTaskCardUi>,
    onEvent: (PcCarePlanEvent) -> Unit,
) {
    MeshaScreenHeader(
        title = state.title,
        subtitle = state.monitorDate.takeIf { it.isNotBlank() },
        actions = {
            SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(PcCarePlanEvent.Refresh) })
        },
    )
    state.message?.let { message ->
        Text(
            text = message,
            color = MeshaColors.Warn,
            style = MeshaType.caption,
            modifier = Modifier
                .padding(horizontal = 16.dp, vertical = 4.dp)
                .clickable { onEvent(PcCarePlanEvent.DismissMessage) },
        )
    }
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(bottom = 20.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        item(key = "new_task") {
            Button(
                onClick = { onEvent(PcCarePlanEvent.StartCreate) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = MeshaColors.BrandD,
                    contentColor = MeshaColors.PageBg,
                ),
            ) {
                Text(text = "Plan a care task", style = MeshaType.pillStrong)
            }
        }
        item(key = "category_chips") {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                state.categories.forEach { option -> // compose-guard:ignore: fixed backend category set (deworming/ticks/hoof/hair — 4 chips), never park-scale data
                    PlanChoiceChip(
                        label = option.label,
                        selected = option.key == state.monitorCategoryKey,
                        onClick = { onEvent(PcCarePlanEvent.SelectMonitorCategory(option.key)) },
                    )
                }
            }
        }
        item(key = "date_bar") {
            PcCareDateBar(
                selectedDateIso = state.monitorDate,
                onSelectDate = { onEvent(PcCarePlanEvent.SelectMonitorDate(it)) },
            )
        }
        if (rows.itemCount == 0 && state.emptyMessage != null) {
            item(key = "empty") {
                EmptyState(
                    title = state.emptyMessage,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                    icon = MeshaIcons.Check,
                    tone = EmptyTone.Neutral,
                )
            }
        }
        items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
            rows[index]?.let { card ->
                PcCareTaskCard(
                    card = card,
                    onCancel = { onEvent(PcCarePlanEvent.CancelTask(card.taskId)) },
                    onOpen = {},
                )
            }
        }
        if (rows.loadState.append is LoadState.Loading) {
            item(key = "loading_footer") {
                Box(modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = MeshaColors.BrandD)
                }
            }
        }
    }
}

@Composable
private fun PlanCreateWizard(
    state: PcCarePlanUiState,
    onEvent: (PcCarePlanEvent) -> Unit,
) {
    MeshaScreenHeader(
        title = "Plan a care task",
        subtitle = state.selectedCategoryLabel.takeIf { it.isNotBlank() },
        onBack = { onEvent(PcCarePlanEvent.PreviousStep) },
    )
    state.message?.let { message ->
        Text(
            text = message,
            color = MeshaColors.Warn,
            style = MeshaType.caption,
            modifier = Modifier
                .padding(horizontal = 16.dp, vertical = 4.dp)
                .clickable { onEvent(PcCarePlanEvent.DismissMessage) },
        )
    }
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        when (state.step) {
            PcCarePlanStep.CATEGORY -> {
                item(key = "step_title") { WizardStepTitle("Which work?") }
                items(count = state.categories.size, key = { state.categories[it].key }) { index ->
                    val option = state.categories[index]
                    WizardOptionRow(
                        label = option.label,
                        selected = option.key == state.selectedCategoryKey,
                        onClick = { onEvent(PcCarePlanEvent.SelectCategory(option.key)) },
                    )
                }
            }
            PcCarePlanStep.DATE -> {
                item(key = "step_title") { WizardStepTitle("For which day?") }
                item(key = "date_bar") {
                    PcCareDateBar(
                        selectedDateIso = state.selectedDate,
                        onSelectDate = { onEvent(PcCarePlanEvent.SelectDate(it)) },
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
                item(key = "step_title") { WizardStepTitle("Which pen?") }
                items(count = state.pens.size, key = { state.pens[it].shedId }) { index ->
                    val pen = state.pens[index]
                    val taken = pen.existingTaskId.isNotBlank()
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(horizontal = 16.dp)
                            .clip(RoundedCornerShape(12.dp))
                            .background(if (pen.shedId == state.selectedShedId) MeshaColors.Surf2 else MeshaColors.Surf)
                            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                            // A pen already covered by a live task is greyed out and inert.
                            .clickable(enabled = !taken) { onEvent(PcCarePlanEvent.SelectPen(pen.shedId, pen.partitionLabel)) }
                            .padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            // Backend-composed pen display, verbatim.
                            text = pen.locationDisplay,
                            color = if (taken) PcCareDim else MeshaColors.Ink,
                            style = MeshaType.cardSubtitle,
                            modifier = Modifier.weight(1f),
                        )
                        if (taken) {
                            Text(text = "Already planned", color = PcCareDim, style = MeshaType.caption)
                        }
                    }
                }
                if (!state.pensEndReached) {
                    item(key = "pens_footer") {
                        // Passive footer: composing it asks for the next page.
                        LaunchedEffect(state.pens.size) { onEvent(PcCarePlanEvent.LoadMorePens) }
                        Box(modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                            CircularProgressIndicator(color = MeshaColors.BrandD)
                        }
                    }
                } else if (state.pensLoading) {
                    item(key = "pens_loading") {
                        Box(modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                            CircularProgressIndicator(color = MeshaColors.BrandD)
                        }
                    }
                }
            }
            PcCarePlanStep.OPERATORS -> {
                item(key = "step_title") { WizardStepTitle("Who does the work?") }
                items(count = state.operators.size, key = { state.operators[it].key }) { index ->
                    val option = state.operators[index]
                    WizardOptionRow(
                        label = option.label,
                        selected = option.key in state.selectedOperatorIds,
                        onClick = { onEvent(PcCarePlanEvent.ToggleOperator(option.key)) },
                    )
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
                        ReviewLine("Day", state.selectedDate)
                        ReviewLine("Farm", state.selectedParkLabel)
                        ReviewLine("Pen", state.selectedPenLabel)
                        ReviewLine(
                            "People",
                            state.operators
                                .filter { it.key in state.selectedOperatorIds }
                                .joinToString(", ") { it.label },
                        )
                    }
                }
            }
            PcCarePlanStep.LIST -> Unit
        }

        item(key = "wizard_actions") {
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                OutlinedButton(onClick = { onEvent(PcCarePlanEvent.CloseCreate) }, modifier = Modifier.weight(1f)) {
                    Text(text = "Close", color = MeshaColors.Muted, style = MeshaType.pillStrong)
                }
                if (state.step == PcCarePlanStep.REVIEW) {
                    Button(
                        onClick = { onEvent(PcCarePlanEvent.Create) },
                        enabled = !state.creating,
                        modifier = Modifier.weight(1f),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = MeshaColors.BrandD,
                            contentColor = MeshaColors.PageBg,
                            disabledContainerColor = MeshaColors.Surf2,
                            disabledContentColor = MeshaColors.Faint,
                        ),
                    ) {
                        Text(text = if (state.creating) "Creating…" else "Create task", style = MeshaType.pillStrong)
                    }
                } else {
                    Button(
                        onClick = { onEvent(PcCarePlanEvent.NextStep) },
                        modifier = Modifier.weight(1f),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = MeshaColors.BrandD,
                            contentColor = MeshaColors.PageBg,
                        ),
                    ) {
                        Text(text = "Next", style = MeshaType.pillStrong)
                    }
                }
            }
        }
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
private fun WizardOptionRow(label: String, selected: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(if (selected) MeshaColors.Surf2 else MeshaColors.Surf)
            .border(1.dp, if (selected) MeshaColors.BrandD else MeshaColors.Hair, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = label, color = MeshaColors.Ink, style = MeshaType.cardSubtitle, modifier = Modifier.weight(1f))
        if (selected) {
            Text(text = "Selected", color = MeshaColors.BrandD, style = MeshaType.caption)
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

@Composable
private fun PlanChoiceChip(label: String, selected: Boolean, onClick: () -> Unit) {
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(10.dp))
            .background(if (selected) MeshaColors.BrandD else MeshaColors.Surf2)
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    ) {
        Text(
            text = label,
            color = if (selected) MeshaColors.PageBg else MeshaColors.Muted,
            style = MeshaType.pillStrong,
        )
    }
}
