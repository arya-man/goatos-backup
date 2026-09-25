package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; SaleDetailViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring.

import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.ui.EmptyTone
import sg.mesha.goatos.core.ui.EmptyState
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
        if (state.gone) {
            EmptyState(
                title = SALE_GONE,
                modifier = Modifier.fillMaxWidth().padding(MeshaDimens.gutter),
                icon = MeshaIcons.Warn,
                tone = EmptyTone.Warn,
            )
            return@Column
        }
        if (state.notFound) {
            EmptyState(
                title = SALE_NOT_FOUND,
                subtitle = SALE_NOT_FOUND_HINT,
                modifier = Modifier.fillMaxWidth().padding(MeshaDimens.gutter),
                icon = MeshaIcons.Warn,
                tone = EmptyTone.Warn,
                action = { VendorsPrimaryButton(label = SALES_TRY_AGAIN, enabled = !state.isRefreshing, onClick = { onEvent(SaleDetailEvent.Refresh) }) },
            )
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
            // THE FEED STORE'S QUESTION, and the two ways to answer it. Closing a sale is the
            // moment its feed leaves the store, so the store may refuse a close it never saw when
            // the sale was recorded. The sentence is the server's, naming the farm, the feed and
            // both figures, and is shown verbatim.
            if (state.stockConfirmMessage.isNotBlank()) {
                item(key = "stock_confirm") {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = state.stockConfirmMessage)
                        Text(text = STOCK_CONFIRM_HINT, color = MeshaColors.Muted, style = MeshaType.caption)
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            VendorsGhostButton(label = STOCK_CONFIRM_CANCEL, onClick = { onEvent(SaleDetailEvent.DismissStatusStock) })
                            VendorsPrimaryButton(
                                label = STOCK_CONFIRM_CLOSE,
                                enabled = !state.editInFlight,
                                onClick = { onEvent(SaleDetailEvent.ConfirmStatusStock) },
                                modifier = Modifier.weight(1f),
                            )
                        }
                    }
                }
            }
            items(count = state.sections.size, key = { "section_${state.sections[it].title}" }) { index ->
                VendorsDetailSection(state.sections[index])
            }
            item(key = "steps") { StepsCard(state, onEvent) }
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
        if (state.payments.isEmpty() && state.pendingPayments.isEmpty()) {
            Text(text = NO_PAYMENTS, color = MeshaColors.Muted, style = MeshaType.caption)
        }
        // Saved on this phone and not on the ledger yet: shown so nobody enters it a second time.
        state.pendingPayments.forEach { line ->
            Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
                Text(text = line, color = MeshaColors.Ink, style = MeshaType.rowValue)
                Text(text = PENDING_PAYMENT, color = MeshaColors.Warn, style = MeshaType.caption)
            }
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
        if (state.finalStatusPending.isNotBlank()) {
            VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = "$FINAL_STATUS_ASK_PREFIX${state.finalStatusPending}$FINAL_STATUS_ASK_SUFFIX")
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                VendorsGhostButton(label = FINAL_STATUS_KEEP, onClick = { onEvent(SaleDetailEvent.DismissFinalStatus) })
                VendorsPrimaryButton(
                    label = FINAL_STATUS_CONFIRM,
                    enabled = !state.editInFlight,
                    onClick = { onEvent(SaleDetailEvent.ConfirmFinalStatus) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

/**
 * The sale's SOP steps (SALES SOP, maintainer instruction 2026-09-19): what happens after the sale
 * was recorded -- tagging, loading, the money -- authored on the web and run by the backend as one
 * workflow per sale. The card shows the backend's counters and next step verbatim and opens the
 * shared workflow screen; it never lists or counts steps of its own.
 */
@Composable
private fun StepsCard(state: SaleDetailUiState, onEvent: (SaleDetailEvent) -> Unit) {
    val hasSteps = state.stepsWorkflowId.isNotBlank()
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .clickable(enabled = hasSteps, role = Role.Button) { onEvent(SaleDetailEvent.OpenSteps(state.stepsWorkflowId)) }
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(text = STEPS_TITLE, color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        when {
            hasSteps -> {
                Text(text = state.stepsProgressLine.ifBlank { STEPS_OPEN }, color = MeshaColors.Ink, style = MeshaType.rowValue)
                if (state.stepsNextLine.isNotBlank()) {
                    Text(text = state.stepsNextLine, color = MeshaColors.Muted, style = MeshaType.caption)
                }
                Text(text = STEPS_OPEN, color = MeshaColors.BrandD, style = MeshaType.pillStrong)
            }
            state.stepsUnavailable -> Text(text = STEPS_OFFLINE, color = MeshaColors.Muted, style = MeshaType.caption)
            else -> Text(text = STEPS_PENDING, color = MeshaColors.Muted, style = MeshaType.caption)
        }
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
private const val STEPS_TITLE = "STEPS FOR THIS SALE"
private const val STEPS_OPEN = "Open the steps"
private const val STEPS_PENDING = "Steps will appear once the sale is synced."
private const val STEPS_OFFLINE = "Waiting for network to read the steps."
private const val TAGGED_NONE = "No animals tagged yet"
// The feed store's question at the OTHER moment feed leaves it: closing a sale recorded earlier.
private const val STOCK_CONFIRM_HINT = "Close it anyway only if the feed really did leave. If a load reached the farm and is not recorded yet, record that purchase instead."
private const val STOCK_CONFIRM_CLOSE = "I checked the store — close it"
private const val STOCK_CONFIRM_CANCEL = "Leave it"
private const val PENDING_PAYMENT = "Saved on this phone · waiting to send"
private const val FINAL_STATUS_ASK_PREFIX = "Mark this sale as "
private const val FINAL_STATUS_ASK_SUFFIX = "? This cannot be undone, and any animals tagged to it go back to their pens."
private const val FINAL_STATUS_KEEP = "Keep the sale"
private const val FINAL_STATUS_CONFIRM = "Yes, mark it"

private const val SALE_NOT_FOUND = "This sale is not on this phone yet"
private const val SALE_NOT_FOUND_HINT = "Open the sales list while online so it can load, then try again."
private const val SALE_GONE = "This sale no longer exists"
