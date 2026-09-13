package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderers; AnimalPurchaseLoadsViewModel and
// AnimalPurchaseLoadCreateViewModel (in :app) own the animal_purchase_* AnalyticsEventsAnimalPurchase
// + CrashReporter wiring for every read refresh, row open and queued write.

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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
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
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = copy[COPY_LOAD_FORM_TITLE].orEmpty(), onBack = { onEvent(AnimalPurchaseLoadCreateEvent.Back) })
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            item(key = "load_ref") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseLoadField.LOAD_REF].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, it)) },
                    label = copy[COPY_LOAD_FIELD_LOAD_REF].orEmpty(),
                    required = true,
                    readOnly = locked,
                    error = requiredHint.takeIf { AnimalPurchaseLoadField.LOAD_REF in state.fieldErrors },
                )
            }
            item(key = "vendor") {
                VendorsDropdownField(
                    label = copy[COPY_LOAD_FIELD_VENDOR].orEmpty(),
                    selectedValue = state.values[AnimalPurchaseLoadField.VENDOR].orEmpty(),
                    options = state.vendors,
                    onSelect = { if (!locked) onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.VENDOR, it)) },
                    required = true,
                    error = requiredHint.takeIf { AnimalPurchaseLoadField.VENDOR in state.fieldErrors },
                )
            }
            item(key = "farm") {
                VendorsFormGroup(title = copy[COPY_LOAD_FIELD_FARM].orEmpty()) {
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
            item(key = "expected") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseLoadField.EXPECTED_COUNT].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.EXPECTED_COUNT, it)) },
                    label = copy[COPY_LOAD_FIELD_EXPECTED].orEmpty(),
                    keyboard = KeyboardType.Number,
                    readOnly = locked,
                    error = requiredHint.takeIf { AnimalPurchaseLoadField.EXPECTED_COUNT in state.fieldErrors },
                )
            }
            item(key = "notes") {
                VendorsTextField(
                    value = state.values[AnimalPurchaseLoadField.NOTES].orEmpty(),
                    onValueChange = { onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.NOTES, it)) },
                    label = copy[COPY_LOAD_FIELD_NOTES].orEmpty(),
                    singleLine = false,
                    readOnly = locked,
                )
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
const val COPY_ANIMAL_FIELD_SPECIES = "animal.field.species"
const val COPY_ANIMAL_FIELD_SEX = "animal.field.sex"
const val COPY_ANIMAL_FIELD_BREED = "animal.field.breed"
const val COPY_ANIMAL_FIELD_AGE = "animal.field.age"
const val COPY_ANIMAL_FIELD_WEIGHT = "animal.field.weight"
const val COPY_ANIMAL_FIELD_CONDITION = "animal.field.condition"
const val COPY_ANIMAL_FIELD_TEMP_TAG = "animal.field.temp_tag"
const val COPY_ANIMAL_FIELD_NOTES = "animal.field.notes"
const val COPY_ANIMAL_FIELD_VIDEO = "animal.field.video"
const val COPY_ANIMAL_VIDEO_RECORD = "animal.video.record"
const val COPY_ANIMAL_VIDEO_RETAKE = "animal.video.retake"
const val COPY_ANIMAL_VIDEO_HINT = "animal.video.hint"
const val COPY_ANIMAL_SAVE = "animal.save"
const val COPY_ANIMAL_SAVING = "animal.saving"
const val COPY_ANIMAL_DECISION_PENDING = "animal.decision.pending"
const val COPY_ANIMAL_DECIDED_BY = "animal.decided_by"
