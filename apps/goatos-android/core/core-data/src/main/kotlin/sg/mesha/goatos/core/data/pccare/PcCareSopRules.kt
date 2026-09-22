package sg.mesha.goatos.core.data.pccare

import sg.mesha.goatos.core.network.dto.PcCareSopCategoryDto
import sg.mesha.goatos.core.network.dto.PcCareSopDto
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto

/**
 * PC CARE SOP (maintainer decision 2026-09-22): the compiled pc_care.tasks rule set the backend
 * serves on the planner catalog (the PUBLISHED version a new task is stamped with) and on the
 * single-task read (the task's PINNED version). The phone renders it and holds no rule of its own:
 * whether the evening-before feed & water removal applies, to WHICH work, from which evening, what
 * the evening crew records and answers per pen, and -- per work category -- what the operator
 * records for every animal and answers when submitting.
 *
 * The capture MODE of a category (scan-and-record vs roster-pick) is NOT here: how the operator
 * reaches an animal is what the work is, and the task read still carries it as `capture_mode`.
 */
data class PcCareSopRules(
    val version: Int,
    val removalMode: String,
    /** The work the removal may accompany; the planner is offered it on these categories alone. */
    val removalAppliesTo: List<String>,
    /** The EFFECTIVE removal evening ("HH:MM" IST) the server resolved; blank = unknown. */
    val removalCutoffTime: String,
    val removalInstruction: String,
    val removalProofs: List<PcCareSopSlot>,
    val removalQuestions: List<WeighingSopQuestionDto>,
    val categories: Map<String, PcCareSopCategory>,
) {
    /** Every task of a listed category carries the removal; the planner is not asked. */
    val removalRequired: Boolean get() = removalMode == REMOVAL_REQUIRED

    /** The planner decides per task, on the listed work. */
    val removalOptional: Boolean get() = removalMode == REMOVAL_OPTIONAL

    /** No task carries the removal; the wizard offers no removal step at all. */
    val removalOff: Boolean get() = removalMode == REMOVAL_OFF

    /**
     * Whether the removal may accompany THIS work. Never under `off`; otherwise only for a
     * category the document lists -- the wizard must not offer a removal the create would refuse.
     */
    fun removalAppliesToCategory(category: String): Boolean = !removalOff && removalAppliesTo.contains(category)

    /** Whether the planner is ASKED about the removal for this work (as opposed to told). */
    fun removalIsAChoice(category: String): Boolean = removalOptional && removalAppliesToCategory(category)

    /** The card for one work category; a category the document does not carry reads empty. */
    fun category(category: String): PcCareSopCategory = categories[category] ?: PcCareSopCategory()

    companion object {
        const val REMOVAL_REQUIRED = "required"
        const val REMOVAL_OPTIONAL = "optional"
        const val REMOVAL_OFF = "off"

        /**
         * What a phone runs with no served rules at all: an older server, or a cache written
         * before this build. It is the SEEDED document's shape -- the removal optional on
         * deworming alone -- so the wizard behaves exactly as it did before the SOP existed.
         */
        val Seeded = PcCareSopRules(
            version = 0,
            removalMode = REMOVAL_OPTIONAL,
            removalAppliesTo = listOf("deworming"),
            removalCutoffTime = "",
            removalInstruction = "",
            removalProofs = emptyList(),
            removalQuestions = emptyList(),
            categories = emptyMap(),
        )
    }
}

/** One authored capture: what it is called, what it takes, and whether the work needs it. */
data class PcCareSopSlot(
    val key: String,
    val title: String,
    val hint: String,
    val kind: String,
    val required: Boolean,
    /** Recorder-chrome guidance in seconds; 0 = none. Never a client-enforced cap. */
    val minSeconds: Int = 0,
) {
    val acceptsVideo: Boolean get() = kind == KIND_VIDEO || kind == KIND_EITHER || kind.isBlank()
    val acceptsPhoto: Boolean get() = kind == KIND_PHOTO || kind == KIND_EITHER

    companion object {
        const val KIND_VIDEO = "video"
        const val KIND_PHOTO = "photo"
        const val KIND_EITHER = "either"
    }
}

/** One work category's card. */
data class PcCareSopCategory(
    val instruction: String = "",
    val proofs: List<PcCareSopSlot> = emptyList(),
    val questions: List<WeighingSopQuestionDto> = emptyList(),
)

fun PcCareSlotDto.toSopSlot(): PcCareSopSlot = PcCareSopSlot(
    key = fieldKey,
    title = label,
    hint = description,
    // An older server sends no kind; every seeded slot is a live-camera video.
    kind = kind.ifBlank { PcCareSopSlot.KIND_VIDEO },
    required = required,
    minSeconds = minDurationHintSeconds,
)

fun PcCareSopCategoryDto.toSopCategory(): PcCareSopCategory = PcCareSopCategory(
    instruction = instruction,
    proofs = proofs.map { it.toSopSlot() },
    questions = questions,
)

fun PcCareSopDto.toRules(): PcCareSopRules = PcCareSopRules(
    version = version,
    removalMode = feedWaterRemoval.mode.ifBlank { PcCareSopRules.REMOVAL_OPTIONAL },
    removalAppliesTo = feedWaterRemoval.appliesTo,
    removalCutoffTime = feedWaterRemoval.cutoffTime,
    removalInstruction = feedWaterRemoval.instruction,
    removalProofs = feedWaterRemoval.proofs.map {
        PcCareSopSlot(
            key = it.key,
            title = it.title,
            hint = it.hint,
            kind = it.kind.ifBlank { PcCareSopSlot.KIND_VIDEO },
            required = it.required,
        )
    },
    removalQuestions = feedWaterRemoval.questions,
    categories = categories.mapValues { (_, card) -> card.toSopCategory() },
)
