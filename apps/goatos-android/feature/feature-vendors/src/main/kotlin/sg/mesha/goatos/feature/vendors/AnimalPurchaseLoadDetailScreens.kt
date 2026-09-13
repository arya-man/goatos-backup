package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderers; AnimalPurchaseLoadDetailViewModel and
// AnimalPurchaseAnimalCreateViewModel (in :app) own the animal_purchase_* AnalyticsEventsAnimalPurchase
// + CrashReporter wiring for every read refresh, capture and queued write.

import androidx.compose.foundation.background
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
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.PopupProperties
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
                            onPreviewAction = { onEvent(AnimalPurchaseLoadDetailEvent.PreviewAction(it)) },
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
    onPreviewAction: (String) -> Unit,
) {
    VendorsCard(onClick = null) {
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
            // The SERVER's decision chip, verbatim, in its tone.
            VendorsChip(label = card.decisionLabel, tone = card.decisionTone)
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
        if (card.mediaUrl.isNotBlank()) {
            ProofMediaPreview(
                path = card.mediaUrl,
                kind = ProofMediaPreviewKind.Video,
                mediaIdentity = card.candidateId,
                modifier = Modifier.fillMaxWidth(),
                onPreviewAction = onPreviewAction,
            )
        }
    }
}

/**
 * Record one animal on offer (L2 drill): goat/sheep, sex, breed (free text with the herd's own
 * spellings suggested), rough age and weight, how it looks, an optional temporary tag, a note, and
 * the mandatory in-app-camera video. Every label comes from the backend `copy` map.
 *
 * The form never closes on enqueue: it follows the queued row and returns to the load once the
 * server accepted the animal (or the write is durably queued offline), and shows the server's own
 * sentence when the write was refused.
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
    val requiredHint = copy[COPY_REQUIRED_HINT].orEmpty()
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = copy[COPY_ANIMAL_FORM_TITLE].orEmpty(), onBack = { onEvent(AnimalPurchaseAnimalCreateEvent.Back) })
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            item(key = "species") {
                VendorsFormGroup(title = copy[COPY_ANIMAL_FIELD_SPECIES].orEmpty()) {
                    VendorsSegmented(
                        options = state.species,
                        selectedValue = state.values[AnimalPurchaseAnimalField.SPECIES].orEmpty(),
                        onSelect = { if (!locked) onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.SPECIES, it)) },
                    )
                    if (AnimalPurchaseAnimalField.SPECIES in state.fieldErrors) Text(text = requiredHint, color = MeshaColors.Danger, style = MeshaType.caption)
                }
            }
            item(key = "sex") {
                VendorsFormGroup(title = copy[COPY_ANIMAL_FIELD_SEX].orEmpty()) {
                    VendorsSegmented(
                        options = state.sexes,
                        selectedValue = state.values[AnimalPurchaseAnimalField.SEX].orEmpty(),
                        onSelect = { if (!locked) onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.SEX, it)) },
                    )
                    if (AnimalPurchaseAnimalField.SEX in state.fieldErrors) Text(text = requiredHint, color = MeshaColors.Danger, style = MeshaType.caption)
                }
            }
            item(key = "breed") {
                AnimalPurchaseBreedField(
                    value = state.values[AnimalPurchaseAnimalField.BREED].orEmpty(),
                    label = copy[COPY_ANIMAL_FIELD_BREED].orEmpty(),
                    suggestions = state.breedSuggestions,
                    readOnly = locked,
                    onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.BREED, it)) },
                )
            }
            item(key = "age") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseAnimalField.AGE_MONTHS].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.AGE_MONTHS, it)) },
                    label = copy[COPY_ANIMAL_FIELD_AGE].orEmpty(),
                    keyboard = KeyboardType.Number,
                    readOnly = locked,
                    error = requiredHint.takeIf { AnimalPurchaseAnimalField.AGE_MONTHS in state.fieldErrors },
                )
            }
            item(key = "weight") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseAnimalField.WEIGHT_KG].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.WEIGHT_KG, it)) },
                    label = copy[COPY_ANIMAL_FIELD_WEIGHT].orEmpty(),
                    keyboard = KeyboardType.Decimal,
                    readOnly = locked,
                    error = requiredHint.takeIf { AnimalPurchaseAnimalField.WEIGHT_KG in state.fieldErrors },
                )
            }
            item(key = "condition") {
                VendorsFormGroup(title = copy[COPY_ANIMAL_FIELD_CONDITION].orEmpty()) {
                    VendorsSegmented(
                        options = state.conditions,
                        selectedValue = state.values[AnimalPurchaseAnimalField.CONDITION].orEmpty(),
                        onSelect = { if (!locked) onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.CONDITION, it)) },
                    )
                    if (AnimalPurchaseAnimalField.CONDITION in state.fieldErrors) Text(text = requiredHint, color = MeshaColors.Danger, style = MeshaType.caption)
                }
            }
            item(key = "temp_tag") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseAnimalField.TEMP_TAG].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.TEMP_TAG, it)) },
                    label = copy[COPY_ANIMAL_FIELD_TEMP_TAG].orEmpty(),
                    readOnly = locked,
                )
            }
            item(key = "notes") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseAnimalField.NOTES].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.NOTES, it)) },
                    label = copy[COPY_ANIMAL_FIELD_NOTES].orEmpty(),
                    singleLine = false,
                    readOnly = locked,
                )
            }
            item(key = "video") {
                VendorsFormGroup(title = copy[COPY_ANIMAL_FIELD_VIDEO].orEmpty()) {
                    Text(text = copy[COPY_ANIMAL_VIDEO_HINT].orEmpty(), color = MeshaColors.Muted, style = MeshaType.caption)
                    if (state.videoLocalUri.isNotBlank() && state.videoIdentity.isNotBlank()) {
                        ProofMediaPreview(
                            path = state.videoLocalUri,
                            kind = ProofMediaPreviewKind.Video,
                            mediaIdentity = state.videoIdentity,
                            modifier = Modifier.fillMaxWidth(),
                            onPreviewAction = { onEvent(AnimalPurchaseAnimalCreateEvent.VideoPreviewAction(it)) },
                        )
                    }
                    if (state.videoMissing) Text(text = requiredHint, color = MeshaColors.Danger, style = MeshaType.caption)
                    if (state.videoStatus == AnimalPurchaseVideoStatus.FAILED && state.videoLocalUri.isNotBlank()) {
                        // The recording is still on the phone; only its upload gave up.
                        VendorsPrimaryButton(
                            label = copy[COPY_ANIMAL_SEND_FAILED].orEmpty(),
                            onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.RetryVideoUpload) },
                            enabled = !locked,
                            modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    VendorsGhostButton(
                        label = if (state.videoStatus == AnimalPurchaseVideoStatus.RECORDED || state.videoStatus == AnimalPurchaseVideoStatus.FAILED) copy[COPY_ANIMAL_VIDEO_RETAKE].orEmpty() else copy[COPY_ANIMAL_VIDEO_RECORD].orEmpty(),
                        onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.RecordVideo) },
                        enabled = !locked && state.videoStatus != AnimalPurchaseVideoStatus.WORKING,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
            item(key = "hint") { Text(text = requiredHint, color = MeshaColors.Muted, style = MeshaType.caption) }
        }
        Row(modifier = Modifier.fillMaxWidth().padding(MeshaDimens.gutter)) {
            VendorsPrimaryButton(
                label = if (state.submitInFlight) copy[COPY_ANIMAL_SAVING].orEmpty() else copy[COPY_ANIMAL_SAVE].orEmpty(),
                enabled = !locked && !state.submitInFlight && state.videoStatus != AnimalPurchaseVideoStatus.WORKING,
                onClick = { onEvent(AnimalPurchaseAnimalCreateEvent.Submit) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

/**
 * Free-text breed with the herd's own spellings suggested underneath as the person types. The
 * menu is bounded to the first few prefix matches and never takes focus, so typing continues.
 */
@Composable
private fun AnimalPurchaseBreedField(
    value: String,
    label: String,
    suggestions: List<String>,
    readOnly: Boolean,
    onValueChange: (String) -> Unit,
) {
    var dismissedFor by remember { mutableStateOf("") }
    val matches = remember(value, suggestions) {
        val needle = value.trim()
        if (needle.isBlank()) emptyList()
        else suggestions.filter { it.startsWith(needle, ignoreCase = true) && !it.equals(needle, ignoreCase = true) }.take(BREED_SUGGESTION_LIMIT)
    }
    Box(Modifier.fillMaxWidth()) {
        VendorsTextField(
            value = value,
            onValueChange = { onValueChange(it); dismissedFor = "" },
            label = label,
            readOnly = readOnly,
        )
        DropdownMenu(
            expanded = !readOnly && matches.isNotEmpty() && dismissedFor != value,
            onDismissRequest = { dismissedFor = value },
            properties = PopupProperties(focusable = false),
            modifier = Modifier.heightIn(max = 240.dp).background(MeshaColors.Surf),
        ) {
            matches.forEach { suggestion ->
                DropdownMenuItem(
                    text = { Text(suggestion, color = MeshaColors.Ink, style = MeshaType.body) },
                    onClick = { onValueChange(suggestion); dismissedFor = suggestion },
                )
            }
        }
    }
}

private const val BREED_SUGGESTION_LIMIT = 6
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

