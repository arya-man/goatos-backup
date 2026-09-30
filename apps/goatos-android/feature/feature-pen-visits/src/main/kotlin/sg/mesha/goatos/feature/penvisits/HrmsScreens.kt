package sg.mesha.goatos.feature.penvisits

// telemetry:exempt pure stateless renderers; EnquiryReportViewModel / RecordViolationViewModel /
// ForMeHrmsViewModel (in :app) own the hrms_* AnalyticsEventsHrms + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaCard
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.component.MeshaSectionLabel
import sg.mesha.goatos.core.designsystem.component.MeshaStatusPill
import sg.mesha.goatos.core.designsystem.component.MeshaTone
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone

/**
 * The HRMS part of the For me list, injected at the TOP of the pen-visit LazyColumn: the park
 * head's open enquiries (one card each) and the "Record a violation" entry. Nothing renders when
 * the server said this is not the person's (403) -- the tab then reads exactly as before.
 */
fun LazyListScope.forMeHrmsItems(hrms: ForMeHrmsUi, onEvent: (ForMeHrmsEvent) -> Unit) {
    if (hrms.showViolations) {
        item(key = "hrms_violations") {
            ViolationsEntry(onClick = { onEvent(ForMeHrmsEvent.OpenViolations) })
        }
    }
    if (hrms.showEnquiries && hrms.enquiries.isNotEmpty()) {
        item(key = "hrms_enquiries_label") {
            MeshaSectionLabel(
                text = stringResource(R.string.hrms_enquiries_section),
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 6.dp),
            )
        }
        itemsIndexed(hrms.enquiries, key = { _, card -> card.listKey }) { _, card ->
            EnquiryCard(card) { onEvent(ForMeHrmsEvent.OpenEnquiry(card.enquiryId)) }
        }
    }
    if (!hrms.penVisitsDenied && (hrms.showViolations || hrms.enquiries.isNotEmpty())) {
        item(key = "hrms_pen_visits_label") {
            MeshaSectionLabel(
                text = stringResource(R.string.hrms_pen_visits_section),
                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 6.dp),
            )
        }
    }
}

@Composable
private fun ViolationsEntry(onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(role = Role.Button, onClick = onClick)
            .heightIn(min = 52.dp)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.BrandD, modifier = Modifier.size(20.dp))
        Text(
            text = stringResource(R.string.hrms_violations),
            color = MeshaColors.BrandD,
            style = MeshaType.bodyStrong,
            modifier = Modifier.weight(1f),
        )
        Icon(MeshaIcons.Chevron, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(18.dp))
    }
}

@Composable
private fun EnquiryCard(card: EnquiryCardUi, onOpen: () -> Unit) {
    val accent = if (card.overdue) MeshaColors.Danger else MeshaColors.Warn
    Row(
        modifier = penVisitCardModifier(onClick = onOpen),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Box(
            modifier = Modifier
                .size(40.dp)
                .clip(RoundedCornerShape(12.dp))
                .background(accent.copy(alpha = 0.14f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(MeshaIcons.Death, contentDescription = null, tint = accent, modifier = Modifier.size(20.dp))
        }
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(text = card.title, color = MeshaColors.Ink, style = MeshaType.cardTitle, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (card.subjectLabel.isNotBlank()) {
                Text(text = card.subjectLabel, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
            MeshaStatusPill(label = card.statusLabel, tone = if (card.overdue) MeshaTone.Danger else MeshaTone.Warn)
            if (card.dueLabel.isNotBlank()) {
                Text(
                    text = stringResource(R.string.hrms_due, card.dueLabel),
                    color = if (card.overdue) MeshaColors.Danger else MeshaColors.Muted,
                    style = MeshaType.caption,
                )
            }
            if (card.parkLabel.isNotBlank()) {
                Text(text = card.parkLabel, color = MeshaColors.Faint, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * The enquiry report (L1 drill of For me). Open: the questions the enquiry's PINNED SOP version
 * asks, then each person responsible with a violation and a fine -- or nobody -- and Submit.
 * Submitted: what was answered and who was penalised, read only.
 */
@Composable
fun EnquiryReportScreen(
    state: EnquiryReportUiState,
    onEvent: (EnquiryReportEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg).imePadding()) {
        MeshaScreenHeader(
            title = state.title.ifBlank { stringResource(R.string.hrms_enquiry_fallback_title) },
            onBack = { onEvent(EnquiryReportEvent.Back) },
        )
        when {
            state.loading -> Loading()
            state.unavailable -> Unavailable(stringResource(R.string.hrms_enquiry_unavailable)) { onEvent(EnquiryReportEvent.Retry) }
            else -> EnquiryReportBody(state, onEvent)
        }
    }
}

@Composable
private fun EnquiryReportBody(state: EnquiryReportUiState, onEvent: (EnquiryReportEvent) -> Unit) {
    val open = !state.submitted
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 32.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        item(key = "head") { EnquiryHead(state) }
        item(key = "questions_label") { MeshaSectionLabel(stringResource(R.string.hrms_report)) }
        itemsIndexed(state.questions, key = { _, q -> "q:" + q.id }) { _, q ->
            QuestionCard(q, open, onEvent)
        }
        item(key = "responsible_label") {
            Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                MeshaSectionLabel(stringResource(if (open) R.string.hrms_responsible else R.string.hrms_recorded))
                if (open) {
                    Text(stringResource(R.string.hrms_responsible_hint), color = MeshaColors.Muted, style = MeshaType.caption)
                }
            }
        }
        if (!open) {
            if (state.recorded.isEmpty()) {
                item(key = "nobody") { Text(stringResource(R.string.hrms_nobody), color = MeshaColors.Muted, style = MeshaType.body) }
            }
            itemsIndexed(state.recorded, key = { _, v -> "v:" + v.listKey }) { _, v -> RecordedViolationCard(v) }
        } else if (state.types.isEmpty()) {
            item(key = "no_types") { Text(stringResource(R.string.hrms_no_types), color = MeshaColors.Muted, style = MeshaType.body) }
        } else {
            itemsIndexed(state.penalties, key = { index, _ -> "p:$index" }) { index, penalty ->
                PenaltyCard(index, penalty, state, onEvent)
            }
            item(key = "add_person") {
                OutlineAction(
                    label = stringResource(R.string.hrms_add_person),
                    enabled = !state.submitting,
                    onClick = { onEvent(EnquiryReportEvent.AddPerson) },
                )
            }
        }
        if (open) {
            item(key = "submit") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (state.message.isNotBlank()) ErrorLine(state.message)
                    MeshaPrimaryButton(
                        text = stringResource(if (state.submitting) R.string.hrms_submitting else R.string.hrms_submit_report),
                        enabled = !state.submitting,
                        onClick = { onEvent(EnquiryReportEvent.Submit) },
                    )
                }
            }
        } else if (state.queuedOffline) {
            item(key = "queued") { Text(stringResource(R.string.hrms_queued_offline), color = MeshaColors.Muted, style = MeshaType.caption) }
        }
    }
}

@Composable
private fun EnquiryHead(state: EnquiryReportUiState) {
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            if (state.subjectLabel.isNotBlank()) {
                Text(state.subjectLabel, color = MeshaColors.Ink, style = MeshaType.cardTitle)
            }
            if (state.parkLabel.isNotBlank()) {
                Text(state.parkLabel, color = MeshaColors.Muted, style = MeshaType.cardSubtitle)
            }
            MeshaStatusPill(
                label = state.statusLabel,
                tone = when {
                    state.submitted -> MeshaTone.Ok
                    state.overdue -> MeshaTone.Danger
                    else -> MeshaTone.Warn
                },
            )
            FactLine(stringResource(R.string.hrms_happened), state.happenedLabel)
            FactLine(stringResource(R.string.hrms_opened), state.openedLabel)
            FactLine(stringResource(R.string.hrms_due_label), state.dueLabel, danger = state.overdue && !state.submitted)
            if (state.submittedLine.isNotBlank()) {
                Text(state.submittedLine, color = MeshaColors.Ok, style = MeshaType.bodyStrong)
            }
        }
    }
}

@Composable
private fun FactLine(label: String, value: String, danger: Boolean = false) {
    if (value.isBlank()) return
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(label, color = MeshaColors.Faint, style = MeshaType.caption, modifier = Modifier.padding(top = 1.dp))
        Text(value, color = if (danger) MeshaColors.Danger else MeshaColors.Ink, style = MeshaType.body)
    }
}

@Composable
private fun QuestionCard(q: EnquiryQuestionUi, open: Boolean, onEvent: (EnquiryReportEvent) -> Unit) {
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(q.title, color = MeshaColors.Ink, style = MeshaType.bodyStrong, modifier = Modifier.weight(1f, fill = false))
                if (q.required && open) Text(stringResource(R.string.hrms_required), color = MeshaColors.Faint, style = MeshaType.caption)
            }
            when {
                !open -> Text(
                    text = when {
                        q.kind == "yes_no" && q.yes != null -> stringResource(if (q.yes) R.string.hrms_yes else R.string.hrms_no)
                        q.text.isNotBlank() -> q.text
                        else -> "—"
                    },
                    color = MeshaColors.Ink,
                    style = MeshaType.body,
                )
                q.kind == "yes_no" -> Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    ChoiceChip(stringResource(R.string.hrms_yes), selected = q.yes == true) { onEvent(EnquiryReportEvent.AnswerYesNo(q.id, true)) }
                    ChoiceChip(stringResource(R.string.hrms_no), selected = q.yes == false) { onEvent(EnquiryReportEvent.AnswerYesNo(q.id, false)) }
                }
                else -> OutlinedTextField(
                    value = q.text,
                    onValueChange = { onEvent(EnquiryReportEvent.AnswerText(q.id, it.take(MAX_TEXT))) },
                    modifier = Modifier.fillMaxWidth(),
                    minLines = 3,
                )
            }
        }
    }
}

@Composable
private fun PenaltyCard(index: Int, penalty: PenaltyDraftUi, state: EnquiryReportUiState, onEvent: (EnquiryReportEvent) -> Unit) {
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            ChoicePicker(
                label = stringResource(R.string.hrms_person),
                placeholder = stringResource(R.string.hrms_person_choose),
                options = state.people,
                selectedKey = penalty.personId,
                onSelect = { onEvent(EnquiryReportEvent.SetPerson(index, it)) },
            )
            ChoicePicker(
                label = stringResource(R.string.hrms_violation),
                placeholder = stringResource(R.string.hrms_violation_choose),
                options = state.types,
                selectedKey = penalty.typeKey,
                onSelect = { onEvent(EnquiryReportEvent.SetType(index, it)) },
            )
            FineField(penalty.fine) { onEvent(EnquiryReportEvent.SetFine(index, it)) }
            OutlinedTextField(
                value = penalty.note,
                onValueChange = { onEvent(EnquiryReportEvent.SetNote(index, it.take(MAX_TEXT))) },
                label = { Text(stringResource(R.string.hrms_note)) },
                modifier = Modifier.fillMaxWidth(),
            )
            OutlineAction(
                label = stringResource(R.string.hrms_remove),
                enabled = !state.submitting,
                onClick = { onEvent(EnquiryReportEvent.RemovePerson(index)) },
            )
        }
    }
}

@Composable
private fun RecordedViolationCard(v: RecordedViolationUi) {
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(v.personName, color = MeshaColors.Ink, style = MeshaType.bodyStrong)
            Text("${v.typeLabel} · ${v.fineLabel}", color = MeshaColors.Muted, style = MeshaType.body)
            if (v.note.isNotBlank()) Text(v.note, color = MeshaColors.Faint, style = MeshaType.caption)
        }
    }
}

/** Record a violation (L1 drill of For me): a person in the park head's parks, a type, a fine. */
@Composable
fun RecordViolationScreen(
    state: RecordViolationUiState,
    onEvent: (RecordViolationEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg).imePadding()) {
        MeshaScreenHeader(
            title = stringResource(R.string.hrms_record_violation),
            onBack = { onEvent(RecordViolationEvent.Back) },
        )
        when {
            state.loading -> Loading()
            state.unavailable -> Unavailable(stringResource(R.string.hrms_violation_unavailable)) { onEvent(RecordViolationEvent.Retry) }
            state.done -> RecordViolationDone(state, onEvent)
            else -> RecordViolationForm(state, onEvent)
        }
    }
}

@Composable
private fun RecordViolationForm(state: RecordViolationUiState, onEvent: (RecordViolationEvent) -> Unit) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 32.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        if (state.people.isEmpty() || state.types.isEmpty()) {
            item(key = "empty") {
                EmptyState(
                    title = stringResource(if (state.types.isEmpty()) R.string.hrms_no_types else R.string.hrms_no_people),
                    modifier = Modifier.fillMaxWidth(),
                    icon = MeshaIcons.Warn,
                    tone = EmptyTone.Neutral,
                )
            }
            return@LazyColumn
        }
        item(key = "form") {
            MeshaCard {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    ChoicePicker(
                        label = stringResource(R.string.hrms_person),
                        placeholder = stringResource(R.string.hrms_person_choose),
                        options = state.people,
                        selectedKey = state.personId,
                        onSelect = { onEvent(RecordViolationEvent.SetPerson(it)) },
                    )
                    ChoicePicker(
                        label = stringResource(R.string.hrms_violation),
                        placeholder = stringResource(R.string.hrms_violation_choose),
                        options = state.types,
                        selectedKey = state.typeKey,
                        onSelect = { onEvent(RecordViolationEvent.SetType(it)) },
                    )
                    FineField(state.fine) { onEvent(RecordViolationEvent.SetFine(it)) }
                    ChoicePicker(
                        label = stringResource(R.string.hrms_when),
                        placeholder = "",
                        options = state.days,
                        selectedKey = state.day,
                        onSelect = { onEvent(RecordViolationEvent.SetDay(it)) },
                    )
                    OutlinedTextField(
                        value = state.note,
                        onValueChange = { onEvent(RecordViolationEvent.SetNote(it.take(MAX_TEXT))) },
                        label = { Text(stringResource(R.string.hrms_note)) },
                        modifier = Modifier.fillMaxWidth(),
                        minLines = 2,
                    )
                }
            }
        }
        item(key = "submit") {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (state.message.isNotBlank()) ErrorLine(state.message)
                MeshaPrimaryButton(
                    text = stringResource(if (state.submitting) R.string.hrms_submitting else R.string.hrms_record),
                    enabled = state.canSubmit,
                    onClick = { onEvent(RecordViolationEvent.Submit) },
                )
            }
        }
    }
}

@Composable
private fun RecordViolationDone(state: RecordViolationUiState, onEvent: (RecordViolationEvent) -> Unit) {
    Column(
        modifier = Modifier.fillMaxWidth().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        EmptyState(
            title = stringResource(if (state.queuedOffline) R.string.hrms_violation_queued else R.string.hrms_violation_recorded),
            modifier = Modifier.fillMaxWidth(),
            icon = MeshaIcons.CheckCircle,
            tone = EmptyTone.Neutral,
        )
        MeshaPrimaryButton(
            text = stringResource(R.string.hrms_record_another),
            enabled = true,
            onClick = { onEvent(RecordViolationEvent.RecordAnother) },
        )
    }
}

/** A labelled single choice over backend options. The selected option's label, verbatim. */
@Composable
private fun ChoicePicker(
    label: String,
    placeholder: String,
    options: List<ChoiceUi>,
    selectedKey: String,
    onSelect: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val selected = options.firstOrNull { it.key == selectedKey }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(label, color = MeshaColors.Faint, style = MeshaType.fieldLabel)
        Box {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MeshaColors.Surf)
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                    .clickable(role = Role.DropdownList) { expanded = true }
                    .heightIn(min = 50.dp)
                    .padding(horizontal = 14.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = selected?.label ?: placeholder,
                        color = if (selected != null) MeshaColors.Ink else MeshaColors.Faint,
                        style = MeshaType.body,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (selected != null && selected.detail.isNotBlank()) {
                        Text(selected.detail, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                }
                Icon(MeshaIcons.ChevronDown, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(18.dp))
            }
            DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
                options.forEach { option -> // compose-guard:ignore: bounded backend option list (a park's people / the SOP's types / seven days)
                    DropdownMenuItem(
                        text = {
                            Column {
                                Text(option.label, style = MeshaType.body, color = MeshaColors.Ink)
                                if (option.detail.isNotBlank()) Text(option.detail, style = MeshaType.caption, color = MeshaColors.Muted)
                            }
                        },
                        onClick = {
                            expanded = false
                            onSelect(option.key)
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun FineField(value: String, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = value,
        onValueChange = { raw -> onChange(raw.filter { it.isDigit() }.take(MAX_FINE_DIGITS)) },
        label = { Text(stringResource(R.string.hrms_fine)) },
        placeholder = { Text(stringResource(R.string.hrms_fine_default)) },
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun ChoiceChip(label: String, selected: Boolean, onClick: () -> Unit) {
    Text(
        text = label,
        color = if (selected) MeshaColors.BrandD else MeshaColors.Muted,
        style = if (selected) MeshaType.pillStrong else MeshaType.pill,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) MeshaColors.BrandTint else MeshaColors.Surf)
            .border(1.dp, if (selected) MeshaColors.BrandD else MeshaColors.Hair, RoundedCornerShape(999.dp))
            .selectable(selected = selected, role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 10.dp),
    )
}

@Composable
private fun OutlineAction(label: String, enabled: Boolean, onClick: () -> Unit) {
    PenVisitGhostButton(label = label, enabled = enabled, onClick = onClick)
}

@Composable
private fun ErrorLine(message: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.Top) {
        Icon(MeshaIcons.Warn, contentDescription = null, tint = MeshaColors.Danger, modifier = Modifier.size(18.dp))
        Text(message, color = MeshaColors.Danger, style = MeshaType.body)
    }
}

@Composable
private fun Loading() {
    Box(modifier = Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = MeshaColors.BrandD)
    }
}

@Composable
private fun Unavailable(title: String, onRetry: () -> Unit) {
    EmptyState(
        title = title,
        modifier = Modifier.fillMaxWidth().padding(16.dp),
        icon = MeshaIcons.Warn,
        tone = EmptyTone.Warn,
        action = { PenVisitGhostButton(label = stringResource(R.string.pen_visits_action_try_again), enabled = true, onClick = onRetry) },
    )
}

private const val MAX_TEXT = 2000
private const val MAX_FINE_DIGITS = 7

/**
 * The park head's violations (L1 drill of For me): the month's recorded and withdrawn violations
 * in the parks they head, newest first, with the month's totals and the way to record another.
 * Paged ~20 rows with a PASSIVE loading footer, never a "Load more" button.
 */
@Composable
fun ViolationsListScreen(
    state: ViolationsListUiState,
    onEvent: (ViolationsListEvent) -> Unit,
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(ViolationsListEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = stringResource(R.string.hrms_violations),
            onBack = { onEvent(ViolationsListEvent.Back) },
            below = {
                SyncStatusIndicator(
                    isRefreshing = state.isRefreshing,
                    lastSyncedAt = state.lastSyncedAt,
                    hasData = state.rows.isNotEmpty() || !state.loading,
                    refreshFailedLabel = if (state.refreshFailed) stringResource(R.string.hrms_violations_refresh_failed) else null,
                )
            },
            actions = {
                SyncIconButton(
                    isSyncing = state.isRefreshing,
                    onSync = { onEvent(ViolationsListEvent.Refresh) },
                    contentDescription = stringResource(R.string.pen_visits_action_refresh),
                )
            },
        )
        when {
            state.loading -> Loading()
            state.unavailable -> Unavailable(stringResource(R.string.hrms_violation_unavailable)) { onEvent(ViolationsListEvent.Refresh) }
            else -> ViolationsListBody(state, onEvent)
        }
    }
}

@Composable
private fun ViolationsListBody(state: ViolationsListUiState, onEvent: (ViolationsListEvent) -> Unit) {
    val listState = rememberLazyListState()
    // Prefetch the next page while the reader is ~3 rows from the end: the footer only spins.
    val nearEnd by remember {
        derivedStateOf {
            val last = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            last >= listState.layoutInfo.totalItemsCount - 3
        }
    }
    LaunchedEffect(nearEnd, state.rows.size) {
        if (nearEnd) onEvent(ViolationsListEvent.LoadMore)
    }
    LazyColumn(
        state = listState,
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = 16.dp, end = 16.dp, top = 4.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (state.canRecord) {
            item(key = "record") {
                MeshaPrimaryButton(
                    text = stringResource(R.string.hrms_record_violation),
                    enabled = true,
                    onClick = { onEvent(ViolationsListEvent.Record) },
                )
            }
        }
        if (state.months.isNotEmpty()) {
            item(key = "months") {
                Row(
                    modifier = Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    state.months.forEach { m -> // compose-guard:ignore: at most twelve backend month chips
                        ChoiceChip(m.label, selected = m.key == state.month) { onEvent(ViolationsListEvent.SelectMonth(m.key)) }
                    }
                }
            }
        }
        item(key = "summary") {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp), modifier = Modifier.fillMaxWidth()) {
                SummaryTile(stringResource(R.string.hrms_summary_violations), state.count.toString(), Modifier.weight(1f))
                SummaryTile(stringResource(R.string.hrms_summary_fines), state.fineLabel.ifBlank { "—" }, Modifier.weight(1f))
                SummaryTile(stringResource(R.string.hrms_summary_people), state.people.toString(), Modifier.weight(1f))
            }
        }
        if (state.rows.isEmpty()) {
            item(key = "empty") {
                EmptyState(
                    title = stringResource(R.string.hrms_violations_empty),
                    modifier = Modifier.fillMaxWidth(),
                    icon = MeshaIcons.CheckCircle,
                    tone = EmptyTone.Neutral,
                )
            }
        }
        itemsIndexed(state.rows, key = { _, row -> row.listKey }) { _, row -> ViolationRow(row) }
        if (state.loadingMore) {
            item(key = "loading_footer") {
                Box(modifier = Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(color = MeshaColors.BrandD)
                }
            }
        }
    }
}

@Composable
private fun SummaryTile(label: String, value: String, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(label, color = MeshaColors.Faint, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(value, color = MeshaColors.Ink, style = MeshaType.cardTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun ViolationRow(row: ViolationRowUi) {
    MeshaCard {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    row.personName,
                    color = if (row.withdrawn) MeshaColors.Muted else MeshaColors.Ink,
                    style = MeshaType.bodyStrong,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(row.fineLabel, color = if (row.withdrawn) MeshaColors.Faint else MeshaColors.Ink, style = MeshaType.bodyStrong)
            }
            if (row.designation.isNotBlank()) {
                Text(row.designation, color = MeshaColors.Faint, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(row.typeLabel, color = MeshaColors.Muted, style = MeshaType.body)
            Text(
                listOf(row.dateLabel, row.sourceLabel, row.recordedByName).filter { it.isNotBlank() }.joinToString(" · "),
                color = MeshaColors.Faint,
                style = MeshaType.caption,
            )
            if (row.note.isNotBlank()) Text(row.note, color = MeshaColors.Muted, style = MeshaType.caption)
            if (row.withdrawn) {
                MeshaStatusPill(label = row.statusLabel, tone = MeshaTone.Muted)
                if (row.withdrawReason.isNotBlank()) Text(row.withdrawReason, color = MeshaColors.Faint, style = MeshaType.caption)
            }
        }
    }
}
