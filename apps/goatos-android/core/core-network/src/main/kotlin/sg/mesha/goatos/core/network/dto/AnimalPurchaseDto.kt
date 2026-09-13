package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Animal purchases (maintainer decision 2026-09-13, `docs/decisions/animal-purchases.md`): the
 * procurement desk records a purchase LOAD on the phone, then the animals on offer inside it one
 * at a time, each with an in-app-camera video; the CEO/CXO accepts or rejects each animal on
 * admin-web and the phone shows the answer.
 *
 * ALL business copy (row titles, summaries, decision chips, form labels, hints) is BACKEND-OWNED
 * and rendered verbatim. The phone's stored copy is a placeholder overwritten by the server read,
 * never treated as truth.
 *
 * Wire contract of record: `contracts/openapi/app-api.yaml`, tag `AnimalPurchases`.
 */
@Serializable
data class AnimalPurchaseOptionDto(
    @SerialName("value") val value: String,
    /** Farm-worded label, rendered verbatim. */
    @SerialName("label") val label: String = "",
)

/** `GET /app/procurement/animal-purchases/options` — the two forms' vocabulary and copy. */
@Serializable
data class AnimalPurchaseOptionsDto(
    @SerialName("species") val species: List<AnimalPurchaseOptionDto> = emptyList(),
    @SerialName("sexes") val sexes: List<AnimalPurchaseOptionDto> = emptyList(),
    @SerialName("conditions") val conditions: List<AnimalPurchaseOptionDto> = emptyList(),
    @SerialName("farms") val farms: List<AnimalPurchaseOptionDto> = emptyList(),
    /** The herd's own breed spellings; the breed field itself stays free text. */
    @SerialName("breed_suggestions") val breedSuggestions: List<String> = emptyList(),
    /** The phone form's copy, keyed (`load.field.load_ref`, `animal.save`, ...), rendered verbatim. */
    @SerialName("copy") val copy: Map<String, String> = emptyMap(),
)

/** WHOLE-LOAD decision counts, never page sums. */
@Serializable
data class AnimalPurchaseCountsDto(
    @SerialName("total") val total: Int = 0,
    @SerialName("pending") val pending: Int = 0,
    @SerialName("accepted") val accepted: Int = 0,
    @SerialName("rejected") val rejected: Int = 0,
)

/** One purchase load (list-row and detail-header shape). */
@Serializable
data class AnimalPurchaseLoadDto(
    @SerialName("load_id") val loadId: String,
    @SerialName("load_ref") val loadRef: String = "",
    /** Backend-owned row title ("Load 132 · Vendor"), verbatim. */
    @SerialName("title") val title: String = "",
    @SerialName("vendor_id") val vendorId: String = "",
    @SerialName("vendor_name") val vendorName: String = "",
    /** `CBE` | `CPT`. */
    @SerialName("farm") val farm: String = "",
    @SerialName("expected_count") val expectedCount: Int = 0,
    @SerialName("notes") val notes: String = "",
    /** `open` | `closed`. */
    @SerialName("status") val status: String = "",
    @SerialName("counts") val counts: AnimalPurchaseCountsDto = AnimalPurchaseCountsDto(),
    /** Backend-owned line under the title, verbatim. */
    @SerialName("summary") val summary: String = "",
    @SerialName("recorded_by") val recordedBy: String = "",
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    @SerialName("row_version") val rowVersion: Long = 0,
)

/** `GET /app/procurement/animal-purchases/loads` — one keyset page, newest first. */
@Serializable
data class AnimalPurchaseLoadPageDto(
    @SerialName("loads") val loads: List<AnimalPurchaseLoadDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    /** Whether THIS caller may add loads and animals (the write permission), server-resolved. */
    @SerialName("can_record") val canRecord: Boolean = false,
)

/** `POST /app/procurement/animal-purchases/loads` body. */
@Serializable
data class AnimalPurchaseLoadCreateRequestDto(
    @SerialName("load_ref") val loadRef: String,
    @SerialName("vendor_id") val vendorId: String,
    @SerialName("farm") val farm: String,
    @SerialName("expected_count") val expectedCount: Int = 0,
    @SerialName("notes") val notes: String = "",
)

/** One animal on offer inside a load. */
@Serializable
data class AnimalPurchaseAnimalDto(
    @SerialName("candidate_id") val candidateId: String,
    @SerialName("load_id") val loadId: String = "",
    @SerialName("load_ref") val loadRef: String = "",
    @SerialName("seq_no") val seqNo: Int = 0,
    /** Backend-owned row title ("Animal 7 · Female goat"), verbatim. */
    @SerialName("title") val title: String = "",
    @SerialName("species") val species: String = "",
    @SerialName("species_label") val speciesLabel: String = "",
    @SerialName("sex") val sex: String = "",
    @SerialName("sex_label") val sexLabel: String = "",
    @SerialName("breed") val breed: String = "",
    @SerialName("age_months") val ageMonths: Int? = null,
    @SerialName("weight_kg") val weightKg: Double? = null,
    @SerialName("condition") val condition: String = "",
    @SerialName("condition_label") val conditionLabel: String = "",
    @SerialName("temp_tag") val tempTag: String = "",
    @SerialName("notes") val notes: String = "",
    @SerialName("video_proof_ref") val videoProofRef: String = "",
    /** Signed playback link; blank when the video cannot be served right now. */
    @SerialName("media_url") val mediaUrl: String = "",
    @SerialName("media_mime") val mediaMime: String = "",
    /** `pending` | `accepted` | `rejected`. */
    @SerialName("decision") val decision: String = "",
    /** Backend-owned chip copy, verbatim. */
    @SerialName("decision_label") val decisionLabel: String = "",
    /** `neutral` | `ok` | `bad`. */
    @SerialName("decision_tone") val decisionTone: String = "",
    @SerialName("decided_by_name") val decidedByName: String = "",
    @SerialName("decided_at") val decidedAt: String = "",
    @SerialName("decision_note") val decisionNote: String = "",
    @SerialName("recorded_by") val recordedBy: String = "",
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("updated_at") val updatedAt: String = "",
    @SerialName("row_version") val rowVersion: Long = 0,
)

/** `GET /app/procurement/animal-purchases/loads/{load_id}/animals` — one keyset page. */
@Serializable
data class AnimalPurchaseAnimalPageDto(
    @SerialName("animals") val animals: List<AnimalPurchaseAnimalDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    @SerialName("counts") val counts: AnimalPurchaseCountsDto = AnimalPurchaseCountsDto(),
)

/** `GET /app/procurement/animal-purchases/loads/{load_id}` — the load plus its FIRST animal page. */
@Serializable
data class AnimalPurchaseLoadDetailDto(
    @SerialName("load") val load: AnimalPurchaseLoadDto,
    @SerialName("animals") val animals: List<AnimalPurchaseAnimalDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String = "",
    @SerialName("can_record") val canRecord: Boolean = false,
)

/** `POST /app/procurement/animal-purchases/loads/{load_id}/animals` body. */
@Serializable
data class AnimalPurchaseAnimalCreateRequestDto(
    @SerialName("species") val species: String,
    @SerialName("sex") val sex: String,
    @SerialName("breed") val breed: String = "",
    @SerialName("age_months") val ageMonths: Int? = null,
    @SerialName("weight_kg") val weightKg: Double? = null,
    @SerialName("condition") val condition: String,
    @SerialName("temp_tag") val tempTag: String = "",
    @SerialName("notes") val notes: String = "",
    /** A FINISHED in-app-camera video proof reference; resolved by the sync engine at dispatch. */
    @SerialName("video_proof_ref") val videoProofRef: String = "",
)
