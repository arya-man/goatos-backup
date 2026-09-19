package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderers; AnimalPurchaseLoadDetailViewModel and
// AnimalPurchaseAnimalCreateViewModel (in :app) own the animal_purchase_* AnalyticsEventsAnimalPurchase
// + CrashReporter wiring for every read refresh, capture and queued write.

import androidx.compose.foundation.background
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.RadioButton
import androidx.compose.material3.RadioButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import androidx.paging.compose.itemKey
import kotlinx.coroutines.delay
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.EmptyState
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.ProofMediaPreview
import sg.mesha.goatos.core.ui.ProofMediaPreviewKind
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * How often the open load screen re-reads the server while it stays RESUMED. The CEO decides on
 * admin-web, and the maintainer wants the answer on the phone without a tap
 * (docs/decisions/animal-purchases.md rule 5). Lifecycle-bound: the loop runs only while resumed.
 */
const val ANIMAL_PURCHASE_LOAD_AUTO_REFRESH_MS = 30_000L

/**
 * One purchase load (L1 drill): the backend title, summary and whole-load counts on top, then the
 * animals recorded in it, each with its SERVER-owned decision chip. Refresh-on-open, on resume AND
 * every [ANIMAL_PURCHASE_LOAD_AUTO_REFRESH_MS] while resumed — the phone's stored copy is a
 * placeholder the next server read overwrites.
 */
@Composable
fun AnimalPurchaseLoadDetailScreen(
    state: AnimalPurchaseLoadDetailUiState,
    rows: LazyPagingItems<AnimalPurchaseAnimalCardUi>,
    onEvent: (AnimalPurchaseLoadDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(AnimalPurchaseLoadDetailEvent.Refresh) }
    val listState = rememberLazyListState()
    // Periodic re-read, bound to the RESUMED state: leaving the screen or backgrounding the app
    // cancels the loop, returning restarts it.
    val lifecycleOwner = LocalLifecycleOwner.current
    LaunchedEffect(lifecycleOwner) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            while (true) {
                delay(ANIMAL_PURCHASE_LOAD_AUTO_REFRESH_MS)
                onEvent(AnimalPurchaseLoadDetailEvent.Refresh)
            }
        }
    }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                subtitle = state.summary.ifBlank { null },
                onBack = { onEvent(AnimalPurchaseLoadDetailEvent.Back) },
                below = {
                    if (state.countChips.isNotEmpty()) {
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                            // A small FIXED set of three chips (awaiting / accepted / rejected), each
                            // a backend-worded label with its whole-load count.
                            state.countChips.forEach { chip ->
                                VendorsChip(label = listOf(chip.label, chip.count.toString()).filter { it.isNotBlank() }.joinToString(" "), tone = chip.tone)
                            }
                        }
                    }
                    SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = rows.itemCount > 0)
                },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(AnimalPurchaseLoadDetailEvent.Refresh) }) },
            )
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                state = listState,
                contentPadding = PaddingValues(top = 4.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (state.animalsTitle.isNotBlank()) {
                    item(key = "animals_title") {
                        Text(
                            text = state.animalsTitle,
                            color = MeshaColors.Muted,
                            style = MeshaType.sectionLabel,
                            modifier = Modifier.padding(horizontal = MeshaDimens.gutter, vertical = 4.dp),
                        )
                    }
                }
                if (rows.itemCount == 0 && state.queuedAnimals.isEmpty() && state.emptyMessage != null) {
                    item(key = "empty") {
                        EmptyState(
                            title = state.emptyMessage,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter),
                            icon = MeshaIcons.Goat,
                            tone = EmptyTone.Neutral,
                        )
                    }
                }
                items(
                    items = state.queuedAnimals,
                    key = { "queued:" + it.listKey },
                ) { queued -> AnimalPurchaseQueuedAnimalCard(queued, onRetry = { onEvent(AnimalPurchaseLoadDetailEvent.RetryQueued(queued.listKey)) }) }
                items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                    rows[index]?.let { card ->
                        AnimalPurchaseAnimalCard(
                            card = card,
                            decidedByLabel = state.decidedByLabel,
                            inlineRemotePhoto = false,
                            onPreviewAction = { onEvent(AnimalPurchaseLoadDetailEvent.PreviewAction(it)) },
                            onOpen = { onEvent(AnimalPurchaseLoadDetailEvent.OpenAnimal(card.candidateId)) },
                        )
                    }
                }
                if (rows.loadState.append is LoadState.Loading) {
                    item(key = "loading_footer") {
                        Box(Modifier.fillMaxWidth().padding(vertical = 12.dp), contentAlignment = Alignment.Center) {
                            CircularProgressIndicator(color = MeshaColors.BrandD)
                        }
                    }
                }
            }
        }
        if (state.canRecord && state.addLabel.isNotBlank()) {
            VendorsAddButton(
                label = state.addLabel,
                onClick = { onEvent(AnimalPurchaseLoadDetailEvent.AddAnimal) },
                modifier = Modifier.align(Alignment.BottomEnd).padding(MeshaDimens.gutter),
            )
        }
    }
}

@Composable
private fun AnimalPurchaseAnimalCard(
    card: AnimalPurchaseAnimalCardUi,
    decidedByLabel: String,
    inlineRemotePhoto: Boolean,
    onPreviewAction: (String) -> Unit,
    onOpen: () -> Unit,
) {
    // The whole card opens the animal's record (every answer and capture); the inline preview
    // keeps its own play/expand controls on top of that.
    VendorsCard(onClick = onOpen) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Column(Modifier.weight(1f)) {
                // Backend-owned title, rendered verbatim.
                Text(text = card.title, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val detailLine = listOf(card.breed, card.ageWeightLine).filter { it.isNotBlank() }.joinToString(" · ")
                if (detailLine.isNotBlank()) {
                    Spacer(Modifier.height(2.dp))
                    Text(text = detailLine, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            // The SERVER's decision chip, verbatim, in its tone, beside the inspector's own
            // field verdict when the row carries one (a questionnaire row).
            Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                VendorsChip(label = card.decisionLabel, tone = card.decisionTone)
                if (card.fieldVerdictLabel.isNotBlank()) VendorsChip(label = card.fieldVerdictLabel, tone = card.fieldVerdictTone)
            }
        }
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (card.conditionLabel.isNotBlank()) VendorsChip(label = card.conditionLabel, tone = VendorsTone.INFO)
            if (card.tempTag.isNotBlank()) {
                Text(text = card.tempTag, color = MeshaColors.Ink, style = MeshaType.rowValue, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        if (card.decidedByName.isNotBlank()) {
            Text(
                text = listOf(decidedByLabel, card.decidedByName).filter { it.isNotBlank() }.joinToString(" "),
                color = MeshaColors.Muted,
                style = MeshaType.rowCaption,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (card.decisionNote.isNotBlank()) {
            Text(text = card.decisionNote, color = MeshaColors.Ink, style = MeshaType.rowLabel)
        }
        if (card.previewUrl.isNotBlank()) {
            ProofMediaPreview(
                path = card.previewUrl,
                kind = if (card.previewIsPhoto) ProofMediaPreviewKind.Photo else ProofMediaPreviewKind.Video,
                mediaIdentity = card.previewIdentity,
                modifier = Modifier.fillMaxWidth(),
                inlineRemotePhoto = inlineRemotePhoto,
                onPreviewAction = onPreviewAction,
            )
        }
    }
}

/**
 * Record one animal on offer (L2 drill): the farm's Procurement SOP questionnaire, served by the
 * backend and rendered IN ORDER, one widget per question kind — a single choice, a checkbox list,
 * a text or number, or an in-app-camera capture block for a media slot. It is PAGED BY SECTION
 * exactly like the SOP form: page 1 is every question before the first section heading, then one
 * page per heading, with a step indicator, a Next that checks ONLY that page, a Back, and Save on
 * the last page only. A question whose `only_if` does not hold is absent, not greyed, and a page
 * left with no applicable question is skipped. Every word on screen is the served question text
 * or the backend `copy` map.
 *
 * The form never closes on enqueue: it follows the queued row and returns to the load once the
 * server accepted the animal (or the write is durably queued offline). A refused submit — the
 * phone's own check or the server's `422` — opens the page of the first failing question, scrolls
 * to it and shows the sentence on it.
 */
@Composable
fun AnimalPurchaseAnimalCreateScreen(
    state: AnimalPurchaseAnimalCreateUiState,
    onEvent: (AnimalPurchaseAnimalCreateEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LaunchedEffect(state.closeAfterSave) {
        if (state.closeAfterSave) {
            delay(ANIMAL_CLOSE_AFTER_SAVE_MS)
            onEvent(AnimalPurchaseAnimalCreateEvent.Back)
        }
    }
    val copy = state.copy
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    val listState = rememberLazyListState()
    // The rows ABOVE the first question: the result banner and the page hint (when there is one).
    val fixedRowsBeforeQuestions = 1 + if (state.pageHint.isNotBlank()) 1 else 0
    // A page change lands at the top; a refused Next/Save lands on the first failing question.
    LaunchedEffect(state.scrollRequest) {
        if (state.scrollRequest == 0) return@LaunchedEffect
        val index = state.questions.indexOfFirst { it.id == state.scrollToQuestionId }
        listState.animateScrollToItem(if (index >= 0) index + fixedRowsBeforeQuestions else 0)
    }
    val anyCaptureWorking = state.questions.any { it.captureWorking }
    val actionsEnabled = !locked && !state.submitInFlight && !anyCaptureWorking && state.questions.isNotEmpty()
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = copy[COPY_ANIMAL_FORM_TITLE].orEmpty(),
            subtitle = copy[COPY_REQUIRED_HINT]?.takeIf { it.isNotBlank() },
            onBack = { onEvent(AnimalPurchaseAnimalCreateEvent.Back) },
        )
        if (state.stepCount > 0) {
            VendorsStepper(
                stepCount = state.stepCount,
                currentIndex = state.stepIndex,
                caption = listOf(
                    "${copy[COPY_ANIMAL_STEP] ?: STEP} ${state.stepIndex + 1} ${copy[COPY_ANIMAL_STEP_OF] ?: OF} ${state.stepCount}",
                    state.pageTitle,
                ).filter { it.isNotBlank() }.joinToString(" · "),
            )
        }
        LazyColumn(
            state = listState,
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            if (state.pageHint.isNotBlank()) {
                item(key = "page_hint") { Text(text = state.pageHint, color = MeshaColors.Muted, style = MeshaType.caption) }
            }
            items(items = state.questions, key = { it.id }, contentType = { it.kind }) { question ->
                AnimalPurchaseQuestionItem(question = question, state = state, locked = locked, onEvent = onEvent)
            }
        }
        VendorsWizardBar(contextLine = "") {
            if (!state.isFirstPage) {
                VendorsGhostButton(
                    label = copy[COPY_ANIMAL_BACK] ?: PREVIOUS,
                    onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.PreviousPage) },
                    enabled = !locked && !state.submitInFlight && !anyCaptureWorking,
                    modifier = Modifier.weight(1f),
                )
            }
            if (state.isLastPage) {
                VendorsPrimaryButton(
                    label = if (state.submitInFlight) copy[COPY_ANIMAL_SAVING].orEmpty() else copy[COPY_ANIMAL_SAVE].orEmpty(),
                    enabled = actionsEnabled,
                    onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.Submit) },
                    modifier = Modifier.weight(1f),
                )
            } else {
                VendorsPrimaryButton(
                    label = copy[COPY_ANIMAL_NEXT] ?: NEXT,
                    enabled = actionsEnabled,
                    onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.NextPage) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

/** Wizard chrome, the same words every other multi-step form in this module uses. */
// Backend-owned chrome copy (animal.step / animal.step_of / animal.next / animal.back); these
// literals are the fallback only while an older API serves no such keys.
const val COPY_ANIMAL_STEP = "animal.step"
const val COPY_ANIMAL_STEP_OF = "animal.step_of"
const val COPY_ANIMAL_NEXT = "animal.next"
const val COPY_ANIMAL_BACK = "animal.back"
private const val NEXT = "Next"
private const val PREVIOUS = "Back"
private const val STEP = "Step"
private const val OF = "of"

/** Segmented control for a choice of this many short options or fewer; a radio list otherwise. */
private const val SEGMENTED_MAX_OPTIONS = 3
private const val SEGMENTED_MAX_LABEL_CHARS = 12

@Composable
private fun AnimalPurchaseQuestionItem(
    question: AnimalPurchaseQuestionUi,
    state: AnimalPurchaseAnimalCreateUiState,
    locked: Boolean,
    onEvent: (AnimalPurchaseAnimalCreateEvent) -> Unit,
) {
    val copy = state.copy
    when (question.kind) {
        AnimalPurchaseQuestionKind.SECTION -> Column(Modifier.fillMaxWidth().padding(top = 8.dp)) {
            Text(text = question.title, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
            if (question.hint.isNotBlank()) Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        AnimalPurchaseQuestionKind.CHOICE -> AnimalPurchaseQuestionGroup(question) {
            val selected = state.scalarAnswers[question.id].orEmpty()
            val segmented = question.options.size <= SEGMENTED_MAX_OPTIONS && question.options.all { it.label.length <= SEGMENTED_MAX_LABEL_CHARS }
            if (segmented) {
                VendorsSegmented(
                    options = question.options,
                    selectedValue = selected,
                    onSelect = { if (!locked) onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged(question.id, it)) },
                )
            } else {
                Column(Modifier.fillMaxWidth()) {
                    question.options.forEach { option ->
                        AnimalPurchaseOptionRow(
                            label = option.label,
                            selected = option.value == selected,
                            single = true,
                            enabled = !locked,
                            onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.ChoiceChanged(question.id, option.value)) },
                        )
                    }
                }
            }
            if (question.allowOther && selected == OTHER_OPTION_VALUE) {
                AnimalPurchaseOtherField(question, state, locked, onEvent)
            }
        }
        AnimalPurchaseQuestionKind.MULTI -> AnimalPurchaseQuestionGroup(question) {
            val ticked = state.multiAnswers[question.id].orEmpty()
            Column(Modifier.fillMaxWidth()) {
                question.options.forEach { option ->
                    val checked = option.value in ticked
                    AnimalPurchaseOptionRow(
                        label = option.label,
                        selected = checked,
                        single = false,
                        enabled = !locked,
                        onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.MultiToggled(question.id, option.value, !checked)) },
                    )
                }
            }
            if (question.allowOther && OTHER_OPTION_VALUE in ticked) {
                AnimalPurchaseOtherField(question, state, locked, onEvent)
            }
        }
        AnimalPurchaseQuestionKind.TEXT -> VendorsTextField(
            value = state.scalarAnswers[question.id].orEmpty(),
            onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged(question.id, it)) },
            label = question.title,
            required = question.required,
            readOnly = locked,
            singleLine = question.id != NOTES_QUESTION_ID,
            error = question.error,
            supporting = question.hint.takeIf { it.isNotBlank() },
        )
        AnimalPurchaseQuestionKind.NUMBER -> VendorsTextField(
            value = state.scalarAnswers[question.id].orEmpty(),
            onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged(question.id, it)) },
            label = question.title,
            required = question.required,
            keyboard = KeyboardType.Decimal,
            readOnly = locked,
            error = question.error,
            supporting = listOf(question.hint, question.rangeLine).filter { it.isNotBlank() }.joinToString(" · ").takeIf { it.isNotBlank() },
            trailing = question.unit.takeIf { it.isNotBlank() }?.let { unit -> { Text(text = unit, color = MeshaColors.Muted, style = MeshaType.rowValue) } },
        )
        AnimalPurchaseQuestionKind.MEDIA -> AnimalPurchaseQuestionGroup(question) {
            AnimalPurchaseCaptureBlock(question = question, copy = copy, locked = locked, onEvent = onEvent)
        }
        // The vendor picker belongs to the load form; an animal never carries one.
        AnimalPurchaseQuestionKind.VENDOR -> Unit
    }
}

/** A titled group: the question (starred when required), its hint, the content, then its error. */
@Composable
private fun AnimalPurchaseQuestionGroup(question: AnimalPurchaseQuestionUi, content: @Composable () -> Unit) {
    VendorsFormGroup(title = if (question.required) "${question.title} *" else question.title) {
        if (question.hint.isNotBlank()) Text(text = question.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        content()
        question.error?.let { Text(text = it, color = MeshaColors.Danger, style = MeshaType.caption) }
    }
}

/** One radio (single) or checkbox (multi) row, the whole row tappable. */
@Composable
internal fun AnimalPurchaseOptionRow(label: String, selected: Boolean, single: Boolean, enabled: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusSmall))
            .selectable(selected = selected, enabled = enabled, role = if (single) Role.RadioButton else Role.Checkbox, onClick = onClick)
            .padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        if (single) {
            RadioButton(selected = selected, onClick = null, enabled = enabled, colors = RadioButtonDefaults.colors(selectedColor = MeshaColors.Brand, unselectedColor = MeshaColors.Muted))
        } else {
            Checkbox(checked = selected, onCheckedChange = null, enabled = enabled, colors = CheckboxDefaults.colors(checkedColor = MeshaColors.Brand, uncheckedColor = MeshaColors.Muted, checkmarkColor = MeshaColors.OnBrand))
        }
        Text(text = label, color = MeshaColors.Ink, style = MeshaType.body, modifier = Modifier.weight(1f))
    }
}

/** The free text a chosen "other" option carries, sent as `<id>_other`. */
@Composable
private fun AnimalPurchaseOtherField(
    question: AnimalPurchaseQuestionUi,
    state: AnimalPurchaseAnimalCreateUiState,
    locked: Boolean,
    onEvent: (AnimalPurchaseAnimalCreateEvent) -> Unit,
) {
    val otherId = question.id + OTHER_SUFFIX
    VendorsTextField(
        value = state.scalarAnswers[otherId].orEmpty(),
        onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.TextChanged(otherId, it)) },
        label = state.copy[COPY_ANIMAL_OTHER_HINT].orEmpty(),
        readOnly = locked,
    )
}

/**
 * The capture block of one media slot: every capture taken so far (photo thumbnail or video
 * preview, a remove, a retry when its upload gave up), then the "Take photo" / "Record video"
 * buttons the slot accepts while it is under its cap.
 */
@Composable
private fun AnimalPurchaseCaptureBlock(
    question: AnimalPurchaseQuestionUi,
    copy: Map<String, String>,
    locked: Boolean,
    onEvent: (AnimalPurchaseAnimalCreateEvent) -> Unit,
) {
    question.captures.forEach { capture ->
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            ProofMediaPreview(
                path = capture.localUri,
                kind = if (capture.isVideo) ProofMediaPreviewKind.Video else ProofMediaPreviewKind.Photo,
                mediaIdentity = capture.proofId,
                modifier = Modifier.fillMaxWidth(),
                onPreviewAction = { onEvent(AnimalPurchaseAnimalCreateEvent.PreviewAction(it)) },
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                if (capture.uploadFailed) {
                    // The capture is still on the phone; only its upload gave up.
                    VendorsPrimaryButton(
                        label = copy[COPY_ANIMAL_SEND_FAILED].orEmpty(),
                        onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.RetryCapture(question.id, capture.proofId)) },
                        enabled = !locked,
                        modifier = Modifier.weight(1f),
                    )
                }
                VendorsGhostButton(
                    label = copy[COPY_ANIMAL_MEDIA_REMOVE].orEmpty(),
                    onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.RemoveCapture(question.id, capture.proofId)) },
                    enabled = !locked && !question.captureWorking,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
    val underCap = question.maxFiles <= 0 || question.captures.size < question.maxFiles
    if (underCap) {
        val addMore = question.captures.isNotEmpty()
        if (addMore) Text(text = copy[COPY_ANIMAL_MEDIA_ADD_MORE].orEmpty(), color = MeshaColors.Muted, style = MeshaType.caption)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.fillMaxWidth()) {
            if (question.acceptsPhoto) {
                VendorsGhostButton(
                    label = copy[COPY_ANIMAL_MEDIA_PHOTO].orEmpty(),
                    onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.TakePhoto(question.id)) },
                    enabled = !locked && !question.captureWorking,
                    modifier = Modifier.weight(1f),
                )
            }
            if (question.acceptsVideo) {
                VendorsGhostButton(
                    label = copy[COPY_ANIMAL_MEDIA_VIDEO].orEmpty(),
                    onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.RecordVideo(question.id)) },
                    enabled = !locked && !question.captureWorking,
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

/** The questionnaire's "other" option value and the answer key suffix its free text rides under
 *  (the wire contract's `<id>_other`; core-data's `AnimalPurchaseAnswers.OTHER_SUFFIX` is the same
 *  string, restated here because this module cannot see core-data). */
const val OTHER_OPTION_VALUE = "other"
const val OTHER_SUFFIX = "_other"

/** The SOP's free-text note question, the one multi-line field on the form. */
private const val NOTES_QUESTION_ID = "notes"

private const val ANIMAL_CLOSE_AFTER_SAVE_MS = 900L

/** A saved-but-not-sent animal: the typed facts and a waiting chip, no decision and no video
 *  playback (the video is still on this phone). Disappears when the server row lands. */
@Composable
private fun AnimalPurchaseQueuedAnimalCard(card: AnimalPurchaseQueuedAnimalUi, onRetry: () -> Unit) {
    VendorsCard(onClick = if (card.sendFailed) onRetry else null) {
        Row(verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
            Column(Modifier.weight(1f)) {
                Text(text = card.title, color = MeshaColors.Ink, style = MeshaType.listTitle)
                val sub = listOf(card.breed, card.ageWeightLine).filter { it.isNotBlank() }.joinToString(" · ")
                if (sub.isNotBlank()) Text(text = sub, color = MeshaColors.Muted, style = MeshaType.rowCaption)
            }
            if (card.sendFailed) VendorsChip(label = card.failedLabel, tone = VendorsTone.DANGER)
            else VendorsChip(label = card.waitingLabel, tone = VendorsTone.WARN)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            if (card.conditionLabel.isNotBlank()) VendorsChip(label = card.conditionLabel, tone = VendorsTone.INFO)
            if (card.tempTag.isNotBlank()) Text(text = card.tempTag, color = MeshaColors.Ink, style = MeshaType.rowLabel)
        }
    }
}
