package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderers; the @HiltViewModels in :app own the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read refresh and queued write.

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
import androidx.compose.foundation.clickable
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.size
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
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton
import sg.mesha.goatos.core.ui.SyncStatusIndicator

/**
 * Pipeline and evidence (maintainer instruction 2026-09-04): the five panels the web's
 * /sales/config carries beside the deals ledger. On the phone they are an L1 drill from the Sales
 * tab — the web opens them as drawers on the same page, which is the same relationship.
 */
@Composable
fun SalesPipelineHubScreen(
    state: SalesPipelineHubUiState,
    onEvent: (SalesPipelineHubEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(SalesPipelineHubEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = HUB_TITLE,
            subtitle = HUB_SUBTITLE,
            onBack = { onEvent(SalesPipelineHubEvent.Back) },
            below = { SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = state.entries.isNotEmpty()) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(SalesPipelineHubEvent.Refresh) }) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.disabledReason.isNotBlank()) {
                item(key = "disabled") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = state.disabledReason) }
            }
            items(count = state.entries.size, key = { state.entries[it].panel.name }) { index ->
                val entry = state.entries[index]
                VendorsCard(onClick = { onEvent(SalesPipelineHubEvent.Open(entry.panel)) }) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        VendorsIconTile(icon = MeshaIcons.Sale, tint = MeshaColors.Teal, background = MeshaColors.TealX)
                        Column(Modifier.weight(1f)) {
                            Text(text = entry.title, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Spacer(Modifier.height(2.dp))
                            Text(text = entry.subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 2, overflow = TextOverflow.Ellipsis)
                        }
                        if (entry.countLine.isNotBlank()) {
                            VendorsChip(label = entry.countLine, tone = VendorsTone.INFO)
                        }
                    }
                }
            }
        }
    }
}

/**
 * One board of leads — buyers, or farmer groups. ONE screen serves both: they differ in their form
 * fields and their subtitle, never in their shape, and a second near-copy would drift.
 */
@Composable
fun SalesLeadBoardScreen(
    state: SalesLeadBoardUiState,
    panel: SalesPipelinePanel,
    rows: LazyPagingItems<SalesLeadCardUi>,
    onEvent: (SalesLeadBoardEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(SalesLeadBoardEvent.Refresh) }
    Box(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        Column(Modifier.fillMaxSize()) {
            MeshaScreenHeader(
                title = state.title,
                subtitle = state.countLine.ifBlank { null },
                onBack = { onEvent(SalesLeadBoardEvent.Back) },
                below = { SyncStatusIndicator(isRefreshing = state.isRefreshing, lastSyncedAt = state.lastSyncedAt, hasData = rows.itemCount > 0) },
                actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(SalesLeadBoardEvent.Refresh) }) },
            )
            val form = state.form
            if (form == null) {
                // Search and the status chips sit ABOVE the list, outside it, so they stay put
                // while the board scrolls -- a board of two hundred leads is unusable if the way
                // to narrow it scrolls away with the first flick.
                VendorsSearchField(
                    value = state.search,
                    placeholder = state.searchPlaceholder,
                    onValueChange = { onEvent(SalesLeadBoardEvent.SearchChanged(it)) },
                )
                if (state.filters.isNotEmpty()) {
                    VendorsFilterRow(filters = state.filters, onSelect = { onEvent(SalesLeadBoardEvent.SelectStatus(it)) })
                }
            }
            // The form REPLACES the board while it is open rather than riding as its first row: on
            // a board of 200 leads a row-shaped form is scrolled away the moment a finger moves,
            // and its Save button sits below a screenful of other people's records.
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                contentPadding = PaddingValues(start = MeshaDimens.gutter, end = MeshaDimens.gutter, top = 8.dp, bottom = 96.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (state.writeMessage.isNotBlank()) {
                    item(key = "write") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
                }
                if (form != null) {
                    item(key = "form") { LeadForm(state, panel, form, onEvent) }
                    return@LazyColumn
                }
                if (rows.itemCount == 0 && state.emptyMessage.isNotBlank()) {
                    item(key = "empty") {
                        EmptyState(title = state.emptyMessage, modifier = Modifier.fillMaxWidth(), icon = MeshaIcons.Sale)
                    }
                }
                // The key carries the BOARD as well as the lead id, so a row can never be reused
                // across the two boards this screen serves.
                items(count = rows.itemCount, key = rows.itemKey { it.listKey }) { index ->
                    rows[index]?.let { card ->
                        LeadCard(card = card, state = state, onEvent = onEvent)
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
        if (state.canRecord && state.form == null) {
            VendorsAddButton(
                label = if (panel == SalesPipelinePanel.BUYER_LEADS) ADD_BUYER_LEAD else ADD_FARMER_GROUP,
                onClick = { onEvent(SalesLeadBoardEvent.OpenForm) },
                modifier = Modifier.align(Alignment.BottomEnd).padding(MeshaDimens.gutter),
            )
        }
    }
}

/**
 * One lead. Tapping it opens it OUT IN PLACE (maintainer choice 2026-09-05) rather than navigating
 * to a screen of its own: someone working down a call list wants the row above and the row below
 * still in front of them.
 *
 * The number is the point of the whole board, so it sits at the top of what opens and is tappable
 * straight into the dialler. A lead with none says so rather than showing an empty line -- every
 * imported lead starts without one.
 */
@Composable
private fun LeadCard(
    card: SalesLeadCardUi,
    state: SalesLeadBoardUiState,
    onEvent: (SalesLeadBoardEvent) -> Unit,
) {
    val context = LocalContext.current
    val expanded = state.expandedLeadId == card.leadId
    VendorsCard(onClick = { onEvent(SalesLeadBoardEvent.ToggleExpanded(card.leadId)) }) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Column(Modifier.weight(1f)) {
                Text(text = card.title, color = MeshaColors.Ink, style = MeshaType.listTitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (card.subtitle.isNotBlank()) {
                    Spacer(Modifier.height(2.dp))
                    Text(text = card.subtitle, color = MeshaColors.Muted, style = MeshaType.cardSubtitle, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
            // The status chip itself opens the picker, so working down a call list stays ONE tap
            // per lead: the row does not have to be opened out to move the conversation on.
            if (card.statusLabel.isNotBlank()) {
                VendorsChip(
                    label = card.statusLabel,
                    tone = card.statusTone,
                    modifier = Modifier.clickable { onEvent(SalesLeadBoardEvent.OpenStatusPicker(card.leadId)) },
                )
            }
        }
        if (card.metaLine.isNotBlank()) {
            Text(text = card.metaLine, color = MeshaColors.Muted, style = MeshaType.rowCaption, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (expanded) {
            Spacer(Modifier.height(8.dp))
            if (card.phoneNumber.isNotBlank()) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(MeshaDimens.radiusCard))
                        .background(MeshaColors.TealX)
                        .clickable {
                            onEvent(SalesLeadBoardEvent.CallLead(card.leadId))
                            dialNumber(context, card.phoneNumber)
                        }
                        .padding(horizontal = 12.dp, vertical = 10.dp),
                ) {
                    Icon(MeshaIcons.Phone, contentDescription = null, tint = MeshaColors.Teal, modifier = Modifier.size(MeshaDimens.iconMd))
                    Text(text = card.phoneNumber, color = MeshaColors.Ink, style = MeshaType.listTitle)
                }
            } else {
                Text(text = state.noPhoneMessage, color = MeshaColors.Muted, style = MeshaType.rowCaption)
            }
            card.details.forEach { row -> VendorsDetailRow(row) }
            Spacer(Modifier.height(4.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                VendorsGhostButton(
                    label = CHANGE_STATUS,
                    onClick = { onEvent(SalesLeadBoardEvent.OpenStatusPicker(card.leadId)) },
                    modifier = Modifier.weight(1f),
                )
                VendorsGhostButton(
                    label = EDIT_LEAD,
                    onClick = { onEvent(SalesLeadBoardEvent.EditLead(card)) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
        // The status picker opens in place on the tapped card, so the board never navigates away
        // from the row the operator is looking at. It stays reachable WITHOUT opening the lead out
        // first, because working down a list of calls is one tap per row.
        if (state.statusPickerLeadId == card.leadId && state.statusOptions.isNotEmpty()) {
            VendorsDropdownField(
                label = LABEL_CALL_STATUS,
                selectedValue = card.statusLabel,
                options = state.statusOptions,
                onSelect = { onEvent(SalesLeadBoardEvent.ChangeStatus(card.leadId, it)) },
            )
        }
    }
}

/** Hands a recorded number to the phone's dialler, pre-filled and not yet dialled. */
private fun dialNumber(context: android.content.Context, phoneNumber: String) {
    val intent = android.content.Intent(
        android.content.Intent.ACTION_DIAL,
        android.net.Uri.parse("tel:" + phoneNumber.trim()),
    ).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
    // exception:exempt a device with no dialler cannot be helped from here, and the board must not
    // fall over because one tap found nothing to open.
    runCatching { context.startActivity(intent) }
}

@Composable
private fun LeadForm(
    state: SalesLeadBoardUiState,
    panel: SalesPipelinePanel,
    form: SalesLeadFormUi,
    onEvent: (SalesLeadBoardEvent) -> Unit,
) {
    fun value(field: String) = form.values[field].orEmpty()
    fun error(field: String) = form.fieldErrors[field]
    fun changed(field: String) = { v: String -> onEvent(SalesLeadBoardEvent.FieldChanged(field, v)) }
    val editing = form.editingLeadId.isNotBlank()
    val title = when {
        editing && panel == SalesPipelinePanel.BUYER_LEADS -> EDIT_BUYER_LEAD
        editing -> EDIT_FARMER_GROUP
        panel == SalesPipelinePanel.BUYER_LEADS -> ADD_BUYER_LEAD
        else -> ADD_FARMER_GROUP
    }
    VendorsFormGroup(title = title) {
        if (panel == SalesPipelinePanel.BUYER_LEADS) {
            VendorsTextField(value(SalesBuyerLeadField.BUYER_NAME.name), changed(SalesBuyerLeadField.BUYER_NAME.name), LABEL_BUYER_NAME, required = true, error = error(SalesBuyerLeadField.BUYER_NAME.name))
            VendorsTextField(value(SalesBuyerLeadField.BUYER_PLACE.name), changed(SalesBuyerLeadField.BUYER_PLACE.name), LABEL_PLACE, error = error(SalesBuyerLeadField.BUYER_PLACE.name))
            VendorsDropdownField(LABEL_FARM, value(SalesBuyerLeadField.FARM.name), state.farms, changed(SalesBuyerLeadField.FARM.name), error = error(SalesBuyerLeadField.FARM.name), placeholder = HINT_PICK)
            VendorsDropdownField(LABEL_ANIMAL_TYPE, value(SalesBuyerLeadField.ANIMAL_TYPE.name), state.animalTypes, changed(SalesBuyerLeadField.ANIMAL_TYPE.name), error = error(SalesBuyerLeadField.ANIMAL_TYPE.name), placeholder = HINT_PICK)
            VendorsDropdownField(LABEL_BREED, value(SalesBuyerLeadField.BREED.name), state.breeds, changed(SalesBuyerLeadField.BREED.name), error = error(SalesBuyerLeadField.BREED.name), placeholder = HINT_PICK)
            VendorsTextField(value(SalesBuyerLeadField.PHONE_NUMBER.name), changed(SalesBuyerLeadField.PHONE_NUMBER.name), LABEL_PHONE, keyboard = KeyboardType.Phone, error = error(SalesBuyerLeadField.PHONE_NUMBER.name))
            VendorsDateField(LABEL_RECORDED_ON, value(SalesBuyerLeadField.RECORDED_DATE.name), changed(SalesBuyerLeadField.RECORDED_DATE.name), error = error(SalesBuyerLeadField.RECORDED_DATE.name), maxIso = state.today)
            VendorsDropdownField(LABEL_CALL_STATUS, value(SalesBuyerLeadField.CALL_STATUS.name), state.statusOptions, changed(SalesBuyerLeadField.CALL_STATUS.name), error = error(SalesBuyerLeadField.CALL_STATUS.name), placeholder = HINT_PICK)
        } else {
            VendorsTextField(value(SalesFpoLeadField.FPO_NAME.name), changed(SalesFpoLeadField.FPO_NAME.name), LABEL_FPO_NAME, required = true, error = error(SalesFpoLeadField.FPO_NAME.name))
            VendorsTextField(value(SalesFpoLeadField.CROPS.name), changed(SalesFpoLeadField.CROPS.name), LABEL_CROPS, error = error(SalesFpoLeadField.CROPS.name))
            VendorsTextField(value(SalesFpoLeadField.DISTRICT.name), changed(SalesFpoLeadField.DISTRICT.name), LABEL_DISTRICT, error = error(SalesFpoLeadField.DISTRICT.name))
            VendorsTextField(value(SalesFpoLeadField.TALUK.name), changed(SalesFpoLeadField.TALUK.name), LABEL_TALUK, error = error(SalesFpoLeadField.TALUK.name))
            VendorsTextField(value(SalesFpoLeadField.STATE.name), changed(SalesFpoLeadField.STATE.name), LABEL_STATE, error = error(SalesFpoLeadField.STATE.name))
            VendorsTextField(value(SalesFpoLeadField.PHONE_NUMBER.name), changed(SalesFpoLeadField.PHONE_NUMBER.name), LABEL_PHONE, keyboard = KeyboardType.Phone, error = error(SalesFpoLeadField.PHONE_NUMBER.name))
            VendorsDropdownField(LABEL_CALL_STATUS, value(SalesFpoLeadField.CALL_STATUS.name), state.statusOptions, changed(SalesFpoLeadField.CALL_STATUS.name), error = error(SalesFpoLeadField.CALL_STATUS.name), placeholder = HINT_PICK)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            VendorsGhostButton(label = CANCEL, onClick = { onEvent(SalesLeadBoardEvent.CloseForm) }, enabled = !form.inFlight)
            VendorsPrimaryButton(label = SAVE, enabled = !form.inFlight, onClick = { onEvent(SalesLeadBoardEvent.Submit) }, modifier = Modifier.weight(1f))
        }
    }
}

/**
 * The three evidence forms — a market quote, a sold-tag list, a weight check. One screen, because
 * they share every part except which fields they show.
 */
@Composable
fun SalesEvidenceScreen(
    state: SalesEvidenceUiState,
    panel: SalesPipelinePanel,
    onEvent: (SalesEvidenceEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    LaunchedEffect(state.closeAfterSave) {
        if (state.closeAfterSave) {
            delay(CLOSE_AFTER_SAVE_MS)
            onEvent(SalesEvidenceEvent.Back)
        }
    }
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = state.title, subtitle = state.subtitle.ifBlank { null }, onBack = { onEvent(SalesEvidenceEvent.Back) })
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            if (state.writeMessage.isNotBlank()) {
                item(key = "write") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            }
            if (!locked) {
                item(key = "form") {
                    when (panel) {
                        SalesPipelinePanel.MARKET_QUOTE -> QuoteForm(state, onEvent)
                        SalesPipelinePanel.WEIGHT_CHECK -> WeightCheckForm(state, onEvent)
                        else -> SoldTagsForm(state, onEvent)
                    }
                }
            }
        }
        VendorsWizardBar(contextLine = "") {
            if (locked) {
                val saved = state.writeStatus == VendorsWriteStatus.SYNCED
                VendorsPrimaryButton(label = if (saved) SAVED else SAVING, enabled = saved, onClick = {}, modifier = Modifier.weight(1f))
            } else {
                VendorsPrimaryButton(
                    label = SAVE,
                    enabled = state.canRecord && !state.submitInFlight,
                    onClick = { onEvent(SalesEvidenceEvent.Submit) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun QuoteForm(state: SalesEvidenceUiState, onEvent: (SalesEvidenceEvent) -> Unit) {
    fun v(f: SalesQuoteField) = state.values[f.name].orEmpty()
    fun e(f: SalesQuoteField) = state.fieldErrors[f.name]
    fun c(f: SalesQuoteField) = { value: String -> onEvent(SalesEvidenceEvent.FieldChanged(f.name, value)) }
    VendorsFormGroup(title = QUOTE_GROUP) {
        // BREED is the field the server requires here, not the market name -- a quote is filed
        // against the animal it prices.
        VendorsTextField(v(SalesQuoteField.MARKET), c(SalesQuoteField.MARKET), LABEL_MARKET, error = e(SalesQuoteField.MARKET))
        VendorsDropdownField(LABEL_CATEGORY, v(SalesQuoteField.CATEGORY), state.animalTypes, c(SalesQuoteField.CATEGORY), error = e(SalesQuoteField.CATEGORY), placeholder = HINT_PICK)
        VendorsDropdownField(LABEL_BREED, v(SalesQuoteField.BREED), state.breeds, c(SalesQuoteField.BREED), required = true, error = e(SalesQuoteField.BREED), placeholder = HINT_PICK)
        VendorsTextField(v(SalesQuoteField.SOURCE), c(SalesQuoteField.SOURCE), LABEL_SOURCE, error = e(SalesQuoteField.SOURCE), supporting = SOURCE_HINT)
        VendorsTextField(v(SalesQuoteField.EX_FARM_RATE), c(SalesQuoteField.EX_FARM_RATE), LABEL_EX_FARM, error = e(SalesQuoteField.EX_FARM_RATE))
        VendorsTextField(v(SalesQuoteField.TRANSPORT_RATE), c(SalesQuoteField.TRANSPORT_RATE), LABEL_TRANSPORT, error = e(SalesQuoteField.TRANSPORT_RATE))
        VendorsTextField(v(SalesQuoteField.LANDING_COST_PER_KG), c(SalesQuoteField.LANDING_COST_PER_KG), LABEL_LANDING, keyboard = KeyboardType.Decimal, error = e(SalesQuoteField.LANDING_COST_PER_KG))
        VendorsTextField(v(SalesQuoteField.MARKET_PRICE_PER_KG), c(SalesQuoteField.MARKET_PRICE_PER_KG), LABEL_MARKET_PRICE, keyboard = KeyboardType.Decimal, error = e(SalesQuoteField.MARKET_PRICE_PER_KG))
    }
}

@Composable
private fun WeightCheckForm(state: SalesEvidenceUiState, onEvent: (SalesEvidenceEvent) -> Unit) {
    fun v(f: SalesWeightCheckField) = state.values[f.name].orEmpty()
    fun e(f: SalesWeightCheckField) = state.fieldErrors[f.name]
    fun c(f: SalesWeightCheckField) = { value: String -> onEvent(SalesEvidenceEvent.FieldChanged(f.name, value)) }
    VendorsFormGroup(title = WEIGHT_GROUP) {
        VendorsTextField(v(SalesWeightCheckField.TAG_NUMBER), c(SalesWeightCheckField.TAG_NUMBER), LABEL_TAG, error = e(SalesWeightCheckField.TAG_NUMBER))
        VendorsTextField(v(SalesWeightCheckField.BOOK_WEIGHT_KG), c(SalesWeightCheckField.BOOK_WEIGHT_KG), LABEL_BOOK_WEIGHT, required = true, keyboard = KeyboardType.Decimal, error = e(SalesWeightCheckField.BOOK_WEIGHT_KG))
        VendorsTextField(v(SalesWeightCheckField.VIDEO_WEIGHT_KG), c(SalesWeightCheckField.VIDEO_WEIGHT_KG), LABEL_VIDEO_WEIGHT, required = true, keyboard = KeyboardType.Decimal, error = e(SalesWeightCheckField.VIDEO_WEIGHT_KG))
        Text(text = LABEL_FARM_BORN, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = FARM_BORN_OPTIONS, selectedValue = v(SalesWeightCheckField.FARM_BORN), onSelect = c(SalesWeightCheckField.FARM_BORN))
    }
}

@Composable
private fun SoldTagsForm(state: SalesEvidenceUiState, onEvent: (SalesEvidenceEvent) -> Unit) {
    VendorsFormGroup(title = TAGS_GROUP) {
        VendorsDropdownField(LABEL_FARM, state.values[FIELD_FARM].orEmpty(), state.farms, { onEvent(SalesEvidenceEvent.FieldChanged(FIELD_FARM, it)) }, required = true, error = state.fieldErrors[FIELD_FARM], placeholder = HINT_PICK)
        state.tagRows.forEach { row ->
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                    .background(MeshaColors.Surf2)
                    .padding(10.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                // The ANIMAL is what the server requires on every row; the tag number is optional,
                // because a sold animal is not always the one whose tag was legible.
                VendorsTextField(row.animalLabel, { onEvent(SalesEvidenceEvent.TagRowChanged(row.rowKey, FIELD_ANIMAL_LABEL, it)) }, LABEL_ANIMAL, required = true, error = row.error.ifBlank { null })
                VendorsTextField(row.tagNumber, { onEvent(SalesEvidenceEvent.TagRowChanged(row.rowKey, FIELD_TAG_NUMBER, it)) }, LABEL_TAG)
                VendorsTextField(row.weightKg, { onEvent(SalesEvidenceEvent.TagRowChanged(row.rowKey, FIELD_WEIGHT_KG, it)) }, LABEL_WEIGHT, keyboard = KeyboardType.Decimal)
                if (state.tagRows.size > 1) {
                    VendorsGhostButton(label = REMOVE_ROW, onClick = { onEvent(SalesEvidenceEvent.RemoveTagRow(row.rowKey)) }, modifier = Modifier.fillMaxWidth())
                }
            }
        }
        VendorsGhostButton(label = ADD_ROW, onClick = { onEvent(SalesEvidenceEvent.AddTagRow) }, modifier = Modifier.fillMaxWidth())
    }
}

/** Keys the sold-tag row editor sends back; they are the wire field names. Public because the
 *  view model that builds the request from them lives in another module. */
const val FIELD_FARM = "farm"
const val FIELD_TAG_NUMBER = "tag_number"
const val FIELD_ANIMAL_LABEL = "animal_label"
const val FIELD_WEIGHT_KG = "weight_kg"

private val FARM_BORN_OPTIONS = listOf(VendorsOptionUi("true", "Farm born"), VendorsOptionUi("false", "Purchased"))

private const val CLOSE_AFTER_SAVE_MS = 2_000L
private const val HUB_TITLE = "Pipeline and evidence"
private const val HUB_SUBTITLE = "Buyer demand, farmer groups, market quotes and the weight evidence behind a sale"
private const val ADD_BUYER_LEAD = "Add buyer lead"
private const val ADD_FARMER_GROUP = "Add farmer group"
private const val EDIT_BUYER_LEAD = "Change buyer lead"
private const val EDIT_FARMER_GROUP = "Change farmer group"
private const val CHANGE_STATUS = "Change status"
private const val EDIT_LEAD = "Change details"
private const val QUOTE_GROUP = "MARKET QUOTE"
private const val WEIGHT_GROUP = "WEIGHT CHECK"
private const val TAGS_GROUP = "SOLD TAGS"
private const val HINT_PICK = "Tap to choose"
private const val CANCEL = "Cancel"
private const val SAVE = "Save"
private const val SAVING = "Saving…"
private const val SAVED = "Saved"
private const val ADD_ROW = "Add another tag"
private const val REMOVE_ROW = "Remove this tag"
private const val LABEL_BUYER_NAME = "Buyer name"
private const val LABEL_PLACE = "Place"
private const val LABEL_FARM = "Farm"
private const val LABEL_ANIMAL_TYPE = "Animal type"
private const val LABEL_BREED = "Breed"
private const val LABEL_RECORDED_ON = "Recorded on"
private const val LABEL_CALL_STATUS = "Call status"
private const val LABEL_PHONE = "Phone number"
private const val LABEL_FPO_NAME = "Farmer group name"
private const val LABEL_CROPS = "Crops"
private const val LABEL_DISTRICT = "District"
private const val LABEL_TALUK = "Taluk"
private const val LABEL_STATE = "State"
private const val LABEL_MARKET = "Market"
private const val LABEL_CATEGORY = "Category"
private const val LABEL_SOURCE = "Source"
private const val SOURCE_HINT = "Who quoted it"
private const val LABEL_EX_FARM = "Ex-farm rate"
private const val LABEL_TRANSPORT = "Transport rate"
private const val LABEL_LANDING = "Landing cost per kg (₹)"
private const val LABEL_MARKET_PRICE = "Market price per kg (₹)"
private const val LABEL_TAG = "Tag number"
private const val LABEL_ANIMAL = "Animal"
private const val LABEL_WEIGHT = "Weight (kg)"
private const val LABEL_BOOK_WEIGHT = "Book weight (kg)"
private const val LABEL_VIDEO_WEIGHT = "Weight from the video (kg)"
private const val LABEL_FARM_BORN = "Where the animal came from"
