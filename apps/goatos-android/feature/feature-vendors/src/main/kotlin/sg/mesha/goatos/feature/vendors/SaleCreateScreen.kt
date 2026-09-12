package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleCreateViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for the queued write.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
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
import kotlinx.coroutines.delay
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Record a sale (L1 drill): THREE steps, the web drawer's fields in the web drawer's order.
 *
 *   1. The sale    date, farm, then ONE CARD PER PRODUCT LINE -- product, breed, animals, weight,
 *                  value -- with "Add another product" and a running total (maintainer decision
 *                  2026-09-12: sheep and goats of several breeds go into one sale)
 *   2. The buyer   pick from the vendor register (search), then name and place
 *   3. The money   the sale total (read-only, the sum of the lines), advance, status, comments
 */
@Composable
fun SaleCreateScreen(
    state: SaleCreateUiState,
    onEvent: (SaleCreateEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    LaunchedEffect(state.closeAfterSave) {
        if (state.closeAfterSave) {
            delay(CLOSE_AFTER_SAVE_MS)
            onEvent(SaleCreateEvent.Back)
        }
    }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = TITLE, subtitle = STEP_TITLES.getOrNull(state.step), onBack = { onEvent(SaleCreateEvent.Back) })
        VendorsStepper(stepCount = state.stepCount, currentIndex = state.step, caption = "Step ${state.step + 1} of ${state.stepCount}")
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
            when (state.step) {
                0 -> item(key = "sale") { SaleStep(state, onEvent) }
                1 -> item(key = "buyer") { BuyerStep(state, onEvent) }
                else -> item(key = "money") { MoneyStep(state, onEvent) }
            }
        }
        VendorsWizardBar(contextLine = state.contextLine) {
            if (locked) {
                // The record is on its way or landed; the banner above says which. Nothing to press:
                // the screen closes on its own once the write is durable (CLOSE_AFTER_SAVE_MS).
                // "Saved" keeps the brand green so the success reads at a glance; "Saving…" is muted.
                val saved = state.writeStatus == VendorsWriteStatus.SYNCED
                VendorsPrimaryButton(label = if (saved) SAVED else SAVING, enabled = saved, onClick = {}, modifier = Modifier.weight(1f))
            } else {
                if (state.step > 0) VendorsGhostButton(label = PREVIOUS, onClick = { onEvent(SaleCreateEvent.Previous) })
                val last = state.step == state.stepCount - 1
                VendorsPrimaryButton(
                    label = if (last) SAVE else NEXT,
                    enabled = !state.submitInFlight,
                    onClick = { onEvent(if (last) SaleCreateEvent.Submit else SaleCreateEvent.Next) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun SaleStep(state: SaleCreateUiState, onEvent: (SaleCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = STEP_TITLES[0]) {
        VendorsDateField(LABEL_SALE_DATE, v[SaleField.SALE_DATE].orEmpty(), { onEvent(SaleCreateEvent.FieldChanged(SaleField.SALE_DATE, it)) }, required = true, error = e[SaleField.SALE_DATE], maxIso = state.maxDate, supporting = HINT_SALE_DATE)
        Text(text = LABEL_FARM, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = state.farms, selectedValue = v[SaleField.FARM].orEmpty(), onSelect = { onEvent(SaleCreateEvent.FieldChanged(SaleField.FARM, it)) })
        e[SaleField.FARM]?.let { Text(it, color = MeshaColors.Danger, style = MeshaType.caption) }
    }
    Spacer(Modifier.height(10.dp))
    VendorsFormGroup(title = LABEL_WHAT_WAS_SOLD) {
        Text(text = HINT_LINES, color = MeshaColors.Muted, style = MeshaType.caption)
        state.lines.forEachIndexed { index, line ->
            Spacer(Modifier.height(8.dp))
            SaleLineCard(index = index, line = line, products = state.productTypes, removable = state.lines.size > 1, onEvent = onEvent)
        }
        Spacer(Modifier.height(10.dp))
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            VendorsGhostButton(label = ADD_LINE, onClick = { onEvent(SaleCreateEvent.AddLine) }, enabled = state.canAddLine)
            Spacer(Modifier.weight(1f))
            Column(horizontalAlignment = Alignment.End) {
                Text(text = LABEL_SALE_TOTAL, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
                Text(text = state.totalLine.ifBlank { "—" }, color = MeshaColors.Ink, style = MeshaType.body, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

/**
 * One product line: product and breed on top, animals / weight / value in one row beneath. The
 * card owns no state -- every keystroke goes up as a LineChanged event keyed by the line id, so a
 * removed line above it cannot shift its values onto a neighbour.
 */
@Composable
private fun SaleLineCard(index: Int, line: SaleLineDraftUi, products: List<VendorsOptionUi>, removable: Boolean, onEvent: (SaleCreateEvent) -> Unit) {
    val e = line.errors
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
            .background(MeshaColors.Bg)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(text = "$LABEL_LINE ${index + 1}", color = MeshaColors.Muted, style = MeshaType.fieldLabel, modifier = Modifier.weight(1f))
            if (removable) {
                Row(
                    modifier = Modifier
                        .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                        .clickable(role = Role.Button, onClick = { onEvent(SaleCreateEvent.RemoveLine(line.id)) })
                        .padding(horizontal = 8.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    Icon(MeshaIcons.Close, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(14.dp))
                    Text(text = REMOVE_LINE, color = MeshaColors.Muted, style = MeshaType.caption)
                }
            }
        }
        Text(text = LABEL_PRODUCT, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = products, selectedValue = line.product, onSelect = { onEvent(SaleCreateEvent.LineChanged(line.id, SaleLineField.PRODUCT_TYPE, it)) })
        e[SaleLineField.PRODUCT_TYPE]?.let { Text(it, color = MeshaColors.Danger, style = MeshaType.caption) }
        VendorsDropdownField(LABEL_BREED, line.breed, line.breeds, { onEvent(SaleCreateEvent.LineChanged(line.id, SaleLineField.BREED, it)) }, required = true, error = e[SaleLineField.BREED], placeholder = if (line.breeds.isEmpty()) HINT_PICK_PRODUCT_FIRST else HINT_PICK)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            VendorsTextField(line.animals, { onEvent(SaleCreateEvent.LineChanged(line.id, SaleLineField.ANIMAL_COUNT, it)) }, LABEL_ANIMALS, keyboard = KeyboardType.Number, error = e[SaleLineField.ANIMAL_COUNT], modifier = Modifier.weight(1f))
            VendorsTextField(line.weightKg, { onEvent(SaleCreateEvent.LineChanged(line.id, SaleLineField.TOTAL_WEIGHT_KG, it)) }, LABEL_WEIGHT, keyboard = KeyboardType.Decimal, error = e[SaleLineField.TOTAL_WEIGHT_KG], modifier = Modifier.weight(1f))
        }
        VendorsTextField(line.value, { onEvent(SaleCreateEvent.LineChanged(line.id, SaleLineField.SALES_VALUE, it)) }, LABEL_LINE_VALUE, required = true, keyboard = KeyboardType.Decimal, error = e[SaleLineField.SALES_VALUE])
    }
}

@Composable
private fun BuyerStep(state: SaleCreateUiState, onEvent: (SaleCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = LABEL_BUYER_PICK) {
        VendorsSearchField(value = state.buyerSearch, placeholder = HINT_SEARCH_BUYER, onValueChange = { onEvent(SaleCreateEvent.BuyerSearchChanged(it)) })
        e[SaleField.BUYER_VENDOR_ID]?.let { Text(it, color = MeshaColors.Danger, style = MeshaType.caption) }
        when (state.buyerListState) {
            SaleBuyerListState.LOADING -> Text(text = HINT_BUYERS_LOADING, color = MeshaColors.Muted, style = MeshaType.caption)
            SaleBuyerListState.UNAVAILABLE -> Text(text = HINT_BUYERS_UNAVAILABLE, color = MeshaColors.Danger, style = MeshaType.caption)
            SaleBuyerListState.EMPTY -> Text(text = HINT_BUYERS_EMPTY, color = MeshaColors.Muted, style = MeshaType.caption)
            SaleBuyerListState.READY -> {
                if (state.buyers.isEmpty()) {
                    Text(text = HINT_NO_MATCH, color = MeshaColors.Muted, style = MeshaType.caption)
                } else {
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        state.buyers.forEach { buyer -> BuyerRow(buyer, selected = buyer.vendorId == v[SaleField.BUYER_VENDOR_ID]) { onEvent(SaleCreateEvent.BuyerPicked(buyer.vendorId)) } }
                    }
                }
                if (state.buyersTruncated) Text(text = HINT_BUYERS_TRUNCATED, color = MeshaColors.Muted, style = MeshaType.caption)
            }
        }
    }
    Spacer(Modifier.height(10.dp))
    VendorsFormGroup(title = LABEL_BUYER_DETAILS) {
        VendorsTextField(v[SaleField.BUYER_NAME].orEmpty(), { onEvent(SaleCreateEvent.FieldChanged(SaleField.BUYER_NAME, it)) }, LABEL_BUYER_NAME, required = true, error = e[SaleField.BUYER_NAME], supporting = HINT_PREFILL)
        VendorsTextField(v[SaleField.BUYER_PLACE].orEmpty(), { onEvent(SaleCreateEvent.FieldChanged(SaleField.BUYER_PLACE, it)) }, LABEL_BUYER_PLACE, error = e[SaleField.BUYER_PLACE])
    }
}

@Composable
private fun BuyerRow(buyer: SaleBuyerOptionUi, selected: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
            .background(if (selected) MeshaColors.OkX else MeshaColors.Bg)
            .clickable(role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(text = buyer.name, color = MeshaColors.Ink, style = MeshaType.body, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (buyer.place.isNotBlank()) Text(text = buyer.place, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (selected) Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Brand)
    }
}

@Composable
private fun MoneyStep(state: SaleCreateUiState, onEvent: (SaleCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = STEP_TITLES[2]) {
        // The sale value is the SUM of the lines entered on step 1 -- shown, never typed, so the
        // money can never disagree with what was sold.
        VendorsTextField(state.totalValueLine, {}, LABEL_VALUE, readOnly = true, supporting = HINT_TOTAL)
        VendorsTextField(v[SaleField.ADVANCE_AMOUNT].orEmpty(), { onEvent(SaleCreateEvent.FieldChanged(SaleField.ADVANCE_AMOUNT, it)) }, LABEL_ADVANCE, keyboard = KeyboardType.Decimal, error = e[SaleField.ADVANCE_AMOUNT])
        Text(text = LABEL_STATUS, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = state.statuses, selectedValue = v[SaleField.STATUS].orEmpty(), onSelect = { onEvent(SaleCreateEvent.FieldChanged(SaleField.STATUS, it)) })
        Text(text = HINT_STATUS, color = MeshaColors.Muted, style = MeshaType.caption)
        e[SaleField.STATUS]?.let { Text(it, color = MeshaColors.Danger, style = MeshaType.caption) }
        VendorsTextField(v[SaleField.COMMENTS].orEmpty(), { onEvent(SaleCreateEvent.FieldChanged(SaleField.COMMENTS, it)) }, LABEL_COMMENTS, singleLine = false, error = e[SaleField.COMMENTS])
    }
}

private const val TITLE = "Record sale"
private val STEP_TITLES = listOf("The sale", "The buyer", "The money")
private const val NEXT = "Next"
private const val PREVIOUS = "Back"
private const val SAVE = "Save sale"
/** How long the saved banner stays on the finished form before the screen closes itself. */
private const val CLOSE_AFTER_SAVE_MS = 2_000L
private const val SAVING = "Saving…"
private const val SAVED = "Saved"
private const val LABEL_SALE_DATE = "Sale date"
private const val HINT_SALE_DATE = "Up to 60 days ahead for a planned sale."
private const val LABEL_FARM = "Farm"
private const val LABEL_PRODUCT = "Product"
private const val LABEL_BREED = "Breed"
private const val LABEL_ANIMALS = "Animals"
private const val LABEL_WEIGHT = "Weight (kg)"
private const val LABEL_WHAT_WAS_SOLD = "What was sold"
private const val HINT_LINES = "One line per product and breed. Each line has its own animals, weight and value; the total adds up on its own."
private const val LABEL_LINE = "Line"
private const val LABEL_LINE_VALUE = "Value (₹)"
private const val ADD_LINE = "Add another product"
private const val REMOVE_LINE = "Remove"
private const val LABEL_SALE_TOTAL = "Sale total"
private const val HINT_TOTAL = "The sum of the lines on step 1."
private const val HINT_PICK = "Tap to choose"
private const val HINT_PICK_PRODUCT_FIRST = "Pick the product first"
private const val LABEL_BUYER_PICK = "Who bought"
private const val HINT_SEARCH_BUYER = "Search vendors — type 2 letters to narrow"
private const val HINT_BUYERS_LOADING = "Loading the vendor register…"
private const val HINT_BUYERS_UNAVAILABLE = "The vendor register could not be loaded. Connect and pull to refresh."
private const val HINT_BUYERS_EMPTY = "No active vendor yet. Add the buyer under Vendors first."
private const val HINT_NO_MATCH = "No vendor matches. Try another spelling, or add the buyer under Vendors."
private const val HINT_BUYERS_TRUNCATED = "Showing the first part of a large register. Search to narrow."
private const val LABEL_BUYER_DETAILS = "Buyer details"
private const val LABEL_BUYER_NAME = "Buyer name"
private const val HINT_PREFILL = "Filled from the vendor; edit if the sale was made to a different name."
private const val LABEL_BUYER_PLACE = "Buyer place"
private const val LABEL_VALUE = "Sale value"
private const val LABEL_ADVANCE = "Advance received (₹)"
private const val LABEL_STATUS = "Status"
private const val HINT_STATUS = "Deal Closed is the normal case; pick Advance Paid or In Discussion for a sale still in progress."
private const val LABEL_COMMENTS = "Comments"
