package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import sg.mesha.goatos.core.data.weighing.WeighingSopRules
import sg.mesha.goatos.core.data.weighing.missingIndividualAnswers
import sg.mesha.goatos.core.data.weighing.missingLumpSumAnswers
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.weighing.WeighingCaptureAnswerUi
import sg.mesha.goatos.feature.weighing.WeighingCaptureQuestionUi
import sg.mesha.goatos.feature.weighing.WeighingCaptureSlotUi
import sg.mesha.goatos.feature.weighing.WeighingCaptureSopUi
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow

/*
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the capture screen's mapping
 * from the task's PINNED rules to its two SEPARATE sections. The PRIMARY capture of each section
 * keeps its existing control and field key (per-animal video `weighing_individual_video`, the
 * pen's group videos `weighing_shed_partition_video`), so a task on the seed renders and replays
 * exactly as before; every other authored slot gets its own field key below.
 */

internal const val INDIVIDUAL_SLOT_FIELD_PREFIX = "weighing_individual_slot_"
internal const val SHED_SLOT_FIELD_PREFIX = "weighing_shed_slot_"

internal fun WeighingSopQuestionDto.toCaptureQuestionUi() = WeighingCaptureQuestionUi(
    id = id,
    kind = kind,
    title = title,
    hint = hint,
    required = required,
    options = options.map { it.value to it.label },
    allowOther = allowOther,
    unit = unit,
)

internal fun JsonObject?.toAnswerUi(questions: List<WeighingSopQuestionDto>): Map<String, WeighingCaptureAnswerUi> {
    val answers = this ?: return emptyMap()
    return questions.mapNotNull { q ->
        val raw = answers[q.id] ?: return@mapNotNull null
        val ui = when (raw) {
            is JsonArray -> WeighingCaptureAnswerUi(values = raw.mapNotNull { (it as? JsonPrimitive)?.content }.toSet())
            is JsonPrimitive -> WeighingCaptureAnswerUi(
                value = raw.content.let { if (!raw.isString && it.endsWith(".0")) it.removeSuffix(".0") else it },
                otherText = (answers["${q.id}_other"] as? JsonPrimitive)?.content.orEmpty(),
            )
            else -> return@mapNotNull null
        }
        q.id to ui
    }.toMap()
}

/** Sets a pick-one / text / number answer; blank removes it. Numbers ride as JSON numbers. */
internal fun JsonObject?.withAnswer(question: WeighingSopQuestionDto?, value: String): JsonObject {
    val id = question?.id ?: return this ?: JsonObject(emptyMap())
    val out = (this ?: JsonObject(emptyMap())).toMutableMap()
    val trimmed = value.trim()
    when {
        trimmed.isEmpty() -> out.remove(id)
        question.kind == "number" -> out[id] = trimmed.toDoubleOrNull()?.let { JsonPrimitive(it) } ?: JsonPrimitive(trimmed)
        question.kind == "text" -> out[id] = JsonPrimitive(value)
        else -> out[id] = JsonPrimitive(trimmed)
    }
    return JsonObject(out)
}

/** Ticks / unticks one option of a pick-many question. */
internal fun JsonObject?.withToggled(question: WeighingSopQuestionDto?, value: String): JsonObject {
    val id = question?.id ?: return this ?: JsonObject(emptyMap())
    val out = (this ?: JsonObject(emptyMap())).toMutableMap()
    val current = (out[id] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.content }.orEmpty()
    val next = if (value in current) current - value else current + value
    if (next.isEmpty()) out.remove(id) else out[id] = JsonArray(next.map { JsonPrimitive(it) })
    return JsonObject(out)
}

/** The "other" free text of a pick-one, riding under `<id>_other`. */
internal fun JsonObject?.withOther(questionId: String, text: String): JsonObject {
    val out = (this ?: JsonObject(emptyMap())).toMutableMap()
    if (text.isBlank()) out.remove("${questionId}_other") else out["${questionId}_other"] = JsonPrimitive(text)
    return JsonObject(out.filterValues { it != JsonNull })
}

/** What one animal still owes, by the SOP's own titles: slots first, then required questions. */
internal fun WeighingSopRules.animalCapturesMissing(primarySynced: Boolean, extraSynced: Set<String>, answers: JsonObject?): List<String> {
    val primaryKey = primaryIndividualSlot?.key
    val slots = individualProofs.filter { slot ->
        slot.required && if (slot.key == primaryKey) !primarySynced else slot.key !in extraSynced
    }.map { it.title }
    return slots + missingIndividualAnswers(answers).map { it.title }
}

/** The screen's two capture sections from the pinned rules and what has been captured so far. */
internal fun WeighingSopRules.toCaptureSopUi(
    extraPenProofs: Map<String, List<WeighingProofUiRow>>,
    primaryPenSynced: Int,
    penAnswers: JsonObject?,
): WeighingCaptureSopUi {
    val primaryPen = lumpSumProofs.firstOrNull()
    val penExtras = lumpSumProofs.drop(1).map { slot ->
        WeighingCaptureSlotUi(slot.key, slot.title, slot.hint, slot.kind, slot.required, slot.min, slot.max, extraPenProofs[slot.key].orEmpty())
    }
    val penMissing = buildList {
        if (primaryPen != null && primaryPenSynced < primaryPen.min) add(primaryPen.title)
        penExtras.forEach { slot ->
            if (slot.proofs.count { it.status == ProofUploadStatus.SYNCED } < slot.min) add(slot.title)
        }
        missingLumpSumAnswers(penAnswers).forEach { add(it.title) }
    }
    return WeighingCaptureSopUi(
        animalExtraSlots = individualProofs.drop(1).map { WeighingCaptureSlotUi(it.key, it.title, it.hint, it.kind, it.required) },
        animalQuestions = individualQuestions.map { it.toCaptureQuestionUi() },
        primaryPenSlotTitle = primaryPen?.title.orEmpty(),
        primaryPenSlotMax = primaryPen?.max ?: WeighingSopRules.Seeded.lumpSumVideoMax,
        primaryPenSlotKind = primaryPen?.kind ?: "video",
        penExtraSlots = penExtras,
        penQuestions = lumpSumQuestions.map { it.toCaptureQuestionUi() },
        penAnswers = penAnswers.toAnswerUi(lumpSumQuestions),
        penCapturesMissing = penMissing,
    )
}
