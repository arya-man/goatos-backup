package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.core.network.dto.VendorFormDto
import sg.mesha.goatos.core.network.dto.VendorFormPageDto
import sg.mesha.goatos.core.network.dto.VendorQuestionDto
import sg.mesha.goatos.core.network.dto.VendorWriteDto
import sg.mesha.goatos.feature.vendors.VendorFormPageUi
import sg.mesha.goatos.feature.vendors.VendorQuestionKind
import sg.mesha.goatos.feature.vendors.VendorQuestionUi
import sg.mesha.goatos.feature.vendors.VendorsOptionUi

/**
 * VENDOR FORM IS AUTHORED (maintainer instruction 2026-09-19): the pure half of the vendor
 * wizard -- how the published form's answers are checked, sent and prefilled. Kept out of the
 * ViewModel so it is testable without Hilt (VendorFormAnswersTest).
 */

// ---- the published form (VENDOR FORM IS AUTHORED, 2026-09-19) ----

/** A question is asked only when its "ask only when" condition holds on the current answers. */
internal fun VendorQuestionUi.isAsked(answers: Map<String, String>): Boolean =
    onlyIfQuestion.isBlank() || answers[onlyIfQuestion].orEmpty().trim() == onlyIfValue

/** Evaluate the entire document in order, so stale hidden parents never activate descendants. */
internal fun visibleVendorAnswers(pages: List<VendorFormPageUi>, answers: Map<String, String>): Map<String, String> = buildMap {
    for (q in pages.flatMap { it.questions }) {
        if (!q.isAsked(this)) continue
        answers[q.id]?.let { put(q.id, it) }
        if (q.allowOther && answers[q.id].orEmpty().trim() == "other") {
            answers[q.id + "_other"]?.let { put(q.id + "_other", it) }
        }
    }
}

/** Mirrors the backend's cheapest answer checks so the refusal shows beside the box before the write. */
internal fun validateVendorFormPage(page: VendorFormPageUi, answers: Map<String, String>): Map<String, String> {
    val errors = mutableMapOf<String, String>() // mobile-guard:ignore: per-call validation result, at most one entry per asked question, returned and dropped
    for (q in page.questions) {
        if (!q.isAsked(answers)) continue
        val value = answers[q.id].orEmpty().trim()
        if (value.isBlank()) {
            if (q.required) errors[q.id] = VENDOR_REQUIRED
            continue
        }
        when (q.kind) {
            VendorQuestionKind.CHOICE -> {
                if (q.options.none { it.value == value }) errors[q.id] = PICK_ONE_OFFERED
                else if (value == OTHER && q.allowOther && answers[q.id + OTHER_SUFFIX].orEmpty().isBlank()) errors[q.id] = SAY_OTHER
            }
            VendorQuestionKind.MULTI -> if (value.split('|').any { v -> q.options.none { it.value == v.trim() } }) errors[q.id] = PICK_ONE_OFFERED
            VendorQuestionKind.NUMBER -> {
                val n = value.toDoubleOrNull()
                val min = q.min
                val max = q.max
                when {
                    n == null -> errors[q.id] = NUMBER
                    min != null && n < min -> errors[q.id] = "$AT_LEAST ${trimNumber(min)}"
                    max != null && n > max -> errors[q.id] = "$AT_MOST ${trimNumber(max)}"
                    q.id == FIELD_PRICE && !Regex("""^\d{1,10}(\.\d{1,2})?$""").matches(value) -> errors[q.id] = AMOUNT
                    q.id == FIELD_ETA && (value.toIntOrNull() == null || value.toInt() < 0) -> errors[q.id] = WHOLE_DAYS
                }
            }
            VendorQuestionKind.TEXT -> Unit
        }
    }
    return errors
}

private fun trimNumber(v: Double): String = if (v == Math.floor(v)) v.toLong().toString() else v.toString()

internal fun vendorContextLine(a: Map<String, String>, pages: List<VendorFormPageUi>): String {
    fun label(id: String): String? {
        val v = a[id].orEmpty().trim().ifBlank { return null }
        val q = pages.flatMap { it.questions }.firstOrNull { it.id == id }
        return q?.options?.firstOrNull { it.value == v }?.label ?: v
    }
    return dotJoin(a[FIELD_BUSINESS_NAME], label(FIELD_RECORD_TYPE), dotJoin(a[FIELD_CITY], label(FIELD_STATE)))
}

/**
 * Every asked answer, blank included, plus the typed columns filled from the same answers.
 * A question hidden by its condition is dropped so a stale follow-up answer never travels.
 */
internal fun vendorAnswersToWrite(answers: Map<String, String>, pages: List<VendorFormPageUi>, version: Int, sopCode: String = "sales.vendor"): VendorWriteDto {
    val visible = visibleVendorAnswers(pages, answers)
    val asked = pages.flatMap { it.questions }.filter { it.isAsked(visible) }
    val sent = buildMap {
        for (q in asked) {
            put(q.id, answers[q.id].orEmpty().trim())
            answers[q.id + OTHER_SUFFIX]?.takeIf { it.isNotBlank() }?.let { put(q.id + OTHER_SUFFIX, it.trim()) }
        }
    }
    fun t(id: String) = sent[id].orEmpty()
    return VendorWriteDto(
        recordType = t(FIELD_RECORD_TYPE),
        businessName = t(FIELD_BUSINESS_NAME),
        contactPersonName = t("contact_person_name"),
        phoneNumber = t("phone_number"),
        breed = t("breed"),
        feed = t("feed"),
        status = t("status").ifBlank { "active" },
        filteredStock = t("filtered_stock").toIntOrNull(),
        pricePerGoat = t(FIELD_PRICE).ifBlank { null },
        readyToFiltered = t("ready_to_filtered"),
        etaAfterOrderDays = t(FIELD_ETA).toIntOrNull(),
        details = t("details"),
        state = t(FIELD_STATE),
        city = t(FIELD_CITY),
        comments = t("comments"),
        capacityQuantity = t("capacity_quantity").ifBlank { null },
        capacityUnit = t("capacity_unit"),
        supplyFrequency = t("supply_frequency"),
        averageAnimalWeightKg = t("average_animal_weight_kg").ifBlank { null },
        answers = sent,
        questionnaireVersion = version,
        questionnaireSopCode = sopCode,
    )
}

/**
 * On EDIT, a typed column the form does not ask rides from the stored row (the replace would
 * otherwise blank it), as does the existing voice note; the row_version is the fence.
 */
internal fun VendorWriteDto.carryingUnaskedVendorColumnsOf(stored: VendorDto, pages: List<VendorFormPageUi>): VendorWriteDto {
    val askedIds = pages.flatMap { it.questions }.map { it.id }.toSet()
    fun <T> keep(id: String, fromForm: T, fromStore: T): T = if (id in askedIds) fromForm else fromStore
    return copy(
        contactPersonName = keep("contact_person_name", contactPersonName, stored.contactPersonName.orEmpty()),
        phoneNumber = keep("phone_number", phoneNumber, stored.phoneNumber.orEmpty()),
        breed = keep("breed", breed, stored.breed.orEmpty()),
        feed = keep("feed", feed, stored.feed.orEmpty()),
        status = keep("status", status, stored.status),
        filteredStock = keep("filtered_stock", filteredStock, stored.filteredStock),
        pricePerGoat = keep(FIELD_PRICE, pricePerGoat, stored.pricePerGoat),
        readyToFiltered = keep("ready_to_filtered", readyToFiltered, stored.readyToFiltered.orEmpty()),
        etaAfterOrderDays = keep(FIELD_ETA, etaAfterOrderDays, stored.etaAfterOrderDays),
        details = keep("details", details, stored.details.orEmpty()),
        city = keep(FIELD_CITY, city, stored.city.orEmpty()),
        comments = keep("comments", comments, stored.comments.orEmpty()),
        capacityQuantity = keep("capacity_quantity", capacityQuantity, stored.capacityQuantity),
        capacityUnit = keep("capacity_unit", capacityUnit, stored.capacityUnit.orEmpty()),
        supplyFrequency = keep("supply_frequency", supplyFrequency, stored.supplyFrequency.orEmpty()),
        averageAnimalWeightKg = keep("average_animal_weight_kg", averageAnimalWeightKg, stored.averageAnimalWeightKg),
        voiceNoteProofRef = stored.voiceNoteProofRef.orEmpty(),
        rowVersion = stored.rowVersion,
    )
}

/** The stored row as answers: typed columns by their question id, plus the stored extras. */
internal fun VendorDto.toVendorAnswers(): Map<String, String> = buildMap {
    put(FIELD_BUSINESS_NAME, businessName)
    put(FIELD_RECORD_TYPE, recordType)
    put("contact_person_name", contactPersonName.orEmpty())
    put("phone_number", phoneNumber.orEmpty())
    put(FIELD_STATE, state)
    put(FIELD_CITY, city.orEmpty())
    put("status", status.ifBlank { "active" })
    put("capacity_quantity", capacityQuantity.orEmpty())
    put("capacity_unit", capacityUnit.orEmpty())
    put("supply_frequency", supplyFrequency.orEmpty())
    put("feed", feed.orEmpty())
    put("breed", breed.orEmpty())
    put(FIELD_PRICE, pricePerGoat.orEmpty())
    put(FIELD_ETA, etaAfterOrderDays?.toString().orEmpty())
    put("average_animal_weight_kg", averageAnimalWeightKg.orEmpty())
    put("comments", comments.orEmpty())
    put("details", details.orEmpty())
    put("ready_to_filtered", readyToFiltered.orEmpty())
    put("filtered_stock", filteredStock?.toString().orEmpty())
    putAll(answers)
}

/**
 * The pages of a published ENTRY form with the register's own TYPED columns removed, and any page
 * left with no questions dropped (PROCUREMENT IS SOP-DRIVEN END TO END, 2026-09-20).
 *
 * The feed purchase wizard draws the ledger's own columns -- date, farm, feed, quantity, vendor,
 * the costs -- by hand, so rendering the typed questions again would ask for the same fact twice
 * and let an operator answer each differently. What is left is the buying desk's own questions.
 * A null form (never fetched, or fetched before the document existed) is simply no extra pages,
 * so the wizard is exactly what it was before this existed.
 */
internal fun VendorFormDto?.extraPages(): List<VendorFormPageUi> =
    this?.pages.orEmpty()
        .map { page -> page.toUi().let { it.copy(questions = it.questions.filterNot { q -> q.typed }) } }
        .filter { it.questions.isNotEmpty() }

internal fun VendorFormPageDto.toUi(): VendorFormPageUi =
    VendorFormPageUi(key = key, title = title, hint = hint, questions = questions.map { it.toUi() })

internal fun VendorQuestionDto.toUi(): VendorQuestionUi = VendorQuestionUi(
    id = id,
    kind = when (kind) {
        "multi" -> VendorQuestionKind.MULTI
        "number" -> VendorQuestionKind.NUMBER
        "text" -> VendorQuestionKind.TEXT
        else -> VendorQuestionKind.CHOICE
    },
    title = title,
    hint = hint,
    required = required,
    typed = typed,
    // Only ACTIVE catalog entries are offered for a new value; the server compiles them that way.
    options = options.filter { it.isActive }.map { VendorsOptionUi(it.value, it.label) },
    allowOther = allowOther,
    min = min,
    max = max,
    unit = unit,
    onlyIfQuestion = onlyIf?.questionId.orEmpty(),
    onlyIfValue = onlyIf?.value.orEmpty(),
)

private const val VENDOR_REQUIRED = "Required"
private const val FIELD_BUSINESS_NAME = "business_name"
private const val FIELD_RECORD_TYPE = "record_type"
private const val FIELD_STATE = "state"
private const val FIELD_CITY = "city"
private const val FIELD_PRICE = "price_per_goat"
private const val FIELD_ETA = "eta_after_order_days"
private const val OTHER = "other"
private const val OTHER_SUFFIX = "_other"
private const val PICK_ONE_OFFERED = "Pick one of the choices"
private const val SAY_OTHER = "Say what the other is"
private const val NUMBER = "Enter a number"
private const val AT_LEAST = "Must be at least"
private const val AT_MOST = "Must be at most"
private const val AMOUNT = "Enter an amount, up to two decimals"
private const val WHOLE_DAYS = "Whole days, zero or more"
