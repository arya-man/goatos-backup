package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; FeedPurchaseCreateViewModel (in :app) owns the
// vendors_* AnalyticsEventsVendors + CrashReporter wiring for the queued write.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Record a feed purchase (L1 drill): TWO steps.
 *
 *   1. The load   farm, feed, vendor, quantity, bought on — and, only if it already came in,
 *                 the reached date and the weight received
 *   2. The money  cost split or a total, payment status, amount released
 */
@Composable
fun FeedPurchaseCreateScreen(
    state: FeedPurchaseCreateUiState,
    onEvent: (FeedPurchaseCreateEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    LaunchedEffect(state.closeAfterSave) {
        if (state.closeAfterSave) {
            delay(CLOSE_AFTER_SAVE_MS)
            onEvent(FeedPurchaseCreateEvent.Back)
        }
    }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = TITLE, subtitle = STEP_TITLES.getOrNull(state.step), onBack = { onEvent(FeedPurchaseCreateEvent.Back) })
        VendorsStepper(stepCount = state.stepCount, currentIndex = state.step, caption = "Step ${state.step + 1} of ${state.stepCount}")
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
            when (state.step) {
                0 -> item(key = "load") { LoadStep(state, onEvent) }
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
                if (state.step > 0) VendorsGhostButton(label = PREVIOUS, onClick = { onEvent(FeedPurchaseCreateEvent.Previous) })
                val last = state.step == state.stepCount - 1
                VendorsPrimaryButton(
                    label = if (last) SAVE else NEXT,
                    enabled = !state.submitInFlight,
                    onClick = { onEvent(if (last) FeedPurchaseCreateEvent.Submit else FeedPurchaseCreateEvent.Next) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun LoadStep(state: FeedPurchaseCreateUiState, onEvent: (FeedPurchaseCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = STEP_TITLES[0]) {
        Text(text = LABEL_FARM, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = state.farms, selectedValue = v[PurchaseField.FARM].orEmpty(), onSelect = { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.FARM, it)) })
        e[PurchaseField.FARM]?.let { Text(it, color = MeshaColors.Danger, style = MeshaType.caption) }
        VendorsDropdownField(LABEL_FEED, v[PurchaseField.FEED_ITEM].orEmpty(), state.feedItems, { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.FEED_ITEM, it)) }, required = true, error = e[PurchaseField.FEED_ITEM], placeholder = HINT_PICK)
        VendorsDropdownField(
            LABEL_VENDOR,
            v[PurchaseField.VENDOR].orEmpty(),
            state.vendorSuggestions.map { VendorsOptionUi(it, it) },
            { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.VENDOR, it)) },
            required = true,
            error = e[PurchaseField.VENDOR],
            placeholder = HINT_PICK_VENDOR,
        )
        // The typed box carries only a NEW name: a vendor picked above is shown there, not echoed here.
        val typed = v[PurchaseField.VENDOR].orEmpty().takeUnless { it in state.vendorSuggestions }.orEmpty()
        VendorsTextField(typed, { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.VENDOR, it)) }, LABEL_VENDOR_TYPED, supporting = HINT_VENDOR_TYPED)
        VendorsTextField(v[PurchaseField.QUANTITY_KG].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.QUANTITY_KG, it)) }, LABEL_QUANTITY, required = true, keyboard = KeyboardType.Decimal, error = e[PurchaseField.QUANTITY_KG])
        VendorsDateField(LABEL_BOUGHT_ON, v[PurchaseField.PURCHASE_DATE].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.PURCHASE_DATE, it)) }, required = true, error = e[PurchaseField.PURCHASE_DATE], maxIso = state.today)
    }
    Spacer(Modifier.height(10.dp))
    VendorsFormGroup(title = LABEL_DELIVERY) {
        Text(text = HINT_DELIVERY, color = MeshaColors.Muted, style = MeshaType.caption)
        VendorsDateField(LABEL_REACHED_ON, v[PurchaseField.REACHED_ON].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.REACHED_ON, it)) }, error = e[PurchaseField.REACHED_ON], minIso = v[PurchaseField.PURCHASE_DATE].orEmpty(), maxIso = state.today, clearLabel = CLEAR_DATE)
        VendorsTextField(v[PurchaseField.REACHED_WEIGHT_KG].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.REACHED_WEIGHT_KG, it)) }, LABEL_REACHED_WEIGHT, keyboard = KeyboardType.Decimal, error = e[PurchaseField.REACHED_WEIGHT_KG], supporting = HINT_REACHED_WEIGHT)
    }
}

@Composable
private fun MoneyStep(state: FeedPurchaseCreateUiState, onEvent: (FeedPurchaseCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = LABEL_COSTS) {
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            VendorsTextField(v[PurchaseField.FEED_COST].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.FEED_COST, it)) }, LABEL_FEED_COST, keyboard = KeyboardType.Decimal, error = e[PurchaseField.FEED_COST], modifier = Modifier.weight(1f))
            VendorsTextField(v[PurchaseField.TRANSPORT_COST].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.TRANSPORT_COST, it)) }, LABEL_TRANSPORT_COST, keyboard = KeyboardType.Decimal, error = e[PurchaseField.TRANSPORT_COST], modifier = Modifier.weight(1f))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            VendorsTextField(v[PurchaseField.LOADING_COST].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.LOADING_COST, it)) }, LABEL_LOADING_COST, keyboard = KeyboardType.Decimal, error = e[PurchaseField.LOADING_COST], modifier = Modifier.weight(1f))
            VendorsTextField(v[PurchaseField.UNLOADING_COST].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.UNLOADING_COST, it)) }, LABEL_UNLOADING_COST, keyboard = KeyboardType.Decimal, error = e[PurchaseField.UNLOADING_COST], modifier = Modifier.weight(1f))
        }
        VendorsTextField(v[PurchaseField.TOTAL_COST].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.TOTAL_COST, it)) }, LABEL_TOTAL_COST, keyboard = KeyboardType.Decimal, error = e[PurchaseField.TOTAL_COST], supporting = HINT_TOTAL)
    }
    Spacer(Modifier.height(10.dp))
    VendorsFormGroup(title = LABEL_PAYMENT) {
        Text(text = LABEL_PAYMENT_STATUS, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = state.paymentStatuses, selectedValue = v[PurchaseField.PAYMENT_STATUS].orEmpty(), onSelect = { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.PAYMENT_STATUS, it)) })
        e[PurchaseField.PAYMENT_STATUS]?.let { Text(it, color = MeshaColors.Danger, style = MeshaType.caption) }
        VendorsTextField(v[PurchaseField.PAYMENT_RELEASED].orEmpty(), { onEvent(FeedPurchaseCreateEvent.FieldChanged(PurchaseField.PAYMENT_RELEASED, it)) }, LABEL_PAYMENT_RELEASED, keyboard = KeyboardType.Decimal, error = e[PurchaseField.PAYMENT_RELEASED])
    }
}

private const val TITLE = "Record feed purchase"
private val STEP_TITLES = listOf("The load", "The money")
private const val NEXT = "Next"
private const val PREVIOUS = "Back"
private const val SAVE = "Save purchase"
/** How long the saved banner stays on the finished form before the screen closes itself. */
private const val CLOSE_AFTER_SAVE_MS = 2_000L
private const val SAVING = "Saving…"
private const val SAVED = "Saved"
private const val LABEL_FARM = "Farm"
private const val LABEL_FEED = "Feed"
private const val LABEL_VENDOR = "Vendor"
private const val LABEL_VENDOR_TYPED = "Or type a new vendor"
private const val LABEL_QUANTITY = "Quantity bought (kg)"
private const val LABEL_BOUGHT_ON = "Bought on"
private const val LABEL_DELIVERY = "Delivery"
private const val LABEL_REACHED_ON = "Delivered on"
private const val LABEL_REACHED_WEIGHT = "Weight received (kg)"
private const val LABEL_COSTS = "Landed cost"
private const val LABEL_FEED_COST = "Feed cost"
private const val LABEL_TRANSPORT_COST = "Transport"
private const val LABEL_LOADING_COST = "Loading"
private const val LABEL_UNLOADING_COST = "Unloading"
private const val LABEL_TOTAL_COST = "Total cost"
private const val LABEL_PAYMENT = "Payment"
private const val LABEL_PAYMENT_STATUS = "Payment status"
private const val LABEL_PAYMENT_RELEASED = "Amount released so far (₹)"
private const val HINT_PICK = "Tap to choose"
private const val HINT_PICK_VENDOR = "Vendors bought from before"
private const val HINT_VENDOR_TYPED = "Only if the vendor is not in the list above."
private const val HINT_DELIVERY = "Leave the delivered date blank if the load is still in transit. It is counted as stock only once it is delivered."
private const val HINT_REACHED_WEIGHT = "Leave blank if not weighed yet — the bought quantity counts until then."
private const val HINT_TOTAL = "Leave blank to add up the parts above."
private const val CLEAR_DATE = "Still in transit"
