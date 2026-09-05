package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Vendors module (maintainer decision 2026-09-03): the procurement vendor register and the feed
 * purchase ledger on the phone, view and add, for the CXO and the procurement desk.
 *
 * Every visible sentence is BACKEND-OWNED and rendered verbatim: `display_name`, `status_label`,
 * `location_display`, `capacity_display`. The phone stores catalog VALUES and renders catalog
 * LABELS; it composes none of its own.
 *
 * Wire contracts of record: backend/internal/procurement/adapters/http/vendor_payloads.go and
 * feed_purchase_payloads.go. The vendor's payment instruments are deliberately NOT modelled here:
 * the phone never asks for them and `ignoreUnknownKeys` drops them if a caller with finance read
 * ever receives them, so no bank detail is cached on a device.
 */
@Serializable
data class VendorDto(
    @SerialName("vendor_id") val vendorId: String,
    @SerialName("record_type") val recordType: String = "",
    @SerialName("business_name") val businessName: String = "",
    /** Backend-composed "Business - Contact", VERBATIM. */
    @SerialName("display_name") val displayName: String = "",
    @SerialName("contact_person_name") val contactPersonName: String? = null,
    @SerialName("phone_number") val phoneNumber: String? = null,
    @SerialName("breed") val breed: String? = null,
    @SerialName("feed") val feed: String? = null,
    /** `active` | `inactive` | `negotiating` | `banned`. */
    @SerialName("status") val status: String = "",
    /** Backend-owned status copy, VERBATIM. */
    @SerialName("status_label") val statusLabel: String = "",
    @SerialName("filtered_stock") val filteredStock: Int? = null,
    /** Decimal as a string; money never round-trips through a float. */
    @SerialName("price_per_goat") val pricePerGoat: String? = null,
    @SerialName("ready_to_filtered") val readyToFiltered: String? = null,
    @SerialName("eta_after_order_days") val etaAfterOrderDays: Int? = null,
    @SerialName("details") val details: String? = null,
    @SerialName("state") val state: String = "",
    @SerialName("city") val city: String? = null,
    /** Backend-composed "City, State", VERBATIM. */
    @SerialName("location_display") val locationDisplay: String = "",
    @SerialName("comments") val comments: String? = null,
    /** Decimal as a string; null when no capacity is recorded. */
    @SerialName("capacity_quantity") val capacityQuantity: String? = null,
    /** Catalog VALUE (kg, tonnes, ...); render [capacityDisplay] instead. */
    @SerialName("capacity_unit") val capacityUnit: String? = null,
    /** Catalog VALUE (per_week, ...); render [capacityDisplay] instead. */
    @SerialName("supply_frequency") val supplyFrequency: String? = null,
    /** Backend-composed "5,000 kg · Every 2 weeks", VERBATIM; empty when nothing is recorded. */
    @SerialName("capacity_display") val capacityDisplay: String = "",
    /** A completed `audio` proof id, or null when no voice note was recorded. */
    @SerialName("voice_note_proof_ref") val voiceNoteProofRef: String? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    @SerialName("row_version") val rowVersion: Long = 0,
)

@Serializable
data class VendorPageDto(
    @SerialName("vendors") val vendors: List<VendorDto> = emptyList(),
    /** WHOLE-FILTER count, never the page length. */
    @SerialName("total") val total: Int = 0,
    @SerialName("limit") val limit: Int = 0,
    @SerialName("offset") val offset: Int = 0,
)

@Serializable
data class VendorCatalogEntryDto(
    @SerialName("value") val value: String,
    @SerialName("label") val label: String,
    /** False = retired: still rendered on a vendor that carries it, never offered for a new one. */
    @SerialName("is_active") val isActive: Boolean = true,
)

/** The register's business-managed vocabularies. Values are stored, labels are rendered. */
@Serializable
data class VendorCatalogDto(
    @SerialName("record_types") val recordTypes: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("breeds") val breeds: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("states") val states: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("cities") val cities: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("statuses") val statuses: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("feeds") val feeds: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("capacity_units") val capacityUnits: List<VendorCatalogEntryDto> = emptyList(),
    @SerialName("supply_frequencies") val supplyFrequencies: List<VendorCatalogEntryDto> = emptyList(),
)

/**
 * Create-vendor body. A REPLACE on the backend: every optional text field is sent as "" when
 * blank, exactly as the web form does, so there is no patch-vs-replace ambiguity. Finance fields
 * are never entered on the phone and are sent blank.
 */
@Serializable
data class VendorWriteDto(
    @SerialName("record_type") val recordType: String,
    @SerialName("business_name") val businessName: String,
    @SerialName("contact_person_name") val contactPersonName: String = "",
    @SerialName("phone_number") val phoneNumber: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("feed") val feed: String = "",
    @SerialName("status") val status: String = "active",
    @SerialName("filtered_stock") val filteredStock: Int? = null,
    @SerialName("price_per_goat") val pricePerGoat: String? = null,
    @SerialName("ready_to_filtered") val readyToFiltered: String = "",
    @SerialName("eta_after_order_days") val etaAfterOrderDays: Int? = null,
    @SerialName("details") val details: String = "",
    @SerialName("state") val state: String,
    @SerialName("city") val city: String = "",
    @SerialName("bank_name") val bankName: String = "",
    @SerialName("account_no") val accountNo: String = "",
    @SerialName("ifsc_code") val ifscCode: String = "",
    @SerialName("upi_id") val upiId: String = "",
    @SerialName("pan_number") val panNumber: String = "",
    @SerialName("comments") val comments: String = "",
    @SerialName("capacity_quantity") val capacityQuantity: String? = null,
    @SerialName("capacity_unit") val capacityUnit: String = "",
    @SerialName("supply_frequency") val supplyFrequency: String = "",
    /** Server proof id of the uploaded audio note; the sync engine fills it at dispatch time. */
    @SerialName("voice_note_proof_ref") val voiceNoteProofRef: String = "",
    @SerialName("row_version") val rowVersion: Long = 0,
)

/** One purchased feed load (the ledger row). Delivery state per the 2026-09-03 decision. */
@Serializable
data class FeedPurchaseDto(
    @SerialName("feed_purchase_id") val feedPurchaseId: String,
    /** Business DATE (YYYY-MM-DD), never an instant. */
    @SerialName("purchase_date") val purchaseDate: String = "",
    @SerialName("farm") val farm: String = "",
    @SerialName("feed_item") val feedItem: String = "",
    @SerialName("batch_no") val batchNo: Int = 0,
    @SerialName("quantity_kg") val quantityKg: Double = 0.0,
    @SerialName("feed_cost") val feedCost: Double? = null,
    @SerialName("transport_cost") val transportCost: Double? = null,
    @SerialName("loading_cost") val loadingCost: Double? = null,
    @SerialName("unloading_cost") val unloadingCost: Double? = null,
    @SerialName("total_cost") val totalCost: Double? = null,
    @SerialName("per_kg_cost") val perKgCost: Double? = null,
    @SerialName("vendor") val vendor: String = "",
    @SerialName("payment_released") val paymentReleased: Double? = null,
    /** `Paid` | `Pending`. */
    @SerialName("payment_status") val paymentStatus: String = "",
    /** BACKEND-derived money still owed; null while the landed cost is unknown. */
    @SerialName("payment_balance") val paymentBalance: Double? = null,
    /** `purchased` (on the road) | `reached`. */
    @SerialName("delivery_status") val deliveryStatus: String = "",
    @SerialName("reached_on") val reachedOn: String? = null,
    @SerialName("reached_weight_kg") val reachedWeightKg: Double? = null,
    /** BACKEND-derived kg counted as stock; null while on the road. */
    @SerialName("stock_kg") val stockKg: Double? = null,
    @SerialName("entry_source") val entrySource: String = "",
    @SerialName("created_at") val createdAt: String = "",
    /** The instalments paid against this load, oldest first, as the server returned them. */
    @SerialName("payments") val payments: List<FeedPurchasePaymentDto> = emptyList(),
)

/** One instalment paid against a purchased load. */
@Serializable
data class FeedPurchasePaymentDto(
    @SerialName("payment_id") val paymentId: String,
    @SerialName("paid_on") val paidOn: String = "",
    @SerialName("amount_rupees") val amountRupees: Double = 0.0,
    @SerialName("note") val note: String = "",
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class FeedPurchasePageDto(
    @SerialName("purchases") val purchases: List<FeedPurchaseDto> = emptyList(),
    /** Whole-filter aggregates, never page sums. */
    @SerialName("total") val total: Int = 0,
    @SerialName("quantity_kg") val quantityKg: Double = 0.0,
    @SerialName("spend_rupees") val spendRupees: Double = 0.0,
    @SerialName("limit") val limit: Int = 0,
    @SerialName("offset") val offset: Int = 0,
)

@Serializable
data class DeliveryStatusOptionDto(
    @SerialName("key") val key: String,
    /** Backend-owned farm label, rendered VERBATIM. */
    @SerialName("label") val label: String,
)

@Serializable
data class FeedItemOptionDto(
    /** Normalized matching key; never display copy. */
    @SerialName("key") val key: String,
    /** The catalog label to render AND to send back as `feed_item`. */
    @SerialName("label") val label: String,
)

/** The record-purchase form's backend-owned vocabulary. */
@Serializable
data class FeedPurchaseOptionsDto(
    @SerialName("farms") val farms: List<String> = emptyList(),
    /** The ACTIVE feed catalog — exactly the set the write accepts. */
    @SerialName("feed_items") val feedItems: List<FeedItemOptionDto> = emptyList(),
    @SerialName("payment_statuses") val paymentStatuses: List<String> = emptyList(),
    /** The closed delivery vocabulary with BACKEND labels ("In transit", "Delivered"), in order. */
    @SerialName("delivery_statuses") val deliveryStatuses: List<DeliveryStatusOptionDto> = emptyList(),
    /** Suppliers already bought from, most recent first; a suggestion list, not a closed set. */
    @SerialName("vendors") val vendors: List<String> = emptyList(),
)

/**
 * Record-purchase body. Optional money fields are null when not entered — a zero transport cost is
 * a real recorded fact and a blank box must not invent it. `reached_on` absent = still on the road.
 */
@Serializable
data class FeedPurchaseWriteDto(
    @SerialName("purchase_date") val purchaseDate: String,
    @SerialName("farm") val farm: String,
    @SerialName("feed_item") val feedItem: String,
    @SerialName("batch_no") val batchNo: Int? = null,
    @SerialName("quantity_kg") val quantityKg: Double,
    @SerialName("feed_cost") val feedCost: Double? = null,
    @SerialName("transport_cost") val transportCost: Double? = null,
    @SerialName("loading_cost") val loadingCost: Double? = null,
    @SerialName("unloading_cost") val unloadingCost: Double? = null,
    @SerialName("total_cost") val totalCost: Double? = null,
    @SerialName("vendor") val vendor: String,
    @SerialName("payment_released") val paymentReleased: Double? = null,
    @SerialName("payment_status") val paymentStatus: String,
    @SerialName("reached_on") val reachedOn: String? = null,
    @SerialName("reached_weight_kg") val reachedWeightKg: Double? = null,
)

// ---------------------------------------------------------------------------------------------
// Changing a recorded load (maintainer instruction 2026-09-04)
// ---------------------------------------------------------------------------------------------
//
// The four writes the web's feed-purchase drawer carries. Each returns the WHOLE updated load --
// `payment_balance`, `per_kg_cost` and `stock_kg` recomputed by the server -- so the phone
// persists that row and derives no money or stock figure of its own.

/** `POST /procurement/feed-purchases/{id}/payments`. The Idempotency-Key is REQUIRED. */
@Serializable
data class FeedPurchasePaymentWriteDto(
    @SerialName("paid_on") val paidOn: String,
    @SerialName("amount_rupees") val amountRupees: Double,
    @SerialName("note") val note: String = "",
)

/** `PUT /procurement/feed-purchases/{id}/payment-status`: `Paid` or `Pending`. */
@Serializable
data class FeedPurchaseStatusWriteDto(
    @SerialName("payment_status") val paymentStatus: String,
)

/**
 * `PUT /procurement/feed-purchases/{id}`: the VALUES of an already-recorded load. Farm, feed and
 * batch are deliberately absent -- identity is immutable, so a mistyped feed is a new row, not an
 * edit of the one the stock cards have already counted.
 */
@Serializable
data class FeedPurchaseEditDto(
    @SerialName("purchase_date") val purchaseDate: String,
    @SerialName("quantity_kg") val quantityKg: Double,
    @SerialName("feed_cost") val feedCost: Double? = null,
    @SerialName("transport_cost") val transportCost: Double? = null,
    @SerialName("loading_cost") val loadingCost: Double? = null,
    @SerialName("unloading_cost") val unloadingCost: Double? = null,
    @SerialName("total_cost") val totalCost: Double? = null,
    @SerialName("vendor") val vendor: String,
)

/** `PUT /procurement/feed-purchases/{id}/delivery`: the truck came in. */
@Serializable
data class FeedPurchaseDeliveryWriteDto(
    @SerialName("reached_on") val reachedOn: String,
    /** Absent when nobody weighed it; the buying weight then stands as the stock figure. */
    @SerialName("reached_weight_kg") val reachedWeightKg: Double? = null,
)
