package sg.mesha.goatos.core.data.weighing

import java.security.MessageDigest
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto

/**
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the phone half of the two
 * SEPARATE capture sections -- per animal and whole pen -- judged against the task's PINNED rules.
 *
 * The SEEDED shape (one "Weighing video" per animal, one video slot per pen, no questions) keeps
 * today's request and idempotency key byte for byte, so a task planned on the seed replays exactly
 * as before; only an authored shape sends `proofs` / `answers` and folds a digest of that evidence
 * into its key (the removal card's shape, PR #274 review round 1).
 */

private val slotJson = Json { ignoreUnknownKeys = true }
private val individualSlotSerializer = MapSerializer(String.serializer(), String.serializer())
private val lumpSumSlotSerializer = MapSerializer(String.serializer(), ListSerializer(String.serializer()))

fun encodeIndividualSlotProofs(slots: Map<String, String>): String = slotJson.encodeToString(individualSlotSerializer, slots)

fun decodeIndividualSlotProofs(raw: String?): Map<String, String> =
    // exception:exempt a corrupt local column reads as "no secondary captures"; the row re-gates
    runCatching { slotJson.decodeFromString(individualSlotSerializer, raw.orEmpty().ifBlank { "{}" }) }.getOrDefault(emptyMap())

fun encodeLumpSumSlotProofs(slots: Map<String, List<String>>): String = slotJson.encodeToString(lumpSumSlotSerializer, slots)

fun decodeLumpSumSlotProofs(raw: String?): Map<String, List<String>> =
    // exception:exempt a corrupt local column reads as "no slotted captures"; the pen re-gates
    runCatching { slotJson.decodeFromString(lumpSumSlotSerializer, raw.orEmpty().ifBlank { "{}" }) }.getOrDefault(emptyMap())

fun encodeCaptureAnswers(answers: JsonObject?): String = (answers ?: JsonObject(emptyMap())).toString()

fun decodeCaptureAnswers(raw: String?): JsonObject =
    // exception:exempt a corrupt local column reads as "not answered"; required questions re-gate
    runCatching { slotJson.parseToJsonElement(raw.orEmpty().ifBlank { "{}" }) as JsonObject }.getOrDefault(JsonObject(emptyMap()))

// --- Per animal ---------------------------------------------------------------------------------

/** One compulsory "Weighing video" slot and no questions: today's request. */
fun WeighingSopRules.individualCaptureIsSeededShape(): Boolean =
    individualProofs.size == 1 &&
        individualProofs[0].key == WeighingSopRules.INDIVIDUAL_PROOF_ANIMAL_VIDEO &&
        individualProofs[0].acceptsVideo &&
        individualQuestions.isEmpty()

/**
 * The compulsory per-animal slots still missing a SERVER proof id: the primary slot is satisfied
 * by [primaryServerProofId] (the legacy serverProofId column), the others by [slotProofs].
 */
fun WeighingSopRules.missingIndividualCaptures(primaryServerProofId: String?, slotProofs: Map<String, String>): List<WeighingRemovalProofSlot> {
    val primaryKey = primaryIndividualSlot?.key
    return individualProofs.filter { slot ->
        slot.required && if (slot.key == primaryKey) primaryServerProofId.isNullOrBlank() else slotProofs[slot.key].isNullOrBlank()
    }
}

/** Required per-animal questions (applicable through their only_if ancestry) left unanswered. */
fun WeighingSopRules.missingIndividualAnswers(answers: JsonObject?): List<WeighingSopQuestionDto> =
    missingRequiredAnswers(individualQuestions, answers)

/** The answers to send: only questions that apply (a hidden draft answer never rides along). */
fun WeighingSopRules.applicableIndividualAnswers(answers: JsonObject?): JsonObject = applicableAnswers(individualQuestions, answers)

/** `{slot key: server proof id}` in slot order, primary first; null for the seeded shape. */
fun WeighingSopRules.individualRequestProofs(primaryServerProofId: String, slotProofs: Map<String, String>): Map<String, String>? {
    if (individualCaptureIsSeededShape()) return null
    val primaryKey = primaryIndividualSlot?.key ?: return null
    val out = linkedMapOf<String, String>() // mobile-guard:ignore: bounded by the SOP's <= 4 per-animal slots
    individualProofs.forEach { slot ->
        val ref = if (slot.key == primaryKey) primaryServerProofId else slotProofs[slot.key].orEmpty()
        if (ref.isNotBlank()) out[slot.key] = ref
    }
    return out
}

/** Idempotency-key suffix: empty for the seeded shape, else a digest of the slot map + answers. */
fun WeighingSopRules.individualSlotKeySuffix(slotProofs: Map<String, String>, answers: JsonObject?): String {
    if (individualCaptureIsSeededShape()) return ""
    val known = individualProofs.map { it.key }.toSet()
    val evidence = slotProofs.filterKeys { it in known }.toSortedMap().entries.joinToString("|") { "${it.key}=${it.value}" } +
        "#" + canonicalAnswers(applicableIndividualAnswers(answers))
    return ":slots:" + sha256Hex(evidence).take(16)
}

// --- Whole pen ----------------------------------------------------------------------------------

/** One video slot named pen_video and no questions: today's request. */
fun WeighingSopRules.lumpSumCaptureIsSeededShape(): Boolean =
    lumpSumProofs.size == 1 &&
        lumpSumProofs[0].key == WeighingSopRules.LUMP_SUM_PROOF_PEN_VIDEO &&
        lumpSumProofs[0].acceptsVideo &&
        lumpSumQuestions.isEmpty()

/** Whole-pen slots whose capture count is outside their min..max, in slot order. */
fun WeighingSopRules.lumpSumSlotProblems(slots: Map<String, List<String>>): List<WeighingCountedProofSlot> =
    lumpSumProofs.filter { slot ->
        val n = slots[slot.key].orEmpty().count { it.isNotBlank() }
        n < slot.min || n > slot.max
    }

fun WeighingSopRules.missingLumpSumAnswers(answers: JsonObject?): List<WeighingSopQuestionDto> =
    missingRequiredAnswers(lumpSumQuestions, answers)

fun WeighingSopRules.applicableLumpSumAnswers(answers: JsonObject?): JsonObject = applicableAnswers(lumpSumQuestions, answers)

/** Every whole-pen capture in slot order (the flat proof_artifact_ids; the first is the primary). */
fun WeighingSopRules.lumpSumFlatProofs(slots: Map<String, List<String>>): List<String> =
    lumpSumProofs.flatMap { slot -> slots[slot.key].orEmpty().filter { it.isNotBlank() } }

/** `{slot key: refs}` in slot order; null for the seeded shape. */
fun WeighingSopRules.lumpSumRequestProofs(slots: Map<String, List<String>>): Map<String, List<String>>? {
    if (lumpSumCaptureIsSeededShape()) return null
    val out = linkedMapOf<String, List<String>>() // mobile-guard:ignore: bounded by the SOP's <= 4 whole-pen slots
    lumpSumProofs.forEach { slot ->
        val refs = slots[slot.key].orEmpty().filter { it.isNotBlank() }
        if (refs.isNotEmpty()) out[slot.key] = refs
    }
    return out
}

// --- Shared question engine (mirrors the backend's applicableSOPQuestions) ------------------------

private fun applicableIds(questions: List<WeighingSopQuestionDto>, answers: JsonObject?): Set<String> {
    val out = mutableSetOf<String>() // mobile-guard:ignore: bounded by the section's <= 20 questions
    questions.forEach { q ->
        val cond = q.onlyIf
        if (cond == null || (cond.questionId in out && answers.choice(cond.questionId) == cond.value)) out += q.id
    }
    return out
}

private fun missingRequiredAnswers(questions: List<WeighingSopQuestionDto>, answers: JsonObject?): List<WeighingSopQuestionDto> {
    val applicable = applicableIds(questions, answers)
    return questions.filter { q -> q.required && q.id in applicable && !answers.answered(q) }
}

private fun applicableAnswers(questions: List<WeighingSopQuestionDto>, answers: JsonObject?): JsonObject {
    if (answers == null) return JsonObject(emptyMap())
    val applicable = applicableIds(questions, answers)
    return buildJsonObject {
        questions.forEach { q ->
            if (q.id !in applicable) return@forEach
            answers[q.id]?.takeIf { it != JsonNull }?.let { put(q.id, it) }
            if (q.kind == "choice" && q.allowOther && answers.choice(q.id) == "other") {
                answers["${q.id}_other"]?.let { put("${q.id}_other", it) }
            }
        }
    }
}

private fun JsonObject?.choice(id: String): String = (this?.get(id) as? JsonPrimitive)?.takeIf { it.isString }?.content?.trim().orEmpty()

private fun JsonObject?.answered(q: WeighingSopQuestionDto): Boolean {
    val value = this?.get(q.id) ?: return false
    return when (value) {
        is JsonNull -> false
        is JsonArray -> value.isNotEmpty()
        is JsonPrimitive -> value.content.isNotBlank() &&
            !(q.kind == "choice" && q.allowOther && value.content == "other" && choice("${q.id}_other").isBlank())
        else -> true
    }
}

private fun canonicalAnswers(answers: JsonObject): String =
    answers.entries.sortedBy { it.key }.joinToString(",") { "${it.key}:${it.value}" }

private fun sha256Hex(value: String): String =
    MessageDigest.getInstance("SHA-256").digest(value.toByteArray()).joinToString("") { "%02x".format(it) }
