package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleDetailViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.clickable
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.LoadingSkeletonList
import sg.mesha.goatos.core.ui.RefreshOnResume
import sg.mesha.goatos.core.ui.SyncIconButton

/** One sale (L1 drill): the sale, the buyer, the money, and the animals tagged to it. */
@Composable
fun SaleDetailScreen(
    state: SaleDetailUiState,
    onEvent: (SaleDetailEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    RefreshOnResume { onEvent(SaleDetailEvent.Refresh) }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = state.title,
            subtitle = state.subtitle.ifBlank { null },
            onBack = { onEvent(SaleDetailEvent.Back) },
            actions = { SyncIconButton(isSyncing = state.isRefreshing, onSync = { onEvent(SaleDetailEvent.Refresh) }) },
        )
        if (state.isLoading) {
            LoadingSkeletonList(modifier = Modifier.padding(MeshaDimens.gutter))
            return@Column
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "status") {
                Row(verticalAlignment = Alignment.CenterVertically) { VendorsChip(label = state.statusLabel, tone = state.statusTone) }
            }
            state.message?.let { message -> item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) } }
            if (state.editMessage.isNotBlank()) {
                item(key = "edit_message") { VendorsResultBanner(status = VendorsWriteStatus.QUEUED, message = state.editMessage) }
            }
            items(count = state.sections.size, key = { "section_${state.sections[it].title}" }) { index ->
                VendorsDetailSection(state.sections[index])
            }
            item(key = "money") { MoneyCard(state, onEvent) }
            item(key = "status_edit") { StatusCard(state, onEvent) }
            item(key = "tagged") { TaggedAnimalsCard(state) }
        }
        if (state.canTagAnimals) {
            VendorsWizardBar(contextLine = "") {
                VendorsPrimaryButton(label = TAG_ANIMALS, enabled = true, onClick = { onEvent(SaleDetailEvent.TagAnimals) }, modifier = Modifier.weight(1f))
            }
        }
    }
}

/**
 * The money on this sale: what the server says is still due, every receipt against it, and the way
 * to add or correct one. Editing a recorded sale is the whole point of this card (maintainer
 * instruction 2026-09-04) — before it the phone could record a sale and then never touch it again.
 *
 * `balanceLine` is the BACKEND's `payment_balance`, formatted. Nothing here subtracts anything.
 */
@Composable
private fun MoneyCard(state: SaleDetailUiState, onEvent: (SaleDetailEvent) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = MONEY_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        if (state.balanceLine.isNotBlank()) {
            Text(text = state.balanceLine, color = MeshaColors.Ink, style = MeshaType.rowValue)
        }
        if (state.payments.isEmpty()) {
            Text(text = NO_PAYMENTS, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        state.payments.forEach { payment ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                    .clickable(enabled = !state.editInFlight, role = Role.Button) { onEvent(SaleDetailEvent.OpenPayment(payment.paymentId)) }
                    .padding(vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Column(Modifier.weight(1f)) {
                    Text(text = payment.amount, color = MeshaColors.Ink, style = MeshaType.rowValue)
                    val caption = listOf(payment.receivedOn, payment.note).filter { it.isNotBlank() }.joinToString(" · ")
                    if (caption.isNotBlank()) {
                        Text(text = caption, color = MeshaColors.Muted, style = MeshaType.caption)
                    }
                }
                Text(text = EDIT_HINT, color = MeshaColors.BrandD, style = MeshaType.caption)
            }
        }
        val editor = state.paymentEditor
        if (editor == null) {
            Spacer(Modifier.height(2.dp))
            VendorsGhostButton(
                label = ADD_PAYMENT,
                onClick = { onEvent(SaleDetailEvent.OpenPayment("")) },
                enabled = !state.editInFlight,
                modifier = Modifier.fillMaxWidth(),
            )
        } else {
            PaymentEditor(state, editor, onEvent)
        }
    }
}

/** Add a receipt, or correct/remove one already recorded. */
@Composable
private fun PaymentEditor(state: SaleDetailUiState, editor: SalePaymentEditorUi, onEvent: (SaleDetailEvent) -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf2)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = if (editor.paymentId.isBlank()) ADD_PAYMENT else EDIT_PAYMENT,
            color = MeshaColors.Muted,
            style = MeshaType.sectionLabel,
        )
        VendorsDateField(
            label = LABEL_RECEIVED_ON,
            valueIso = editor.values[SalePaymentField.RECEIVED_ON].orEmpty(),
            onValueChange = { onEvent(SaleDetailEvent.PaymentFieldChanged(SalePaymentField.RECEIVED_ON, it)) },
            required = true,
            error = editor.fieldErrors[SalePaymentField.RECEIVED_ON],
            // Money cannot be received tomorrow; the picker refuses a future day rather than
            // letting the ledger carry one.
            maxIso = state.today,
        )
        VendorsTextField(
            value = editor.values[SalePaymentField.AMOUNT].orEmpty(),
            onValueChange = { onEvent(SaleDetailEvent.PaymentFieldChanged(SalePaymentField.AMOUNT, it)) },
            label = LABEL_AMOUNT,
            required = true,
            keyboard = KeyboardType.Decimal,
            error = editor.fieldErrors[SalePaymentField.AMOUNT],
        )
        VendorsTextField(
            value = editor.values[SalePaymentField.NOTE].orEmpty(),
            onValueChange = { onEvent(SaleDetailEvent.PaymentFieldChanged(SalePaymentField.NOTE, it)) },
            label = LABEL_NOTE,
            error = editor.fieldErrors[SalePaymentField.NOTE],
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            VendorsGhostButton(label = CANCEL, onClick = { onEvent(SaleDetailEvent.ClosePayment) }, enabled = !editor.inFlight)
            VendorsPrimaryButton(
                label = SAVE_PAYMENT,
                enabled = !editor.inFlight,
                onClick = { onEvent(SaleDetailEvent.SavePayment) },
                modifier = Modifier.weight(1f),
            )
        }
        if (editor.paymentId.isNotBlank()) {
            VendorsGhostButton(
                label = REMOVE_PAYMENT,
                onClick = { onEvent(SaleDetailEvent.DeletePayment) },
                enabled = !editor.inFlight,
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
}

/** Where the deal stands. Picking a word queues the change; the chip above follows server truth. */
@Composable
private fun StatusCard(state: SaleDetailUiState, onEvent: (SaleDetailEvent) -> Unit) {
    if (state.statuses.isEmpty()) return
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = STATUS_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        VendorsDropdownField(
            label = LABEL_STATUS,
            selectedValue = state.statusLabel,
            options = state.statuses,
            onSelect = { if (it != state.statusLabel) onEvent(SaleDetailEvent.ChangeStatus(it)) },
        )
    }
}

@Composable
private fun TaggedAnimalsCard(state: SaleDetailUiState) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = TAGGED_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        Text(text = state.taggedLine.ifBlank { TAGGED_NONE }, color = MeshaColors.Ink, style = MeshaType.rowValue)
        state.taggedGroups.forEach { group ->
            Column {
                Text(text = "${group.location} · ${group.animals}", color = MeshaColors.Ink, style = MeshaType.body)
                if (group.tags.isNotBlank()) Text(text = group.tags, color = MeshaColors.Muted, style = MeshaType.caption)
            }
        }
        if (!state.canTagAnimals && state.tagDisabledReason.isNotBlank()) {
            Spacer(Modifier.height(2.dp))
            Text(text = state.tagDisabledReason, color = MeshaColors.Muted, style = MeshaType.caption)
        }
    }
}

private const val MONEY_TITLE = "MONEY"
private const val STATUS_TITLE = "WHERE THIS DEAL STANDS"
private const val NO_PAYMENTS = "No payments recorded yet"
private const val ADD_PAYMENT = "Add payment"
private const val EDIT_PAYMENT = "Correct this payment"
private const val EDIT_HINT = "Edit"
private const val REMOVE_PAYMENT = "Remove this payment"
private const val SAVE_PAYMENT = "Save payment"
private const val CANCEL = "Cancel"
private const val LABEL_RECEIVED_ON = "Received on"
private const val LABEL_AMOUNT = "Amount (₹)"
private const val LABEL_NOTE = "Note"
private const val LABEL_STATUS = "Status"
private const val TAG_ANIMALS = "Tag animals to sale"
private const val TAGGED_TITLE = "ANIMALS TAGGED"
private const val TAGGED_NONE = "No animals tagged yet"
