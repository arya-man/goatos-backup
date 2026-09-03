package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read refresh, capture, and queued write.

import androidx.compose.runtime.Immutable

/**
 * UI models for the Vendors module (maintainer decision 2026-09-03): the procurement vendor
 * register and the feed purchase ledger, view and add, for the CXO and the procurement desk.
 *
 * Every business sentence a card or detail row shows is BACKEND-OWNED and carried through
 * verbatim: `display_name`, `status_label`, `location_display`, `capacity_display`, the delivery
 * and payment words. The renderer composes none of them. Form FIELD labels are app chrome, the
 * same as every other phone entry form (see feature-counts).
 */

/** Visual tone of a chip; the mapping from a backend value to a tone is the ViewModel's, not the screen's. */
enum class VendorsTone { NEUTRAL, OK, WARN, DANGER, INFO }

/** One backend-owned option (a catalog entry: stored VALUE, rendered LABEL). */
@Immutable
data class VendorsOptionUi(val value: String, val label: String)

/** One filter chip; label is backend-owned, the screen sends back only [key]. */
@Immutable
data class VendorsFilterUi(val key: String, val label: String, val selected: Boolean)

// ---------------------------------------------------------------------------------------------
// Vendor register
// ---------------------------------------------------------------------------------------------

@Immutable
data class VendorCardUi(
    /** Stable list key — the vendor id IS this list's grain. */
    val listKey: String,
    val vendorId: String,
    /** Backend-composed "Business - Contact", VERBATIM. */
    val name: String,
    /** Record type · location, both backend words, joined for the subtitle line. */
    val typeLine: String,
    /** Backend-composed capacity line ("5,000 kg · Every 2 weeks"); blank when not recorded. */
    val capacityLine: String,
    /** Backend-owned status copy, VERBATIM. */
    val statusLabel: String,
    val statusTone: VendorsTone,
    val hasVoiceNote: Boolean,
)

@Immutable
data class VendorsListUiState(
    /** The tab's title — the backend nav label passed through. */
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    /** The typed search text (the screen's own input state, echoed back). */
    val search: String = "",
    /** Status chips: "All" plus the backend catalog statuses, labels verbatim. */
    val filters: List<VendorsFilterUi> = emptyList(),
    /** Whole-filter count line from the last refresh ("306 vendors"); blank before the first. */
    val countLine: String = "",
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    /** Whether the Add button is offered (the caller holds vendor write). */
    val canAdd: Boolean = false,
)

sealed interface VendorsListEvent {
    data object Refresh : VendorsListEvent
    data class SearchChanged(val text: String) : VendorsListEvent
    data class SelectStatus(val key: String) : VendorsListEvent
    data class OpenVendor(val vendorId: String) : VendorsListEvent
    data object AddVendor : VendorsListEvent
}

/** One label/value row of a detail screen. */
@Immutable
data class VendorsDetailRowUi(val label: String, val value: String)

/** A titled group of rows. */
@Immutable
data class VendorsDetailSectionUi(val title: String, val rows: List<VendorsDetailRowUi>)

/** Playback state of a vendor's voice note. */
enum class VoiceNotePlayback { NONE, IDLE, LOADING, PLAYING, FAILED }

@Immutable
data class VendorDetailUiState(
    val title: String = "",
    val subtitle: String = "",
    val statusLabel: String = "",
    val statusTone: VendorsTone = VendorsTone.NEUTRAL,
    val sections: List<VendorsDetailSectionUi> = emptyList(),
    val voiceNote: VoiceNotePlayback = VoiceNotePlayback.NONE,
    /** Resolved signed URL for the note's player; blank until loaded. */
    val voiceNoteUrl: String = "",
    val isRefreshing: Boolean = false,
    /** True while nothing is cached and the first read has not landed. */
    val isLoading: Boolean = true,
    val message: String? = null,
)

sealed interface VendorDetailEvent {
    data object Refresh : VendorDetailEvent
    data object Back : VendorDetailEvent
    data object PlayVoiceNote : VendorDetailEvent
    data object StopVoiceNote : VendorDetailEvent
    data object DismissMessage : VendorDetailEvent
}

/** Fields of the add-vendor wizard. Drafts are STRINGS so a blank number stays blank, never 0. */
enum class VendorField {
    BUSINESS_NAME, RECORD_TYPE, CONTACT_PERSON, PHONE,
    STATE, CITY, STATUS,
    CAPACITY_QUANTITY, CAPACITY_UNIT, SUPPLY_FREQUENCY, FEED, BREED, PRICE_PER_GOAT, ETA_DAYS,
    NOTE,
}

/** What the wizard's voice-note slot is doing. */
enum class VoiceNoteSlotState { EMPTY, WORKING, RECORDED, FAILED }

/** Where the write stands: nothing yet, durable in the outbox, landed on the server, or refused. */
enum class VendorsWriteStatus { IDLE, QUEUED, SYNCED, FAILED }

@Immutable
data class VendorCreateUiState(
    val step: Int = 0,
    val stepCount: Int = 3,
    /** Draft values keyed by field; catalog-backed fields hold the stored VALUE. */
    val values: Map<VendorField, String> = emptyMap(),
    /** Backend catalog vocabularies, labels verbatim. */
    val recordTypes: List<VendorsOptionUi> = emptyList(),
    val states: List<VendorsOptionUi> = emptyList(),
    val statuses: List<VendorsOptionUi> = emptyList(),
    val capacityUnits: List<VendorsOptionUi> = emptyList(),
    val supplyFrequencies: List<VendorsOptionUi> = emptyList(),
    val feeds: List<VendorsOptionUi> = emptyList(),
    val breeds: List<VendorsOptionUi> = emptyList(),
    /** Per-field refusal shown under the box; empty when the step is valid. */
    val fieldErrors: Map<VendorField, String> = emptyMap(),
    /** Backend-composed-style summary of what is chosen so far, for the sticky bar. */
    val contextLine: String = "",
    val voiceNote: VoiceNoteSlotState = VoiceNoteSlotState.EMPTY,
    /** Recorded length, for the slot's label ("0:42"); blank until recorded. */
    val voiceNoteLength: String = "",
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    val submitInFlight: Boolean = false,
    val message: String? = null,
)

sealed interface VendorCreateEvent {
    data class FieldChanged(val field: VendorField, val value: String) : VendorCreateEvent
    data object Next : VendorCreateEvent
    data object Previous : VendorCreateEvent
    data object Back : VendorCreateEvent
    data object RecordVoiceNote : VendorCreateEvent
    data object RemoveVoiceNote : VendorCreateEvent
    data object Submit : VendorCreateEvent
    data object RecordAnother : VendorCreateEvent
    data object DismissMessage : VendorCreateEvent
}

// ---------------------------------------------------------------------------------------------
// Feed purchases
// ---------------------------------------------------------------------------------------------

@Immutable
data class FeedPurchaseCardUi(
    val listKey: String,
    val purchaseId: String,
    /** The feed's catalog label, VERBATIM. */
    val feedItem: String,
    /** "CBE · Load 328" */
    val loadLine: String,
    /** "1,000 kg · ₹23,000" — quantity and landed cost, each formatted by the ViewModel. */
    val quantityLine: String,
    /** "Bought 01-09-2026 · QA Vendor" */
    val metaLine: String,
    /** Backend-owned delivery word ("On the road" / "Reached"), VERBATIM. */
    val deliveryLabel: String,
    val deliveryTone: VendorsTone,
    /** Backend-owned payment word, VERBATIM. */
    val paymentLabel: String,
    val paymentTone: VendorsTone,
)

@Immutable
data class FeedPurchasesListUiState(
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    /** Delivery chips: "All" plus the backend delivery vocabulary, labels verbatim. */
    val filters: List<VendorsFilterUi> = emptyList(),
    /** Whole-filter totals line ("228 loads · 7,85,714 kg · ₹1,60,23,173"); blank before the first refresh. */
    val totalsLine: String = "",
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    val canAdd: Boolean = false,
)

sealed interface FeedPurchasesListEvent {
    data object Refresh : FeedPurchasesListEvent
    data class SelectDelivery(val key: String) : FeedPurchasesListEvent
    data class OpenPurchase(val purchaseId: String) : FeedPurchasesListEvent
    data object AddPurchase : FeedPurchasesListEvent
}

@Immutable
data class FeedPurchaseDetailUiState(
    val title: String = "",
    val subtitle: String = "",
    val deliveryLabel: String = "",
    val deliveryTone: VendorsTone = VendorsTone.NEUTRAL,
    val sections: List<VendorsDetailSectionUi> = emptyList(),
    /** Backend-owned note for a load still on the road; blank once reached. */
    val deliveryNote: String = "",
    val isRefreshing: Boolean = false,
    val isLoading: Boolean = true,
)

sealed interface FeedPurchaseDetailEvent {
    data object Refresh : FeedPurchaseDetailEvent
    data object Back : FeedPurchaseDetailEvent
}

enum class PurchaseField {
    PURCHASE_DATE, FARM, FEED_ITEM, VENDOR, QUANTITY_KG, REACHED_ON, REACHED_WEIGHT_KG,
    FEED_COST, TRANSPORT_COST, LOADING_COST, UNLOADING_COST, TOTAL_COST, PAYMENT_STATUS, PAYMENT_RELEASED,
}

@Immutable
data class FeedPurchaseCreateUiState(
    val step: Int = 0,
    val stepCount: Int = 2,
    val values: Map<PurchaseField, String> = emptyMap(),
    val farms: List<VendorsOptionUi> = emptyList(),
    /** The ACTIVE feed catalog — value = label, exactly what the write sends back. */
    val feedItems: List<VendorsOptionUi> = emptyList(),
    val paymentStatuses: List<VendorsOptionUi> = emptyList(),
    /** Vendors seen on the ledger, for the vendor picker; free text remains allowed. */
    val vendorSuggestions: List<String> = emptyList(),
    val fieldErrors: Map<PurchaseField, String> = emptyMap(),
    val contextLine: String = "",
    /** Today (YYYY-MM-DD) as the date pickers' ceiling; every date here has already happened. */
    val today: String = "",
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    val submitInFlight: Boolean = false,
    val message: String? = null,
)

sealed interface FeedPurchaseCreateEvent {
    data class FieldChanged(val field: PurchaseField, val value: String) : FeedPurchaseCreateEvent
    data object Next : FeedPurchaseCreateEvent
    data object Previous : FeedPurchaseCreateEvent
    data object Back : FeedPurchaseCreateEvent
    data object Submit : FeedPurchaseCreateEvent
    data object RecordAnother : FeedPurchaseCreateEvent
    data object DismissMessage : FeedPurchaseCreateEvent
}
