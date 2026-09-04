package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; FeedPurchaseDetailViewModel (in :app) owns the
// vendors_* AnalyticsEventsVendors + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/**
 * One feed purchase (L1 drill): the load, its money, and where the delivery stands -- and, since
 * the maintainer's 2026-09-04 instruction, every change the web's drawer can make to it. Before
 * that this screen was read-only and told the operator to go and mark the load reached on the web.
 */
@Composable
fun FeedPurchaseDetailScreen(
    state: FeedPurchaseDetailUiState,
    onEvent: (FeedPurchaseDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(FeedPurchaseDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(FeedPurchaseDetailEvent.Back) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(FeedPurchaseDetailEvent.Refresh) }) },
        )
        if (state.isLoading) {
            LoadingSkeletonList(modifier = Modifier.padding(MeshaDimens.gutter))
            return@Column
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "status") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    VendorsChip(label = state.deliveryLabel, tone = state.deliveryTone)
                }
            }
            if (state.editMessage.isNotBlank()) {
                item(key = "edit_message") {
                    VendorsResultBanner(
                        status = if (state.editFailed) VendorsWriteStatus.FAILED else VendorsWriteStatus.QUEUED,
                        message = state.editMessage,
                    )
                }
            }
            // The note says the load is not stock yet; the action that fixes that belongs WITH it,
            // at the top. It used to sit under the detail sections at the bottom of the screen,
            // where the operator had to scroll past the whole load to find it and reported it as
            // missing (2026-09-04).
            if (state.deliveryNote.isNotBlank()) {
                item(key = "delivery_note") {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                            .background(MeshaColors.WarnX)
                            .padding(horizontal = 14.dp, vertical = 12.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Text(text = state.deliveryNote, color = MeshaColors.Warn, style = MeshaType.body)
                        if (state.editor == FeedPurchaseEditorKind.DELIVERY) {
                            DeliveryEditor(state, onEvent)
                        } else if (state.canMarkReached) {
                            VendorsPrimaryButton(
                                label = MARK_REACHED,
                                enabled = !state.editInFlight,
                                onClick = { onEvent(FeedPurchaseDetailEvent.OpenEditor(FeedPurchaseEditorKind.DELIVERY)) },
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                    }
                }
            }
            items(count = state.sections.size, key = { "section_${state.sections[it].title}" }) { index ->
                VendorsDetailSection(state.sections[index])
            }
            item(key = "money_edit") { MoneyCard(state, onEvent) }
            item(key = "load_edit") { LoadCard(state, onEvent) }
        }
    }
}

/** What is still owed on this load, every instalment against it, and the way to add one. */
@Composable
private fun MoneyCard(state: FeedPurchaseDetailUiState, onEvent: (FeedPurchaseDetailEvent) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = PAYMENTS_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        if (state.balanceLine.isNotBlank()) {
            Text(text = state.balanceLine, color = MeshaColors.Ink, style = MeshaType.rowValue)
        }
        if (state.payments.isEmpty()) {
            Text(text = NO_PAYMENTS, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        state.payments.forEach { payment ->
            Column(Modifier.fillMaxWidth().padding(vertical = 6.dp)) {
                Text(text = payment.amount, color = MeshaColors.Ink, style = MeshaType.rowValue)
                val caption = listOf(payment.paidOn, payment.note).filter { it.isNotBlank() }.joinToString(" · ")
                if (caption.isNotBlank()) Text(text = caption, color = MeshaColors.Muted, style = MeshaType.caption)
            }
        }
        if (state.editor == FeedPurchaseEditorKind.PAYMENT) {
            PaymentEditor(state, onEvent)
        } else {
            VendorsGhostButton(
                label = ADD_PAYMENT,
                onClick = { onEvent(FeedPurchaseDetailEvent.OpenEditor(FeedPurchaseEditorKind.PAYMENT)) },
                enabled = !state.editInFlight,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (state.paymentStatuses.isNotEmpty()) {
            VendorsDropdownField(
                label = LABEL_PAYMENT_STATUS,
                selectedValue = state.paymentStatus,
                options = state.paymentStatuses,
                onSelect = { if (it != state.paymentStatus) onEvent(FeedPurchaseDetailEvent.ChangePaymentStatus(it)) },
            )
        }
    }
}

/** Correcting the load's values, and saying the truck came in. */
@Composable
private fun LoadCard(state: FeedPurchaseDetailUiState, onEvent: (FeedPurchaseDetailEvent) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = LOAD_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        // Marking reached is NOT offered here: it lives with the on-the-road note at the top,
        // where the operator is already reading why the load is not stock yet.
        if (state.editor == FeedPurchaseEditorKind.EDIT) {
            EditEditor(state, onEvent)
        } else {
            VendorsGhostButton(
                label = EDIT_PURCHASE,
                onClick = { onEvent(FeedPurchaseDetailEvent.OpenEditor(FeedPurchaseEditorKind.EDIT)) },
                enabled = !state.editInFlight,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

@Composable
private fun PaymentEditor(state: FeedPurchaseDetailUiState, onEvent: (FeedPurchaseDetailEvent) -> Unit) {
    EditorFrame(title = ADD_PAYMENT, state = state, onEvent = onEvent) {
        VendorsDateField(
            label = LABEL_PAID_ON,
            valueIso = state.editorValues[PurchaseField.PAYMENT_PAID_ON].orEmpty(),
            onValueChange = { onEvent(FeedPurchaseDetailEvent.FieldChanged(PurchaseField.PAYMENT_PAID_ON, it)) },
            required = true,
            error = state.editorErrors[PurchaseField.PAYMENT_PAID_ON],
            // Money cannot be paid tomorrow.
            maxIso = state.today,
        )
        VendorsTextField(
            value = state.editorValues[PurchaseField.PAYMENT_AMOUNT].orEmpty(),
            onValueChange = { onEvent(FeedPurchaseDetailEvent.FieldChanged(PurchaseField.PAYMENT_AMOUNT, it)) },
            label = LABEL_AMOUNT,
            required = true,
            keyboard = KeyboardType.Decimal,
            error = state.editorErrors[PurchaseField.PAYMENT_AMOUNT],
        )
        VendorsTextField(
            value = state.editorValues[PurchaseField.PAYMENT_NOTE].orEmpty(),
            onValueChange = { onEvent(FeedPurchaseDetailEvent.FieldChanged(PurchaseField.PAYMENT_NOTE, it)) },
            label = LABEL_NOTE,
            error = state.editorErrors[PurchaseField.PAYMENT_NOTE],
        )
    }
}

/** The load's VALUES. Farm, feed and batch are absent: identity is immutable on the server too. */
@Composable
private fun EditEditor(state: FeedPurchaseDetailUiState, onEvent: (FeedPurchaseDetailEvent) -> Unit) {
    fun v(f: PurchaseField) = state.editorValues[f].orEmpty()
    fun e(f: PurchaseField) = state.editorErrors[f]
    fun c(f: PurchaseField) = { value: String -> onEvent(FeedPurchaseDetailEvent.FieldChanged(f, value)) }
    EditorFrame(title = EDIT_PURCHASE, state = state, onEvent = onEvent) {
        VendorsDateField(LABEL_BOUGHT_ON, v(PurchaseField.PURCHASE_DATE), c(PurchaseField.PURCHASE_DATE), required = true, error = e(PurchaseField.PURCHASE_DATE), maxIso = state.today)
        VendorsTextField(v(PurchaseField.QUANTITY_KG), c(PurchaseField.QUANTITY_KG), LABEL_QUANTITY, required = true, keyboard = KeyboardType.Decimal, error = e(PurchaseField.QUANTITY_KG))
        VendorsTextField(v(PurchaseField.VENDOR), c(PurchaseField.VENDOR), LABEL_VENDOR, required = true, error = e(PurchaseField.VENDOR))
        VendorsTextField(v(PurchaseField.FEED_COST), c(PurchaseField.FEED_COST), LABEL_FEED_COST, keyboard = KeyboardType.Decimal, error = e(PurchaseField.FEED_COST))
        VendorsTextField(v(PurchaseField.TRANSPORT_COST), c(PurchaseField.TRANSPORT_COST), LABEL_TRANSPORT, keyboard = KeyboardType.Decimal, error = e(PurchaseField.TRANSPORT_COST))
        VendorsTextField(v(PurchaseField.LOADING_COST), c(PurchaseField.LOADING_COST), LABEL_LOADING, keyboard = KeyboardType.Decimal, error = e(PurchaseField.LOADING_COST))
        VendorsTextField(v(PurchaseField.UNLOADING_COST), c(PurchaseField.UNLOADING_COST), LABEL_UNLOADING, keyboard = KeyboardType.Decimal, error = e(PurchaseField.UNLOADING_COST))
        VendorsTextField(v(PurchaseField.TOTAL_COST), c(PurchaseField.TOTAL_COST), LABEL_TOTAL, keyboard = KeyboardType.Decimal, error = e(PurchaseField.TOTAL_COST), supporting = TOTAL_HINT)
    }
}

@Composable
private fun DeliveryEditor(state: FeedPurchaseDetailUiState, onEvent: (FeedPurchaseDetailEvent) -> Unit) {
    EditorFrame(title = MARK_REACHED, state = state, onEvent = onEvent) {
        VendorsDateField(
            label = LABEL_REACHED_ON,
            valueIso = state.editorValues[PurchaseField.REACHED_ON].orEmpty(),
            onValueChange = { onEvent(FeedPurchaseDetailEvent.FieldChanged(PurchaseField.REACHED_ON, it)) },
            required = true,
            error = state.editorErrors[PurchaseField.REACHED_ON],
            // A truck cannot arrive before it left, nor in the future.
            minIso = state.purchaseDate,
            maxIso = state.today,
        )
        VendorsTextField(
            value = state.editorValues[PurchaseField.REACHED_WEIGHT_KG].orEmpty(),
            onValueChange = { onEvent(FeedPurchaseDetailEvent.FieldChanged(PurchaseField.REACHED_WEIGHT_KG, it)) },
            label = LABEL_REACHED_WEIGHT,
            keyboard = KeyboardType.Decimal,
            error = state.editorErrors[PurchaseField.REACHED_WEIGHT_KG],
            supporting = REACHED_WEIGHT_HINT,
        )
    }
}

/** The shared frame every editor sits in: its fields, then Cancel and Save. */
@Composable
private fun EditorFrame(
    title: String,
    state: FeedPurchaseDetailUiState,
    onEvent: (FeedPurchaseDetailEvent) -> Unit,
    fields: @Composable () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf2)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(text = title, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        fields()
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            VendorsGhostButton(
                label = CANCEL,
                onClick = { onEvent(FeedPurchaseDetailEvent.OpenEditor(FeedPurchaseEditorKind.NONE)) },
                enabled = !state.editInFlight,
            )
            VendorsPrimaryButton(
                label = SAVE,
                enabled = !state.editInFlight,
                onClick = { onEvent(FeedPurchaseDetailEvent.SubmitEditor) },
                modifier = Modifier.weight(1f),
            )
        }
    }
}

private const val PAYMENTS_TITLE = "PAYMENTS"
private const val LOAD_TITLE = "CHANGE THIS LOAD"
private const val NO_PAYMENTS = "No payments recorded yet"
private const val ADD_PAYMENT = "Add payment"
private const val EDIT_PURCHASE = "Edit purchase"
private const val MARK_REACHED = "Mark reached"
private const val CANCEL = "Cancel"
private const val SAVE = "Save"
private const val LABEL_PAID_ON = "Paid on"
private const val LABEL_AMOUNT = "Amount (₹)"
private const val LABEL_NOTE = "Note"
private const val LABEL_PAYMENT_STATUS = "Payment status"
private const val LABEL_BOUGHT_ON = "Bought on"
private const val LABEL_QUANTITY = "Quantity (kg)"
private const val LABEL_VENDOR = "Vendor"
private const val LABEL_FEED_COST = "Feed cost (₹)"
private const val LABEL_TRANSPORT = "Transport (₹)"
private const val LABEL_LOADING = "Loading (₹)"
private const val LABEL_UNLOADING = "Unloading (₹)"
private const val LABEL_TOTAL = "Landed cost (₹)"
private const val TOTAL_HINT = "Leave blank to use the parts above"
private const val LABEL_REACHED_ON = "Reached on"
private const val LABEL_REACHED_WEIGHT = "Weight received (kg)"
private const val REACHED_WEIGHT_HINT = "Leave blank if nobody weighed it"
