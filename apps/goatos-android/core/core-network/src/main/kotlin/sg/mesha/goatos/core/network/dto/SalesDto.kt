package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Sales on the phone (maintainer instruction 2026-09-04): the Procurement module's Sales tab —
 * the sales done, recording a sale, and tagging the animals it is made of — over the SAME routes
 * the web's /sales/config drawers use.
 *
 * Wire contracts of record: backend/internal/sales/adapters/http/sales_payloads.go (deals,
 * options) and backend/internal/identity/adapters/http/sale_allocation_handler.go (tagging).
 * `payment_balance`, `operational_location_display` and `blocked_reason` are BACKEND-composed and
 * rendered verbatim; the phone computes no money figure and no location string of its own.
 */
@Serializable
data class SalesDealPaymentDto(
    @SerialName("payment_id") val paymentId: String,
    @SerialName("received_on") val receivedOn: String = "",
    @SerialName("amount_rupees") val amountRupees: Double = 0.0,
    @SerialName("note") val note: String? = null,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class SalesDealDto(
    @SerialName("deal_id") val dealId: String,
    @SerialName("sale_date") val saleDate: String = "",
    @SerialName("farm") val farm: String = "",
    @SerialName("buyer_name") val buyerName: String = "",
    @SerialName("buyer_place") val buyerPlace: String? = null,
    @SerialName("buyer_vendor_id") val buyerVendorId: String? = null,
    @SerialName("product_type") val productType: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("animal_count") val animalCount: Double? = null,
    @SerialName("male_count") val maleCount: Double? = null,
    @SerialName("female_count") val femaleCount: Double? = null,
    @SerialName("total_weight_kg") val totalWeightKg: Double? = null,
    @SerialName("advance_amount") val advanceAmount: Double? = null,
    @SerialName("sales_value") val salesValue: Double = 0.0,
    @SerialName("payment_received") val paymentReceived: Double? = null,
    /** BACKEND-derived: value minus received, floored at zero. */
    @SerialName("payment_balance") val paymentBalance: Double = 0.0,
    @SerialName("payments") val payments: List<SalesDealPaymentDto> = emptyList(),
    @SerialName("status") val status: String = "",
    @SerialName("feedback") val feedback: String? = null,
    @SerialName("comments") val comments: String? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
)

@Serializable
data class SalesDealPageDto(
    @SerialName("deals") val deals: List<SalesDealDto> = emptyList(),
    /** WHOLE-FILTER count, never the page length. */
    @SerialName("total") val total: Int = 0,
    @SerialName("limit") val limit: Int = 0,
    @SerialName("offset") val offset: Int = 0,
)

/** `POST /sales/deals`. Unknown keys are a 400 on the server, so this carries exactly its fields. */
@Serializable
data class SalesDealWriteDto(
    @SerialName("sale_date") val saleDate: String,
    @SerialName("farm") val farm: String,
    @SerialName("product_type") val productType: String,
    @SerialName("breed") val breed: String,
    @SerialName("buyer_name") val buyerName: String,
    @SerialName("buyer_place") val buyerPlace: String = "",
    @SerialName("buyer_vendor_id") val buyerVendorId: String,
    @SerialName("animal_count") val animalCount: Double? = null,
    @SerialName("male_count") val maleCount: Double? = null,
    @SerialName("female_count") val femaleCount: Double? = null,
    @SerialName("total_weight_kg") val totalWeightKg: Double? = null,
    @SerialName("sales_value") val salesValue: Double,
    @SerialName("advance_amount") val advanceAmount: Double? = null,
    @SerialName("comments") val comments: String = "",
    @SerialName("status") val status: String = "",
)

@Serializable
data class SalesStatusOptionDto(
    @SerialName("key") val key: String,
    @SerialName("label") val label: String,
    /** Chip tone every surface renders this status in: ok / dng / info / warn. */
    @SerialName("tone") val tone: String = "",
)

/** `GET /sales/options`: the record-sale vocabularies, backend-owned. */
@Serializable
data class SalesOptionsDto(
    @SerialName("farms") val farms: List<String> = emptyList(),
    @SerialName("product_types") val productTypes: List<String> = emptyList(),
    /** Breeds keyed by product type, in offer order. */
    @SerialName("breeds") val breeds: Map<String, List<String>> = emptyMap(),
    @SerialName("statuses") val statuses: List<SalesStatusOptionDto> = emptyList(),
    @SerialName("default_status") val defaultStatus: String = "",
    @SerialName("max_sale_date_days_ahead") val maxSaleDateDaysAhead: Int = 60,
)

/** One selectable buyer from `GET /procurement/vendor-options` (identity and place only). */
@Serializable
data class VendorOptionDto(
    @SerialName("vendor_id") val vendorId: String,
    @SerialName("business_name") val businessName: String = "",
    @SerialName("record_type") val recordType: String = "",
    @SerialName("city") val city: String = "",
    @SerialName("state") val state: String = "",
)

@Serializable
data class VendorOptionsDto(
    @SerialName("vendors") val vendors: List<VendorOptionDto> = emptyList(),
    /** The ACTIVE register is larger than one bounded read; a missing buyer may still exist. */
    @SerialName("truncated") val truncated: Boolean = false,
)

// ---------------------------------------------------------------------------------------------
// Tagging animals to a sale (sale allocation)
// ---------------------------------------------------------------------------------------------

@Serializable
data class SaleLocationParkDto(
    @SerialName("park_id") val parkId: String,
    /** The park SHORT CODE (CBE, CPT) when it has one, else the full name. */
    @SerialName("label") val label: String = "",
)

/** ONE selectable operational location: a pen if the shed is subdivided, else the shed. */
@Serializable
data class SaleLocationEntryDto(
    @SerialName("shed_id") val shedId: String,
    @SerialName("park_id") val parkId: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    /** BACKEND-composed pen name, VERBATIM. */
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
)

@Serializable
data class SaleLocationsDto(
    @SerialName("parks") val parks: List<SaleLocationParkDto> = emptyList(),
    @SerialName("locations") val locations: List<SaleLocationEntryDto> = emptyList(),
)

@Serializable
data class SaleCandidateDto(
    @SerialName("goat_id") val goatId: String,
    @SerialName("display_id") val displayId: String = "",
    @SerialName("tag_number") val tagNumber: String = "",
    @SerialName("secondary_tag_number") val secondaryTagNumber: String = "",
    @SerialName("park_id") val parkId: String = "",
    @SerialName("park_name") val parkName: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("sex") val sex: String = "",
    @SerialName("row_version") val rowVersion: Long = 0,
    @SerialName("sellable") val sellable: Boolean = true,
    @SerialName("blocker") val blocker: String = "",
    /** BACKEND farm copy for why this animal cannot be sold, VERBATIM. */
    @SerialName("blocked_reason") val blockedReason: String = "",
)

@Serializable
data class SaleCandidatePageDto(
    @SerialName("candidates") val candidates: List<SaleCandidateDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
data class SaleAllocationRequestDto(
    @SerialName("sales_deal_id") val salesDealId: String,
    @SerialName("goat_ids") val goatIds: List<String>,
)

@Serializable
data class SaleShedGroupDto(
    @SerialName("park_name") val parkName: String = "",
    @SerialName("shed_id") val shedId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("animals") val animals: Int = 0,
    @SerialName("tag_numbers") val tagNumbers: List<String> = emptyList(),
)

@Serializable
data class SalePreviewDto(
    @SerialName("sales_deal_id") val salesDealId: String = "",
    @SerialName("declared_animal_count") val declaredAnimalCount: Int = 0,
    @SerialName("already_tagged") val alreadyTagged: Int = 0,
    @SerialName("complete") val complete: Boolean = false,
    @SerialName("sellable") val sellable: Int = 0,
    @SerialName("blocked") val blocked: Int = 0,
    @SerialName("shed_groups") val shedGroups: List<SaleShedGroupDto> = emptyList(),
    @SerialName("blocked_animals") val blockedAnimals: List<SaleCandidateDto> = emptyList(),
)

/** Confirm's result, and the shape of `GET /admin/goats/sale-allocations/{deal}`. */
@Serializable
data class SaleAllocationDto(
    @SerialName("sales_deal_id") val salesDealId: String = "",
    @SerialName("allocated") val allocated: Int = 0,
    @SerialName("shed_groups") val shedGroups: List<SaleShedGroupDto> = emptyList(),
)
