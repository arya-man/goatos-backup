package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderers; AnimalPurchaseLoadsViewModel and
// AnimalPurchaseLoadCreateViewModel (in :app) own the animal_purchase_* AnalyticsEventsAnimalPurchase
// + CrashReporter wiring for every read refresh, row open and queued write.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.window.PopupProperties
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * The Procurement module's third tab (`/vendors/animal-purchases`): purchase loads, newest first
 * (maintainer decision 2026-09-13, docs/decisions/animal-purchases.md).
 *
 * Paged (~20 rows) with stable load-id keys and a PASSIVE loading footer — never a "Load more"
 * button (docs/decisions/mobile-data-fetch-anti-patterns.md). Refresh-on-open: every row's counts
 * move as the CEO decides on another surface, so the list re-reads whenever it is looked at.
 */
@Composable
fun AnimalPurchaseLoadsScreen(
    state: AnimalPurchaseLoadsUiState,
    rows: LazyPagingItems<AnimalPurchaseLoadCardUi>,
    onEvent: (AnimalPurchaseLoadsEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(AnimalPurchaseLoadsEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                below = {
                    SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = rows.itemCount > 0)
                },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(AnimalPurchaseLoadsEvent.Refresh) }) },
            )
            // A refresh that inserts a row ABOVE the first visible one (a load recorded a moment
            // ago) keeps the old row anchored; snap to the top so the new row is seen, but only
            // when the person is already near the top -- never yank a deliberate scroll.
            val listState = rememberLazyListState()
            val firstKey = if (rows.itemCount > 0) rows.peek(0)?.listKey else null
            LaunchedEffect(firstKey) {
                if (firstKey != null && listState.firstVisibleItemIndex in 1..3) listState.scrollToItem(0)
            }
            LazyColumn(
                state = listState,
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(top = 4.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (rows.itemCount == 0 && state.emptyMessage != null) {
                    item(key = "empty") {
                        EmptyState(
                            title = state.emptyMessage,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                            icon = if (state.isErrorEmpty) MeshaIcons.Warn else MeshaIcons.Goat,
                            tone = if (state.isErrorEmpty) EmptyTone.Warn else EmptyTone.Neutral,
                        )
                    }
                }
                items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                    rows[index]?.let { card ->
                        AnimalPurchaseLoadCard(card) { onEvent(AnimalPurchaseLoadsEvent.OpenLoad(card.loadId)) }
                    }
                }
                // Passive loading footer: the next page is already in flight while this spins.
                if (rows.loadState.append is LoadState.Loading) {
                    item(key = "loading_footer") {
                        Box(Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                            CircularProgressIndicator(color = MeshaColors.BrandD)
                        }
                    }
                }
            }
        }
        // ONLY the server's `can_record` shows the action: a reader never sees a button the
        // write route would refuse.
        if (state.canRecord && state.addLabel.isNotBlank()) {
            VendorsAddButton(
                label = state.addLabel,
                onClick = { onEvent(AnimalPurchaseLoadsEvent.AddLoad) },
                modifier = Modifier.align(Alignment.BottomEnd).padding(MeshaDimens.gutter),
            )
        }
    }
}

@Composable
private fun AnimalPurchaseLoadCard(card: AnimalPurchaseLoadCardUi, onClick: () -> Unit) {
    VendorsCard(onClick = onClick) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            VendorsIconTile(icon = MeshaIcons.Goat, tint = MeshaColors.Info, background = MeshaColors.InfoX)
            Column(Modifier.weight(1f)) {
                // Backend-owned title and summary, rendered verbatim.
                Text(text = card.title, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(2.dp))
                Text(text = card.summary, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        AnimalPurchaseCountRow(pending = card.pending, accepted = card.accepted, rejected = card.rejected)
    }
}

/**
 * The three whole-load counts as small chips. The backend publishes no words for them on the list
 * row, so the chips carry the bare figures; the tone says which is which (awaiting / accepted /
 * rejected), the same three tones the decision chip on an animal uses.
 */
@Composable
internal fun AnimalPurchaseCountRow(pending: Int, accepted: Int, rejected: Int, modifier: Modifier = Modifier) {
    Row(modifier = modifier, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
        VendorsChip(label = pending.toString(), tone = VendorsTone.NEUTRAL)
        VendorsChip(label = accepted.toString(), tone = VendorsTone.OK)
        VendorsChip(label = rejected.toString(), tone = VendorsTone.DANGER)
    }
}

/**
 * Record a purchase load (L1 drill): load number, vendor from the register, the farm it is for,
 * roughly how many animals, and a note. Every label comes from the backend `copy` map.
 *
 * The form never closes on enqueue: it follows the queued row and opens the recorded load once the
 * server has accepted it (the load id is the SERVER's), or shows the server's own sentence when the
 * write was refused.
 */
@Composable
fun AnimalPurchaseLoadCreateScreen(
    state: AnimalPurchaseLoadCreateUiState,
    onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LaunchedEffect(state.createdLoadId) {
        state.createdLoadId?.let { onEvent(AnimalPurchaseLoadCreateEvent.OpenCreatedLoad(it)) }
    }
    val copy = state.copy
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    val requiredHint = copy[COPY_REQUIRED_HINT].orEmpty()
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg).imePadding()) {
        MeshaScreenHeader(title = copy[COPY_LOAD_FORM_TITLE].orEmpty(), onBack = { onEvent(AnimalPurchaseLoadCreateEvent.Back) })
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            if (state.questions.isNotEmpty()) {
                // PROCUREMENT SOP: the served load form, in the SOP's order. Locked questions keep
                // their own widget; every authored question renders by kind.
                items(state.questions, key = { "q:" + it.id }) { question ->
                    AnimalPurchaseLoadQuestion(question = question, state = state, locked = locked, requiredHint = requiredHint, onEvent = onEvent)
                }
            } else {
                item(key = "load_ref") { LoadRefField(state, locked, requiredHint, copy[COPY_LOAD_FIELD_LOAD_REF].orEmpty(), true, null, onEvent) }
                item(key = "vendor") { VendorField(state, locked, requiredHint, copy[COPY_LOAD_FIELD_VENDOR].orEmpty(), onEvent) }
                item(key = "farm") { FarmField(state, locked, requiredHint, copy[COPY_LOAD_FIELD_FARM].orEmpty(), onEvent) }
                item(key = "expected") { ExpectedField(state, locked, requiredHint, copy[COPY_LOAD_FIELD_EXPECTED].orEmpty(), false, null, onEvent) }
                item(key = "notes") { NotesField(state, locked, requiredHint, copy[COPY_LOAD_FIELD_NOTES].orEmpty(), false, null, onEvent) }
            }
            item(key = "hint") { Text(text = requiredHint, color = MeshaColors.Muted, style = MeshaType.caption) }
        }
        Row(modifier = Modifier.fillMaxWidth().padding(MeshaDimens.gutter)) {
            VendorsPrimaryButton(
                label = copy[COPY_LOAD_SAVE].orEmpty(),
                enabled = !locked && !state.submitInFlight,
                onClick = { onEvent(AnimalPurchaseLoadCreateEvent.Submit) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@Composable
private fun LoadRefField(state: AnimalPurchaseLoadCreateUiState, locked: Boolean, requiredHint: String, label: String, required: Boolean, hint: String?, onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit) {
    VendorsTextField(
        value = state.values[AnimalPurchaseLoadField.LOAD_REF].orEmpty(),
        onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, it)) },
        label = label,
        required = required,
        readOnly = locked,
        supporting = hint,
        error = requiredHint.takeIf { AnimalPurchaseLoadField.LOAD_REF in state.fieldErrors },
    )
}

@Composable
private fun VendorField(state: AnimalPurchaseLoadCreateUiState, locked: Boolean, requiredHint: String, label: String, onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit) {
    AnimalPurchaseVendorSearchField(
        label = label,
        selectedValue = state.values[AnimalPurchaseLoadField.VENDOR].orEmpty(),
        options = state.vendors,
        readOnly = locked,
        onSelect = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.VENDOR, it)) },
        error = requiredHint.takeIf { AnimalPurchaseLoadField.VENDOR in state.fieldErrors },
    )
}

@Composable
private fun FarmField(state: AnimalPurchaseLoadCreateUiState, locked: Boolean, requiredHint: String, label: String, onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit) {
    VendorsFormGroup(title = label) {
        VendorsSegmented(
            options = state.farms,
            selectedValue = state.values[AnimalPurchaseLoadField.FARM].orEmpty(),
            onSelect = { if (!locked) onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.FARM, it)) },
        )
        if (AnimalPurchaseLoadField.FARM in state.fieldErrors) {
            Text(text = requiredHint, color = MeshaColors.Danger, style = MeshaType.caption)
        }
    }
}

@Composable
private fun ExpectedField(state: AnimalPurchaseLoadCreateUiState, locked: Boolean, requiredHint: String, label: String, required: Boolean, hint: String?, onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit) {
    VendorsTextField(
        value = state.values[AnimalPurchaseLoadField.EXPECTED_COUNT].orEmpty(),
        onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.EXPECTED_COUNT, it)) },
        label = label,
        required = required,
        keyboard = KeyboardType.Number,
        readOnly = locked,
        supporting = hint,
        error = requiredHint.takeIf { AnimalPurchaseLoadField.EXPECTED_COUNT in state.fieldErrors },
    )
}

@Composable
private fun NotesField(state: AnimalPurchaseLoadCreateUiState, locked: Boolean, requiredHint: String, label: String, required: Boolean, hint: String?, onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit) {
    VendorsTextField(
        value = state.values[AnimalPurchaseLoadField.NOTES].orEmpty(),
        onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.NOTES, it)) },
        label = label,
        required = required,
        singleLine = false,
        readOnly = locked,
        supporting = hint,
        error = requiredHint.takeIf { AnimalPurchaseLoadField.NOTES in state.fieldErrors },
    )
}

/**
 * One served load-form question. The five locked questions (load_ref / vendor / farm /
 * expected_count / notes) keep the widgets the load has always had, but their title, hint and
 * compulsory mark are the SOP's; every other question renders by kind exactly like the animal
 * form -- pick-one (segmented when short), pick-many, number with unit and range, free text.
 */
@Composable
private fun AnimalPurchaseLoadQuestion(
    question: AnimalPurchaseQuestionUi,
    state: AnimalPurchaseLoadCreateUiState,
    locked: Boolean,
    requiredHint: String,
    onEvent: (AnimalPurchaseLoadCreateEvent) -> Unit,
) {
    val hint = question.hint.takeIf { it.isNotBlank() }
    when (question.id) {
        "load_ref" -> LoadRefField(state, locked, requiredHint, question.title, question.required, hint, onEvent)
        "vendor" -> VendorField(state, locked, requiredHint, question.title, onEvent)
        "farm" -> FarmField(state, locked, requiredHint, question.title, onEvent)
        "expected_count" -> ExpectedField(state, locked, requiredHint, question.title, question.required, hint, onEvent)
        "notes" -> NotesField(state, locked, requiredHint, question.title, question.required, hint, onEvent)
        else -> when (question.kind) {
            AnimalPurchaseQuestionKind.CHOICE -> VendorsFormGroup(title = if (question.required) question.title + " *" else question.title) {
                hint?.let { Text(text = it, color = MeshaColors.Muted, style = MeshaType.caption) }
                val selected = state.answers[question.id].orEmpty()
                val segmented = question.options.size <= 3 && question.options.all { it.label.length <= 14 }
                if (segmented) {
                    VendorsSegmented(options = question.options, selectedValue = selected, onSelect = { if (!locked) onEvent(AnimalPurchaseLoadCreateEvent.AnswerChanged(question.id, it)) })
                } else {
                    Column(Modifier.fillMaxWidth()) {
                        question.options.forEach { option ->
                            AnimalPurchaseOptionRow(label = option.label, selected = option.value == selected, single = true, enabled = !locked) {
                                onEvent(AnimalPurchaseLoadCreateEvent.AnswerChanged(question.id, option.value))
                            }
                        }
                    }
                }
                if (question.allowOther && selected == "other") {
                    VendorsTextField(
                        value = state.answers[question.id + "_other"].orEmpty(),
                        onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.AnswerChanged(question.id + "_other", it)) },
                        label = state.copy[COPY_ANIMAL_OTHER_HINT].orEmpty(),
                        readOnly = locked,
                    )
                }
                question.error?.let { Text(text = it, color = MeshaColors.Danger, style = MeshaType.caption) }
            }
            AnimalPurchaseQuestionKind.MULTI -> VendorsFormGroup(title = if (question.required) question.title + " *" else question.title) {
                hint?.let { Text(text = it, color = MeshaColors.Muted, style = MeshaType.caption) }
                val ticked = state.multiAnswers[question.id].orEmpty()
                Column(Modifier.fillMaxWidth()) {
                    question.options.forEach { option ->
                        val checked = option.value in ticked
                        AnimalPurchaseOptionRow(label = option.label, selected = checked, single = false, enabled = !locked) {
                            onEvent(AnimalPurchaseLoadCreateEvent.MultiToggled(question.id, option.value, !checked))
                        }
                    }
                }
                question.error?.let { Text(text = it, color = MeshaColors.Danger, style = MeshaType.caption) }
            }
            AnimalPurchaseQuestionKind.NUMBER -> VendorsTextField(
                value = state.answers[question.id].orEmpty(),
                onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.AnswerChanged(question.id, it)) },
                label = question.title,
                required = question.required,
                keyboard = KeyboardType.Decimal,
                readOnly = locked,
                error = question.error,
                supporting = listOf(question.hint, question.rangeLine).filter { it.isNotBlank() }.joinToString(" · ").takeIf { it.isNotBlank() },
                trailing = question.unit.takeIf { it.isNotBlank() }?.let { unit -> { Text(text = unit, color = MeshaColors.Muted, style = MeshaType.rowValue) } },
            )
            else -> VendorsTextField(
                value = state.answers[question.id].orEmpty(),
                onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.AnswerChanged(question.id, it)) },
                label = question.title,
                required = question.required,
                readOnly = locked,
                error = question.error,
                supporting = hint,
            )
        }
    }
}

// The backend `copy` map's keys (backend/internal/animalpurchase/adapters/http/handler.go). These
// are KEYS, never words: every visible label is the map's value, rendered verbatim.
const val COPY_REQUIRED_HINT = "required.hint"
const val COPY_LOADS_TITLE = "loads.title"
const val COPY_LOADS_EMPTY = "loads.empty"
const val COPY_LOADS_ADD = "loads.add"
const val COPY_LOAD_FORM_TITLE = "load.form.title"
const val COPY_LOAD_FIELD_LOAD_REF = "load.field.load_ref"
const val COPY_LOAD_FIELD_VENDOR = "load.field.vendor"
const val COPY_LOAD_FIELD_FARM = "load.field.farm"
const val COPY_LOAD_FIELD_EXPECTED = "load.field.expected"
const val COPY_LOAD_FIELD_NOTES = "load.field.notes"
const val COPY_LOAD_SAVE = "load.save"
const val COPY_LOAD_ANIMALS_TITLE = "load.animals.title"
const val COPY_LOAD_ANIMALS_EMPTY = "load.animals.empty"
const val COPY_LOAD_ANIMALS_ADD = "load.animals.add"
const val COPY_ANIMAL_FORM_TITLE = "animal.form.title"
const val COPY_ANIMAL_FORM_HINT = "animal.form.hint"
const val COPY_ANIMAL_MEDIA_PHOTO = "animal.media.photo"
const val COPY_ANIMAL_MEDIA_VIDEO = "animal.media.video"
const val COPY_ANIMAL_MEDIA_ADD_MORE = "animal.media.add_more"
const val COPY_ANIMAL_MEDIA_REMOVE = "animal.media.remove"
const val COPY_ANIMAL_OTHER_HINT = "animal.other.hint"
const val COPY_ANIMAL_SEND_FAILED = "animal.send_failed"
const val COPY_ANIMAL_SAVE = "animal.save"
const val COPY_ANIMAL_SAVING = "animal.saving"
const val COPY_ANIMAL_DECISION_PENDING = "animal.decision.pending"
const val COPY_ANIMAL_DECIDED_BY = "animal.decided_by"
const val COPY_ANIMAL_DETAIL_ANSWERS = "animal.detail.answers"
const val COPY_ANIMAL_DETAIL_MEDIA = "animal.detail.media"
const val COPY_ANIMAL_DETAIL_EMPTY = "animal.detail.empty"
const val COPY_ANIMAL_DETAIL_ATTENTION = "animal.detail.attention"

/**
 * Type-to-find vendor picker (maintainer instruction 2026-09-13: "we have so many vendors, it
 * will be hard to just scroll"). The person types part of the name and picks from the bounded
 * matches underneath; the field stores the VENDOR ID, never the typed text, so a name that
 * matches no register row cannot be saved. Clearing the text clears the selection. Matches are
 * by substring anywhere in the name, case-insensitive, so "mutton" finds every mutton stall.
 */
@Composable
private fun AnimalPurchaseVendorSearchField(
    label: String,
    selectedValue: String,
    options: List<VendorsOptionUi>,
    readOnly: Boolean,
    onSelect: (String) -> Unit,
    error: String?,
) {
    val selectedLabel = options.firstOrNull { it.value == selectedValue }?.label.orEmpty()
    var query by remember(selectedValue) { mutableStateOf(selectedLabel) }
    var dismissedFor by remember { mutableStateOf<String?>(null) }
    val matches = remember(query, options, selectedLabel) {
        val needle = query.trim()
        when {
            needle.isBlank() -> emptyList()
            needle.equals(selectedLabel, ignoreCase = true) -> emptyList()
            else -> options.filter { it.label.contains(needle, ignoreCase = true) }.take(VENDOR_MATCH_LIMIT)
        }
    }
    Box(Modifier.fillMaxWidth()) {
        VendorsTextField(
            value = query,
            onValueChange = { typed ->
                query = typed
                dismissedFor = null
                // Typing over a chosen vendor un-chooses it until a row is picked again.
                if (selectedValue.isNotBlank() && !typed.equals(selectedLabel, ignoreCase = true)) onSelect("")
            },
            label = label,
            required = true,
            readOnly = readOnly,
            error = error,
            trailing = { Icon(MeshaIcons.ChevronDown, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd)) },
        )
        DropdownMenu(
            expanded = !readOnly && matches.isNotEmpty() && dismissedFor != query,
            onDismissRequest = { dismissedFor = query },
            properties = PopupProperties(focusable = false),
            modifier = Modifier.heightIn(max = 280.dp).background(MeshaColors.Surf),
        ) {
            matches.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.label, color = MeshaColors.Ink, style = MeshaType.body) },
                    onClick = {
                        query = option.label
                        dismissedFor = option.label
                        onSelect(option.value)
                    },
                )
            }
        }
    }
}

private const val VENDOR_MATCH_LIMIT = 8

