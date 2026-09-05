package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read refresh and queued write.

import androidx.compose.runtime.Immutable

/**
 * Pipeline and evidence on the phone (maintainer instruction 2026-09-04): the five panels the web's
 * /sales/config carries beside the deals ledger — buyer leads, farmer groups, market quotes, sold-tag
 * lists and weight checks. They are what the retired Sales DB sheet used to hold (maintainer
 * decision 2026-08-18), so entry lives here or nowhere.
 *
 * Every vocabulary is BACKEND-owned and rendered verbatim: the call-status options ride on the same
 * payload as the leads they label, and the farm and breed lists come from `GET /sales/options`.
 */

/** The five panels, in the order the hub lists them. */
enum class SalesPipelinePanel { BUYER_LEADS, FARMER_GROUPS, MARKET_QUOTE, SOLD_TAGS, WEIGHT_CHECK }

/** One row of the hub: what the panel records, and how many are on the board where that applies. */
@Immutable
data class SalesPipelineEntryUi(
    val panel: SalesPipelinePanel,
    val title: String,
    val subtitle: String,
    /** "12 leads" — blank for the panels that are entry-only (quote, tags, weight check). */
    val countLine: String = "",
)

@Immutable
data class SalesPipelineHubUiState(
    val entries: List<SalesPipelineEntryUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    /** Blank when the caller may record; the reason otherwise, rendered verbatim. */
    val disabledReason: String = "",
)

sealed interface SalesPipelineHubEvent {
    data object Refresh : SalesPipelineHubEvent
    data object Back : SalesPipelineHubEvent
    data class Open(val panel: SalesPipelinePanel) : SalesPipelineHubEvent
}

// ---------------------------------------------------------------------------------------------
// Lead boards (buyer leads and farmer groups share one screen shape)
// ---------------------------------------------------------------------------------------------

/** One lead on the board. Every line is composed from the server's own row. */
@Immutable
data class SalesLeadCardUi(
    /** Stable list key: the board's side and the lead id, so a buyer lead and a farmer group that
     *  happened to share an id could still never share a row. */
    val listKey: String,
    val leadId: String,
    /** Buyer name or farmer-group name, VERBATIM. */
    val title: String,
    /** "Hosur · Goat · Malai" / "Maize · Bhavani, Erode, TN" */
    val subtitle: String,
    /** The recorded date, farm-formatted; blank when the row carries none. */
    val metaLine: String,
    /** Backend call-status word, VERBATIM; blank when the row carries none. */
    val statusLabel: String,
    val statusTone: VendorsTone,
    /** The number to dial, exactly as recorded. Blank when the lead carries none. */
    val phoneNumber: String = "",
    /** Everything the server knows about this lead, for the expanded row. */
    val details: List<VendorsDetailRowUi> = emptyList(),
    /**
     * The row's own values keyed by form field, so Change opens on exactly what is recorded. It
     * rides on the card because the board is a paged read: nothing else on the phone holds the
     * lead the finger just landed on, and keeping a side map of every lead ever scrolled past
     * would be the growing in-heap accumulator the memory rule bans.
     */
    val editValues: Map<String, String> = emptyMap(),
)

/** Fields of the buyer-lead form. */
enum class SalesBuyerLeadField { RECORDED_DATE, FARM, BUYER_NAME, BUYER_PLACE, ANIMAL_TYPE, BREED, PHONE_NUMBER, CALL_STATUS }

/** Fields of the farmer-group form. */
enum class SalesFpoLeadField { FPO_NAME, CROPS, DISTRICT, TALUK, STATE, PHONE_NUMBER, CALL_STATUS }

@Immutable
data class SalesLeadBoardUiState(
    val title: String = "",
    /** The call-status vocabulary, backend-owned. */
    val statusOptions: List<VendorsOptionUi> = emptyList(),
    /** Farms and the product/breed vocabularies, for the buyer-lead form only. */
    val farms: List<VendorsOptionUi> = emptyList(),
    val animalTypes: List<VendorsOptionUi> = emptyList(),
    val breeds: List<VendorsOptionUi> = emptyList(),
    val today: String = "",
    /** Non-null while the add or edit form is open. */
    val form: SalesLeadFormUi? = null,
    /** Non-blank while the status picker is open on one lead. */
    val statusPickerLeadId: String = "",
    /** Non-blank while one lead is opened out in place, showing everything known about it. */
    val expandedLeadId: String = "",
    /** What is typed in the search box, echoed back so the field renders what was typed. */
    val search: String = "",
    /** Status chips, "All" first; the selected one is the filter in force. */
    val filters: List<VendorsFilterUi> = emptyList(),
    /** What the search box invites: the fields a search actually matches. */
    val searchPlaceholder: String = "",
    /** Shown in place of a phone number on a lead that carries none. */
    val noPhoneMessage: String = "",
    val countLine: String = "",
    val emptyMessage: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val canRecord: Boolean = true,
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
)

/**
 * The lead form's draft. Keys are the field enums' names, so ONE state serves both boards and both
 * verbs. [editingLeadId] is blank while a NEW lead is being entered and carries the lead id while
 * an existing one is being changed -- a change REPLACES every editable field, so the form opens
 * pre-filled with what the row already holds.
 */
@Immutable
data class SalesLeadFormUi(
    val values: Map<String, String> = emptyMap(),
    val fieldErrors: Map<String, String> = emptyMap(),
    val inFlight: Boolean = false,
    val editingLeadId: String = "",
)

sealed interface SalesLeadBoardEvent {
    data object Refresh : SalesLeadBoardEvent
    data object Back : SalesLeadBoardEvent
    data object OpenForm : SalesLeadBoardEvent
    data object CloseForm : SalesLeadBoardEvent
    data class FieldChanged(val field: String, val value: String) : SalesLeadBoardEvent
    data object Submit : SalesLeadBoardEvent
    /** Open the status picker on one lead; blank closes it. */
    data class OpenStatusPicker(val leadId: String) : SalesLeadBoardEvent
    data class ChangeStatus(val leadId: String, val status: String) : SalesLeadBoardEvent
    data class SearchChanged(val text: String) : SalesLeadBoardEvent
    data class SelectStatus(val key: String) : SalesLeadBoardEvent
    /** Open one lead out in place; the same lead again closes it. */
    data class ToggleExpanded(val leadId: String) : SalesLeadBoardEvent
    /** Change this lead: the form opens on what the row already holds, which is why the CARD
     *  travels with the event -- a paged board holds no other copy of the row that was tapped. */
    data class EditLead(val card: SalesLeadCardUi) : SalesLeadBoardEvent
    /** The number was tapped; the screen hands it to the dialler. */
    data class CallLead(val leadId: String) : SalesLeadBoardEvent
}

// ---------------------------------------------------------------------------------------------
// Evidence forms: market quote, sold-tag list, weight check
// ---------------------------------------------------------------------------------------------

enum class SalesQuoteField { MARKET, CATEGORY, BREED, SOURCE, EX_FARM_RATE, TRANSPORT_RATE, LANDING_COST_PER_KG, MARKET_PRICE_PER_KG }

enum class SalesWeightCheckField { TAG_NUMBER, BOOK_WEIGHT_KG, VIDEO_WEIGHT_KG, FARM_BORN }

/** One line of a sold-tag list being entered. */
@Immutable
data class SalesSoldTagRowUi(
    /** A stable key for the repeated row, minted when the row is added. */
    val rowKey: String,
    val animalLabel: String = "",
    val tagNumber: String = "",
    val weightKg: String = "",
    val error: String = "",
)

/**
 * One state for all three evidence forms: they differ only in which map is filled. Keeping them
 * together means one screen, one submit path and one banner rather than three near-copies.
 */
@Immutable
data class SalesEvidenceUiState(
    val title: String = "",
    val subtitle: String = "",
    val values: Map<String, String> = emptyMap(),
    val fieldErrors: Map<String, String> = emptyMap(),
    /** Sold-tag rows; empty for the other two forms. */
    val tagRows: List<SalesSoldTagRowUi> = emptyList(),
    val farms: List<VendorsOptionUi> = emptyList(),
    val breeds: List<VendorsOptionUi> = emptyList(),
    val animalTypes: List<VendorsOptionUi> = emptyList(),
    val submitInFlight: Boolean = false,
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    /** The write is durable: the banner shows briefly, then the screen closes itself. */
    val closeAfterSave: Boolean = false,
    val canRecord: Boolean = true,
)

sealed interface SalesEvidenceEvent {
    data object Back : SalesEvidenceEvent
    data class FieldChanged(val field: String, val value: String) : SalesEvidenceEvent
    data class TagRowChanged(val rowKey: String, val field: String, val value: String) : SalesEvidenceEvent
    data object AddTagRow : SalesEvidenceEvent
    data class RemoveTagRow(val rowKey: String) : SalesEvidenceEvent
    data object Submit : SalesEvidenceEvent
}
