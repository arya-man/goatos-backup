package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject

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

/** `only_if`: a question is shown (and required) only while another question holds this value. */
@Serializable
data class AnimalPurchaseConditionDto(
    @SerialName("question_id") val questionId: String,
    @SerialName("value") val value: String = "",
)

/**
 * One item of the Procurement SOP questionnaire, served in display order. The wording is the
 * SOP's own and is rendered verbatim; changing a question is a backend version bump, never a
 * client release. `kind` is one of `choice` | `multi` | `text` | `number` | `media` | `section`.
 */
@Serializable
data class AnimalPurchaseQuestionDto(
    @SerialName("id") val id: String,
    @SerialName("kind") val kind: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("hint") val hint: String = "",
    @SerialName("required") val required: Boolean = false,
    @SerialName("options") val options: List<AnimalPurchaseOptionDto> = emptyList(),
    /** A choice/multi may carry free text under its "other" option, sent as `<id>_other`. */
    @SerialName("allow_other") val allowOther: Boolean = false,
    /** Media questions: the capture slot the files land in, its cap and the accepted kinds. */
    @SerialName("slot") val slot: String = "",
    @SerialName("max_files") val maxFiles: Int = 0,
    /** `photo` and/or `video`. */
    @SerialName("accepts") val accepts: List<String> = emptyList(),
    /** Number questions: the accepted range and the unit shown beside the field. */
    @SerialName("min") val min: Double? = null,
    @SerialName("max") val max: Double? = null,
    @SerialName("unit") val unit: String = "",
    @SerialName("only_if") val onlyIf: AnimalPurchaseConditionDto? = null,
)

/** `GET /app/procurement/animal-purchases/options` — the two forms' vocabulary and copy. */
@Serializable
data class AnimalPurchaseOptionsDto(
    /** The SOP questionnaire in display order; empty until the server has served it. */
    @SerialName("questionnaire") val questionnaire: List<AnimalPurchaseQuestionDto> = emptyList(),
    @SerialName("questionnaire_version") val questionnaireVersion: Int = 0,
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

/** One recorded answer rendered for display under its own question text, in SOP order. */
@Serializable
data class AnimalPurchaseAnswerRowDto(
    @SerialName("section") val section: String = "",
    @SerialName("question_id") val questionId: String = "",
    @SerialName("question") val question: String = "",
    @SerialName("answer") val answer: String = "",
    /** The SOP reads this answer as a reject signal. */
    @SerialName("attention") val attention: Boolean = false,
)

/** One capture inside a media slot, with its signed playback link when it can be served. */
@Serializable
data class AnimalPurchaseMediaItemDto(
    @SerialName("proof_ref") val proofRef: String = "",
    @SerialName("media_url") val mediaUrl: String = "",
    @SerialName("media_mime") val mediaMime: String = "",
)

/** The captures of one media slot, titled by the backend. */
@Serializable
data class AnimalPurchaseMediaSlotDto(
    @SerialName("slot") val slot: String = "",
    @SerialName("title") val title: String = "",
    @SerialName("items") val items: List<AnimalPurchaseMediaItemDto> = emptyList(),
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
    /** 0 for rows recorded before the questionnaire (the legacy fixed fields + one video). */
    @SerialName("questionnaire_version") val questionnaireVersion: Int = 0,
    /** The answers rendered for display in SOP order; unanswered optional questions omitted. */
    @SerialName("answer_rows") val answerRows: List<AnimalPurchaseAnswerRowDto> = emptyList(),
    /** The captures per media slot, in SOP order. */
    @SerialName("media_slots") val mediaSlots: List<AnimalPurchaseMediaSlotDto> = emptyList(),
    /** The inspector's OWN recommendation (`""` | `selected` | `on_hold`); the CEO's decision stands apart. */
    @SerialName("field_verdict") val fieldVerdict: String = "",
    /** Backend-owned chip copy for the field verdict, verbatim; blank when none. */
    @SerialName("field_verdict_label") val fieldVerdictLabel: String = "",
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

/**
 * `POST /app/procurement/animal-purchases/loads/{load_id}/animals` body: the SOP inspection.
 * `answers` is keyed by question id from the served questionnaire — a choice carries its option
 * value (free text for an "other" choice rides under `<id>_other`), a multi an array of option
 * values, a number a JSON number, text a string. `media` is keyed by slot with the FINISHED
 * in-app-camera proof references in capture order; it is blank at rest in the outbox and filled
 * by the sync engine at dispatch once every upload has drained.
 */
@Serializable
data class AnimalPurchaseAnimalCreateRequestDto(
    /** The SOP version the form rendered (from /options); 0 = whatever is published. */
    @SerialName("questionnaire_version") val questionnaireVersion: Int = 0,
    @SerialName("answers") val answers: JsonObject = JsonObject(emptyMap()),
    @SerialName("media") val media: Map<String, List<String>> = emptyMap(),
)
