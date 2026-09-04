package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read refresh and queued write.

import androidx.compose.runtime.Immutable

/**
 * UI models for the Sales tab of the Procurement module (maintainer instruction 2026-09-04):
 * the sales done, recording a sale, and tagging the animals it is made of.
 *
 * Every business sentence a card or row shows is BACKEND-OWNED and rendered verbatim: the status
 * word and its tone, `payment_balance`, `operational_location_display`, `blocked_reason`. Form
 * FIELD labels are app chrome, as on every other phone entry form.
 */

// ---------------------------------------------------------------------------------------------
// Sales ledger
// ---------------------------------------------------------------------------------------------

@Immutable
data class SaleCardUi(
    val listKey: String,
    val dealId: String,
    /** Buyer name, VERBATIM. */
    val buyer: String,
    /** "Goat · Malai · CBE" */
    val productLine: String,
    /** "12 animals · 300 kg · ₹1,50,000" */
    val valueLine: String,
    /** "Sold 01-09-2026 · Balance ₹1,00,000" */
    val metaLine: String,
    /** Backend status word, VERBATIM. */
    val statusLabel: String,
    val statusTone: VendorsTone,
)

@Immutable
data class SalesListUiState(
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    /** Farm chips: "All" plus the backend farms. */
    val filters: List<VendorsFilterUi> = emptyList(),
    /** Whole-filter count line ("42 sales"); blank before the first refresh. */
    val countLine: String = "",
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val canAdd: Boolean = false,
)

sealed interface SalesListEvent {
    data object Refresh : SalesListEvent
    data class SelectFarm(val key: String) : SalesListEvent
    data class OpenSale(val dealId: String) : SalesListEvent
    data object AddSale : SalesListEvent
    /** Open the pipeline and evidence panels (maintainer instruction 2026-09-04). */
    data object OpenPipeline : SalesListEvent
}

/** One pen's share of the animals tagged to a sale, backend-composed. */
@Immutable
data class SaleShedGroupUi(val location: String, val animals: Int, val tags: String)

@Immutable
data class SaleDetailUiState(
    val title: String = "",
    val subtitle: String = "",
    val statusLabel: String = "",
    val statusTone: VendorsTone = VendorsTone.NEUTRAL,
    val sections: List<VendorsDetailSectionUi> = emptyList(),
    /** Animals already tagged to this sale ("8 of 12 tagged"); blank until read. */
    val taggedLine: String = "",
    val taggedGroups: List<SaleShedGroupUi> = emptyList(),
    /** Whether the tag-animals action is offered (the caller may allocate and the sale is live). */
    val canTagAnimals: Boolean = false,
    /** Backend reason when tagging is not offered; blank when it is. */
    val tagDisabledReason: String = "",
    // --- editing the sale ---
    /** Receipts already on the deal, newest last, as the server returned them. */
    val payments: List<SalePaymentUi> = emptyList(),
    /** "₹1,00,000 still due" / "Fully paid" — BACKEND `payment_balance`, formatted, never derived. */
    val balanceLine: String = "",
    /** The status vocabulary, backend-owned; blank until the options read lands. */
    val statuses: List<VendorsOptionUi> = emptyList(),
    /** Non-null while the receipt editor is open. */
    val paymentEditor: SalePaymentEditorUi? = null,
    /** Latest date a receipt may carry (today): money cannot be received tomorrow. */
    val today: String = "",
    /** A queued edit is on its way; the controls stay put but do not take a second tap. */
    val editInFlight: Boolean = false,
    /** Backend-owned confirmation of the last edit, blank when there is nothing to say. */
    val editMessage: String = "",
    val isRefreshing: Boolean = false,
    val isLoading: Boolean = true,
    val message: String? = null,
)

sealed interface SaleDetailEvent {
    data object Refresh : SaleDetailEvent
    data object Back : SaleDetailEvent
    data object TagAnimals : SaleDetailEvent
    data object DismissMessage : SaleDetailEvent

    // Editing the sale (maintainer instruction 2026-09-04).
    /** Open the receipt editor blank, or on an existing receipt. */
    data class OpenPayment(val paymentId: String) : SaleDetailEvent
    data object ClosePayment : SaleDetailEvent
    data class PaymentFieldChanged(val field: SalePaymentField, val value: String) : SaleDetailEvent
    data object SavePayment : SaleDetailEvent
    /** Remove the receipt the editor is open on. */
    data object DeletePayment : SaleDetailEvent
    /** Move the deal's status word to [status]. */
    data class ChangeStatus(val status: String) : SaleDetailEvent
}

/** The three fields of a buyer receipt. */
enum class SalePaymentField { RECEIVED_ON, AMOUNT, NOTE }

/** One receipt already on the deal, backend-composed. */
@Immutable
data class SalePaymentUi(
    val paymentId: String,
    /** "01-09-2026" */
    val receivedOn: String,
    /** "₹50,000" */
    val amount: String,
    val note: String,
    /** The values the editor reopens on. Kept beside the formatted ones rather than parsed back
     *  out of them: "₹50,000" is not something a number field takes, and a formatted date has
     *  already lost the form the server wants. */
    val receivedOnIso: String = "",
    val amountRaw: String = "",
)

/** The receipt editor: blank for a new receipt, filled when correcting one. */
@Immutable
data class SalePaymentEditorUi(
    /** Blank when adding; the receipt's id when correcting one. */
    val paymentId: String = "",
    val values: Map<SalePaymentField, String> = emptyMap(),
    val fieldErrors: Map<SalePaymentField, String> = emptyMap(),
    val inFlight: Boolean = false,
)

// ---------------------------------------------------------------------------------------------
// Record a sale
// ---------------------------------------------------------------------------------------------

enum class SaleField {
    SALE_DATE, FARM, PRODUCT_TYPE, BREED, ANIMAL_COUNT, TOTAL_WEIGHT_KG,
    BUYER_VENDOR_ID, BUYER_NAME, BUYER_PLACE,
    SALES_VALUE, ADVANCE_AMOUNT, STATUS, COMMENTS,
}

/** One buyer offered by the picker (identity and place only). */
@Immutable
data class SaleBuyerOptionUi(val vendorId: String, val name: String, val place: String)

/** Where the buyer picklist stands; each state has its own line, never a blank dropdown. */
enum class SaleBuyerListState { LOADING, READY, EMPTY, UNAVAILABLE }

@Immutable
data class SaleCreateUiState(
    val step: Int = 0,
    val stepCount: Int = 3,
    val values: Map<SaleField, String> = emptyMap(),
    val farms: List<VendorsOptionUi> = emptyList(),
    val productTypes: List<VendorsOptionUi> = emptyList(),
    /** Breeds of the chosen product; empty until a product is picked. */
    val breeds: List<VendorsOptionUi> = emptyList(),
    val statuses: List<VendorsOptionUi> = emptyList(),
    /** The typed buyer search; the picker narrows on it from two letters. */
    val buyerSearch: String = "",
    val buyers: List<SaleBuyerOptionUi> = emptyList(),
    val buyerListState: SaleBuyerListState = SaleBuyerListState.LOADING,
    /** The register is larger than one read; a missing buyer may still exist on the web. */
    val buyersTruncated: Boolean = false,
    val fieldErrors: Map<SaleField, String> = emptyMap(),
    val contextLine: String = "",
    val today: String = "",
    /** Latest date a sale may carry (today plus the backend horizon). */
    val maxDate: String = "",
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    /** The write is durable (accepted, or queued for when the phone is online): the screen shows the banner briefly and closes. */
    val closeAfterSave: Boolean = false,
    val submitInFlight: Boolean = false,
    val message: String? = null,
)

sealed interface SaleCreateEvent {
    data class FieldChanged(val field: SaleField, val value: String) : SaleCreateEvent
    data class BuyerSearchChanged(val text: String) : SaleCreateEvent
    data class BuyerPicked(val vendorId: String) : SaleCreateEvent
    data object Next : SaleCreateEvent
    data object Previous : SaleCreateEvent
    data object Back : SaleCreateEvent
    data object Submit : SaleCreateEvent
    data object RecordAnother : SaleCreateEvent
    data object DismissMessage : SaleCreateEvent
}

// ---------------------------------------------------------------------------------------------
// Tag animals to a sale
// ---------------------------------------------------------------------------------------------

/** One animal offered by the picker. Every sentence is backend-composed except the check state. */
@Immutable
data class SaleCandidateUi(
    val goatId: String,
    /** The RFID/tag the operator recognises the animal by. */
    val tag: String,
    /** "G-1042 · Malai · male" */
    val detailLine: String,
    /** Backend pen name, VERBATIM. */
    val location: String,
    val selected: Boolean,
    val sellable: Boolean,
    /** Backend farm copy for why it cannot be sold; blank when sellable. */
    val blockedReason: String,
)

enum class SaleTagStep { PICK, REVIEW, DONE }

@Immutable
data class SaleTagAnimalsUiState(
    val step: SaleTagStep = SaleTagStep.PICK,
    /** "Kumar Traders · Goat · 12 animals" — the sale being tagged, from its cached row. */
    val saleLine: String = "",
    val parks: List<VendorsOptionUi> = emptyList(),
    val selectedParkId: String = "",
    /** Pens of the chosen park; value = shed id + partition, label = backend pen name. */
    val pens: List<VendorsOptionUi> = emptyList(),
    val selectedPenKey: String = "",
    val search: String = "",
    val candidates: List<SaleCandidateUi> = emptyList(),
    val candidatesLoading: Boolean = false,
    val hasMore: Boolean = false,
    /** Blank while the catalog loads or when it failed; the empty state names the reason. */
    val candidatesEmptyMessage: String = "",
    val selectedCount: Int = 0,
    /** "8 still to pick" / "All 12 picked" / blank when the sale declares no count. */
    val pickLine: String = "",
    /** Review: backend-composed pen groups and the animals it refused. */
    val reviewGroups: List<SaleShedGroupUi> = emptyList(),
    val reviewBlocked: List<SaleCandidateUi> = emptyList(),
    val reviewLine: String = "",
    val reviewInFlight: Boolean = false,
    val confirmInFlight: Boolean = false,
    /** Done: what was marked sold. */
    val doneLine: String = "",
    val message: String? = null,
)

sealed interface SaleTagAnimalsEvent {
    data object Back : SaleTagAnimalsEvent
    data class SelectPark(val parkId: String) : SaleTagAnimalsEvent
    data class SelectPen(val penKey: String) : SaleTagAnimalsEvent
    data class SearchChanged(val text: String) : SaleTagAnimalsEvent
    data class ToggleAnimal(val goatId: String) : SaleTagAnimalsEvent
    data object LoadMore : SaleTagAnimalsEvent
    data object Review : SaleTagAnimalsEvent
    data object BackToPick : SaleTagAnimalsEvent
    data object Confirm : SaleTagAnimalsEvent
    data object Done : SaleTagAnimalsEvent
    data object DismissMessage : SaleTagAnimalsEvent
}
