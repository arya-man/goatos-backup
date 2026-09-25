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

/** One cleared animal on the review step with the weight typed for it (maintainer decision 2026-09-08). */
@Immutable
data class SaleReviewAnimalUi(
    val goatId: String,
    val tag: String,
    /** Backend pen name, VERBATIM. */
    val location: String,
    val weight: String,
    /** Blank until Confirm finds this weight missing or not a weight. */
    val error: String,
)

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
    // --- the sale's SOP steps (SALES SOP, 2026-09-19) ---
    /** The sale's workflow id once the backend has opened it; blank until then. */
    val stepsWorkflowId: String = "",
    /** "2 of 5 done" -- the backend card's counters, VERBATIM. Blank until read. */
    val stepsProgressLine: String = "",
    /** "Next: Record the animals being loaded" -- the card's next step title; blank when done. */
    val stepsNextLine: String = "",
    /** Whether the workflow read failed (offline); the card then says the steps are on the server. */
    val stepsUnavailable: Boolean = false,
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
    /**
     * The feed store's own question, raised by CLOSING a sale whose feed the store no longer shows
     * -- the server's sentence, naming the balance and what the sale takes. Blank when it has not
     * been asked. The desk answers it with [SaleDetailEvent.ConfirmStatusStock].
     */
    val stockConfirmMessage: String = "",
    val isRefreshing: Boolean = false,
    val isLoading: Boolean = true,
    /** Loaded, and this phone holds no copy of the sale (not synced yet, or not in a loaded page). */
    val notFound: Boolean = false,
    val message: String? = null,
)

sealed interface SaleDetailEvent {
    data object Refresh : SaleDetailEvent
    data object Back : SaleDetailEvent
    data object TagAnimals : SaleDetailEvent
    /** Open the sale's SOP steps (the shared workflow screen). */
    data class OpenSteps(val workflowId: String) : SaleDetailEvent
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
    /** Close the sale anyway, having read what the feed store holds. */
    data object ConfirmStatusStock : SaleDetailEvent
    /** Leave the sale as it is; the store's figure stands. */
    data object DismissStatusStock : SaleDetailEvent
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

/** Deal-level fields. What was SOLD lives on the lines ([SaleLineField]), not here. */
enum class SaleField {
    SALE_DATE, FARM,
    BUYER_VENDOR_ID, BUYER_NAME, BUYER_PLACE,
    ADVANCE_AMOUNT, STATUS, COMMENTS,
}

/** The fields of one product line of a sale. */
enum class SaleLineField { PRODUCT_TYPE, BREED, ANIMAL_COUNT, TOTAL_WEIGHT_KG, SALES_VALUE, QUANTITY, RATE_PER_UNIT }

/**
 * One product/breed line being entered (maintainer decision 2026-09-12: one sale carries sheep
 * AND goats of several breeds, each line with its own animals, weight and value). Values stay as
 * typed; the ViewModel parses on submit so a half-typed "12." survives recomposition.
 */
@Immutable
data class SaleLineDraftUi(
    /** Screen-local key; never sent. */
    val id: Int,
    val product: String = "",
    val breed: String = "",
    val animals: String = "",
    val weightKg: String = "",
    val value: String = "",
    /**
     * How much, at what rate, for a line priced by the unit -- feed by the kilogram, sheep tags by
     * number (maintainer instruction 2026-09-23). Blank on an animal line, which is sold as a lot.
     */
    val quantity: String = "",
    val rate: String = "",
    /**
     * What this line ASKS is the BACKEND's answer about the product, never this screen's reading
     * of its kind: a new kind of item must not be asked the wrong questions here while the web
     * asks the right ones.
     */
    val pricedPerUnit: Boolean = false,
    /** kg | number: decides whether the line asks for kilograms or for how many. */
    val unit: String = "",
    /** True for a feed line, whose variant list is the farm's own feed catalogue. */
    val isFeed: Boolean = false,
    /**
     * The variants of THIS line's product; empty until a product is picked. An item with no second
     * dimension -- manure, tags -- has exactly one, its own name, and the screen then asks nothing.
     */
    val breeds: List<VendorsOptionUi> = emptyList(),
    val errors: Map<SaleLineField, String> = emptyMap(),
) {
    /**
     * Whether the variant question has an answer worth asking for. An item whose only variant is
     * its own name is not a choice, so the screen states it instead of offering a list of one.
     */
    val variantIsItself: Boolean get() = breeds.size == 1 && breeds.first().value == product
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
    /** The product lines, in entry order; never empty once the options have loaded. */
    val lines: List<SaleLineDraftUi> = emptyList(),
    /** "₹2,21,000 · 19 animals · 540 kg" -- the running total of the lines, previewed on the phone; the ledger's figure is the backend's. */
    val totalLine: String = "",
    /** Sale value as a plain figure, for the money step. */
    val totalValueLine: String = "",
    val canAddLine: Boolean = true,
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
    /**
     * The server asked for this sale to be confirmed because it takes more feed than the store
     * shows (maintainer decision 2026-09-23). The sentence is BACKEND copy naming the farm, the
     * feed and both figures, rendered verbatim; the screen keys its behaviour on the server's
     * CODE, never on these words.
     *
     * It arrives LATE on the phone, not at the tap: a sale is queued and sent when there is a
     * network, so the answer comes back as a rejection of that queued row. The screen then offers
     * to record it anyway, which sends the same sale again with the answer attached.
     */
    val stockConfirmMessage: String = "",
    /** The write is durable (accepted, or queued for when the phone is online): the screen shows the banner briefly and closes. */
    val closeAfterSave: Boolean = false,
    val submitInFlight: Boolean = false,
    val message: String? = null,
)

sealed interface SaleCreateEvent {
    data class FieldChanged(val field: SaleField, val value: String) : SaleCreateEvent
    data class LineChanged(val lineId: Int, val field: SaleLineField, val value: String) : SaleCreateEvent
    data object AddLine : SaleCreateEvent
    data class RemoveLine(val lineId: Int) : SaleCreateEvent
    data class BuyerSearchChanged(val text: String) : SaleCreateEvent
    data class BuyerPicked(val vendorId: String) : SaleCreateEvent
    data object Next : SaleCreateEvent
    data object Previous : SaleCreateEvent
    data object Back : SaleCreateEvent
    data object Submit : SaleCreateEvent
    /** Send the same sale again, saying the feed store was checked. */
    data object ConfirmStockAndSubmit : SaleCreateEvent
    data object DismissStockConfirm : SaleCreateEvent
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
    /** Review: every cleared animal and its weight box; all must be filled before Confirm. */
    val reviewAnimals: List<SaleReviewAnimalUi> = emptyList(),
    val allWeighed: Boolean = false,
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
    data class WeightChanged(val goatId: String, val value: String) : SaleTagAnimalsEvent
    data object Confirm : SaleTagAnimalsEvent
    data object Done : SaleTagAnimalsEvent
    data object DismissMessage : SaleTagAnimalsEvent
}
